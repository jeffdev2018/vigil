package daemon

import (
	"context"
	"encoding/json"
	"testing"
)

// Approval gates (K05): which MCP tools pause for a human is the workspace's
// decision, and it arrives on the claim. One daemon serves several
// workspaces, so reading it from the daemon's own environment could only ever
// be wrong; the run's own pattern replaces the compiled default outright.
func TestRemoteMCPToolGateUsesTheTaskPattern(t *testing.T) {
	srv, calls := fakeGateServer(t, "denied")
	d := &Daemon{cfg: Config{ServerBaseURL: srv.URL}}
	params := json.RawMessage(`{"arguments":{}}`)

	// The workspace widened the pattern to its own vocabulary.
	widened := Task{ID: "task-1", AuthToken: "mat_test", SensitiveTools: `(?i)deploy|rotate_key`}
	gate := d.remoteMCPToolGate(widened, nil)
	if gate == nil {
		t.Fatal("a task with a scoped token must be gated")
	}
	if ok, _ := gate(context.Background(), "deploy_service", params); ok {
		t.Error("a tool the workspace named sensitive must be asked about, and a denial must refuse it")
	}
	if calls.Load() == 0 {
		t.Fatal("the gate never reached the server: the workspace pattern was not consulted")
	}
	before := calls.Load()
	// "merge" is in the compiled default and NOT in the workspace's pattern.
	// The workspace replaces the default, it does not add to it.
	if ok, _ := gate(context.Background(), "merge_pull_request", params); !ok {
		t.Error("a tool outside the workspace's pattern must run without asking")
	}
	if calls.Load() != before {
		t.Error("a tool outside the workspace's pattern must not open a gate")
	}

	// No pattern on the claim — an older server, or a failed workspace read —
	// keeps the compiled default rather than gating nothing.
	fallback := d.remoteMCPToolGate(Task{ID: "task-2", AuthToken: "mat_test"}, nil)
	if ok, _ := fallback(context.Background(), "delete_repository", params); ok {
		t.Error("an empty pattern must fall back to the compiled default, never to an open gate")
	}
}
