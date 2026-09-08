package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm)

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

// A task without an issue is refused explicitly rather than left queued
// forever — the honest state for kinds the native runtime does not run yet.
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
	svc := NewNativeAgentService(db.New(pool), tasks, issues, &scriptedNativeLLM{})

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
	if want := "native runtime only supports issue tasks"; len(failure) < len(want) || failure[:len(want)] != want {
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
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm)

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
