package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// F17: the done gate. It only bites with the workspace setting on, on a
// done-category status, and with a critical finding on the ACTIVE plan.

func setPlanVerificationGate(t *testing.T, on bool) {
	t.Helper()
	var previous []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('plan_verification_gate', $2::boolean) WHERE id = $1`, testWorkspaceID, on)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
}

func setIssueStatus(t *testing.T, issueID, status string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID,
	))
}

func TestPlanVerificationGateBlocksDoneOnCritical(t *testing.T) {
	setPlanVerificationGate(t, true)
	issue := dbfx.Issue(t, "gate critical", testutil.Cols{"status": "in_review"})
	_, taskID := seedVerification(t, issue)
	reportVerification(t, issue, taskID, []map[string]any{{"severity": "critical", "title": "Missing"}}).Want(http.StatusOK)

	resp := setIssueStatus(t, issue, "done").Want(http.StatusConflict)
	if code, _ := resp.Map()["code"].(string); code != ErrCodePlanVerificationCritical {
		t.Fatalf("409 code = %q, want %q (body %s)", code, ErrCodePlanVerificationCritical, resp.Text())
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "in_review" {
		t.Fatalf("status after refused move = %q, want untouched", status)
	}
	// Any other category still moves.
	setIssueStatus(t, issue, "in_progress").Want(http.StatusOK)

	// The batch path is gated the same way.
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue},
		"updates":   map[string]any{"status": "done"},
	})).Want(http.StatusConflict)
}

func TestPlanVerificationGateLetsMinorAndNewPlanThrough(t *testing.T) {
	setPlanVerificationGate(t, true)
	minor := dbfx.Issue(t, "gate minor", testutil.Cols{"status": "in_review"})
	_, taskID := seedVerification(t, minor)
	reportVerification(t, minor, taskID, []map[string]any{{"severity": "minor", "title": "Naming"}, {"severity": "major", "title": "Partial"}}).Want(http.StatusOK)
	setIssueStatus(t, minor, "done").Want(http.StatusOK)

	// A critical report on a plan that was since superseded no longer blocks.
	superseded := dbfx.Issue(t, "gate superseded", testutil.Cols{"status": "in_review"})
	_, taskID2 := seedVerification(t, superseded)
	reportVerification(t, superseded, taskID2, []map[string]any{{"severity": "critical", "title": "Missing"}}).Want(http.StatusOK)
	setIssueStatus(t, superseded, "done").Want(http.StatusConflict)
	putPlan(t, superseded, "revised plan")
	setIssueStatus(t, superseded, "done").Want(http.StatusOK)
}

func TestPlanVerificationGateOffIgnoresCritical(t *testing.T) {
	setPlanVerificationGate(t, false)
	issue := dbfx.Issue(t, "gate off", testutil.Cols{"status": "in_review"})
	_, taskID := seedVerification(t, issue)
	reportVerification(t, issue, taskID, []map[string]any{{"severity": "critical", "title": "Missing"}}).Want(http.StatusOK)
	setIssueStatus(t, issue, "done").Want(http.StatusOK)
}
