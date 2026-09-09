package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func callCreateDependency(t *testing.T, issueID, targetID, depType string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.CreateIssueDependency, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/dependencies", map[string]any{
			"target_issue_id": targetID,
			"type":            depType,
		}),
		"id", issueID,
	))
}

func callListDependencies(t *testing.T, issueID string) IssueDependenciesResponse {
	t.Helper()
	var out IssueDependenciesResponse
	testutil.Call(t, testHandler.ListIssueDependencies, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/dependencies", nil),
		"id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

func issueRevision(t *testing.T, issueID string) int64 {
	t.Helper()
	var rev int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&rev)
	return rev
}

func TestCreateIssueDependencyRejectsSelfReference(t *testing.T) {
	a := dbfx.Issue(t, "dependency self A")
	callCreateDependency(t, a, a, "blocks").Want(http.StatusBadRequest)
}

func TestCreateIssueDependencyRejectsUnknownType(t *testing.T) {
	a := dbfx.Issue(t, "dependency type A")
	b := dbfx.Issue(t, "dependency type B")
	callCreateDependency(t, a, b, "depends").Want(http.StatusBadRequest)
}

func TestCreateIssueDependencyRejectsOtherWorkspace(t *testing.T) {
	a := dbfx.Issue(t, "dependency isolation A")
	otherWorkspace := dbfx.Workspace(t, "Dependency isolation", "dep-isolation-"+uuid.NewString())
	foreign := dbfx.Issue(t, "dependency isolation foreign", testutil.Cols{"workspace_id": otherWorkspace})

	callCreateDependency(t, a, foreign, "blocks").Want(http.StatusNotFound)
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM issue_dependency WHERE issue_id = $1 OR depends_on_issue_id = $1`, a); got != 0 {
		t.Fatalf("dependencies on A after a rejected create = %d, want 0", got)
	}
}

func TestCreateIssueDependencyRejectsDuplicate(t *testing.T) {
	a := dbfx.Issue(t, "dependency duplicate A")
	b := dbfx.Issue(t, "dependency duplicate B")

	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated)
	callCreateDependency(t, a, b, "blocks").Want(http.StatusConflict)
	// blocked_by is the same stored row read from the other side.
	callCreateDependency(t, b, a, "blocked_by").Want(http.StatusConflict)

	callCreateDependency(t, a, b, "related").Want(http.StatusCreated)
	callCreateDependency(t, b, a, "related").Want(http.StatusConflict)
}

func TestCreateIssueDependencyRejectsCycle(t *testing.T) {
	a := dbfx.Issue(t, "dependency cycle A")
	b := dbfx.Issue(t, "dependency cycle B")
	c := dbfx.Issue(t, "dependency cycle C")

	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated)
	// Direct reversal.
	callCreateDependency(t, b, a, "blocks").Want(http.StatusConflict)
	callCreateDependency(t, a, b, "blocked_by").Want(http.StatusConflict)

	// Transitive: A → B → C, then C blocks A closes the loop.
	callCreateDependency(t, b, c, "blocks").Want(http.StatusCreated)
	callCreateDependency(t, c, a, "blocks").Want(http.StatusConflict)
	// `related` never participates in cycles.
	callCreateDependency(t, c, a, "related").Want(http.StatusCreated)
}

func TestIssueDependencyRoundTrip(t *testing.T) {
	a := dbfx.Issue(t, "dependency round-trip A")
	b := dbfx.Issue(t, "dependency round-trip B", testutil.Cols{"status": "done"})
	revA, revB := issueRevision(t, a), issueRevision(t, b)

	var created IssueDependencyResponse
	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated).JSON(&created)
	if created.Type != "blocks" || created.Issue.ID != b {
		t.Fatalf("created = %+v, want type blocks on issue %s", created, b)
	}

	fromA := callListDependencies(t, a)
	if len(fromA.Blocks) != 1 || fromA.Blocks[0].Issue.ID != b || len(fromA.BlockedBy) != 0 {
		t.Fatalf("A dependencies = %+v, want blocks [B]", fromA)
	}
	if fromA.Blocks[0].Issue.Status != "done" {
		t.Errorf("embedded issue status = %q, want the target's current status", fromA.Blocks[0].Issue.Status)
	}
	fromB := callListDependencies(t, b)
	if len(fromB.BlockedBy) != 1 || fromB.BlockedBy[0].Issue.ID != a || len(fromB.Blocks) != 0 {
		t.Fatalf("B dependencies = %+v, want blocked_by [A]", fromB)
	}
	if fromA.Blocks[0].ID != fromB.BlockedBy[0].ID {
		t.Errorf("both sides must expose the same dependency id: %s vs %s", fromA.Blocks[0].ID, fromB.BlockedBy[0].ID)
	}
	if issueRevision(t, a) <= revA || issueRevision(t, b) <= revB {
		t.Errorf("creating a dependency must bump both revisions so issue:updated is admitted")
	}

	// Deleting from B's side removes the shared row.
	depID := fromB.BlockedBy[0].ID
	testutil.Call(t, testHandler.DeleteIssueDependency, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/issues/"+b+"/dependencies/"+depID, nil),
		"id", b, "depId", depID,
	)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.DeleteIssueDependency, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/issues/"+b+"/dependencies/"+depID, nil),
		"id", b, "depId", depID,
	)).Want(http.StatusNotFound)

	if after := callListDependencies(t, a); len(after.Blocks) != 0 {
		t.Fatalf("A dependencies after delete = %+v, want none", after)
	}
}

func TestDeleteIssueDependencyRequiresOwnership(t *testing.T) {
	a := dbfx.Issue(t, "dependency ownership A")
	b := dbfx.Issue(t, "dependency ownership B")
	stranger := dbfx.Issue(t, "dependency ownership stranger")

	var created IssueDependencyResponse
	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated).JSON(&created)

	testutil.Call(t, testHandler.DeleteIssueDependency, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/issues/"+stranger+"/dependencies/"+created.ID, nil),
		"id", stranger, "depId", created.ID,
	)).Want(http.StatusNotFound)
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM issue_dependency WHERE id = $1`, created.ID); got != 1 {
		t.Fatalf("dependency rows after a stranger's delete = %d, want 1", got)
	}
}

// TestListIssueDependencyStackIsBounded pins the ceiling the anti-cycle check
// and the PR stack (F10) rely on: a chain longer than the bound is cut, not
// walked forever.
func TestListIssueDependencyStackIsBounded(t *testing.T) {
	const chain = 12
	ids := make([]string, chain)
	for i := range ids {
		ids[i] = dbfx.Issue(t, fmt.Sprintf("dependency stack %d", i))
	}
	for i := 0; i+1 < chain; i++ {
		callCreateDependency(t, ids[i], ids[i+1], "blocks").Want(http.StatusCreated)
	}

	stack, err := testHandler.Queries.ListIssueDependencyStack(context.Background(), db.ListIssueDependencyStackParams{
		IssueID:  parseUUID(ids[0]),
		MaxDepth: issueDependencyStackDepth,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stack) != issueDependencyStackDepth {
		t.Fatalf("stack size = %d, want the %d-level bound", len(stack), issueDependencyStackDepth)
	}
	for i, row := range stack {
		if row.Depth != int32(i+1) || uuidToString(row.IssueID) != ids[i+1] {
			t.Fatalf("stack[%d] = (%s, %d), want (%s, %d)", i, uuidToString(row.IssueID), row.Depth, ids[i+1], i+1)
		}
	}
}

// F30 additions: the unique index that makes a duplicate edge impossible even
// under a race, and the bulk endpoint the Gantt's arrow layer reads.

// Acceptance 8 (second half): the handler's pre-insert existence check races,
// so the guarantee lives in uq_issue_dependency_edge. Inserted directly, past
// the handler, because a handler-level duplicate is already a 409 — this test
// is about what happens when two racers both get past that check.
func TestIssueDependencyUniqueIndexRejectsDuplicateEdge(t *testing.T) {
	ctx := context.Background()
	a := dbfx.Issue(t, "dup edge A")
	b := dbfx.Issue(t, "dup edge B")
	first, err := testHandler.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
		IssueID: parseUUID(a), DependsOnIssueID: parseUUID(b), Type: "blocks",
	})
	if err != nil {
		t.Fatalf("first edge: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_dependency WHERE id = $1`, first.ID)
	})
	if _, err := testHandler.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
		IssueID: parseUUID(a), DependsOnIssueID: parseUUID(b), Type: "blocks",
	}); err == nil {
		t.Fatal("a second identical edge must be refused by the unique index")
	} else if !isUniqueViolation(err) {
		t.Fatalf("expected a unique violation, got %v", err)
	}
}

func TestListIssueDependenciesBulkReturnsEdgesInsideTheCanvas(t *testing.T) {
	a := dbfx.Issue(t, "bulk dep A")
	b := dbfx.Issue(t, "bulk dep B")
	outside := dbfx.Issue(t, "bulk dep outside")
	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated)
	callCreateDependency(t, a, outside, "blocks").Want(http.StatusCreated)

	var out struct {
		Dependencies []IssueDependencyEdge `json:"dependencies"`
		Total        int                   `json:"total"`
	}
	testutil.Call(t, testHandler.ListIssueDependenciesBulk,
		newRequest(http.MethodPost, "/api/issue-dependencies/bulk", map[string]any{
			"issue_ids": []string{a, b},
		})).Want(http.StatusOK).JSON(&out)

	if len(out.Dependencies) != 1 {
		t.Fatalf("only the edge with BOTH ends on the canvas should come back, got %+v", out.Dependencies)
	}
	edge := out.Dependencies[0]
	if edge.From != a || edge.To != b || edge.Type != "blocks" {
		t.Errorf("edge should be %s blocks %s, got %+v", a, b, edge)
	}
}

// An unparseable id must not cost the whole arrow layer: the canvas hands over
// whatever it is drawing, and one bad entry is a client bug, not a reason to
// leave every arrow off the screen.
func TestListIssueDependenciesBulkSkipsUnparseableIDs(t *testing.T) {
	a := dbfx.Issue(t, "bulk dep skip A")
	b := dbfx.Issue(t, "bulk dep skip B")
	callCreateDependency(t, a, b, "blocks").Want(http.StatusCreated)

	var out struct {
		Dependencies []IssueDependencyEdge `json:"dependencies"`
	}
	testutil.Call(t, testHandler.ListIssueDependenciesBulk,
		newRequest(http.MethodPost, "/api/issue-dependencies/bulk", map[string]any{
			"issue_ids": []string{a, "not-a-uuid", b},
		})).Want(http.StatusOK).JSON(&out)
	if len(out.Dependencies) != 1 {
		t.Fatalf("expected the one valid edge, got %+v", out.Dependencies)
	}
}

// R01: duplicate is a first-class relation — accepted, listed under its own
// key, and symmetric like related (A duplicate B then B duplicate A is the
// same link seen twice, refused 409 rather than stored twice).
func TestIssueDependencyDuplicate(t *testing.T) {
	a := dbfx.Issue(t, "dependency duplicate A")
	b := dbfx.Issue(t, "dependency duplicate B")

	callCreateDependency(t, a, b, "duplicate").Want(http.StatusCreated)
	// The reverse declaration is the same link: conflict, not a second row.
	callCreateDependency(t, b, a, "duplicate").Want(http.StatusConflict)
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM issue_dependency WHERE type = 'duplicate'`); got != 1 {
		t.Fatalf("duplicate rows = %d, want 1", got)
	}

	// Both sides list it under `duplicate`.
	for _, issueID := range []string{a, b} {
		out := callListDependencies(t, issueID)
		if len(out.Duplicate) != 1 || out.Duplicate[0].Issue.Title == "" {
			t.Fatalf("duplicate list for %s = %+v, want the one link", issueID, out.Duplicate)
		}
		if len(out.Blocks) != 0 || len(out.BlockedBy) != 0 || len(out.Related) != 0 {
			t.Fatalf("duplicate leaked into another bucket: %+v", out)
		}
	}
}
