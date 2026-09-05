package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Decision SLA (K35): a policy gives cards a deadline; overdue cards step
// to the substitute, then the leads; an answer stops it.

// rememberSettings restores the workspace settings when the test ends.
func rememberSettings(t *testing.T) {
	t.Helper()
	var prev string
	dbfx.QueryRow(t, `SELECT COALESCE(settings::text, '{}') FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prev)
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `UPDATE workspace SET settings = $2::jsonb WHERE id = $1`, testWorkspaceID, prev); err != nil {
			t.Errorf("restore settings: %v", err)
		}
	})
}

func setDecisionSLA(t *testing.T, deadlineMinutes int, substitute string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('decision_sla', jsonb_build_object('deadline_minutes', $2::int, 'substitute_user_id', $3::text)) WHERE id = $1`, testWorkspaceID, deadlineMinutes, substitute)
}

func TestDecisionSLAEscalatesSubstituteThenLeadsUntilAnswered(t *testing.T) {
	substitute := dbfx.User(t, "Sub", "sla-substitute-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, substitute, "member")
	rememberSettings(t)
	setDecisionSLA(t, 60, substitute)
	issue := dbfx.Issue(t, "sla escalation")
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	if created.Decision.SlaDeadlineAt == nil || created.Decision.EscalationLevel != 0 {
		t.Fatalf("card = %+v, want a deadline and level 0", created.Decision)
	}
	deadline, _ := time.Parse(time.RFC3339, *created.Decision.SlaDeadlineAt)
	if d := time.Until(deadline); d < 55*time.Minute || d > 65*time.Minute {
		t.Fatalf("deadline in %s, want about an hour", d)
	}

	// Before the deadline nothing moves.
	if n, err := testHandler.EscalateOverdueDecisions(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("early tick moved %d (err %v), want 0", n, err)
	}
	// Past it: level 1, the substitute alone hears about it.
	if n, err := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(61*time.Minute)); err != nil || n != 1 {
		t.Fatalf("first tick moved %d (err %v), want 1", n, err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_escalated' AND recipient_id = $2`, issue, substitute); n != 1 {
		t.Fatalf("substitute escalation items = %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_escalated' AND recipient_id = $2`, issue, testUserID); n != 0 {
		t.Fatalf("lead escalation items after level 1 = %d, want 0", n)
	}
	var level int32
	var nextDeadline *time.Time
	dbfx.QueryRow(t, `SELECT escalation_level, sla_deadline_at FROM issue_decision WHERE id = $1`, created.Decision.ID).Scan(&level, &nextDeadline)
	if level != 1 || nextDeadline == nil {
		t.Fatalf("after level 1: level %d deadline %v", level, nextDeadline)
	}
	// The same tick again is a no-op: the next deadline is an hour away.
	if n, _ := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(62*time.Minute)); n != 0 {
		t.Fatalf("repeat tick moved %d, want 0", n)
	}
	// One more deadline later: level 2, the leads hear about it, and the
	// attention inbox lists the escalation.
	if n, _ := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(125*time.Minute)); n != 1 {
		t.Fatalf("second tick moved %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_escalated' AND recipient_id = $2`, issue, testUserID); n != 1 {
		t.Fatalf("lead escalation items after level 2 = %d, want 1", n)
	}
	found := false
	for _, item := range listAttention(t) {
		if item.Type == "decision_escalated" && item.IssueID != nil && *item.IssueID == issue && item.Reason == "decision_escalated" {
			found = true
		}
	}
	if !found {
		t.Fatal("the escalation must reach the attention inbox")
	}
	// Level 2 is the end of the chain.
	if n, _ := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(300*time.Minute)); n != 0 {
		t.Fatalf("third tick moved %d, want 0", n)
	}

	// Answering closes it and clears the attention inbox of it.
	var answered decisionEnvelope
	respondDecision(t, issue, created.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK).JSON(&answered)
	if answered.Decision.EscalationLevel != 2 || answered.Decision.EscalatedAt == nil {
		t.Fatalf("answered card = %+v, want the escalation history kept", answered.Decision)
	}
	for _, item := range listAttention(t) {
		if item.Type == "decision_escalated" && item.IssueID != nil && *item.IssueID == issue {
			t.Fatal("an answered escalation must leave the attention inbox")
		}
	}
}

func TestDecisionSLAWithoutPolicyOrSubstitute(t *testing.T) {
	rememberSettings(t)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) - 'decision_sla' WHERE id = $1`, testWorkspaceID)
	issue := dbfx.Issue(t, "sla none")
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	if created.Decision.SlaDeadlineAt != nil {
		t.Fatalf("card without a policy = %+v, want no deadline", created.Decision)
	}
	if n, _ := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(24*time.Hour)); n != 0 {
		t.Fatalf("tick moved %d without a policy, want 0", n)
	}

	// A policy without a substitute (or with one who left) goes to the leads
	// at level 1.
	setDecisionSLA(t, 30, uuid.NewString())
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	if n, _ := testHandler.EscalateOverdueDecisions(t.Context(), time.Now().Add(31*time.Minute)); n != 1 {
		t.Fatalf("tick moved %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_escalated' AND recipient_id = $2`, issue, testUserID); n != 1 {
		t.Fatalf("lead items with an unknown substitute = %d, want 1", n)
	}
	_ = testutil.Cols{}
}
