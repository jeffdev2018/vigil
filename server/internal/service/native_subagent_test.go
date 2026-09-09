package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	openai "github.com/openai/openai-go/v3"
)

// The report contract: citations are matched against the receipts, both
// ways, and the shape is stable for the model (never null).
func TestNativeVerifyReport(t *testing.T) {
	receipts := []nativeReceipt{{ID: "r1", Tool: "get_issue", OK: true}, {ID: "r2", Tool: "add_comment", OK: true}, {ID: "r3", Tool: "get_note", OK: false}}
	out := nativeVerifyReport("Read the issue [r1] and commented [r2]. Also archived it [r7] and [r1] again.", receipts)
	if strings.Join(out.UnverifiedCitations, ",") != "r7" {
		t.Fatalf("unverified = %v", out.UnverifiedCitations)
	}
	if strings.Join(out.UncitedReceipts, ",") != "r3" {
		t.Fatalf("uncited = %v", out.UncitedReceipts)
	}
	empty := nativeVerifyReport("Nothing to report.", nil)
	if empty.Receipts == nil || empty.UnverifiedCitations == nil || empty.UncitedReceipts == nil {
		t.Fatal("empty lists must serialise as [] for the model")
	}
}

// routedNativeLLM plays a script per route, chosen by a substring of the
// brief (the run's first user message), so a run and its sub-agents — which
// call concurrently — each get their own turns. Records the peak number of
// in-flight calls on a route.
type routedNativeLLM struct {
	mu          sync.Mutex
	routes      map[string][]openai.ChatCompletion
	idx         map[string]int
	delay       time.Duration
	inFlight    map[string]*int32
	maxInFlight map[string]int32
	calls       int
}

func (r *routedNativeLLM) Enabled() bool { return true }

func (r *routedNativeLLM) route(params openai.ChatCompletionNewParams) string {
	if len(params.Messages) < 2 || params.Messages[1].OfUser == nil {
		return "default"
	}
	brief := params.Messages[1].OfUser.Content.OfString.Value
	for key := range r.routes {
		if key != "default" && strings.Contains(brief, key) {
			return key
		}
	}
	return "default"
}

func (r *routedNativeLLM) Chat(_ context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	key := r.route(params)
	r.mu.Lock()
	if r.idx == nil {
		r.idx, r.inFlight, r.maxInFlight = map[string]int{}, map[string]*int32{}, map[string]int32{}
	}
	if r.inFlight[key] == nil {
		r.inFlight[key] = new(int32)
	}
	turns := r.routes[key]
	i := r.idx[key]
	r.idx[key]++
	r.calls++
	counter := r.inFlight[key]
	r.mu.Unlock()
	if i >= len(turns) {
		return nil, errors.New("script exhausted for route " + key)
	}
	n := atomic.AddInt32(counter, 1)
	r.mu.Lock()
	if n > r.maxInFlight[key] {
		r.maxInFlight[key] = n
	}
	r.mu.Unlock()
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	atomic.AddInt32(counter, -1)
	c := turns[i]
	if c.Model == "" {
		c.Model = "scripted-model"
	}
	// Every turn costs something, so usage rows exist for each run.
	c.Usage = openai.CompletionUsage{PromptTokens: 120, CompletionTokens: 30}
	return &c, nil
}

type subagentFixture struct {
	pool                                     *pgxpool.Pool
	fx                                       *testutil.Fixture
	workspaceID, runtimeID, agentID, issueID string
	tasks                                    *TaskService
}

func newSubagentFixture(t *testing.T) *subagentFixture {
	t.Helper()
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("sub-owner-%d", suffix), fmt.Sprintf("sub-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("sub-ws-%d", suffix), fmt.Sprintf("sub-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, ws); err != nil {
		t.Fatal(err)
	}
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native"})
	agentID := fx.Agent(t, "Delegating worker", runtimeID)
	issueID := fx.Issue(t, "Split the work", testutil.Cols{"description": "Two independent reads.", "assignee_type": "agent", "assignee_id": agentID})
	return &subagentFixture{pool: pool, fx: fx, workspaceID: ws, runtimeID: runtimeID, agentID: agentID, issueID: issueID, tasks: NewTaskService(db.New(pool), pool, nil, events.New())}
}

func (f *subagentFixture) run(t *testing.T, llm NativeAgentLLM) string {
	t.Helper()
	ctx := context.Background()
	taskID := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})
	issues := NewIssueService(db.New(f.pool), f.pool, events.New(), nil, f.tasks)
	svc := NewNativeAgentService(db.New(f.pool), f.tasks, issues, llm, events.New())
	claimed, err := f.tasks.claimTask(ctx, util.MustParseUUID(f.agentID), util.MustParseUUID(f.runtimeID), false)
	if err != nil || claimed == nil || util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	svc.runTask(ctx, *claimed)
	return taskID
}

func (f *subagentFixture) subtasks(t *testing.T, parent string) []db.AgentTaskQueue {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT id, status, leg_role, workflow_root_task_id, delegated_from_task_id, COALESCE(result::text, ''), COALESCE(error, '') FROM agent_task_queue WHERE delegated_from_task_id = $1 ORDER BY created_at`, parent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []db.AgentTaskQueue
	for rows.Next() {
		var task db.AgentTaskQueue
		var result, errText string
		if err := rows.Scan(&task.ID, &task.Status, &task.LegRole, &task.WorkflowRootTaskID, &task.DelegatedFromTaskID, &result, &errText); err != nil {
			t.Fatal(err)
		}
		task.Result = []byte(result)
		task.Error = pgtype_text(errText)
		out = append(out, task)
	}
	return out
}

func (f *subagentFixture) toolResults(t *testing.T, taskID string) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT COALESCE(output, '') FROM task_message WHERE task_id = $1 AND type = 'tool_result' ORDER BY seq`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

// One delegation, end to end: the sub-run is its own task (a subagent leg of
// the parent), its receipts are journaled, its report is verified, and the
// parent reads the verdict in its tool result.
func TestNativeAgentDelegatesAndVerifiesReceipts(t *testing.T) {
	f := newSubagentFixture(t)
	llm := &routedNativeLLM{routes: map[string][]openai.ChatCompletion{
		"default": {
			nativeToolCallTurn("call_d1", "delegate", `{"task":"Read the issue and report its title.","context":"Only the title matters."}`),
			nativeTextTurn("Delegated the read; the sub-agent reported the title."),
		},
		"Delegated task on issue": {
			nativeToolCallTurn("call_s1", "get_issue", `{}`),
			nativeTextTurn("Read the issue [r1]: its title is Split the work. I also archived it [r9]."),
		},
	}}
	parent := f.run(t, llm)

	subs := f.subtasks(t, parent)
	if len(subs) != 1 {
		t.Fatalf("sub-tasks = %d, want 1", len(subs))
	}
	sub := subs[0]
	if sub.Status != "completed" || sub.LegRole != LegRoleSubagent || util.UUIDToString(sub.WorkflowRootTaskID) != parent {
		t.Fatalf("sub-task = status %q leg %q root %s, want completed subagent leg rooted at the parent", sub.Status, sub.LegRole, util.UUIDToString(sub.WorkflowRootTaskID))
	}
	var stored struct {
		Summary  string          `json:"summary"`
		Receipts []nativeReceipt `json:"receipts"`
	}
	if err := json.Unmarshal(sub.Result, &stored); err != nil || len(stored.Receipts) != 1 || stored.Receipts[0].Tool != "get_issue" || !stored.Receipts[0].OK {
		t.Fatalf("sub-task result = %s (%v)", sub.Result, err)
	}
	results := f.toolResults(t, parent)
	if len(results) != 1 {
		t.Fatalf("parent tool results = %d, want the delegate result", len(results))
	}
	for _, want := range []string{`"unverified_citations":["r9"]`, `"uncited_receipts":[]`, `"id":"r1"`, `"tool":"get_issue"`, `"status":"completed"`, "its title is Split the work"} {
		if !strings.Contains(results[0], want) {
			t.Fatalf("delegate result lacks %s: %s", want, results[0])
		}
	}
	// The sub-run's transcript has its tool call and its report; the
	// delegate call itself was never offered to the sub-agent.
	var subTypes []string
	rows, err := f.pool.Query(context.Background(), `SELECT type FROM task_message WHERE task_id = $1 ORDER BY seq`, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ty string
		_ = rows.Scan(&ty)
		subTypes = append(subTypes, ty)
	}
	rows.Close()
	if strings.Join(subTypes, " ") != "tool_use tool_result text" {
		t.Fatalf("sub transcript = %v", subTypes)
	}
	var usageRows int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM task_usage WHERE task_id = $1`, sub.ID).Scan(&usageRows); err != nil || usageRows != 1 {
		t.Fatalf("sub-task usage rows = %d (%v), want 1", usageRows, err)
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, parent).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("parent status = %q (%v)", status, err)
	}
}

// A sub-agent whose tool returns a list, not an object (list_issues), gets a
// receipt like any other call — the regression that once took the server
// down with a type assertion in the receipt journal.
func TestNativeAgentSubagentReceiptsForListResults(t *testing.T) {
	f := newSubagentFixture(t)
	llm := &routedNativeLLM{routes: map[string][]openai.ChatCompletion{
		"default": {
			nativeToolCallTurn("call_d1", "delegate", `{"task":"List the issues and report how many you see."}`),
			nativeTextTurn("Delegated the listing."),
		},
		"Delegated task on issue": {
			nativeToolCallTurn("call_s1", "list_issues", `{"limit":3}`),
			nativeTextTurn("Listed the issues [r1]: one issue."),
		},
	}}
	parent := f.run(t, llm)
	subs := f.subtasks(t, parent)
	if len(subs) != 1 || subs[0].Status != "completed" {
		t.Fatalf("sub-tasks = %+v, want one completed", subs)
	}
	results := f.toolResults(t, parent)
	if len(results) != 1 || !strings.Contains(results[0], `"tool":"list_issues"`) || !strings.Contains(results[0], `"ok":true`) || !strings.Contains(results[0], `"unverified_citations":[]`) {
		t.Fatalf("delegate result = %v", results)
	}
}

// Four delegations in one turn run three at a time; the run may start six
// in total and the seventh is refused with the reason in its result.
func TestNativeAgentSubagentConcurrencyAndBudget(t *testing.T) {
	f := newSubagentFixture(t)
	four := make([]struct{ id, task string }, 0, 4)
	for i := 1; i <= 4; i++ {
		four = append(four, struct{ id, task string }{fmt.Sprintf("d%d", i), fmt.Sprintf("Piece %d", i)})
	}
	three := make([]struct{ id, task string }, 0, 3)
	for i := 5; i <= 7; i++ {
		three = append(three, struct{ id, task string }{fmt.Sprintf("d%d", i), fmt.Sprintf("Piece %d", i)})
	}
	subTurns := make([]openai.ChatCompletion, 0, 6)
	for i := 0; i < 6; i++ {
		subTurns = append(subTurns, nativeTextTurn("Piece done; nothing to cite."))
	}
	llm := &routedNativeLLM{delay: 60 * time.Millisecond, routes: map[string][]openai.ChatCompletion{
		"default": {
			nativeDelegateTurn(four),
			nativeDelegateTurn(three),
			nativeTextTurn("All pieces gathered."),
		},
		"Delegated task on issue": subTurns,
	}}
	parent := f.run(t, llm)

	subs := f.subtasks(t, parent)
	if len(subs) != nativeSubagentsPerRun {
		t.Fatalf("sub-tasks = %d, want the budget %d", len(subs), nativeSubagentsPerRun)
	}
	peak := llm.maxInFlight["Delegated task on issue"]
	if peak < 2 || peak > nativeSubagentConcurrency {
		t.Fatalf("peak concurrent sub-agent calls = %d, want between 2 and %d", peak, nativeSubagentConcurrency)
	}
	results := f.toolResults(t, parent)
	if len(results) != 7 {
		t.Fatalf("parent tool results = %d, want 7", len(results))
	}
	refused := 0
	for _, r := range results {
		if strings.Contains(r, "sub-agent budget") {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("refused delegations = %d, want exactly the seventh", refused)
	}
}

// A sub-agent's state-changing calls count against its own small ceiling
// and against the run's: the run cannot buy more effect by delegating.
func TestNativeAgentSubagentSharesEffectfulBudget(t *testing.T) {
	f := newSubagentFixture(t)
	subTurns := make([]openai.ChatCompletion, 0, 7)
	for i := 0; i < 5; i++ {
		subTurns = append(subTurns, nativeToolCallTurn(fmt.Sprintf("s%d", i), "add_comment", fmt.Sprintf(`{"content":"sub %d"}`, i)))
	}
	subTurns = append(subTurns, nativeTextTurn("Posted four comments [r1] [r2] [r3] [r4]; the fifth was refused [r5]."))
	parentTurns := []openai.ChatCompletion{nativeToolCallTurn("d1", "delegate", `{"task":"Post five comments."}`)}
	for i := 0; i < 7; i++ {
		parentTurns = append(parentTurns, nativeToolCallTurn(fmt.Sprintf("p%d", i), "add_comment", fmt.Sprintf(`{"content":"parent %d"}`, i)))
	}
	parentTurns = append(parentTurns, nativeTextTurn("Done what the budget allowed."))
	llm := &routedNativeLLM{routes: map[string][]openai.ChatCompletion{"default": parentTurns, "Delegated task on issue": subTurns}}
	parent := f.run(t, llm)

	var subComments, parentComments int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1 AND content LIKE 'sub %'`, f.issueID).Scan(&subComments); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1 AND content LIKE 'parent %'`, f.issueID).Scan(&parentComments); err != nil {
		t.Fatal(err)
	}
	if subComments != nativeSubagentMaxEffectful {
		t.Fatalf("sub-agent comments = %d, want its ceiling %d", subComments, nativeSubagentMaxEffectful)
	}
	if subComments+parentComments != nativeMaxEffectfulActions {
		t.Fatalf("comments = %d + %d, want the run's ceiling %d in total", subComments, parentComments, nativeMaxEffectfulActions)
	}
	results := f.toolResults(t, parent)
	if !strings.Contains(results[0], `"unverified_citations":[]`) || !strings.Contains(results[0], `"ok":false`) {
		t.Fatalf("delegate result should carry the refused call as a receipt with ok=false: %s", results[0])
	}
}

func pgtype_text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// nativeDelegateTurn is one assistant turn issuing several delegate calls.
func nativeDelegateTurn(calls []struct{ id, task string }) openai.ChatCompletion {
	turn := openai.ChatCompletion{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Role: "assistant"}, FinishReason: "tool_calls"}}}
	for _, c := range calls {
		turn.Choices[0].Message.ToolCalls = append(turn.Choices[0].Message.ToolCalls, openai.ChatCompletionMessageToolCallUnion{
			ID: c.id, Type: "function",
			Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "delegate", Arguments: fmt.Sprintf(`{"task":%q}`, c.task)},
		})
	}
	return turn
}
