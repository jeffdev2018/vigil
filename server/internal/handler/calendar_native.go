package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// calendarToolAdapter backs the native runtime's calendar tools with the
// same handlers the API exposes, so a run and a person read the same
// windows and file the same proposals. Calls are dispatched in-process
// through recorders, the way the MCP server dispatches its leaves.
type calendarToolAdapter struct {
	h *Handler
	// taskID, when set, is stamped as X-Task-ID so resolveActor trusts the
	// agent identity the way the auth middleware would for a task token.
	taskID string
}

func (a calendarToolAdapter) call(ctx context.Context, method, path string, query url.Values, body any, fn http.HandlerFunc, wsID pgtype.UUID, actorType, actorID string, params map[string]string) (any, error) {
	var reader *strings.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req := httptest.NewRequest(method, path, reader).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", uuidToString(wsID))
	if actorType == "agent" {
		// The handlers gate on workspace membership before resolving the
		// actor, so the replayed call authenticates as the workspace owner
		// (what a real run token carries in user_id) and acts as the agent.
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", actorID)
		if a.taskID != "" {
			req.Header.Set("X-Task-ID", a.taskID)
		}
		req.Header.Set("X-User-ID", a.h.workspaceOwnerUserID(ctx, wsID))
	} else {
		req.Header.Set("X-User-ID", actorID)
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	fn(rec, req)
	var out any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		out = rec.Body.String()
	}
	if rec.Code >= 400 {
		if m, ok := out.(map[string]any); ok {
			if msg, ok := m["error"].(string); ok {
				return nil, errors.New(msg)
			}
		}
		return nil, fmt.Errorf("calendar: HTTP %d", rec.Code)
	}
	return out, nil
}

// workspaceOwnerUserID is the member a replayed in-process call authenticates
// as. Handlers gate on workspace membership, and an agent is not a member, so
// a run's call borrows the workspace owner's seat while the actor headers keep
// the agent as the author of whatever gets written.
func (h *Handler) workspaceOwnerUserID(ctx context.Context, wsID pgtype.UUID) string {
	members, err := h.Queries.ListMembersWithUser(ctx, wsID)
	if err != nil {
		return ""
	}
	for _, m := range members {
		if m.Role == "owner" {
			return uuidToString(m.UserID)
		}
	}
	if len(members) > 0 {
		return uuidToString(members[0].UserID)
	}
	return ""
}

func (a calendarToolAdapter) ListEvents(ctx context.Context, wsID pgtype.UUID, from, to time.Time) (any, error) {
	q := url.Values{"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	return a.call(ctx, http.MethodGet, "/api/calendar/events", q, nil, a.h.ListCalendarEvents, wsID, "member", a.h.workspaceOwnerUserID(ctx, wsID), nil)
}

func (a calendarToolAdapter) Agenda(ctx context.Context, wsID pgtype.UUID, from, to time.Time) (any, error) {
	q := url.Values{"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	return a.call(ctx, http.MethodGet, "/api/calendar/agenda", q, nil, a.h.GetCalendarAgenda, wsID, "member", a.h.workspaceOwnerUserID(ctx, wsID), nil)
}

func (a calendarToolAdapter) FindSlots(ctx context.Context, wsID pgtype.UUID, participants []string, durationMinutes int, from, to time.Time, tz string) (any, error) {
	q := url.Values{"participants": {strings.Join(participants, ",")}, "duration": {strconv.Itoa(durationMinutes)}, "from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	if tz != "" {
		q.Set("tz", tz)
	}
	return a.call(ctx, http.MethodGet, "/api/calendar/slots", q, nil, a.h.FindCalendarSlots, wsID, "member", a.h.workspaceOwnerUserID(ctx, wsID), nil)
}

func (a calendarToolAdapter) Propose(ctx context.Context, wsID, agentID, issueID pgtype.UUID, input map[string]any) (any, error) {
	body := map[string]any{}
	for k, v := range input {
		body[k] = v
	}
	body["issue_id"] = uuidToString(issueID)
	return a.call(ctx, http.MethodPost, "/api/calendar/events", nil, body, a.h.CreateCalendarEvent, wsID, "agent", uuidToString(agentID), nil)
}
