package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/push"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/slack-go/slack"
)

// K64/K63 debts: a device registers its Expo token; a briefing or a new
// Decision Card pushes to every device with the badge (my unanswered
// decisions); a dead token is dropped; a Slack digest button answers the
// card through the same path as the web button, only for a bound member.

type expoStub struct {
	srv      *httptest.Server
	received [][]push.Message
	fail     map[string]string
}

func newExpoStub(t *testing.T) *expoStub {
	t.Helper()
	s := &expoStub{fail: map[string]string{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msgs []push.Message
		_ = json.NewDecoder(r.Body).Decode(&msgs)
		s.received = append(s.received, msgs)
		tickets := make([]map[string]any, 0, len(msgs))
		for _, m := range msgs {
			if e, bad := s.fail[m.To]; bad {
				tickets = append(tickets, map[string]any{"status": "error", "message": e, "details": map[string]any{"error": e}})
			} else {
				tickets = append(tickets, map[string]any{"status": "ok", "id": "t-" + m.To})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": tickets})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func TestPushTokensAndBadges(t *testing.T) {
	stub := newExpoStub(t)
	prev := testHandler.Push
	testHandler.Push = push.NewExpoSender(stub.srv.URL)
	t.Cleanup(func() {
		testHandler.Push = prev
		testPool.Exec(context.Background(), `DELETE FROM mobile_push_token WHERE user_id = $1`, testUserID)
	})
	testutil.Call(t, testHandler.RegisterPushToken, newRequest(http.MethodPut, "/api/me/push-token", map[string]any{"token": "not-an-expo-token"})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.RegisterPushToken, newRequest(http.MethodPut, "/api/me/push-token", map[string]any{"token": "ExponentPushToken[abc]", "platform": "ios"})).Want(http.StatusOK)
	testutil.Call(t, testHandler.RegisterPushToken, newRequest(http.MethodPut, "/api/me/push-token", map[string]any{"token": "ExponentPushToken[abc]", "platform": "ios"})).Want(http.StatusOK)
	testutil.Call(t, testHandler.RegisterPushToken, newRequest(http.MethodPut, "/api/me/push-token", map[string]any{"token": "ExponentPushToken[dead]", "platform": "android"})).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM mobile_push_token WHERE user_id = $1`, testUserID); n != 2 {
		t.Fatalf("tokens = %d, want 2 (upsert)", n)
	}
	// A new Decision Card pushes to the managers with the badge = their unanswered cards.
	stub.fail["ExponentPushToken[dead]"] = "DeviceNotRegistered"
	issue := dbfx.Issue(t, "push decision issue")
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue) })
	if len(stub.received) == 0 {
		t.Fatal("asking a decision must push")
	}
	batch := stub.received[len(stub.received)-1]
	var mine *push.Message
	for i := range batch {
		if batch[i].To == "ExponentPushToken[abc]" {
			mine = &batch[i]
		}
	}
	if mine == nil || mine.Badge == nil || *mine.Badge < 1 || !strings.HasPrefix(mine.Title, "Decision needed") || mine.Data["decision_id"] != created.Decision.ID {
		t.Fatalf("decision push = %+v", batch)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM mobile_push_token WHERE token = 'ExponentPushToken[dead]'`); n != 0 {
		t.Fatal("a token Expo reports unregistered must be dropped")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND workspace_id = $2 AND details->>'title' LIKE 'Decision needed%'`, AuditPushSent, testWorkspaceID); n < 1 {
		t.Fatal("push must be audited")
	}
	testutil.Call(t, testHandler.UnregisterPushToken, newRequest(http.MethodDelete, "/api/me/push-token", map[string]any{"token": "ExponentPushToken[abc]"})).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM mobile_push_token WHERE user_id = $1`, testUserID); n != 0 {
		t.Fatalf("tokens after unregister = %d", n)
	}
}

func TestSlackDigestButtonAnswersDecision(t *testing.T) {
	agent := dbfx.Agent(t, "digest button agent", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "digest button issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	inst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "channel_type": "slack", "config": `{"app_id":"A-DIGEST"}`, "status": "active", "installer_user_id": testUserID})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM channel_user_binding WHERE installation_id = $1`, inst)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, inst)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	var replies []string
	actions := &SlackDigestActions{H: testHandler, Reply: func(_ string, text string) error { replies = append(replies, text); return nil }}
	click := func(userID, value string) string {
		cb := slack.InteractionCallback{ResponseURL: "https://hooks.example.test/r", User: slack.User{ID: userID}}
		cb.ActionCallback.BlockActions = []*slack.BlockAction{{ActionID: "multica_digest", Value: value}}
		actions.HandleInteraction(context.Background(), "A-DIGEST", cb)
		return replies[len(replies)-1]
	}
	value := "decide|" + issue + "|" + created.Decision.ID + "|keep"
	if got := click("U-unbound", value); !strings.Contains(got, "Link your Slack account") {
		t.Fatalf("unbound click = %q", got)
	}
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{"installation_id": inst, "workspace_id": testWorkspaceID, "multica_user_id": testUserID, "channel_type": "slack", "channel_user_id": "U-bound"})
	if got := click("U-bound", "decide|garbage"); !strings.Contains(got, "not valid") {
		t.Fatalf("garbage click = %q", got)
	}
	if got := click("U-bound", value); !strings.Contains(got, "Keep it for now") {
		t.Fatalf("bound click = %q", got)
	}
	var response string
	dbfx.QueryRow(t, `SELECT response::text FROM issue_decision WHERE id = $1`, created.Decision.ID).Scan(&response)
	if !strings.Contains(response, `"keep"`) {
		t.Fatalf("decision response = %s", response)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND handoff_note LIKE 'Decision on%'`, issue, agent); n != 1 {
		t.Fatalf("resume runs = %d", n)
	}
	if got := click("U-bound", value); got != "Already answered." {
		t.Fatalf("second click = %q", got)
	}
	// The digest buttons: the answered card is gone, the briefing link stays.
	acts := testHandler.briefingDigestActions(context.Background(), parseUUID(testWorkspaceID), MorningBriefingResponse{AwaitingReview: []BriefingItem{{IssueID: issue, Identifier: "X-1"}}}, "https://app/acme")
	for _, a := range acts {
		if a.Value == value {
			t.Fatal("an answered card must not keep its button")
		}
	}
	hasReview := false
	for _, a := range acts {
		if a.Label == "Review X-1" && strings.HasSuffix(a.URL, "/issues/"+issue) {
			hasReview = true
		}
	}
	if !hasReview || acts[len(acts)-1].URL != "https://app/acme/inbox?view=briefing" {
		t.Fatalf("actions = %+v", acts)
	}
}

var _ db.ChannelInstallation
