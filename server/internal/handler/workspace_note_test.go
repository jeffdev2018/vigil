package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func noteRequest(method, path, workspaceID string, body any) *http.Request {
	return testutil.WithHeaders(
		testutil.JSONRequest(method, path, body),
		"X-User-ID", testUserID,
		"X-Workspace-ID", workspaceID,
	)
}

func noteWorkspaceHandler(handler http.HandlerFunc) http.HandlerFunc {
	return middleware.RequireWorkspaceMember(testHandler.Queries)(handler).ServeHTTP
}

// createNote goes through the endpoint rather than a raw INSERT so the test
// exercises the id/source/actor assignment the handler owns.
func createNote(t *testing.T, workspaceID string, body CreateWorkspaceNoteRequest) WorkspaceNoteResponse {
	t.Helper()
	var note WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.CreateWorkspaceNote),
		noteRequest(http.MethodPost, "/api/workspace/notes", workspaceID, body)).
		Want(http.StatusCreated).
		JSON(&note)
	testDBFixtureCleanupNote(t, note.ID)
	return note
}

func testDBFixtureCleanupNote(t *testing.T, id string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM workspace_note WHERE id = $1`, id)
}

func TestWorkspaceNoteCRUDRoundTrip(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Brain CRUD", "brain-crud-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")

	created := createNote(t, workspaceID, CreateWorkspaceNoteRequest{
		Title:   "  Deploys go through the release tag  ",
		Content: "Push `v0.x.x` on main; release.yml publishes the binaries.",
		Tags:    []string{"Deploy", "  release ", "deploy"},
	})
	if created.Title != "Deploys go through the release tag" {
		t.Errorf("title = %q, want the trimmed title", created.Title)
	}
	if created.Source != "manual" || created.CreatedByType != "member" {
		t.Errorf("source/created_by = %q/%q, want manual/member", created.Source, created.CreatedByType)
	}
	// Tags are lowercased, de-duplicated and sorted so the filter is one
	// equality test and "Deploy"/"deploy" cannot split a facet.
	if len(created.Tags) != 2 || created.Tags[0] != "deploy" || created.Tags[1] != "release" {
		t.Errorf("tags = %v, want [deploy release]", created.Tags)
	}
	if created.Revision != 1 {
		t.Errorf("revision = %d, want 1", created.Revision)
	}

	var listed struct {
		Items []WorkspaceNoteResponse `json:"items"`
		Tags  []string                `json:"tags"`
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("list returned %d items, want the one note just created", len(listed.Items))
	}
	if len(listed.Tags) != 2 {
		t.Errorf("facet tags = %v, want both tags of the only note", listed.Tags)
	}

	var fetched WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.GetWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodGet, "/api/workspace/notes/"+created.ID, workspaceID, nil), "id", created.ID)).
		Want(http.StatusOK).
		JSON(&fetched)
	if fetched.Content != created.Content {
		t.Errorf("content round trip = %q", fetched.Content)
	}

	pinned := true
	var updated WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.UpdateWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPatch, "/api/workspace/notes/"+created.ID, workspaceID,
			UpdateWorkspaceNoteRequest{Pinned: &pinned, Revision: created.Revision}), "id", created.ID)).
		Want(http.StatusOK).
		JSON(&updated)
	if !updated.Pinned || updated.Revision != created.Revision+1 {
		t.Fatalf("pinned=%v revision=%d, want true/%d", updated.Pinned, updated.Revision, created.Revision+1)
	}

	var archived WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.ArchiveWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPost, "/api/workspace/notes/"+created.ID+"/archive", workspaceID, nil), "id", created.ID)).
		Want(http.StatusOK).
		JSON(&archived)
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at is nil after archive")
	}

	// An archived note leaves the default listing but stays reachable.
	listed.Items = nil
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&listed)
	if len(listed.Items) != 0 {
		t.Errorf("archived note still in the default listing: %d items", len(listed.Items))
	}

	var unarchived WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.UnarchiveWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPost, "/api/workspace/notes/"+created.ID+"/unarchive", workspaceID, nil), "id", created.ID)).
		Want(http.StatusOK).
		JSON(&unarchived)
	if unarchived.ArchivedAt != nil {
		t.Errorf("archived_at = %v after unarchive, want nil", *unarchived.ArchivedAt)
	}
}

func TestWorkspaceNoteUpdateWithStaleRevisionConflicts(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Brain revision", "brain-rev-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	note := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Conflict", Content: "one"})

	newContent := "two"
	testutil.Call(t, noteWorkspaceHandler(testHandler.UpdateWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPatch, "/api/workspace/notes/"+note.ID, workspaceID,
			UpdateWorkspaceNoteRequest{Content: &newContent, Revision: note.Revision}), "id", note.ID)).
		Want(http.StatusOK)

	// Second writer still holds revision 1: the edit must be refused, not
	// silently applied over the first writer's content.
	other := "three"
	testutil.Call(t, noteWorkspaceHandler(testHandler.UpdateWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPatch, "/api/workspace/notes/"+note.ID, workspaceID,
			UpdateWorkspaceNoteRequest{Content: &other, Revision: note.Revision}), "id", note.ID)).
		Want(http.StatusConflict)

	// A PATCH that forgets the revision entirely is a 400, not an overwrite.
	testutil.Call(t, noteWorkspaceHandler(testHandler.UpdateWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPatch, "/api/workspace/notes/"+note.ID, workspaceID,
			UpdateWorkspaceNoteRequest{Content: &other}), "id", note.ID)).
		Want(http.StatusBadRequest)
}

func TestWorkspaceNoteDeletePermissions(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Brain delete", "brain-del-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "member")

	// Written by somebody else: a plain member is not allowed to erase it.
	otherUserID := uuid.NewString()
	foreignID := uuid.NewString()
	dbfx.Insert(t, "workspace_note", testutil.Cols{
		"id": foreignID, "workspace_id": workspaceID, "title": "Someone else's note",
		"created_by_type": "member", "created_by_id": otherUserID,
	})
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/workspace/notes/"+foreignID, workspaceID, nil), "id", foreignID)).
		Want(http.StatusForbidden)

	// Its own author may delete it.
	own := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "My note", Content: "mine"})
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/workspace/notes/"+own.ID, workspaceID, nil), "id", own.ID)).
		Want(http.StatusNoContent)

	// Promote to admin: now the foreign note goes too.
	dbfx.Exec(t, `UPDATE member SET role = 'admin' WHERE workspace_id = $1 AND user_id = $2`, workspaceID, testUserID)
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/workspace/notes/"+foreignID, workspaceID, nil), "id", foreignID)).
		Want(http.StatusNoContent)
}

func TestWorkspaceNoteSearchAndTagFilter(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Brain search", "brain-search-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	createNote(t, workspaceID, CreateWorkspaceNoteRequest{
		Title: "Postgres connection pooling", Content: "pgbouncer sits in front", Tags: []string{"db"},
	})
	createNote(t, workspaceID, CreateWorkspaceNoteRequest{
		Title: "Frontend routing", Content: "the app router owns the segments", Tags: []string{"web"},
	})

	var listed struct {
		Items []WorkspaceNoteResponse `json:"items"`
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes?search=pgbouncer", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&listed)
	if len(listed.Items) != 1 || listed.Items[0].Title != "Postgres connection pooling" {
		t.Fatalf("search=pgbouncer returned %d items, want only the db note", len(listed.Items))
	}

	listed.Items = nil
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes?tag=WEB", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&listed)
	if len(listed.Items) != 1 || listed.Items[0].Title != "Frontend routing" {
		t.Fatalf("tag=WEB returned %d items, want only the web note (the filter lowercases)", len(listed.Items))
	}
}
