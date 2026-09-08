package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func projectMemoryRequest(method, projectID string, body any) *http.Request {
	return testutil.WithURLParams(newRequest(method, "/api/projects/"+projectID+"/memory", body), "id", projectID)
}

func TestProjectMemoryPublicationAndScope(t *testing.T) {
	projectID := dbfx.Project(t, "Shared conventions", testutil.Cols{"description": "Original description"})
	otherProjectID := dbfx.Project(t, "Other project")
	var empty ProjectMemoryResponse
	testutil.Call(t, testHandler.GetProjectMemory, projectMemoryRequest("GET", projectID, nil)).Want(http.StatusOK).JSON(&empty)
	if empty.Revision != 0 || len(empty.Rules) != 0 {
		t.Fatalf("initial memory: %+v", empty)
	}
	var published ProjectMemoryResponse
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"Use pnpm workspaces.", "Confirm the project timezone before scheduling."}, "expected_revision": 0,
	})).Want(http.StatusOK).JSON(&published)
	if published.Revision != 1 || published.ReviewedBy == nil || *published.ReviewedBy != testUserID || published.ReviewedAt == nil {
		t.Fatalf("missing review attribution: %+v", published)
	}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"Stale overwrite"}, "expected_revision": 0,
	})).Want(http.StatusConflict)
	for _, scope := range []struct {
		id     pgtype.UUID
		wanted bool
	}{
		{parseUUID(projectID), true}, {parseUUID(otherProjectID), false}, {pgtype.UUID{}, false},
	} {
		claim, err := testHandler.resolveClaimProjectContext(context.Background(), scope.id, parseUUID(testWorkspaceID))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(claim.Description, "Use pnpm workspaces."); got != scope.wanted {
			t.Fatalf("memory escaped its project: %+v", claim)
		}
		if scope.wanted && !strings.Contains(claim.Description, "revision 1") {
			t.Fatalf("claim lacks revision: %+v", claim)
		}
	}
	// A corrupt cross-tenant project reference must not leak its memory.
	foreignID := dbfx.Workspace(t, "Foreign memory workspace", "foreign-memory-workspace")
	claim, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(projectID), parseUUID(foreignID))
	if err != nil {
		t.Fatal(err)
	}
	if claim.Description != "" {
		t.Fatalf("foreign project memory leaked: %s", claim.Description)
	}
	var storedDescription string
	dbfx.QueryRow(t, `SELECT description FROM project WHERE id=$1`, projectID).Scan(&storedDescription)
	if storedDescription != "Original description" {
		t.Fatalf("memory overwrote project description: %q", storedDescription)
	}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{}, "expected_revision": 1,
	})).Want(http.StatusOK)
	claim, err = testHandler.resolveClaimProjectContext(context.Background(), parseUUID(projectID), parseUUID(testWorkspaceID))
	if err != nil || claim.Description != "Original description" {
		t.Fatalf("cleared rules still injected: %+v, %v", claim, err)
	}
}

func TestProjectMemoryRejectsUnreviewedWrites(t *testing.T) {
	projectID := dbfx.Project(t, "Memory permissions")
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := projectMemoryRequest("PUT", projectID, map[string]any{"rules": []string{"Bypass review"}, "expected_revision": 0})
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.UpdateProjectMemory, req).Want(http.StatusForbidden)
	}
	for _, body := range []map[string]any{
		{"rules": []string{"Use pnpm"}},
		{"rules": []string{""}, "expected_revision": 0},
		{"rules": []string{strings.Repeat("x", 501)}, "expected_revision": 0},
		{"rules": []string{"two\nlines"}, "expected_revision": 0},
		{"rules": make([]string, 21), "expected_revision": 0},
		{"rules": []string{}, "expected_revision": -1},
	} {
		testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, body)).Want(http.StatusBadRequest)
	}
	userID := dbfx.User(t, "Memory reader", "memory-reader@example.test")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	req := projectMemoryRequest("PUT", projectID, map[string]any{"rules": []string{"Use pnpm"}, "expected_revision": 0})
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.UpdateProjectMemory, req).Want(http.StatusForbidden)
	if _, err := testPool.Exec(context.Background(), `UPDATE project SET lead_type='member',lead_id=$2 WHERE id=$1`, projectID, userID); err != nil {
		t.Fatal(err)
	}
	req = projectMemoryRequest("PUT", projectID, map[string]any{"rules": []string{"Use pnpm"}, "expected_revision": 0})
	req.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.UpdateProjectMemory, req).Want(http.StatusForbidden)
}

func TestProjectMemoryHistoryRestoreAndExpiration(t *testing.T) {
	id := dbfx.Project(t, "Versioned memory", testutil.Cols{"description": "Base"})
	var saved ProjectMemoryResponse
	publish := func(body map[string]any) {
		testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, body)).Want(http.StatusOK).JSON(&saved)
	}
	publish(map[string]any{"rules": []string{"Use the temporary endpoint"}, "expected_revision": 0, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if saved.ExpiresAt == nil || saved.Expired {
		t.Fatalf("missing expiry: %+v", saved)
	}
	// Simulate the same persisted version after its expiration has passed.
	dbfx.Exec(t, `UPDATE project SET memory_expires_at=now()-interval '1 hour' WHERE id=$1`, id)
	dbfx.Exec(t, `UPDATE project_memory_version SET expires_at=now()-interval '1 hour' WHERE project_id=$1 AND revision=1`, id)
	claim, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil || claim.Description != "Base" {
		t.Fatalf("expired memory injected: %+v %v", claim, err)
	}
	publish(map[string]any{"rules": []string{"Current rule"}, "expected_revision": 1, "expires_at": nil})
	publish(map[string]any{"restore_revision": 1, "expected_revision": 2})
	if saved.Revision != 3 || saved.Rules[0] != "Use the temporary endpoint" || !saved.Expired {
		t.Fatalf("restore silently renewed memory: %+v", saved)
	}
	claim, err = testHandler.resolveClaimProjectContext(context.Background(), parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil || claim.Description != "Base" {
		t.Fatalf("restored expired rules injected: %+v %v", claim, err)
	}
	var history struct {
		Versions []ProjectMemoryResponse `json:"versions"`
		Next     *int32                  `json:"next_before_revision"`
	}
	testutil.Call(t, testHandler.ListProjectMemoryHistory, projectMemoryRequest("GET", id, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 4 || history.Next != nil || history.Versions[0].RestoredFromRevision == nil || *history.Versions[0].RestoredFromRevision != 1 || history.Versions[1].Rules[0] != "Current rule" {
		t.Fatalf("lost history: %+v", history)
	}
	// An older client omitting expiry must not silently remove it.
	publish(map[string]any{"rules": []string{"Still expired"}, "expected_revision": 3})
	if !saved.Expired {
		t.Fatal("omission removed expiration")
	}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, map[string]any{"restore_revision": 2, "expected_revision": 3})).Want(http.StatusConflict)
	for _, body := range []map[string]any{
		{"rules": []string{}, "expected_revision": 4, "expires_at": "not a date"},
		{"rules": []string{}, "expected_revision": 4, "expires_at": "2000-01-01T00:00:00Z"},
		{"rules": []string{}, "expected_revision": 4, "restore_revision": 1},
		{"expected_revision": 4, "restore_revision": 1, "expires_at": nil},
	} {
		testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, body)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, map[string]any{"restore_revision": 999, "expected_revision": 4})).Want(http.StatusNotFound)
	outsider := dbfx.User(t, "Memory outsider", "memory-history-outsider@example.test")
	req := projectMemoryRequest("GET", id, nil)
	req.Header.Set("X-User-ID", outsider)
	testutil.Call(t, testHandler.ListProjectMemoryHistory, req).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.DeleteProject, projectMemoryRequest("DELETE", id, nil)).Want(http.StatusNoContent)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM project_memory_version WHERE project_id=$1`, id).Scan(&count)
	if count != 0 {
		t.Fatal("project deletion left memory history")
	}
}

func TestProjectMemoryHistoryConcurrencyPaginationAndRollback(t *testing.T) {
	id := dbfx.Project(t, "Memory CAS")
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			w := httptest.NewRecorder()
			testHandler.UpdateProjectMemory(w, projectMemoryRequest("PUT", id, map[string]any{"rules": []string{"Only one"}, "expected_revision": 0}))
			codes <- w.Code
		}()
	}
	a, b := <-codes, <-codes
	if !(a == 200 && b == 409 || a == 409 && b == 200) {
		t.Fatalf("CAS results %d %d", a, b)
	}
	original := testHandler.TxStarter
	t.Cleanup(func() { testHandler.TxStarter = original })
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, map[string]any{"rules": []string{"Must roll back"}, "expected_revision": 1})).Want(http.StatusInternalServerError)
	testHandler.TxStarter = original
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM project_memory_version WHERE project_id=$1`, id).Scan(&count)
	if count != 2 {
		t.Fatalf("failed transaction leaked history: %d", count)
	}
	// Twenty-one more publications exercise the exclusive revision cursor.
	for revision := 1; revision <= 21; revision++ {
		testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", id, map[string]any{"rules": []string{fmt.Sprintf("Rule %d", revision)}, "expected_revision": revision})).Want(http.StatusOK)
	}
	var history struct {
		Versions []ProjectMemoryResponse `json:"versions"`
		Next     *int32                  `json:"next_before_revision"`
	}
	testutil.Call(t, testHandler.ListProjectMemoryHistory, projectMemoryRequest("GET", id, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 20 || history.Next == nil || *history.Next != 3 {
		t.Fatalf("first history page: %+v", history)
	}
	req := projectMemoryRequest("GET", id, nil)
	req.URL.RawQuery = "before_revision=3"
	testutil.Call(t, testHandler.ListProjectMemoryHistory, req).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 3 || history.Next != nil || history.Versions[0].Revision != 2 || history.Versions[2].Revision != 0 {
		t.Fatalf("history tail: %+v", history)
	}
}

func TestProjectMemoryHistoryWorkspaceDeletion(t *testing.T) {
	ws := dbfx.Workspace(t, "Memory history sweep", fmt.Sprintf("memory-history-%d", time.Now().UnixNano()))
	dbfx.Member(t, ws, testUserID, "owner")
	id := dbfx.Project(t, "History sweep", testutil.Cols{"workspace_id": ws})
	req := projectMemoryRequest("PUT", id, map[string]any{"rules": []string{"Temporary"}, "expected_revision": 0})
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.UpdateProjectMemory, req).Want(http.StatusOK)
	req = testutil.WithURLParams(newRequest("DELETE", "/api/workspaces/"+ws, nil), "id", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM project_memory_version WHERE workspace_id=$1`, ws).Scan(&count)
	if count != 0 {
		t.Fatalf("workspace deletion left %d versions", count)
	}
}

func TestProjectMemoryPromotionFromDeliveryCorrection(t *testing.T) {
	projectID := dbfx.Project(t, "Correction promotion project")
	otherProjectID := dbfx.Project(t, "Unrelated project")
	issue, _, _, _, review := correctionFixture(t)
	dbfx.Exec(t, `UPDATE issue SET project_id=$2 WHERE id=$1`, issue, projectID)

	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", otherProjectID, map[string]any{
		"rules": []string{"Keep the empty-state link working."}, "expected_revision": 0, "source_review_id": review.ID,
	})).Want(http.StatusNotFound)

	var accepted DeliveryReview
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, reviewRequest(readDelivery(t, issue)))).Want(http.StatusCreated).JSON(&accepted)
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"Keep the empty-state link working."}, "expected_revision": 0, "source_review_id": accepted.ID,
	})).Want(http.StatusConflict)

	var published ProjectMemoryResponse
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"Keep the empty-state link working."}, "expected_revision": 0, "source_review_id": review.ID,
	})).Want(http.StatusOK).JSON(&published)
	if published.Revision != 1 || len(published.SourceReview) == 0 {
		t.Fatalf("missing correction provenance: %+v", published)
	}
	var source AgentMemoryCorrectionSource
	if err := json.Unmarshal(published.SourceReview, &source); err != nil {
		t.Fatal(err)
	}
	if source.ReviewID != review.ID || source.IssueID != issue || source.Feedback != review.Feedback {
		t.Fatalf("lost evidence: %+v", source)
	}

	var retry ProjectMemoryResponse
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"Keep the empty-state link working."}, "expected_revision": 1, "source_review_id": review.ID,
	})).Want(http.StatusOK).JSON(&retry)
	if retry.Revision != 1 {
		t.Fatalf("idempotent retry bumped revision: %+v", retry)
	}
	testutil.Call(t, testHandler.UpdateProjectMemory, projectMemoryRequest("PUT", projectID, map[string]any{
		"rules": []string{"A different shared rule."}, "expected_revision": 1, "source_review_id": review.ID,
	})).Want(http.StatusConflict)

	var history struct {
		Versions []ProjectMemoryResponse `json:"versions"`
	}
	testutil.Call(t, testHandler.ListProjectMemoryHistory, projectMemoryRequest("GET", projectID, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) == 0 || len(history.Versions[0].SourceReview) == 0 {
		t.Fatalf("history lost provenance: %+v", history)
	}
}
