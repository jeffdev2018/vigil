package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Per-issue A2A budget — the WIDE-loop breaker (F19 / JEF-32).
//
// Depth cannot see this loop: A speaks to B on issue 1, B answers on issue 2,
// and a third path returns to issue 1 with depth reset to 0 because the
// intermediate trigger was a human. Nothing links those runs, so the only net
// is a rate on the issue itself.

// seedA2ARuns backdates `count` A2A-triggered runs onto an issue by `ageExpr`,
// a SQL interval expression relative to now().
func seedA2ARuns(t *testing.T, issueID, agentID string, count int, ageExpr string) {
	t.Helper()
	for i := 0; i < count; i++ {
		dbfx.Task(t, agentID, testutil.Cols{
			"issue_id":   issueID,
			"runtime_id": handlerTestRuntimeID(t),
			"a2a_depth":  1,
			"status":     "completed",
			"created_at": testutil.Raw(fmt.Sprintf("now() - %s", ageExpr)),
		})
	}
}

// ACCEPTANCE 5: the 21st A2A run on an issue inside the window is refused with
// a2a_budget_exceeded, and writes nothing.
func TestA2ABudget_RefusesPastTheHourlyAllowance(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	max := service.MaxA2ARunsPerIssuePerHour()
	if max != 20 {
		t.Skipf("MULTICA_A2A_MAX_RUNS_PER_ISSUE_PER_HOUR is overridden to %d; this test pins the shipped default of 20", max)
	}
	fx := newA2AFixture(t, "budget")
	// 20 A2A runs already this hour: the allowance is spent.
	seedA2ARuns(t, fx.IssueID, fx.SenderID, int(max), "interval '1 minute'")

	resp := sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID, "intent": "question", "body": "one too many",
	}).Want(http.StatusTooManyRequests)
	if got := readReasonCode(t, resp.Body.Bytes()); got != "a2a_budget_exceeded" {
		t.Errorf("reason_code = %q, want a2a_budget_exceeded", got)
	}
	if got := countCommentsWithIntent(t, fx.IssueID); got != 0 {
		t.Fatalf("a budget-refused message wrote %d comments, want 0", got)
	}
}

// The window SLIDES: runs older than it stop counting, so a busy hour does not
// silence an issue permanently. Same 20 runs as above, aged past the window.
func TestA2ABudget_RunsOutsideTheWindowDoNotCount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	max := service.MaxA2ARunsPerIssuePerHour()
	window := service.A2ABudgetWindow()
	if max != 20 || window.Hours() != 1 {
		t.Skipf("A2A budget overridden (max=%d window=%s); this test pins the shipped defaults", max, window)
	}
	fx := newA2AFixture(t, "budget-window")
	seedA2ARuns(t, fx.IssueID, fx.SenderID, int(max)+5, "interval '2 hours'")

	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID, "intent": "question", "body": "the hour has rolled over",
	}).Want(http.StatusCreated)
}

// Ordinary human-triggered runs are NOT charged to the agent-loop budget. This
// is the other half of "a human-triggered run always has depth 0": if it were
// not true, a busy issue would lock its agents out of talking to each other.
func TestA2ABudget_HumanTriggeredRunsAreNotCharged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	max := service.MaxA2ARunsPerIssuePerHour()
	fx := newA2AFixture(t, "budget-human")
	for i := 0; i < int(max)+5; i++ {
		dbfx.Task(t, fx.SenderID, testutil.Cols{
			"issue_id": fx.IssueID, "runtime_id": handlerTestRuntimeID(t),
			"a2a_depth": 0, "status": "completed",
		})
	}
	sendA2A(t, fx.IssueID, fx.SenderID, fx.SenderTa, map[string]any{
		"to_agent_id": fx.RecvID, "intent": "review", "body": "human traffic must not count",
	}).Want(http.StatusCreated)
}
