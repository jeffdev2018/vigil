package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	composiointeg "github.com/multica-ai/multica/server/internal/integrations/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Google Calendar through Composio (OS plan, chantier 19). A member who
// connected the googlecalendar toolkit in Settings → Integrations → Composio
// gets two things without an agent in the loop: an import of their Google
// events into the workspace calendar for a window, and an export of the
// events they schedule here. Both go through the deterministic tool path
// (ExecuteTool), never through an agent's MCP session.
//
// The tool slugs and argument names are Composio's; they are constants so
// a rename is one edit. Responses are read defensively: Composio nests the
// provider payload under data, and the exact wrapper has changed before.
const (
	googleCalendarToolkit = "googlecalendar"
	googleFindEventsTool  = "GOOGLECALENDAR_FIND_EVENT"
	googleCreateEventTool = "GOOGLECALENDAR_CREATE_EVENT"
	googleImportMaxEvents = 250
)

// CalendarExternalSync is what the calendar needs from an external
// calendar provider. The Composio-backed implementation is the only one;
// the interface exists so tests can fake it.
type CalendarExternalSync interface {
	HasConnection(ctx context.Context, userID pgtype.UUID) bool
	ListEvents(ctx context.Context, userID pgtype.UUID, from, to time.Time) ([]ExternalCalendarEvent, error)
	CreateEvent(ctx context.Context, userID pgtype.UUID, e db.CalendarEvent) (externalID string, err error)
}

// ExternalCalendarEvent is one event read from the provider.
type ExternalCalendarEvent struct {
	ID          string
	Title       string
	Description string
	Location    string
	Start, End  time.Time
	AllDay      bool
}

// composioCalendarSync adapts the Composio service.
type composioCalendarSync struct{ svc *composiointeg.Service }

// NewComposioCalendarSync wraps the Composio service; nil when Composio is off.
func NewComposioCalendarSync(svc *composiointeg.Service) CalendarExternalSync {
	if svc == nil {
		return nil
	}
	return composioCalendarSync{svc: svc}
}

func (c composioCalendarSync) HasConnection(ctx context.Context, userID pgtype.UUID) bool {
	return c.svc.HasConnection(ctx, userID, googleCalendarToolkit)
}

func (c composioCalendarSync) ListEvents(ctx context.Context, userID pgtype.UUID, from, to time.Time) ([]ExternalCalendarEvent, error) {
	data, err := c.svc.ExecuteToolForUser(ctx, userID, googleCalendarToolkit, googleFindEventsTool, map[string]any{
		"calendar_id": "primary", "timeMin": from.UTC().Format(time.RFC3339), "timeMax": to.UTC().Format(time.RFC3339), "max_results": googleImportMaxEvents, "single_events": true,
	})
	if err != nil {
		return nil, err
	}
	return parseGoogleEvents(data), nil
}

func (c composioCalendarSync) CreateEvent(ctx context.Context, userID pgtype.UUID, e db.CalendarEvent) (string, error) {
	dur := e.EndsAt.Time.Sub(e.StartsAt.Time)
	args := map[string]any{
		"calendar_id": "primary", "summary": e.Title, "description": e.Description, "location": e.Location,
		"start_datetime": e.StartsAt.Time.UTC().Format(time.RFC3339), "timezone": e.Timezone,
		"event_duration_hour": int(dur / time.Hour), "event_duration_minutes": int((dur % time.Hour) / time.Minute),
	}
	data, err := c.svc.ExecuteToolForUser(ctx, userID, googleCalendarToolkit, googleCreateEventTool, args)
	if err != nil {
		return "", err
	}
	if id := findStringKey(data, "id", 3); id != "" {
		return id, nil
	}
	return "", errors.New("google calendar: the created event carried no id")
}

// parseGoogleEvents finds the event list wherever Composio nested it and
// maps the Google fields; anything unreadable is skipped, never fatal.
func parseGoogleEvents(data map[string]any) []ExternalCalendarEvent {
	list := findEventList(data, 4)
	out := make([]ExternalCalendarEvent, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		title, _ := m["summary"].(string)
		start, allDay, ok1 := googleTime(m["start"])
		end, _, ok2 := googleTime(m["end"])
		if id == "" || !ok1 || !ok2 || !end.After(start) {
			continue
		}
		desc, _ := m["description"].(string)
		loc, _ := m["location"].(string)
		if title == "" {
			title = "(untitled)"
		}
		out = append(out, ExternalCalendarEvent{ID: id, Title: title, Description: desc, Location: loc, Start: start, End: end, AllDay: allDay})
	}
	return out
}

func googleTime(v any) (time.Time, bool, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return time.Time{}, false, false
	}
	if s, ok := m["dateTime"].(string); ok && s != "" {
		t, err := time.Parse(time.RFC3339, s)
		return t, false, err == nil
	}
	if s, ok := m["date"].(string); ok && s != "" {
		t, err := time.Parse("2006-01-02", s)
		return t, true, err == nil
	}
	return time.Time{}, false, false
}

func findEventList(v any, depth int) []any {
	if depth < 0 {
		return nil
	}
	switch x := v.(type) {
	case []any:
		if len(x) > 0 {
			if m, ok := x[0].(map[string]any); ok {
				if _, has := m["start"]; has {
					return x
				}
			}
		}
	case map[string]any:
		for _, key := range []string{"items", "events", "event_data", "data", "response_data"} {
			if inner, ok := x[key]; ok {
				if list := findEventList(inner, depth-1); list != nil {
					return list
				}
			}
		}
		for _, inner := range x {
			if list := findEventList(inner, depth-1); list != nil {
				return list
			}
		}
	}
	return nil
}

func findStringKey(v any, key string, depth int) string {
	if depth < 0 {
		return ""
	}
	if m, ok := v.(map[string]any); ok {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
		for _, inner := range m {
			if s := findStringKey(inner, key, depth-1); s != "" {
				return s
			}
		}
	}
	return ""
}

// exportCalendarEvent mirrors a scheduled event to the member's Google
// calendar when they connected one. Best effort, off the request path.
func (h *Handler) exportCalendarEvent(ctx context.Context, e db.CalendarEvent, userID string) {
	if h.CalendarSync == nil || e.Source != "vigil" {
		return
	}
	uid := parseUUID(userID)
	if !h.CalendarSync.HasConnection(ctx, uid) {
		return
	}
	go func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("calendar export: panic", "event_id", uuidToString(e.ID), "panic", rec)
			}
		}()
		externalID, err := h.CalendarSync.CreateEvent(ctx, uid, e)
		if err != nil {
			slog.Warn("calendar export: google create failed", "event_id", uuidToString(e.ID), "error", err)
			return
		}
		if err := h.Queries.SetCalendarEventExternalID(ctx, db.SetCalendarEventExternalIDParams{ID: e.ID, Source: "vigil+google", ExternalID: externalID}); err != nil {
			slog.Warn("calendar export: external id not stored", "event_id", uuidToString(e.ID), "error", err)
		}
	}(ctx)
}

// ImportGoogleCalendar: POST /api/calendar/google/import {from, to} — pulls
// the member's Google events for the window into the workspace calendar
// (source google, keyed by external id, so a second import updates instead
// of duplicating). The member is the only participant of what they import.
func (h *Handler) ImportGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
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
	if h.CalendarSync == nil {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	from := time.Now().UTC().Truncate(24 * time.Hour)
	to := from.Add(31 * 24 * time.Hour)
	if s := strings.TrimSpace(req.From); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC 3339")
			return
		}
		from = t
	}
	if s := strings.TrimSpace(req.To); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC 3339")
			return
		}
		to = t
	}
	if !to.After(from) || to.Sub(from) > calendarMaxWindow {
		writeError(w, http.StatusBadRequest, "the window must be positive and at most a year")
		return
	}
	uid := parseUUID(userID)
	if !h.CalendarSync.HasConnection(r.Context(), uid) {
		writeErrorCode(w, http.StatusConflict, "no_google_connection", "connect Google Calendar under Settings → Integrations → Composio first")
		return
	}
	external, err := h.CalendarSync.ListEvents(r.Context(), uid, from, to)
	if err != nil {
		if errors.Is(err, composiointeg.ErrNoConnection) {
			writeErrorCode(w, http.StatusConflict, "no_google_connection", "connect Google Calendar first")
			return
		}
		writeError(w, http.StatusBadGateway, "google calendar: "+err.Error())
		return
	}
	existing := map[string]db.CalendarEvent{}
	if rows, err := h.Queries.ListCalendarEventsInWindow(r.Context(), db.ListCalendarEventsInWindowParams{WorkspaceID: wsUUID, Since: tsz(from.Add(-24 * time.Hour)), Until: tsz(to.Add(24 * time.Hour)), IncludeCancelled: true}); err == nil {
		for _, e := range rows {
			if e.Source == "google" && e.ExternalID != "" {
				existing[e.ExternalID] = e
			}
		}
	}
	created, updated := 0, 0
	for _, x := range external {
		if prev, ok := existing[x.ID]; ok {
			if _, err := h.Queries.UpdateCalendarEvent(r.Context(), db.UpdateCalendarEventParams{ID: prev.ID, WorkspaceID: wsUUID, Title: x.Title, Description: x.Description, StartsAt: tsz(x.Start), EndsAt: tsz(x.End), AllDay: x.AllDay, Timezone: prev.Timezone, Location: x.Location, IssueID: prev.IssueID, ProjectID: prev.ProjectID}); err == nil {
				updated++
			}
			continue
		}
		e, err := h.Queries.CreateCalendarEvent(r.Context(), db.CreateCalendarEventParams{
			ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: x.Title, Description: x.Description, StartsAt: tsz(x.Start), EndsAt: tsz(x.End), AllDay: x.AllDay, Timezone: "UTC", Location: x.Location,
			Status: CalendarStatusScheduled, CreatedByType: "member", CreatedByID: uid, Source: "google", ExternalID: x.ID,
		})
		if err != nil {
			continue
		}
		_, _ = h.Queries.AddCalendarEventParticipant(r.Context(), db.AddCalendarEventParticipantParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, EventID: e.ID, ParticipantType: "member", ParticipantID: uid, Response: "accepted", Required: true})
		created++
	}
	if created+updated > 0 {
		h.publish("calendar:changed", workspaceID, "member", userID, map[string]any{"imported": created + updated})
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "updated": updated, "seen": len(external)})
}
