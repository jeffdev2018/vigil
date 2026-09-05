package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
)

// newPendingTriageItem inserts one visible pending item. Titles must be
// unique per source: uq_triage_item_pending_title collapses repeats.
func newPendingTriageItem(t *testing.T, title string, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id":     testWorkspaceID,
		"source_id":        uuid.NewString(),
		"origin_type":      "autopilot",
		"title":            title,
		"normalized_title": title,
		"state":            triage.StatePending,
		"shadow":           false,
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Insert(t, "triage_item", cols)
}

func TestTriageMergeFoldsItemIntoIssueAndComments(t *testing.T) {
	issueID := dbfx.Issue(t, "Payments already tracked")
	itemID := newPendingTriageItem(t, "merge target "+uuid.NewString())

	var out struct {
		State              string `json:"state"`
		DuplicateOfIssueID string `json:"duplicate_of_issue_id"`
	}
	testutil.Call(t, testHandler.MergeTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/merge", map[string]any{"issue_id": issueID}),
		"id", itemID,
	)).Want(http.StatusOK).JSON(&out)

	if out.State != triage.StateMerged || out.DuplicateOfIssueID != issueID {
		t.Fatalf("merge response = %+v, want merged into %s", out, issueID)
	}

	var state, dupID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state, COALESCE(duplicate_of_issue_id::text, '') FROM triage_item WHERE id = $1`, itemID,
	).Scan(&state, &dupID); err != nil {
		t.Fatalf("load merged item: %v", err)
	}
	if state != triage.StateMerged || dupID != issueID {
		t.Fatalf("item = %s / %s, want merged / %s", state, dupID, issueID)
	}

	// The merge is only visible where the work lives if the target issue says so.
	var content, authorType string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content, author_type FROM comment WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, issueID,
	).Scan(&content, &authorType); err != nil {
		t.Fatalf("load merge comment: %v", err)
	}
	if authorType != "system" || content == "" || content[:len("Merged from triage: ")] != "Merged from triage: " {
		t.Fatalf("comment = %s/%q, want a system 'Merged from triage:' notice", authorType, content)
	}

	// A second merge finds nothing pending.
	testutil.Call(t, testHandler.MergeTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/merge", map[string]any{"issue_id": issueID}),
		"id", itemID,
	)).Want(http.StatusConflict)
}

func TestTriageMergeRejectsMissingIssue(t *testing.T) {
	itemID := newPendingTriageItem(t, "merge no target "+uuid.NewString())

	testutil.Call(t, testHandler.MergeTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/merge", map[string]any{}),
		"id", itemID,
	)).Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.MergeTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/merge",
			map[string]any{"issue_id": uuid.NewString()}),
		"id", itemID,
	)).Want(http.StatusNotFound)
}
