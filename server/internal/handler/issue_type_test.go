package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Work item type catalogue tests (F30 / JEF-34).

// seedTestTypeCatalog makes the shared test workspace's type catalogue present.
// The fixture creates its workspace with raw SQL, so it has none — which is
// itself the unseeded case TestListIssueTypesSelfHealsUnseededWorkspace covers.
func seedTestTypeCatalog(t *testing.T) {
	t.Helper()
	if err := testHandler.Queries.SeedIssueTypeEntries(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed type catalog: %v", err)
	}
}

type issueTypeListEnvelope struct {
	Types []IssueTypeResponse `json:"types"`
	Total int                 `json:"total"`
}

func listIssueTypes(t *testing.T) issueTypeListEnvelope {
	t.Helper()
	var out issueTypeListEnvelope
	testutil.Call(t, testHandler.ListIssueTypes, newRequest(http.MethodGet, "/api/issue-types", nil)).
		Want(http.StatusOK).JSON(&out)
	return out
}

// createTestIssueType inserts a custom type directly and removes it afterwards,
// so catalogue state cannot leak between tests in the shared workspace.
func createTestIssueType(t *testing.T, key string) db.IssueType {
	t.Helper()
	seedTestTypeCatalog(t)
	entry, err := testHandler.Queries.CreateIssueTypeEntry(context.Background(), db.CreateIssueTypeEntryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         key,
		Name:        key,
		Description: "",
		Color:       "#123456",
		Icon:        "",
	})
	if err != nil {
		t.Fatalf("create custom type %q: %v", key, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_type WHERE id = $1`, entry.ID)
	})
	return entry
}

// Acceptance 1: a fresh workspace has the four system types, and reading the
// catalogue is what seeds a workspace created before F30.
func TestListIssueTypesSelfHealsUnseededWorkspace(t *testing.T) {
	testPool.Exec(context.Background(), `DELETE FROM issue_type WHERE workspace_id = $1`, parseUUID(testWorkspaceID))
	out := listIssueTypes(t)
	got := map[string]IssueTypeResponse{}
	for _, entry := range out.Types {
		got[entry.Key] = entry
	}
	for _, key := range []string{"bug", "story", "epic", "task"} {
		entry, ok := got[key]
		if !ok {
			t.Fatalf("system type %q missing from a freshly seeded catalogue: %+v", key, out.Types)
		}
		if !entry.IsSystem {
			t.Errorf("%q must be flagged is_system so the UI can refuse to archive it", key)
		}
	}
	// Seed order is the display order, and it is what a workspace that never
	// opens settings sees forever.
	if len(out.Types) < 4 || out.Types[0].Key != "bug" || out.Types[3].Key != "task" {
		t.Errorf("seeded catalogue should read bug, story, epic, task; got %v", keysOf(out.Types))
	}
}

func keysOf(entries []IssueTypeResponse) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Key
	}
	return out
}

// Acceptance 1 (second half): a system type is not archivable.
func TestArchiveIssueTypeRefusesSystemType(t *testing.T) {
	seedTestTypeCatalog(t)
	entry, err := testHandler.Queries.GetIssueTypeEntryByKey(context.Background(), db.GetIssueTypeEntryByKeyParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         "bug",
	})
	if err != nil {
		t.Fatalf("load bug: %v", err)
	}
	id := uuidToString(entry.ID)
	body := testutil.Call(t, testHandler.ArchiveIssueType, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issue-types/"+id+"/archive", nil), "id", id),
	).Want(http.StatusConflict).Map()
	if body["code"] != "issue_type_is_system" {
		t.Errorf("archiving a system type must carry a machine-readable code, got %v", body)
	}
}

// Acceptance 2: a custom type is accepted; an unknown key is a 400 on write.
func TestCreateAndArchiveCustomIssueType(t *testing.T) {
	seedTestTypeCatalog(t)
	var created IssueTypeResponse
	testutil.Call(t, testHandler.CreateIssueType, newRequest(http.MethodPost, "/api/issue-types", map[string]any{
		"name":  "Spike",
		"color": "#00ff00",
		"icon":  "zap",
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_type WHERE id = $1`, parseUUID(created.ID))
	})
	if created.Key != "spike" {
		t.Errorf("key should be derived from the name, got %q", created.Key)
	}
	if created.IsSystem {
		t.Error("a created type must never be is_system")
	}

	body := testutil.Call(t, testHandler.ArchiveIssueType, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issue-types/"+created.ID+"/archive", nil), "id", created.ID),
	).Want(http.StatusOK).Map()
	if body["archived_at"] == nil {
		t.Errorf("archive must stamp archived_at, got %v", body)
	}
}

func TestCreateIssueTypeRejectsReservedKey(t *testing.T) {
	seedTestTypeCatalog(t)
	testutil.Call(t, testHandler.CreateIssueType, newRequest(http.MethodPost, "/api/issue-types", map[string]any{
		"name": "Another bug", "key": "bug", "color": "#00ff00",
	})).Want(http.StatusBadRequest)
}

// A name written entirely outside the ASCII key alphabet must still mint a
// usable key rather than 400 — the settings dialog has no key field.
func TestCreateIssueTypeDerivesKeyFromNonLatinName(t *testing.T) {
	seedTestTypeCatalog(t)
	var created IssueTypeResponse
	testutil.Call(t, testHandler.CreateIssueType, newRequest(http.MethodPost, "/api/issue-types", map[string]any{
		"name": "缺陷调查", "color": "#00ff00",
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_type WHERE id = $1`, parseUUID(created.ID))
	})
	if created.Key == "" || !issueTypeKeyPattern.MatchString(created.Key) {
		t.Errorf("derived key %q does not satisfy the column's format constraint", created.Key)
	}
}

// Acceptance 2 + 3: an unknown type is refused, a known one sticks, and the
// untyped default keeps working.
func TestUpdateIssueAcceptsAndValidatesIssueType(t *testing.T) {
	seedTestTypeCatalog(t)
	issue := dbfx.Issue(t, "typed issue")

	// Untyped by default (acceptance 3).
	var initial IssueResponse
	testutil.Call(t, testHandler.GetIssue, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue, nil), "id", issue),
	).Want(http.StatusOK).JSON(&initial)
	if initial.IssueType != nil {
		t.Fatalf("a new issue must be untyped, got %v", *initial.IssueType)
	}

	var updated IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"issue_type": "bug"}), "id", issue),
	).Want(http.StatusOK).JSON(&updated)
	if updated.IssueType == nil || *updated.IssueType != "bug" {
		t.Fatalf("issue_type should be bug, got %v", updated.IssueType)
	}

	body := testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"issue_type": "not_a_type"}), "id", issue),
	).Want(http.StatusBadRequest).Map()
	if body["code"] != "unknown_issue_type" {
		t.Errorf("an unknown type must carry a machine-readable code, got %v", body)
	}

	// Explicit null clears the classification.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"issue_type": nil}), "id", issue),
	).Want(http.StatusOK).JSON(&updated)
	if updated.IssueType != nil {
		t.Errorf("an explicit null must clear the type, got %v", *updated.IssueType)
	}
}

func TestUpdateIssueRefusesArchivedIssueType(t *testing.T) {
	entry := createTestIssueType(t, "retired_type")
	if _, err := testHandler.Queries.ArchiveIssueTypeEntry(context.Background(), db.ArchiveIssueTypeEntryParams{
		ID: entry.ID, WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	issue := dbfx.Issue(t, "archived type target")
	body := testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"issue_type": "retired_type"}), "id", issue),
	).Want(http.StatusBadRequest).Map()
	if body["code"] != "archived_issue_type" {
		t.Errorf("expected archived_issue_type, got %v", body)
	}
}

func TestListIssuesFiltersByIssueType(t *testing.T) {
	seedTestTypeCatalog(t)
	bug := dbfx.Issue(t, "a bug", testutil.Cols{"issue_type": "bug"})
	dbfx.Issue(t, "a story", testutil.Cols{"issue_type": "story"})

	var out struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues,
		newRequest(http.MethodGet, "/api/issues?issue_type=bug&limit=200", nil)).
		Want(http.StatusOK).JSON(&out)
	found := false
	for _, issue := range out.Issues {
		if issue.IssueType == nil || *issue.IssueType != "bug" {
			t.Fatalf("issue_type=bug returned %v (%s)", issue.IssueType, issue.Title)
		}
		if issue.ID == bug {
			found = true
		}
	}
	if !found {
		t.Error("the bug that was just created is missing from its own filter")
	}
}

func TestBatchUpdateIssuesSetsIssueType(t *testing.T) {
	seedTestTypeCatalog(t)
	first := dbfx.Issue(t, "batch typed a")
	second := dbfx.Issue(t, "batch typed b")
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{first, second},
		"updates":   map[string]any{"issue_type": "epic"},
	})).Want(http.StatusOK)

	for _, id := range []string{first, second} {
		var stored string
		dbfx.QueryRow(t, `SELECT issue_type FROM issue WHERE id = $1`, id).Scan(&stored)
		if stored != "epic" {
			t.Errorf("issue %s: expected epic, got %q", id, stored)
		}
	}

	// A bad key fails the whole batch: the catalogue is per workspace, so it is
	// wrong for every item, and reporting N per-issue skips would hide that.
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{first},
		"updates":   map[string]any{"issue_type": "nope"},
	})).Want(http.StatusBadRequest)
}

func TestReorderIssueTypesRequiresEveryActiveType(t *testing.T) {
	seedTestTypeCatalog(t)
	entries, err := testHandler.Queries.ListActiveCustomIssueTypeEntries(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, uuidToString(e.ID))
	}
	// Reversed and complete: accepted.
	reversed := make([]string, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}
	testutil.Call(t, testHandler.ReorderIssueTypes,
		newRequest(http.MethodPut, "/api/issue-types/reorder", map[string]any{"ids": reversed})).
		Want(http.StatusOK)
	t.Cleanup(func() {
		testutil.Call(t, testHandler.ReorderIssueTypes,
			newRequest(http.MethodPut, "/api/issue-types/reorder", map[string]any{"ids": ids}))
	})

	// A partial order would assign positions from the array index and collide
	// with the row left out, so it is refused rather than half-applied.
	testutil.Call(t, testHandler.ReorderIssueTypes,
		newRequest(http.MethodPut, "/api/issue-types/reorder", map[string]any{"ids": ids[:1]})).
		Want(http.StatusConflict)
}
