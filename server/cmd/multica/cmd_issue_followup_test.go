package main

// `multica issue followup` (JEF-373) — the flag contract and the two answers
// the CLI is responsible for turning into something readable: the body it
// sends, and the 429 the budget produces.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newFollowupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "followup"}
	cmd.Flags().String("when", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

// followupCLIServer answers the issue resolver, the agent lookup and the
// follow-up endpoints. status drives what the follow-up POST replies.
func followupCLIServer(t *testing.T, captured *map[string]any, capturedPath, capturedMethod *string, status int, errBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "22222222-2222-2222-2222-222222222222", "name": "CodeBot"},
			})
		case strings.Contains(r.URL.Path, "/followups"):
			*capturedPath = r.URL.Path
			*capturedMethod = r.Method
			if r.Method == http.MethodPost {
				if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
					t.Errorf("decode follow-up body: %v", err)
				}
				if status != http.StatusCreated {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(errBody))
					return
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"followup": map[string]any{
					"id": "f1", "issue_id": "issue-1", "agent_id": "22222222-2222-2222-2222-222222222222",
					"agent_name": "CodeBot", "fires_at": "2026-09-11T09:00:00Z", "note": "check the deploy",
					"scheduled_by_type": "member", "scheduled_by_id": nil, "created_at": "2026-09-10T09:00:00Z",
				}})
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"followups": []map[string]any{{
					"id": "f1", "agent_name": "CodeBot", "fires_at": "2026-09-11T09:00:00Z",
					"note": "check the deploy", "scheduled_by_type": "member", "scheduled_by_id": "u1",
				}},
				"budget": map[string]any{"max_per_agent_per_day": 20, "max_per_workspace_per_day": 200},
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "title": "t", "status": "todo", "priority": "none",
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func followupTestEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunIssueFollowup_SendsWhenNoteAndResolvedAgent(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := followupCLIServer(t, &body, &path, &method, http.StatusCreated, "")
	defer srv.Close()
	followupTestEnv(t, srv.URL)

	cmd := newFollowupTestCmd()
	_ = cmd.Flags().Set("when", "+90")
	_ = cmd.Flags().Set("note", "check the deploy")
	_ = cmd.Flags().Set("agent", "CodeBot")
	if err := runIssueFollowup(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueFollowup: %v", err)
	}

	if method != http.MethodPost || !strings.HasSuffix(path, "/followups") {
		t.Errorf("%s %s, want POST on the followups endpoint", method, path)
	}
	if got := body["when"]; got != "+90" {
		t.Errorf("when = %#v, want the offset verbatim (the server owns the window)", got)
	}
	if got := body["note"]; got != "check the deploy" {
		t.Errorf("note = %#v", got)
	}
	if got := body["agent_id"]; got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("agent_id = %#v, want CodeBot's resolved id", got)
	}
}

func TestRunIssueFollowup_OmitsAgentWhenNotNamed(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := followupCLIServer(t, &body, &path, &method, http.StatusCreated, "")
	defer srv.Close()
	followupTestEnv(t, srv.URL)

	cmd := newFollowupTestCmd()
	_ = cmd.Flags().Set("when", "2026-09-11T09:00:00+02:00")
	if err := runIssueFollowup(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueFollowup: %v", err)
	}
	// No agent_id at all, so the server falls back to the issue's agent
	// assignee instead of being handed an empty string it must reject.
	if _, ok := body["agent_id"]; ok {
		t.Errorf("agent_id was sent without --agent: %#v", body)
	}
}

func TestRunIssueFollowup_PrintsTheBudgetMessageOn429(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := followupCLIServer(t, &body, &path, &method, http.StatusTooManyRequests,
		`{"error":"follow-up budget reached: at most 20 per agent per day"}`)
	defer srv.Close()
	followupTestEnv(t, srv.URL)

	cmd := newFollowupTestCmd()
	_ = cmd.Flags().Set("when", "+90")
	err := runIssueFollowup(cmd, []string{"MUL-1"})
	if err == nil {
		t.Fatalf("expected the budget refusal to surface as an error")
	}
	if !strings.Contains(err.Error(), "at most 20 per agent per day") {
		t.Errorf("error %q must carry the server's budget sentence", err.Error())
	}
}

func TestRunIssueFollowup_RefusesMissingWhenBeforeCallingTheServer(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newFollowupTestCmd()
	err := runIssueFollowup(cmd, []string{"MUL-1"})
	if err == nil || !strings.Contains(err.Error(), "--when") {
		t.Fatalf("error = %v, want a refusal naming --when", err)
	}
}

func TestRunIssueFollowups_ListsAndCancels(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := followupCLIServer(t, &body, &path, &method, http.StatusCreated, "")
	defer srv.Close()
	followupTestEnv(t, srv.URL)

	list := &cobra.Command{Use: "followups"}
	list.Flags().String("output", "table", "")
	list.Flags().Bool("full-id", false, "")
	if err := runIssueFollowups(list, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueFollowups: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("list used %s, want GET", method)
	}

	cancel := &cobra.Command{Use: "followup-cancel"}
	if err := runIssueFollowupCancel(cancel, []string{"MUL-1", "f1"}); err != nil {
		t.Fatalf("runIssueFollowupCancel: %v", err)
	}
	if method != http.MethodDelete || !strings.HasSuffix(path, "/followups/f1") {
		t.Errorf("%s %s, want DELETE on the follow-up", method, path)
	}
}
