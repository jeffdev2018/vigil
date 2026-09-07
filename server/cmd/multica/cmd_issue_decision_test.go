package main

import (
	"encoding/json"
	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueDecisionCLIRequestAndGet(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/issues/MUL-1":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "issue-uuid", "identifier": "MUL-1"})
		case "/api/issues/issue-uuid/decisions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.Method != "POST" || body["id"] != "stable-id" || body["source_task_id"] != "source-id" || body["recipient_id"] != "human-id" || body["question"] != "A or B?" {
				t.Errorf("unexpected request: %s %+v", r.Method, body)
			}
			requests++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "stable-id", "status": "open"})
		case "/api/issues/issue-uuid/decisions/stable-id":
			if r.Method != "GET" {
				t.Error("expected GET")
			}
			requests++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "stable-id", "status": "answered", "answer": "A"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	cmd := &cobra.Command{}
	for name, value := range map[string]string{"id": "stable-id", "source-run": "source-id", "recipient": "human-id", "question": "A or B?", "context": "Tradeoffs"} {
		cmd.Flags().String(name, value, "")
	}
	cmd.Flags().StringArray("option", []string{"A", "B"}, "")
	_ = cmd.Flags().Set("question", "A or B?")
	_ = cmd.Flags().Set("context", "Tradeoffs")
	if err := issueDecisionRequestCmd.RunE(cmd, []string{"MUL-1"}); err != nil {
		t.Fatal(err)
	}
	if err := issueDecisionGetCmd.RunE(cmd, []string{"MUL-1", "stable-id"}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}
