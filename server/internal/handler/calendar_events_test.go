package handler

// Native calendar (OS plan, chantier 19): scheduling with invitations, the
// window and agenda reads, the slot finder, responses, the outbound feed, an
// agent's proposal settled through its Decision Card, reminders, and the
// Google import through a fake provider.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func calendarCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM calendar_event_participant WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM calendar_event WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM calendar_feed_token WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type IN ('calendar_invitation', 'calendar_reminder')`, testWorkspaceID)
	})
}

func createCalendarEvent(t *testing.T, body map[string]any, headers ...string) (CalendarEntry, *testutil.Response) {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/calendar/events", body)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	var out struct {
		Event CalendarEntry `json:"event"`
	}
	resp := testutil.Call(t, testHandler.CreateCalendarEvent, req)
	if resp.Code == http.StatusCreated {
		resp.JSON(&out)
	}
	return out.Event, resp
}

// calendarGuest is a second member of the test workspace, to be invited.
func calendarGuest(t *testing.T) string {
	t.Helper()
	guest := dbfx.Insert(t, "user", testutil.Cols{"email": "cal-" + uuid.NewString()[:8] + "@example.com", "name": "Cal Guest"})
	dbfx.InsertNoID(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": guest, "role": "member"}, "workspace_id = $1 AND user_id = $2", testWorkspaceID, guest)
	return guest
}

func TestCalendarSchedulesInvitesListsAndAnswers(t *testing.T) {
	calendarCleanup(t)
	otherUser := calendarGuest(t)
	agent := dbfx.Agent(t, "cal agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "cal issue "+uuid.NewString()[:8], testutil.Cols{"due_date": "2026-10-02"})
	start := time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)

	e, resp := createCalendarEvent(t, map[string]any{
		"title": "Design review", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339), "timezone": "Europe/Paris", "issue_id": issue,
		"participants": []map[string]any{{"type": "member", "id": otherUser}, {"type": "agent", "id": agent}},
	})
	resp.Want(http.StatusCreated)
	if e.Status != CalendarStatusScheduled || len(e.Participants) != 2 || e.IssueIdentifier == "" || e.CreatedBy.Type != "member" {
		t.Fatalf("event = %+v", e)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'event_id' = $3`, InboxTypeCalendarInvitation, otherUser, e.ID); n != 1 {
		t.Fatalf("invitations for the guest = %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2`, InboxTypeCalendarInvitation, testUserID); n != 0 {
		t.Fatalf("the organiser must not be invited to their own event")
	}
	// Validation.
	_, bad := createCalendarEvent(t, map[string]any{"title": "x", "starts_at": start.Format(time.RFC3339), "ends_at": start.Format(time.RFC3339)})
	bad.Want(http.StatusBadRequest)
	_, bad = createCalendarEvent(t, map[string]any{"title": "x", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339), "timezone": "Mars/Olympus"})
	bad.Want(http.StatusBadRequest)

	// Window read and agenda.
	var list struct {
		Events []CalendarEntry `json:"events"`
	}
	testutil.Call(t, testHandler.ListCalendarEvents, newRequest(http.MethodGet, "/api/calendar/events?from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Events) != 1 || list.Events[0].ID != e.ID {
		t.Fatalf("window = %+v", list.Events)
	}
	testutil.Call(t, testHandler.ListCalendarEvents, newRequest(http.MethodGet, "/api/calendar/events?from=2026-11-01T00:00:00Z&to=2026-11-03T00:00:00Z", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Events) != 0 {
		t.Fatalf("another window = %+v", list.Events)
	}
	testutil.Call(t, testHandler.ListCalendarEvents, newRequest(http.MethodGet, "/api/calendar/events?from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z&participant_type=agent&participant_id="+agent, nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Events) != 1 {
		t.Fatalf("agent's window = %+v", list.Events)
	}
	var agenda CalendarAgendaResponse
	testutil.Call(t, testHandler.GetCalendarAgenda, newRequest(http.MethodGet, "/api/calendar/agenda?from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z", nil)).Want(http.StatusOK).JSON(&agenda)
	if len(agenda.Events) != 1 || len(agenda.IssuesDue) < 1 || agenda.IssuesDue[0].DueDate != "2026-10-02" {
		t.Fatalf("agenda = %+v", agenda)
	}

	// The guest answers; the organiser, not a participant, is refused.
	guestReq := testutil.WithHeaders(newRequest(http.MethodPost, "/api/calendar/events/"+e.ID+"/respond", map[string]any{"response": "accepted"}), "X-User-ID", otherUser)
	testutil.Call(t, testHandler.RespondCalendarEvent, testutil.WithURLParams(guestReq, "id", e.ID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.RespondCalendarEvent, testutil.WithURLParams(newRequest(http.MethodPost, "/api/calendar/events/"+e.ID+"/respond", map[string]any{"response": "accepted"}), "id", e.ID)).Want(http.StatusNotFound)
	var one struct {
		Event CalendarEntry `json:"event"`
	}
	testutil.Call(t, testHandler.GetCalendarEvent, testutil.WithURLParams(newRequest(http.MethodGet, "/api/calendar/events/"+e.ID, nil), "id", e.ID)).Want(http.StatusOK).JSON(&one)
	accepted := false
	for _, p := range one.Event.Participants {
		if p.ID == otherUser && p.Response == "accepted" {
			accepted = true
		}
	}
	if !accepted {
		t.Fatalf("guest response not recorded: %+v", one.Event.Participants)
	}

	// Update moves it (guest re-invited) and keeps the recorded answer; cancel marks it.
	testutil.Call(t, testHandler.UpdateCalendarEvent, testutil.WithURLParams(newRequest(http.MethodPut, "/api/calendar/events/"+e.ID, map[string]any{
		"title": "Design review (moved)", "starts_at": start.Add(2 * time.Hour).Format(time.RFC3339), "ends_at": start.Add(3 * time.Hour).Format(time.RFC3339), "timezone": "Europe/Paris",
		"participants": []map[string]any{{"type": "member", "id": otherUser}},
	}), "id", e.ID)).Want(http.StatusOK).JSON(&one)
	if one.Event.Title != "Design review (moved)" || len(one.Event.Participants) != 1 || one.Event.Participants[0].Response != "accepted" {
		t.Fatalf("after update = %+v", one.Event)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'event_id' = $3`, InboxTypeCalendarInvitation, otherUser, e.ID); n != 2 {
		t.Fatalf("a moved event re-invites: %d items, want 2", n)
	}
	testutil.Call(t, testHandler.CancelCalendarEvent, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/calendar/events/"+e.ID, nil), "id", e.ID)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.ListCalendarEvents, newRequest(http.MethodGet, "/api/calendar/events?from=2026-10-01T00:00:00Z&to=2026-10-03T00:00:00Z", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Events) != 0 {
		t.Fatalf("cancelled event still listed by default")
	}
}

func TestFindFreeSlotsRespectsBusyTimesAndWorkingHours(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	// Thursday 1 October 2026, 08:00 Paris → the day; a meeting 10:00–11:30 Paris.
	from := time.Date(2026, 10, 1, 8, 0, 0, 0, paris)
	to := from.Add(24 * time.Hour)
	busy := []busyInterval{{time.Date(2026, 10, 1, 10, 0, 0, 0, paris), time.Date(2026, 10, 1, 11, 30, 0, 0, paris)}}
	slots := findFreeSlots(from, to, time.Hour, paris, true, busy, 3)
	if len(slots) != 3 {
		t.Fatalf("slots = %+v", slots)
	}
	want := []string{"09:00", "11:30", "12:30"}
	for i, s := range slots {
		if got := s.start.In(paris).Format("15:04"); got != want[i] {
			t.Errorf("slot %d starts %s, want %s", i, got, want[i])
		}
	}
	// Agents only: any hour, so the first slot is the window start.
	agentSlots := findFreeSlots(from, to, 30*time.Minute, paris, false, busy, 1)
	if len(agentSlots) != 1 || !agentSlots[0].start.Equal(from) {
		t.Fatalf("agent slots = %+v", agentSlots)
	}
	// A weekend has no member slot at all.
	sat := time.Date(2026, 10, 3, 8, 0, 0, 0, paris)
	if got := findFreeSlots(sat, sat.Add(24*time.Hour), time.Hour, paris, true, nil, 3); len(got) != 0 {
		t.Fatalf("weekend slots = %+v", got)
	}
}

func TestCalendarSlotsEndpointReadsEveryParticipantsEvents(t *testing.T) {
	calendarCleanup(t)
	agent := dbfx.Agent(t, "busy agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	// Next Monday at 10:00 UTC, the agent is busy for two hours.
	day := time.Now().UTC().Add(7 * 24 * time.Hour)
	for day.Weekday() != time.Monday {
		day = day.Add(24 * time.Hour)
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	busyStart := day.Add(10 * time.Hour)
	if _, resp := createCalendarEvent(t, map[string]any{"title": "Busy", "starts_at": busyStart.Format(time.RFC3339), "ends_at": busyStart.Add(2 * time.Hour).Format(time.RFC3339), "participants": []map[string]any{{"type": "agent", "id": agent}}}); resp.Code != http.StatusCreated {
		t.Fatalf("busy event: %d", resp.Code)
	}
	var out struct {
		Slots []CalendarSlot `json:"slots"`
	}
	path := "/api/calendar/slots?participants=member:" + testUserID + ",agent:" + agent + "&duration=60&from=" + day.Add(9*time.Hour).Format(time.RFC3339) + "&to=" + day.Add(18*time.Hour).Format(time.RFC3339) + "&tz=UTC"
	testutil.Call(t, testHandler.FindCalendarSlots, newRequest(http.MethodGet, path, nil)).Want(http.StatusOK).JSON(&out)
	if len(out.Slots) == 0 {
		t.Fatalf("no slots")
	}
	for _, s := range out.Slots {
		st, _ := time.Parse(time.RFC3339, s.StartsAt)
		en, _ := time.Parse(time.RFC3339, s.EndsAt)
		if st.Before(busyStart.Add(2*time.Hour)) && en.After(busyStart) {
			t.Fatalf("slot %s–%s overlaps the agent's event", s.StartsAt, s.EndsAt)
		}
	}
	testutil.Call(t, testHandler.FindCalendarSlots, newRequest(http.MethodGet, "/api/calendar/slots?participants=robot:x", nil)).Want(http.StatusBadRequest)
}

func TestCalendarFeedTokenServesTheMembersEvents(t *testing.T) {
	calendarCleanup(t)
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	e, _ := createCalendarEvent(t, map[string]any{"title": "Feed me; please", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339), "participants": []map[string]any{{"type": "member", "id": testUserID}}})
	var minted struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	testutil.Call(t, testHandler.MintCalendarFeedToken, newRequest(http.MethodPost, "/api/calendar/feed-token", nil)).Want(http.StatusCreated).JSON(&minted)
	if !strings.HasPrefix(minted.Path, "/api/calendar/ics/mcal_") {
		t.Fatalf("path = %q", minted.Path)
	}
	token := strings.TrimPrefix(minted.Path, "/api/calendar/ics/")
	var status map[string]any
	testutil.Call(t, testHandler.GetCalendarFeedToken, newRequest(http.MethodGet, "/api/calendar/feed-token", nil)).Want(http.StatusOK).JSON(&status)
	if status["configured"] != true {
		t.Fatalf("status = %+v", status)
	}
	// Public read with the token, no session.
	req := httptest.NewRequest(http.MethodGet, minted.Path, nil)
	rec := testutil.Call(t, testHandler.ServeCalendarFeed, testutil.WithURLParams(req, "token", token)).Want(http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.Contains(body, "UID:"+e.ID+"@vigil") || !strings.Contains(body, "SUMMARY:Feed me\\; please") {
		t.Fatalf("feed = %s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Fatalf("content-type = %q", ct)
	}
	testutil.Call(t, testHandler.ServeCalendarFeed, testutil.WithURLParams(httptest.NewRequest(http.MethodGet, "/api/calendar/ics/mcal_nope", nil), "token", "mcal_nope")).Want(http.StatusNotFound)
	// Revoke: the same URL dies.
	testutil.Call(t, testHandler.RevokeCalendarFeedToken, newRequest(http.MethodDelete, "/api/calendar/feed-token", nil)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.ServeCalendarFeed, testutil.WithURLParams(httptest.NewRequest(http.MethodGet, minted.Path, nil), "token", token)).Want(http.StatusNotFound)
}

func TestAgentProposalIsSettledByItsDecisionCard(t *testing.T) {
	calendarCleanup(t)
	issue, task, agent := runningAgentRun(t, "calendar proposal")
	guest := calendarGuest(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Hour)
	body := map[string]any{"title": "Kickoff", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339), "participants": []map[string]any{{"type": "member", "id": guest}, {"type": "member", "id": testUserID}}}
	// Without an issue an agent cannot propose.
	_, resp := createCalendarEvent(t, body, gateHeaders(task, agent)...)
	resp.Want(http.StatusBadRequest)
	body["issue_id"] = issue
	e, resp := createCalendarEvent(t, body, gateHeaders(task, agent)...)
	resp.Want(http.StatusCreated)
	if e.Status != CalendarStatusProposed || e.DecisionID == nil || e.CreatedBy.Type != "agent" {
		t.Fatalf("proposal = %+v", e)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND details->>'event_id' = $2`, InboxTypeCalendarInvitation, e.ID); n != 0 {
		t.Fatalf("a proposal must not invite anyone yet")
	}
	// The feed shows the proposal as a card of kind calendar_proposal.
	feed := listApprovals(t, "?issue_id="+issue)
	card := findApproval(feed.Approvals, ApprovalSourceDecision, *e.DecisionID)
	if card == nil || card.Kind != ApprovalKindCalendar || len(card.Options) != 2 {
		t.Fatalf("card = %+v", card)
	}
	// Accepting schedules it and invites the participants.
	respondDecision(t, issue, *e.DecisionID, map[string]any{"option_id": calendarAcceptOption}).Want(http.StatusOK)
	var one struct {
		Event CalendarEntry `json:"event"`
	}
	testutil.Call(t, testHandler.GetCalendarEvent, testutil.WithURLParams(newRequest(http.MethodGet, "/api/calendar/events/"+e.ID, nil), "id", e.ID)).Want(http.StatusOK).JSON(&one)
	if one.Event.Status != CalendarStatusScheduled {
		t.Fatalf("after accept = %+v", one.Event)
	}
	// The guest is invited; the acceptor already knows.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND details->>'event_id' = $2 AND recipient_id = $3`, InboxTypeCalendarInvitation, e.ID, guest); n != 1 {
		t.Fatalf("invitations after accept = %d, want 1", n)
	}
	// A second proposal, declined, is cancelled.
	e2, resp := createCalendarEvent(t, map[string]any{"title": "Second", "starts_at": start.Add(24 * time.Hour).Format(time.RFC3339), "ends_at": start.Add(25 * time.Hour).Format(time.RFC3339), "issue_id": issue}, gateHeaders(task, agent)...)
	resp.Want(http.StatusCreated)
	respondDecision(t, issue, *e2.DecisionID, map[string]any{"option_id": calendarDeclineOption}).Want(http.StatusOK)
	var status string
	dbfx.QueryRow(t, `SELECT status FROM calendar_event WHERE id = $1`, e2.ID).Scan(&status)
	if status != CalendarStatusCancelled {
		t.Fatalf("declined proposal status = %q", status)
	}
}

func TestCalendarRemindersFireOnceBeforeTheStart(t *testing.T) {
	calendarCleanup(t)
	start := time.Now().UTC().Add(10 * time.Minute)
	e, _ := createCalendarEvent(t, map[string]any{"title": "Soon", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(30 * time.Minute).Format(time.RFC3339), "participants": []map[string]any{{"type": "member", "id": testUserID}}})
	createCalendarEvent(t, map[string]any{"title": "Later", "starts_at": start.Add(2 * time.Hour).Format(time.RFC3339), "ends_at": start.Add(3 * time.Hour).Format(time.RFC3339), "participants": []map[string]any{{"type": "member", "id": testUserID}}})
	if n := testHandler.RemindCalendarEvents(context.Background()); n != 1 {
		t.Fatalf("reminded = %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'event_id' = $3`, InboxTypeCalendarReminder, testUserID, e.ID); n != 1 {
		t.Fatalf("reminder items = %d", n)
	}
	if n := testHandler.RemindCalendarEvents(context.Background()); n != 0 {
		t.Fatalf("second sweep reminded %d", n)
	}
}

type fakeCalendarSync struct {
	connected bool
	events    []ExternalCalendarEvent
	created   []db.CalendarEvent
}

func (f *fakeCalendarSync) HasConnection(context.Context, pgtype.UUID) bool { return f.connected }
func (f *fakeCalendarSync) ListEvents(context.Context, pgtype.UUID, time.Time, time.Time) ([]ExternalCalendarEvent, error) {
	return f.events, nil
}
func (f *fakeCalendarSync) CreateEvent(_ context.Context, _ pgtype.UUID, e db.CalendarEvent) (string, error) {
	f.created = append(f.created, e)
	return "g-" + uuidToString(e.ID)[:8], nil
}

func TestGoogleImportAndExportThroughTheProvider(t *testing.T) {
	calendarCleanup(t)
	prev := testHandler.CalendarSync
	fake := &fakeCalendarSync{connected: true}
	testHandler.CalendarSync = fake
	t.Cleanup(func() { testHandler.CalendarSync = prev })
	start := time.Now().UTC().Add(5 * 24 * time.Hour).Truncate(time.Hour)
	fake.events = []ExternalCalendarEvent{{ID: "g-1", Title: "Dentist", Start: start, End: start.Add(time.Hour)}, {ID: "g-2", Title: "Offsite", Start: start.Add(24 * time.Hour), End: start.Add(48 * time.Hour), AllDay: true}}

	var out struct {
		Created, Updated, Seen int
	}
	testutil.Call(t, testHandler.ImportGoogleCalendar, newRequest(http.MethodPost, "/api/calendar/google/import", map[string]any{"from": start.Add(-time.Hour).Format(time.RFC3339), "to": start.Add(72 * time.Hour).Format(time.RFC3339)})).Want(http.StatusOK).JSON(&out)
	if out.Created != 2 || out.Seen != 2 {
		t.Fatalf("import = %+v", out)
	}
	// A second import updates rather than duplicates.
	fake.events[0].Title = "Dentist (moved)"
	testutil.Call(t, testHandler.ImportGoogleCalendar, newRequest(http.MethodPost, "/api/calendar/google/import", map[string]any{"from": start.Add(-time.Hour).Format(time.RFC3339), "to": start.Add(72 * time.Hour).Format(time.RFC3339)})).Want(http.StatusOK).JSON(&out)
	if out.Created != 0 || out.Updated != 2 {
		t.Fatalf("second import = %+v", out)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM calendar_event WHERE workspace_id = $1 AND source = 'google' AND title = 'Dentist (moved)'`, testWorkspaceID); n != 1 {
		t.Fatalf("updated google event rows = %d", n)
	}
	// Scheduling here exports to the provider (off the request; wait briefly).
	e, _ := createCalendarEvent(t, map[string]any{"title": "Exported", "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339)})
	deadline := time.Now().Add(3 * time.Second)
	for len(fake.created) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(fake.created) != 1 || fake.created[0].Title != "Exported" {
		t.Fatalf("exported = %+v", fake.created)
	}
	deadline = time.Now().Add(3 * time.Second)
	var external string
	for external == "" && time.Now().Before(deadline) {
		dbfx.QueryRow(t, `SELECT external_id FROM calendar_event WHERE id = $1`, e.ID).Scan(&external)
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.HasPrefix(external, "g-") {
		t.Fatalf("external id = %q", external)
	}
	fake.connected = false
	testutil.Call(t, testHandler.ImportGoogleCalendar, newRequest(http.MethodPost, "/api/calendar/google/import", map[string]any{})).Want(http.StatusConflict)
}
