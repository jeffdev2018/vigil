package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The bridge's rules, each exercised against the in-memory store in
// fake_store_test.go. The canonical home for signature and payload parsing is
// webhook_test.go; this file is about what an event DOES.

func issueEvent(t *testing.T, action, assigneeID, stateType, title string) WebhookEvent {
	t.Helper()
	data := map[string]any{
		"id": "iss_1", "identifier": "ENG-7", "title": title, "description": "desc",
		"url": "https://linear.app/acme/issue/ENG-7", "teamId": "team_1",
		"state": map[string]any{"id": "st_1", "name": stateType, "type": stateType},
	}
	if assigneeID != "" {
		data["assigneeId"] = assigneeID
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal issue data: %v", err)
	}
	return WebhookEvent{Action: action, Type: "Issue", OrganizationID: "org_1", Data: raw}
}

func commentEvent(t *testing.T, commentID, userID, body string) WebhookEvent {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id": commentID, "body": body, "issueId": "iss_1",
		"user": map[string]any{"id": userID, "name": "Dana"},
	})
	if err != nil {
		t.Fatalf("marshal comment data: %v", err)
	}
	return WebhookEvent{Action: "create", Type: "Comment", OrganizationID: "org_1", Data: raw}
}

func TestLinearAssignmentMirrorsIssueExactlyOnce(t *testing.T) {
	bridge, store, _, creator, inst := newTestBridge(t)
	ev := issueEvent(t, "update", "user_app", "started", "Ship it")

	if err := bridge.HandleIssueEvent(context.Background(), inst, ev); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	// Linear redelivers on any non-2xx, so the same payload arriving twice is
	// the normal case, not an edge case.
	if err := bridge.HandleIssueEvent(context.Background(), inst, ev); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	if store.createdIssues != 1 {
		t.Fatalf("created %d mirror issues, want exactly 1 (a redelivered webhook must not create a second)", store.createdIssues)
	}
	if len(store.issueLinks) != 1 {
		t.Fatalf("created %d links, want 1", len(store.issueLinks))
	}
	if creator.last.AgentID != inst.AgentID {
		t.Fatalf("mirror assigned to %v, want the installation's agent %v", creator.last.AgentID, inst.AgentID)
	}
	if creator.last.Status != issuestatus.InProgress {
		t.Fatalf("mirror status = %q, want %q (started maps to in_progress)", creator.last.Status, issuestatus.InProgress)
	}
	if creator.last.InstallationID != inst.ID {
		t.Fatalf("mirror origin id = %v, want the installation id", creator.last.InstallationID)
	}
}

func TestLinearIssueAssignedElsewhereIsIgnoredThenPaused(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	ctx := context.Background()

	// Assigned to a human: not ours, nothing happens.
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_human", "unstarted", "Theirs")); err != nil {
		t.Fatalf("unrelated issue: %v", err)
	}
	if store.createdIssues != 0 {
		t.Fatalf("created %d issues for an issue assigned to someone else, want 0", store.createdIssues)
	}

	// Assign to us, then hand it back to a human.
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "update", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "update", "user_human", "unstarted", "Ours")); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if got := store.issueLinks[0].SyncState; got != "paused" {
		t.Fatalf("sync_state = %q after unassignment, want paused", got)
	}
}

func TestLinearIssueUpdateOverwritesTitleAndStatus(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Original")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "update", "user_app", "completed", "Renamed")); err != nil {
		t.Fatalf("update: %v", err)
	}
	issue := store.issues[key(store.issueLinks[0].IssueID)]
	if issue.Title != "Renamed" {
		t.Fatalf("title = %q, want Renamed (Linear owns the title of a mirror)", issue.Title)
	}
	if issue.Status != issuestatus.Done {
		t.Fatalf("status = %q, want %q (completed maps to done)", issue.Status, issuestatus.Done)
	}
}

func TestLinearRemoveActionPausesTheLink(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "remove", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := store.issueLinks[0].SyncState; got != "paused" {
		t.Fatalf("sync_state = %q after remove, want paused", got)
	}
	// The Multica issue survives: a run may already have happened against it.
	if store.createdIssues != 1 {
		t.Fatalf("mirror issue count = %d, want 1", store.createdIssues)
	}
}

func TestLinearInboundCommentMirroredOnceAndOwnEchoIgnored(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}

	human := commentEvent(t, "c_1", "user_human", "please add tests")
	if err := bridge.HandleCommentEvent(ctx, inst, human); err != nil {
		t.Fatalf("inbound comment: %v", err)
	}
	if err := bridge.HandleCommentEvent(ctx, inst, human); err != nil {
		t.Fatalf("inbound redelivery: %v", err)
	}
	if store.createdComments != 1 {
		t.Fatalf("created %d comments, want exactly 1 (a redelivered comment must not be mirrored twice)", store.createdComments)
	}
	if got := store.comments[0].Content; got == "" || got == "please add tests" {
		t.Fatalf("mirrored body = %q, want the Linear author named in it", got)
	}

	// A comment authored by the app itself is one WE pushed coming back.
	if err := bridge.HandleCommentEvent(ctx, inst, commentEvent(t, "c_2", "user_app", "agent reply")); err != nil {
		t.Fatalf("own echo: %v", err)
	}
	if store.createdComments != 1 {
		t.Fatalf("created %d comments, want 1 (the app's own comment must not be mirrored back)", store.createdComments)
	}
}

func TestLinearPushCommentSkipsInboundMirrorAndRecordsOutbound(t *testing.T) {
	bridge, store, api, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	link := store.issueLinks[0]
	if err := bridge.HandleCommentEvent(ctx, inst, commentEvent(t, "c_1", "user_human", "from linear")); err != nil {
		t.Fatalf("inbound comment: %v", err)
	}
	mirrored := store.comments[0]

	// The comment the bridge just wrote must not be pushed back to where it
	// came from — this is the second half of the loop guard.
	if err := bridge.PushComment(ctx, link.IssueID, mirrored.ID, mirrored.Content); err != nil {
		t.Fatalf("push mirrored comment: %v", err)
	}
	if len(api.comments) != 0 {
		t.Fatalf("pushed %d comments to Linear, want 0 (a mirrored comment must not be echoed back)", len(api.comments))
	}

	// A genuinely local comment does go out, and gets its own link so the
	// webhook echo of it is ignored in turn.
	local := newUUID()
	if err := bridge.PushComment(ctx, link.IssueID, local, "agent says hello"); err != nil {
		t.Fatalf("push local comment: %v", err)
	}
	if len(api.comments) != 1 || api.comments[0].IssueID != "iss_1" {
		t.Fatalf("pushed %+v, want one comment on iss_1", api.comments)
	}
	if _, err := store.GetLinearCommentLinkByComment(ctx, local); err != nil {
		t.Fatalf("outbound comment link not recorded: %v", err)
	}
}

func TestLinearPushStatusUsesReverseMapAndCachesTeamStates(t *testing.T) {
	bridge, store, api, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	link := store.issueLinks[0]

	if err := bridge.PushStatus(ctx, link.IssueID, issuestatus.Done); err != nil {
		t.Fatalf("push done: %v", err)
	}
	if len(api.stateWrites) != 1 || api.stateWrites[0].StateID != "st_done" {
		t.Fatalf("state writes = %+v, want one write to st_done", api.stateWrites)
	}

	if err := bridge.PushStatus(ctx, link.IssueID, issuestatus.InProgress); err != nil {
		t.Fatalf("push in_progress: %v", err)
	}
	if len(api.stateWrites) != 2 || api.stateWrites[1].StateID != "st_doing" {
		t.Fatalf("state writes = %+v, want a second write to st_doing", api.stateWrites)
	}
	// The board is read once and cached: a status move must not cost an extra
	// round trip per issue.
	if api.stateCalls != 1 {
		t.Fatalf("ListTeamStates called %d times, want 1 (states are cached for %s)", api.stateCalls, teamStatesTTL)
	}

	// A status Linear has no column for is a no-op, not an error: workspaces
	// are free to define statuses the map does not cover.
	if err := bridge.PushStatus(ctx, link.IssueID, "human_review"); err != nil {
		t.Fatalf("push unmapped status: %v", err)
	}
	if len(api.stateWrites) != 2 {
		t.Fatalf("state writes = %d after an unmapped status, want still 2", len(api.stateWrites))
	}
}

func TestLinearReverseStatusMapIsDeterministic(t *testing.T) {
	// triage and unstarted both map to todo. Whichever wins must be the same
	// on every call, or the same status move would land in different columns.
	m := DefaultStatusMap()
	first := reverseStatusMap(m)[issuestatus.Todo]
	for i := 0; i < 20; i++ {
		if got := reverseStatusMap(m)[issuestatus.Todo]; got != first {
			t.Fatalf("reverse map for todo = %q on run %d, want the stable %q", got, i, first)
		}
	}
	if first != "triage" {
		t.Fatalf("reverse map for todo = %q, want triage (first in LinearStateTypes order)", first)
	}
}

func TestLinearUnauthorizedMarksInstallationBrokenOnce(t *testing.T) {
	bridge, store, api, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	link := store.issueLinks[0]

	alerts := 0
	bridge.OnInstallationBroken = func(context.Context, db.LinearInstallation, string) { alerts++ }
	api.commentErr = ErrUnauthorized

	if err := bridge.PushComment(ctx, link.IssueID, newUUID(), "hello"); err == nil {
		t.Fatal("push with a rejected token returned nil, want the error surfaced")
	}
	broken, err := store.GetLinearInstallation(ctx, inst.ID)
	if err != nil {
		t.Fatalf("reload installation: %v", err)
	}
	if broken.Status != "broken" {
		t.Fatalf("installation status = %q after a 401, want broken", broken.Status)
	}
	if got := store.issueLinks[0].SyncState; got != "broken" {
		t.Fatalf("link sync_state = %q after a 401, want broken", got)
	}
	if alerts != 1 {
		t.Fatalf("filed %d inbox alerts, want 1", alerts)
	}

	// Still broken, so a second failure must not file a second alert — a dead
	// token fails on every push, and an item per failure is a reason to mute
	// the inbox rather than fix it.
	if err := bridge.PushComment(ctx, link.IssueID, newUUID(), "hello again"); err == nil {
		t.Fatal("second push returned nil, want the error surfaced")
	}
	if alerts != 1 {
		t.Fatalf("filed %d inbox alerts across two failures, want 1", alerts)
	}
}

func TestLinearTransientFailureLeavesInstallationActive(t *testing.T) {
	bridge, store, api, _, inst := newTestBridge(t)
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	api.commentErr = &APIError{Status: 500, Message: "upstream"}
	if err := bridge.PushComment(ctx, store.issueLinks[0].IssueID, newUUID(), "hello"); err == nil {
		t.Fatal("push returned nil on a 500, want the error surfaced")
	}
	reloaded, _ := store.GetLinearInstallation(ctx, inst.ID)
	if reloaded.Status != "active" {
		t.Fatalf("installation status = %q after a 500, want it left active (only a rejected token is terminal)", reloaded.Status)
	}
	if got := store.issueLinks[0].LastError; got == "" {
		t.Fatal("link last_error is empty after a failed push, want the cause recorded")
	}
}

func TestLinearRunOutcomeCommentCarriesCostAndLink(t *testing.T) {
	bridge, store, api, _, inst := newTestBridge(t)
	bridge.AppURL = "https://app.example.com"
	ctx := context.Background()
	if err := bridge.HandleIssueEvent(ctx, inst, issueEvent(t, "create", "user_app", "unstarted", "Ours")); err != nil {
		t.Fatalf("create: %v", err)
	}
	issueID := store.issueLinks[0].IssueID
	if err := bridge.PushRunOutcome(ctx, issueID, true, "$0.1234"); err != nil {
		t.Fatalf("push outcome: %v", err)
	}
	if len(api.comments) != 1 {
		t.Fatalf("posted %d comments, want 1", len(api.comments))
	}
	body := api.comments[0].Body
	for _, want := range []string{"completed", "$0.1234", "https://app.example.com/issues/"} {
		if !strings.Contains(body, want) {
			t.Fatalf("outcome comment %q is missing %q", body, want)
		}
	}
}

func TestLinearStatusMapFallsBackOnCorruptStored(t *testing.T) {
	inst := db.LinearInstallation{StatusMap: []byte("not json")}
	if got := StatusMap(inst)["started"]; got != issuestatus.InProgress {
		t.Fatalf("status map from corrupt JSON gave %q for started, want the default %q", got, issuestatus.InProgress)
	}
	// A partial map keeps the defaults for everything it does not name, so one
	// customized row cannot silently stop status syncing altogether.
	inst.StatusMap = []byte(`{"started":"in_review"}`)
	m := StatusMap(inst)
	if m["started"] != "in_review" {
		t.Fatalf("stored override lost: started -> %q", m["started"])
	}
	if m["completed"] != issuestatus.Done {
		t.Fatalf("default lost for an unnamed key: completed -> %q", m["completed"])
	}
}

// ---------------------------------------------------------------------------
// client.go against a real HTTP server
// ---------------------------------------------------------------------------

func TestLinearClientAgainstGraphQLServer(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Errorf("Authorization = %q, want tok", got)
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastQuery = body.Query
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(lastQuery, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"id":"u1","name":"Multica"},"organization":{"id":"o1","name":"Acme"}}}`)
		case strings.Contains(lastQuery, "commentCreate"):
			fmt.Fprint(w, `{"data":{"commentCreate":{"success":true,"comment":{"id":"c9"}}}}`)
		case strings.Contains(lastQuery, "states"):
			fmt.Fprint(w, `{"data":{"team":{"states":{"nodes":[{"id":"s1","name":"Done","type":"completed","position":3}]}}}}`)
		default:
			fmt.Fprint(w, `{"data":{}}`)
		}
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), Endpoint: srv.URL, Token: "tok"}
	ctx := context.Background()

	viewer, err := c.Viewer(ctx)
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	if viewer.ID != "u1" || viewer.OrganizationID != "o1" || viewer.OrganizationName != "Acme" {
		t.Fatalf("viewer = %+v", viewer)
	}
	id, err := c.CreateComment(ctx, "iss_1", "hello")
	if err != nil || id != "c9" {
		t.Fatalf("CreateComment = %q, %v", id, err)
	}
	states, err := c.ListTeamStates(ctx, "team_1")
	if err != nil || len(states) != 1 || states[0].Type != "completed" {
		t.Fatalf("ListTeamStates = %+v, %v", states, err)
	}
}

func TestLinearClientMapsAuthFailuresToErrUnauthorized(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"http 401", http.StatusUnauthorized, `{}`},
		{"http 403", http.StatusForbidden, `{}`},
		// Linear answers an expired token with 200 + a GraphQL error, so the
		// status code alone is not enough to notice a dead installation.
		{"graphql authentication error", http.StatusOK,
			`{"errors":[{"message":"Authentication required","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := &Client{HTTP: srv.Client(), Endpoint: srv.URL, Token: "tok"}
			if _, err := c.Viewer(context.Background()); err != ErrUnauthorized {
				t.Fatalf("Viewer error = %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestLinearOAuthStateRoundTripAndRejections(t *testing.T) {
	secret := []byte("state-key")
	now := time.Now()
	claims := StateClaims{WorkspaceID: "ws", UserID: "u", AgentID: "a", Redirect: "/settings/integrations", Exp: now.Add(time.Minute).Unix()}
	token, err := SignState(secret, claims)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := VerifyState(secret, token, now)
	if err != nil || got.WorkspaceID != "ws" || got.AgentID != "a" {
		t.Fatalf("VerifyState = %+v, %v", got, err)
	}
	if _, err := VerifyState([]byte("other"), token, now); err != ErrStateSignature {
		t.Fatalf("wrong secret: got %v, want ErrStateSignature", err)
	}
	if _, err := VerifyState(secret, token, now.Add(2*time.Minute)); err != ErrStateExpired {
		t.Fatalf("expired: got %v, want ErrStateExpired", err)
	}
	if _, err := VerifyState(secret, "garbage", now); err != ErrStateMalformed {
		t.Fatalf("malformed: got %v, want ErrStateMalformed", err)
	}
}

func TestLinearAuthorizeURLCarriesActorApp(t *testing.T) {
	cfg := OAuthConfig{ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://api.example.com/cb"}
	got, err := cfg.AuthorizeURLFor("st")
	if err != nil {
		t.Fatalf("AuthorizeURLFor: %v", err)
	}
	// actor=app is what gives the bridge its own Linear identity; without it
	// the token acts as the installing human and nothing can be assigned to it.
	for _, want := range []string{"client_id=cid", "actor=app", "state=st", "issues%3Acreate"} {
		if !strings.Contains(got, want) {
			t.Fatalf("authorize url %q missing %q", got, want)
		}
	}
	if _, err := (OAuthConfig{}).AuthorizeURLFor("st"); err != ErrOAuthNotConfigured {
		t.Fatalf("unconfigured: got %v, want ErrOAuthNotConfigured", err)
	}
}
