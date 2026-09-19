package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	t.Setenv("GIT_CONFIG_COUNT", "") // no inherited env-scoped git config
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

// gitGateEnv is a hermetic environment for a real git run under the approval
// gate: no global or system config (a developer's own core.hooksPath would
// otherwise leak in), no inherited GIT_CONFIG_* entries, plus the gate's.
func gitGateEnv(t *testing.T, gate map[string]string, extra ...string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "MULTICA_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=agent", "GIT_AUTHOR_EMAIL=agent@example.com", "GIT_COMMITTER_NAME=agent", "GIT_COMMITTER_EMAIL=agent@example.com")
	for k, v := range gate {
		env = append(env, k+"="+v)
	}
	return append(env, extra...)
}

func gitRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A file name is agent-controlled. Built by string interpolation, a name
// carrying quotes rewrote the reviewer-facing JSON (extra keys, spoofed
// paths). Every value must reach the server as data, and names with spaces
// must stay one path each.
func TestPrePushHookSendsFileNamesAsJSONData(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	var got atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/task-1/gates", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got.Store(body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-1", "status": "pending"})
	})
	mux.HandleFunc("/api/tasks/task-1/gates/gate-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-1", "status": "approved"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	root := t.TempDir()
	hooksDir, err := ensureGateHooksDir(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	base := gitGateEnv(t, nil)
	gitRun(t, root, base, "init", "-q", "--bare", remote)
	gitRun(t, root, base, "init", "-q", "-b", "main", work)
	gitRun(t, work, base, "remote", "add", "origin", remote)

	names := []string{
		`x","evil":"1`,
		`a b.txt`,
		`back\slash`,
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(work, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, work, base, "add", "-A")
	gitRun(t, work, base, "commit", "-q", "-m", "files")

	t.Setenv("GIT_CONFIG_COUNT", "") // gitGateEnv drops inherited GIT_CONFIG_* entries
	env := gitGateEnv(t, gateEnvironment(hooksDir, remote, time.Minute),
		"MULTICA_TASK_ID=task-1", "MULTICA_TOKEN=mat_test", "MULTICA_SERVER_URL="+srv.URL, "MULTICA_WORKSPACE_ID=ws-1")
	gitRun(t, work, env, "push", "-q", "origin", "main")

	body, _ := got.Load().(map[string]any)
	if body == nil {
		t.Fatal("the gate was never opened")
	}
	details, _ := body["details"].(map[string]any)
	if len(details) != 4 {
		t.Fatalf("details = %v, want exactly remote/url/refs/paths", details)
	}
	var paths []string
	for _, p := range details["paths"].([]any) {
		paths = append(paths, p.(string))
	}
	sort.Strings(paths)
	want := append([]string(nil), names...)
	sort.Strings(want)
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %q, want %q", paths, want)
	}
	if details["remote"] != "origin" || details["url"] != remote || details["refs"] != "refs/heads/main" {
		t.Fatalf("details = %v", details)
	}

	// An update of an existing branch lists the diff against the remote tip.
	second := `new "quoted" name`
	if err := os.WriteFile(filepath.Join(work, second), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, base, "add", "-A")
	gitRun(t, work, base, "commit", "-q", "-m", "more")
	gitRun(t, work, env, "push", "-q", "origin", "main")
	body, _ = got.Load().(map[string]any)
	details, _ = body["details"].(map[string]any)
	if ps, _ := details["paths"].([]any); len(ps) != 1 || ps[0] != second {
		t.Fatalf("update paths = %v, want [%q]", details["paths"], second)
	}
}

// Selecting the gate's hook directory through core.hooksPath must not silence
// the repository's own hooks: the Co-authored-by prepare-commit-msg hook the
// repo cache installs, a commit-msg hook, and an existing pre-push that
// refuses all keep working, whether they live in .git/hooks or in the repo's
// configured core.hooksPath.
func TestGateHooksRelayToTheRepositoryHooks(t *testing.T) {
	root := t.TempDir()
	hooksDir, err := ensureGateHooksDir(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "pre-push", "post-checkout", "post-merge", "pre-rebase", "post-rewrite"} {
		if info, err := os.Stat(filepath.Join(hooksDir, name)); err != nil || info.Mode()&0o111 == 0 {
			t.Fatalf("gate hooks dir must provide an executable %s relay: %v", name, err)
		}
	}
	trailerHook := "#!/bin/sh\ngit interpret-trailers --in-place --trailer 'Co-authored-by: multica-agent <github@multica.ai>' \"$1\"\n"
	commitMsgHook := "#!/bin/sh\necho commit-msg ran >> \"$(git rev-parse --git-dir)/hook.log\"\n"
	refusePush := "#!/bin/sh\ncat >/dev/null\necho repo pre-push refused >&2\nexit 1\n"

	for _, layout := range []string{"dot-git-hooks", "configured-hooks-path"} {
		t.Run(layout, func(t *testing.T) {
			dir := filepath.Join(root, layout)
			remote := dir + "-remote.git"
			base := gitGateEnv(t, nil)
			gitRun(t, root, base, "init", "-q", "-b", "main", dir)
			gitRun(t, root, base, "init", "-q", "--bare", remote)
			gitRun(t, dir, base, "remote", "add", "origin", remote)
			repoHooks := filepath.Join(dir, ".git", "hooks")
			if layout == "configured-hooks-path" {
				repoHooks = filepath.Join(dir, ".githooks")
				gitRun(t, dir, base, "config", "core.hooksPath", ".githooks")
			}
			writeExecutable(t, filepath.Join(repoHooks, "prepare-commit-msg"), trailerHook)
			writeExecutable(t, filepath.Join(repoHooks, "commit-msg"), commitMsgHook)
			writeExecutable(t, filepath.Join(repoHooks, "pre-push"), refusePush)

			// An inherited env-scoped config entry survives next to the gate's.
			env := gitGateEnv(t, nil, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.useConfigOnly", "GIT_CONFIG_VALUE_0=false")
			t.Setenv("GIT_CONFIG_COUNT", "1")
			for k, v := range gateEnvironment(hooksDir, "", time.Minute) {
				env = append(env, k+"="+v)
			}
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, dir, env, "add", "f.txt")
			gitRun(t, dir, env, "commit", "-q", "-m", "change")
			msg := gitRun(t, dir, env, "log", "-1", "--format=%B")
			if !strings.Contains(msg, "Co-authored-by: multica-agent <github@multica.ai>") {
				t.Fatalf("prepare-commit-msg did not run under the gate; message:\n%s", msg)
			}
			if log, _ := os.ReadFile(filepath.Join(dir, ".git", "hook.log")); !strings.Contains(string(log), "commit-msg ran") {
				t.Fatalf("commit-msg did not run under the gate")
			}
			if hooksPath := strings.TrimSpace(gitRun(t, dir, env, "config", "core.hooksPath")); hooksPath != hooksDir {
				t.Fatalf("core.hooksPath = %q, want the gate dir %q", hooksPath, hooksDir)
			}
			cmd := exec.Command("git", "push", "-q", "origin", "main")
			cmd.Dir = dir
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "repo pre-push refused") {
				t.Fatalf("the repository's pre-push must still refuse: err=%v\n%s", err, out)
			}
		})
	}
}

// An env-scoped git config the daemon inherited (auth headers, URL rewrites)
// must survive: the gate appends its entry instead of claiming index 0.
func TestGateEnvironmentAppendsToInheritedGitConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "2")
	env := gateEnvironment("/hooks", "", time.Minute)
	if env["GIT_CONFIG_COUNT"] != "3" || env["GIT_CONFIG_KEY_2"] != "core.hooksPath" || env["GIT_CONFIG_VALUE_2"] != "/hooks" {
		t.Fatalf("env = %v, want the gate entry appended at index 2", env)
	}
	if _, clobbered := env["GIT_CONFIG_KEY_0"]; clobbered {
		t.Fatalf("env = %v overwrites an inherited entry", env)
	}
}
