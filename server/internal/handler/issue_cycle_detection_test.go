package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateIssueRejectsCircularParent covers UpdateIssue's cycle-detection
// walk end to end (audit P3: the walk used to call the unscoped
// h.Queries.GetIssue instead of GetIssueInWorkspace — same behavior within
// one workspace, but no longer able to silently read another workspace's
// issue row if the same-workspace parent invariant were ever broken
// elsewhere). a -> b -> c is set up, then a is retargeted to parent c,
// which would close the loop and must be rejected.
func TestUpdateIssueRejectsCircularParent(t *testing.T) {
	a := createIssueForTest(t, map[string]any{"title": "cycle a"})
	b := createIssueForTest(t, map[string]any{"title": "cycle b"})
	c := createIssueForTest(t, map[string]any{"title": "cycle c"})

	setParent := func(id, parentID string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := withURLParam(newRequest(http.MethodPut, "/api/issues/"+id, map[string]any{"parent_issue_id": parentID}), "id", id)
		testHandler.UpdateIssue(w, r)
		return w
	}

	if w := setParent(b.ID, a.ID); w.Code != http.StatusOK {
		t.Fatalf("set b's parent to a: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setParent(c.ID, b.ID); w.Code != http.StatusOK {
		t.Fatalf("set c's parent to b: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// a -> c would close the loop a -> c -> b -> a.
	if w := setParent(a.ID, c.ID); w.Code != http.StatusBadRequest {
		t.Fatalf("set a's parent to c: expected 400 (circular), got %d: %s", w.Code, w.Body.String())
	}
}

// TestBatchUpdateIssuesRejectsCircularParent is the same scenario through
// BatchUpdateIssues, which has its own cycle-detection walk.
func TestBatchUpdateIssuesRejectsCircularParent(t *testing.T) {
	a := createIssueForTest(t, map[string]any{"title": "batch cycle a"})
	b := createIssueForTest(t, map[string]any{"title": "batch cycle b"})
	c := createIssueForTest(t, map[string]any{"title": "batch cycle c"})

	batchUpdate := func(id, parentID string) {
		t.Helper()
		w := httptest.NewRecorder()
		body := map[string]any{
			"issue_ids": []string{id},
			"updates":   map[string]any{"parent_issue_id": parentID},
		}
		r := newRequest(http.MethodPost, "/api/issues/batch-update?workspace_id="+testWorkspaceID, body)
		testHandler.BatchUpdateIssues(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("BatchUpdateIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	batchUpdate(b.ID, a.ID)
	batchUpdate(c.ID, b.ID)

	// a -> c would close the loop; BatchUpdateIssues skips cycle-detected
	// updates rather than erroring the whole batch, so assert the parent
	// was NOT applied instead of asserting a request-level error status.
	batchUpdate(a.ID, c.ID)

	var parentIsSet bool
	if err := testPool.QueryRow(context.Background(), `SELECT parent_issue_id IS NOT NULL FROM issue WHERE id = $1`, a.ID).Scan(&parentIsSet); err != nil {
		t.Fatalf("read a's parent_issue_id: %v", err)
	}
	if parentIsSet {
		t.Fatalf("a's parent_issue_id was set despite the circular relationship a -> c -> b -> a")
	}
}
