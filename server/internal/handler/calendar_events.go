package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/icalendar"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Native calendar (OS plan, chantier 19). Events with participants who are
// members or agents; a member schedules, an agent proposes and a person
// accepts through a Decision Card; a unified agenda joins events with issue
// due dates, cycles and meetings; every member can publish their own ICS
// feed; a slot finder respects everyone's existing events and the members'
// working hours; reminders land in the inbox a quarter hour before.

const (
	CalendarStatusProposed  = "proposed"
	CalendarStatusScheduled = "scheduled"
	CalendarStatusCancelled = "cancelled"

	calendarOptionPrefix   = "calendar:"
	calendarAcceptOption   = "calendar:accept"
	calendarDeclineOption  = "calendar:decline"
	calendarFeedTokenPfx   = "mcal_"
	calendarReminderLead   = 15 * time.Minute
	calendarMaxParticipant = 50
	calendarMaxWindow      = 366 * 24 * time.Hour
	calendarSlotMax        = 10

	InboxTypeCalendarInvitation = "calendar_invitation"
	InboxTypeCalendarReminder   = "calendar_reminder"

	AuditCalendarEventCreated = "calendar_event.created"
	AuditCalendarEventUpdated = "calendar_event.updated"
	AuditCalendarEventStatus  = "calendar_event.status_changed"
)

// Members' default working hours for the slot finder, in each member's
// timezone (or the workspace's asked timezone); agents are available always.
const (
	calendarWorkStartHour = 9
	calendarWorkEndHour   = 18
)

// CalendarParticipant is one attendee, member or agent.
type CalendarParticipant struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Response string `json:"response"`
	Required bool   `json:"required"`
}

// CalendarActor names who created or proposed.
type CalendarActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// CalendarEntry is the wire shape of an event.
type CalendarEntry struct {
	ID              string                `json:"id"`
	Title           string                `json:"title"`
	Description     string                `json:"description"`
	StartsAt        string                `json:"starts_at"`
	EndsAt          string                `json:"ends_at"`
	AllDay          bool                  `json:"all_day"`
	Timezone        string                `json:"timezone"`
	Location        string                `json:"location"`
	IssueID         *string               `json:"issue_id"`
	IssueIdentifier string                `json:"issue_identifier,omitempty"`
	ProjectID       *string               `json:"project_id"`
	Status          string                `json:"status"`
	CreatedBy       CalendarActor         `json:"created_by"`
	Source          string                `json:"source"`
	ExternalID      string                `json:"external_id,omitempty"`
	DecisionID      *string               `json:"decision_id"`
	Participants    []CalendarParticipant `json:"participants"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
}

type calendarParticipantInput struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Required *bool  `json:"required"`
}

// CalendarEventInput is the create/update body.
type CalendarEventInput struct {
	Title        string                     `json:"title"`
	Description  string                     `json:"description"`
	StartsAt     string                     `json:"starts_at"`
	EndsAt       string                     `json:"ends_at"`
	AllDay       bool                       `json:"all_day"`
	Timezone     string                     `json:"timezone"`
	Location     string                     `json:"location"`
	IssueID      string                     `json:"issue_id"`
	ProjectID    string                     `json:"project_id"`
	Participants []calendarParticipantInput `json:"participants"`
}

type calendarNames struct {
	h      *Handler
	ctx    context.Context
	wsID   pgtype.UUID
	agents map[string]string
	users  map[string]string
	prefix string
}

func (h *Handler) newCalendarNames(ctx context.Context, wsID pgtype.UUID) *calendarNames {
	n := &calendarNames{h: h, ctx: ctx, wsID: wsID, agents: map[string]string{}, users: map[string]string{}, prefix: h.getIssuePrefix(ctx, wsID)}
	if agents, err := h.Queries.ListAllAgentsAnyKind(ctx, wsID); err == nil {
		for _, a := range agents {
			n.agents[uuidToString(a.ID)] = a.Name
		}
	}
	if members, err := h.Queries.ListMembersWithUser(ctx, wsID); err == nil {
		for _, m := range members {
			n.users[uuidToString(m.UserID)] = m.UserName
		}
	}
	return n
}

func (n *calendarNames) of(kind string, id pgtype.UUID) string {
	key := uuidToString(id)
	if kind == "agent" {
		return n.agents[key]
	}
	return n.users[key]
}

func (h *Handler) calendarEventToResponse(n *calendarNames, e db.CalendarEvent, parts []db.CalendarEventParticipant) CalendarEntry {
	out := CalendarEntry{
		ID: uuidToString(e.ID), Title: e.Title, Description: e.Description, StartsAt: timestampToString(e.StartsAt), EndsAt: timestampToString(e.EndsAt),
		AllDay: e.AllDay, Timezone: e.Timezone, Location: e.Location, IssueID: uuidToPtr(e.IssueID), ProjectID: uuidToPtr(e.ProjectID), Status: e.Status,
		CreatedBy: CalendarActor{Type: e.CreatedByType, ID: uuidToString(e.CreatedByID), Name: n.of(e.CreatedByType, e.CreatedByID)},
		Source:    e.Source, ExternalID: e.ExternalID, DecisionID: uuidToPtr(e.DecisionID), Participants: []CalendarParticipant{},
		CreatedAt: timestampToString(e.CreatedAt), UpdatedAt: timestampToString(e.UpdatedAt),
	}
	if e.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(n.ctx, e.IssueID); err == nil && issue.WorkspaceID == n.wsID {
			out.IssueIdentifier = n.prefix + "-" + strconv.Itoa(int(issue.Number))
		}
	}
	for _, p := range parts {
		if p.EventID != e.ID {
			continue
		}
		out.Participants = append(out.Participants, CalendarParticipant{Type: p.ParticipantType, ID: uuidToString(p.ParticipantID), Name: n.of(p.ParticipantType, p.ParticipantID), Response: p.Response, Required: p.Required})
	}
	return out
}

func (h *Handler) calendarEventsToResponses(ctx context.Context, wsID pgtype.UUID, rows []db.CalendarEvent) []CalendarEntry {
	n := h.newCalendarNames(ctx, wsID)
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, e := range rows {
		ids = append(ids, e.ID)
	}
	var parts []db.CalendarEventParticipant
	if len(ids) > 0 {
		parts, _ = h.Queries.ListCalendarEventParticipants(ctx, ids)
	}
	out := make([]CalendarEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, h.calendarEventToResponse(n, e, parts))
	}
	return out
}

// parseCalendarWindow reads from/to (RFC 3339), defaulting to the coming
// month, and caps the span at a year.
func parseCalendarWindow(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	q := r.URL.Query()
	from := time.Now().UTC().Truncate(24 * time.Hour)
	to := from.Add(31 * 24 * time.Hour)
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC 3339")
			return from, to, false
		}
		from = t
		to = from.Add(31 * 24 * time.Hour)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC 3339")
			return from, to, false
		}
		to = t
	}
	if !to.After(from) {
		writeError(w, http.StatusBadRequest, "to must be after from")
		return from, to, false
	}
	if to.Sub(from) > calendarMaxWindow {
		writeError(w, http.StatusBadRequest, "the window may span at most a year")
		return from, to, false
	}
	return from, to, true
}

func tsz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// ListCalendarEvents: GET /api/calendar/events?from&to&participant_type&participant_id&include_cancelled
func (h *Handler) ListCalendarEvents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	from, to, ok := parseCalendarWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var rows []db.CalendarEvent
	var err error
	if pt, pid := strings.TrimSpace(q.Get("participant_type")), strings.TrimSpace(q.Get("participant_id")); pt != "" || pid != "" {
		if pt != "member" && pt != "agent" {
			writeError(w, http.StatusBadRequest, "participant_type must be member or agent")
			return
		}
		pu, ok := parseUUIDOrBadRequest(w, pid, "participant_id")
		if !ok {
			return
		}
		rows, err = h.Queries.ListCalendarEventsForParticipantInWindow(r.Context(), db.ListCalendarEventsForParticipantInWindowParams{WorkspaceID: wsUUID, ParticipantType: pt, ParticipantID: pu, Since: tsz(from), Until: tsz(to)})
	} else {
		rows, err = h.Queries.ListCalendarEventsInWindow(r.Context(), db.ListCalendarEventsInWindowParams{WorkspaceID: wsUUID, Since: tsz(from), Until: tsz(to), IncludeCancelled: q.Get("include_cancelled") == "true"})
	}
	if err != nil {
		slog.Warn("calendar: list events failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": h.calendarEventsToResponses(r.Context(), wsUUID, rows), "from": from.UTC().Format(time.RFC3339), "to": to.UTC().Format(time.RFC3339)})
}

// GetCalendarEvent: GET /api/calendar/events/{id}
func (h *Handler) GetCalendarEvent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	e, err := h.Queries.GetCalendarEvent(r.Context(), db.GetCalendarEventParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event": h.calendarEventsToResponses(r.Context(), wsUUID, []db.CalendarEvent{e})[0]})
}

type calendarValidated struct {
	title, description, timezone, location string
	starts, ends                           time.Time
	allDay                                 bool
	issueID, projectID                     pgtype.UUID
	participants                           []calendarParticipantInput
}

func (h *Handler) validateCalendarInput(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, in CalendarEventInput) (calendarValidated, bool) {
	var v calendarValidated
	v.title = strings.TrimSpace(in.Title)
	if v.title == "" || len(v.title) > 300 {
		writeError(w, http.StatusBadRequest, "title is required (at most 300 characters)")
		return v, false
	}
	v.description = strings.TrimSpace(in.Description)
	if len(v.description) > 20000 {
		writeError(w, http.StatusBadRequest, "description is too long")
		return v, false
	}
	v.location = strings.TrimSpace(in.Location)
	v.timezone = strings.TrimSpace(in.Timezone)
	if v.timezone == "" {
		v.timezone = "UTC"
	}
	if _, err := time.LoadLocation(v.timezone); err != nil {
		writeError(w, http.StatusBadRequest, "timezone must be an IANA zone")
		return v, false
	}
	starts, err := time.Parse(time.RFC3339, strings.TrimSpace(in.StartsAt))
	if err != nil {
		writeError(w, http.StatusBadRequest, "starts_at must be RFC 3339")
		return v, false
	}
	ends, err := time.Parse(time.RFC3339, strings.TrimSpace(in.EndsAt))
	if err != nil {
		writeError(w, http.StatusBadRequest, "ends_at must be RFC 3339")
		return v, false
	}
	if !ends.After(starts) {
		writeError(w, http.StatusBadRequest, "ends_at must be after starts_at")
		return v, false
	}
	v.starts, v.ends, v.allDay = starts, ends, in.AllDay
	if raw := strings.TrimSpace(in.IssueID); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "issue_id")
		if !ok {
			return v, false
		}
		if issue, err := h.Queries.GetIssue(r.Context(), id); err != nil || issue.WorkspaceID != wsUUID {
			writeError(w, http.StatusBadRequest, "issue_id does not name an issue of this workspace")
			return v, false
		}
		v.issueID = id
	}
	if raw := strings.TrimSpace(in.ProjectID); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return v, false
		}
		v.projectID = id
	}
	if len(in.Participants) > calendarMaxParticipant {
		writeError(w, http.StatusBadRequest, "at most 50 participants")
		return v, false
	}
	for _, p := range in.Participants {
		if p.Type != "member" && p.Type != "agent" {
			writeError(w, http.StatusBadRequest, "participant type must be member or agent")
			return v, false
		}
		id, ok := parseUUIDOrBadRequest(w, p.ID, "participant id")
		if !ok {
			return v, false
		}
		if !h.isWorkspaceEntity(r.Context(), p.Type, uuidToString(id), uuidToString(wsUUID)) {
			writeError(w, http.StatusBadRequest, "participant "+p.ID+" is not in this workspace")
			return v, false
		}
		v.participants = append(v.participants, p)
	}
	return v, true
}

func (h *Handler) writeCalendarParticipants(ctx context.Context, wsUUID, eventID pgtype.UUID, parts []calendarParticipantInput, keepResponses map[string]string) {
	_ = h.Queries.DeleteCalendarEventParticipants(ctx, eventID)
	for _, p := range parts {
		id, _ := decisionUUID(p.ID)
		required := true
		if p.Required != nil {
			required = *p.Required
		}
		response := "pending"
		if prev, ok := keepResponses[p.Type+":"+uuidToString(id)]; ok {
			response = prev
		}
		if _, err := h.Queries.AddCalendarEventParticipant(ctx, db.AddCalendarEventParticipantParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, EventID: eventID, ParticipantType: p.Type, ParticipantID: id, Response: response, Required: required}); err != nil {
			slog.Warn("calendar: add participant failed", "event_id", uuidToString(eventID), "error", err)
		}
	}
}

// CreateCalendarEvent: POST /api/calendar/events. A member schedules; an
// agent (run token, MCP) proposes: the event is filed as proposed on its
// issue with a Decision Card the issue's people accept or decline.
func (h *Handler) CreateCalendarEvent(w http.ResponseWriter, r *http.Request) {
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
	var in CalendarEventInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	v, ok := h.validateCalendarInput(w, r, wsUUID, in)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	status := CalendarStatusScheduled
	if actorType == "agent" {
		if !v.issueID.Valid {
			writeError(w, http.StatusBadRequest, "an agent's proposal needs issue_id: the people of that issue decide")
			return
		}
		status = CalendarStatusProposed
	}
	ctx := r.Context()
	e, err := h.Queries.CreateCalendarEvent(ctx, db.CreateCalendarEventParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: v.title, Description: v.description, StartsAt: tsz(v.starts), EndsAt: tsz(v.ends), AllDay: v.allDay, Timezone: v.timezone, Location: v.location,
		IssueID: v.issueID, ProjectID: v.projectID, Status: status, CreatedByType: actorType, CreatedByID: parseUUID(actorID), Source: "vigil", ExternalID: "",
	})
	if err != nil {
		slog.Warn("calendar: create failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the event")
		return
	}
	h.writeCalendarParticipants(ctx, wsUUID, e.ID, v.participants, nil)
	if status == CalendarStatusProposed {
		if err := h.fileCalendarProposal(ctx, e, v, actorID); err != nil {
			slog.Warn("calendar: proposal card failed", append(logger.RequestAttrs(r), "error", err)...)
		} else if updated, err := h.Queries.GetCalendarEvent(ctx, db.GetCalendarEventParams{ID: e.ID, WorkspaceID: wsUUID}); err == nil {
			e = updated
		}
	} else {
		h.notifyCalendarInvitations(ctx, e, actorType, actorID)
		h.exportCalendarEvent(ctx, e, userID)
	}
	h.audit(ctx, wsUUID, actorType, actorID, AuditCalendarEventCreated, "calendar_event", e.ID, map[string]any{"title": e.Title, "status": e.Status, "starts_at": timestampToString(e.StartsAt)}, nil)
	h.publishCalendarChanged(wsUUID, e, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"event": h.calendarEventsToResponses(ctx, wsUUID, []db.CalendarEvent{e})[0]})
}

// fileCalendarProposal files the Decision Card an agent's proposal rides.
func (h *Handler) fileCalendarProposal(ctx context.Context, e db.CalendarEvent, v calendarValidated, agentID string) error {
	issue, err := h.Queries.GetIssue(ctx, e.IssueID)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation(e.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	when := e.StartsAt.Time.In(loc).Format("Mon 2 Jan 15:04") + " – " + e.EndsAt.Time.In(loc).Format("15:04") + " (" + e.Timezone + ")"
	options, _ := json.Marshal([]DecisionOption{
		{ID: calendarAcceptOption, Label: "Accept", Impact: "the event is scheduled and the participants invited"},
		{ID: calendarDeclineOption, Label: "Decline", Impact: "the proposal is cancelled"},
	})
	decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID: e.WorkspaceID, IssueID: e.IssueID, AskedByType: "agent", AskedByID: parseUUID(agentID),
		Question: "Proposed event · " + e.Title + " · " + when, Options: options, Urgency: "normal", SlaDeadlineAt: h.decisionDeadline(ctx, e.WorkspaceID),
	})
	if err != nil {
		return err
	}
	if err := h.Queries.SetCalendarEventDecision(ctx, db.SetCalendarEventDecisionParams{ID: e.ID, DecisionID: decision.ID}); err != nil {
		return err
	}
	h.notifyDecisionRequested(ctx, issue, decision, "agent", agentID)
	return nil
}

// applyCalendarForDecision settles a proposal when its card is answered.
// Wired into answerDecisionCore; returns true when the card was a proposal.
func (h *Handler) applyCalendarForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	if !strings.HasPrefix(optionID, calendarOptionPrefix) {
		return false
	}
	e, err := h.Queries.GetCalendarEventByDecision(ctx, decision.ID)
	if err != nil {
		return false
	}
	status := CalendarStatusCancelled
	if optionID == calendarAcceptOption {
		status = CalendarStatusScheduled
	}
	updated, err := h.Queries.SetCalendarEventStatus(ctx, db.SetCalendarEventStatusParams{ID: e.ID, WorkspaceID: e.WorkspaceID, Status: status})
	if err != nil {
		return true
	}
	if status == CalendarStatusScheduled {
		h.notifyCalendarInvitations(ctx, updated, actorType, actorID)
		if actorType == "member" {
			h.exportCalendarEvent(ctx, updated, actorID)
		}
	}
	h.audit(ctx, e.WorkspaceID, actorType, actorID, AuditCalendarEventStatus, "calendar_event", e.ID, map[string]any{"status": status, "via": "proposal"}, nil)
	h.publishCalendarChanged(e.WorkspaceID, updated, actorType, actorID)
	return true
}

// UpdateCalendarEvent: PUT /api/calendar/events/{id} (members).
func (h *Handler) UpdateCalendarEvent(w http.ResponseWriter, r *http.Request) {
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
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	ctx := r.Context()
	prev, err := h.Queries.GetCalendarEvent(ctx, db.GetCalendarEventParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	var in CalendarEventInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	v, ok := h.validateCalendarInput(w, r, wsUUID, in)
	if !ok {
		return
	}
	e, err := h.Queries.UpdateCalendarEvent(ctx, db.UpdateCalendarEventParams{ID: id, WorkspaceID: wsUUID, Title: v.title, Description: v.description, StartsAt: tsz(v.starts), EndsAt: tsz(v.ends), AllDay: v.allDay, Timezone: v.timezone, Location: v.location, IssueID: v.issueID, ProjectID: v.projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the event")
		return
	}
	if in.Participants != nil {
		keep := map[string]string{}
		if parts, err := h.Queries.ListCalendarEventParticipants(ctx, []pgtype.UUID{id}); err == nil {
			for _, p := range parts {
				keep[p.ParticipantType+":"+uuidToString(p.ParticipantID)] = p.Response
			}
		}
		h.writeCalendarParticipants(ctx, wsUUID, id, v.participants, keep)
	}
	moved := !prev.StartsAt.Time.Equal(e.StartsAt.Time) || !prev.EndsAt.Time.Equal(e.EndsAt.Time)
	if moved && e.Status == CalendarStatusScheduled {
		h.notifyCalendarInvitations(ctx, e, "member", userID)
	}
	h.audit(ctx, wsUUID, "member", userID, AuditCalendarEventUpdated, "calendar_event", e.ID, map[string]any{"title": e.Title, "moved": moved}, nil)
	h.publishCalendarChanged(wsUUID, e, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"event": h.calendarEventsToResponses(ctx, wsUUID, []db.CalendarEvent{e})[0]})
}

// CancelCalendarEvent: DELETE /api/calendar/events/{id} — cancels rather than
// erases, so a subscribed feed learns the event is off.
func (h *Handler) CancelCalendarEvent(w http.ResponseWriter, r *http.Request) {
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
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	e, err := h.Queries.SetCalendarEventStatus(r.Context(), db.SetCalendarEventStatusParams{ID: id, WorkspaceID: wsUUID, Status: CalendarStatusCancelled})
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, AuditCalendarEventStatus, "calendar_event", e.ID, map[string]any{"status": CalendarStatusCancelled}, nil)
	h.publishCalendarChanged(wsUUID, e, "member", userID)
	w.WriteHeader(http.StatusNoContent)
}

// RespondCalendarEvent: POST /api/calendar/events/{id}/respond {response}
// — the caller's own attendance (members; an agent answers through its run).
func (h *Handler) RespondCalendarEvent(w http.ResponseWriter, r *http.Request) {
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
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Response {
	case "accepted", "declined", "tentative":
	default:
		writeError(w, http.StatusBadRequest, "response must be accepted, declined or tentative")
		return
	}
	e, err := h.Queries.GetCalendarEvent(r.Context(), db.GetCalendarEventParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if _, err := h.Queries.SetCalendarEventParticipantResponse(r.Context(), db.SetCalendarEventParticipantResponseParams{EventID: id, ParticipantType: actorType, ParticipantID: parseUUID(actorID), Response: req.Response}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "you are not a participant of this event")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to record the response")
		return
	}
	h.publishCalendarChanged(wsUUID, e, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"event": h.calendarEventsToResponses(r.Context(), wsUUID, []db.CalendarEvent{e})[0]})
}

func (h *Handler) publishCalendarChanged(wsUUID pgtype.UUID, e db.CalendarEvent, actorType, actorID string) {
	if h.Bus == nil {
		return
	}
	h.publish(protocol.EventCalendarChanged, uuidToString(wsUUID), actorType, actorID, map[string]any{"event_id": uuidToString(e.ID), "issue_id": uuidToString(e.IssueID), "status": e.Status, "starts_at": timestampToString(e.StartsAt)})
}

// notifyCalendarInvitations files an inbox item and a push for every member
// participant but the actor.
func (h *Handler) notifyCalendarInvitations(ctx context.Context, e db.CalendarEvent, actorType, actorID string) {
	parts, err := h.Queries.ListCalendarEventParticipants(ctx, []pgtype.UUID{e.ID})
	if err != nil {
		return
	}
	loc, _ := time.LoadLocation(e.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	body := e.StartsAt.Time.In(loc).Format("Mon 2 Jan 2006 15:04") + " – " + e.EndsAt.Time.In(loc).Format("15:04") + " (" + e.Timezone + ")"
	if e.Location != "" {
		body += " · " + e.Location
	}
	details, _ := json.Marshal(map[string]any{"event_id": uuidToString(e.ID), "starts_at": timestampToString(e.StartsAt)})
	var pushTo []pgtype.UUID
	for _, p := range parts {
		if p.ParticipantType != "member" || (actorType == "member" && uuidToString(p.ParticipantID) == actorID) {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: e.WorkspaceID, RecipientType: "member", RecipientID: p.ParticipantID, Type: InboxTypeCalendarInvitation, Severity: "attention",
			IssueID: e.IssueID, Title: e.Title, Body: pgtype.Text{String: body, Valid: true}, ActorType: pgtype.Text{String: actorType, Valid: true}, ActorID: parseUUID(actorID), Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(e.WorkspaceID), actorType, actorID, map[string]any{"item": inboxToResponse(item)})
		pushTo = append(pushTo, p.ParticipantID)
	}
	if len(pushTo) > 0 {
		h.pushToUsers(ctx, e.WorkspaceID, pushTo, "Invitation: "+e.Title, body, map[string]any{"kind": InboxTypeCalendarInvitation, "event_id": uuidToString(e.ID)})
	}
}

// RemindCalendarEvents files a reminder for every scheduled event starting
// within the lead time. Runs from the scheduler once a minute.
func (h *Handler) RemindCalendarEvents(ctx context.Context) int {
	now := time.Now()
	rows, err := h.Queries.ListCalendarEventsToRemind(ctx, db.ListCalendarEventsToRemindParams{Now: tsz(now), Until: tsz(now.Add(calendarReminderLead))})
	if err != nil {
		slog.Warn("calendar reminders: list failed", "error", err)
		return 0
	}
	n := 0
	for _, e := range rows {
		if _, err := h.Queries.MarkCalendarEventReminded(ctx, e.ID); err != nil {
			continue
		}
		parts, err := h.Queries.ListCalendarEventParticipants(ctx, []pgtype.UUID{e.ID})
		if err != nil {
			continue
		}
		minutes := int(time.Until(e.StartsAt.Time).Round(time.Minute) / time.Minute)
		body := "Starts in " + strconv.Itoa(minutes) + " min"
		if e.Location != "" {
			body += " · " + e.Location
		}
		details, _ := json.Marshal(map[string]any{"event_id": uuidToString(e.ID), "starts_at": timestampToString(e.StartsAt)})
		var pushTo []pgtype.UUID
		for _, p := range parts {
			if p.ParticipantType != "member" || p.Response == "declined" {
				continue
			}
			item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
				ID: dbid.NewV7(), WorkspaceID: e.WorkspaceID, RecipientType: "member", RecipientID: p.ParticipantID, Type: InboxTypeCalendarReminder, Severity: "attention",
				IssueID: e.IssueID, Title: e.Title, Body: pgtype.Text{String: body, Valid: true}, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
			})
			if err != nil {
				continue
			}
			h.publish(protocol.EventInboxNew, uuidToString(e.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
			pushTo = append(pushTo, p.ParticipantID)
		}
		if len(pushTo) > 0 {
			h.pushToUsers(ctx, e.WorkspaceID, pushTo, e.Title, body, map[string]any{"kind": InboxTypeCalendarReminder, "event_id": uuidToString(e.ID)})
		}
		n++
	}
	return n
}

// ---- Agenda -------------------------------------------------------------------

// CalendarAgendaResponse joins what has a date in the workspace.
type CalendarAgendaResponse struct {
	From      string          `json:"from"`
	To        string          `json:"to"`
	Events    []CalendarEntry `json:"events"`
	IssuesDue []AgendaIssue   `json:"issues_due"`
	Cycles    []AgendaCycle   `json:"cycles"`
	Meetings  []AgendaMeeting `json:"meetings"`
}

type AgendaIssue struct {
	ID           string  `json:"id"`
	Identifier   string  `json:"identifier"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	DueDate      string  `json:"due_date"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
}

type AgendaCycle struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type AgendaMeeting struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
}

// GetCalendarAgenda: GET /api/calendar/agenda?from&to
func (h *Handler) GetCalendarAgenda(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	from, to, ok := parseCalendarWindow(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	out := CalendarAgendaResponse{From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339), Events: []CalendarEntry{}, IssuesDue: []AgendaIssue{}, Cycles: []AgendaCycle{}, Meetings: []AgendaMeeting{}}
	if rows, err := h.Queries.ListCalendarEventsInWindow(ctx, db.ListCalendarEventsInWindowParams{WorkspaceID: wsUUID, Since: tsz(from), Until: tsz(to)}); err == nil {
		out.Events = h.calendarEventsToResponses(ctx, wsUUID, rows)
	}
	prefix := h.getIssuePrefix(ctx, wsUUID)
	day := func(t time.Time) pgtype.Date { return pgtype.Date{Time: t.UTC().Truncate(24 * time.Hour), Valid: true} }
	if rows, err := h.Queries.ListIssuesDueBetween(ctx, db.ListIssuesDueBetweenParams{WorkspaceID: wsUUID, Since: day(from), Until: day(to.Add(24*time.Hour - time.Second))}); err == nil {
		for _, i := range rows {
			out.IssuesDue = append(out.IssuesDue, AgendaIssue{ID: uuidToString(i.ID), Identifier: prefix + "-" + strconv.Itoa(int(i.Number)), Title: i.Title, Status: i.Status, DueDate: i.DueDate.Time.Format("2006-01-02"), AssigneeType: textToPtr(i.AssigneeType), AssigneeID: uuidToPtr(i.AssigneeID)})
		}
	}
	if rows, err := h.Queries.ListCyclesOverlapping(ctx, db.ListCyclesOverlappingParams{WorkspaceID: wsUUID, Since: day(from), Until: day(to)}); err == nil {
		for _, c := range rows {
			out.Cycles = append(out.Cycles, AgendaCycle{ID: uuidToString(c.ID), Name: c.Name, StartDate: c.StartDate.Time.Format("2006-01-02"), EndDate: c.EndDate.Time.Format("2006-01-02")})
		}
	}
	if rows, err := h.Queries.ListMeetingsBetween(ctx, db.ListMeetingsBetweenParams{WorkspaceID: wsUUID, Since: tsz(from), Until: tsz(to)}); err == nil {
		for _, m := range rows {
			out.Meetings = append(out.Meetings, AgendaMeeting{ID: uuidToString(m.ID), Title: m.Title, Status: m.Status, StartedAt: timestampToString(m.StartedAt), EndedAt: timestampToPtr(m.EndedAt)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Slots ---------------------------------------------------------------------

// CalendarSlot is one free window every participant can make.
type CalendarSlot struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

type busyInterval struct{ start, end time.Time }

// FindCalendarSlots: GET /api/calendar/slots?participants=member:<id>,agent:<id>&duration=30&from&to&tz
// Members are free within working hours (09:00–18:00 in tz) outside their
// events; agents are free outside their events at any hour. The first ten
// windows of the asked length, earliest first.
func (h *Handler) FindCalendarSlots(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	q := r.URL.Query()
	from, to, ok := parseCalendarWindow(w, r)
	if !ok {
		return
	}
	if from.Before(time.Now()) {
		from = time.Now().Add(15 * time.Minute).Truncate(15 * time.Minute)
	}
	duration := 30 * time.Minute
	if raw := strings.TrimSpace(q.Get("duration")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 5 || n > 24*60 {
			writeError(w, http.StatusBadRequest, "duration must be 5..1440 minutes")
			return
		}
		duration = time.Duration(n) * time.Minute
	}
	tzName := strings.TrimSpace(q.Get("tz"))
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "tz must be an IANA zone")
		return
	}
	type who struct {
		kind string
		id   pgtype.UUID
	}
	var people []who
	hasMember := false
	for _, raw := range strings.Split(q.Get("participants"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		kind, id, ok := strings.Cut(raw, ":")
		if !ok || (kind != "member" && kind != "agent") {
			writeError(w, http.StatusBadRequest, "participants must be member:<id> or agent:<id>")
			return
		}
		u, ok := parseUUIDOrBadRequest(w, id, "participants")
		if !ok {
			return
		}
		people = append(people, who{kind, u})
		if kind == "member" {
			hasMember = true
		}
	}
	if len(people) == 0 {
		writeError(w, http.StatusBadRequest, "participants is required")
		return
	}
	ctx := r.Context()
	var busy []busyInterval
	for _, p := range people {
		rows, err := h.Queries.ListCalendarEventsForParticipantInWindow(ctx, db.ListCalendarEventsForParticipantInWindowParams{WorkspaceID: wsUUID, ParticipantType: p.kind, ParticipantID: p.id, Since: tsz(from), Until: tsz(to)})
		if err != nil {
			continue
		}
		for _, e := range rows {
			busy = append(busy, busyInterval{e.StartsAt.Time, e.EndsAt.Time})
		}
	}
	slots := findFreeSlots(from, to, duration, loc, hasMember, busy, calendarSlotMax)
	out := make([]CalendarSlot, 0, len(slots))
	for _, s := range slots {
		out = append(out, CalendarSlot{StartsAt: s.start.UTC().Format(time.RFC3339), EndsAt: s.end.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": out, "duration_minutes": int(duration / time.Minute), "tz": tzName})
}

// findFreeSlots walks the window on a 15-minute grid and returns the first
// windows of the asked length that overlap no busy interval and, when a
// member is involved, sit inside working hours on a weekday in loc.
func findFreeSlots(from, to time.Time, duration time.Duration, loc *time.Location, workingHours bool, busy []busyInterval, max int) []busyInterval {
	sort.Slice(busy, func(i, j int) bool { return busy[i].start.Before(busy[j].start) })
	var out []busyInterval
	step := 15 * time.Minute
	for t := from.Truncate(step); !t.Add(duration).After(to) && len(out) < max; t = t.Add(step) {
		end := t.Add(duration)
		if workingHours {
			lt := t.In(loc)
			le := end.In(loc)
			if lt.Weekday() == time.Saturday || lt.Weekday() == time.Sunday {
				continue
			}
			if lt.Hour() < calendarWorkStartHour || le.Hour() > calendarWorkEndHour || (le.Hour() == calendarWorkEndHour && le.Minute() > 0) || le.Day() != lt.Day() {
				continue
			}
		}
		clash := false
		for _, b := range busy {
			if b.start.Before(end) && b.end.After(t) {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		out = append(out, busyInterval{t, end})
		t = end.Add(-step) // the next candidate starts after this slot
	}
	return out
}

// ---- Outbound ICS feed ----------------------------------------------------------

func calendarFeedHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// MintCalendarFeedToken: POST /api/calendar/feed-token — rotates the caller's
// feed URL; the clear token is in this response only.
func (h *Handler) MintCalendarFeedToken(w http.ResponseWriter, r *http.Request) {
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
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint a token")
		return
	}
	token := calendarFeedTokenPfx + hex.EncodeToString(buf)
	if _, err := h.Queries.UpsertCalendarFeedToken(r.Context(), db.UpsertCalendarFeedTokenParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, UserID: parseUUID(userID), TokenHash: calendarFeedHash(token)}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the token")
		return
	}
	path := "/api/calendar/ics/" + token
	url := path
	if base := strings.TrimRight(h.cfg.PublicURL, "/"); base != "" {
		url = base + path
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": url, "path": path})
}

// GetCalendarFeedToken: GET /api/calendar/feed-token — whether one exists.
func (h *Handler) GetCalendarFeedToken(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.Queries.GetCalendarFeedTokenForUser(r.Context(), db.GetCalendarFeedTokenForUserParams{WorkspaceID: wsUUID, UserID: parseUUID(userID)})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "created_at": timestampToString(row.CreatedAt)})
}

// RevokeCalendarFeedToken: DELETE /api/calendar/feed-token
func (h *Handler) RevokeCalendarFeedToken(w http.ResponseWriter, r *http.Request) {
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
	_ = h.Queries.DeleteCalendarFeedToken(r.Context(), db.DeleteCalendarFeedTokenParams{WorkspaceID: wsUUID, UserID: parseUUID(userID)})
	w.WriteHeader(http.StatusNoContent)
}

// ServeCalendarFeed: GET /api/calendar/ics/{token} — public on purpose: the
// token in the path is the credential. The feed carries the member's own
// events (participant or creator) for the past month and the year ahead,
// cancelled ones marked so clients drop them.
func (h *Handler) ServeCalendarFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(chi.URLParam(r, "token"), ".ics")
	if token == "" || !strings.HasPrefix(token, calendarFeedTokenPfx) {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}
	row, err := h.Queries.GetCalendarFeedTokenByHash(r.Context(), calendarFeedHash(token))
	if err != nil {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}
	now := time.Now()
	rows, err := h.Queries.ListCalendarEventsForParticipantInWindow(r.Context(), db.ListCalendarEventsForParticipantInWindowParams{WorkspaceID: row.WorkspaceID, ParticipantType: "member", ParticipantID: row.UserID, Since: tsz(now.Add(-31 * 24 * time.Hour)), Until: tsz(now.Add(366 * 24 * time.Hour))})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build the feed")
		return
	}
	all, _ := h.Queries.ListCalendarEventsInWindow(r.Context(), db.ListCalendarEventsInWindowParams{WorkspaceID: row.WorkspaceID, Since: tsz(now.Add(-31 * 24 * time.Hour)), Until: tsz(now.Add(366 * 24 * time.Hour)), IncludeCancelled: true})
	seen := map[string]bool{}
	var events []icalendar.OutboundEvent
	add := func(e db.CalendarEvent) {
		id := uuidToString(e.ID)
		if seen[id] {
			return
		}
		seen[id] = true
		status := "CONFIRMED"
		switch e.Status {
		case CalendarStatusProposed:
			status = "TENTATIVE"
		case CalendarStatusCancelled:
			status = "CANCELLED"
		}
		events = append(events, icalendar.OutboundEvent{UID: id + "@vigil", Summary: e.Title, Description: e.Description, Location: e.Location, Start: e.StartsAt.Time, End: e.EndsAt.Time, AllDay: e.AllDay, Status: status, Stamp: e.UpdatedAt.Time, URL: h.calendarEventURL(r.Context(), row.WorkspaceID)})
	}
	for _, e := range rows {
		add(e)
	}
	for _, e := range all {
		if e.CreatedByType == "member" && e.CreatedByID == row.UserID {
			add(e)
		}
	}
	name := "Vigil"
	if ws, err := h.Queries.GetWorkspace(r.Context(), row.WorkspaceID); err == nil {
		name = "Vigil · " + ws.Name
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\""+icalendar.FeedFilename(name)+"\"")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(icalendar.Write(name, events)))
}

func (h *Handler) calendarEventURL(ctx context.Context, wsUUID pgtype.UUID) string {
	base := strings.TrimRight(h.cfg.AppURL, "/")
	if base == "" {
		return ""
	}
	if ws, err := h.Queries.GetWorkspace(ctx, wsUUID); err == nil {
		return base + "/" + ws.Slug + "/calendar"
	}
	return ""
}
