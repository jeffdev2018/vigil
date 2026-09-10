package main

// `multica autopilot draft` (JEF-373) — draft prints and writes nothing;
// --create goes to /propose with a resolved agent; 503 reads as "no model
// configured" rather than a transport failure.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newAutopilotDraftTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "draft"}
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().Bool("create", false, "")
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func autopilotDraftCLIServer(t *testing.T, hits *[]string, captured *map[string]any, draftStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits = append(*hits, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "22222222-2222-2222-2222-222222222222", "name": "CodeBot"},
			})
		case "/api/autopilots/draft":
			if draftStatus != http.StatusOK {
				w.WriteHeader(draftStatus)
				_, _ = w.Write([]byte(`{"error":"no model is configured to draft; write the schedule yourself"}`))
				return
			}
			_ = json.NewDecoder(r.Body).Decode(captured)
			_ = json.NewEncoder(w).Encode(map[string]any{"draft": map[string]any{
				"title": "Open tickets", "cron_expression": "0 9 * * 1", "timezone": "Europe/Paris",
				"description": "List the open tickets", "execution_mode": "create_issue",
				"issue_title_template": "Open tickets {{date}}", "reason": "weekly on Monday",
				"next_runs": []string{"2026-09-14T07:00:00Z"}, "model": "gpt-x",
			}})
		case "/api/autopilots/propose":
			_ = json.NewDecoder(r.Body).Decode(captured)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"autopilot":   map[string]any{"id": "ap-1", "title": "Open tickets", "status": "paused"},
				"decision_id": "dec-1",
				"next_runs":   []string{"2026-09-14T07:00:00Z"},
			})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/issues/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-1", "identifier": "MUL-1", "title": "t", "status": "todo", "priority": "none"})
				return
			}
			http.NotFound(w, r)
		}
	}))
}

func autopilotDraftEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunAutopilotDraft_PreviewWritesNothing(t *testing.T) {
	var hits []string
	var body map[string]any
	srv := autopilotDraftCLIServer(t, &hits, &body, http.StatusOK)
	defer srv.Close()
	autopilotDraftEnv(t, srv.URL)

	cmd := newAutopilotDraftTestCmd()
	_ = cmd.Flags().Set("timezone", "Europe/Paris")
	if err := runAutopilotDraft(cmd, []string{"every Monday at 9, list the open tickets"}); err != nil {
		t.Fatalf("runAutopilotDraft: %v", err)
	}
	for _, h := range hits {
		if strings.Contains(h, "/propose") {
			t.Fatalf("a bare draft reached %s; it must write nothing", h)
		}
	}
	if got := body["text"]; got != "every Monday at 9, list the open tickets" {
		t.Errorf("text = %#v, want the sentence verbatim", got)
	}
	if got := body["timezone"]; got != "Europe/Paris" {
		t.Errorf("timezone = %#v", got)
	}
}

func TestRunAutopilotDraft_CreateProposesWithResolvedAgent(t *testing.T) {
	var hits []string
	var body map[string]any
	srv := autopilotDraftCLIServer(t, &hits, &body, http.StatusOK)
	defer srv.Close()
	autopilotDraftEnv(t, srv.URL)

	cmd := newAutopilotDraftTestCmd()
	_ = cmd.Flags().Set("create", "true")
	_ = cmd.Flags().Set("agent", "CodeBot")
	_ = cmd.Flags().Set("issue", "MUL-1")
	if err := runAutopilotDraft(cmd, []string{"every Monday at 9, list the open tickets"}); err != nil {
		t.Fatalf("runAutopilotDraft: %v", err)
	}
	if got := body["assignee_id"]; got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("assignee_id = %#v, want CodeBot's resolved id", got)
	}
	if got := body["issue_id"]; got != "issue-1" {
		t.Errorf("issue_id = %#v, want the resolved issue", got)
	}
	var proposed bool
	for _, h := range hits {
		proposed = proposed || strings.Contains(h, "/api/autopilots/propose")
	}
	if !proposed {
		t.Errorf("--create never called /propose: %v", hits)
	}
}

func TestRunAutopilotDraft_RefusesCreateWithoutAgent(t *testing.T) {
	autopilotDraftEnv(t, "http://127.0.0.1:1")
	cmd := newAutopilotDraftTestCmd()
	_ = cmd.Flags().Set("create", "true")
	err := runAutopilotDraft(cmd, []string{"every Monday at 9"})
	if err == nil || !strings.Contains(err.Error(), "--agent") {
		t.Fatalf("error = %v, want a refusal naming --agent", err)
	}
}

func TestRunAutopilotDraft_ExplainsAMissingModel(t *testing.T) {
	var hits []string
	var body map[string]any
	srv := autopilotDraftCLIServer(t, &hits, &body, http.StatusServiceUnavailable)
	defer srv.Close()
	autopilotDraftEnv(t, srv.URL)

	cmd := newAutopilotDraftTestCmd()
	err := runAutopilotDraft(cmd, []string{"every Monday at 9"})
	if err == nil {
		t.Fatalf("expected the 503 to surface")
	}
	if !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("error %q should say no model is configured", err.Error())
	}
}
