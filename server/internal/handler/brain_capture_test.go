package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func brainWorkspace(t *testing.T) string {
	t.Helper()
	workspaceID := dbfx.Workspace(t, "Brain capture", "brain-capture-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	return workspaceID
}

type captureEnvelope struct {
	Capture BrainCaptureResponse   `json:"capture"`
	Note    *WorkspaceNoteResponse `json:"note"`
}

func capture(t *testing.T, workspaceID string, body map[string]any) BrainCaptureResponse {
	t.Helper()
	var out captureEnvelope
	testutil.Call(t, noteWorkspaceHandler(testHandler.CreateBrainCapture),
		noteRequest(http.MethodPost, "/api/brain/captures", workspaceID, body)).
		Want(http.StatusCreated).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, out.Capture.ID)
	return out.Capture
}

func captureAction(t *testing.T, workspaceID, id, action string, body any, want int) captureEnvelope {
	t.Helper()
	var out captureEnvelope
	req := testutil.WithURLParams(noteRequest(http.MethodPost, "/api/brain/captures/"+id+"/"+action, workspaceID, body), "id", id)
	var h http.HandlerFunc
	switch action {
	case "organize":
		h = testHandler.OrganizeBrainCapture
	case "reopen":
		h = testHandler.ReopenBrainCapture
	case "suggest":
		h = testHandler.SuggestBrainCapture
	}
	res := testutil.Call(t, noteWorkspaceHandler(h), req).Want(want)
	if want < 300 {
		res.JSON(&out)
	}
	return out
}

func listCaptures(t *testing.T, workspaceID, status string) (items []BrainCaptureResponse, rawCount int64) {
	t.Helper()
	var out struct {
		Captures []BrainCaptureResponse `json:"captures"`
		RawCount int64                  `json:"raw_count"`
	}
	path := "/api/brain/captures"
	if status != "" {
		path += "?status=" + status
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListBrainCaptures),
		noteRequest(http.MethodGet, path, workspaceID, nil)).Want(http.StatusOK).JSON(&out)
	return out.Captures, out.RawCount
}

func TestBrainCaptureValidation(t *testing.T) {
	workspaceID := brainWorkspace(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty", map[string]any{"content": "   "}},
		{"bad url", map[string]any{"url": "ftp://x"}},
		{"too long", map[string]any{"content": strings.Repeat("a", brainCaptureMaxContentRunes+1)}},
		{"binary kind through json", map[string]any{"kind": "image", "content": "x"}},
	}
	for _, tc := range cases {
		testutil.Call(t, noteWorkspaceHandler(testHandler.CreateBrainCapture),
			noteRequest(http.MethodPost, "/api/brain/captures", workspaceID, tc.body)).Want(http.StatusBadRequest)
	}
}

func TestBrainCaptureLifecycle(t *testing.T) {
	workspaceID := brainWorkspace(t)

	text := capture(t, workspaceID, map[string]any{"content": "Deploys go through the release tag, never a manual push", "origin": "cli"})
	if text.Kind != "text" || text.Status != "raw" || text.Origin != "cli" || text.CreatedByType != "member" {
		t.Fatalf("capture = %+v, want raw text from cli by a member", text)
	}
	link := capture(t, workspaceID, map[string]any{"url": "https://example.com/runbook", "origin": "bogus"})
	if link.Kind != "link" || link.Origin != "web" {
		t.Errorf("link capture = kind %q origin %q, want link/web (unknown origin falls back to web)", link.Kind, link.Origin)
	}
	todo := capture(t, workspaceID, map[string]any{"kind": "todo", "content": "call the vendor back"})

	raw, rawCount := listCaptures(t, workspaceID, "")
	if len(raw) != 3 || rawCount != 3 {
		t.Fatalf("inbox holds %d (raw_count %d), want 3", len(raw), rawCount)
	}
	if raw[0].ID != todo.ID {
		t.Errorf("inbox is newest first: got %s first, want the todo", raw[0].ID)
	}

	// Organize into a new note: the capture body becomes the note, the
	// title falls back to the first line, the note is sourced "capture".
	out := captureAction(t, workspaceID, text.ID, "organize", map[string]any{"action": "note", "tags": []string{"Deploy"}}, http.StatusOK)
	if out.Note == nil {
		t.Fatal("organize as note returned no note")
	}
	dbfx.Cleanup(t, `DELETE FROM workspace_note WHERE id = $1`, out.Note.ID)
	if out.Note.Source != "capture" || out.Note.Title != "Deploys go through the release tag, never a manual push" || len(out.Note.Tags) != 1 || out.Note.Tags[0] != "deploy" {
		t.Errorf("note = %+v, want source capture, first-line title, lowercased tag", out.Note)
	}
	if out.Capture.Status != "organized" || out.Capture.NoteID == nil || *out.Capture.NoteID != out.Note.ID || out.Capture.OrganizedBy == nil {
		t.Errorf("capture after organize = %+v, want organized and linked to the note", out.Capture)
	}
	// Organizing twice is a conflict, never a second note.
	captureAction(t, workspaceID, text.ID, "organize", map[string]any{"action": "note"}, http.StatusConflict)

	// Merge into the note just created: the text is appended, tags union.
	merged := captureAction(t, workspaceID, link.ID, "organize", map[string]any{"action": "merge", "note_id": out.Note.ID, "tags": []string{"runbook"}}, http.StatusOK)
	if merged.Note == nil || !strings.HasSuffix(merged.Note.Content, "https://example.com/runbook") || merged.Note.Revision != 2 {
		t.Fatalf("merged note = %+v, want the link appended at revision 2", merged.Note)
	}
	if len(merged.Note.Tags) != 2 {
		t.Errorf("merged tags = %v, want the union [deploy runbook]", merged.Note.Tags)
	}
	// Merge into a note of another workspace: not found, never a leak.
	otherWS := dbfx.Workspace(t, "Other", "brain-other-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	foreign := createNote(t, otherWS, CreateWorkspaceNoteRequest{Title: "foreign", Content: "x"})
	captureAction(t, workspaceID, todo.ID, "organize", map[string]any{"action": "merge", "note_id": foreign.ID}, http.StatusNotFound)

	// Discard, then reopen.
	discarded := captureAction(t, workspaceID, todo.ID, "organize", map[string]any{"action": "discard"}, http.StatusOK)
	if discarded.Capture.Status != "discarded" || discarded.Note != nil {
		t.Errorf("discard = %+v, want discarded without a note", discarded.Capture)
	}
	if _, rawCount := listCaptures(t, workspaceID, ""); rawCount != 0 {
		t.Errorf("raw_count after organizing everything = %d, want 0", rawCount)
	}
	if done, _ := listCaptures(t, workspaceID, "organized"); len(done) != 2 {
		t.Errorf("organized list has %d, want 2", len(done))
	}
	reopened := captureAction(t, workspaceID, todo.ID, "reopen", nil, http.StatusOK)
	if reopened.Capture.Status != "raw" {
		t.Errorf("reopen = %q, want raw", reopened.Capture.Status)
	}
	// Only a discarded capture reopens.
	captureAction(t, workspaceID, text.ID, "reopen", nil, http.StatusConflict)
	// Bad action / bad status filter.
	captureAction(t, workspaceID, todo.ID, "organize", map[string]any{"action": "archive"}, http.StatusBadRequest)
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListBrainCaptures),
		noteRequest(http.MethodGet, "/api/brain/captures?status=nope", workspaceID, nil)).Want(http.StatusBadRequest)
	// Without a model, a suggestion on demand says so instead of guessing.
	captureAction(t, workspaceID, todo.ID, "suggest", nil, http.StatusServiceUnavailable)

	// Delete for good: the row is gone, the list no longer shows it.
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteBrainCapture),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/brain/captures/"+todo.ID, workspaceID, nil), "id", todo.ID)).Want(http.StatusNoContent)
	testutil.Call(t, noteWorkspaceHandler(testHandler.GetBrainCapture),
		testutil.WithURLParams(noteRequest(http.MethodGet, "/api/brain/captures/"+todo.ID, workspaceID, nil), "id", todo.ID)).Want(http.StatusNotFound)
	// A delete must audit under its own event type, not "brain.organized" —
	// otherwise a dashboard filtering by event type miscounts deletes as
	// organize actions (audit trail regression guard).
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2 AND details->>'action' = 'delete'`, AuditBrainDeleted, todo.ID); n != 1 {
		t.Errorf("delete audit rows with AuditBrainDeleted = %d, want 1", n)
	}
}

// An iOS voice memo (.m4a, declared audio/mp4) sniffs as video/mp4; it must
// still land as an audio capture queued for transcription, not as a file.
func TestBrainCaptureUploadM4aVoiceMemoIsAudio(t *testing.T) {
	workspaceID := brainWorkspace(t)
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="voice-memo-1.m4a"`},
		"Content-Type":        {"audio/mp4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(append([]byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00M4A mp42isom"), make([]byte, 64)...))
	_ = writer.WriteField("content", "Voice memo")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var out captureEnvelope
	testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture), req).Want(http.StatusCreated).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, out.Capture.ID)
	if out.Capture.Attachment != nil {
		dbfx.Cleanup(t, `DELETE FROM attachment WHERE id = $1`, out.Capture.Attachment.ID)
	}
	if out.Capture.Kind != "audio" || out.Capture.TranscriptionStatus == "none" {
		t.Fatalf("m4a upload = kind %q, transcription %q; want an audio capture queued for transcription", out.Capture.Kind, out.Capture.TranscriptionStatus)
	}
	if out.Capture.Attachment == nil || out.Capture.Attachment.ContentType != "audio/mp4" {
		t.Fatalf("attachment = %+v, want content type audio/mp4", out.Capture.Attachment)
	}
}

func TestBrainCaptureUploadBecomesAttachmentAndNote(t *testing.T) {
	workspaceID := brainWorkspace(t)
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "whiteboard.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	_ = writer.WriteField("content", "photo of the sprint board")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var out captureEnvelope
	testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture), req).Want(http.StatusCreated).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, out.Capture.ID)
	if out.Capture.Kind != "image" || out.Capture.Attachment == nil || out.Capture.TitleHint != "whiteboard" || out.Capture.Content != "photo of the sprint board" {
		t.Fatalf("upload capture = %+v, want an image with its attachment and the file name as title hint", out.Capture)
	}
	dbfx.Cleanup(t, `DELETE FROM attachment WHERE id = $1`, out.Capture.Attachment.ID)
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND capture_id = $2`, out.Capture.Attachment.ID, out.Capture.ID); n != 1 {
		t.Errorf("attachment is not linked back to the capture")
	}

	organized := captureAction(t, workspaceID, out.Capture.ID, "organize", map[string]any{"action": "note", "title": "Sprint board"}, http.StatusOK)
	if organized.Note == nil {
		t.Fatal("no note")
	}
	dbfx.Cleanup(t, `DELETE FROM workspace_note WHERE id = $1`, organized.Note.ID)
	if !strings.Contains(organized.Note.Content, "![whiteboard.png](") || !strings.HasPrefix(organized.Note.Content, "photo of the sprint board") {
		t.Errorf("note content = %q, want the caption then the image embedded", organized.Note.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND note_id = $2`, out.Capture.Attachment.ID, organized.Note.ID); n != 1 {
		t.Errorf("attachment is not linked to the note it was filed into")
	}
	// Deleting the organized capture keeps the file the note now holds.
	testutil.Call(t, noteWorkspaceHandler(testHandler.DeleteBrainCapture),
		testutil.WithURLParams(noteRequest(http.MethodDelete, "/api/brain/captures/"+out.Capture.ID, workspaceID, nil), "id", out.Capture.ID)).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1`, out.Capture.Attachment.ID); n != 1 {
		t.Errorf("attachment held by a note was deleted with the capture")
	}
	// No file field → 400, missing storage → 503.
	testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture),
		testutil.WithHeaders(httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", strings.NewReader("")), "X-User-ID", testUserID, "X-Workspace-ID", workspaceID)).
		Want(http.StatusBadRequest)
	testHandler.Storage = nil
	testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture),
		testutil.WithHeaders(httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", strings.NewReader("")), "X-User-ID", testUserID, "X-Workspace-ID", workspaceID)).
		Want(http.StatusServiceUnavailable)
}

// ListBrainCaptures batches its attachment lookup into one call instead of
// one GetAttachment per capture with an attachment; this pins several
// captures, each with its own distinct uploaded file, through one list call
// so a batching bug that hands one capture another's attachment fails this
// test instead of shipping silently.
func TestListBrainCapturesBatchesAttachmentsWithoutMixingThem(t *testing.T) {
	workspaceID := brainWorkspace(t)
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })

	upload := func(filename string, data []byte, caption string) BrainCaptureResponse {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = writer.WriteField("content", caption)
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", workspaceID)
		var out captureEnvelope
		testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture), req).Want(http.StatusCreated).JSON(&out)
		dbfx.Cleanup(t, `DELETE FROM brain_capture WHERE id = $1`, out.Capture.ID)
		dbfx.Cleanup(t, `DELETE FROM attachment WHERE id = $1`, out.Capture.Attachment.ID)
		return out.Capture
	}

	one := upload("alpha.png", []byte("\x89PNG alpha bytes"), "alpha capture")
	two := upload("bravo.png", []byte("\x89PNG bravo bytes"), "bravo capture")
	three := upload("charlie.png", []byte("\x89PNG charlie bytes"), "charlie capture")
	// A capture with no attachment sits alongside the three that have one.
	textOnly := capture(t, workspaceID, map[string]any{"content": "no attachment here"})

	listed, rawCount := listCaptures(t, workspaceID, "")
	if len(listed) != 4 || rawCount != 4 {
		t.Fatalf("inbox holds %d (raw_count %d), want 4", len(listed), rawCount)
	}
	byID := map[string]BrainCaptureResponse{}
	for _, c := range listed {
		byID[c.ID] = c
	}
	for _, want := range []struct {
		capture  BrainCaptureResponse
		filename string
	}{
		{one, "alpha.png"}, {two, "bravo.png"}, {three, "charlie.png"},
	} {
		got, ok := byID[want.capture.ID]
		if !ok || got.Attachment == nil {
			t.Fatalf("capture %s missing from the list or its attachment: %+v", want.capture.ID, got)
		}
		if got.Attachment.ID != want.capture.Attachment.ID || got.Attachment.Filename != want.filename {
			t.Fatalf("capture %s got attachment %+v, want its own %s (%s)", want.capture.ID, got.Attachment, want.capture.Attachment.ID, want.filename)
		}
	}
	if got := byID[textOnly.ID]; got.Attachment != nil {
		t.Fatalf("text-only capture must have no attachment, got %+v", got.Attachment)
	}
}

func searchNotes(t *testing.T, workspaceID, query string) []WorkspaceNoteSearchHit {
	t.Helper()
	var out struct {
		Notes  []WorkspaceNoteSearchHit `json:"notes"`
		Vector bool                     `json:"vector"`
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.SearchWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes/search?"+query, workspaceID, nil)).Want(http.StatusOK).JSON(&out)
	return out.Notes
}

func TestWorkspaceNoteRankedSearch(t *testing.T) {
	workspaceID := brainWorkspace(t)
	deploy := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Deploy procedure", Content: "Push the release tag on main. The Homebrew tap follows the tag. Never deploy on a Friday.", Tags: []string{"ops"}})
	vendor := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Vendor contacts", Content: "The printer vendor answers on Mondays.", Tags: []string{"vendor"}})
	archived := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Old deploy notes", Content: "deploy deploy deploy, the old way"})
	testutil.Call(t, noteWorkspaceHandler(testHandler.ArchiveWorkspaceNote),
		testutil.WithURLParams(noteRequest(http.MethodPost, "/api/workspace/notes/"+archived.ID+"/archive", workspaceID, nil), "id", archived.ID)).Want(http.StatusOK)

	hits := searchNotes(t, workspaceID, "q=deploy+tag")
	if len(hits) != 1 || hits[0].ID != deploy.ID {
		t.Fatalf("search deploy tag = %d hits (first %v), want only the live deploy note", len(hits), hits)
	}
	if hits[0].Snippet == "" || !strings.Contains(hits[0].Snippet, "<mark>") || hits[0].LexRank == nil || hits[0].VecRank != nil || hits[0].Score <= 0 {
		t.Errorf("hit = score %v snippet %q lex %v vec %v, want a marked snippet ranked lexically only", hits[0].Score, hits[0].Snippet, hits[0].LexRank, hits[0].VecRank)
	}
	if hits := searchNotes(t, workspaceID, "q=deploy&archived=true"); len(hits) != 2 {
		t.Errorf("archived=true returned %d, want the archived note too", len(hits))
	}
	if hits := searchNotes(t, workspaceID, "q=deploy&tag=vendor"); len(hits) != 0 {
		t.Errorf("tag filter leaked %d hits", len(hits))
	}
	// Web-search syntax: a quoted phrase and a negation.
	if hits := searchNotes(t, workspaceID, `q=%22release+tag%22+-friday`); len(hits) != 0 {
		t.Errorf("negated term still matched %d", len(hits))
	}

	// Vector leg: a stored vector close to the query vector lifts a note the
	// lexical leg does not see. 1536 dims: the query is a unit vector on
	// axis 0; the vendor note's vector is the same; the deploy note points
	// the other way.
	unit := func(sign string) string {
		parts := make([]string, 1536)
		for i := range parts {
			parts[i] = "0"
		}
		parts[0] = sign + "1"
		return "[" + strings.Join(parts, ",") + "]"
	}
	dbfx.Exec(t, `INSERT INTO workspace_note_embedding (note_id, workspace_id, embedding, embedding_model, content_hash) VALUES ($1, $2, $3::vector, 'test-model', 'h')`, vendor.ID, workspaceID, unit(""))
	dbfx.Exec(t, `INSERT INTO workspace_note_embedding (note_id, workspace_id, embedding, embedding_model, content_hash) VALUES ($1, $2, $3::vector, 'test-model', 'h')`, deploy.ID, workspaceID, unit("-"))
	dbfx.Cleanup(t, `DELETE FROM workspace_note_embedding WHERE workspace_id = $1`, workspaceID)
	origEmbedder := testHandler.BrainEmbedder
	testHandler.BrainEmbedder = stubEmbedder{literal: unit(""), model: "test-model"}
	t.Cleanup(func() { testHandler.BrainEmbedder = origEmbedder })
	hits = searchNotes(t, workspaceID, "q=printer")
	if len(hits) != 1 || hits[0].ID != vendor.ID {
		t.Fatalf("fused search = %v, want the vendor note alone: a search needs a lexical match", hits)
	}
	if hits[0].LexRank == nil || hits[0].VecRank == nil {
		t.Errorf("ranks = %v/%v, want vendor on both legs", hits[0].LexRank, hits[0].VecRank)
	}
	// Merge candidates still take the vector neighbours: a model judges them.
	candidates := testHandler.brainCaptureCandidates(context.Background(), db.BrainCapture{WorkspaceID: parseUUID(workspaceID), Content: "printer"})
	if len(candidates) != 2 || candidates[0].ID != parseUUID(vendor.ID) || candidates[1].LexRank.Valid || !candidates[1].VecRank.Valid {
		t.Errorf("candidates = %v, want vendor then deploy on the vector leg only", candidates)
	}
	// A vector from another model never fuses.
	testHandler.BrainEmbedder = stubEmbedder{literal: unit(""), model: "other-model"}
	if hits := searchNotes(t, workspaceID, "q=printer"); len(hits) != 1 {
		t.Errorf("other model fused %d hits, want the lexical hit alone", len(hits))
	}
	testutil.Call(t, noteWorkspaceHandler(testHandler.SearchWorkspaceNotes),
		noteRequest(http.MethodGet, "/api/workspace/notes/search?q=", workspaceID, nil)).Want(http.StatusBadRequest)
}

// TestWorkspaceNoteSearchFiltersByQuery is the regression for the audit
// finding "the palette and the Brain page return every note whatever the
// query": with an embeddings provider, the vector leg is a nearest-neighbour
// list that always has neighbours, so every embedded note surfaced as a hit.
// A query that matches nothing returns nothing; two different queries return
// different notes.
func TestWorkspaceNoteSearchFiltersByQuery(t *testing.T) {
	workspaceID := brainWorkspace(t)
	deploy := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Deploy procedure", Content: "Push the release tag on main."})
	vendor := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "Vendor contacts", Content: "The printer vendor answers on Mondays."})
	image := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: "IMG_0111", Content: "image"})
	axis := func(i int) string {
		parts := make([]string, 1536)
		for j := range parts {
			parts[j] = "0"
		}
		parts[i] = "1"
		return "[" + strings.Join(parts, ",") + "]"
	}
	for i, id := range []string{deploy.ID, vendor.ID, image.ID} {
		dbfx.Exec(t, `INSERT INTO workspace_note_embedding (note_id, workspace_id, embedding, embedding_model, content_hash) VALUES ($1, $2, $3::vector, 'test-model', 'h')`, id, workspaceID, axis(i))
	}
	dbfx.Cleanup(t, `DELETE FROM workspace_note_embedding WHERE workspace_id = $1`, workspaceID)
	origEmbedder := testHandler.BrainEmbedder
	// The query vector sits right next to the image note: a real provider
	// always has a nearest note, however unrelated the query is.
	testHandler.BrainEmbedder = stubEmbedder{literal: axis(2), model: "test-model"}
	t.Cleanup(func() { testHandler.BrainEmbedder = origEmbedder })

	if hits := searchNotes(t, workspaceID, "q=zzzxxqqnonsense123"); len(hits) != 0 {
		t.Fatalf("a query matching no note returned %d hits (%v), want none", len(hits), hits)
	}
	printer := searchNotes(t, workspaceID, "q=printer")
	release := searchNotes(t, workspaceID, "q=release")
	if len(printer) != 1 || printer[0].ID != vendor.ID {
		t.Errorf("q=printer = %v, want the vendor note only", printer)
	}
	if len(release) != 1 || release[0].ID != deploy.ID {
		t.Errorf("q=release = %v, want the deploy note only", release)
	}
}

// stubEmbedder answers a fixed query vector so the fusion is testable
// without a provider.
type stubEmbedder struct{ literal, model string }

func (s stubEmbedder) EmbedNoteAsync(pgtype.UUID) {}
func (s stubEmbedder) QueryEmbedding(_ context.Context, _ string) (string, string, bool) {
	return s.literal, s.model, true
}
func (s stubEmbedder) Enabled() bool { return true }

func TestFirstLineCutsAtAWordBoundary(t *testing.T) {
	cases := map[string]string{
		"short":              "short",
		"first line\nsecond": "first line",
		"les déploiements passent par le tag de release visuel, jamais par un push manuel sur le serveur": "les déploiements passent par le tag de release visuel, jamais par un push",
		strings.Repeat("a", 100): strings.Repeat("a", 80),
	}
	for in, want := range cases {
		if got := firstLine(in, 80); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBrainCaptureUploadCompensatesOnAttachFailure covers the fix for the
// audit finding "AttachAttachmentToCapture fails after a successful
// upload+CreateAttachment+CreateBrainCapture leaves an orphan attachment row
// and an orphan storage object" (brain_capture.go:UploadBrainCapture). A
// BEFORE UPDATE trigger on attachment forces the same failure
// AttachAttachmentToCapture would hit in production (DB blip, constraint),
// and the test asserts both the attachment row and the storage object are
// gone afterward, not just the brain_capture row.
func TestBrainCaptureUploadCompensatesOnAttachFailure(t *testing.T) {
	ctx := context.Background()
	workspaceID := brainWorkspace(t)
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })

	const functionName = "brain_capture_attach_fail_fn"
	const triggerName = "brain_capture_attach_fail_trg"
	if _, err := testPool.Exec(ctx, `
CREATE OR REPLACE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.capture_id IS NOT NULL THEN
		RAISE EXCEPTION 'forced attach-to-capture failure';
	END IF;
	RETURN NEW;
END;
$$;`); err != nil {
		t.Fatalf("install failure function: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
CREATE TRIGGER `+triggerName+`
BEFORE UPDATE ON attachment
FOR EACH ROW EXECUTE FUNCTION `+functionName+`();`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DROP TRIGGER IF EXISTS `+triggerName+` ON attachment`)
		testPool.Exec(ctx, `DROP FUNCTION IF EXISTS `+functionName+`()`)
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "whiteboard.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	_ = writer.WriteField("content", "photo of the sprint board")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/captures/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)

	resp := testutil.Call(t, noteWorkspaceHandler(testHandler.UploadBrainCapture), req).Want(http.StatusInternalServerError)
	_ = resp

	if n := dbfx.Count(t, `SELECT count(*) FROM brain_capture WHERE workspace_id = $1`, workspaceID); n != 0 {
		t.Fatalf("brain_capture rows after failed attach = %d, want 0 (DeleteBrainCapture already covered this)", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE workspace_id = $1`, workspaceID); n != 0 {
		t.Fatalf("attachment rows after failed attach = %d, want 0: the fix must delete the orphan attachment row", n)
	}
	store.mu.Lock()
	remaining := len(store.files)
	store.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("storage objects after failed attach = %d, want 0: the fix must delete the orphan storage object", remaining)
	}
}
