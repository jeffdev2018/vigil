package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SCIM 2.0 provisioning (K60): an identity provider creates and removes a
// workspace's members with a dedicated token. Deprovisioning revokes access
// in the same request: the membership is removed with the full revocation
// sweep and every session of the user is invalidated before the response.

const (
	scimUserSchema     = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimListSchema     = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimPatchSchema    = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	AuditScimProvision = "scim.provisioned"
	AuditScimRevoke    = "scim.deprovisioned"
	AuditScimToken     = "scim.token_issued"
)

// --- token management (workspace owner) ------------------------------------------

type ScimTokenResponse struct {
	ID         string  `json:"id"`
	Hint       string  `json:"token_hint"`
	Active     bool    `json:"active"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	// Token is present once, at creation.
	Token string `json:"token,omitempty"`
}

func scimTokenToResponse(t db.ScimToken) ScimTokenResponse {
	return ScimTokenResponse{ID: uuidToString(t.ID), Hint: t.TokenHint, Active: t.Active, CreatedAt: timestampToString(t.CreatedAt), LastUsedAt: timestampToPtr(t.LastUsedAt)}
}

// GET /api/workspaces/{id}/scim-tokens — owner/admin.
func (h *Handler) ListScimTokens(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	rows, err := h.Queries.ListScimTokens(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list SCIM tokens")
		return
	}
	out := make([]ScimTokenResponse, 0, len(rows))
	for _, t := range rows {
		out = append(out, scimTokenToResponse(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// POST /api/workspaces/{id}/scim-tokens — owner only; shown once, the
// previous active token is retired so one provisioning source is in force.
func (h *Handler) CreateScimToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner"); !ok {
		return
	}
	token := middleware.SCIMTokenPrefix + randomURLToken(36)
	if err := h.Queries.DeactivateScimTokens(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate SCIM tokens")
		return
	}
	row, err := h.Queries.CreateScimToken(r.Context(), db.CreateScimTokenParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, TokenHash: middleware.HashSCIMToken(token), TokenHint: token[:9] + "…" + token[len(token)-4:], CreatedBy: parseUUID(requestUserID(r))})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the SCIM token")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditScimToken, "workspace", wsUUID, map[string]any{"token_id": uuidToString(row.ID)}, nil)
	resp := scimTokenToResponse(row)
	resp.Token = token
	writeJSON(w, http.StatusCreated, resp)
}

// DELETE /api/workspaces/{id}/scim-tokens/{tokenId} — owner only.
func (h *Handler) DeleteScimToken(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner"); !ok {
		return
	}
	tokenUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "tokenId"), "token id")
	if !ok {
		return
	}
	rows, err := h.Queries.DeactivateScimToken(r.Context(), db.DeactivateScimTokenParams{ID: tokenUUID, WorkspaceID: wsUUID})
	if err != nil || rows == 0 {
		writeError(w, http.StatusNotFound, "no active token with this id")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- SCIM resources ---------------------------------------------------------------

type scimName struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Formatted  string `json:"formatted,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
	Type    string `json:"type,omitempty"`
}

type scimUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        scimName    `json:"name"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []scimEmail `json:"emails"`
	Active      bool        `json:"active"`
	Meta        scimMeta    `json:"meta"`
}

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	Location     string `json:"location,omitempty"`
}

func writeSCIM(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSCIMError(w http.ResponseWriter, status int, detail string) {
	writeSCIM(w, status, map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"}, "status": strconv.Itoa(status), "detail": detail})
}

func scimUserOf(r *http.Request, m db.ListMembersWithUserRow, externalID string) scimUser {
	return scimUser{
		Schemas: []string{scimUserSchema}, ID: uuidToString(m.ID), ExternalID: externalID, UserName: m.UserEmail,
		Name: scimName{Formatted: m.UserName}, DisplayName: m.UserName, Emails: []scimEmail{{Value: m.UserEmail, Primary: true, Type: "work"}}, Active: true,
		Meta: scimMeta{ResourceType: "User", Created: timestampToString(m.CreatedAt), Location: "/scim/v2/Users/" + uuidToString(m.ID)},
	}
}

func scimWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	wsUUID, err := parseUUIDString(r.Header.Get("X-Workspace-ID"))
	if err != nil {
		writeSCIMError(w, http.StatusUnauthorized, "no workspace bound to this token")
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

func parseUUIDString(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(strings.TrimSpace(s)); err != nil || !id.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid")
	}
	return id, nil
}

// GET /scim/v2/ServiceProviderConfig
func (h *Handler) ScimServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	writeSCIM(w, http.StatusOK, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":   map[string]any{"supported": true}, "bulk": map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter": map[string]any{"supported": true, "maxResults": 200}, "changePassword": map[string]any{"supported": false},
		"sort": map[string]any{"supported": false}, "etag": map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "Bearer token", "description": "A SCIM token issued in the workspace settings"}},
	})
}

// GET /scim/v2/ResourceTypes
func (h *Handler) ScimResourceTypes(w http.ResponseWriter, r *http.Request) {
	writeSCIM(w, http.StatusOK, map[string]any{"schemas": []string{scimListSchema}, "totalResults": 1, "Resources": []map[string]any{{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User", "endpoint": "/Users", "schema": scimUserSchema}}})
}

var scimUserNameFilterRe = regexp.MustCompile(`(?i)^userName\s+eq\s+"([^"]+)"$`)

// GET /scim/v2/Users?filter=userName eq "x"&startIndex=1&count=100
func (h *Handler) ScimListUsers(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	wantEmail := ""
	if filter != "" {
		m := scimUserNameFilterRe.FindStringSubmatch(filter)
		if m == nil {
			writeSCIMError(w, http.StatusBadRequest, "only the filter userName eq \"value\" is supported")
			return
		}
		wantEmail = strings.ToLower(m[1])
	}
	externals := h.scimExternalIDs(r.Context(), wsUUID)
	var all []scimUser
	for _, m := range members {
		if wantEmail != "" && strings.ToLower(m.UserEmail) != wantEmail {
			continue
		}
		all = append(all, scimUserOf(r, m, externals[uuidToString(m.ID)]))
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if start < 1 {
		start = 1
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 || count > 200 {
		count = 100
	}
	page := []scimUser{}
	for i := start - 1; i < len(all) && len(page) < count; i++ {
		page = append(page, all[i])
	}
	writeSCIM(w, http.StatusOK, map[string]any{"schemas": []string{scimListSchema}, "totalResults": len(all), "startIndex": start, "itemsPerPage": len(page), "Resources": page})
}

func (h *Handler) scimExternalIDs(ctx context.Context, wsUUID pgtype.UUID) map[string]string {
	out := map[string]string{}
	rows, err := h.Queries.ListMembers(ctx, wsUUID)
	if err != nil {
		return out
	}
	for _, m := range rows {
		if m.ScimExternalID.Valid {
			out[uuidToString(m.ID)] = m.ScimExternalID.String
		}
	}
	return out
}

func (h *Handler) scimMember(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.ListMembersWithUserRow, bool) {
	id, err := parseUUIDString(chi.URLParam(r, "id"))
	if err != nil {
		writeSCIMError(w, http.StatusNotFound, "user not found")
		return db.ListMembersWithUserRow{}, false
	}
	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "failed to load users")
		return db.ListMembersWithUserRow{}, false
	}
	for _, m := range members {
		if m.ID == id {
			return m, true
		}
	}
	writeSCIMError(w, http.StatusNotFound, "user not found")
	return db.ListMembersWithUserRow{}, false
}

// GET /scim/v2/Users/{id}
func (h *Handler) ScimGetUser(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	m, ok := h.scimMember(w, r, wsUUID)
	if !ok {
		return
	}
	writeSCIM(w, http.StatusOK, scimUserOf(r, m, h.scimExternalIDs(r.Context(), wsUUID)[uuidToString(m.ID)]))
}

type scimUserRequest struct {
	ExternalID  string      `json:"externalId"`
	UserName    string      `json:"userName"`
	Name        scimName    `json:"name"`
	DisplayName string      `json:"displayName"`
	Emails      []scimEmail `json:"emails"`
	Active      *bool       `json:"active"`
}

func (req scimUserRequest) email() string {
	for _, e := range req.Emails {
		if e.Primary && e.Value != "" {
			return strings.ToLower(strings.TrimSpace(e.Value))
		}
	}
	if len(req.Emails) > 0 && req.Emails[0].Value != "" {
		return strings.ToLower(strings.TrimSpace(req.Emails[0].Value))
	}
	return strings.ToLower(strings.TrimSpace(req.UserName))
}

func (req scimUserRequest) displayName() string {
	if req.DisplayName != "" {
		return req.DisplayName
	}
	if req.Name.Formatted != "" {
		return req.Name.Formatted
	}
	return strings.TrimSpace(req.Name.GivenName + " " + req.Name.FamilyName)
}

// POST /scim/v2/Users — creates the user (if new) and its membership.
func (h *Handler) ScimCreateUser(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	var req scimUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalid body")
		return
	}
	email := req.email()
	if !strings.Contains(email, "@") {
		writeSCIMError(w, http.StatusBadRequest, "userName must be an email address")
		return
	}
	if req.Active != nil && !*req.Active {
		writeSCIMError(w, http.StatusBadRequest, "an inactive user cannot be created")
		return
	}
	user, _, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "failed to create the user")
		return
	}
	if name := req.displayName(); name != "" && name != user.Name {
		if err := h.Queries.SetUserName(r.Context(), db.SetUserNameParams{ID: user.ID, Name: name}); err == nil {
			user.Name = name
		}
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: wsUUID}); err == nil {
		writeSCIMError(w, http.StatusConflict, "user already provisioned")
		return
	}
	member, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{WorkspaceID: wsUUID, UserID: user.ID, Role: "member"})
	if err != nil {
		writeSCIMError(w, http.StatusInternalServerError, "failed to add the member")
		return
	}
	if req.ExternalID != "" {
		_ = h.Queries.SetMemberScimExternalID(r.Context(), db.SetMemberScimExternalIDParams{ID: member.ID, ScimExternalID: pgtype.Text{String: req.ExternalID, Valid: true}})
	}
	h.MembershipCache.Invalidate(r.Context(), uuidToString(user.ID), uuidToString(wsUUID))
	h.audit(r.Context(), wsUUID, "system", "", AuditScimProvision, "member", member.ID, map[string]any{"email": email, "external_id": req.ExternalID}, nil)
	h.publish(protocol.EventMemberAdded, uuidToString(wsUUID), "system", "", map[string]any{"member_id": uuidToString(member.ID), "workspace_id": uuidToString(wsUUID), "user_id": uuidToString(user.ID)})
	row := db.ListMembersWithUserRow{ID: member.ID, WorkspaceID: wsUUID, UserID: user.ID, Role: member.Role, CreatedAt: member.CreatedAt, UserName: user.Name, UserEmail: user.Email, UserAvatarUrl: user.AvatarUrl}
	writeSCIM(w, http.StatusCreated, scimUserOf(r, row, req.ExternalID))
}

// PUT /scim/v2/Users/{id} — replace: only `active` changes anything here;
// names follow the identity provider, the email is the identity.
func (h *Handler) ScimReplaceUser(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	m, ok := h.scimMember(w, r, wsUUID)
	if !ok {
		return
	}
	var req scimUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Active != nil && !*req.Active {
		h.scimDeprovision(r.Context(), wsUUID, m, "put")
		writeSCIM(w, http.StatusOK, h.scimInactive(m, req.ExternalID))
		return
	}
	if name := req.displayName(); name != "" && name != m.UserName {
		if err := h.Queries.SetUserName(r.Context(), db.SetUserNameParams{ID: m.UserID, Name: name}); err == nil {
			m.UserName = name
		}
	}
	writeSCIM(w, http.StatusOK, scimUserOf(r, m, req.ExternalID))
}

// PATCH /scim/v2/Users/{id} — the deactivation every provider sends.
func (h *Handler) ScimPatchUser(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	m, ok := h.scimMember(w, r, wsUUID)
	if !ok {
		return
	}
	var req struct {
		Operations []struct {
			Op    string `json:"op"`
			Path  string `json:"path"`
			Value any    `json:"value"`
		} `json:"Operations"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalid body")
		return
	}
	deactivate := false
	for _, op := range req.Operations {
		if !strings.EqualFold(op.Op, "replace") && !strings.EqualFold(op.Op, "add") {
			continue
		}
		active, found := scimActiveValue(op.Path, op.Value)
		if found && !active {
			deactivate = true
		}
	}
	if deactivate {
		h.scimDeprovision(r.Context(), wsUUID, m, "patch")
		writeSCIM(w, http.StatusOK, h.scimInactive(m, ""))
		return
	}
	writeSCIM(w, http.StatusOK, scimUserOf(r, m, h.scimExternalIDs(r.Context(), wsUUID)[uuidToString(m.ID)]))
}

func scimActiveValue(path string, value any) (bool, bool) {
	switch {
	case strings.EqualFold(path, "active"):
		return scimBool(value)
	case path == "":
		if obj, ok := value.(map[string]any); ok {
			if v, ok := obj["active"]; ok {
				return scimBool(v)
			}
		}
	}
	return false, false
}

func scimBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		return strings.EqualFold(t, "true"), true
	}
	return false, false
}

// DELETE /scim/v2/Users/{id}
func (h *Handler) ScimDeleteUser(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := scimWorkspace(w, r)
	if !ok {
		return
	}
	m, ok := h.scimMember(w, r, wsUUID)
	if !ok {
		return
	}
	h.scimDeprovision(r.Context(), wsUUID, m, "delete")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) scimInactive(m db.ListMembersWithUserRow, externalID string) scimUser {
	u := scimUserOf(nil, m, externalID)
	u.Active = false
	return u
}

// scimDeprovision removes the membership with the full revocation sweep and
// invalidates the user's sessions before returning: access is gone when the
// identity provider gets its answer.
func (h *Handler) scimDeprovision(ctx context.Context, wsUUID pgtype.UUID, m db.ListMembersWithUserRow, via string) {
	result, err := h.revokeAndRemoveMember(ctx, wsUUID, m.UserID, m.ID, pgtype.UUID{})
	if err != nil {
		slog.Warn("scim: remove member failed", "member_id", uuidToString(m.ID), "error", err)
	}
	if err := h.Queries.InvalidateUserSessions(ctx, m.UserID); err != nil {
		slog.Warn("scim: invalidate sessions failed", "user_id", uuidToString(m.UserID), "error", err)
	}
	if middleware.Revocations != nil {
		middleware.Revocations.Invalidate(ctx, uuidToString(m.UserID), time.Now())
	}
	h.MembershipCache.Invalidate(ctx, uuidToString(m.UserID), uuidToString(wsUUID))
	wsIDStr := uuidToString(wsUUID)
	logRevocation(result, wsIDStr, uuidToString(m.UserID))
	h.publishRevocation(ctx, result, wsIDStr, "system", "")
	h.audit(ctx, wsUUID, "system", "", AuditScimRevoke, "member", m.ID, map[string]any{"email": m.UserEmail, "via": via}, nil)
	h.publish(protocol.EventMemberRemoved, wsIDStr, "system", "", map[string]any{"member_id": uuidToString(m.ID), "workspace_id": wsIDStr, "user_id": uuidToString(m.UserID)})
	h.notifyDaemonWorkspacesChanged(uuidToString(m.UserID))
}
