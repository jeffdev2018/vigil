package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The diff a racing attempt delivered (F11).
//
// Racing puts several attempts on one issue side by side, and the compare view
// is only useful if each column can show what its attempt actually changed. The
// daemon is the only party that ever sees the repository, so it measures the
// diff at finalization — after Finalize has committed whatever the agent left
// uncommitted, so the branch tip is the complete deliverable — and sends it
// with the completion callback.

const (
	// maxRunDiffBytes bounds the unified patch a run will send. Past it the
	// stat travels alone and the UI marks the attempt truncated: a patch this
	// large is unreadable in a side-by-side column anyway, and the row it would
	// land in is read on every load of the compare view.
	maxRunDiffBytes = 256 << 10 // 256 KiB

	// runDiffTimeout bounds each git invocation. These are local reads of an
	// already-finalized branch; a slow one means a wedged repository, and the
	// diff is a nice-to-have that must never hold a daemon slot open.
	runDiffTimeout = 30 * time.Second
)

// runDiff is what computeRunDiff produced. Unified is empty when the patch
// exceeded the bound — the stat is still valid then.
type runDiff struct {
	Stat    protocol.TaskDiffStat
	Unified string
}

// computeRunDiff measures base..tip in gitRoot.
//
// base is the commit the task's worktree started from and tip the branch
// Finalize delivered, so the result is this attempt's own contribution — not
// whatever the branch carried from earlier turns.
//
// Best-effort by contract: no git, no repository, a bad revision or a failing
// command all return nil after a warning. The attempt is finished either way,
// and the compare view showing "—" for one column beats failing a run that
// produced work.
func computeRunDiff(ctx context.Context, gitRoot, base, tip string, maxBytes int, logger *slog.Logger) *runDiff {
	if gitRoot == "" || base == "" || tip == "" {
		return nil
	}
	rng := base + ".." + tip

	numstat, err := runDiffGit(ctx, gitRoot, maxRunDiffBytes, "diff", "--numstat", rng)
	if err != nil {
		if logger != nil {
			logger.Warn("run diff: could not measure the attempt's changes (the run is unaffected)",
				"git_root", gitRoot, "range", rng, "error", err)
		}
		return nil
	}
	out := &runDiff{Stat: parseNumstat(numstat)}

	// The stat is what the compare view needs; the patch is a bonus. A failure
	// here therefore keeps the stat and drops the patch, exactly like the
	// over-the-bound case.
	unified, err := runDiffGit(ctx, gitRoot, maxBytes, "diff", rng)
	if err != nil {
		if logger != nil {
			logger.Warn("run diff: measured the attempt but could not read its patch",
				"git_root", gitRoot, "range", rng, "error", err)
		}
		return out
	}
	out.Unified = unified
	return out
}

// errRunDiffTooLarge marks output that exceeded the caller's bound.
var errRunDiffTooLarge = errors.New("output exceeds the diff bound")

// runDiffGit runs one git command and returns at most maxBytes of stdout,
// reporting errRunDiffTooLarge rather than buffering an unbounded patch.
func runDiffGit(ctx context.Context, gitRoot string, maxBytes int, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runDiffTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", gitRoot}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	// One byte past the bound is what distinguishes "exactly at the bound" from
	// "truncated".
	buf, readErr := io.ReadAll(io.LimitReader(stdout, int64(maxBytes)+1))
	if readErr == nil && len(buf) > maxBytes {
		// Stop git rather than drain a patch we have already refused.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", errRunDiffTooLarge
	}
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return "", err
	}
	if readErr != nil {
		return "", readErr
	}
	return string(buf), nil
}

// parseNumstat sums `git diff --numstat` output. A binary file reports "-" for
// both counts: it is one changed file with no line counts.
func parseNumstat(out string) protocol.TaskDiffStat {
	var stat protocol.TaskDiffStat
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) < 3 {
			continue
		}
		stat.Files++
		if n, err := strconv.Atoi(fields[0]); err == nil && n > 0 {
			stat.Insertions += n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
			stat.Deletions += n
		}
	}
	return stat
}
