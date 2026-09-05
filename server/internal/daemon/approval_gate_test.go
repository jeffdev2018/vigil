package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Approval gates (K05), daemon side: the hook is written and selected
// through the environment, blocks a push to the watched remote until the
// server answers, lets any other remote through without a call, and the
// gate client and tool matcher behave.

func fakeGateServer(t *testing.T, outcome string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/task-1/gates", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer mat_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["gate_type"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-1", "status": "pending"})
	})
	mux.HandleFunc("/api/tasks/task-1/gates/gate-1", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-1", "status": outcome})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func runHook(t *testing.T, hooksDir, serverURL, watchedRemote, remoteURL string) (int, string) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join(hooksDir, "pre-push"), "origin", remoteURL)
	cmd.Stdin = strings.NewReader("refs/heads/main abc refs/heads/main def\n")
	cmd.Env = append(os.Environ(), "MULTICA_TASK_ID=task-1", "MULTICA_TOKEN=mat_test", "MULTICA_SERVER_URL="+serverURL, "MULTICA_WORKSPACE_ID=ws-1", "MULTICA_GATE_REMOTE="+watchedRemote, "MULTICA_GATE_TIMEOUT=60")
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run hook: %v\n%s", err, out)
	}
	return code, string(out)
}

func TestPrePushHookGatesTheWatchedRemoteOnly(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	root := t.TempDir()
	hooksDir, err := ensureGateHooksDir(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(hooksDir, "pre-push"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("hook must be executable: %v %v", info, err)
	}
	env := gateEnvironment(hooksDir, "git@github.com:org/repo.git", 30*time.Minute)
	if env["GIT_CONFIG_KEY_0"] != "core.hooksPath" || env["GIT_CONFIG_VALUE_0"] != hooksDir || env["MULTICA_GATE_TIMEOUT"] != "1800" {
		t.Fatalf("env = %v", env)
	}

	approve, calls := fakeGateServer(t, "approved")
	if code, out := runHook(t, hooksDir, approve.URL, "git@github.com:org/repo.git", "git@github.com:org/repo.git"); code != 0 || !strings.Contains(out, "approved") {
		t.Fatalf("approved push: exit %d, %s", code, out)
	}
	if calls.Load() < 2 {
		t.Fatalf("expected an open and a poll, got %d calls", calls.Load())
	}
	deny, _ := fakeGateServer(t, "denied")
	if code, out := runHook(t, hooksDir, deny.URL, "git@github.com:org/repo.git", "git@github.com:org/repo.git"); code != 1 || !strings.Contains(out, "denied") {
		t.Fatalf("denied push: exit %d, %s", code, out)
	}
	// A personal fork is not the watched remote: no call, no block.
	fork, forkCalls := fakeGateServer(t, "denied")
	if code, _ := runHook(t, hooksDir, fork.URL, "git@github.com:org/repo.git", "git@github.com:me/fork.git"); code != 0 || forkCalls.Load() != 0 {
		t.Fatalf("fork push: exit %d, calls %d", code, forkCalls.Load())
	}
	// A server that cannot be reached refuses the push rather than letting it through.
	if code, out := runHook(t, hooksDir, "http://127.0.0.1:9", "git@github.com:org/repo.git", "git@github.com:org/repo.git"); code != 1 || !strings.Contains(out, "refused") {
		t.Fatalf("unreachable server: exit %d, %s", code, out)
	}
}

func TestApprovalGateClientAndSensitiveTools(t *testing.T) {
	srv, _ := fakeGateServer(t, "denied")
	c := newApprovalGateClient(srv.URL+"/", "mat_test", "task-1")
	status, err := c.Ask(context.Background(), "mcp_tool_call", "MCP tool stripe_refund", map[string]any{"tool": "stripe_refund"}, time.Minute)
	if err != nil || status != "denied" {
		t.Fatalf("ask = %q, %v", status, err)
	}
	bad := newApprovalGateClient(srv.URL, "mat_wrong", "task-1")
	if _, err := bad.Ask(context.Background(), "mcp_tool_call", "x", nil, time.Minute); err == nil {
		t.Fatal("a refused open must be an error, never an approval")
	}
	re := sensitiveToolMatcher("")
	for name, want := range map[string]bool{"stripe_refund": true, "github_merge_pull_request": true, "delete_file": true, "list_issues": false, "search": false} {
		if re.MatchString(name) != want {
			t.Fatalf("%s sensitive = %v, want %v", name, !want, want)
		}
	}
	if !sensitiveToolMatcher("(").MatchString("merge") {
		t.Fatal("an invalid pattern must fall back to the default")
	}
}
