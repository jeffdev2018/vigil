package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Reverting a conversation to one of its turns.
//
// Every worktree turn leaves a record behind (see writeBranchRecord) pinned by
// refs/multica/turn/<taskKey>. That record is a complete description of where
// the branch stood at the end of that turn: its tree is the user's directory as
// the branch then carried it, and its second parent is the commit the turn
// delivered. Restoring a turn is therefore two ref writes and nothing else.
//
// What this deliberately does NOT do: touch the working copy, the index or the
// stash of the user's repository. `update-ref` moves the branch pointer even
// when that branch is checked out somewhere — which is the point. The files on
// the user's disk are theirs, and a revert that rewrote them would be a data
// loss path with no undo. If the branch is checked out, their `git status`
// simply shows the difference afterwards, and they decide what to do with it.

// RevertParams describes one revert of a conversation branch to an earlier
// turn.
type RevertParams struct {
	// LocalPath is the user's configured directory; its repository owns the
	// branch.
	LocalPath string
	// Branch is the conversation branch to move.
	Branch string
	// Checkpoint is the target turn's record commit — the value the server
	// stored as that run's checkpoint_sha.
	Checkpoint string
	// LaterTurnTaskIDs are the runs whose turn records must disappear with the
	// work they delivered. The server owns this list: it is the same set of
	// tasks it deletes from the queue.
	LaterTurnTaskIDs []string
}

// ErrRevertRefused marks the outcomes a user can act on — a branch that moved,
// a checkpoint that is gone — as opposed to an infrastructure failure. The
// message always names the cause; callers surface it verbatim.
var ErrRevertRefused = errors.New("revert refused")

// RevertToTurn moves Branch back to the commit Checkpoint recorded, restores
// that turn's snapshot as the branch's state, and drops the later turn records.
func RevertToTurn(params RevertParams, logger *slog.Logger) error {
	if params.LocalPath == "" || params.Branch == "" || params.Checkpoint == "" {
		return fmt.Errorf("execenv: revert needs a local path, a branch and a checkpoint")
	}

	gitRoot, err := resolveGitRoot(params.LocalPath)
	if err != nil {
		return err
	}

	unlock, err := lockGitRoot(gitRoot, logger)
	if err != nil {
		return fmt.Errorf("execenv: could not lock %q to revert branch %s: %w", gitRoot, params.Branch, err)
	}
	defer unlock()

	branchRef := "refs/heads/" + params.Branch
	tip, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", branchRef)
	if err != nil || tip == "" {
		return fmt.Errorf("%w: branch %s no longer exists in %s",
			ErrRevertRefused, params.Branch, gitRoot)
	}

	// The checkpoint is a commit in the user's repository, and the user may
	// have gc'd or rewritten it away since the run.
	target, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", params.Checkpoint+"^2")
	if err != nil || target == "" {
		return fmt.Errorf("%w: the checkpoint recorded for that run is no longer in %s "+
			"(record %s); nothing was changed",
			ErrRevertRefused, gitRoot, shortID(params.Checkpoint))
	}

	// The proof that reverting only undoes Multica's own turns: if the branch
	// no longer contains the commit that turn delivered, someone rebased, reset
	// or force-moved it since, and resetting to the old commit would silently
	// throw that away.
	if _, err := runGit(gitRoot, "merge-base", "--is-ancestor", target, tip); err != nil {
		return fmt.Errorf("%w: branch %s has moved off that run's checkpoint — it no longer contains commit %s. "+
			"Something rebased, reset or force-moved the branch after the run, and reverting would discard that work. "+
			"Nothing was changed; reset the branch yourself if that is what you want",
			ErrRevertRefused, params.Branch, shortID(target))
	}

	// Compare-and-swap on the old value: a run that finalized between the read
	// above and this write moved the branch, and overwriting it blind would eat
	// the commit it just delivered.
	if out, err := runGit(gitRoot, "update-ref", branchRef, target, tip); err != nil {
		return fmt.Errorf("%w: branch %s moved while it was being reverted (%s); nothing was changed",
			ErrRevertRefused, params.Branch, strings.TrimSpace(out))
	}

	// Restore the turn's snapshot as the branch's current state, so the next
	// turn replays the user's edits from where that turn left them rather than
	// from a turn that no longer exists.
	if out, err := runGit(gitRoot, "update-ref", userStateRef(params.Branch), params.Checkpoint); err != nil {
		return fmt.Errorf("branch %s was reverted to %s but its recorded state could not be restored: %s: %w",
			params.Branch, shortID(target), strings.TrimSpace(out), err)
	}

	// Best-effort: the turns are gone from the branch either way, and the
	// orphan prune on the next task catches whatever this misses.
	for _, taskID := range params.LaterTurnTaskIDs {
		if taskID == "" {
			continue
		}
		ref := TurnRefForTask(taskID)
		if out, err := runGit(gitRoot, "update-ref", "-d", ref); err != nil && logger != nil {
			logger.Debug("execenv: no turn checkpoint to drop for a reverted run",
				"ref", ref, "output", strings.TrimSpace(out))
		}
	}

	if logger != nil {
		logger.Info("execenv: reverted a conversation branch to an earlier turn",
			"git_root", gitRoot, "branch", params.Branch,
			"from", tip, "to", target, "dropped_turns", len(params.LaterTurnTaskIDs))
	}
	return nil
}
