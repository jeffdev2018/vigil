package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Inline approvals in chat: one button payload every platform can carry, one
// decide core behind every platform's click, and the ask posted to the chats
// when it is filed then rewritten when it settles.

func TestApprovalValueRoundTrip(t *testing.T) {
	issue, ask := parseUUID("11111111-1111-4111-8111-111111111111"), parseUUID("22222222-2222-4222-8222-222222222222")
	for _, source := range []string{ApprovalSourceDecision, ApprovalSourceTransition, ApprovalSourceGoalQuestion} {
		value := encodeApprovalValue(source, issue, ask, 3)
		// Telegram refuses callback_data over 64 bytes, which is the whole
		// reason the UUIDs are packed rather than spelled out.
		if len(value) > 64 {
			t.Fatalf("%s payload is %d bytes: %q", source, len(value), value)
		}
		click, ok := decodeApprovalValue(value)
		if !ok || click.Source != source || uuidToString(click.IssueID) != uuidToString(issue) || uuidToString(click.AskID) != uuidToString(ask) || click.Index != 3 {
			t.Fatalf("%s round trip = %+v (ok=%v)", source, click, ok)
		}
	}
	// The three-part payload sitting in Slack history still decides.
	legacy, ok := decodeApprovalValue("decide|" + uuidToString(issue) + "|" + uuidToString(ask) + "|keep")
	if !ok || legacy.Source != ApprovalSourceDecision || legacy.OptionID != "keep" || legacy.Index != -1 {
		t.Fatalf("legacy payload = %+v (ok=%v)", legacy, ok)
	}
	for _, bad := range []string{"", "nope", "decide|", "decide|garbage", "decide|z|AAAA|BBBB|0", "decide|d|not-base64!!|x|0", "decide|d|" + packUUID(issue) + "|" + packUUID(ask) + "|-1"} {
		if _, ok := decodeApprovalValue(bad); ok {
			t.Fatalf("%q must not decode", bad)
		}
	}
}

// approvalChatSender is a rich sender that can also edit what it posted: the
// Slack / Telegram / Lark shape. Delivery runs on its own goroutine, so every
// field is read and written under the mutex.
type approvalChatSender struct {
	mu      sync.Mutex
	posted  []string
	actions [][]channel.DigestAction
	updates []string
}

func (f *approvalChatSender) SendDigest(_ context.Context, _ db.ChannelInstallation, chatID, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, chatID+"|"+text)
	return "msg-1", nil
}

func (f *approvalChatSender) SendRichDigest(_ context.Context, _ db.ChannelInstallation, chatID, text string, actions []channel.DigestAction) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, chatID+"|"+text)
	f.actions = append(f.actions, actions)
	return "msg-1", nil
}

func (f *approvalChatSender) UpdateMessage(_ context.Context, _ db.ChannelInstallation, chatID, messageID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, chatID+"|"+messageID+"|"+text)
	return nil
}

func (f *approvalChatSender) snapshot() ([]string, [][]channel.DigestAction, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...), append([][]channel.DigestAction(nil), f.actions...), append([]string(nil), f.updates...)
}

// plainChatSender is a sender that cannot render buttons or edit: the
// DingTalk / WeCom shape, where an approval degrades to a deep link.
type plainChatSender struct {
	mu     sync.Mutex
	posted []string
}

func (f *plainChatSender) SendDigest(_ context.Context, _ db.ChannelInstallation, chatID, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, chatID+"|"+text)
	return "", nil
}

func (f *plainChatSender) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
}

// waitForChat polls until a best-effort chat delivery has landed. Posting and
// updating run on their own goroutine so no endpoint waits on a chat API;
// the test has to.
func waitForChat(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestApprovalAskPostedToChannelsAndUpdatedOnSettle(t *testing.T) {
	rememberSettings(t)
	rich, plain := &approvalChatSender{}, &plainChatSender{}
	prev := testHandler.DigestSenders
	testHandler.DigestSenders = map[string]ChannelDigestSender{"slack": rich, "wecom": plain}
	t.Cleanup(func() { testHandler.DigestSenders = prev })

	agent := dbfx.Agent(t, "approval post agent", handlerTestRuntimeID(t))
	slackInst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "channel_type": "slack", "config": `{"app_id":"A-POST"}`, "status": "active", "installer_user_id": testUserID})
	wecomInst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "channel_type": "wecom", "config": `{"app_id":"W-POST"}`, "status": "active", "installer_user_id": testUserID})
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"morning_briefing":{"enabled":false,"channels":[{"type":"slack","chat_id":"C-ask"},{"type":"wecom","chat_id":"W-ask"}]}}'::jsonb WHERE id = $1`, testWorkspaceID)
	issue := dbfx.Issue(t, "approval post issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM channel_approval_message WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = ANY($1)`, []string{slackInst, wecomInst})
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})

	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)

	// Filing the ask posts it, off the request's goroutine. The rich channel
	// gets buttons; the one that cannot render them gets the same words plus a
	// deep link.
	waitForChat(t, "the ask to reach both chats", func() bool {
		posted, _, _ := rich.snapshot()
		return len(posted) == 1 && len(plain.snapshot()) == 1
	})
	posted, actionSets, _ := rich.snapshot()
	if !strings.HasPrefix(posted[0], "C-ask|") || !strings.Contains(posted[0], "Decision needed") {
		t.Fatalf("rich post = %q", posted[0])
	}
	if plainPosts := plain.snapshot(); !strings.Contains(plainPosts[0], "Decide it here: ") || !strings.Contains(plainPosts[0], "/issues/"+issue) {
		t.Fatalf("plain post = %q", plainPosts[0])
	}
	acts := actionSets[0]
	if len(acts) < 2 || acts[len(acts)-1].URL == "" {
		t.Fatalf("actions = %+v", acts)
	}
	click, ok := decodeApprovalValue(acts[0].Value)
	if !ok || click.Source != ApprovalSourceDecision || uuidToString(click.AskID) != created.Decision.ID {
		t.Fatalf("first button = %+v (ok=%v)", click, ok)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM channel_approval_message WHERE ask_id = $1 AND settled = false`, created.Decision.ID); n != 2 {
		t.Fatalf("recorded posts = %d", n)
	}

	// Settling it rewrites the message that carried the buttons, and tells the
	// channel that cannot edit in a follow-up. Both say the option's LABEL,
	// not the option id the event payload carries.
	reply := testHandler.DecideApprovalFromChannel(context.Background(), "slack", "A-POST", "U-post", acts[0].Value)
	if !strings.Contains(reply, "Link your Slack account") {
		t.Fatalf("unbound click = %q", reply)
	}
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{"installation_id": slackInst, "workspace_id": testWorkspaceID, "multica_user_id": testUserID, "channel_type": "slack", "channel_user_id": "U-post"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1`, slackInst)
	})
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "slack", "A-POST", "U-post", acts[0].Value); !strings.Contains(reply, "Answered") {
		t.Fatalf("bound click = %q", reply)
	}
	waitForChat(t, "the settled ask to be rewritten", func() bool {
		_, _, updates := rich.snapshot()
		return len(updates) == 1 && len(plain.snapshot()) == 2
	})
	_, _, updates := rich.snapshot()
	// The words are the option's LABEL — the button the reader clicked — not
	// the option id the approval:decided payload carries.
	settled := "settled: " + acts[0].Label + "."
	if !strings.Contains(updates[0], "C-ask|msg-1|") || !strings.Contains(updates[0], settled) {
		t.Fatalf("update = %q, want %q", updates[0], settled)
	}
	if plainPosts := plain.snapshot(); !strings.Contains(plainPosts[1], settled) {
		t.Fatalf("plain follow-up = %q", plainPosts[1])
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM channel_approval_message WHERE ask_id = $1 AND settled = false`, created.Decision.ID); n != 0 {
		t.Fatalf("still-pending posts after settle = %d", n)
	}
	// A second settle does not re-post: the rows are marked.
	testHandler.settleApprovalMessages(context.Background(), parseUUID(testWorkspaceID), parseUUID(issue), ApprovalSourceDecision, created.Decision.ID, "keep")
	time.Sleep(200 * time.Millisecond)
	if _, _, u := rich.snapshot(); len(u) != 1 {
		t.Fatalf("settle is not idempotent: updates=%v", u)
	}
	if len(plain.snapshot()) != 2 {
		t.Fatalf("settle is not idempotent: plain posts=%v", plain.snapshot())
	}
}

func TestChannelButtonDecidesTransitionAndGoalQuestion(t *testing.T) {
	agent := dbfx.Agent(t, "approval sources agent", handlerTestRuntimeID(t))
	inst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "channel_type": "telegram", "config": `{"app_id":"77"}`, "status": "active", "installer_user_id": testUserID})
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{"installation_id": inst, "workspace_id": testWorkspaceID, "multica_user_id": testUserID, "channel_type": "telegram", "channel_user_id": "42"})
	issue := dbfx.Issue(t, "approval sources issue", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM issue_transition_request WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM issue_goal WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM channel_user_binding WHERE installation_id = $1`, inst)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, inst)
	})

	// A held transition: option 0 approves, and the issue actually moves.
	req := dbfx.Insert(t, "issue_transition_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issue, "from_status": "in_progress", "to_status": "done",
		"requested_by_type": "member", "requested_by_id": testUserID, "state": "pending",
	})

	// It reaches the chats at file time, not only in the morning digest: the
	// bus listener calls this with what approval:asked carried, and the ask is
	// loaded through the same feed builder GET /api/approvals uses.
	postedAsks := postApprovalsToFakeChat(t, func() {
		testHandler.PostApprovalToChannels(context.Background(), ApprovalSourceTransition, req, issue)
	}, 1)
	transitionActions := postedAsks[0]
	if len(transitionActions) != 3 {
		t.Fatalf("transition buttons = %+v", transitionActions)
	}
	if transitionActions[0].Label != "Approve" || transitionActions[1].Label != "Reject" || transitionActions[2].URL == "" {
		t.Fatalf("transition buttons = %+v", transitionActions)
	}
	if click, ok := decodeApprovalValue(transitionActions[1].Value); !ok || click.Source != ApprovalSourceTransition || click.Index != 1 || uuidToString(click.AskID) != req {
		t.Fatalf("reject button = %+v (ok=%v)", click, ok)
	}

	value := encodeApprovalValue(ApprovalSourceTransition, parseUUID(issue), parseUUID(req), 0)
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "77", "42", value); !strings.Contains(reply, "Approved") {
		t.Fatalf("transition click = %q", reply)
	}
	var state, status string
	dbfx.QueryRow(t, `SELECT state FROM issue_transition_request WHERE id = $1`, req).Scan(&state)
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if state != "approved" || status != "done" {
		t.Fatalf("after approve: state=%q status=%q", state, status)
	}
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "77", "42", value); reply != "Already decided." {
		t.Fatalf("second transition click = %q", reply)
	}

	// A goal-loop question: the option's own words become the answer.
	goal := dbfx.Insert(t, "issue_goal", testutil.Cols{
		"id": uuidToString(dbid.NewV7()), "workspace_id": testWorkspaceID, "issue_id": issue, "goal": "ship it", "status": "waiting_user",
		"question": `{"kind":"choice","prompt":"Which database?","options":["Postgres","SQLite"],"asked_at":"2026-09-09T00:00:00Z"}`,
	})
	goalAsks := postApprovalsToFakeChat(t, func() {
		testHandler.PostApprovalToChannels(context.Background(), ApprovalSourceGoalQuestion, goal, issue)
	}, 1)
	if opts := goalAsks[0]; len(opts) != 3 || opts[0].Label != "Postgres" || opts[1].Label != "SQLite" {
		t.Fatalf("goal question buttons = %+v", goalAsks[0])
	}

	goalValue := encodeApprovalValue(ApprovalSourceGoalQuestion, parseUUID(issue), parseUUID(goal), 1)
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "77", "42", goalValue); !strings.Contains(reply, "SQLite") {
		t.Fatalf("goal click = %q", reply)
	}
	var question string
	dbfx.QueryRow(t, `SELECT question::text FROM issue_goal WHERE id = $1`, goal).Scan(&question)
	if !strings.Contains(strings.ReplaceAll(question, " ", ""), `"answer":"SQLite"`) {
		t.Fatalf("goal question = %s", question)
	}
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "77", "42", goalValue); reply != "Already answered." {
		t.Fatalf("second goal click = %q", reply)
	}
	// An option that is not on the question is a sentence, not an error.
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "77", "42", encodeApprovalValue(ApprovalSourceGoalQuestion, parseUUID(issue), parseUUID(goal), 9)); !strings.Contains(reply, "Already answered") && !strings.Contains(reply, "not on the question") {
		t.Fatalf("out-of-range goal click = %q", reply)
	}
	// An unknown app id never reaches a workspace.
	if reply := testHandler.DecideApprovalFromChannel(context.Background(), "telegram", "nope", "42", goalValue); !strings.Contains(reply, "not connected") {
		t.Fatalf("unknown app click = %q", reply)
	}
}

// postApprovalsToFakeChat runs post while a fake chat channel is configured
// and returns the action sets it received, one per posted ask.
func postApprovalsToFakeChat(t *testing.T, post func(), want int) [][]channel.DigestAction {
	t.Helper()
	tag := uuid.NewString()[:8]
	rememberSettings(t)
	fake := &approvalChatSender{}
	prev := testHandler.DigestSenders
	testHandler.DigestSenders = map[string]ChannelDigestSender{"slack": fake}
	defer func() { testHandler.DigestSenders = prev }()
	inst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": dbfx.Agent(t, "ask poster agent "+tag, handlerTestRuntimeID(t)), "channel_type": "slack", "config": `{"app_id":"A-ASK-` + tag + `"}`, "status": "active", "installer_user_id": testUserID})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM channel_approval_message WHERE installation_id = $1`, inst)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, inst)
	})
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"morning_briefing":{"enabled":false,"channels":[{"type":"slack","chat_id":"C-ask"}]}}'::jsonb WHERE id = $1`, testWorkspaceID)
	post()
	waitForChat(t, "the ask to reach the chat", func() bool {
		_, actions, _ := fake.snapshot()
		return len(actions) >= want
	})
	_, actions, _ := fake.snapshot()
	return actions
}
