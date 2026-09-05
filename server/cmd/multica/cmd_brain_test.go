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
