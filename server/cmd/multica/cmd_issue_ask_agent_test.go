package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `multica issue ask-agent` (F19 / JEF-32) — flag contract.
//
// The server owns every decision that matters (permission, depth, budget), so
// what is worth pinning here is the small set of refusals the CLI makes BEFORE
// the request goes out — each one turns an opaque 400 into a sentence naming the
// allowed values — and the shape of the body it sends when it does.

func newAskAgentTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ask-agent"}
	cmd.Flags().String("to", "", "")
	cmd.Flags().String("intent", "", "")
	cmd.Flags().String("body", "", "")
	cmd.Flags().Bool("body-stdin", false, "")
	cmd.Flags().String("body-file", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func askAgentCLIServer(t *testing.T, captured *map[string]any, capturedPath *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "22222222-2222-2222-2222-222222222222", "name": "CodeBot"},
			})
		case strings.Contains(r.URL.Path, "/agent-messages"):
			*capturedPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
				t.Errorf("decode agent-message body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "c1", "a2a_intent": "review"})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "title": "t",
				"status": "todo", "priority": "none",
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunIssueAskAgent_SendsIntentAndResolvedRecipient(t *testing.T) {
	var body map[string]any
	var path string
	srv := askAgentCLIServer(t, &body, &path)
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newAskAgentTestCmd()
	_ = cmd.Flags().Set("to", "CodeBot")
	_ = cmd.Flags().Set("intent", "review")
	_ = cmd.Flags().Set("body", "look at the migration please")
	_ = cmd.Flags().Set("output", "table")
	if err := runIssueAskAgent(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueAskAgent: %v", err)
	}

	if !strings.HasSuffix(path, "/agent-messages") {
		t.Errorf("posted to %q, want the agent-messages endpoint", path)
	}
	if got := body["intent"]; got != "review" {
		t.Errorf("intent = %#v, want review", got)
	}
	if got := body["to_agent_id"]; got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("to_agent_id = %#v, want CodeBot's resolved id", got)
	}
	if got := body["body"]; got != "look at the migration please" {
		t.Errorf("body = %#v, want the message verbatim", got)
	}
	// The client must NOT compose the mention markup: the server does, so the
	// declared intent and the agent it addresses cannot disagree.
	if s, _ := body["body"].(string); strings.Contains(s, "mention://") {
		t.Errorf("client composed mention markup into the body: %q", s)
	}
	if _, ok := body["content"]; ok {
		t.Errorf("client sent a `content` field; this is not the comment endpoint: %#v", body)
	}
}

func TestRunIssueAskAgent_RefusesBadFlagsBeforeCallingTheServer(t *testing.T) {
	// No server: any of these reaching the network would fail differently, which
	// is itself the assertion that they are refused client-side.
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cases := map[string]struct {
		flags map[string]string
		want  string
	}{
		"missing recipient": {
			flags: map[string]string{"intent": "question", "body": "hi"},
			want:  "--to",
		},
		"missing intent": {
			flags: map[string]string{"to": "CodeBot", "body": "hi"},
			want:  "question, review, handoff",
		},
		"unknown intent": {
			flags: map[string]string{"to": "CodeBot", "intent": "gossip", "body": "hi"},
			want:  "question, review, handoff",
		},
		"missing body": {
			flags: map[string]string{"to": "CodeBot", "intent": "handoff"},
			want:  "--body",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := newAskAgentTestCmd()
			for k, v := range tc.flags {
				_ = cmd.Flags().Set(k, v)
			}
			err := runIssueAskAgent(cmd, []string{"MUL-1"})
			if err == nil {
				t.Fatalf("expected a refusal, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should name %q", err.Error(), tc.want)
			}
		})
	}
}
