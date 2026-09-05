package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/icalendar"
)

// Calendar subscription. The user pastes the read-only ICS URL their calendar
// already publishes; no OAuth, no provider integration, no write access. It
// answers one question — "what meeting is happening right now?" — which names
// a recording the user would otherwise call "Meeting 2026-09-04 14:03".
//
// The URL is a capability: an ICS link is unauthenticated, so anyone holding
// it reads the calendar. It is therefore stored per workspace (see migration
// 640), never echoed to anyone but its owner, and never fetched without the
// SSRF guard below.

const (
	// calendarFetchTimeout bounds one feed download.
	calendarFetchTimeout = 10 * time.Second
	// calendarMaxBytes bounds one feed. A year of a busy shared calendar is
	// a few hundred kilobytes; past this it is not a calendar we can use.
	calendarMaxBytes = 2 << 20
	// calendarCacheTTL is how long a parsed feed is reused. The window this
	// serves is "the next half hour", so five-minute-old data answers it, and
	// a meeting starting in three minutes is already in the cached window.
	calendarCacheTTL = 5 * time.Minute
	// calendarWindow is how far ahead recurrences are expanded, and the
	// ceiling on the `within` query parameter.
	calendarWindow = 14 * 24 * time.Hour
	// calendarDefaultWithin is the window when the caller names none.
	calendarDefaultWithin = 30 * time.Minute
	// calendarPast is how far back the parsed window reaches, so a meeting
	// that started before the fetch is still reported as in progress.
	calendarPast      = 4 * time.Hour
	calendarMaxURLLen = 2000
)

// calendarDialControl refuses to connect anywhere but a public address. The
// URL comes from a user and the server fetches it: without this, anyone with
// an account could point a feed at the cloud metadata service or at something
// only reachable from inside the network, and read the answer back through
// this endpoint. Indirected through a variable so a test can reach its own
// httptest server on loopback; the real check is tested directly.
var calendarDialControl = blockNonPublicAddress

var calendarHTTPClient = &http.Client{
	Timeout: calendarFetchTimeout,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				return calendarDialControl(network, address)
			},
		}).DialContext,
	},
}

// blockNonPublicAddress is the guard itself, on the resolved address: the
// check has to happen after DNS, or a hostname resolving to 127.0.0.1 walks
// straight through it.
func blockNonPublicAddress(_ string, address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("calendar: bad address %q", address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("calendar: unresolved address %q", host)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("calendar: refusing to fetch a feed at a non-public address")
	}
	return nil
}

type calendarCacheEntry struct {
	url    string
	events []icalendar.Event
	err    error
	at     time.Time
}

var (
	calendarCacheMu sync.Mutex
	calendarCache   = map[string]calendarCacheEntry{}
)

// normalizeCalendarURL validates the user's input and returns the URL to
// fetch. `webcal://` is the scheme calendar apps hand out for subscriptions;
// it is plain HTTPS on the wire. Nothing else is accepted: `http://` would
// carry the capability URL in clear text, and `file://` and friends are how a
// server-side fetcher reads the disk.
func normalizeCalendarURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if len(raw) > calendarMaxURLLen {
		return "", errors.New("url is too long")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("url is not a valid URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "webcal":
		parsed.Scheme = "https"
	default:
		return "", errors.New("url must start with https:// or webcal://")
	}
	if parsed.Host == "" {
		return "", errors.New("url is missing a host")
	}
	return parsed.String(), nil
}

// fetchCalendarFeed downloads one feed, bounded in time and size.
func fetchCalendarFeed(ctx context.Context, feedURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, calendarFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, errors.New("could not request the feed")
	}
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.5")
	resp, err := calendarHTTPClient.Do(req)
	if err != nil {
		// The upstream error can name an internal host the caller guessed at,
		// so it never reaches the response body.
		slog.Warn("calendar fetch failed", "error", err)
		return nil, errors.New("could not reach the calendar feed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the calendar feed answered %d", resp.StatusCode)
	}
	// One byte past the cap tells a truncated read from an exact fit.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, calendarMaxBytes+1))
	if err != nil {
		return nil, errors.New("could not read the calendar feed")
	}
	if len(raw) > calendarMaxBytes {
		return nil, errors.New("the calendar feed is too large")
	}
	if len(raw) == 0 {
		return nil, errors.New("the calendar feed is empty")
	}
	return raw, nil
}

// calendarEvents returns the caller's events for the next two weeks, from the
// cache when it is fresh. `configured` is false when the user has no feed —
// which is not an error anywhere this is called from, but reads differently
// from a calendar with nothing in it.
func (h *Handler) calendarEvents(ctx context.Context, workspaceID pgtype.UUID, userID string, now time.Time) (events []icalendar.Event, configured bool, err error) {
	uid, err := util.ParseUUID(userID)
	if err != nil {
		return nil, false, nil
	}
	feed, err := h.Queries.GetUserCalendarFeed(ctx, db.GetUserCalendarFeedParams{WorkspaceID: workspaceID, UserID: uid})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read calendar feed: %w", err)
	}
	key := uuidToString(workspaceID) + ":" + userID
	calendarCacheMu.Lock()
	entry, cached := calendarCache[key]
	calendarCacheMu.Unlock()
	// A changed URL invalidates the entry: it is a different calendar, and
	// the previous one's error is not this one's.
	if cached && entry.url == feed.Url && now.Sub(entry.at) < calendarCacheTTL {
		return entry.events, true, entry.err
	}

	raw, err := fetchCalendarFeed(ctx, feed.Url)
	if err == nil {
		events, err = icalendar.Parse(raw, now.Add(-calendarPast), now.Add(calendarWindow))
		if err != nil {
			err = errors.New("the calendar feed is not valid iCalendar")
		}
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	// Best effort: the outcome is a diagnostic for Settings, and failing to
	// record it must not turn a working read into an error.
	if recErr := h.Queries.RecordUserCalendarFeedFetch(ctx, db.RecordUserCalendarFeedFetchParams{
		WorkspaceID: workspaceID, UserID: uid, Url: feed.Url, LastError: message,
	}); recErr != nil {
		slog.Warn("calendar: record fetch failed", "error", recErr)
	}
	calendarCacheMu.Lock()
	calendarCache[key] = calendarCacheEntry{url: feed.Url, events: events, err: err, at: now}
	calendarCacheMu.Unlock()
	return events, true, err
}

// currentCalendarEvent is the event covering `now`, if any. Errors are
// swallowed: every caller has something reasonable to do without a calendar,
// and none of them should fail because a feed is down.
func (h *Handler) currentCalendarEvent(ctx context.Context, workspaceID pgtype.UUID, userID string, now time.Time) (icalendar.Event, bool) {
	events, _, err := h.calendarEvents(ctx, workspaceID, userID, now)
	if err != nil {
		return icalendar.Event{}, false
	}
	for _, event := range events {
		if event.Active(now) && strings.TrimSpace(event.Summary) != "" {
			return event, true
		}
	}
	return icalendar.Event{}, false
}

type CalendarEventResponse struct {
	Summary    string    `json:"summary"`
	URL        string    `json:"url,omitempty"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	InProgress bool      `json:"in_progress"`
}

type CalendarUpcomingResponse struct {
	Events []CalendarEventResponse `json:"events"`
	// Configured is false when the user has no feed: the client shows the
	// setting rather than an empty list that looks like a free afternoon.
	Configured bool `json:"configured"`
}

type CalendarFeedResponse struct {
	URL           string  `json:"url"`
	LastFetchedAt *string `json:"last_fetched_at,omitempty"`
	LastError     string  `json:"last_error,omitempty"`
}

func calendarFeedToResponse(feed db.UserCalendarFeed) CalendarFeedResponse {
	out := CalendarFeedResponse{URL: feed.Url, LastError: feed.LastError}
	if feed.LastFetchedAt.Valid {
		at := feed.LastFetchedAt.Time.UTC().Format(time.RFC3339)
		out.LastFetchedAt = &at
	}
	return out
}

// GetCalendarFeed returns the caller's own subscription. GET /api/calendar/feed.
func (h *Handler) GetCalendarFeed(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.calendarCaller(w, r)
	if !ok {
		return
	}
	feed, err := h.Queries.GetUserCalendarFeed(r.Context(), db.GetUserCalendarFeedParams{
		WorkspaceID: workspaceID, UserID: parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, CalendarFeedResponse{})
			return
		}
		slog.Error("get calendar feed failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load the calendar feed")
		return
	}
	writeJSON(w, http.StatusOK, calendarFeedToResponse(feed))
}

type calendarFeedRequest struct {
	URL string `json:"url"`
}

// SetCalendarFeed saves the caller's subscription. PUT /api/calendar/feed.
func (h *Handler) SetCalendarFeed(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.calendarCaller(w, r)
	if !ok {
		return
	}
	var req calendarFeedRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	feedURL, err := normalizeCalendarURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	feed, err := h.Queries.UpsertUserCalendarFeed(r.Context(), db.UpsertUserCalendarFeedParams{
		WorkspaceID: workspaceID, UserID: parseUUID(userID), Url: feedURL,
	})
	if err != nil {
		slog.Error("save calendar feed failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save the calendar feed")
		return
	}
	h.forgetCalendarCache(workspaceID, userID)
	writeJSON(w, http.StatusOK, calendarFeedToResponse(feed))
}

// DeleteCalendarFeed removes the caller's subscription. DELETE /api/calendar/feed.
func (h *Handler) DeleteCalendarFeed(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.calendarCaller(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteUserCalendarFeed(r.Context(), db.DeleteUserCalendarFeedParams{
		WorkspaceID: workspaceID, UserID: parseUUID(userID),
	}); err != nil {
		slog.Error("delete calendar feed failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove the calendar feed")
		return
	}
	h.forgetCalendarCache(workspaceID, userID)
	w.WriteHeader(http.StatusNoContent)
}

// UpcomingCalendar answers what is happening now or about to.
// GET /api/calendar/upcoming?within=30m.
//
// 502 on a feed that cannot be read, with the reason: this endpoint is also
// the "Check feed" button in Settings, and a user who just pasted a URL needs
// to know whether it works.
func (h *Handler) UpcomingCalendar(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.calendarCaller(w, r)
	if !ok {
		return
	}
	within := calendarDefaultWithin
	if raw := strings.TrimSpace(r.URL.Query().Get("within")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "within must be a positive duration such as 30m")
			return
		}
		within = min(parsed, calendarWindow)
	}
	now := time.Now()
	events, configured, err := h.calendarEvents(r.Context(), workspaceID, userID, now)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "calendar_feed_failed", err.Error())
		return
	}
	out := CalendarUpcomingResponse{Events: []CalendarEventResponse{}, Configured: configured}
	deadline := now.Add(within)
	for _, event := range events {
		if event.Start.After(deadline) || !event.End.After(now) {
			continue
		}
		out.Events = append(out.Events, CalendarEventResponse{
			Summary:    event.Summary,
			URL:        event.URL,
			Start:      event.Start.UTC(),
			End:        event.End.UTC(),
			InProgress: event.Active(now),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// calendarCaller resolves the caller and their workspace. A calendar feed is
// personal, so there is no path parameter and nothing to authorize beyond
// being a member of the workspace, which the router already enforces.
func (h *Handler) calendarCaller(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", pgtype.UUID{}, false
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return "", pgtype.UUID{}, false
	}
	if _, err := util.ParseUUID(userID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return "", pgtype.UUID{}, false
	}
	return userID, workspaceID, true
}

func (h *Handler) forgetCalendarCache(workspaceID pgtype.UUID, userID string) {
	calendarCacheMu.Lock()
	delete(calendarCache, uuidToString(workspaceID)+":"+userID)
	calendarCacheMu.Unlock()
}
