package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const cliAuthProcessTimeout = 10 * time.Minute

// CLI sign-in states reported to the server. The empty string is the third
// state — "unknown" — and is simply not reported: registration replaces the
// runtime's metadata document, so a provider we could not read drops its
// stale cli_auth record instead of asserting something we did not verify.
const (
	cliAuthStateAuthenticated   = "authenticated"
	cliAuthStateUnauthenticated = "unauthenticated"
)

// cliAuthStatusTimeout bounds one status probe. The command only reads a local
// credential store, so anything slower than this is a hung CLI, not a slow one.
const cliAuthStatusTimeout = 15 * time.Second

// cliAuthStatusTTL is how long a probed state is reused. It is the window in
// which a sign-out done directly in a terminal still shows as signed in; a
// sign-in or sign-out done THROUGH Multica refreshes the cache immediately, so
// this only ever lags out-of-band changes.
const cliAuthStatusTTL = 30 * time.Minute

type cliAuthSnapshot struct {
	state string
	at    time.Time
}

var (
	cliAuthURLPattern  = regexp.MustCompile(`https?://[^\s<>"']+`)
	cliAuthCodePattern = regexp.MustCompile(`(?i)\b[A-Z0-9]{4}(?:-[A-Z0-9]{4})+\b`)
)

func (d *Daemon) resolveRuntimeCommand(ctx context.Context, rt Runtime) (string, []string, error) {
	if customSpec, isCustom := d.customProfileLaunchForRuntime(rt.ID); isCustom {
		return customSpec.path, agent.FilterLaunchPrefix(rt.Provider, customSpec.fixedArgs, d.logger), nil
	}
	entry, ok := d.agents()[rt.Provider]
	if !ok {
		return "", nil, fmt.Errorf("no agent configured for provider %q", rt.Provider)
	}
	entry, _ = d.resolveAgentEntry(ctx, rt.Provider, entry)
	if entry.Path == "" {
		return "", nil, fmt.Errorf("no executable found for provider %q", rt.Provider)
	}
	return entry.Path, nil, nil
}

// cliAuthStatusCommand returns the provider's NON-INTERACTIVE sign-in status
// command, from the shared table in pkg/agent (which the API reads too, so the
// two sides can never disagree about what a provider supports). A provider
// with no such command returns false and stays unknown.
//
// The commands there all document the same contract — exit 0 when signed in,
// non-zero when not — so the exit code alone answers the question and the
// output (which carries the account identity) is discarded. We deliberately do
// NOT fall back to looking for a credentials file: Claude Code stores its
// tokens in the macOS Keychain and in ~/.claude/.credentials.json on Linux, so
// "no file" would report a signed-in Mac as signed out.
//
// The exit code is only trusted because the runtime is already version-gated
// (pkg/agent MinVersions): an older CLI that does not know the subcommand would
// also exit non-zero, and would be read as signed out.
func cliAuthStatusCommand(provider string) ([]string, bool) {
	return agent.CLIAuthStatus(provider)
}

// runCliAuthStatus executes one status probe and maps its exit code to a state.
// Returns "" when the command could not be run at all (missing binary, timeout,
// permission) — an unreadable store is not a signed-out one.
func runCliAuthStatus(ctx context.Context, execPath string, args []string) string {
	probeCtx, cancel := context.WithTimeout(ctx, cliAuthStatusTimeout)
	defer cancel()
	// Output is discarded on purpose: `claude auth status` prints the signed-in
	// account, which must not reach the daemon log or the server.
	cmd := exec.CommandContext(probeCtx, execPath, args...)
	err := cmd.Run()
	if err == nil {
		return cliAuthStateAuthenticated
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && probeCtx.Err() == nil {
		return cliAuthStateUnauthenticated
	}
	return ""
}

// cliAuthStateForProvider is the cached sign-in state reported with each
// registration, refreshed at most once per cliAuthStatusTTL so the register
// path does not fork a CLI on every convergence tick.
//
// execPath must be the binary the caller's version probe just accepted. The
// provider is deliberately NOT resolved again here: a second resolution can
// land on a different binary than the one that was version-gated, and it would
// also reach a CLI the caller never intended to run.
func (d *Daemon) cliAuthStateForProvider(ctx context.Context, provider, execPath string) string {
	args, ok := cliAuthStatusCommand(provider)
	if !ok || execPath == "" {
		return ""
	}
	d.cliAuthStatusMu.Lock()
	snapshot, cached := d.cliAuthStatus[provider]
	d.cliAuthStatusMu.Unlock()
	if cached && time.Since(snapshot.at) < cliAuthStatusTTL {
		return snapshot.state
	}
	state := probeCliAuthStatus(ctx, execPath, args)
	d.setCliAuthState(provider, state)
	return state
}

// setCliAuthState publishes a freshly-observed state. Called after a sign-in or
// sign-out this daemon performed, so the next registration reports the truth
// instead of a stale cached probe.
func (d *Daemon) setCliAuthState(provider, state string) {
	d.cliAuthStatusMu.Lock()
	defer d.cliAuthStatusMu.Unlock()
	if d.cliAuthStatus == nil {
		d.cliAuthStatus = map[string]cliAuthSnapshot{}
	}
	d.cliAuthStatus[provider] = cliAuthSnapshot{state: state, at: time.Now()}
}

// cliAuthCommand returns the arguments for one sign-in action, from the same
// shared table. An action a provider does not document is an error rather than
// an empty argument list: running the agent CLI with no arguments would start
// the agent itself.
func cliAuthCommand(provider, action string) ([]string, error) {
	args, ok := agent.CLIAuthAction(provider, action)
	if !ok {
		return nil, fmt.Errorf("CLI %s is not supported for provider %q", action, provider)
	}
	return args, nil
}

type cliAuthOutputWriter struct {
	mu       sync.Mutex
	buffer   string
	url      string
	code     string
	onUpdate func(string, string)
}

func (w *cliAuthOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buffer += string(p)
	if len(w.buffer) > 64<<10 {
		w.buffer = w.buffer[len(w.buffer)-(64<<10):]
	}
	oldURL, oldCode := w.url, w.code
	if matches := cliAuthURLPattern.FindAllString(w.buffer, -1); len(matches) > 0 {
		w.url = strings.TrimRight(matches[len(matches)-1], ".,;:)]}")
	}
	if codes := cliAuthCodePattern.FindAllString(w.buffer, -1); len(codes) > 0 {
		w.code = strings.ToUpper(codes[len(codes)-1])
	}
	url, code := w.url, w.code
	changed := (url != oldURL || code != oldCode) && (url != "" || code != "")
	w.mu.Unlock()
	if changed && w.onUpdate != nil {
		w.onUpdate(url, code)
	}
	return len(p), nil
}

func (d *Daemon) handleCliAuth(ctx context.Context, rt Runtime, pending PendingCliAuth) {
	d.logger.Info("CLI authentication requested", "runtime_id", rt.ID, "request_id", pending.ID, "provider", rt.Provider, "action", pending.Action)

	execPath, fixedArgs, err := d.resolveRuntimeCommand(ctx, rt)
	if err != nil {
		d.reportCliAuthFailure(ctx, rt, pending.ID, err)
		return
	}
	actionArgs, err := cliAuthCommand(rt.Provider, pending.Action)
	if err != nil {
		d.reportCliAuthFailure(ctx, rt, pending.ID, err)
		return
	}
	statusArgs, hasStatus := cliAuthStatusCommand(rt.Provider)

	authCtx, cancel := context.WithTimeout(ctx, cliAuthProcessTimeout)
	defer cancel()
	writer := &cliAuthOutputWriter{onUpdate: func(verificationURL, userCode string) {
		d.reportCliAuthResult(authCtx, rt, pending.ID, map[string]any{
			"status":           "running",
			"verification_url": verificationURL,
			"user_code":        userCode,
		})
	}}
	cmd := exec.CommandContext(authCtx, execPath, append(fixedArgs, actionArgs...)...)
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := processtree.Run(authCtx, cmd, time.Second); err != nil {
		if authCtx.Err() != nil {
			err = fmt.Errorf("authentication request expired before the CLI completed it")
		}
		d.reportCliAuthFailure(context.WithoutCancel(ctx), rt, pending.ID, err)
		return
	}

	// A zero exit only means the CLI did not error out; it is not proof the
	// account is usable. Ask the provider what it now thinks, and fall back to
	// the intent of the action when it cannot say.
	//
	// A provider with no documented status command is never probed: running
	// its executable with no arguments would start the agent CLI, not read a
	// credential store.
	authenticated := pending.Action == "login"
	state := ""
	if hasStatus {
		state = probeCliAuthStatus(context.WithoutCancel(ctx), execPath, statusArgs)
	}
	if state != "" {
		authenticated = state == cliAuthStateAuthenticated
	}
	d.setCliAuthState(rt.Provider, state)
	d.reportCliAuthResult(context.WithoutCancel(ctx), rt, pending.ID, map[string]any{
		"status":        "completed",
		"authenticated": authenticated,
	})
}

func (d *Daemon) reportCliAuthFailure(ctx context.Context, rt Runtime, requestID string, err error) {
	message := "CLI authentication failed"
	if err != nil {
		message = redact.Text(err.Error())
	}
	d.reportCliAuthResult(ctx, rt, requestID, map[string]any{"status": "failed", "error": message})
}
