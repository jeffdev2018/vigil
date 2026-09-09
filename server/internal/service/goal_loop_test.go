package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/goalstate"
	"github.com/multica-ai/multica/server/pkg/protocol"
	openai "github.com/openai/openai-go/v3"
)

// The judge's reply is parsed leniently on shape and strictly on vocabulary.
func TestParseGoalJudgeAnswer(t *testing.T) {
	v, err := parseGoalJudgeAnswer("Sure:\n```json\n{\"satisfied\": false, \"blocker\": \"goal_not_met_yet\", \"reason\": \"tests missing\", \"evidence_summary\": \"wrote the judge\", \"next_step\": \"write tests\"}\n```")
	if err != nil || v.Satisfied || v.Blocker != "goal_not_met_yet" || v.Reason != "tests missing" || v.EvidenceSummary != "wrote the judge" || v.NextStep != "write tests" {
		t.Fatalf("fenced verdict = %+v, %v", v, err)
	}
	v, err = parseGoalJudgeAnswer(`{"satisfied": true, "blocker": "whatever", "reason": "all done", "next_step": "nothing"}`)
	if err != nil || !v.Satisfied || v.Blocker != "" || v.NextStep != "" {
		t.Fatalf("satisfied verdict should drop blocker and next step: %+v, %v", v, err)
	}
	if _, err := parseGoalJudgeAnswer(`{"satisfied": false, "blocker": "tired"}`); err == nil {
		t.Fatal("an unknown blocker must be refused")
	}
	if _, err := parseGoalJudgeAnswer("I think it is fine."); err == nil {
		t.Fatal("prose without JSON must be refused")
	}
	if goalSignature("Done:  A, B") != goalSignature("done: a, b") {
		t.Fatal("signature must ignore case and whitespace")
	}
}

func TestGoalLoopSettingsFrom(t *testing.T) {
	if s := GoalLoopSettingsFrom(nil); s.MaxContinuations != goalDefaultMaxContinuations || !s.ProposeDone {
		t.Fatalf("defaults = %+v", s)
	}
	if s := GoalLoopSettingsFrom([]byte(`{"goal_loop":{"max_continuations":0,"propose_done":false}}`)); s.MaxContinuations != 0 || s.ProposeDone {
		t.Fatalf("explicit off = %+v", s)
	}
	if s := GoalLoopSettingsFrom([]byte(`{"goal_loop":{"max_continuations":999}}`)); s.MaxContinuations != goalMaxMaxContinuations || !s.ProposeDone {
		t.Fatalf("clamp keeps propose_done default: %+v", s)
	}
	if closingStatusOf([]byte(`{"output":"cli said so"}`)) != "cli said so" || closingStatusOf([]byte(`{"summary":"native","output":"x"}`)) != "native" {
		t.Fatal("closing status must prefer summary, then output")
	}
}

type goalFixture struct {
	pool                                             *pgxpool.Pool
	fx                                               *testutil.Fixture
	workspaceID, userID, runtimeID, agentID, issueID string
	tasks                                            *TaskService
	goal                                             *GoalLoopService
	llm                                              *scriptedNativeLLM
	bus                                              *events.Bus
}

func newGoalFixture(t *testing.T, runtimeMode string) *goalFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("goal-owner-%d", suffix), fmt.Sprintf("goal-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("goal-ws-%d", suffix), fmt.Sprintf("goal-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	cols := testutil.Cols{"runtime_mode": "local", "provider": runtimeMode}
	if runtimeMode == "native" {
		cols["runtime_mode"] = "native"
		cols["daemon_id"] = "native"
	}
	runtimeID := fx.Runtime(t, runtimeMode, cols)
	agentID := fx.Agent(t, "Goal worker", runtimeID)
	issueID := fx.Issue(t, "Ship the goal loop", testutil.Cols{
		"description":   "Judge, continuation, state.",
		"assignee_type": "agent",
		"assignee_id":   agentID,
		"status":        "in_progress",
	})
	bus := events.New()
	llm := &scriptedNativeLLM{}
	tasks := NewTaskService(db.New(pool), pool, nil, bus)
	goal := NewGoalLoopService(db.New(pool), tasks, llm, bus)
	return &goalFixture{pool: pool, fx: fx, workspaceID: ws, userID: user, runtimeID: runtimeID, agentID: agentID, issueID: issueID, tasks: tasks, goal: goal, llm: llm, bus: bus}
}

// runNative claims the task and drives one native run through the scripted
// model, judge included.
func (f *goalFixture) runNative(t *testing.T, taskID string, turns ...openai.ChatCompletion) {
	t.Helper()
	ctx := context.Background()
	f.llm.turns = turns
	f.llm.calls = 0
	issues := NewIssueService(db.New(f.pool), f.pool, f.bus, nil, f.tasks)
	svc := NewNativeAgentService(db.New(f.pool), f.tasks, issues, f.llm, f.bus)
	svc.Goal = f.goal
	claimed, err := f.tasks.claimTask(ctx, util.MustParseUUID(f.agentID), util.MustParseUUID(f.runtimeID), false)
	if err != nil || claimed == nil || util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claim task %s: got %v (%v)", taskID, claimed, err)
	}
	svc.runTask(ctx, *claimed)
}

func (f *goalFixture) newTask(t *testing.T, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return f.fx.Task(t, f.agentID, cols)
}

func (f *goalFixture) queued(t *testing.T) []db.AgentTaskQueue {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT id, leg_role, workflow_root_task_id, COALESCE(handoff_note, '') FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, f.issueID)
	if err != nil {
		t.Fatalf("list queued: %v", err)
	}
	defer rows.Close()
	var out []db.AgentTaskQueue
	for rows.Next() {
		var task db.AgentTaskQueue
		var note string
		if err := rows.Scan(&task.ID, &task.LegRole, &task.WorkflowRootTaskID, &note); err != nil {
			t.Fatalf("scan: %v", err)
		}
		task.HandoffNote.String, task.HandoffNote.Valid = note, true
		out = append(out, task)
	}
	return out
}

func (f *goalFixture) outcome(t *testing.T, taskID string) string {
	t.Helper()
	var out string
	if err := f.pool.QueryRow(context.Background(), `SELECT COALESCE(result->'goal_loop'->>'outcome', '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&out); err != nil {
		t.Fatalf("read outcome: %v", err)
	}
	return out
}

func (f *goalFixture) row(t *testing.T) db.IssueGoal {
	t.Helper()
	goal, err := db.New(f.pool).GetIssueGoal(context.Background(), db.GetIssueGoalParams{IssueID: util.MustParseUUID(f.issueID), WorkspaceID: util.MustParseUUID(f.workspaceID)})
	if err != nil {
		t.Fatalf("goal row: %v", err)
	}
	return goal
}

func (f *goalFixture) comments(t *testing.T, authorType string) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT content FROM comment WHERE issue_id = $1 AND author_type = $2 ORDER BY created_at, id`, f.issueID, authorType)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	return out
}

func (f *goalFixture) issue(t *testing.T) db.Issue {
	t.Helper()
	issue, err := db.New(f.pool).GetIssue(context.Background(), util.MustParseUUID(f.issueID))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return issue
}

func judgeTurn(satisfied bool, blocker, reason string) openai.ChatCompletion {
	return nativeTextTurn(fmt.Sprintf(`{"satisfied": %t, "blocker": %q, "reason": %q, "evidence_summary": "evidence for %s", "next_step": "next after %s"}`, satisfied, blocker, reason, reason, reason))
}

// Not met: the next run is queued as a continuation leg of the judged run,
// with a handoff note; the goal row keeps the chain state and the evidence.
func TestGoalLoopContinuesAsLeg(t *testing.T) {
	f := newGoalFixture(t, "native")
	first := f.newTask(t)
	f.runNative(t, first, nativeTextTurn("Wrote the judge. Continuation and state remain."), judgeTurn(false, "goal_not_met_yet", "two parts remain"))
	if f.llm.calls != 2 || len(f.llm.last.Tools) != 0 {
		t.Fatalf("model calls = %d, tools on last = %d; want run + tool-less judge", f.llm.calls, len(f.llm.last.Tools))
	}
	if got := f.outcome(t, first); got != "continued" {
		t.Fatalf("outcome = %q, want continued (reason: %s)", got, f.row(t).LastReason)
	}
	queued := f.queued(t)
	if len(queued) != 1 || queued[0].LegRole != LegRoleContinuation || util.UUIDToString(queued[0].WorkflowRootTaskID) != first {
		t.Fatalf("queued = %+v, want one continuation leg rooted at the first run", queued)
	}
	if note := queued[0].HandoffNote.String; !strings.HasPrefix(note, "Continuation 1/8") || !strings.Contains(note, "two parts remain") || !strings.Contains(note, "next after") {
		t.Fatalf("handoff note = %q", note)
	}
	row := f.row(t)
	if row.Status != GoalStatusActive || row.Continuation != 0 || row.LastOutcome != "continued" || row.NextStep == "" || !strings.Contains(string(row.Evidence), "evidence for") {
		t.Fatalf("goal row = %+v", row)
	}
	if c := f.comments(t, "agent"); len(c) != 0 {
		t.Fatalf("a continued loop must not comment, got %q", c)
	}

	// The continuation run: the brief carries the goal state, the verdict
	// counts it as continuation 1, the evidence accumulates.
	f.runNative(t, util.UUIDToString(queued[0].ID), nativeTextTurn("Wrote the tests. Docs remain."), judgeTurn(false, "goal_not_met_yet", "docs remain"))
	brief := f.llm.first.Messages[1].OfUser.Content.OfString.Value
	if !strings.Contains(brief, "Goal state for this issue") || !strings.Contains(brief, "continuation 0 of at most 8") || !strings.Contains(brief, "evidence for") {
		t.Fatalf("continuation brief lacks the goal state: %q", brief)
	}
	row = f.row(t)
	if row.Continuation != 1 || row.LastOutcome != "continued" || strings.Count(string(row.Evidence), "evidence for") != 2 {
		t.Fatalf("goal row after continuation = %+v", row)
	}
	if q := f.queued(t); len(q) != 1 || !strings.HasPrefix(q[0].HandoffNote.String, "Continuation 2/8") || util.UUIDToString(q[0].WorkflowRootTaskID) != first {
		t.Fatalf("second continuation = %+v", q)
	}
}

// Met: the issue moves to done through the transition gate (no rule here:
// applied), the row is satisfied, the team is told.
func TestGoalLoopSatisfiedProposesDone(t *testing.T) {
	f := newGoalFixture(t, "native")
	task := f.newTask(t)
	f.runNative(t, task, nativeTextTurn("All three parts shipped and tested."), judgeTurn(true, "", "everything is there"))
	if got := f.outcome(t, task); got != "satisfied" {
		t.Fatalf("outcome = %q", got)
	}
	if issue := f.issue(t); issue.Status != "done" {
		t.Fatalf("issue status = %q, want done", issue.Status)
	}
	if row := f.row(t); row.Status != GoalStatusSatisfied {
		t.Fatalf("goal status = %q", row.Status)
	}
	if c := f.comments(t, "agent"); len(c) != 1 || !strings.Contains(c[0], "moved to done") {
		t.Fatalf("done comment = %q", c)
	}
	if q := f.queued(t); len(q) != 0 {
		t.Fatalf("satisfied must not queue, got %+v", q)
	}

	// propose_done off: the verdict is recorded, the issue is left alone.
	g := newGoalFixture(t, "native")
	if _, err := g.pool.Exec(context.Background(), `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"propose_done":false}}'::jsonb WHERE id = $1`, g.workspaceID); err != nil {
		t.Fatal(err)
	}
	task = g.newTask(t)
	g.runNative(t, task, nativeTextTurn("Done."), judgeTurn(true, "", "done"))
	if issue := g.issue(t); issue.Status != "in_progress" {
		t.Fatalf("issue status = %q, want untouched", issue.Status)
	}
	if row := g.row(t); row.Status != GoalStatusSatisfied {
		t.Fatalf("goal status = %q", row.Status)
	}
}

// ask_user: the run stops, the team gets an inbox item and a comment with
// the options, the chain waits; the answer queues the next run with it.
func TestGoalLoopQuestionAndAnswer(t *testing.T) {
	f := newGoalFixture(t, "native")
	ctx := context.Background()
	task := f.newTask(t)
	f.runNative(t, task,
		nativeToolCallTurn("call_1", "ask_user", `{"question":"Which database?","kind":"choice","options":["Postgres","SQLite"]}`),
		nativeTextTurn("Asked the team which database to use; waiting."),
	)
	// tool call + wrap-up; no judge call: the question is the verdict.
	if f.llm.calls != 2 {
		t.Fatalf("model calls = %d, want tool turn + wrap-up, no judge", f.llm.calls)
	}
	if got := f.outcome(t, task); got != "stopped:needs_user_input" {
		t.Fatalf("outcome = %q", got)
	}
	row := f.row(t)
	if row.Status != GoalStatusWaitingUser {
		t.Fatalf("goal status = %q", row.Status)
	}
	q := goalQuestionOf(row.Question)
	if q == nil || q.Kind != "choice" || len(q.Options) != 2 || q.RunID != task || q.Answer != "" {
		t.Fatalf("question = %+v", q)
	}
	c := f.comments(t, "agent")
	if len(c) != 1 || !strings.Contains(c[0], "Which database?") || !strings.Contains(c[0], "2. SQLite") {
		t.Fatalf("question comment = %q", c)
	}
	var inbox int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = $2 AND recipient_id = $3 AND archived = false`, f.issueID, GoalInboxQuestionType, f.userID).Scan(&inbox); err != nil || inbox != 1 {
		t.Fatalf("inbox items for the issue creator = %d (%v), want 1", inbox, err)
	}
	if qd := f.queued(t); len(qd) != 0 {
		t.Fatalf("a question must not queue a run, got %+v", qd)
	}

	// Nothing waiting → conflict; a real answer resumes the chain.
	g := newGoalFixture(t, "native")
	if _, err := g.goal.Answer(ctx, g.issue(t), "Postgres", util.MustParseUUID(g.userID), "Jeff"); err != ErrGoalNotWaiting {
		t.Fatalf("answer without a question = %v, want ErrGoalNotWaiting", err)
	}
	updated, err := f.goal.Answer(ctx, f.issue(t), "1", util.MustParseUUID(f.userID), "Jeff")
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if updated.Status != GoalStatusActive {
		t.Fatalf("goal status after answer = %q", updated.Status)
	}
	if q := goalQuestionOf(updated.Question); q == nil || q.Answer != "Postgres" || q.AnsweredBy != f.userID || q.AnsweredByName != "Jeff" {
		t.Fatalf("answered question = %+v (an option number resolves to its text)", q)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND type = $2 AND archived = false`, f.issueID, GoalInboxQuestionType).Scan(&inbox); err != nil || inbox != 0 {
		t.Fatalf("open inbox items after answer = %d (%v), want 0", inbox, err)
	}
	if m := f.comments(t, "member"); len(m) != 1 || !strings.Contains(m[0], "Postgres") {
		t.Fatalf("answer comment = %q", m)
	}
	qd := f.queued(t)
	if len(qd) != 1 || qd[0].LegRole != LegRoleContinuation || !strings.Contains(qd[0].HandoffNote.String, "Jeff answered: Postgres") {
		t.Fatalf("run after answer = %+v", qd)
	}
	// The follow-up run reads the answer in its brief.
	f.runNative(t, util.UUIDToString(qd[0].ID), nativeTextTurn("Using Postgres. Done."), judgeTurn(true, "", "done with Postgres"))
	brief := f.llm.first.Messages[1].OfUser.Content.OfString.Value
	if !strings.Contains(brief, "The team answered: Postgres") {
		t.Fatalf("follow-up brief lacks the answer: %q", brief)
	}
}

// The chain is bounded by the row: the eighth continuation is the last, two
// identical statuses in a row stop the loop, a human blocker stops it too.
func TestGoalLoopBoundsAndStops(t *testing.T) {
	ctx := context.Background()
	prime := func(f *goalFixture, continuation, noProgress int, signature string) {
		goal, err := db.New(f.pool).EnsureIssueGoal(ctx, db.EnsureIssueGoalParams{ID: dbid.NewV7(), WorkspaceID: util.MustParseUUID(f.workspaceID), IssueID: util.MustParseUUID(f.issueID), MaxContinuations: 8})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.New(f.pool).UpdateIssueGoalState(ctx, db.UpdateIssueGoalStateParams{ID: goal.ID, Status: GoalStatusActive, Continuation: int32(continuation), NoProgress: int32(noProgress), LastSignature: signature, LastOutcome: "continued", Evidence: []byte(`["earlier"]`)}); err != nil {
			t.Fatal(err)
		}
	}

	f := newGoalFixture(t, "native")
	prime(f, 7, 0, "x")
	eighth := f.newTask(t, testutil.Cols{"leg_role": LegRoleContinuation, "handoff_note": "Continuation 8/8"})
	f.runNative(t, eighth, nativeTextTurn("Still one part left."), judgeTurn(false, "goal_not_met_yet", "one part left"))
	if got := f.outcome(t, eighth); got != "stopped:exhausted" {
		t.Fatalf("outcome = %q, want stopped:exhausted", got)
	}
	if row := f.row(t); row.Status != GoalStatusStopped || row.Continuation != 8 {
		t.Fatalf("goal row = %+v", row)
	}
	if c := f.comments(t, "agent"); len(c) != 1 || !strings.Contains(c[0], "exhausted") {
		t.Fatalf("stop comment = %q", c)
	}
	if q := f.queued(t); len(q) != 0 {
		t.Fatalf("exhausted must not queue, got %+v", q)
	}

	status := "Blocked on the same thing."
	g := newGoalFixture(t, "native")
	prime(g, 1, 1, goalSignature(status))
	stuck := g.newTask(t, testutil.Cols{"leg_role": LegRoleContinuation, "handoff_note": "Continuation 2/8"})
	g.runNative(t, stuck, nativeTextTurn(status), judgeTurn(false, "goal_not_met_yet", "no progress"))
	if got := g.outcome(t, stuck); got != "stopped:stagnation" {
		t.Fatalf("outcome = %q, want stopped:stagnation", got)
	}

	h := newGoalFixture(t, "native")
	wait := h.newTask(t)
	h.runNative(t, wait, nativeTextTurn("Waiting for the vendor's reply."), judgeTurn(false, "external_wait", "vendor reply pending"))
	if got := h.outcome(t, wait); got != "stopped:external_wait" {
		t.Fatalf("outcome = %q", got)
	}
	if c := h.comments(t, "agent"); len(c) != 1 || !strings.Contains(c[0], "external_wait") {
		t.Fatalf("stop comment = %q", c)
	}

	// A human-started run after a stop opens a new chain: the count starts
	// over and the old evidence is dropped.
	fresh := h.newTask(t)
	h.runNative(t, fresh, nativeTextTurn("Vendor replied; half done."), judgeTurn(false, "goal_not_met_yet", "half"))
	if row := h.row(t); row.Status != GoalStatusActive || row.Continuation != 0 || strings.Count(string(row.Evidence), "evidence for") != 1 {
		t.Fatalf("goal row after a fresh run = %+v", row)
	}
}

// Pause: no judge, no continuation, queued continuations cancelled. Resume:
// fresh allowance and a run queued from the goal state.
func TestGoalLoopPauseAndResume(t *testing.T) {
	ctx := context.Background()
	f := newGoalFixture(t, "native")
	first := f.newTask(t)
	f.runNative(t, first, nativeTextTurn("Half done."), judgeTurn(false, "goal_not_met_yet", "half"))
	if q := f.queued(t); len(q) != 1 {
		t.Fatalf("expected a queued continuation, got %+v", q)
	}
	if _, err := f.goal.Pause(ctx, f.issue(t)); err != nil {
		t.Fatal(err)
	}
	if q := f.queued(t); len(q) != 0 {
		t.Fatalf("pause must cancel the queued continuation, got %+v", q)
	}
	if row := f.row(t); row.Status != GoalStatusPaused {
		t.Fatalf("goal status = %q", row.Status)
	}
	// A run that settles while paused is not judged and queues nothing.
	stray := f.newTask(t)
	f.runNative(t, stray, nativeTextTurn("Did a bit more."))
	if f.llm.calls != 1 {
		t.Fatalf("model calls while paused = %d, want the run only", f.llm.calls)
	}
	if got := f.outcome(t, stray); got != "stopped:paused" {
		t.Fatalf("outcome = %q", got)
	}
	updated, err := f.goal.Resume(ctx, f.issue(t), util.MustParseUUID(f.userID))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != GoalStatusActive || updated.Continuation != 0 {
		t.Fatalf("goal after resume = %+v", updated)
	}
	q := f.queued(t)
	if len(q) != 1 || q[0].LegRole != LegRoleContinuation || !strings.Contains(q[0].HandoffNote.String, "resumed by a team member") {
		t.Fatalf("run after resume = %+v", q)
	}
}

// Disabled workspace: no judge, no row. Judge outage: the chain stops
// without a comment and the run stays completed.
func TestGoalLoopDisabledAndJudgeOutage(t *testing.T) {
	ctx := context.Background()
	f := newGoalFixture(t, "native")
	if _, err := f.pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, f.workspaceID); err != nil {
		t.Fatal(err)
	}
	off := f.newTask(t)
	f.runNative(t, off, nativeTextTurn("Half done."))
	if f.llm.calls != 1 || f.outcome(t, off) != "" {
		t.Fatalf("disabled loop: calls = %d, outcome = %q", f.llm.calls, f.outcome(t, off))
	}
	if _, err := db.New(f.pool).GetIssueGoal(ctx, db.GetIssueGoalParams{IssueID: util.MustParseUUID(f.issueID), WorkspaceID: util.MustParseUUID(f.workspaceID)}); err == nil {
		t.Fatal("a disabled loop must not create a goal row")
	}

	g := newGoalFixture(t, "native")
	garbage := g.newTask(t)
	g.runNative(t, garbage, nativeTextTurn("Half done."), nativeTextTurn("Looks fine to me!"))
	if got := g.outcome(t, garbage); got != "stopped:judge_unavailable" {
		t.Fatalf("outcome = %q", got)
	}
	if c := g.comments(t, "agent"); len(c) != 0 {
		t.Fatalf("a judge outage must not comment, got %q", c)
	}
	var status string
	if err := g.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, garbage).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("task status = %q (%v), want completed", status, err)
	}
}

// A run on a daemon runtime is judged through the task:completed event: the
// CLI's output is the closing status, the verdict and the continuation are
// the same as for a native run.
func TestGoalLoopJudgesDaemonRunsThroughTheBus(t *testing.T) {
	ctx := context.Background()
	f := newGoalFixture(t, "claude")
	f.goal.Subscribe(f.bus)
	f.llm.turns = []openai.ChatCompletion{judgeTurn(false, "goal_not_met_yet", "cli did half")}
	taskID := f.newTask(t)
	if _, err := f.tasks.claimTask(ctx, util.MustParseUUID(f.agentID), util.MustParseUUID(f.runtimeID), false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(ctx, util.MustParseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]any{"output": "Implemented half of it; tests remain."})
	if _, err := f.tasks.CompleteTask(ctx, util.MustParseUUID(taskID), result, "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	// The judge runs on its own goroutine: the verdict lands first, the
	// continuation (and its leg stamp) right after.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if q := f.queued(t); len(q) == 1 && q[0].LegRole == LegRoleContinuation {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := f.outcome(t, taskID); got != "continued" {
		t.Fatalf("outcome = %q, want continued (judged off the bus)", got)
	}
	prompt := nativeLastUserMessage(t, f.llm.last)
	if !strings.Contains(prompt, "Implemented half of it") {
		t.Fatalf("judge did not read the CLI output: %q", prompt)
	}
	if q := f.queued(t); len(q) != 1 || q[0].LegRole != LegRoleContinuation {
		t.Fatalf("continuation after a daemon run = %+v", q)
	}
	// Publishing the same event twice judges once.
	f.llm.turns = append(f.llm.turns, judgeTurn(false, "goal_not_met_yet", "again"))
	f.bus.Publish(events.Event{Type: protocol.EventTaskCompleted, Payload: map[string]any{"task_id": taskID, "issue_id": f.issueID, "status": "completed"}})
	time.Sleep(300 * time.Millisecond)
	if f.llm.calls != 1 {
		t.Fatalf("judge calls after a duplicate event = %d, want 1", f.llm.calls)
	}
	_ = goalstate.Render
}

// The inbox:new payload carries the recipient: the realtime listener routes
// the event by it, so an item without one never reaches a client live.
func TestInboxItemPayloadCarriesRecipient(t *testing.T) {
	item := db.InboxItem{ID: dbid.NewV7(), WorkspaceID: dbid.NewV7(), RecipientType: "member", RecipientID: dbid.NewV7(), Type: GoalInboxQuestionType, Severity: "action_required", Title: "Question"}
	item.IssueID = dbid.NewV7()
	out := InboxItemPayload(item)
	if out["recipient_id"] != util.UUIDToString(item.RecipientID) || out["recipient_type"] != "member" || out["issue_id"] != util.UUIDToString(item.IssueID) || out["type"] != GoalInboxQuestionType {
		t.Fatalf("payload = %v", out)
	}
}
