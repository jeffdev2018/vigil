package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// stubCalendarFeed serves one ICS document and points the caller's saved feed
// at it. The SSRF guard is lifted for the test's own loopback server; the
// guard itself is pinned by TestBlockNonPublicAddress.
func stubCalendarFeed(t *testing.T, body func() (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status, payload := body()
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	prev := calendarDialControl
	calendarDialControl = func(string, string) error { return nil }
	t.Cleanup(func() { calendarDialControl = prev })
	t.Cleanup(clearCalendarCache)
	clearCalendarCache()
	return srv
}

func clearCalendarCache() {
	calendarCacheMu.Lock()
	clear(calendarCache)
	calendarCacheMu.Unlock()
}

// saveFeed puts a URL in place through the endpoint the UI uses, rewritten to
// https so it passes validation, then swaps in the plain-HTTP test URL.
func saveFeed(t *testing.T, rawURL string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM user_calendar_feed WHERE user_id = $1`, testUserID)
	dbfx.Exec(t, `INSERT INTO user_calendar_feed (workspace_id, user_id, url) VALUES ($1, $2, $3)
	              ON CONFLICT (workspace_id, user_id) DO UPDATE SET url = EXCLUDED.url, last_error = '', last_fetched_at = NULL`,
		testWorkspaceID, testUserID, rawURL)
	clearCalendarCache()
}

func icsAround(t *testing.T, now time.Time) string {
	t.Helper()
	stamp := func(offset time.Duration) string {
		return now.Add(offset).UTC().Format("20060102T150405Z")
	}
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Sprint review\r\nURL:https://meet.example.test/x\r\n" +
		"DTSTART:" + stamp(-10*time.Minute) + "\r\nDTEND:" + stamp(20*time.Minute) + "\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Soon enough\r\n" +
		"DTSTART:" + stamp(15*time.Minute) + "\r\nDTEND:" + stamp(45*time.Minute) + "\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Tomorrow\r\n" +
		"DTSTART:" + stamp(24*time.Hour) + "\r\nDTEND:" + stamp(25*time.Hour) + "\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
}

func TestCalendarUpcomingReportsTheWindow(t *testing.T) {
	now := time.Now()
	hits := 0
	srv := stubCalendarFeed(t, func() (int, string) {
		hits++
		return http.StatusOK, icsAround(t, now)
	})
	saveFeed(t, srv.URL+"/calendar.ics")

	var out CalendarUpcomingResponse
	testutil.Call(t, testHandler.UpcomingCalendar, newRequest(http.MethodGet, "/api/calendar/upcoming?within=30m", nil)).
		Want(http.StatusOK).JSON(&out)
	if !out.Configured {
		t.Fatal("configured must be true once a feed is saved")
	}
	if len(out.Events) != 2 {
		t.Fatalf("events = %+v, want the running one and the one starting in 15m", out.Events)
	}
	if out.Events[0].Summary != "Sprint review" || !out.Events[0].InProgress {
		t.Fatalf("first event = %+v, want the running Sprint review", out.Events[0])
	}
	if out.Events[1].Summary != "Soon enough" || out.Events[1].InProgress {
		t.Fatalf("second event = %+v", out.Events[1])
	}

	// A wider window reaches tomorrow — this is what "Check feed" asks for.
	testutil.Call(t, testHandler.UpcomingCalendar, newRequest(http.MethodGet, "/api/calendar/upcoming?within=48h", nil)).
		Want(http.StatusOK).JSON(&out)
	if len(out.Events) != 3 {
		t.Fatalf("events over 48h = %d, want 3", len(out.Events))
	}
	// Both reads came from one download: a feed is fetched at most once per
	// cache window, per user.
	if hits != 1 {
		t.Fatalf("feed downloads = %d, want 1 (the cache serves the rest)", hits)
	}

	testutil.Call(t, testHandler.UpcomingCalendar, newRequest(http.MethodGet, "/api/calendar/upcoming?within=nonsense", nil)).
		Want(http.StatusBadRequest)
}

func TestCalendarUpcomingWithoutAFeedIsEmptyNotAnError(t *testing.T) {
	dbfx.Cleanup(t, `DELETE FROM user_calendar_feed WHERE user_id = $1`, testUserID)
	dbfx.Exec(t, `DELETE FROM user_calendar_feed WHERE user_id = $1`, testUserID)
	clearCalendarCache()
	var out CalendarUpcomingResponse
	testutil.Call(t, testHandler.UpcomingCalendar, newRequest(http.MethodGet, "/api/calendar/upcoming", nil)).
		Want(http.StatusOK).JSON(&out)
	if out.Configured || len(out.Events) != 0 {
		t.Fatalf("out = %+v, want an unconfigured empty answer", out)
	}
}

// The Settings "Check feed" button reads this: a broken feed has to say why.
func TestCalendarUpcomingReportsAnUnreadableFeed(t *testing.T) {
	srv := stubCalendarFeed(t, func() (int, string) { return http.StatusNotFound, "nope" })
	saveFeed(t, srv.URL+"/calendar.ics")
	body := testutil.Call(t, testHandler.UpcomingCalendar, newRequest(http.MethodGet, "/api/calendar/upcoming", nil)).
		Want(http.StatusBadGateway).Map()
	if body["code"] != "calendar_feed_failed" || !strings.Contains(fmt.Sprint(body["error"]), "404") {
		t.Fatalf("body = %v, want the upstream status", body)
	}
	// And the failure is kept, so Settings can explain a silent calendar.
	var lastError string
	dbfx.QueryRow(t, `SELECT last_error FROM user_calendar_feed WHERE user_id = $1`, testUserID).Scan(&lastError)
	if !strings.Contains(lastError, "404") {
		t.Fatalf("last_error = %q", lastError)
	}
}

func TestCalendarFeedCRUDValidatesTheURL(t *testing.T) {
	dbfx.Cleanup(t, `DELETE FROM user_calendar_feed WHERE user_id = $1`, testUserID)
	var feed CalendarFeedResponse
	testutil.Call(t, testHandler.GetCalendarFeed, newRequest(http.MethodGet, "/api/calendar/feed", nil)).
		Want(http.StatusOK).JSON(&feed)
	if feed.URL != "" {
		t.Fatalf("feed = %+v, want empty before anything is saved", feed)
	}

	// webcal:// is what a calendar app hands out; it is HTTPS on the wire.
	testutil.Call(t, testHandler.SetCalendarFeed,
		newRequest(http.MethodPut, "/api/calendar/feed", map[string]string{"url": " webcal://cal.example.test/f.ics "})).
		Want(http.StatusOK).JSON(&feed)
	if feed.URL != "https://cal.example.test/f.ics" {
		t.Fatalf("saved url = %q", feed.URL)
	}

	// Everything else is refused: http:// would carry a capability URL in
	// clear, and file:// is how a server-side fetcher reads its own disk.
	for _, bad := range []string{"", "   ", "http://cal.example.test/f.ics", "file:///etc/passwd", "ftp://x/y.ics", "https://"} {
		testutil.Call(t, testHandler.SetCalendarFeed,
			newRequest(http.MethodPut, "/api/calendar/feed", map[string]string{"url": bad})).
			Want(http.StatusBadRequest)
	}

	testutil.Call(t, testHandler.DeleteCalendarFeed, newRequest(http.MethodDelete, "/api/calendar/feed", nil)).
		Want(http.StatusNoContent)
	testutil.Call(t, testHandler.GetCalendarFeed, newRequest(http.MethodGet, "/api/calendar/feed", nil)).
		Want(http.StatusOK).JSON(&feed)
	if feed.URL != "" {
		t.Fatalf("feed after delete = %+v", feed)
	}
}

// A meeting created while an event is running takes its name — but only when
// the client sent none.
func TestCreateMeetingTakesItsTitleFromTheCalendar(t *testing.T) {
	stubSTT(t, "unused")
	now := time.Now()
	srv := stubCalendarFeed(t, func() (int, string) { return http.StatusOK, icsAround(t, now) })
	saveFeed(t, srv.URL+"/calendar.ics")

	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	if created.Title != "Sprint review" {
		t.Fatalf("title = %q, want the running event's summary", created.Title)
	}

	var named MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "My own name"})).
		Want(http.StatusCreated).JSON(&named)
	cleanupMeeting(t, named.ID)
	if named.Title != "My own name" {
		t.Fatalf("title = %q, want the client's own", named.Title)
	}
}

// A calendar that cannot be read must never stop a recording from starting.
func TestCreateMeetingFallsBackWhenTheFeedIsBroken(t *testing.T) {
	stubSTT(t, "unused")
	srv := stubCalendarFeed(t, func() (int, string) { return http.StatusInternalServerError, "" })
	saveFeed(t, srv.URL+"/calendar.ics")
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	if !strings.HasPrefix(created.Title, "Meeting ") {
		t.Fatalf("title = %q, want the timestamp fallback", created.Title)
	}
}

// The feed URL comes from a user and the server fetches it, so the guard is
// the whole reason this is safe to offer.
func TestBlockNonPublicAddress(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:443", "[::1]:443", "10.0.0.5:443", "192.168.1.10:443",
		"172.16.0.1:443", "169.254.169.254:80", "0.0.0.0:443",
	} {
		if err := blockNonPublicAddress("tcp", address); err == nil {
			t.Errorf("%s was allowed, want refused", address)
		}
	}
	for _, address := range []string{"93.184.216.34:443", "[2606:2800:220:1:248:1893:25c8:1946]:443"} {
		if err := blockNonPublicAddress("tcp", address); err != nil {
			t.Errorf("%s was refused: %v", address, err)
		}
	}
}
