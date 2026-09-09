package service

import (
	"context"
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

// The estimate counts Latin text at a quarter token per character and
// everything else at one, so CJK is never under-counted.
func TestNativeTokenEstimate(t *testing.T) {
	if got := nativeTokenEstimate(strings.Repeat("a", 400)); got != 100 {
		t.Fatalf("400 ascii chars = %d tokens, want 100", got)
	}
	if got := nativeTokenEstimate("日本語のテキスト"); got != 8 {
		t.Fatalf("8 CJK chars = %d tokens, want 8", got)
	}
	if got := nativeTokenEstimate(""); got != 0 {
		t.Fatalf("empty = %d", got)
	}
}

// Microcompaction stubs the results of every turn but the most recent ones,
// keeps the message sequence well-formed (same count, same ids), and never
// runs twice on the same result.
func TestNativeContextMicrocompact(t *testing.T) {
	cx := newNativeContext("system", "brief")
	big := strings.Repeat("issue payload ", 600) // ~8 KB, ~2 k tokens
	for i := 0; i < 5; i++ {
		cx.addTurn(openai.AssistantMessage(fmt.Sprintf("turn %d", i)), fmt.Sprintf("turn %d", i), []nativeToolResult{{callID: fmt.Sprintf("call_%d", i), tool: "get_issue", content: big}})
	}
	before, msgCount := cx.tokens(), len(cx.messages())
	trimmed, saved := cx.microcompact()
	if trimmed != 3 || saved <= 0 {
		t.Fatalf("trimmed = %d saved = %d, want 3 older results trimmed", trimmed, saved)
	}
	if cx.tokens() >= before || before-cx.tokens() != saved {
		t.Fatalf("tokens before = %d after = %d saved = %d", before, cx.tokens(), saved)
	}
	if len(cx.messages()) != msgCount {
		t.Fatalf("message count changed: %d → %d", msgCount, len(cx.messages()))
	}
	for i, turn := range cx.turns {
		r := turn.results[0]
		wantCompacted := i < 3
		if r.compacted != wantCompacted {
			t.Fatalf("turn %d compacted = %v, want %v", i, r.compacted, wantCompacted)
		}
		if wantCompacted && (!strings.Contains(r.content, `"compacted":true`) || !strings.Contains(r.content, `"tool":"get_issue"`) || !strings.Contains(r.content, "issue payload")) {
			t.Fatalf("stub = %q", r.content)
		}
	}
	if again, _ := cx.microcompact(); again != 0 {
		t.Fatalf("second pass trimmed %d, want 0", again)
	}
	// Short results are left alone: a stub would not be smaller.
	cy := newNativeContext("system", "brief")
	for i := 0; i < 4; i++ {
		cy.addTurn(openai.AssistantMessage("t"), "t", []nativeToolResult{{callID: "c", tool: "get_issue", content: `{"id":"x"}`}})
	}
	if n, _ := cy.microcompact(); n != 0 {
		t.Fatalf("short results trimmed = %d, want 0", n)
	}
}

// Both stages in one pass, without a database: over the soft limit the
// older results are stubbed; still over the hard limit, the model is asked
// for a summary (no tools), the turns are dropped behind it, and the note
// names both.
func TestNativeCompactIfNeededRunsBothStages(t *testing.T) {
	cx := newNativeContext("system", "brief")
	big := strings.Repeat("issue payload ", 600)
	for i := 0; i < 3; i++ {
		cx.addTurn(openai.AssistantMessage(fmt.Sprintf("turn %d", i)), fmt.Sprintf("turn %d", i), []nativeToolResult{{callID: fmt.Sprintf("call_%d", i), tool: "get_issue", content: big}})
	}
	softWas, hardWas := nativeContextSoftTokens, nativeContextHardTokens
	t.Cleanup(func() { nativeContextSoftTokens, nativeContextHardTokens = softWas, hardWas })
	nativeContextSoftTokens = cx.tokens() - 1
	trial := *cx
	trial.turns = append([]nativeTurn(nil), cx.turns...)
	for i := range trial.turns {
		trial.turns[i].results = append([]nativeToolResult(nil), cx.turns[i].results...)
	}
	trial.microcompact()
	nativeContextHardTokens = trial.tokens() - 1

	llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{nativeTextTurn("Summary: read the issue three times; next: reply.")}}
	svc := &NativeAgentService{LLM: llm}
	var usage nativeRunUsage
	note := svc.nativeCompactIfNeeded(context.Background(), cx, &usage)
	if !strings.Contains(note, "Context compacted: 1 older tool result") || !strings.Contains(note, "Context summarized") {
		t.Fatalf("note = %q", note)
	}
	if llm.calls != 1 || len(llm.last.Tools) != 0 || !strings.Contains(nativeLastUserMessage(t, llm.last), "Your context is nearly full") {
		t.Fatalf("summary call: calls = %d tools = %d prompt = %q", llm.calls, len(llm.last.Tools), nativeLastUserMessage(t, llm.last))
	}
	if len(cx.turns) != 0 || !strings.Contains(cx.summary, "three times") {
		t.Fatalf("after summary: turns = %d summary = %q", len(cx.turns), cx.summary)
	}
	msgs := cx.messages()
	if len(msgs) != 3 || msgs[2].OfUser == nil || !strings.Contains(msgs[2].OfUser.Content.OfString.Value, "Summary of your earlier turns") {
		t.Fatalf("messages after summary = %d, want system + brief + note", len(msgs))
	}
	// A summary the model cannot produce leaves the context as it was.
	cy := newNativeContext("system", "brief")
	cy.addTurn(openai.AssistantMessage("t"), "t", []nativeToolResult{{callID: "c", tool: "get_issue", content: big}})
	nativeContextSoftTokens, nativeContextHardTokens = 1, 1
	failing := &NativeAgentService{LLM: &scriptedNativeLLM{fail: true}}
	note = failing.nativeCompactIfNeeded(context.Background(), cy, &usage)
	if !strings.Contains(note, "Context summary failed") || len(cy.turns) != 1 {
		t.Fatalf("failed summary: note = %q turns = %d", note, len(cy.turns))
	}
}

// The loop runs the compaction before each model call: with the hard limit
// under the brief itself, the first tool turn is followed by a summary call
// and the run continues from the note.
func TestNativeAgentSummarizesContextPastTheHardLimit(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, ws); err != nil {
		t.Fatal(err)
	}
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native"})
	agentID := fx.Agent(t, "Native reader", runtimeID)
	issueID := fx.Issue(t, "Long read", testutil.Cols{"description": strings.Repeat("A long description that the model keeps re-reading. ", 60)})
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{
		nativeToolCallTurn("call_1", "get_issue", `{}`),
		// The summary the loop asks for once the hard limit is crossed.
		nativeTextTurn("Summary: read the issue once; next: reply."),
		nativeTextTurn("Done reading. Nothing to change."),
	}}
	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())
	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	// Limits under the brief so the first turn after a tool call summarizes;
	// the very first call sees no turns, so nothing to summarize yet.
	softWas, hardWas := nativeContextSoftTokens, nativeContextHardTokens
	t.Cleanup(func() { nativeContextSoftTokens, nativeContextHardTokens = softWas, hardWas })
	nativeContextSoftTokens, nativeContextHardTokens = 1, 1
	svc.runTask(ctx, *claimed)

	if llm.calls != 3 {
		t.Fatalf("model calls = %d, want tool turn + summary + final", llm.calls)
	}
	var sawSummary, sawTool bool
	for _, m := range llm.last.Messages {
		if m.OfUser != nil && strings.Contains(m.OfUser.Content.OfString.Value, "Summary of your earlier turns") && strings.Contains(m.OfUser.Content.OfString.Value, "read the issue once") {
			sawSummary = true
		}
		if m.OfTool != nil {
			sawTool = true
		}
	}
	if !sawSummary || sawTool {
		t.Fatalf("final call: summary note = %v, tool messages = %v; want the note and no tool messages", sawSummary, sawTool)
	}
	if len(llm.last.Tools) == 0 {
		t.Fatal("the run must keep its tools after a summary")
	}
	var lines []string
	rows, err := pool.Query(ctx, `SELECT content FROM task_message WHERE task_id = $1 AND type = 'system' ORDER BY seq`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, c)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "Context summarized") {
		t.Fatalf("transcript system lines = %q, want a summary line", lines)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("task status = %q (%v)", status, err)
	}
}
