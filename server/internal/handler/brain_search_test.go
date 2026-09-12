package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Brain search over passages (JEF-412). The query matrix itself (parsing,
// folding, snippets, the chunker) lives in internal/service/brain_*_test.go;
// these cases hold what only the database can: the index stays in step with
// every write path, and the SQL admits, excludes and ranks as documented.

func searchQ(t *testing.T, workspaceID, q string) []WorkspaceNoteSearchHit {
	t.Helper()
	return searchNotes(t, workspaceID, "q="+url.QueryEscape(q))
}

func TestBrainSearchFoldsAccentsCJKAndQuestions(t *testing.T) {
	workspaceID := brainWorkspace(t)
	security := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Sécurité du déploiement", Content: "La sécurité passe par la revue des secrets avant chaque déploiement."})
	pipeline := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "生产环境部署流程", Content: "先在预发布环境验证，然后在生产环境部署。"})
	friday := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Friday deploys", Content: "We never deploy on Fridays: the on-call team is thin."})
	createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Printer vendor", Content: "The printer vendor answers on Mondays."})

	cases := []struct {
		query string
		want  string
	}{
		{"securite deploiement", security.ID},
		{"部署流程", pipeline.ID},
		{"生产环境", pipeline.ID},
		{"Can we deploy on a Friday?", friday.ID},
		{"quelle est la procédure de sécurité pour le déploiement ?", security.ID},
	}
	for _, c := range cases {
		hits := searchQ(t, workspaceID, c.query)
		if len(hits) == 0 || hits[0].ID != c.want {
			t.Errorf("%q = %v, want %s first", c.query, hits, c.want)
			continue
		}
		if !strings.Contains(hits[0].Snippet, "<mark>") {
			t.Errorf("%q snippet %q marks nothing", c.query, hits[0].Snippet)
		}
	}
	if hits := searchQ(t, workspaceID, "securite deploiement"); len(hits) != 1 || !strings.Contains(hits[0].Snippet, "<mark>sécurité</mark>") {
		t.Errorf("accent-free query = %v, want the accented words marked", hits)
	}
}

func TestBrainSearchAnswersWithTheMatchingSection(t *testing.T) {
	workspaceID := brainWorkspace(t)
	filler := strings.Repeat("Compile the assets and keep the cache warm between jobs. ", 20)
	content := "# Runbook\n## Build\n" + filler + "\n## Release\n" + strings.Repeat("Announce the version in the team channel first. ", 20) +
		"\n## Rollback\nTo roll back, re-tag the previous version and run the revert pipeline.\n"
	note := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Operations", Content: content})

	hits := searchQ(t, workspaceID, "revert pipeline")
	if len(hits) != 1 || hits[0].ID != note.ID {
		t.Fatalf("hits = %v, want the operations note", hits)
	}
	if hits[0].PassageHeading != "Runbook › Rollback" {
		t.Errorf("passage_heading = %q, want the third section", hits[0].PassageHeading)
	}
	if !strings.Contains(hits[0].Snippet, "<mark>revert</mark> <mark>pipeline</mark>") || strings.Contains(hits[0].Snippet, "Compile") {
		t.Errorf("snippet = %q, want the rollback section", hits[0].Snippet)
	}
}

func TestBrainSearchIndexFollowsEveryWrite(t *testing.T) {
	ctx := context.Background()
	workspaceID := brainWorkspace(t)
	note := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Pager rota", Content: "## Weekdays\nAlice holds the pager.\n## Weekends\nBob holds the pager."})
	if hits := searchQ(t, workspaceID, "pager"); len(hits) != 1 {
		t.Fatalf("pager = %v", hits)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note_passage WHERE note_id = $1`, note.ID); n != 2 {
		t.Fatalf("passages = %d, want one per section", n)
	}

	// An unchanged passage keeps its vector across an edit of another section.
	setPassageVector(t, note.ID, axisVector(0, ""))
	content := "## Weekdays\nAlice holds the pager.\n## Weekends\nCarol holds the pager from now on."
	var updated WorkspaceNoteResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.UpdateWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPatch, "/api/workspace/notes/"+note.ID, workspaceID,
			UpdateWorkspaceNoteRequest{Content: &content, Revision: note.Revision}), "id", note.ID)).
		Want(http.StatusOK).
		JSON(&updated)
	if hits := searchQ(t, workspaceID, "carol"); len(hits) != 1 || hits[0].ID != note.ID || hits[0].PassageHeading != "Weekends" {
		t.Fatalf("carol right after the edit = %v, want the note, from its weekends section", hits)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note_passage WHERE note_id = $1 AND ordinal = 1 AND embedding IS NOT NULL`, note.ID); n != 1 {
		t.Errorf("the untouched weekdays passage lost its vector")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note_passage WHERE note_id = $1 AND ordinal = 2 AND embedding IS NULL`, note.ID); n != 1 {
		t.Errorf("the edited weekends passage kept a vector of its old text")
	}

	// A note written straight to the table, by a path with no indexer.
	raw := dbfx.Insert(t, "workspace_note", testutil.Cols{
		"workspace_id": workspaceID, "id": testutil.Raw("gen_random_uuid()"), "title": "Badge printer",
		"content": "The badge printer lives on the third floor.", "source": "manual", "created_by_type": "member", "created_by_id": testUserID,
	})
	testDBFixtureCleanupNote(t, raw)
	if hits := searchQ(t, workspaceID, "badge printer"); len(hits) != 1 || hits[0].ID != raw {
		t.Errorf("raw insert = %v, want it found", hits)
	}

	// Deleting a note deletes its passages; deleting a workspace's notes
	// deletes theirs.
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/workspace/notes/"+note.ID, workspaceID, nil), "id", note.ID)).
		Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note_passage WHERE note_id = $1`, note.ID); n != 0 {
		t.Errorf("deleted note left %d passages", n)
	}
	if err := testHandler.Queries.DeleteWorkspaceNotes(ctx, parseUUID(workspaceID)); err != nil {
		t.Fatalf("delete workspace notes: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note_passage WHERE workspace_id = $1`, workspaceID); n != 0 {
		t.Errorf("workspace teardown left %d passages", n)
	}
}

// MCP note_list?search= and the search endpoint run the same engine.
func TestBrainListSearchUsesTheRankedEngine(t *testing.T) {
	workspaceID := brainWorkspace(t)
	createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Rollback", Content: "Revert the tag."})
	both := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Rollback and revert", Content: "Revert the tag, then roll back the schema."})

	var listed struct {
		Items []WorkspaceNoteResponse `json:"items"`
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes?search="+url.QueryEscape("rollback schéma")+"&limit=1", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&listed)
	hits := searchQ(t, workspaceID, "rollback schéma")
	if len(listed.Items) != 1 || len(hits) == 0 || listed.Items[0].ID != hits[0].ID || hits[0].ID != both.ID {
		t.Fatalf("list search = %v, search = %v; want the same best note first", listed.Items, hits)
	}
}
