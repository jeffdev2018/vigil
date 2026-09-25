package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Lifecycle scripts (F09) turn a worktree into an executable environment: what
// to run once the checkout exists (setup), and what to run before it is
// delivered (archive).
//
// Two rules shape everything here:
//
//  1. argv, never a shell. The user's configuration is exec'd directly, so a
//     value containing `; rm -rf /` is one literal argument to a program, not a
//     second command. A user who needs a pipeline writes a script and names it.
//  2. Bounded. A hung setup script would otherwise hold a daemon slot forever;
//     the run fails after lifecycleScriptTimeout with the output it produced.
const (
	// lifecycleScriptTimeout bounds one script. Five minutes is generous for an
	// install or a build cache warm-up and short enough that a wedged script
	// costs one run rather than a slot.
	lifecycleScriptTimeout = 5 * time.Minute

	// lifecycleOutputLimit is how much of a script's output is kept for the
	// failure message. The full transcript goes to the run's logs/ file; this
	// bound is what stops a chatty build from becoming the whole task error
	// column.
	lifecycleOutputLimit = 8 << 10
)

// runLifecycleScript executes one argv in workDir and writes its combined
// output to <envRoot>/logs/lifecycle-<name>.log.
//
// Returns nil when there is nothing configured, so callers do not have to
// branch first.
func runLifecycleScript(ctx context.Context, name string, argv []string, workDir, envRoot string, env map[string]string, logger *slog.Logger) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, lifecycleScriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), lifecycleEnviron(env)...)

	started := time.Now()
	out, err := cmd.CombinedOutput()
	writeLifecycleLog(envRoot, name, argv, out, err, logger)

	if err == nil {
		if logger != nil {
			logger.Info("local_directory: lifecycle script finished",
				"script", name, "command", argv[0], "duration_ms", time.Since(started).Milliseconds())
		}
		return nil
	}
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("the %s script (%s) did not finish within %s%s",
			name, argv[0], lifecycleScriptTimeout, formatLifecycleOutput(out))
	}
	return fmt.Errorf("the %s script (%s) failed: %v%s", name, argv[0], err, formatLifecycleOutput(out))
}

func lifecycleEnviron(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}

// formatLifecycleOutput renders the tail of a failed script's output for the
// error message. The tail rather than the head: a build prints its progress
// first and its reason last.
func formatLifecycleOutput(out []byte) string {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > lifecycleOutputLimit {
		// A byte-offset cut through the tail can land mid-rune; ToValidUTF8
		// drops that leftover partial sequence instead of emitting it mangled.
		trimmed = "…" + strings.ToValidUTF8(trimmed[len(trimmed)-lifecycleOutputLimit:], "")
	}
	return "\n\n" + trimmed
}

// writeLifecycleLog keeps the full transcript next to the run, where logs/ is
// already preserved by the partial env cleanup. Best-effort: losing the log
// must never turn a successful script into a failed run.
func writeLifecycleLog(envRoot, name string, argv []string, out []byte, runErr error, logger *slog.Logger) {
	if envRoot == "" {
		return
	}
	dir := filepath.Join(envRoot, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if logger != nil {
			logger.Debug("local_directory: could not create the lifecycle log directory", "error", err)
		}
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n# %s\n", name, strings.Join(argv, " "))
	if runErr != nil {
		fmt.Fprintf(&b, "# exit: %v\n", runErr)
	}
	b.WriteString("\n")
	b.Write(out)
	if err := os.WriteFile(filepath.Join(dir, "lifecycle-"+name+".log"), []byte(b.String()), 0o644); err != nil && logger != nil {
		logger.Debug("local_directory: could not write the lifecycle log", "script", name, "error", err)
	}
}
