package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newBrainSaveTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "save"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("tags", "", "")
	cmd.Flags().String("content", "", "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().Bool("pinned", false, "")
	cmd.Flags().String("id", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newBrainListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("search", "", "")
	cmd.Flags().String("tag", "", "")
	cmd.Flags().Bool("archived", false, "")
	cmd.Flags().Int("limit", 0, "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func brainTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunBrainSaveCreatesANoteWithSplitTags(t *testing.T) {
	var gotBody map[string]any
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/workspace/notes" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "note-1", "title": "Deploys", "revision": 1})
	})

	cmd := newBrainSaveTestCmd()
	_ = cmd.Flags().Set("title", "Deploys")
	_ = cmd.Flags().Set("tags", "deploy, release ,")
	_ = cmd.Flags().Set("content", "Push v0.x.x on main.")
	if _, err := captureStdout(t, func() error { return runBrainSave(cmd, nil) }); err != nil {
		t.Fatalf("runBrainSave: %v", err)
	}

	tags, _ := gotBody["tags"].([]any)
	if len(tags) != 2 || tags[0] != "deploy" || tags[1] != "release" {
		t.Fatalf("tags = %#v, want [deploy release] with the blank entry dropped", gotBody["tags"])
	}
	if gotBody["content"] != "Push v0.x.x on main." {
		t.Errorf("content = %#v", gotBody["content"])
	}
	// --pinned was not passed, so the request must not assert a value for it:
	// an unspecified flag is "leave it alone", not "unpin".
	if _, present := gotBody["pinned"]; present {
		t.Errorf("body carries pinned=%#v although the flag was never set", gotBody["pinned"])
	}
}

func TestRunBrainSaveReadsContentFromAFile(t *testing.T) {
	var gotBody map[string]any
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "note-1", "title": "From file"})
	})

	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# Body from a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newBrainSaveTestCmd()
	_ = cmd.Flags().Set("title", "From file")
	_ = cmd.Flags().Set("content-file", path)
	if _, err := captureStdout(t, func() error { return runBrainSave(cmd, nil) }); err != nil {
		t.Fatalf("runBrainSave: %v", err)
	}
	if gotBody["content"] != "# Body from a file\n" {
		t.Fatalf("content = %#v, want the file body", gotBody["content"])
	}
}

// Passing both content sources is a mistake with two plausible outcomes, so it
// is refused rather than silently resolved.
func TestRunBrainSaveRejectsBothContentSources(t *testing.T) {
	cmd := newBrainSaveTestCmd()
	_ = cmd.Flags().Set("title", "x")
	_ = cmd.Flags().Set("content", "inline")
	_ = cmd.Flags().Set("content-file", "/tmp/whatever.md")
	if err := runBrainSave(cmd, nil); err == nil {
		t.Fatal("runBrainSave accepted both --content and --content-file")
	}
}

func TestRunBrainSaveRequiresATitleForANewNote(t *testing.T) {
	if err := runBrainSave(newBrainSaveTestCmd(), nil); err == nil {
		t.Fatal("runBrainSave created a note without a title")
	}
}

// Updating sends back the revision the server currently holds, which is what
// makes a concurrent edit a 409 instead of a silent overwrite.
func TestRunBrainSaveUpdateSendsTheServerRevision(t *testing.T) {
	var patched map[string]any
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "note-1", "title": "Old", "revision": 7})
		case http.MethodPatch:
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &patched)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "note-1", "title": "New", "revision": 8})
		default:
			http.NotFound(w, r)
		}
	})

	cmd := newBrainSaveTestCmd()
	_ = cmd.Flags().Set("id", "note-1")
	_ = cmd.Flags().Set("content", "new body")
	if _, err := captureStdout(t, func() error { return runBrainSave(cmd, nil) }); err != nil {
		t.Fatalf("runBrainSave: %v", err)
	}
	if patched["revision"] != float64(7) {
		t.Fatalf("revision = %#v, want the 7 the server reported", patched["revision"])
	}
}

func TestRunBrainListPassesTheFiltersThrough(t *testing.T) {
	var gotQuery string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{"id": "note-1", "title": "Deploys", "tags": []string{"deploy"}, "source": "manual"}},
			"tags":  []string{"deploy"},
		})
	})

	cmd := newBrainListTestCmd()
	_ = cmd.Flags().Set("search", "pgbouncer")
	_ = cmd.Flags().Set("tag", "db")
	_ = cmd.Flags().Set("archived", "true")
	out, err := captureStdout(t, func() error { return runBrainList(cmd, nil) })
	if err != nil {
		t.Fatalf("runBrainList: %v", err)
	}
	for _, want := range []string{"search=pgbouncer", "tag=db", "archived=true"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if !strings.Contains(out, "Deploys") {
		t.Errorf("table output missing the note title:\n%s", out)
	}
}

// --- capture inbox ---------------------------------------------------------

func newBrainCaptureTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "capture"}
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("kind", "", "")
	cmd.Flags().String("title-hint", "", "")
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "text", "")
	return cmd
}

func newBrainInboxTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "inbox"}
	cmd.Flags().String("status", "raw", "")
	cmd.Flags().Int("limit", 0, "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func newBrainOrganizeTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "organize"}
	cmd.Flags().String("as", "", "")
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("tags", "", "")
	cmd.Flags().String("content", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().Bool("pinned", false, "")
	cmd.Flags().String("output", "text", "")
	return cmd
}

func newBrainSearchTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "search"}
	cmd.Flags().String("tag", "", "")
	cmd.Flags().Bool("archived", false, "")
	cmd.Flags().Int("limit", 0, "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

// A capture from the CLI must declare origin "cli" and must NOT declare a
// kind it was not given: the server infers link vs text, and a wrong kind
// here would file a bare URL as prose.
func TestRunBrainCaptureSendsTheCLIOriginAndNoInferredKind(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"capture": map[string]any{"id": "cap-1", "kind": "link", "status": "raw"}})
	})

	cmd := newBrainCaptureTestCmd()
	_ = cmd.Flags().Set("url", "https://example.com/post")
	_ = cmd.Flags().Set("title-hint", "A post to read")
	out, err := captureStdout(t, func() error { return runBrainCapture(cmd, []string{"worth", "keeping"}) })
	if err != nil {
		t.Fatalf("runBrainCapture: %v", err)
	}
	if gotPath != "/api/brain/captures" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["origin"] != "cli" {
		t.Errorf("origin = %#v, want cli", gotBody["origin"])
	}
	if gotBody["content"] != "worth keeping" {
		t.Errorf("content = %#v", gotBody["content"])
	}
	if gotBody["url"] != "https://example.com/post" {
		t.Errorf("url = %#v", gotBody["url"])
	}
	if gotBody["title_hint"] != "A post to read" {
		t.Errorf("title_hint = %#v", gotBody["title_hint"])
	}
	if _, present := gotBody["kind"]; present {
		t.Errorf("body asserts kind=%#v although --kind was never set; the server infers it", gotBody["kind"])
	}
	if !strings.Contains(out, "cap-1") {
		t.Errorf("output does not name the capture:\n%s", out)
	}
}

func TestRunBrainCaptureRejectsBadFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags map[string]string
		args  []string
	}{
		// image/audio/file are upload-only; --kind here would be refused by
		// the server with a 400, so the CLI says so before the round trip.
		{name: "upload kind", flags: map[string]string{"kind": "image"}, args: []string{"x"}},
		// The kind of an uploaded file comes from its content type; letting
		// --kind through would allow the two to disagree.
		{name: "kind with file", flags: map[string]string{"kind": "text", "file": "/tmp/whatever.png"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newBrainCaptureTestCmd()
			for k, v := range tc.flags {
				_ = cmd.Flags().Set(k, v)
			}
			if err := runBrainCapture(cmd, tc.args); err == nil {
				t.Fatal("runBrainCapture accepted the flags")
			}
		})
	}
}

func TestRunBrainCaptureRefusesAnEmptyCapture(t *testing.T) {
	if stdinIsPiped() {
		t.Skip("stdin is piped in this environment; a bare capture legitimately reads it")
	}
	if err := runBrainCapture(newBrainCaptureTestCmd(), nil); err == nil {
		t.Fatal("runBrainCapture captured nothing at all")
	}
}

func TestRunBrainCaptureUploadsAFileAsMultipart(t *testing.T) {
	var gotPath, gotContentType, gotOrigin, gotFilename string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotOrigin = r.FormValue("origin")
		if fh := r.MultipartForm.File["file"]; len(fh) == 1 {
			gotFilename = fh[0].Filename
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"capture": map[string]any{"id": "cap-2", "kind": "audio", "transcription_status": "pending"}})
	})

	path := filepath.Join(t.TempDir(), "memo.m4a")
	if err := os.WriteFile(path, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newBrainCaptureTestCmd()
	_ = cmd.Flags().Set("file", path)
	out, err := captureStdout(t, func() error { return runBrainCapture(cmd, nil) })
	if err != nil {
		t.Fatalf("runBrainCapture: %v", err)
	}
	if gotPath != "/api/brain/captures/upload" {
		t.Errorf("path = %q, want the upload endpoint", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("content type = %q", gotContentType)
	}
	if gotOrigin != "cli" {
		t.Errorf("origin = %q, want cli", gotOrigin)
	}
	if gotFilename != "memo.m4a" {
		t.Errorf("filename = %q", gotFilename)
	}
	// A pending transcription is the one thing the user cannot see from the
	// capture id alone, so it must be said.
	if !strings.Contains(out, "Transcription") {
		t.Errorf("output does not mention the pending transcription:\n%s", out)
	}
}

func TestRunBrainInboxPassesTheStatusAndPrintsTheRawCount(t *testing.T) {
	var gotQuery string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"captures": []map[string]any{
				{"id": "cap-1", "kind": "link", "status": "raw", "origin": "mobile", "title_hint": "Read this"},
				{"id": "cap-2", "kind": "text", "status": "raw", "origin": "cli", "content": "\n\nfirst real line\nsecond"},
			},
			"raw_count": 7,
		})
	})

	cmd := newBrainInboxTestCmd()
	_ = cmd.Flags().Set("status", "all")
	_ = cmd.Flags().Set("limit", "50")
	out, err := captureStdout(t, func() error { return runBrainInbox(cmd, nil) })
	if err != nil {
		t.Fatalf("runBrainInbox: %v", err)
	}
	for _, want := range []string{"status=all", "limit=50"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	// The title hint wins over the body; a body-only capture falls back to
	// its first non-blank line, not to the leading blank ones.
	for _, want := range []string{"Read this", "first real line", "7 capture(s) still raw"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunBrainInboxRejectsAnUnknownStatus(t *testing.T) {
	cmd := newBrainInboxTestCmd()
	_ = cmd.Flags().Set("status", "pending")
	if err := runBrainInbox(cmd, nil); err == nil {
		t.Fatal("runBrainInbox accepted an unknown status")
	}
}

func TestRunBrainOrganizeSendsTheActionAndTags(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"capture": map[string]any{"id": "cap-1", "status": "organized"},
			"note":    map[string]any{"id": "note-9", "title": "Deploys"},
		})
	})

	cmd := newBrainOrganizeTestCmd()
	_ = cmd.Flags().Set("as", "note")
	_ = cmd.Flags().Set("tags", "deploy, release ,")
	out, err := captureStdout(t, func() error { return runBrainOrganize(cmd, []string{"cap-1"}) })
	if err != nil {
		t.Fatalf("runBrainOrganize: %v", err)
	}
	if gotPath != "/api/brain/captures/cap-1/organize" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["action"] != "note" {
		t.Errorf("action = %#v", gotBody["action"])
	}
	tags, _ := gotBody["tags"].([]any)
	if len(tags) != 2 || tags[0] != "deploy" {
		t.Errorf("tags = %#v, want the blank entry dropped", gotBody["tags"])
	}
	if _, present := gotBody["pinned"]; present {
		t.Errorf("body carries pinned=%#v although the flag was never set", gotBody["pinned"])
	}
	if !strings.Contains(out, "note-9") {
		t.Errorf("output does not name the created note:\n%s", out)
	}
}

// A merge without a target would 400 at the server; refusing locally keeps
// the mistake one message instead of a round trip.
func TestRunBrainOrganizeRejectsBadActions(t *testing.T) {
	for _, tc := range []struct{ name, action, note string }{
		{name: "unknown action", action: "file"},
		{name: "merge without a note", action: "merge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newBrainOrganizeTestCmd()
			_ = cmd.Flags().Set("as", tc.action)
			if tc.note != "" {
				_ = cmd.Flags().Set("note", tc.note)
			}
			if err := runBrainOrganize(cmd, []string{"cap-1"}); err == nil {
				t.Fatal("runBrainOrganize accepted the action")
			}
		})
	}
}

func TestRunBrainReopenPostsToTheReopenEndpoint(t *testing.T) {
	var gotPath string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"capture": map[string]any{"id": "cap-1", "status": "raw"}})
	})
	cmd := &cobra.Command{Use: "reopen"}
	cmd.Flags().String("output", "text", "")
	if _, err := captureStdout(t, func() error { return runBrainReopen(cmd, []string{"cap-1"}) }); err != nil {
		t.Fatalf("runBrainReopen: %v", err)
	}
	if gotPath != "/api/brain/captures/cap-1/reopen" {
		t.Errorf("path = %q", gotPath)
	}
}

// A 503 from /suggest means the workspace has no assist-layer model. That is
// a configuration answer, not a transient failure, so the message must say so
// instead of surfacing the raw status.
func TestRunBrainSuggestExplainsAMissingModel(t *testing.T) {
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "no assist model configured"})
	})
	cmd := &cobra.Command{Use: "suggest"}
	cmd.Flags().String("output", "text", "")
	err := runBrainSuggest(cmd, []string{"cap-1"})
	if err == nil {
		t.Fatal("runBrainSuggest reported success on a 503")
	}
	if !strings.Contains(err.Error(), "no model configured") {
		t.Fatalf("error = %q, want it to name the missing model", err.Error())
	}
}

func TestRunBrainSuggestPrintsTheSuggestion(t *testing.T) {
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"capture": map[string]any{
			"id": "cap-1",
			"suggestion": map[string]any{
				"action": "merge", "title": "Deploys go through the release tag",
				"tags": []string{"deploy"}, "merge_note": map[string]any{"id": "note-3", "title": "Deploys"},
			},
		}})
	})
	cmd := &cobra.Command{Use: "suggest"}
	cmd.Flags().String("output", "text", "")
	out, err := captureStdout(t, func() error { return runBrainSuggest(cmd, []string{"cap-1"}) })
	if err != nil {
		t.Fatalf("runBrainSuggest: %v", err)
	}
	for _, want := range []string{"merge", "note-3", "deploy"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunBrainSearchPassesTheQueryAndStripsHighlightMarkers(t *testing.T) {
	var gotQuery string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"notes": []map[string]any{{
				"id": "note-1", "title": "Deploys", "score": 0.0328,
				"snippet": "push <mark>v0.x.x</mark>\non main",
			}},
			"vector": false,
		})
	})

	cmd := newBrainSearchTestCmd()
	_ = cmd.Flags().Set("tag", "deploy")
	_ = cmd.Flags().Set("archived", "true")
	_ = cmd.Flags().Set("limit", "5")
	out, err := captureStdout(t, func() error { return runBrainSearch(cmd, []string{"release tag"}) })
	if err != nil {
		t.Fatalf("runBrainSearch: %v", err)
	}
	for _, want := range []string{"q=release+tag", "tag=deploy", "archived=true", "limit=5"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if strings.Contains(out, "<mark>") {
		t.Errorf("table output still carries HTML highlight markers:\n%s", out)
	}
	for _, want := range []string{"v0.x.x on main", "0.0328", "no embeddings model"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunBrainDeleteWithYesSkipsThePrompt(t *testing.T) {
	var gotMethod, gotPath string
	brainTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().Bool("yes", false, "")
	_ = cmd.Flags().Set("yes", "true")
	if _, err := captureStdout(t, func() error { return runBrainDelete(cmd, []string{"cap-1"}) }); err != nil {
		t.Fatalf("runBrainDelete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/brain/captures/cap-1" {
		t.Fatalf("request = %s %s, want DELETE /api/brain/captures/cap-1", gotMethod, gotPath)
	}
}
