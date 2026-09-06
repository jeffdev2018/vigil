package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/linear"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Linear Bridge (K21): the HTTP surface. The OAuth handshake that connects a
// Linear organization to a workspace, the read and management endpoints the settings
// tab uses, and the public webhook Linear delivers to.

const (
	// InboxTypeLinearAlert tells the workspace's managers that the Linear
	// connection stopped working. Without it a revoked token is a log line and
	// the bridge silently stops mirroring.
	InboxTypeLinearAlert = "linear_alert"
	// AuditLinearConnected / AuditLinearDisconnected record the two lifecycle
	// events an admin can be asked about later.
	AuditLinearConnected    = "linear.connected"
	AuditLinearDisconnected = "linear.disconnected"

	// linearStateTTL bounds how long an authorize URL stays redeemable.
	linearStateTTL = 15 * time.Minute
	// linearMaxWebhookBody caps the body we will read from an unauthenticated
	// endpoint before the signature has been checked.
	linearMaxWebhookBody = 1 << 20
)

// LinearInstallationResponse is the settings tab's view of the connection. It
// never carries the access token or the webhook secret.
type LinearInstallationResponse struct {
	ID               string            `json:"id"`
	Connected        bool              `json:"connected"`
	Configured       bool              `json:"configured"`
	OrgID            string            `json:"linear_org_id"`
	OrgName          string            `json:"linear_org_name"`
	AgentID          string            `json:"agent_id"`
	AgentName        string            `json:"agent_name"`
	Status           string            `json:"status"`
	LastError        string            `json:"last_error"`
	StatusMap        map[string]string `json:"status_map"`
	InstalledBy      string            `json:"installed_by"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	LinearStateTypes []string          `json:"linear_state_types"`
}

// LinearLinkResponse is one mirrored issue's link.
type LinearLinkResponse struct {
	ID            string `json:"id"`
	IssueID       string `json:"issue_id"`
	LinearIssueID string `json:"linear_issue_id"`
	Identifier    string `json:"linear_issue_identifier"`
	TeamID        string `json:"linear_team_id"`
	URL           string `json:"linear_url"`
	SyncState     string `json:"sync_state"`
	LastSyncedAt  string `json:"last_synced_at"`
	LastError     string `json:"last_error"`
}

func linearInstallationToResponse(inst db.LinearInstallation, agentName string) LinearInstallationResponse {
	return LinearInstallationResponse{
		ID:               uuidToString(inst.ID),
		Connected:        true,
		Configured:       true,
		OrgID:            inst.LinearOrgID,
		OrgName:          inst.LinearOrgName,
		AgentID:          uuidToString(inst.AgentID),
		AgentName:        agentName,
		Status:           inst.Status,
		LastError:        inst.LastError,
		StatusMap:        linear.StatusMap(inst),
		InstalledBy:      uuidToString(inst.InstalledBy),
		CreatedAt:        timestampToString(inst.CreatedAt),
		UpdatedAt:        timestampToString(inst.UpdatedAt),
		LinearStateTypes: linear.LinearStateTypes,
	}
}

func linearLinkToResponse(link db.LinearIssueLink) LinearLinkResponse {
	return LinearLinkResponse{
		ID:            uuidToString(link.ID),
		IssueID:       uuidToString(link.IssueID),
		LinearIssueID: link.LinearIssueID,
		Identifier:    link.LinearIssueIdentifier,
		TeamID:        link.LinearTeamID,
		URL:           link.LinearUrl,
		SyncState:     link.SyncState,
		LastSyncedAt:  timestampToString(link.LastSyncedAt),
		LastError:     link.LastError,
	}
}

// linearEnabled reports whether the integration is wired at all: without the
// at-rest key there is nowhere safe to put the token, so every endpoint
// answers "not configured" rather than storing plaintext.
func (h *Handler) linearEnabled() bool {
	return h.Linear != nil && h.LinearSecretBox != nil
}

// GetLinearInstallation — GET /api/workspaces/{id}/linear/installation.
func (h *Handler) GetLinearInstallation(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	if !h.linearEnabled() {
		writeJSON(w, http.StatusOK, LinearInstallationResponse{Configured: false, StatusMap: linear.DefaultStatusMap(), LinearStateTypes: linear.LinearStateTypes})
		return
	}
	inst, err := h.Queries.GetLinearInstallationByWorkspace(r.Context(), wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, LinearInstallationResponse{
				Configured:       h.LinearOAuth.Configured(),
				StatusMap:        linear.DefaultStatusMap(),
				LinearStateTypes: linear.LinearStateTypes,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read linear installation")
		return
	}
	resp := linearInstallationToResponse(inst, h.linearAgentName(r.Context(), inst.AgentID))
	resp.Configured = h.LinearOAuth.Configured()
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) linearAgentName(ctx context.Context, agentID pgtype.UUID) string {
	if !agentID.Valid {
		return ""
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return ""
	}
	return agent.Name
}

// StartLinearOAuth — POST /api/workspaces/{id}/linear/oauth/start.
// Returns the authorize URL; the browser navigates to it and Linear redirects
// back to the public callback below.
func (h *Handler) StartLinearOAuth(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if !h.linearEnabled() || !h.LinearOAuth.Configured() || len(h.LinearStateSecret) == 0 {
		writeError(w, http.StatusServiceUnavailable, "linear integration is not configured on this deployment")
		return
	}
	var body struct {
		AgentID  string `json:"agent_id"`
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(body.AgentID), "agent id")
	if !ok {
		return
	}
	// The agent must belong to THIS workspace: it is the assignee every
	// mirrored issue gets, so accepting a foreign id would let one workspace
	// point Linear traffic at another's agent.
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	state, err := linear.SignState(h.LinearStateSecret, linear.StateClaims{
		WorkspaceID: uuidToString(wsUUID),
		UserID:      userID,
		AgentID:     uuidToString(agentID),
		Redirect:    body.Redirect,
		Exp:         time.Now().Add(linearStateTTL).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start linear oauth")
		return
	}
	authorizeURL, err := h.LinearOAuth.AuthorizeURLFor(state)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "linear integration is not configured on this deployment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorize_url": authorizeURL})
}

// LinearOAuthCallback — GET /api/integrations/linear/oauth/callback.
// Public: Linear redirects a browser here with no workspace in the path, so
// the signed state is the only thing that says which workspace approved.
func (h *Handler) LinearOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		h.redirectLinearResult(w, r, "", "invalid_request")
		return
	}
	if !h.linearEnabled() || !h.LinearOAuth.Configured() || len(h.LinearStateSecret) == 0 {
		h.redirectLinearResult(w, r, "", "not_configured")
		return
	}
	claims, err := linear.VerifyState(h.LinearStateSecret, state, time.Now())
	if err != nil {
		// Every state failure reports the same thing: naming which check failed
		// would tell an attacker whether they got the signature right.
		h.redirectLinearResult(w, r, "", "invalid_state")
		return
	}
	wsUUID, err := parseUUIDChecked(claims.WorkspaceID)
	if err != nil {
		h.redirectLinearResult(w, r, claims.Redirect, "invalid_state")
		return
	}
	agentID, err := parseUUIDChecked(claims.AgentID)
	if err != nil {
		h.redirectLinearResult(w, r, claims.Redirect, "invalid_state")
		return
	}

	token, err := h.LinearOAuth.Exchange(r.Context(), code)
	if err != nil {
		slog.Warn("linear: token exchange failed", "error", err, "workspace_id", claims.WorkspaceID)
		h.redirectLinearResult(w, r, claims.Redirect, "exchange_failed")
		return
	}
	client := h.newLinearClient(token.AccessToken)
	viewer, err := client.Viewer(r.Context())
	if err != nil {
		slog.Warn("linear: viewer lookup failed", "error", err, "workspace_id", claims.WorkspaceID)
		h.redirectLinearResult(w, r, claims.Redirect, "exchange_failed")
		return
	}

	secret, err := linear.NewWebhookSecret()
	if err != nil {
		h.redirectLinearResult(w, r, claims.Redirect, "install_failed")
		return
	}
	webhookID := ""
	if url := h.linearWebhookURL(); url != "" {
		webhookID, err = client.CreateWebhook(r.Context(), url, secret, []string{"Issue", "Comment"})
		if err != nil {
			// Without a webhook there is no inbound half, so this is a failed
			// install rather than a degraded one.
			slog.Warn("linear: webhookCreate failed", "error", err, "workspace_id", claims.WorkspaceID)
			h.redirectLinearResult(w, r, claims.Redirect, "webhook_failed")
			return
		}
	}

	sealedToken, err := h.LinearSecretBox.Seal([]byte(token.AccessToken))
	if err != nil {
		h.redirectLinearResult(w, r, claims.Redirect, "install_failed")
		return
	}
	sealedSecret, err := h.LinearSecretBox.Seal([]byte(secret))
	if err != nil {
		h.redirectLinearResult(w, r, claims.Redirect, "install_failed")
		return
	}
	statusMap, _ := json.Marshal(linear.DefaultStatusMap())

	inst, err := h.Queries.UpsertLinearInstallation(r.Context(), db.UpsertLinearInstallationParams{
		WorkspaceID:            wsUUID,
		AgentID:                agentID,
		LinearOrgID:            viewer.OrganizationID,
		LinearOrgName:          viewer.OrganizationName,
		ActorUserID:            viewer.ID,
		AccessTokenEncrypted:   sealedToken,
		WebhookSecretEncrypted: sealedSecret,
		LinearWebhookID:        webhookID,
		StatusMap:              statusMap,
		InstalledBy:            linearUUIDOrZero(claims.UserID),
	})
	if err != nil {
		slog.Error("linear: persist installation failed", "error", err, "workspace_id", claims.WorkspaceID)
		h.redirectLinearResult(w, r, claims.Redirect, "install_failed")
		return
	}
	h.audit(r.Context(), wsUUID, "member", claims.UserID, AuditLinearConnected, "linear_installation", inst.ID, map[string]any{
		"linear_org_id": viewer.OrganizationID, "agent_id": claims.AgentID,
	}, nil)
	h.redirectLinearResult(w, r, claims.Redirect, "")
}

// DeleteLinearInstallation — DELETE /api/workspaces/{id}/linear/installation.
func (h *Handler) DeleteLinearInstallation(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	inst, err := h.Queries.GetLinearInstallationByWorkspace(r.Context(), wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"disconnected": true})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read linear installation")
		return
	}
	// Best-effort webhook removal: a token Linear no longer accepts must not
	// leave the workspace unable to disconnect.
	if inst.LinearWebhookID != "" && h.LinearSecretBox != nil {
		if token, derr := h.LinearSecretBox.Open(inst.AccessTokenEncrypted); derr == nil {
			if werr := h.newLinearClient(string(token)).DeleteWebhook(r.Context(), inst.LinearWebhookID); werr != nil {
				slog.Warn("linear: webhookDelete failed", "error", werr, "installation_id", uuidToString(inst.ID))
			}
		}
	}
	if err := h.Queries.PurgeWorkspaceLinearCommentLinks(r.Context(), wsUUID); err != nil {
		slog.Warn("linear: purge comment links failed", "error", err)
	}
	if err := h.Queries.PurgeWorkspaceLinearIssueLinks(r.Context(), wsUUID); err != nil {
		slog.Warn("linear: purge issue links failed", "error", err)
	}
	if err := h.Queries.DeleteLinearInstallation(r.Context(), db.DeleteLinearInstallationParams{ID: inst.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disconnect linear")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, AuditLinearDisconnected, "linear_installation", inst.ID, nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"disconnected": true})
}

// UpdateLinearStatusMap — PUT /api/workspaces/{id}/linear/installation/status-map.
func (h *Handler) UpdateLinearStatusMap(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	var body struct {
		StatusMap map[string]string `json:"status_map"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	inst, err := h.Queries.GetLinearInstallationByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "linear is not connected in this workspace")
		return
	}
	// Both sides are validated at the boundary: an unknown Linear state type
	// would never match an event, and an unknown Multica status key would make
	// every inbound update fail the status write instead of here.
	cleaned := map[string]string{}
	for linearType, statusKey := range body.StatusMap {
		if !linearStateTypeAllowed(linearType) {
			writeError(w, http.StatusBadRequest, "unknown linear state type: "+linearType)
			return
		}
		key := strings.TrimSpace(statusKey)
		if key == "" {
			continue
		}
		if _, err := issuestatus.Resolve(r.Context(), h.Queries, wsUUID, key); err != nil {
			writeError(w, http.StatusBadRequest, "unknown issue status: "+key)
			return
		}
		cleaned[linearType] = key
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save status map")
		return
	}
	updated, err := h.Queries.UpdateLinearInstallationStatusMap(r.Context(), db.UpdateLinearInstallationStatusMapParams{
		ID: inst.ID, StatusMap: raw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save status map")
		return
	}
	writeJSON(w, http.StatusOK, linearInstallationToResponse(updated, h.linearAgentName(r.Context(), updated.AgentID)))
}

func linearStateTypeAllowed(t string) bool {
	for _, known := range linear.LinearStateTypes {
		if known == t {
			return true
		}
	}
	return false
}

// GetLinearLink — GET /api/workspaces/{id}/linear/links?issue_id=...
func (h *Handler) GetLinearLink(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("issue_id")), "issue id")
	if !ok {
		return
	}
	link, err := h.Queries.GetLinearIssueLinkByIssue(r.Context(), issueID)
	if err != nil || link.WorkspaceID != wsUUID {
		writeJSON(w, http.StatusOK, map[string]any{"link": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": linearLinkToResponse(link)})
}

// ResyncLinearLink — POST /api/workspaces/{id}/linear/links/{linkId}/resync.
func (h *Handler) ResyncLinearLink(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.linearWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	if !h.linearEnabled() {
		writeError(w, http.StatusServiceUnavailable, "linear integration is not configured on this deployment")
		return
	}
	linkID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "linkId"), "link id")
	if !ok {
		return
	}
	link, err := h.Queries.GetLinearIssueLinkInWorkspace(r.Context(), db.GetLinearIssueLinkInWorkspaceParams{
		ID: linkID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "linear link not found")
		return
	}
	if err := h.Linear.Resync(r.Context(), link); err != nil {
		writeError(w, http.StatusBadGateway, "linear resync failed: "+err.Error())
		return
	}
	refreshed, err := h.Queries.GetLinearIssueLinkInWorkspace(r.Context(), db.GetLinearIssueLinkInWorkspaceParams{
		ID: linkID, WorkspaceID: wsUUID,
	})
	if err != nil {
		refreshed = link
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": linearLinkToResponse(refreshed)})
}

// LinearWebhook — POST /api/integrations/linear/webhook.
//
// Public and unauthenticated by construction: Linear carries no session. The
// HMAC signature over the raw body IS the authentication, so nothing is read
// out of the payload before it verifies.
func (h *Handler) LinearWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.linearEnabled() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, linearMaxWebhookBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}
	signature := r.Header.Get(linear.SignatureHeader)

	// organizationId narrows the candidates; the signature picks the exact
	// installation, so the same Linear org connected from two workspaces still
	// routes correctly.
	var envelope struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.OrganizationID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	candidates, err := h.Queries.ListLinearInstallationsByOrg(r.Context(), envelope.OrganizationID)
	if err != nil || len(candidates) == 0 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	for _, inst := range candidates {
		secret, err := h.Linear.WebhookSecret(inst)
		if err != nil {
			continue
		}
		ev, err := linear.ParseWebhook(body, signature, secret, time.Now())
		if err != nil {
			continue
		}
		h.dispatchLinearEvent(r.Context(), inst, ev)
		// 200 once the signature verified, whatever the bridge made of the
		// event: Linear retries on a non-2xx, and retrying a payload we cannot
		// act on would just repeat forever.
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
}

func (h *Handler) dispatchLinearEvent(ctx context.Context, inst db.LinearInstallation, ev linear.WebhookEvent) {
	var err error
	switch ev.Type {
	case "Issue":
		err = h.Linear.HandleIssueEvent(ctx, inst, ev)
	case "Comment":
		err = h.Linear.HandleCommentEvent(ctx, inst, ev)
	default:
		return
	}
	if err != nil {
		slog.Warn("linear: webhook handling failed",
			"error", err, "type", ev.Type, "action", ev.Action,
			"installation_id", uuidToString(inst.ID))
	}
}

// ---------------------------------------------------------------------------
// Bridge host wiring
// ---------------------------------------------------------------------------

// CreateMirrorIssue implements linear.IssueCreator. Going through
// IssueService.Create rather than an INSERT is what gives a mirrored issue the
// ordinary agent-assigned auto-enqueue — the run, its profile and its budget
// are the same ones any other assigned issue gets.
func (h *Handler) CreateMirrorIssue(ctx context.Context, in linear.MirrorIssueInput) (db.Issue, error) {
	status := in.Status
	if status == "" {
		status = issuestatus.Todo
	}
	if _, err := issuestatus.Resolve(ctx, h.Queries, in.WorkspaceID, status); err != nil {
		status = issuestatus.Todo
	}
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID:    in.WorkspaceID,
		Title:          in.Title,
		Description:    pgtype.Text{String: in.Description, Valid: in.Description != ""},
		Status:         status,
		Priority:       "none",
		AssigneeType:   pgtype.Text{String: "agent", Valid: true},
		AssigneeID:     in.AgentID,
		CreatorType:    "member",
		CreatorID:      in.CreatedBy,
		OriginType:     pgtype.Text{String: "linear", Valid: true},
		OriginID:       in.InstallationID,
		AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: uuidToString(in.CreatedBy)})
	if err != nil {
		return db.Issue{}, err
	}
	return res.Issue, nil
}

// PublishLinearComment broadcasts a comment the bridge wrote, so an open
// timeline shows it without a refetch.
func (h *Handler) PublishLinearComment(ctx context.Context, issue db.Issue, comment db.Comment, issueRevision int64) {
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      issueRevision,
	})
}

// PublishLinearIssue broadcasts a mirrored issue update. status_changed is
// false on purpose: this update CAME from Linear, and marking it as a status
// change would make the outbound subscriber push it straight back.
func (h *Handler) PublishLinearIssue(ctx context.Context, issue db.Issue) {
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue":          map[string]any{"id": uuidToString(issue.ID), "status": issue.Status},
		"status_changed": false,
	})
}

// NoteLinearInstallationBroken files the inbox alert for a connection Linear
// stopped accepting. Same accountability rule as the routing alert: the
// workspace's managers are who can reconnect it.
func (h *Handler) NoteLinearInstallationBroken(ctx context.Context, inst db.LinearInstallation, reason string) {
	managers, err := h.Queries.ListWorkspaceManagerUserIDs(ctx, inst.WorkspaceID)
	if err != nil {
		slog.Warn("linear alert: list managers failed", "error", err, "workspace_id", uuidToString(inst.WorkspaceID))
		return
	}
	// One alert per manager per day: a broken token fails on every push, and
	// an item per failure is a reason to mute the inbox rather than fix it.
	day := time.Now().UTC().Format("2006-01-02") + ":linear:" + uuidToString(inst.ID)
	details, _ := json.Marshal(map[string]any{
		"installation_id": uuidToString(inst.ID),
		"linear_org_id":   inst.LinearOrgID,
		"day":             day,
	})
	for _, userID := range managers {
		already, err := h.Queries.CountInboxItemsForDay(ctx, db.CountInboxItemsForDayParams{
			WorkspaceID: inst.WorkspaceID, RecipientID: userID, Type: InboxTypeLinearAlert, Day: day,
		})
		if err != nil {
			slog.Warn("linear alert: dedup check failed", "error", err)
		}
		if already > 0 {
			continue
		}
		if _, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: inst.WorkspaceID, RecipientType: "member", RecipientID: userID,
			Type: InboxTypeLinearAlert, Severity: "action_required",
			Title:   truncate("Linear is disconnected: reconnect to resume syncing", 120),
			Body:    pgtype.Text{String: truncate(reason, 1000), Valid: true},
			Details: details,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("linear alert: inbox failed", "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (h *Handler) linearWorkspaceFromURL(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

// newLinearClient builds an API for a token, honouring the endpoint override
// tests point at an httptest server.
func (h *Handler) newLinearClient(token string) linear.API {
	if h.LinearClientFactory != nil {
		return h.LinearClientFactory(token)
	}
	return linear.NewClient(token)
}

// linearWebhookURL is the public address Linear delivers to.
func (h *Handler) linearWebhookURL() string {
	base := strings.TrimRight(strings.TrimSpace(h.LinearPublicURL), "/")
	if base == "" {
		return ""
	}
	return base + "/api/integrations/linear/webhook"
}

// redirectLinearResult sends the browser back to the settings tab, carrying
// the outcome in the query string. An empty errCode means success.
func (h *Handler) redirectLinearResult(w http.ResponseWriter, r *http.Request, redirect, errCode string) {
	target := strings.TrimSpace(redirect)
	if !isSafeRelativePath(target) {
		target = "/settings/integrations"
	}
	u, err := url.Parse(target)
	if err != nil {
		u = &url.URL{Path: "/settings/integrations"}
	}
	q := u.Query()
	if errCode == "" {
		q.Set("linear", "connected")
	} else {
		q.Set("linear_error", errCode)
	}
	u.RawQuery = q.Encode()
	base := strings.TrimRight(strings.TrimSpace(h.LinearAppURL), "/")
	http.Redirect(w, r, base+u.String(), http.StatusFound)
}

// isSafeRelativePath refuses anything that could send the browser off-site.
// The redirect comes out of a signed state, but it was put there by a client,
// so it is still untrusted input at this boundary.
func isSafeRelativePath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// parseUUIDChecked is the safe parse for ids arriving from the OAuth state.
// The state is signed, but it was still assembled from client input, so the
// callback must reject a malformed id rather than panic on it.
func parseUUIDChecked(s string) (pgtype.UUID, error) {
	return util.ParseUUID(s)
}

func linearUUIDOrZero(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
