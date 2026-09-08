package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	openai "github.com/openai/openai-go/v3"
)

// scriptedNativeLLM plays a fixed sequence of model turns so the loop's
// behaviour — not the model's — is what's under test.
type scriptedNativeLLM struct {
	turns []openai.ChatCompletion
	calls int
}

func (f *scriptedNativeLLM) Enabled() bool { return true }

func (f *scriptedNativeLLM) Chat(_ context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	// The brief must always ride along; a run without workspace context is
	// the first thing a regression would break.
	if len(params.Messages) < 2 {
		return nil, errors.New("expected at least system + user messages")
	}
	if f.calls >= len(f.turns) {
		return nil, errors.New("script exhausted: the loop kept calling after the final answer")
	}
	c := f.turns[f.calls]
	f.calls++
	return &c, nil
}

func nativeToolCallTurn(callID, name, args string) openai.ChatCompletion {
	return openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID:   callID,
					Type: "function",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{
						Name:      name,
						Arguments: args,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}
}

func nativeTextTurn(text string) openai.ChatCompletion {
	return openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: "assistant", Content: text},
			FinishReason: "stop",
		}},
	}
}

// The end-to-end native run: a claimable task on a native runtime executes a
// tool call, journals the transcript, authors its comment as the agent, and
// settles the task completed with a summary.
func TestNativeAgentRunExecutesToolAndCompletes(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Summarise and comment")
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{
		nativeToolCallTurn("call_1", "add_comment", `{"content":"Voilà le résumé."}`),
		nativeTextTurn("Commentaire ajouté."),
	}}
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())

	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("task was not claimable on the native runtime")
	}
	if util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), taskID)
	}
	svc.runTask(ctx, *claimed)

	// The comment landed under the agent's authorship, attributed to the run.
	var commentCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
		  AND content = 'Voilà le résumé.' AND source_task_id = $3
	`, issueID, agentID, taskID).Scan(&commentCount); err != nil {
		t.Fatalf("count agent comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("agent comments = %d, want 1", commentCount)
	}

	// The transcript carries the tool call, its result, and the final answer.
	messages, err := db.New(pool).ListTaskMessages(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list task messages: %v", err)
	}
	wantTypes := []string{"tool_use", "tool_result", "text"}
	if len(messages) != len(wantTypes) {
		t.Fatalf("transcript = %v, want %v", messageTypes(messages), wantTypes)
	}
	for i, want := range wantTypes {
		if messages[i].Type != want {
			t.Fatalf("transcript = %v, want %v", messageTypes(messages), wantTypes)
		}
	}
	if got := messages[0].Tool.String; got != "add_comment" {
		t.Fatalf("tool_use tool = %q, want add_comment", got)
	}
	if got := messages[2].Content.String; got != "Commentaire ajouté." {
		t.Fatalf("final text = %q, want the closing answer", got)
	}

	// The task settled completed with the summary the model gave.
	var status string
	var result []byte
	if err := pool.QueryRow(ctx, `SELECT status, result FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &result); err != nil {
		t.Fatalf("read task outcome: %v", err)
	}
	if status != "completed" {
		t.Fatalf("task status = %q, want completed", status)
	}
	var decoded struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("result is not the expected JSON: %v (%s)", err, string(result))
	}
	if decoded.Summary != "Commentaire ajouté." {
		t.Fatalf("result summary = %q, want the final answer", decoded.Summary)
	}
}

// A task carrying none of the four brief kinds is refused explicitly rather
// than left queued forever — the honest state for work the native runtime
// cannot brief.
func TestNativeAgentRefusesNonIssueTask(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(pool), tasks, issues, &scriptedNativeLLM{}, events.New())

	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("task was not claimable on the native runtime")
	}
	svc.runTask(ctx, *claimed)

	var status string
	var failure string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(error, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &failure); err != nil {
		t.Fatalf("read task outcome: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !strings.Contains(failure, "unsupported task kind") {
		t.Fatalf("failure = %q, want it to name the unsupported kind", failure)
	}
}

// A model that never stops calling tools is bounded by the turn limit, and
// the run still settles with a visible outcome instead of looping.
func TestNativeAgentStopsAtTurnLimit(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Loop forever")
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	turns := make([]openai.ChatCompletion, nativeMaxTurns)
	for i := range turns {
		turns[i] = nativeToolCallTurn(fmt.Sprintf("call_%d", i), "get_issue", `{}`)
	}
	llm := &scriptedNativeLLM{turns: turns}
	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())

	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("task was not claimable on the native runtime")
	}
	svc.runTask(ctx, *claimed)

	if llm.calls != nativeMaxTurns {
		t.Fatalf("model calls = %d, want exactly the turn limit %d", llm.calls, nativeMaxTurns)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("task status = %q, want completed with the bounded-stop summary", status)
	}
}

func messageTypes(messages []db.TaskMessage) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.Type)
	}
	return out
}

// transition_issue must behave exactly like the gated HTTP path: a rule that
// does not grant the agent refuses the move, a rule requiring approval files
// a request (audit + approver inbox) and leaves the status untouched, and an
// allowing rule applies it.
func TestNativeAgentTransitionRespectsF28Rules(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Gated move", testutil.Cols{"status": "todo"})
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(pool), tasks, issues, &scriptedNativeLLM{}, events.New())

	agentUUID := util.MustParseUUID(agentID)
	issueUUID := util.MustParseUUID(issueID)
	agent, err := db.New(pool).GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	issue, err := db.New(pool).GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: agent.WorkspaceID})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	tctx := nativeToolContext{task: db.AgentTaskQueue{ID: util.MustParseUUID(taskID)}, agent: agent, issue: &issue, workspaceID: agent.WorkspaceID}

	// Case 1: a rule that grants only owners refuses the agent outright.
	fx.Insert(t, "issue_transition_rule", testutil.Cols{
		"workspace_id":      ws,
		"to_category":       "done",
		"allowed_roles":     testutil.Raw(`ARRAY['owner']::text[]`),
		"allow_actor_types": testutil.Raw(`ARRAY[]::text[]`),
		"approver_roles":    testutil.Raw(`ARRAY[]::text[]`),
		"created_by":        user,
	})
	_, err = svc.callNativeTool(ctx, tctx, "transition_issue", map[string]any{"status": "done"})
	if err == nil {
		t.Fatal("transition to done was allowed by an owner-only rule")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("status = %q after a refused move, want todo", status)
	}

	// Case 2: an agent-granting rule that requires approval files the request
	// and holds the write.
	fx.Exec(t, `UPDATE issue_transition_rule SET allowed_roles = ARRAY[]::text[], allow_actor_types = ARRAY['agent']::text[], requires_approval = TRUE, approver_roles = ARRAY['owner']::text[]`)
	out, err := svc.callNativeTool(ctx, tctx, "transition_issue", map[string]any{"status": "done"})
	if err != nil {
		t.Fatalf("held transition errored instead of filing a request: %v", err)
	}
	held, _ := out.(map[string]any)
	if held == nil || held["held"] != true {
		t.Fatalf("transition result = %v, want a held request", out)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("status = %q after a held move, want todo until approval", status)
	}
	var requests, inbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_transition_request WHERE issue_id = $1 AND state = 'pending'`, issueID).Scan(&requests); err != nil {
		t.Fatalf("count requests: %v", err)
	}
	if requests != 1 {
		t.Fatalf("pending requests = %d, want 1", requests)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = 'transition_approval_requested'`, issueID).Scan(&inbox); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if inbox != 1 {
		t.Fatalf("approver inbox items = %d, want 1 for the owner", inbox)
	}

	// Case 3: the same rule without approval applies the move.
	fx.Exec(t, `UPDATE issue_transition_rule SET requires_approval = FALSE`)
	fx.Exec(t, `DELETE FROM issue_transition_request WHERE issue_id = $1`, issueID)
	out, err = svc.callNativeTool(ctx, tctx, "transition_issue", map[string]any{"status": "done"})
	if err != nil {
		t.Fatalf("allowed transition errored: %v", err)
	}
	applied, _ := out.(map[string]any)
	if applied == nil || applied["changed"] != true {
		t.Fatalf("transition result = %v, want applied", out)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q after an allowed move, want done", status)
	}
}

// The quick-create kind: a task whose context JSON carries the prompt runs
// the loop with a quick-create brief, files the issue through create_issue,
// and settles completed. The issue is assigned to the agent, so the follow-up
// run on the new issue is queued behind this one.
func TestNativeAgentQuickCreateRun(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	contextJSON := fmt.Sprintf(`{"type":"quick_create","prompt":"Créer une issue pour préparer la démo","workspace_id":"%s","requester_id":"%s"}`, ws, user)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "context": testutil.Raw("'" + contextJSON + "'::jsonb")})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{
		nativeToolCallTurn("call_1", "create_issue", `{"title":"Préparer la démo","description":"Préparation de la démo","priority":"medium"}`),
		nativeTextTurn("Issue créée et assignée."),
	}}
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())

	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("task was not claimable on the native runtime")
	}
	svc.runTask(ctx, *claimed)

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("task status = %q, want completed", status)
	}
	var title, issueStatus string
	var assigneeID string
	if err := pool.QueryRow(ctx, `SELECT title, status, assignee_id::text FROM issue WHERE creator_type = 'agent' AND creator_id = $1 ORDER BY created_at DESC LIMIT 1`, agentID).Scan(&title, &issueStatus, &assigneeID); err != nil {
		t.Fatalf("read agent-created issue: %v", err)
	}
	if title != "Préparer la démo" || issueStatus != "todo" || assigneeID != agentID {
		t.Fatalf("created issue = (%q, %q, %s), want the scripted issue assigned to the agent in todo", title, issueStatus, assigneeID)
	}
}

// internal/handler/issue_transition_writers_test.go classifies this file
// "gated" (transition_issue runs the shared gate); update_issue's pin claim
// is still worth proving against the database: the model asks for a status
// change outright, and the row must ignore it while still applying the edits
// the tool does own. If that ever stops holding, a native agent can move an
// issue without meeting the F28 transition gate. (Adapted from PR #181 to
// the lot B service signature.)
func TestNativeUpdateIssueCannotMoveStatus(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-pin-%d", suffix), fmt.Sprintf("native-pin-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-pin-ws-%d", suffix), fmt.Sprintf("native-pin-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Pinned status")
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{
		nativeToolCallTurn("call_1", "update_issue",
			`{"title":"Retitled by the agent","priority":"high","status":"done"}`),
		nativeTextTurn("Fait."),
	}}
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())

	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed == nil {
		t.Fatal("task was not claimable on the native runtime")
	}
	if util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), taskID)
	}
	svc.runTask(ctx, *claimed)

	var status, title, priority string
	if err := pool.QueryRow(ctx,
		`SELECT status, title, priority FROM issue WHERE id = $1`, issueID,
	).Scan(&status, &title, &priority); err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if status != "todo" {
		t.Errorf("status = %q, want todo — update_issue moved a status without the F28 gate", status)
	}
	// The edits the tool does own still landed, so a green test means the pin
	// held, not that the whole call failed.
	if title != "Retitled by the agent" {
		t.Errorf("title = %q, want the agent's new title", title)
	}
	if priority != "high" {
		t.Errorf("priority = %q, want high", priority)
	}
}
