package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/push"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Mobile push (K64/K63). The app registers its Expo token; the server
// pushes the morning digest and new Decision Cards, carrying the badge
// (my unanswered decisions) so the app icon shows the number.

const AuditPushSent = "push.sent"

// RegisterPushToken: PUT /api/me/push-token {token, platform}.
func (h *Handler) RegisterPushToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || !push.IsExpoToken(strings.TrimSpace(req.Token)) {
		writeError(w, http.StatusBadRequest, "token must be an Expo push token")
		return
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform != "ios" && platform != "android" {
		platform = "ios"
	}
	row, err := h.Queries.UpsertMobilePushToken(r.Context(), db.UpsertMobilePushTokenParams{ID: dbid.NewV7(), UserID: parseUUID(userID), Token: strings.TrimSpace(req.Token), Platform: platform})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register the push token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": uuidToString(row.ID), "platform": row.Platform, "registered": true})
}

// UnregisterPushToken: DELETE /api/me/push-token {token}.
func (h *Handler) UnregisterPushToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if err := h.Queries.DeleteMobilePushToken(r.Context(), db.DeleteMobilePushTokenParams{UserID: parseUUID(userID), Token: strings.TrimSpace(req.Token)}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the push token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registered": false})
}

// pushToUsers sends one notification to every device of the given users,
// with the badge set to each user's unanswered decisions in the workspace.
// Best effort: no sender or no token means nothing happens.
func (h *Handler) pushToUsers(ctx context.Context, wsID pgtype.UUID, userIDs []pgtype.UUID, title, body string, data map[string]any) int {
	if h.Push == nil || len(userIDs) == 0 {
		return 0
	}
	tokens, err := h.Queries.ListMobilePushTokensForUsers(ctx, userIDs)
	if err != nil || len(tokens) == 0 {
		return 0
	}
	badges := map[string]int{}
	msgs := make([]push.Message, 0, len(tokens))
	for _, t := range tokens {
		uid := uuidToString(t.UserID)
		badge, ok := badges[uid]
		if !ok {
			n, err := h.Queries.CountPendingDecisionInboxItems(ctx, db.CountPendingDecisionInboxItemsParams{WorkspaceID: wsID, RecipientID: t.UserID})
			if err == nil {
				badge = int(n)
			}
			badges[uid] = badge
		}
		b := badge
		msgs = append(msgs, push.Message{To: t.Token, Title: title, Body: body, Badge: &b, Data: data, Sound: "default"})
	}
	tickets, err := h.Push.Send(ctx, msgs)
	if err != nil {
		slog.Warn("push: send failed", "error", err, "workspace_id", uuidToString(wsID))
	}
	sent := 0
	for i, t := range tickets {
		if t.Status == "ok" {
			sent++
			continue
		}
		if t.Details.Error == "DeviceNotRegistered" && i < len(msgs) {
			_ = h.Queries.DeleteMobilePushTokenEverywhere(ctx, msgs[i].To)
		}
	}
	h.audit(ctx, wsID, "system", "", AuditPushSent, "workspace", wsID, map[string]any{"title": title, "devices": len(msgs), "accepted": sent}, nil)
	return sent
}
