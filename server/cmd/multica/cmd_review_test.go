package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// F06 · `multica review flag`. The server owns what a valid flag is, so what
// these tests pin is the shape the CLI hands it: the --line range grammar, the
// confidence bounds, and the stdin body.

const reviewFlagTestIssue = "22222222-2222-4222-8222-222222222222"

func newReviewFlagAddTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("pr", "", "")
	cmd.Flags().String("sha", "", "")
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("line", "", "")
	cmd.Flags().String("side", "new", "")
	cmd.Flags().String("severity", "", "")
	cmd.Flags().Int("confidence", -1, "")
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("body", "", "")
	_ = cmd.Flags().Set("issue", reviewFlagTestIssue)
	_ = cmd.Flags().Set("pr", "33333333-3333-4333-8333-333333333333")
	_ = cmd.Flags().Set("file", "server/internal/handler/review_flag.go")
	_ = cmd.Flags().Set("severity", "bug")
	_ = cmd.Flags().Set("title", "the retry path swallows the error")
	return cmd
}

// reviewFlagServer answers the one call the command makes and captures the body.
func reviewFlagServer(t *testing.T, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/issues/"+reviewFlagTestIssue+"/review-flags") {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			(*body)["state"] = r.URL.Query().Get("state")
			json.NewEncoder(w).Encode(map[string]any{"flags": []any{}, "counts": map[string]any{}})
			return
		}
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "flag-1"})
	}))
}

func setReviewEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

// withReviewStdin replaces os.Stdin with a pipe carrying `content` for one call.
func withReviewStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	w.Close()
	original := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = original
		r.Close()
	})
}

func TestParseLineRange(t *testing.T) {
	cases := []struct {
		in          string
		start, end  int
		wantErr     bool
		description string
	}{
		{in: "42", start: 42, end: 42, description: "a single line is a one-line range"},
		{in: " 42 - 48 ", start: 42, end: 48, description: "surrounding space is not part of the number"},
		{in: "42-48", start: 42, end: 48},
		{in: "1-1", start: 1, end: 1},
		{in: "", wantErr: true, description: "no line at all"},
		{in: "0", wantErr: true, description: "line numbers start at 1"},
		{in: "48-42", wantErr: true, description: "an inverted range would silently point elsewhere"},
		{in: "abc", wantErr: true},
		{in: "42-", wantErr: true},
		{in: "42-x", wantErr: true},
	}
	for _, tc := range cases {
		start, end, err := parseLineRange(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseLineRange(%q) accepted it — %s", tc.in, tc.description)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLineRange(%q): %v", tc.in, err)
			continue
		}
		if start != tc.start || end != tc.end {
			t.Errorf("parseLineRange(%q) = %d-%d, want %d-%d", tc.in, start, end, tc.start, tc.end)
		}
	}
}

func TestReviewFlagAddPostsTheRangeAndOptionalFields(t *testing.T) {
	body := map[string]any{}
	srv := reviewFlagServer(t, &body)
	defer srv.Close()
	setReviewEnv(t, srv.URL)

	cmd := newReviewFlagAddTestCmd()
	_ = cmd.Flags().Set("line", "42-48")
	_ = cmd.Flags().Set("confidence", "80")
	_ = cmd.Flags().Set("side", "old")
	_ = cmd.Flags().Set("sha", "abc123")
	if err := runReviewFlagAdd(cmd, nil); err != nil {
		t.Fatalf("runReviewFlagAdd: %v", err)
	}
	if body["line_start"] != float64(42) || body["line_end"] != float64(48) {
		t.Errorf("range = %v-%v, want 42-48", body["line_start"], body["line_end"])
	}
	if body["confidence"] != float64(80) || body["side"] != "old" || body["head_sha"] != "abc123" {
		t.Errorf("optional fields = %+v", body)
	}
}

// An omitted --confidence must not reach the server as 0: "did not say" and
// "0% sure" sort differently, and 0 would claim certainty of worthlessness.
func TestReviewFlagAddOmitsUnsetConfidenceAndSha(t *testing.T) {
	body := map[string]any{}
	srv := reviewFlagServer(t, &body)
	defer srv.Close()
	setReviewEnv(t, srv.URL)

	cmd := newReviewFlagAddTestCmd()
	_ = cmd.Flags().Set("line", "7")
	if err := runReviewFlagAdd(cmd, nil); err != nil {
		t.Fatalf("runReviewFlagAdd: %v", err)
	}
	if _, present := body["confidence"]; present {
		t.Errorf("confidence sent as %v when the flag was never set", body["confidence"])
	}
	if _, present := body["head_sha"]; present {
		t.Errorf("head_sha sent as %v when --sha was never set", body["head_sha"])
	}
	if body["line_start"] != float64(7) || body["line_end"] != float64(7) {
		t.Errorf("single line = %v-%v, want 7-7", body["line_start"], body["line_end"])
	}
	if body["side"] != "new" {
		t.Errorf("side = %v, want the new-side default", body["side"])
	}
}

func TestReviewFlagAddReadsBodyFromStdin(t *testing.T) {
	body := map[string]any{}
	srv := reviewFlagServer(t, &body)
	defer srv.Close()
	setReviewEnv(t, srv.URL)
	withReviewStdin(t, "line one\n\nline two\n")

	cmd := newReviewFlagAddTestCmd()
	_ = cmd.Flags().Set("line", "12")
	_ = cmd.Flags().Set("body", "-")
	if err := runReviewFlagAdd(cmd, nil); err != nil {
		t.Fatalf("runReviewFlagAdd: %v", err)
	}
	if body["body"] != "line one\n\nline two" {
		t.Errorf("body = %q, want the stdin content with the trailing newline trimmed", body["body"])
	}
}

// Every refusal below happens before the client is built, so a bad invocation
// never reaches the network.
func TestReviewFlagAddRefusesBadInput(t *testing.T) {
	setReviewEnv(t, "http://127.0.0.1:1")

	cases := []struct {
		name string
		set  map[string]string
	}{
		{"no line", map[string]string{}},
		{"inverted range", map[string]string{"line": "48-42"}},
		{"non-numeric line", map[string]string{"line": "forty-two"}},
		{"confidence over 100", map[string]string{"line": "1", "confidence": "101"}},
		{"negative confidence", map[string]string{"line": "1", "confidence": "-5"}},
		{"unknown severity", map[string]string{"line": "1", "severity": "critical"}},
		{"no title", map[string]string{"line": "1", "title": "   "}},
		{"no file", map[string]string{"line": "1", "file": ""}},
		{"no pr", map[string]string{"line": "1", "pr": ""}},
		{"no issue", map[string]string{"line": "1", "issue": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newReviewFlagAddTestCmd()
			for k, v := range tc.set {
				_ = cmd.Flags().Set(k, v)
			}
			if err := runReviewFlagAdd(cmd, nil); err == nil {
				t.Fatal("accepted an invocation the server would have rejected")
			}
		})
	}
}

func TestReviewFlagListPassesTheStateFilter(t *testing.T) {
	body := map[string]any{}
	srv := reviewFlagServer(t, &body)
	defer srv.Close()
	setReviewEnv(t, srv.URL)

	newListCmd := func(state string) *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().String("issue", "", "")
		cmd.Flags().String("state", "open", "")
		_ = cmd.Flags().Set("issue", reviewFlagTestIssue)
		if state != "" {
			_ = cmd.Flags().Set("state", state)
		}
		return cmd
	}

	if err := runReviewFlagList(newListCmd(""), nil); err != nil {
		t.Fatalf("default list: %v", err)
	}
	if body["state"] != "open" {
		t.Errorf("default state = %v, want open", body["state"])
	}
	if err := runReviewFlagList(newListCmd("all"), nil); err != nil {
		t.Fatalf("all list: %v", err)
	}
	if body["state"] != "all" {
		t.Errorf("state = %v, want all", body["state"])
	}
	if err := runReviewFlagList(newListCmd("resolved"), nil); err == nil {
		t.Error("accepted a state the endpoint does not know")
	}
}
