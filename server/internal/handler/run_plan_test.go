package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F04 · the living run plan. The plan is a task_message of type 'plan', so
// these tests pin the two things that are NOT free from reusing that row: the
// authorization rule (only the run itself writes its own plan) and the
// replacement rule (highest seq wins, older versions stay in the transcript).

// runPlanRequest builds the POST a run makes with its own task token. The
// X-Actor-Source header is what the auth middleware stamps for an `mat_`
// credential; a member JWT never carries it.
func runPlanRequest(t *testing.T, pathTaskID, callerTaskID string, body any) *http.Request {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/tasks/"+pathTaskID+"/plan", body)
	req = withURLParam(req, "taskId", pathTaskID)
	if callerTaskID != "" {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Task-ID", callerTaskID)
	}
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.MustParseUUID(testUserID),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
		})
	if err != nil {
		t.Fatalf("load member row: %v", err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

func planItems(pairs ...string) []any {
	items := make([]any, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, map[string]any{"text": pairs[i], "status": pairs[i+1]})
	}
	return items
}

func TestSetRunPlanStoresNormalizedChecklist(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-store")

	var plan RunPlan
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
		"items": planItems(
			"  Read the failing test  ", "done",
			"Fix the parser", "in_progress",
			"Update the docs", "pending",
		),
	})).Want(http.StatusCreated).JSON(&plan)

	if len(plan.Items) != 3 {
		t.Fatalf("items = %d, want 3: %+v", len(plan.Items), plan.Items)
	}
	if plan.Items[0].Text != "Read the failing test" {
		t.Errorf("item text = %q, want it trimmed to %q", plan.Items[0].Text, "Read the failing test")
	}
	// The seq is the client's "is this newer than what I render" key, so it has
	// to come back on the write, not only on the next read.
	if plan.Seq <= 0 {
		t.Errorf("seq = %d, want a server-assigned positive seq", plan.Seq)
	}

	// The row is an ordinary task_message, which is the whole point: the
	// transcript, the realtime broadcast and the purge come for free.
	var msgType, content string
	var input []byte
	dbfx.QueryRow(t,
		`SELECT type, content, input FROM task_message WHERE task_id = $1 AND type = 'plan'`,
		taskID).Scan(&msgType, &content, &input)
	if msgType != "plan" {
		t.Errorf("stored type = %q, want plan", msgType)
	}
	if content != "1/3 done" {
		t.Errorf("stored content = %q, want the collapsed transcript summary %q", content, "1/3 done")
	}
	if !strings.Contains(string(input), "Fix the parser") {
		t.Errorf("stored input = %s, want it to carry the checklist", input)
	}
}

// The daemon numbers its own messages from an in-process counter starting at 1,
// so a plan allocated at MAX(seq)+1 would take the number the daemon's next
// flush is about to use and the clients' merge-by-seq would drop one of them.
func TestSetRunPlanSeqCannotCollideWithDaemonMessages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-seq")

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "starting"},
		map[string]any{"seq": 2, "type": "text", "content": "still going"},
	})).Want(http.StatusOK)

	var plan RunPlan
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
		"items": planItems("Step", "pending"),
	})).Want(http.StatusCreated).JSON(&plan)

	if plan.Seq <= runPlanSeqFloor {
		t.Fatalf("plan seq = %d, want it above the reserved floor %d so the daemon's counter can never reach it",
			plan.Seq, runPlanSeqFloor)
	}

	// The daemon keeps counting from where it was; nothing it writes may land
	// on the plan's seq.
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 3, "type": "text", "content": "after the plan"},
	})).Want(http.StatusOK)

	var duplicates int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM (SELECT seq FROM task_message WHERE task_id = $1 GROUP BY seq HAVING count(*) > 1) d`,
		taskID).Scan(&duplicates)
	if duplicates != 0 {
		t.Errorf("%d seq value(s) used twice on the run; the clients dedupe by seq, so one message would be lost", duplicates)
	}
}

func TestSetRunPlanReplacesTheEarlierPlan(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-replace")

	var first RunPlan
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
		"items": planItems("Investigate", "in_progress", "Fix", "pending"),
	})).Want(http.StatusCreated).JSON(&first)

	var second RunPlan
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
		"items": planItems("Investigate", "done", "Fix", "in_progress"),
	})).Want(http.StatusCreated).JSON(&second)

	if second.Seq <= first.Seq {
		t.Fatalf("second plan seq = %d, first = %d; the newer plan must win the highest-seq comparison",
			second.Seq, first.Seq)
	}

	// Both versions stay: the plan is a message, and the transcript is the
	// record of what the run believed at each point.
	var rows int
	dbfx.QueryRow(t, `SELECT count(*) FROM task_message WHERE task_id = $1 AND type = 'plan'`, taskID).Scan(&rows)
	if rows != 2 {
		t.Errorf("stored plan rows = %d, want 2; replacement is a new message, never an UPDATE", rows)
	}

	// And only the newest one is what a reader gets.
	plan := runPlanOfTaskInList(t, taskID)
	if plan == nil {
		t.Fatal("execution log carries no plan for the run")
	}
	if plan.Seq != second.Seq || plan.Items[0].Status != "done" {
		t.Errorf("hydrated plan = %+v, want the second version (seq %d)", plan, second.Seq)
	}
}

func TestSetRunPlanRejectsCallersThatAreNotTheRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-auth")
	otherTaskID := seedBatchTask(t, "run-plan-auth-other")
	body := map[string]any{"items": planItems("Step", "pending")}

	cases := []struct {
		name         string
		callerTaskID string
		strip        func(*http.Request)
		why          string
	}{
		{
			name:  "no task token at all",
			why:   "a plain member must not be able to write what the agent believes it is doing",
			strip: func(*http.Request) {},
		},
		{
			name:         "another run's task token",
			callerTaskID: otherTaskID,
			why:          "the token binds one run; it must not author another run's plan",
			strip:        func(*http.Request) {},
		},
		{
			name:         "forged X-Task-ID without the server-set actor source",
			callerTaskID: taskID,
			why:          "X-Actor-Source is the only unforgeable signal that the caller is a run",
			strip:        func(r *http.Request) { r.Header.Del("X-Actor-Source") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := runPlanRequest(t, taskID, tc.callerTaskID, body)
			tc.strip(req)
			testutil.Call(t, testHandler.SetRunPlan, req).Want(http.StatusForbidden)

			var rows int
			dbfx.QueryRow(t, `SELECT count(*) FROM task_message WHERE task_id = $1 AND type = 'plan'`, taskID).Scan(&rows)
			if rows != 0 {
				t.Errorf("a rejected write persisted %d plan row(s): %s", rows, tc.why)
			}
		})
	}
}

func TestSetRunPlanRejectsFinishedRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			taskID := seedBatchTask(t, "run-plan-terminal-"+status)
			dbfx.Exec(t, `UPDATE agent_task_queue SET status = $2 WHERE id = $1`, taskID, status)

			testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
				"items": planItems("Too late", "pending"),
			})).Want(http.StatusConflict)
		})
	}
}

func TestSetRunPlanRejectsMalformedChecklists(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-validation")

	tooMany := make([]any, 0, runPlanMaxItems+1)
	for i := 0; i <= runPlanMaxItems; i++ {
		tooMany = append(tooMany, map[string]any{"text": fmt.Sprintf("step %d", i), "status": "pending"})
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		{"no items", map[string]any{"items": []any{}}},
		{"items absent", map[string]any{}},
		{"over the item cap", map[string]any{"items": tooMany}},
		{"empty text", map[string]any{"items": planItems("   ", "pending")}},
		{"text over the length cap", map[string]any{
			"items": planItems(strings.Repeat("x", runPlanMaxTextLen+1), "pending"),
		}},
		{"unknown status", map[string]any{"items": planItems("Step", "blocked")}},
		{"two items in progress", map[string]any{
			"items": planItems("One", "in_progress", "Two", "in_progress"),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, tc.body)).
				Want(http.StatusBadRequest)
		})
	}

	// Exactly at the caps is valid — the boundary belongs to the accepted side.
	atCap := make([]any, 0, runPlanMaxItems)
	for i := 0; i < runPlanMaxItems; i++ {
		atCap = append(atCap, map[string]any{"text": strings.Repeat("y", runPlanMaxTextLen), "status": "pending"})
	}
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{"items": atCap})).
		Want(http.StatusCreated)
}

// A run that published no plan must leave the field off the wire entirely: the
// UI keys the whole block on its absence, and an empty checklist would assert
// the run has nothing to do.
func TestTaskResponsesOmitPlanWhenTheRunPublishedNone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-absent")

	raw := listTasksByIssueRaw(t, issueIDForTask(t, taskID))
	if strings.Contains(raw, `"plan"`) {
		t.Errorf("execution log carries a plan key for a run that never published one: %s", raw)
	}
}

func TestGetActiveTaskForIssueHydratesPlan(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-plan-active")
	testutil.Call(t, testHandler.SetRunPlan, runPlanRequest(t, taskID, taskID, map[string]any{
		"items": planItems("Ship it", "in_progress"),
	})).Want(http.StatusCreated)

	issueID := issueIDForTask(t, taskID)
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/active-task", nil)
	req = withURLParam(req, "id", issueID)
	var out struct {
		Tasks []AgentTaskResponse `json:"tasks"`
	}
	testutil.Call(t, testHandler.GetActiveTaskForIssue, req).Want(http.StatusOK).JSON(&out)

	for _, task := range out.Tasks {
		if task.ID != taskID {
			continue
		}
		if task.Plan == nil || len(task.Plan.Items) != 1 || task.Plan.Items[0].Status != "in_progress" {
			t.Fatalf("active task plan = %+v, want the published checklist", task.Plan)
		}
		return
	}
	t.Fatalf("run %s missing from the active-task read", taskID)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func listTasksByIssueRaw(t *testing.T, issueID string) string {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil)
	req = withURLParam(req, "id", issueID)
	var out json.RawMessage
	testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&out)
	return string(out)
}

func runPlanOfTaskInList(t *testing.T, taskID string) *RunPlan {
	t.Helper()
	issueID := issueIDForTask(t, taskID)
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil)
	req = withURLParam(req, "id", issueID)
	var tasks []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&tasks)
	for i := range tasks {
		if tasks[i].ID == taskID {
			return tasks[i].Plan
		}
	}
	return nil
}
