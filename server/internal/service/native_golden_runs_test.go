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
	openai "github.com/openai/openai-go/v3"
)

// N19 — the golden suite itself is the gate: every étalon must pass, or the
// tool loop regressed. Cases share one workspace bootstrap pattern but run
// serially so a single DB failure is attributable to one slug.

func TestNativeGoldenCatalogLocked(t *testing.T) {
	cat := NativeGoldenCatalog()
	if len(cat) != 10 {
		t.Fatalf("catalog size = %d, want 10 étalon tasks", len(cat))
	}
	seen := map[string]bool{}
	for _, c := range cat {
		if c.ID == "" || c.ExpectedTool == "" || c.Class == "" {
			t.Fatalf("incomplete case: %+v", c)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate id %s", c.ID)
		}
		seen[c.ID] = true
	}
	if NativeGoldenSuiteName == "" {
		t.Fatal("suite name empty")
	}
}

func TestNativeGoldenRunsSuite(t *testing.T) {
	catalog := NativeGoldenCatalog()
	runners := map[string]func(*testing.T, *nativeGoldenEnv) string{
		"G01_helpdesk_comment":  runGoldenHelpdeskComment,
		"G02_get_issue":         runGoldenGetIssue,
		"G03_list_issues":       runGoldenListIssues,
		"G04_update_issue":      runGoldenUpdateIssue,
		"G05_transition_issue":  runGoldenTransitionIssue,
		"G06_create_issue":      runGoldenCreateIssue,
		"G07_save_note":         runGoldenSaveNote,
		"G08_update_note":       runGoldenUpdateNote,
		"G09_search_workspace":  runGoldenSearchWorkspace,
		"G10_schedule_followup": runGoldenScheduleFollowup,
	}
	if len(runners) != len(catalog) {
		t.Fatalf("runners=%d catalog=%d — keep them in lockstep", len(runners), len(catalog))
	}

	passed := 0
	var failures []string
	for _, c := range catalog {
		c := c
		run, ok := runners[c.ID]
		if !ok {
			t.Fatalf("no runner for %s", c.ID)
		}
		t.Run(c.ID, func(t *testing.T) {
			env := newNativeGoldenEnv(t, c)
			if detail := run(t, env); detail != "" {
				failures = append(failures, c.ID+": "+detail)
				t.Fatalf("%s failed: %s", c.ID, detail)
			}
			passed++
		})
	}

	rate := float64(passed) / float64(len(catalog))
	if rate < nativeGoldenPassThreshold {
		t.Fatalf("native golden pass rate = %.2f (%d/%d), want ≥ %.2f; failures: %v",
			rate, passed, len(catalog), nativeGoldenPassThreshold, failures)
	}
}

type nativeGoldenEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	fx        *testutil.Fixture
	ws        string
	user      string
	runtimeID string
	agentID   string
	caseMeta  NativeGoldenCase
}

func newNativeGoldenEnv(t *testing.T, meta NativeGoldenCase) *nativeGoldenEnv {
	t.Helper()
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("gold-owner-%d", suffix), fmt.Sprintf("gold-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("gold-ws-%d", suffix), fmt.Sprintf("gold-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, ws); err != nil {
		t.Fatalf("disable goal loop: %v", err)
	}
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Golden worker", runtimeID)
	return &nativeGoldenEnv{ctx: ctx, pool: pool, fx: fx, ws: ws, user: user, runtimeID: runtimeID, agentID: agentID, caseMeta: meta}
}

func (e *nativeGoldenEnv) runScripted(t *testing.T, taskID string, turns []openai.ChatCompletion) *scriptedNativeLLM {
	t.Helper()
	llm := &scriptedNativeLLM{turns: turns}
	tasks := NewTaskService(db.New(e.pool), e.pool, nil, events.New())
	issues := NewIssueService(db.New(e.pool), e.pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(e.pool), tasks, issues, llm, events.New())
	claimed, err := tasks.claimTask(e.ctx, util.MustParseUUID(e.agentID), util.MustParseUUID(e.runtimeID), false)
	if err != nil || claimed == nil {
		t.Fatalf("claim %s: %v (%v)", taskID, claimed, err)
	}
	svc.runTask(e.ctx, *claimed)
	return llm
}

func (e *nativeGoldenEnv) taskCompleted(t *testing.T, taskID string) string {
	t.Helper()
	var status string
	if err := e.pool.QueryRow(e.ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status); err != nil {
		return "read task: " + err.Error()
	}
	if status != "completed" {
		return fmt.Sprintf("status=%q want completed", status)
	}
	return ""
}

func (e *nativeGoldenEnv) toolUsed(t *testing.T, taskID, tool string) string {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(e.ctx, `SELECT COUNT(*) FROM task_message WHERE task_id=$1 AND type='tool_use' AND tool=$2`, taskID, tool).Scan(&n); err != nil {
		return err.Error()
	}
	if n < 1 {
		return fmt.Sprintf("tool_use %s missing", tool)
	}
	return ""
}

// closingTurns: after an effectful tool, N18 diverts premature prose into a
// wrap-up — script two text turns so the run settles.
func goldenClosing(cite string) []openai.ChatCompletion {
	return []openai.ChatCompletion{
		nativeTextTurn("All done."),
		nativeTextTurn("Status: " + cite + ". Nothing remains. Nothing blocked."),
	}
}

func runGoldenHelpdeskComment(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Helpdesk: VPN down")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "add_comment", `{"content":"Have you tried reconnecting the VPN client?"}`),
	}, goldenClosing("posted helpdesk reply [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "add_comment"); d != "" {
		return d
	}
	var n int
	e.pool.QueryRow(e.ctx, `SELECT COUNT(*) FROM comment WHERE issue_id=$1 AND content LIKE '%VPN%'`, issueID).Scan(&n)
	if n != 1 {
		return fmt.Sprintf("comments=%d", n)
	}
	return ""
}

func runGoldenGetIssue(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Read me")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	e.runScripted(t, taskID, []openai.ChatCompletion{
		nativeToolCallTurn("c1", "get_issue", `{}`),
		nativeTextTurn("Issue loaded; title is Read me."),
	})
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	return e.toolUsed(t, taskID, "get_issue")
}

func runGoldenListIssues(t *testing.T, e *nativeGoldenEnv) string {
	e.fx.Issue(t, "Alpha")
	e.fx.Issue(t, "Beta")
	issueID := e.fx.Issue(t, "List them")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	e.runScripted(t, taskID, []openai.ChatCompletion{
		nativeToolCallTurn("c1", "list_issues", `{"limit":10}`),
		nativeTextTurn("Listed the workspace issues."),
	})
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	return e.toolUsed(t, taskID, "list_issues")
}

func runGoldenUpdateIssue(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Old title")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "update_issue", `{"title":"New title"}`),
	}, goldenClosing("renamed issue [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "update_issue"); d != "" {
		return d
	}
	var title string
	e.pool.QueryRow(e.ctx, `SELECT title FROM issue WHERE id=$1`, issueID).Scan(&title)
	if title != "New title" {
		return fmt.Sprintf("title=%q", title)
	}
	return ""
}

func runGoldenTransitionIssue(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Start work", testutil.Cols{"status": "todo"})
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "transition_issue", `{"status":"in_progress"}`),
	}, goldenClosing("moved to in_progress [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "transition_issue"); d != "" {
		return d
	}
	var status string
	e.pool.QueryRow(e.ctx, `SELECT status FROM issue WHERE id=$1`, issueID).Scan(&status)
	if status != "in_progress" {
		return fmt.Sprintf("status=%q", status)
	}
	return ""
}

func runGoldenCreateIssue(t *testing.T, e *nativeGoldenEnv) string {
	contextJSON := fmt.Sprintf(`{"type":"quick_create","prompt":"File a demo prep issue","workspace_id":"%s","requester_id":"%s"}`, e.ws, e.user)
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{
		"runtime_id": e.runtimeID,
		"context":    testutil.Raw("'" + contextJSON + "'::jsonb"),
	})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "create_issue", `{"title":"Prep demo","description":"Steps for the demo","priority":"medium"}`),
	}, goldenClosing("filed Prep demo [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "create_issue"); d != "" {
		return d
	}
	var n int
	e.pool.QueryRow(e.ctx, `SELECT COUNT(*) FROM issue WHERE workspace_id=$1 AND title='Prep demo'`, e.ws).Scan(&n)
	if n != 1 {
		return fmt.Sprintf("created issues=%d", n)
	}
	return ""
}

func runGoldenSaveNote(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Capture note")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "save_note", `{"title":"VPN runbook","content":"Restart the client, then the gateway.","tags":["helpdesk"]}`),
	}, goldenClosing("saved VPN runbook [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "save_note"); d != "" {
		return d
	}
	var n int
	e.pool.QueryRow(e.ctx, `SELECT COUNT(*) FROM workspace_note WHERE workspace_id=$1 AND title='VPN runbook'`, e.ws).Scan(&n)
	if n != 1 {
		return fmt.Sprintf("notes=%d", n)
	}
	return ""
}

func runGoldenUpdateNote(t *testing.T, e *nativeGoldenEnv) string {
	noteID := seedBrainNote(t, e.pool, e.ws, "Weekly CR", "Week 1.", nil, false)
	contextJSON := fmt.Sprintf(`{"type":"note_target","note_id":"%s","instruction":"Add week 2."}`, noteID)
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{
		"runtime_id": e.runtimeID,
		"context":    testutil.Raw("'" + contextJSON + "'::jsonb"),
	})
	args, _ := json.Marshal(map[string]string{"note_id": noteID, "content": "Week 1.\nWeek 2: shipped."})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "update_note", string(args)),
	}, goldenClosing("updated living doc [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "update_note"); d != "" {
		return d
	}
	var rev int64
	var content string
	e.pool.QueryRow(e.ctx, `SELECT revision, content FROM workspace_note WHERE id=$1`, noteID).Scan(&rev, &content)
	if rev != 2 || !strings.Contains(content, "Week 2") {
		return fmt.Sprintf("rev=%d content=%q", rev, content)
	}
	return ""
}

func runGoldenSearchWorkspace(t *testing.T, e *nativeGoldenEnv) string {
	e.fx.Issue(t, "UniqueNeedleXYZ search target")
	issueID := e.fx.Issue(t, "Searcher")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	e.runScripted(t, taskID, []openai.ChatCompletion{
		nativeToolCallTurn("c1", "search_workspace", `{"query":"UniqueNeedleXYZ"}`),
		nativeTextTurn("Found the needle issue."),
	})
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	return e.toolUsed(t, taskID, "search_workspace")
}

func runGoldenScheduleFollowup(t *testing.T, e *nativeGoldenEnv) string {
	issueID := e.fx.Issue(t, "Follow up tomorrow")
	taskID := e.fx.Task(t, e.agentID, testutil.Cols{"issue_id": issueID, "runtime_id": e.runtimeID})
	turns := append([]openai.ChatCompletion{
		nativeToolCallTurn("c1", "schedule_followup", `{"when":"+90","note":"Check client reply"}`),
	}, goldenClosing("scheduled follow-up [r1]")...)
	e.runScripted(t, taskID, turns)
	if d := e.taskCompleted(t, taskID); d != "" {
		return d
	}
	if d := e.toolUsed(t, taskID, "schedule_followup"); d != "" {
		return d
	}
	var n int
	e.pool.QueryRow(e.ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id=$1 AND status='deferred' AND trigger_summary LIKE '%Check client reply%'`, e.agentID).Scan(&n)
	if n != 1 {
		return fmt.Sprintf("deferred=%d", n)
	}
	return ""
}
