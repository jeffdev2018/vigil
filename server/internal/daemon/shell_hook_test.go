package daemon

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

func hookTask(profile *permissionprofile.Profile) Task {
	return Task{ID: "task-1", Agent: &AgentData{Name: "bot", PermissionProfile: profile}}
}

// The gate is registered only when there is something to gate. Everything else
// behaves exactly as it did before it existed, which is what makes this safe
// to turn on for every claude run rather than behind a flag.
func TestShellHookForRun(t *testing.T) {
	original := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = original })
	resolveSelfExecutable = func() (string, error) { return "/opt/multica/bin/multica", nil }

	narrow := &permissionprofile.Profile{Name: "narrow", AllowedCommands: []string{"git status"}}

	if got := shellHookForRun(hookTask(narrow), "codex", nil); got != nil {
		t.Errorf("codex has no hook Multica knows how to install: %+v", got)
	}
	if got := shellHookForRun(hookTask(nil), "claude", nil); got != nil {
		t.Errorf("no profile, nothing to gate: %+v", got)
	}
	if got := shellHookForRun(Task{ID: "t"}, "claude", nil); got != nil {
		t.Errorf("no agent, nothing to gate: %+v", got)
	}
	open := &permissionprofile.Profile{Name: "code", AllowedCommands: []string{"*"}}
	if got := shellHookForRun(hookTask(open), "claude", nil); got != nil {
		t.Errorf("an open allowlist gates nothing and must not pay for a hook: %+v", got)
	}
	got := shellHookForRun(hookTask(narrow), "claude", nil)
	if got == nil || got.Command != "/opt/multica/bin/multica" {
		t.Fatalf("hook = %+v, want the daemon's own binary", got)
	}

	// Without a binary to call there is no gate, and the run must not pretend
	// otherwise: returning a hook with an empty command would register a
	// handler that fails and, per the contract, fails open.
	resolveSelfExecutable = func() (string, error) { return "", errors.New("no executable") }
	if got := shellHookForRun(hookTask(narrow), "claude", slog.Default()); got != nil {
		t.Errorf("unresolvable binary must register no hook: %+v", got)
	}
}

// The allowlist reaches the hook through the environment, never over the
// network: a PreToolUse hook that times out does not block the tool call, so a
// callback here would fail open on every slow moment.
func TestHookEnvironment(t *testing.T) {
	narrow := &permissionprofile.Profile{Name: "narrow", AllowedCommands: []string{"git status", "make test"}}

	if got := hookEnvironment(hookTask(narrow), ""); got != nil {
		t.Errorf("no marker path means no hook was registered: %v", got)
	}
	if got := hookEnvironment(hookTask(nil), "/tmp/marker"); got != nil {
		t.Errorf("no profile, no environment: %v", got)
	}
	env := hookEnvironment(hookTask(narrow), "/tmp/marker")
	if env["MULTICA_HOOK_ALLOWED_COMMANDS"] != "git status\nmake test" {
		t.Errorf("allowed commands = %q, want one per line", env["MULTICA_HOOK_ALLOWED_COMMANDS"])
	}
	if env["MULTICA_HOOK_PROFILE"] != "narrow" || env["MULTICA_HOOK_OBSERVED_FILE"] != "/tmp/marker" {
		t.Errorf("env = %v", env)
	}
}
