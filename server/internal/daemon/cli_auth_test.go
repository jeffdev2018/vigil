package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCliAuthCommandCoversEveryDocumentedProvider(t *testing.T) {
	tests := map[string][]string{
		"codex:login":         {"login", "--device-auth"},
		"codex:logout":        {"logout"},
		"claude:login":        {"auth", "login"},
		"claude:logout":       {"auth", "logout"},
		"cursor-agent:login":  {"login"},
		"cursor-agent:logout": {"logout"},
		"copilot:login":       {"login"},
	}
	for key, want := range tests {
		provider, action, _ := strings.Cut(key, ":")
		got, err := cliAuthCommand(provider, action)
		if err != nil || strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("cliAuthCommand(%s) = %v, %v", key, got, err)
		}
	}
	// An action a provider does not document must be an error, never an empty
	// argument list: running the agent CLI with no arguments starts the agent.
	for _, key := range []string{"copilot:logout", "opencode:login", "gemini:login", "other:login"} {
		provider, action, _ := strings.Cut(key, ":")
		if got, err := cliAuthCommand(provider, action); err == nil {
			t.Fatalf("cliAuthCommand(%s) = %v, want an error", key, got)
		}
	}
}

func TestHandleCliAuthReportsDeviceCodeAndNeverReportsOrLogsSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses a POSIX script")
	}
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	bin := filepath.Join(t.TempDir(), "fake-codex")
	script := "#!/bin/sh\nprintf '%s\\n' 'Open https://auth.openai.com/codex/device' 'Code: ABCD-EFGH' '" + secret + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reports []map[string]any
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode report: %v", err)
		}
		mu.Lock()
		reports = append(reports, report)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	var logs bytes.Buffer
	d.logger = slog.New(slog.NewTextHandler(&logs, nil))
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: bin}}

	d.handleCliAuth(context.Background(), Runtime{ID: "rt-1", Provider: "codex"}, PendingCliAuth{ID: "req-1", Action: "login"})

	mu.Lock()
	defer mu.Unlock()
	if len(reports) < 2 {
		t.Fatalf("reports = %#v, want progress and completion", reports)
	}
	if reports[0]["verification_url"] != "https://auth.openai.com/codex/device" || reports[0]["user_code"] != "ABCD-EFGH" {
		t.Fatalf("progress report = %#v", reports[0])
	}
	last := reports[len(reports)-1]
	if last["status"] != "completed" || last["authenticated"] != true {
		t.Fatalf("terminal report = %#v", last)
	}
	wire, _ := json.Marshal(reports)
	if strings.Contains(string(wire), secret) || strings.Contains(logs.String(), secret) {
		t.Fatal("CLI output secret crossed a report or logger boundary")
	}
}

func TestCliAuthOutputWriterHandlesSplitOutput(t *testing.T) {
	var gotURL, gotCode string
	w := &cliAuthOutputWriter{onUpdate: func(url, code string) {
		gotURL, gotCode = url, code
	}}
	_, _ = w.Write([]byte("Open https://auth.example/de"))
	_, _ = w.Write([]byte("vice\nEnter code: abcd-"))
	_, _ = w.Write([]byte("efgh\n"))
	if gotURL != "https://auth.example/device" || gotCode != "ABCD-EFGH" {
		t.Fatalf("parsed url=%q code=%q", gotURL, gotCode)
	}
}

func TestCliAuthStatusCommandSupportsClaudeAndCodex(t *testing.T) {
	claude, ok := cliAuthStatusCommand("claude")
	if !ok || strings.Join(claude, " ") != "auth status" {
		t.Fatalf("claude status command = %v, %v", claude, ok)
	}
	codex, ok := cliAuthStatusCommand("codex")
	if !ok || strings.Join(codex, " ") != "login status" {
		t.Fatalf("codex status command = %v, %v", codex, ok)
	}
	// Every other provider stays unknown, including ones we CAN sign in:
	// `cursor-agent status` prints the account but documents no exit code, so
	// trusting it would report every Cursor runtime as authenticated.
	for _, provider := range []string{"cursor-agent", "copilot", "opencode", "gemini"} {
		if _, ok := cliAuthStatusCommand(provider); ok {
			t.Fatalf("%s must have no status probe: no documented exit-code contract", provider)
		}
	}
}

// A provider with a login but no status probe must still complete, and must
// never be probed by running its executable with no arguments — that would
// start the agent CLI instead of reading a credential store.
func TestHandleCliAuthWithoutStatusProbeReportsTheAction(t *testing.T) {
	bin := fakeCLI(t, "fake-copilot", `
if [ "$#" -eq 0 ]; then
  echo "INTERACTIVE AGENT STARTED" >&2
  exit 3
fi
exit 0
`)
	var mu sync.Mutex
	var reports []map[string]any
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode report: %v", err)
		}
		mu.Lock()
		reports = append(reports, report)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	d.cfg.Agents = map[string]AgentEntry{"copilot": {Path: bin}}

	d.handleCliAuth(context.Background(), Runtime{ID: "rt-1", Provider: "copilot"}, PendingCliAuth{ID: "req-1", Action: "login"})

	mu.Lock()
	defer mu.Unlock()
	if len(reports) == 0 {
		t.Fatal("no report")
	}
	last := reports[len(reports)-1]
	if last["status"] != "completed" || last["authenticated"] != true {
		t.Fatalf("report = %v, want a completed login", last)
	}
	// The state is unknown, not asserted, so registration drops the record
	// rather than claiming something no command verified.
	if got := d.cliAuthStateForProvider(context.Background(), "copilot", bin); got != "" {
		t.Fatalf("cached state = %q, want unknown", got)
	}
}

// fakeCLI writes an executable POSIX script and returns its path. Default tests
// must never resolve or execute a user-installed agent CLI.
func fakeCLI(t *testing.T, name, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses a POSIX script")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCliAuthStatusMapsExitCodeToState(t *testing.T) {
	ctx := context.Background()
	signedIn := fakeCLI(t, "signed-in", "exit 0\n")
	if got := runCliAuthStatus(ctx, signedIn, []string{"auth", "status"}); got != cliAuthStateAuthenticated {
		t.Errorf("exit 0 = %q, want authenticated", got)
	}
	signedOut := fakeCLI(t, "signed-out", "echo 'Not logged in'\nexit 1\n")
	if got := runCliAuthStatus(ctx, signedOut, []string{"login", "status"}); got != cliAuthStateUnauthenticated {
		t.Errorf("exit 1 = %q, want unauthenticated", got)
	}
	// A CLI that cannot be run at all is unknown, never "signed out": an
	// unreadable credential store must not look like a revoked account.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if got := runCliAuthStatus(ctx, missing, []string{"auth", "status"}); got != "" {
		t.Errorf("missing executable = %q, want unknown", got)
	}
}

// TestHandleCliAuthTrustsStatusProbeOverExitCode pins the fix: a login whose
// process exits 0 without actually signing the account in must be reported as
// unauthenticated, not as a success.
func TestHandleCliAuthTrustsStatusProbeOverExitCode(t *testing.T) {
	bin := fakeCLI(t, "fake-codex", `
case "$1 $2" in
  "login status") exit 1 ;;
esac
exit 0
`)
	var mu sync.Mutex
	var reports []map[string]any
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode report: %v", err)
		}
		mu.Lock()
		reports = append(reports, report)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: bin}}

	d.handleCliAuth(context.Background(), Runtime{ID: "rt-1", Provider: "codex"}, PendingCliAuth{ID: "req-1", Action: "login"})

	mu.Lock()
	defer mu.Unlock()
	if len(reports) == 0 {
		t.Fatal("no report")
	}
	last := reports[len(reports)-1]
	if last["status"] != "completed" || last["authenticated"] != false {
		t.Fatalf("terminal report = %#v, want authenticated=false", last)
	}
	if got := d.cliAuthStatus["codex"].state; got != cliAuthStateUnauthenticated {
		t.Fatalf("cached state = %q, want unauthenticated", got)
	}
}

func TestCliAuthStateForProviderServesTheCacheWithinTTL(t *testing.T) {
	ctx := context.Background()
	d := &Daemon{cliAuthStatus: map[string]cliAuthSnapshot{}}
	// The path is a test-created fake: this never resolves an installed CLI.
	signedOut := fakeCLI(t, "fake-claude", "exit 1\n")

	d.setCliAuthState("claude", cliAuthStateAuthenticated)
	if got := d.cliAuthStateForProvider(ctx, "claude", signedOut); got != cliAuthStateAuthenticated {
		t.Fatalf("cached state = %q, want the cached authenticated", got)
	}
	d.cliAuthStatus["claude"] = cliAuthSnapshot{state: cliAuthStateAuthenticated, at: time.Now().Add(-2 * cliAuthStatusTTL)}
	if got := d.cliAuthStateForProvider(ctx, "claude", signedOut); got != cliAuthStateUnauthenticated {
		t.Fatalf("expired state = %q, want a re-probe of the CLI", got)
	}
	// No path means the caller's version probe never accepted a binary; the
	// provider must not be resolved a second time to find one.
	if got := d.cliAuthStateForProvider(ctx, "codex", ""); got != "" {
		t.Fatalf("state without an accepted path = %q, want unknown", got)
	}
	if got := d.cliAuthStateForProvider(ctx, "gemini", signedOut); got != "" {
		t.Fatalf("unsupported provider = %q, want unknown", got)
	}
}

// stubCliAuthProbe neutralizes the sign-in probe for a test that stubs the
// version probe: the path detectBuiltinRuntimes resolves is then not a binary
// the test staged, and executing it would reach an installed agent CLI.
func stubCliAuthProbe(t *testing.T) {
	t.Helper()
	orig := probeCliAuthStatus
	probeCliAuthStatus = func(context.Context, string, []string) string { return "" }
	t.Cleanup(func() { probeCliAuthStatus = orig })
}
