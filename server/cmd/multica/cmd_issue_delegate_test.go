package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

// F01 (JEF-5): `multica issue update --delegate` writes the delegate pair, and
// resolves member-or-agent only. The squad refusal is the interesting half —
// the server answers 400 for a squad delegate, so resolving one client-side
// would turn a clear "delegates are member or agent" into an opaque API error.

// delegateCLIServer answers the lookups `issue update` performs and captures
// the PUT body. members/agents/squads are the three resolution sources.
func delegateCLIServer(t *testing.T, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/workspaces/ws-1/members":
			json.NewEncoder(w).Encode([]map[string]any{
				{"user_id": "11111111-1111-1111-1111-111111111111", "name": "Alice"},
			})
		case r.URL.Path == "/api/agents":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "22222222-2222-2222-2222-222222222222", "name": "CodeBot"},
			})
		case r.URL.Path == "/api/squads":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "33333333-3333-3333-3333-333333333333", "name": "Super Human"},
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			if r.Method == http.MethodPut {
				if err := json.NewDecoder(r.Body).Decode(body); err != nil {
					t.Errorf("decode PUT body: %v", err)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "title": "t",
				"status": "todo", "priority": "none",
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunIssueUpdateSendsDelegatePair(t *testing.T) {
	var body map[string]any
	srv := delegateCLIServer(t, &body)
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUpdateTestCmd()
	_ = cmd.Flags().Set("delegate", "Alice")
	if err := runIssueUpdate(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueUpdate: %v", err)
	}
	if got := body["delegate_type"]; got != "member" {
		t.Fatalf("delegate_type = %#v, want \"member\"", got)
	}
	if got := body["delegate_id"]; got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("delegate_id = %#v, want Alice's user id", got)
	}
	// The delegate is not an assignment: sending assignee_* alongside would
	// silently reassign the issue.
	if _, ok := body["assignee_type"]; ok {
		t.Fatalf("--delegate also sent assignee_type: %#v", body)
	}
}

func TestRunIssueUpdateResolvesAgentDelegateByID(t *testing.T) {
	var body map[string]any
	srv := delegateCLIServer(t, &body)
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUpdateTestCmd()
	_ = cmd.Flags().Set("delegate-id", "22222222-2222-2222-2222-222222222222")
	if err := runIssueUpdate(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueUpdate: %v", err)
	}
	if got := body["delegate_type"]; got != "agent" {
		t.Fatalf("delegate_type = %#v, want \"agent\"", got)
	}
}

// The client refuses a squad before the request goes out, with wording that
// names what IS allowed. Left to the server this arrives as a bare 400.
func TestRunIssueUpdateRefusesSquadDelegate(t *testing.T) {
	srv := delegateCLIServer(t, &map[string]any{})
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")

	for name, resolve := range map[string]func() error{
		"by name": func() error {
			_, _, err := resolveAssignee(context.Background(), client, "Super Human", memberOrAgentKinds)
			return err
		},
		"by id": func() error {
			_, _, err := resolveAssigneeByID(context.Background(), client,
				"33333333-3333-3333-3333-333333333333", memberOrAgentKinds)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := resolve()
			if err == nil {
				t.Fatal("a squad resolved as a delegate target")
			}
			if !strings.Contains(err.Error(), "no member or agent") {
				t.Fatalf("error must say what a delegate may be, got: %v", err)
			}
		})
	}
}

// --delegate and --delegate-id are mutually exclusive, like the assignee pair:
// accepting both would make it ambiguous which one won.
func TestRunIssueUpdateDelegateFlagsAreExclusive(t *testing.T) {
	srv := delegateCLIServer(t, &map[string]any{})
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueUpdateTestCmd()
	_ = cmd.Flags().Set("delegate", "Alice")
	_ = cmd.Flags().Set("delegate-id", "22222222-2222-2222-2222-222222222222")
	err := runIssueUpdate(cmd, []string{"MUL-1"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want a mutually-exclusive rejection", err)
	}
}
