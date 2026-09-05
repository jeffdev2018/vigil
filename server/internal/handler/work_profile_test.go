package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Vigil learns you (K71): five identical answers make a rule; the sixth
// decision arrives with the hint; the rule decides alone only once the
// person switched it on, never on a high-stakes decision nor for a
// non-autonomous asker; an overturned auto-decision demotes the rule; the
// person can forget what was learned.

func TestWorkProfileLearning(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "learn agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "learn issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	// Every human answer in the suite teaches this user something; start clean.
	testPool.Exec(ctx, `DELETE FROM decision_training_example WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	testPool.Exec(ctx, `DELETE FROM work_profile_observation WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM decision_training_example WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM work_profile_observation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND (type = 'decision_auto_decided' OR issue_id = $2)`, testWorkspaceID, issue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
	})
	ask := func(question string) string {
		return dbfx.Insert(t, "issue_decision", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "issue_id": issue, "asked_by_type": "agent", "asked_by_id": agent,
			"question": question, "options": `[{"id":"yes","label":"Yes, go"},{"id":"no","label":"No"}]`, "urgency": "normal"})
	}
	type decisionsOut struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
	}
	listDecisions := func() decisionsOut {
		var out decisionsOut
		testutil.Call(t, testHandler.ListIssueDecisions, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/decisions", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
		return out
	}

	// Five identical answers: the rule exists, but four examples give no hint yet.
	for i := 0; i < 5; i++ {
		d := ask("Deploy to staging?")
		if i == 4 {
			for _, r := range listDecisions().Decisions {
				if r.ID == d && r.Learned != nil {
					t.Fatal("four examples are below the hint threshold")
				}
			}
		}
		respondDecision(t, issue, d, map[string]any{"option_id": "yes"}).Want(http.StatusOK)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM decision_training_example WHERE workspace_id = $1 AND user_id = $2 AND option_id = 'yes'`, testWorkspaceID, testUserID) != 5 {
		t.Fatal("every human answer is a training example")
	}
	// The sixth arrives pre-filled with the counter, auto off by default.
	sixth := ask("Deploy to staging?")
	var hint *DecisionHint
	for _, r := range listDecisions().Decisions {
		if r.ID == sixth {
			hint = r.Learned
		}
	}
	if hint == nil || hint.OptionID != "yes" || hint.Count != 5 || hint.Total != 5 || hint.Auto || hint.Stake != "normal" {
		t.Fatalf("hint on the sixth decision = %+v", hint)
	}
	// Not decided alone while auto is off.
	testHandler.notifyDecisionRequested(ctx, mustIssue(t, issue), mustDecision(t, sixth), "agent", agent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE id = $1 AND responded_at IS NOT NULL`, sixth) != 0 {
		t.Fatal("a rule never decides alone before the person switches it on")
	}
	respondDecision(t, issue, sixth, map[string]any{"option_id": "yes"}).Want(http.StatusOK)

	// The page: the rule and the decision-hour histogram, with the review load.
	var profile WorkProfileResponse
	testutil.Call(t, testHandler.GetMyWorkProfile, newRequest(http.MethodGet, "/api/work-profile", nil)).Want(http.StatusOK).JSON(&profile)
	var ruleID string
	for _, o := range profile.Observations {
		if o.Kind == "decision_rule" {
			ruleID = o.ID
		}
	}
	if ruleID == "" || profile.Examples != 6 || profile.ReviewLoadSeconds != 6*workProfileReviewSeconds || len(profile.AdaptationSurface) == 0 {
		t.Fatalf("profile = %+v", profile)
	}
	// Switch the rule on: the next similar decision is decided for the person and notified.
	testutil.Call(t, testHandler.PatchWorkProfileObservation, testutil.WithURLParams(newRequest(http.MethodPatch, "/api/work-profile/"+ruleID, map[string]any{"auto": true}), "id", ruleID)).Want(http.StatusOK)
	seventh := ask("Deploy to staging?")
	testHandler.notifyDecisionRequested(ctx, mustIssue(t, issue), mustDecision(t, seventh), "agent", agent)
	var responded, option string
	dbfx.QueryRow(t, `SELECT COALESCE(responded_by_id::text, ''), COALESCE(response->>'option_id', '') FROM issue_decision WHERE id = $1`, seventh).Scan(&responded, &option)
	if responded != testUserID || option != "yes" {
		t.Fatalf("auto-decision: responded_by=%s option=%s", responded, option)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = 'decision_auto_decided' AND recipient_id = $1 AND issue_id = $2`, testUserID, issue) != 1 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM decision_training_example WHERE decision_id = $1 AND auto`, seventh) != 1 {
		t.Fatal("an auto-decision is notified and recorded as auto")
	}
	// High stakes are never automated, whatever the rule says.
	gate := ask("Blocked action · send the invoice by email")
	testHandler.notifyDecisionRequested(ctx, mustIssue(t, issue), mustDecision(t, gate), "agent", agent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE id = $1 AND responded_at IS NOT NULL`, gate) != 0 {
		t.Fatal("a high-stakes decision is never auto-approved")
	}
	// Nor is a decision asked by a non-autonomous agent.
	dbfx.Exec(t, `UPDATE agent SET trust_mode = 'propose' WHERE id = $1`, agent)
	proposeD := ask("Deploy to staging?")
	testHandler.notifyDecisionRequested(ctx, mustIssue(t, issue), mustDecision(t, proposeD), "agent", agent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE id = $1 AND responded_at IS NOT NULL`, proposeD) != 0 {
		t.Fatal("the Trust Dial gates auto-decisions")
	}
	dbfx.Exec(t, `UPDATE agent SET trust_mode = 'autonomous' WHERE id = $1`, agent)

	// Overturning the auto-decision lowers the rule and demotes it (1/1 > 20 %).
	var exampleID string
	dbfx.QueryRow(t, `SELECT id FROM decision_training_example WHERE decision_id = $1`, seventh).Scan(&exampleID)
	testutil.Call(t, testHandler.OverturnDecisionExample, testutil.WithURLParams(newRequest(http.MethodPost, "/api/decision-examples/"+exampleID+"/overturn", nil), "id", exampleID)).Want(http.StatusOK).JSON(&profile)
	var corrections int32
	var auto bool
	var state string
	dbfx.QueryRow(t, `SELECT corrections, auto, state FROM work_profile_observation WHERE id = $1`, ruleID).Scan(&corrections, &auto, &state)
	if corrections != 1 || auto || state != "proposed" || profile.Overturned != 1 {
		t.Fatalf("after overturn: corrections=%d auto=%v state=%s overturned=%d", corrections, auto, state, profile.Overturned)
	}
	// A high-stakes rule cannot be switched on; forgetting removes the observation.
	gateID := ask("Blocked action · pay the supplier")
	respondDecision(t, issue, gateID, map[string]any{"option_id": "yes"}).Want(http.StatusOK)
	var gateRule string
	dbfx.QueryRow(t, `SELECT id FROM work_profile_observation WHERE workspace_id = $1 AND user_id = $2 AND key LIKE 'decision:gate:%'`, testWorkspaceID, testUserID).Scan(&gateRule)
	testutil.Call(t, testHandler.PatchWorkProfileObservation, testutil.WithURLParams(newRequest(http.MethodPatch, "/api/work-profile/"+gateRule, map[string]any{"auto": true}), "id", gateRule)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DeleteWorkProfileObservation, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/work-profile/"+ruleID, nil), "id", ruleID)).Want(http.StatusNoContent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM work_profile_observation WHERE id = $1`, ruleID) != 0 {
		t.Fatal("forgetting removes the observation")
	}
}

func mustDecision(t *testing.T, id string) db.IssueDecision {
	t.Helper()
	var issueID string
	dbfx.QueryRow(t, `SELECT issue_id FROM issue_decision WHERE id = $1`, id).Scan(&issueID)
	d, err := testHandler.Queries.GetIssueDecision(context.Background(), db.GetIssueDecisionParams{ID: parseUUID(id), IssueID: parseUUID(issueID)})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
