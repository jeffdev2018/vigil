package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Outcome Contract (K12): a criterion without proof keeps the issue out of
// done; proofs survive a list edit; legacy strings read as unproven.

type criteriaEnvelope struct {
	Criteria []AcceptanceCriterion `json:"criteria"`
}

func setCriteria(t *testing.T, issueID string, criteria []map[string]any, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPut, "/api/issues/"+issueID+"/acceptance-criteria", map[string]any{"criteria": criteria})
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.SetAcceptanceCriteria, testutil.WithURLParams(req, "id", issueID))
}

func proveCriterion(t *testing.T, issueID, criterionID string, body map[string]any, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPatch, "/api/issues/"+issueID+"/acceptance-criteria/"+criterionID+"/proof", body)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.ProveAcceptanceCriterion, testutil.WithURLParams(req, "id", issueID, "criterionId", criterionID))
}

// criteriaOf decodes into a fresh envelope each time: json.Unmarshal into a
// reused slice keeps stale fields on its elements.
func criteriaOf(t *testing.T, resp *testutil.Response) []AcceptanceCriterion {
	t.Helper()
	var out criteriaEnvelope
	resp.Want(http.StatusOK).JSON(&out)
	return out.Criteria
}

func listCriteria(t *testing.T, issueID string) []AcceptanceCriterion {
	t.Helper()
	var out criteriaEnvelope
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/acceptance-criteria", nil)
	testutil.Call(t, testHandler.ListAcceptanceCriteria, testutil.WithURLParams(req, "id", issueID)).Want(http.StatusOK).JSON(&out)
	return out.Criteria
}

func moveIssue(t *testing.T, issueID, status string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID))
}

func TestAcceptanceCriteriaContractGatesDone(t *testing.T) {
	issue := dbfx.Issue(t, "outcome contract", testutil.Cols{"status": "in_review"})
	var crit []AcceptanceCriterion
	crit = criteriaOf(t, setCriteria(t, issue, []map[string]any{{"text": "Tests pass"}, {"text": "Wording reviewed"}}))
	if len(crit) != 2 || crit[0].ProofState != ProofStateMissing || crit[0].ID == "" {
		t.Fatalf("criteria = %+v, want two unproven criteria with ids", crit)
	}
	tests, wording := crit[0].ID, crit[1].ID

	var refused struct {
		Code     string                `json:"code"`
		Criteria []AcceptanceCriterion `json:"criteria"`
	}
	moveIssue(t, issue, "done").Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodeUnsatisfiedAcceptanceCriteria || len(refused.Criteria) != 2 {
		t.Fatalf("refusal = %+v, want code %s naming both criteria", refused, ErrCodeUnsatisfiedAcceptanceCriteria)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "in_review" {
		t.Fatalf("status after refused move = %q, want untouched", status)
	}
	// Not-done moves are never gated.
	moveIssue(t, issue, "in_progress").Want(http.StatusOK)

	// An agent proves the test criterion; its human_validation claim on the
	// other only reaches pending_human.
	agent := []string{"X-Actor-Source", "task_token"}
	proveCriterion(t, issue, tests, map[string]any{"proof_type": "test", "proof_ref": "go test ./internal/handler"}, agent...).Want(http.StatusOK)
	crit = criteriaOf(t, proveCriterion(t, issue, wording, map[string]any{"proof_type": "human_validation"}, agent...))
	if crit[0].ProofState != ProofStateSatisfied || crit[1].ProofState != ProofStatePendingHuman || crit[1].ValidatedBy != "" {
		t.Fatalf("after agent proofs = %+v", crit)
	}
	moveIssue(t, issue, "done").Want(http.StatusConflict).JSON(&refused)
	if len(refused.Criteria) != 1 || refused.Criteria[0].ID != wording {
		t.Fatalf("refusal = %+v, want only the pending criterion", refused.Criteria)
	}

	// The human's own click satisfies it and records who.
	crit = criteriaOf(t, proveCriterion(t, issue, wording, map[string]any{"proof_type": "human_validation"}))
	if crit[1].ProofState != ProofStateSatisfied || crit[1].ValidatedBy != testUserID {
		t.Fatalf("after human validation = %+v", crit[1])
	}
	moveIssue(t, issue, "done").Want(http.StatusOK)

	// Validation: unknown proof type, missing ref, unknown criterion.
	proveCriterion(t, issue, tests, map[string]any{"proof_type": "vibes", "proof_ref": "x"}).Want(http.StatusBadRequest)
	proveCriterion(t, issue, tests, map[string]any{"proof_type": "url"}).Want(http.StatusBadRequest)
	proveCriterion(t, issue, "nope", map[string]any{"proof_type": "url", "proof_ref": "https://x"}).Want(http.StatusNotFound)
}

func TestAcceptanceCriteriaEditKeepsProofsOfUnchangedText(t *testing.T) {
	issue := dbfx.Issue(t, "outcome contract edit")
	var crit []AcceptanceCriterion
	crit = criteriaOf(t, setCriteria(t, issue, []map[string]any{{"text": "A"}, {"text": "B"}}))
	a, b := crit[0].ID, crit[1].ID
	proveCriterion(t, issue, a, map[string]any{"proof_type": "url", "proof_ref": "https://ci/1"}).Want(http.StatusOK)
	proveCriterion(t, issue, b, map[string]any{"proof_type": "url", "proof_ref": "https://ci/2"}).Want(http.StatusOK)

	// Same id + same text keeps the proof; id-less same text (the CLI's case)
	// keeps it too; a reworded criterion starts over; a new one is added.
	setCriteria(t, issue, []map[string]any{{"id": a, "text": "A"}, {"text": "B"}, {"id": a, "text": "A reworded"}, {"text": "C"}}).Want(http.StatusBadRequest)
	crit = criteriaOf(t, setCriteria(t, issue, []map[string]any{{"id": a, "text": "A"}, {"text": "B"}, {"text": "C"}}))
	if len(crit) != 3 || crit[0].ID != a || crit[0].ProofState != ProofStateSatisfied || crit[1].ID != b || crit[1].ProofState != ProofStateSatisfied || crit[2].ProofState != ProofStateMissing {
		t.Fatalf("after edit = %+v", crit)
	}
	crit = criteriaOf(t, setCriteria(t, issue, []map[string]any{{"id": a, "text": "A reworded"}}))
	if len(crit) != 1 || crit[0].ID != a || crit[0].ProofState != ProofStateMissing || crit[0].ProofRef != "" {
		t.Fatalf("after rewording = %+v", crit)
	}
	// Clearing a proof.
	proveCriterion(t, issue, a, map[string]any{"proof_type": "url", "proof_ref": "https://ci/3"}).Want(http.StatusOK)
	crit = criteriaOf(t, proveCriterion(t, issue, a, map[string]any{"proof_type": ""}))
	if crit[0].ProofState != ProofStateMissing {
		t.Fatalf("after clearing = %+v", crit[0])
	}
}

func TestAcceptanceCriteriaLegacyAndMalformedRows(t *testing.T) {
	// A pre-K12 row of bare strings: unproven criteria with positional ids,
	// and they gate done like any other.
	legacy := dbfx.Issue(t, "legacy criteria", testutil.Cols{"status": "in_review", "acceptance_criteria": `["Old promise", "", 42]`})
	got := listCriteria(t, legacy)
	if len(got) != 1 || got[0].ID != "c1" || got[0].Text != "Old promise" || got[0].ProofState != ProofStateMissing {
		t.Fatalf("legacy criteria = %+v", got)
	}
	moveIssue(t, legacy, "done").Want(http.StatusConflict)
	proveCriterion(t, legacy, "c1", map[string]any{"proof_type": "file", "proof_ref": "docs/old.md"}).Want(http.StatusOK)
	moveIssue(t, legacy, "done").Want(http.StatusOK)

	// A row claiming satisfied without a proof behind it is not believed.
	claimed := dbfx.Issue(t, "claimed criteria", testutil.Cols{"status": "in_review", "acceptance_criteria": `[{"id":"x","text":"Trust me","proof_state":"satisfied"}]`})
	if got := listCriteria(t, claimed); got[0].ProofState != ProofStateMissing {
		t.Fatalf("claimed = %+v, want missing", got[0])
	}
	moveIssue(t, claimed, "done").Want(http.StatusConflict)

	// Not an array at all: no criteria, no crash, no gate.
	odd := dbfx.Issue(t, "odd criteria", testutil.Cols{"status": "in_review", "acceptance_criteria": `{"not":"a list"}`})
	if got := listCriteria(t, odd); len(got) != 0 {
		t.Fatalf("odd = %+v, want none", got)
	}
	moveIssue(t, odd, "done").Want(http.StatusOK)
}

func TestAcceptanceCriteriaGateBatchUpdate(t *testing.T) {
	issue := dbfx.Issue(t, "batch gated", testutil.Cols{"status": "in_review", "acceptance_criteria": `["Unproven"]`})
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue},
		"updates":   map[string]any{"status": "done"},
	})).Want(http.StatusConflict)
}
