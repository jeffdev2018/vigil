package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Task watchdog (K73): configured per issue with a different agent and a
// human owner; scans only a tree at rest, once per rest period; a verdict
// stays inside the tree (a neighbour is never touched), goes through a
// human decision below the trust tier, reopens a done-without-proof issue
// citing the criterion, records a legitimate stop, and escalates to the
// owner on the third relaunch.

func watchdogOutput(verdict, summary, findings string) string {
	return "Report.\n```watchdog_verdict\n{\"verdict\":\"" + verdict + "\",\"summary\":\"" + summary + "\",\"findings\":[" + findings + "]}\n```\n"
}

func TestTaskWatchdog(t *testing.T) {
	ctx := context.Background()
	assignee := dbfx.Agent(t, "wd assignee "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	watchdog := dbfx.Agent(t, "wd watchdog "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	root := dbfx.Issue(t, "wd root "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": assignee})
	child := dbfx.Issue(t, "wd child done", testutil.Cols{"status": "done", "parent_issue_id": root, "acceptance_criteria": `[{"id":"c1","text":"tests pass","proof_state":"unproven"}]`})
	grandchild := dbfx.Issue(t, "wd grandchild blocked", testutil.Cols{"status": "blocked", "parent_issue_id": child})
	neighbour := dbfx.Issue(t, "wd neighbour done", testutil.Cols{"status": "done"})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM watchdog_verdict WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_watchdog WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, root)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, root)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'watchdog_escalation'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id IN ($1, $2, $3)`, root, child, grandchild)
	})
	var childNumber int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, child).Scan(&childNumber)
	prefix := testHandler.getIssuePrefix(ctx, parseUUID(testWorkspaceID))

	// Configuration: a different agent than the assignee, the caller as owner.
	testutil.Call(t, testHandler.SetIssueWatchdog, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+root+"/watchdog", map[string]any{"agent_id": assignee}), "id", root)).Want(http.StatusBadRequest)
	var cfg struct {
		Watchdog struct {
			ID          string `json:"id"`
			AgentID     string `json:"agent_id"`
			OwnerID     string `json:"owner_id"`
			RestMinutes int32  `json:"rest_minutes"`
		} `json:"watchdog"`
	}
	testutil.Call(t, testHandler.SetIssueWatchdog, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+root+"/watchdog", map[string]any{"agent_id": watchdog, "instructions": "Be strict about proofs.", "rest_minutes": 15}), "id", root)).Want(http.StatusOK).JSON(&cfg)
	if cfg.Watchdog.AgentID != watchdog || cfg.Watchdog.OwnerID != testUserID || cfg.Watchdog.RestMinutes != 15 {
		t.Fatalf("watchdog config = %+v", cfg.Watchdog)
	}
	testutil.Call(t, testHandler.GetIssueWatchdog, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+root+"/watchdog", nil), "id", root)).Want(http.StatusOK)

	// Not at rest yet (the tree was just created): the scheduler starts nothing.
	if n, err := testHandler.ScanWatchdogs(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("fresh tree must not be scanned: n=%d err=%v", n, err)
	}
	// In motion: a running task on the grandchild blocks even a forced scan.
	running := dbfx.Task(t, assignee, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": grandchild, "status": "running"})
	testutil.Call(t, testHandler.ScanIssueWatchdogNow, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+root+"/watchdog/scan", nil), "id", root)).Want(http.StatusConflict)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, running)

	// At rest for two hours: the scheduler starts exactly one scan, by the watchdog agent, on the root.
	dbfx.Exec(t, `UPDATE issue SET created_at = now() - interval '2 hours', updated_at = now() - interval '2 hours', last_activity_at = now() - interval '2 hours' WHERE id IN ($1, $2, $3)`, root, child, grandchild)
	dbfx.Exec(t, `UPDATE agent_task_queue SET created_at = now() - interval '2 hours' WHERE id = $1`, running)
	if n, err := testHandler.ScanWatchdogs(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("rested tree must be scanned once: n=%d err=%v", n, err)
	}
	var scanTask string
	dbfx.QueryRow(t, `SELECT last_scan_task_id FROM issue_watchdog WHERE id = $1`, cfg.Watchdog.ID).Scan(&scanTask)
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1 AND agent_id = $2 AND issue_id = $3`, scanTask, watchdog, root) != 1 {
		t.Fatal("the scan is a run of the watchdog agent on the root")
	}
	if n, _ := testHandler.ScanWatchdogs(ctx, time.Now()); n != 0 {
		t.Fatal("one scan per rest period, and never while one runs")
	}

	// Verdict: motion — reopen the child (done without proof) and a neighbour outside the tree.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, scanTask)
	testHandler.storeWatchdogVerdict(ctx, mustTask(t, scanTask), watchdogOutput("motion", "The child claims done but c1 has no proof.",
		`{"issue":"`+prefix+`-`+strconv.Itoa(childNumber)+`","action":"reopen","reason":"marked done without proof","missing_criterion":"c1"},{"issue":"`+neighbour+`","action":"reopen","reason":"out of scope"}`))
	var verdictID, verdictKind, review string
	var findings, dropped []byte
	dbfx.QueryRow(t, `SELECT id, verdict, human_review, findings, dropped FROM watchdog_verdict WHERE task_id = $1`, scanTask).Scan(&verdictID, &verdictKind, &review, &findings, &dropped)
	if verdictKind != "motion" || review != "pending" || !strings.Contains(string(findings), child) || !strings.Contains(string(dropped), neighbour) {
		t.Fatalf("verdict %s/%s findings=%s dropped=%s", verdictKind, review, findings, dropped)
	}
	// Below the tier nothing moves without a human: a decision on the root, the issues untouched.
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, child).Scan(&status)
	if status != "done" || dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE issue_id = $1 AND task_id = $2`, root, scanTask) != 1 {
		t.Fatalf("a motion verdict below the tier waits for a decision (child %s)", status)
	}
	var decisionID string
	dbfx.QueryRow(t, `SELECT decision_id FROM watchdog_verdict WHERE id = $1`, verdictID).Scan(&decisionID)
	respondDecision(t, root, decisionID, map[string]any{"option_id": "apply_watchdog"}).Want(http.StatusOK)
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, child).Scan(&status)
	var neighbourStatus string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, neighbour).Scan(&neighbourStatus)
	if status != "todo" || neighbourStatus != "done" {
		t.Fatalf("approval reopens the child (%s) and never touches the neighbour (%s)", status, neighbourStatus)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND author_id = $2 AND source_task_id = $3 AND content LIKE '%Missing criterion: c1%'`, child, watchdog, scanTask) != 1 {
		t.Fatal("the reopen comment cites the missing criterion and carries the scan run as origin")
	}
	dbfx.QueryRow(t, `SELECT human_review FROM watchdog_verdict WHERE id = $1`, verdictID).Scan(&review)
	if review != "confirmed" || dbfx.Count(t, `SELECT COUNT(*) FROM issue_watchdog WHERE id = $1 AND motion_streak = 1`, cfg.Watchdog.ID) != 1 {
		t.Fatalf("applying the decision confirms the verdict (%s) and counts the relaunch", review)
	}

	// A legitimate stop is recorded and resets the streak.
	forceScan := func() string {
		var out struct {
			TaskID string `json:"task_id"`
		}
		testutil.Call(t, testHandler.ScanIssueWatchdogNow, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+root+"/watchdog/scan", nil), "id", root)).Want(http.StatusCreated).JSON(&out)
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, out.TaskID)
		return out.TaskID
	}
	scan2 := forceScan()
	testHandler.storeWatchdogVerdict(ctx, mustTask(t, scan2), watchdogOutput("legitimate", "Blocked on a real dependency.", ""))
	if dbfx.Count(t, `SELECT COUNT(*) FROM watchdog_verdict WHERE task_id = $1 AND verdict = 'legitimate'`, scan2) != 1 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND source_task_id = $2 AND content LIKE 'Watchdog verdict: the stop is legitimate%'`, root, scan2) != 1 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM issue_watchdog WHERE id = $1 AND motion_streak = 0`, cfg.Watchdog.ID) != 1 {
		t.Fatal("a legitimate verdict is commented on the root and resets the streak")
	}

	// Third relaunch in a row: escalate to the owner instead.
	dbfx.Exec(t, `UPDATE issue_watchdog SET motion_streak = 2 WHERE id = $1`, cfg.Watchdog.ID)
	scan3 := forceScan()
	testHandler.storeWatchdogVerdict(ctx, mustTask(t, scan3), watchdogOutput("motion", "Still nothing.", `{"issue":"`+child+`","action":"ask_proof","reason":"no proof yet"}`))
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = 'watchdog_escalation' AND recipient_id = $1 AND issue_id = $2`, testUserID, root) != 1 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE task_id = $1`, scan3) != 0 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM watchdog_verdict WHERE task_id = $1 AND (applied->>'escalated')::bool`, scan3) != 1 {
		t.Fatal("the third relaunch escalates to the owner without a decision")
	}

	// The owner can overturn a verdict; the contract risk and revision are settable.
	dbfx.QueryRow(t, `SELECT id FROM watchdog_verdict WHERE task_id = $1`, scan3).Scan(&verdictID)
	testutil.Call(t, testHandler.ReviewWatchdogVerdict, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/watchdog-verdicts/"+verdictID+"/review", map[string]any{"confirmed": false}), "id", verdictID)).Want(http.StatusOK)
	dbfx.QueryRow(t, `SELECT human_review FROM watchdog_verdict WHERE id = $1`, verdictID).Scan(&review)
	if review != "overturned" {
		t.Fatalf("review = %s", review)
	}
	testutil.Call(t, testHandler.SetIssueContractRisk, testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+root+"/contract-risk", map[string]any{"risk": "wild"}), "id", root)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.SetIssueContractRisk, testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+root+"/contract-risk", map[string]any{"risk": "high"}), "id", root)).Want(http.StatusOK)
	if _, err := testHandler.Queries.UpdateIssueAcceptanceCriteria(ctx, db.UpdateIssueAcceptanceCriteriaParams{ID: parseUUID(root), AcceptanceCriteria: []byte(`[]`)}); err != nil {
		t.Fatal(err)
	}
	var risk string
	var revision int32
	dbfx.QueryRow(t, `SELECT contract_risk, contract_revision FROM issue WHERE id = $1`, root).Scan(&risk, &revision)
	if risk != "high" || revision != 1 {
		t.Fatalf("contract risk/revision = %s/%d", risk, revision)
	}

	// Verdicts list and delete.
	var list struct {
		Verdicts []WatchdogVerdictResponse `json:"verdicts"`
	}
	testutil.Call(t, testHandler.ListIssueWatchdogVerdicts, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+root+"/watchdog/verdicts", nil), "id", root)).Want(http.StatusOK).JSON(&list)
	if len(list.Verdicts) != 3 {
		t.Fatalf("verdicts = %d", len(list.Verdicts))
	}
	testutil.Call(t, testHandler.DeleteIssueWatchdog, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/issues/"+root+"/watchdog", nil), "id", root)).Want(http.StatusNoContent)
}
