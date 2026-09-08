package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Multiplayer chat participants (K31 / JEF-181).
//
// A chat_session is owned by its creator and read/written by any participant.
// The creator has no chat_session_participant row — they are the implicit
// owner, which is why every roster response synthesizes them at the head of
// the list and why no backfill was needed for sessions that predate the table.

type ChatParticipantResponse struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
	Role      string  `json:"role"`
	JoinedAt  string  `json:"joined_at"`
	// Online means this user currently holds a WebSocket connection to this
	// node. See chatParticipantOnline for what that does and does not prove.
	Online bool `json:"online"`
}

type ChatParticipantListResponse struct {
	Participants []ChatParticipantResponse `json:"participants"`
}

type AddChatParticipantRequest struct {
	UserID string `json:"user_id"`
}

// chatParticipantOnline reports whether the user has a live realtime
// connection. Every client auto-subscribes to its own ScopeUser room at
// connect time, so the room's existence is the cheapest liveness signal the
// server already maintains — no heartbeat table, no presence store.
//
// ponytail: node-local. In a multi-node deployment a user connected to another
// API node reads as offline here. Promote to a Redis-backed presence key (the
// runtime liveness store already does exactly this) if that becomes visible.
func (h *Handler) chatParticipantOnline(userID string) bool {
	if h.Hub == nil {
		return false
	}
	return h.Hub.HasLocalSubscribers(realtime.ScopeUser, userID)
}

// ListChatParticipants returns the session roster: the implicit creator-owner
// first, then everyone who was added, in join order.
func (h *Handler) ListChatParticipants(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}

	out := make([]ChatParticipantResponse, 0, 4)
	// The creator row is synthesized, not stored. Their user row is loaded
	// separately because the roster query joins only stored participants.
	if creator, err := h.Queries.GetUser(r.Context(), session.CreatorID); err == nil {
		out = append(out, ChatParticipantResponse{
			UserID:    uuidToString(creator.ID),
			Name:      creator.Name,
			AvatarURL: textToPtr(creator.AvatarUrl),
			Role:      "owner",
			JoinedAt:  timestampToString(session.CreatedAt),
			Online:    h.chatParticipantOnline(uuidToString(creator.ID)),
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load chat session creator")
		return
	}

	rows, err := h.Queries.ListChatSessionParticipants(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat participants")
		return
	}
	for _, row := range rows {
		id := uuidToString(row.UserID)
		// Defensive: a stored row for the creator would otherwise double them.
		if id == uuidToString(session.CreatorID) {
			continue
		}
		out = append(out, ChatParticipantResponse{
			UserID:    id,
			Name:      row.Name,
			AvatarURL: textToPtr(row.AvatarUrl),
			Role:      row.Role,
			JoinedAt:  timestampToString(row.JoinedAt),
			Online:    h.chatParticipantOnline(id),
		})
	}

	writeJSON(w, http.StatusOK, ChatParticipantListResponse{Participants: out})
}

// AddChatParticipant adds a workspace member to the session. Creator only:
// a participant can invite nobody, which keeps the roster's blast radius the
// creator's decision alone. Idempotent — re-adding is a 200, not a conflict.
func (h *Handler) AddChatParticipant(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	if !requireChatSessionCreator(w, session, userID) {
		return
	}

	var req AddChatParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, req.UserID, "user_id")
	if !ok {
		return
	}
	// A chat session grants access to the workspace's agent and its whole
	// transcript, so only someone who already belongs to the workspace can be
	// added. Membership is checked here because there is no database FK.
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      targetID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user is not a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to verify workspace membership")
		return
	}
	// The creator is already an implicit owner; storing a row would duplicate
	// them in every roster.
	if targetID == session.CreatorID {
		writeJSON(w, http.StatusOK, map[string]string{"user_id": req.UserID})
		return
	}

	if _, err := h.Queries.AddChatSessionParticipant(r.Context(), db.AddChatSessionParticipantParams{
		ChatSessionID: session.ID,
		UserID:        targetID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add chat participant")
		return
	}

	sessionIDStr := uuidToString(session.ID)
	h.publishChat(protocol.EventChatParticipantAdded, workspaceID, "member", userID, sessionIDStr,
		protocol.ChatParticipantPayload{ChatSessionID: sessionIDStr, UserID: req.UserID})

	writeJSON(w, http.StatusOK, map[string]string{"user_id": req.UserID})
}

// RemoveChatParticipant removes a member from the session. The creator may
// remove anyone; a participant may remove only themselves ("leave"). The
// creator cannot be removed — deleting or archiving the session is the way to
// end it, and both are already creator-only.
func (h *Handler) RemoveChatParticipant(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	targetIDStr := chi.URLParam(r, "userId")
	targetID, ok := parseUUIDOrBadRequest(w, targetIDStr, "user id")
	if !ok {
		return
	}
	isSelf := targetIDStr == userID
	if !isSelf && uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "only the chat session creator can remove other participants")
		return
	}
	if targetID == session.CreatorID {
		writeError(w, http.StatusBadRequest, "the chat session creator cannot leave; delete or archive it instead")
		return
	}

	if _, err := h.Queries.RemoveChatSessionParticipant(r.Context(), db.RemoveChatSessionParticipantParams{
		ChatSessionID: session.ID,
		UserID:        targetID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove chat participant")
		return
	}

	sessionIDStr := uuidToString(session.ID)
	h.publishChat(protocol.EventChatParticipantRemoved, workspaceID, "member", userID, sessionIDStr,
		protocol.ChatParticipantPayload{ChatSessionID: sessionIDStr, UserID: targetIDStr})

	w.WriteHeader(http.StatusNoContent)
}

// SendChatTyping broadcasts an ephemeral typing ping. Nothing is written: the
// event is the whole feature, and receivers expire the indicator on their own
// timer rather than waiting for a "stopped typing" that is never sent.
func (h *Handler) SendChatTyping(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}

	sessionIDStr := uuidToString(session.ID)
	h.publishChat(protocol.EventChatTyping, workspaceID, "member", userID, sessionIDStr,
		protocol.ChatTypingPayload{
			ChatSessionID: sessionIDStr,
			UserID:        userID,
			At:            time.Now().UTC().Format(time.RFC3339),
		})

	w.WriteHeader(http.StatusNoContent)
}
