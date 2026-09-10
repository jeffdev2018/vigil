package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Promote / discard for the branch a terminal run delivered (JEF-255).
//
// The branch lives in the user's own repository, so the daemon is the only
// party that can act on it. Promote pushes the branch to origin and nothing
// else — the pull request is the server's half, opened through the workspace's
// VCS connection once the push is reported. Discard removes any worktree still
// registered for the branch, then deletes the branch and its recorded
// local-directory snapshot.
//
// Both are guarded, never forceful: neither action may touch the repository's
// default branch, promote never pushes anywhere but origin, and discard
// refuses a branch the user has checked out in their main working tree.

// ErrBranchActionRefused marks the outcomes a user can act on — no origin
// remote, a branch checked out in the main tree — as opposed to an
// infrastructure failure. The message always names the cause; callers surface
// it verbatim.
var ErrBranchActionRefused = errors.New("branch action refused")

// BranchActionParams describes one promote/discard against a run's branch.
type BranchActionParams struct {
	// LocalPath is the user's configured directory; its repository owns the
	// branch.
	LocalPath string
	// Branch is the branch the run delivered.
	Branch string
}

// BranchActionOutcome is what the action established. HeadSHA is the branch
// tip the action saw (the pushed commit for promote, the deleted tip for
// discard). RemoteURL is the origin pushed to, on promote — the server parses
// it to find the VCS connection that opens the PR. DefaultBranch is the
// repository's default branch as resolved here, for the same PR step.
type BranchActionOutcome struct {
	HeadSHA       string
	RemoteURL     string
	DefaultBranch string
}

// PromoteBranch pushes Branch to origin with upstream tracking. It never
// touches the default branch and never pushes to any other remote.
func PromoteBranch(params BranchActionParams, logger *slog.Logger) (BranchActionOutcome, error) {
	gitRoot, branch, unlock, err := prepareBranchAction(params, logger)
	if err != nil {
		return BranchActionOutcome{}, err
	}
	defer unlock()

	tip, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil || tip == "" {
		return BranchActionOutcome{}, fmt.Errorf("%w: branch %s no longer exists in %s",
			ErrBranchActionRefused, branch, gitRoot)
	}

	def := defaultBranchOf(gitRoot)
	if branch == def {
		return BranchActionOutcome{}, fmt.Errorf("%w: %s is the repository's default branch and is never promoted",
			ErrBranchActionRefused, branch)
	}

	// origin only, and it must exist: "promote" means "share this branch with
	// the team's remote", and a repository without one has nowhere to share to.
	// Pushing anywhere else would surprise the user; silently succeeding without
	// a push would be a lie.
	remoteURL, err := runGitTrimmed(gitRoot, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(remoteURL) == "" {
		return BranchActionOutcome{}, fmt.Errorf("no_remote: %s has no origin remote; add one and promote again", gitRoot)
	}

	if out, err := runGit(gitRoot, "push", "-u", "origin", branch); err != nil {
		return BranchActionOutcome{}, fmt.Errorf("git push origin %s failed: %s: %w",
			branch, strings.TrimSpace(out), err)
	}

	if logger != nil {
		logger.Info("execenv: promoted a run branch to origin",
			"git_root", gitRoot, "branch", branch, "head", shortID(tip), "remote", remoteURL)
	}
	return BranchActionOutcome{HeadSHA: tip, RemoteURL: remoteURL, DefaultBranch: def}, nil
}

// DiscardBranch removes whatever the run left behind: the worktree still
// registered for the branch, if any, then the branch itself, then the branch's
// recorded local-directory snapshot. A branch that is already gone is a
// completed discard, not an error — the user's own cleanup gets there first
// sometimes.
func DiscardBranch(params BranchActionParams, logger *slog.Logger) (BranchActionOutcome, error) {
	gitRoot, branch, unlock, err := prepareBranchAction(params, logger)
	if err != nil {
		return BranchActionOutcome{}, err
	}
	defer unlock()

	def := defaultBranchOf(gitRoot)
	if branch == def {
		return BranchActionOutcome{}, fmt.Errorf("%w: %s is the repository's default branch and is never discarded",
			ErrBranchActionRefused, branch)
	}

	// A worktree still holding the branch is normally a leftover task worktree
	// whose Finalize could not finish removing it. The main working tree is the
	// exception that refuses: discarding there would move the user's own
	// checkout out from under them.
	checkedOut, mainTree, err := worktreesHoldingBranch(gitRoot, branch)
	if err != nil {
		return BranchActionOutcome{}, err
	}
	if mainTree != "" {
		return BranchActionOutcome{}, fmt.Errorf("%w: branch %s is checked out in your working tree at %s; switch it elsewhere first",
			ErrBranchActionRefused, branch, mainTree)
	}
	for _, path := range checkedOut {
		if err := removeLocalWorktreeDir(gitRoot, path, logger); err != nil {
			return BranchActionOutcome{}, fmt.Errorf("could not remove the worktree still holding branch %s: %w", branch, err)
		}
	}

	// The tip read happens before the delete so the report can name what was
	// discarded. Already-gone is success: the end state the user asked for
	// (no branch, no worktree) already holds.
	tip, _ := runGitTrimmed(gitRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if tip != "" {
		if out, err := runGit(gitRoot, "branch", "-D", branch); err != nil {
			return BranchActionOutcome{}, fmt.Errorf("git branch -D %s failed: %s: %w",
				branch, strings.TrimSpace(out), err)
		}
	}
	// The branch's recorded snapshot is meaningless without it; dropBranch's
	// second half, done explicitly because the branch half above is NOT
	// best-effort here.
	if out, err := runGit(gitRoot, "update-ref", "-d", userStateRef(branch)); err != nil && logger != nil {
		logger.Debug("execenv: no local-directory snapshot to drop for discarded branch",
			"branch", branch, "output", strings.TrimSpace(out))
	}

	if logger != nil {
		logger.Info("execenv: discarded a run branch",
			"git_root", gitRoot, "branch", branch, "head", shortID(tip),
			"worktrees_removed", len(checkedOut), "branch_existed", tip != "")
	}
	return BranchActionOutcome{HeadSHA: tip, DefaultBranch: def}, nil
}

// prepareBranchAction resolves and locks the repository both actions work on.
func prepareBranchAction(params BranchActionParams, logger *slog.Logger) (gitRoot, branch string, unlock func(), err error) {
	branch = strings.TrimSpace(params.Branch)
	if params.LocalPath == "" || branch == "" {
		return "", "", nil, fmt.Errorf("execenv: a branch action needs a local path and a branch")
	}
	gitRoot, err = resolveGitRoot(params.LocalPath)
	if err != nil {
		return "", "", nil, err
	}
	unlock, err = lockGitRoot(gitRoot, logger)
	if err != nil {
		return "", "", nil, fmt.Errorf("execenv: could not lock %q for %s: %w", gitRoot, branch, err)
	}
	return gitRoot, branch, unlock, nil
}

// defaultBranchOf names the repository's default branch: origin's HEAD when
// the clone has one, else whatever the main working tree has checked out. ""
// when neither answers (a detached main tree with no origin) — the guards then
// simply never match, which is the safe reading for a branch named
// agent/<agent>/<issue>.
func defaultBranchOf(gitRoot string) string {
	if ref, err := runGitTrimmed(gitRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimPrefix(ref, "origin/"); name != "" && name != ref {
			return name
		}
	}
	head, err := runGitTrimmed(gitRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return head
}

// worktreesHoldingBranch lists the paths of every linked worktree that has
// branch checked out. The main working tree's path is returned separately: a
// discard refuses that one outright rather than removing it.
func worktreesHoldingBranch(gitRoot, branch string) (linked []string, mainTree string, err error) {
	out, err := runGitTrimmed(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, "", fmt.Errorf("could not list worktrees in %s: %w", gitRoot, err)
	}
	wantRef := "refs/heads/" + branch
	var path, branchRef string
	first := true
	flush := func() {
		if branchRef == wantRef && path != "" {
			// `git worktree list --porcelain` prints the main working tree
			// first. A discard refuses that one outright rather than removing
			// the user's own checkout.
			if first {
				mainTree = path
			} else {
				linked = append(linked, path)
			}
		}
		first = false
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			flush()
			path, branchRef = "", ""
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			branchRef = strings.TrimPrefix(line, "branch ")
		}
	}
	flush()
	return linked, mainTree, nil
}
