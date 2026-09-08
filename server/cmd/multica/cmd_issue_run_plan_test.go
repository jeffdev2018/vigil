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

// F04 · `multica issue run-plan set`. The server owns what a valid plan is, so
// what these tests pin is the shape the CLI hands it: --item parsing (the last
// colon separates the status), the stdin fallback, and the refusal to POST an
// empty plan.

func newRunPlanSetTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("item", nil, "")
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

// planServer answers the two calls the command makes and captures the body.
func planServer(t *testing.T, taskID string, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/"+taskID+"/plan" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"items": (*body)["items"], "seq": 1000001})
	}))
}

func setPlanEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

// withStdin replaces os.Stdin with a pipe carrying `content` for one call.
func withStdin(t *testing.T, content string) {
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

func TestRunIssueRunPlanSetPostsItemFlags(t *testing.T) {
	const taskID = "11111111-1111-4111-8111-111111111111"
	var body map[string]any
	srv := planServer(t, taskID, &body)
	defer srv.Close()
	setPlanEnv(t, srv.URL)

	cmd := newRunPlanSetTestCmd()
	// The first item's text carries a colon of its own: only the LAST one
	// separates the status, or a plan line can never mention a package path.
	_ = cmd.Flags().Set("item", "fix: the parser:in_progress")
	_ = cmd.Flags().Set("item", "  update the docs  :pending")
	if err := runIssueRunPlanSet(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueRunPlanSet: %v", err)
	}

	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v, want two entries", body["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["text"] != "fix: the parser" || first["status"] != "in_progress" {
		t.Errorf("first item = %#v, want text %q status %q", first, "fix: the parser", "in_progress")
	}
	second, _ := items[1].(map[string]any)
	if second["text"] != "update the docs" || second["status"] != "pending" {
		t.Errorf("second item = %#v, want the text trimmed and status pending", second)
	}
}

func TestRunIssueRunPlanSetReadsStdinJSON(t *testing.T) {
	const taskID = "22222222-2222-4222-8222-222222222222"
	var body map[string]any
	srv := planServer(t, taskID, &body)
	defer srv.Close()
	setPlanEnv(t, srv.URL)
	withStdin(t, `{"items":[{"text":"read the spec","status":"done"}]}`)

	cmd := newRunPlanSetTestCmd()
	if err := runIssueRunPlanSet(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueRunPlanSet: %v", err)
	}

	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want the single stdin entry", body["items"])
	}
	if first, _ := items[0].(map[string]any); first["text"] != "read the spec" {
		t.Errorf("item = %#v, want the stdin document passed through untouched", first)
	}
}

func TestRunIssueRunPlanSetRejectsUnusableInput(t *testing.T) {
	const taskID = "33333333-3333-4333-8333-333333333333"

	cases := []struct {
		name  string
		items []string
		stdin string
		want  string
	}{
		{name: "empty stdin", stdin: "", want: "no plan given"},
		{name: "stdin is not an object", stdin: "[1,2,3]", want: "JSON object"},
		{name: "stdin object without items", stdin: `{"plan":[]}`, want: "missing the items array"},
		{name: "item without a status", items: []string{"just some text"}, want: "has no status"},
		{name: "item without text", items: []string{":pending"}, want: "has no text"},
		{name: "item with an empty status", items: []string{"text:"}, want: "has no status"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No server: an unusable plan must fail before anything is sent, so
			// a request here would fail the test by connection error anyway.
			setPlanEnv(t, "http://127.0.0.1:1")
			if len(tc.items) == 0 {
				withStdin(t, tc.stdin)
			}
			cmd := newRunPlanSetTestCmd()
			for _, item := range tc.items {
				_ = cmd.Flags().Set("item", item)
			}
			err := runIssueRunPlanSet(cmd, []string{taskID})
			if err == nil {
				t.Fatalf("runIssueRunPlanSet accepted %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q so the agent can fix the call", err, tc.want)
			}
		})
	}
}
