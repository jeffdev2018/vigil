package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/twenty"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
)

// Twenty CRM integration handlers (OS plan, chantier 2). A connection belongs
// to a workspace: reading it is a member's right, changing it an owner's or
// admin's. Every management endpoint answers 503 when the integration is not
// configured (MULTICA_TWENTY_SECRET_KEY unset), like the other integrations.
// The inbound webhook is public: the token in the path names the workspace and
// the HMAC signature proves the delivery came from that workspace's Twenty.

const (
	maxTwentyWebhookBytes = 1 << 20
	AuditTwentyConnected  = "twenty.connected"
	AuditTwentyUpdated    = "twenty.settings_updated"
	AuditTwentyDisconnect = "twenty.disconnected"
	AuditTwentyBacklinked = "twenty.backlinked"
)

// TwentyConnectRequest is the POST /connect body.
type TwentyConnectRequest struct {
	BaseURL        string   `json:"base_url"`
	APIKey         string   `json:"api_key"`
	Events         []string `json:"events"`
	ExposeToAgents *bool    `json:"expose_to_agents"`
}

// TwentySettingsRequest is the PUT /settings body.
type TwentySettingsRequest struct {
	Events         []string `json:"events"`
	ExposeToAgents bool     `json:"expose_to_agents"`
}

// TwentyStatusResponse is what GET returns: connected or not, and the
// connection when there is one. `available` says whether the operator
// configured the integration at all, so the settings page can explain a
// missing card instead of showing a broken one.
type TwentyStatusResponse struct {
	Available  bool               `json:"available"`
	Connected  bool               `json:"connected"`
	Connection *twenty.Connection `json:"connection,omitempty"`
	Events     []string           `json:"default_events"`
}

func (h *Handler) twentyReady(w http.ResponseWriter) bool {
	if h.Twenty == nil {
		writeError(w, http.StatusServiceUnavailable, "twenty integration not configured (set MULTICA_TWENTY_SECRET_KEY)")
		return false
	}
	return true
}

// GetTwentyConnection: GET /api/integrations/twenty.
func (h *Handler) GetTwentyConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	resp := TwentyStatusResponse{Available: h.Twenty != nil, Events: twenty.DefaultEvents}
	if h.Twenty == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	var conn twenty.Connection
	var err error
	if roleAllowed(member.Role, "owner", "admin") {
		conn, err = h.Twenty.GetForAdmin(r.Context(), wsUUID)
	} else {
		conn, err = h.Twenty.Get(r.Context(), wsUUID)
	}
	switch {
	case errors.Is(err, twenty.ErrNotConnected):
	case err != nil:
		slog.Error("twenty: load connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load the Twenty connection")
		return
	default:
		resp.Connected = true
		resp.Connection = &conn
	}
	writeJSON(w, http.StatusOK, resp)
}

// ConnectTwenty: POST /api/integrations/twenty/connect. Validates the key
// against the instance, seals it, mints the inbound token, registers the
// webhook. The clear inbound token is in this response only.
func (h *Handler) ConnectTwenty(w http.ResponseWriter, r *http.Request) {
	if !h.twentyReady(w) {
		return
	}
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	var req TwentyConnectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.BaseURL) == "" || strings.TrimSpace(req.APIKey) == "" {
		writeError(w, http.StatusBadRequest, "base_url and api_key are required")
		return
	}
	expose := true
	if req.ExposeToAgents != nil {
		expose = *req.ExposeToAgents
	}
	conn, err := h.Twenty.Connect(r.Context(), wsUUID, twenty.ConnectParams{
		BaseURL: req.BaseURL, APIKey: req.APIKey, Events: req.Events, ExposeToAgents: expose, CreatedBy: userUUID,
	})
	if err != nil {
		writeTwentyError(w, err)
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditTwentyConnected, "workspace", wsUUID,
		map[string]any{"base_url": conn.BaseURL, "events": conn.Events, "expose_to_agents": conn.ExposeToAgents, "webhook_registered": conn.WebhookRegistered}, nil)
	writeJSON(w, http.StatusCreated, conn)
}

// PutTwentySettings: PUT /api/integrations/twenty/settings.
func (h *Handler) PutTwentySettings(w http.ResponseWriter, r *http.Request) {
	if !h.twentyReady(w) {
		return
	}
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req TwentySettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	conn, err := h.Twenty.UpdateSettings(r.Context(), wsUUID, req.Events, req.ExposeToAgents)
	if err != nil {
		writeTwentyError(w, err)
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditTwentyUpdated, "workspace", wsUUID,
		map[string]any{"events": conn.Events, "expose_to_agents": conn.ExposeToAgents}, nil)
	writeJSON(w, http.StatusOK, conn)
}

// DisconnectTwenty: DELETE /api/integrations/twenty.
func (h *Handler) DisconnectTwenty(w http.ResponseWriter, r *http.Request) {
	if !h.twentyReady(w) {
		return
	}
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	if err := h.Twenty.Disconnect(r.Context(), wsUUID); err != nil {
		writeTwentyError(w, err)
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditTwentyDisconnect, "workspace", wsUUID, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// CheckTwentyConnection: POST /api/integrations/twenty/check re-validates the
// key and records the outcome on the row.
func (h *Handler) CheckTwentyConnection(w http.ResponseWriter, r *http.Request) {
	if !h.twentyReady(w) {
		return
	}
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	conn, err := h.Twenty.Check(r.Context(), wsUUID)
	if err != nil {
		writeTwentyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conn)
}

// ListTwentyMembers: GET /api/integrations/twenty/members pairs this
// workspace's members with Twenty's by email.
func (h *Handler) ListTwentyMembers(w http.ResponseWriter, r *http.Request) {
	if !h.twentyReady(w) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	links, err := h.Twenty.Members(r.Context(), wsUUID)
	if err != nil {
		writeTwentyError(w, err)
		return
	}
	if links == nil {
		links = []twenty.MemberLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": links})
}

func writeTwentyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, twenty.ErrNotConnected):
		writeError(w, http.StatusNotFound, "the workspace is not connected to Twenty")
	case errors.Is(err, twenty.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, twenty.ErrUpstream):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		slog.Error("twenty: request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "twenty request failed")
	}
}

// HandleInboundTwentyWebhook: POST /api/triage/inbound/twenty/{token}. Public
// on purpose: the token resolves the workspace's source row, the signature
// (HMAC-SHA256 over `${timestamp}:${body}` with the secret Twenty minted for
// this subscription) proves the sender, and the timestamp bounds replay.
func (h *Handler) HandleInboundTwentyWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" || h.Twenty == nil {
		writeError(w, http.StatusNotFound, "intake endpoint not found")
		return
	}
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), ip) {
		writeWebhookRateLimit(w, r, h.WebhookAbsoluteIPRateLimiter, ip, "absolute_ip", h.Metrics)
		return
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !slidingWindowLimiterCheck(r.Context(), h.WebhookIPRateLimiter, ip) {
		writeWebhookRateLimit(w, r, h.WebhookIPRateLimiter, ip, "bad_credential_ip", h.Metrics)
		return
	}
	badCredential := func() {
		if ip != "" && h.WebhookIPRateLimiter != nil {
			h.WebhookIPRateLimiter.Allow(r.Context(), ip)
		}
	}

	source, err := h.Queries.GetTriageSourceByTokenHash(r.Context(), twenty.HashToken(token))
	if err != nil || source.Kind != triage.SourceTwenty {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("twenty intake: token lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		badCredential()
		writeError(w, http.StatusNotFound, "intake endpoint not found")
		return
	}
	inbound, err := h.Twenty.InboundFor(r.Context(), source.WorkspaceID)
	if err != nil {
		if errors.Is(err, twenty.ErrNotConnected) {
			badCredential()
			writeError(w, http.StatusNotFound, "intake endpoint not found")
			return
		}
		slog.Error("twenty intake: connection unavailable", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxTwentyWebhookBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	if inbound.Secret == "" {
		// Never admit an unverifiable delivery: without the secret Twenty
		// minted, anyone who learned the URL could file items.
		badCredential()
		writeError(w, http.StatusUnauthorized, "no signing secret on record for this subscription; reconnect Twenty")
		return
	}
	if err := twenty.VerifySignature(inbound.Secret, r.Header.Get("X-Twenty-Webhook-Timestamp"), r.Header.Get("X-Twenty-Webhook-Signature"), raw, time.Now()); err != nil {
		badCredential()
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	event, err := twenty.ParseEvent(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Twenty webhook payload")
		return
	}
	if !twenty.Matches(inbound.Events, event.EventName) {
		// Subscribed in Twenty but not in the workspace's settings (they can
		// drift while a re-registration fails): acknowledged, not queued.
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "ignored"})
		return
	}

	params := twenty.CaptureParamsFor(source, inbound.BaseURL, event)
	if triage.Decide(source.Mode) == triage.RouteDrop {
		params.State = triage.StateDropped
		params.DropReason = "source_blocked"
		h.captureTriageInbound(r.Context(), params)
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
		return
	}
	if _, ok := h.captureTriageInbound(r.Context(), params); !ok {
		writeError(w, http.StatusInternalServerError, "failed to record the event")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}

// twentyBacklink files a Task on the CRM record an accepted item came from.
// Runs after the accept response: the issue exists whether or not Twenty
// answers. Only items captured from a Twenty source qualify.
func (h *Handler) twentyBacklink(ctx context.Context, workspaceID, itemID pgtype.UUID, res acceptResult, actorID string) {
	if h.Twenty == nil {
		return
	}
	item, err := h.Queries.GetTriageItemWithSourceKind(ctx, itemID)
	if err != nil || item.SourceKind != triage.SourceTwenty {
		return
	}
	event := triage.StoredBody(item.Payload)
	if len(event) == 0 {
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("twenty backlink: workspace lookup failed", "error", err)
		return
	}
	identifier := fmt.Sprintf("%s-%d", res.prefix, res.issue.Number)
	issueURL := strings.TrimRight(h.cfg.AppURL, "/") + "/" + ws.Slug + "/issues/" + util.UUIDToString(res.issue.ID)
	if err := h.Twenty.Backlink(ctx, workspaceID, event, identifier, res.issue.Title, issueURL); err != nil {
		slog.Warn("twenty backlink: task not filed", "issue", identifier, "error", err)
		return
	}
	h.audit(ctx, workspaceID, "member", actorID, AuditTwentyBacklinked, "issue", res.issue.ID, map[string]any{"item_id": util.UUIDToString(itemID), "identifier": identifier}, nil)
}
