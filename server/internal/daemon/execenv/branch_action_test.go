package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// JEF-255: promote pushes a run's branch to origin; discard removes the
// branch, its leftover worktree and its recorded snapshot. These run against
// real repositories — the failure modes being pinned (a checked-out branch,
// a missing remote, the default branch) only exist in real ones.

// branchActionRepo builds a repo with a delivered run branch on top of main.
func branchActionRepo(t *testing.T) (dir, branch string) {
	t.Helper()
	dir = newTestRepo(t)
	branch = "agent/j/PROJ-1"
	gitRun(t, dir, "checkout", "-b", branch)
	writeFile(t, filepath.Join(dir, "delivered.txt"), "the run's work\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "delivered")
	gitRun(t, dir, "checkout", "main")
	return dir, branch
}

func TestPromoteBranchPushesToOrigin(t *testing.T) {
	dir, branch := branchActionRepo(t)
	// A bare repo as origin, the way a real remote behaves.
	remote := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(remote); err == nil {
		remote = resolved
	}
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, dir, "remote", "add", "origin", remote)

	out, err := PromoteBranch(BranchActionParams{LocalPath: dir, Branch: branch}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if out.RemoteURL != remote {
		t.Errorf("remote_url = %q, want %q", out.RemoteURL, remote)
	}
	tip := gitRun(t, dir, "rev-parse", branch)
	if out.HeadSHA != tip {
		t.Errorf("head_sha = %q, want the branch tip %q", out.HeadSHA, tip)
	}
	if got := gitRun(t, remote, "rev-parse", "refs/heads/"+branch); got != tip {
		t.Errorf("origin does not carry the branch at %q (got %q)", tip, got)
	}
	// -u: the branch's upstream is set, so a later plain `git push` follows.
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}"); got != "origin/"+branch {
		t.Errorf("upstream = %q, want origin/%s", got, branch)
	}
}

func TestPromoteBranchWithoutOriginReportsNoRemote(t *testing.T) {
	dir, branch := branchActionRepo(t)

	_, err := PromoteBranch(BranchActionParams{LocalPath: dir, Branch: branch}, worktreeTestLogger())
	if err == nil || !strings.HasPrefix(err.Error(), "no_remote") {
		t.Fatalf("err = %v, want a no_remote refusal", err)
	}
}

func TestPromoteBranchRefusesTheDefaultBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)

	_, err := PromoteBranch(BranchActionParams{LocalPath: dir, Branch: "main"}, worktreeTestLogger())
	if !errors.Is(err, ErrBranchActionRefused) {
		t.Fatalf("err = %v, want ErrBranchActionRefused", err)
	}
}

func TestPromoteBranchRefusesAGoneBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)

	_, err := PromoteBranch(BranchActionParams{LocalPath: dir, Branch: "agent/j/never-existed"}, worktreeTestLogger())
	if !errors.Is(err, ErrBranchActionRefused) {
		t.Fatalf("err = %v, want ErrBranchActionRefused", err)
	}
}

func TestDiscardBranchDeletesTheBranch(t *testing.T) {
	dir, branch := branchActionRepo(t)
	tip := gitRun(t, dir, "rev-parse", branch)

	out, err := DiscardBranch(BranchActionParams{LocalPath: dir, Branch: branch}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if out.HeadSHA != tip {
		t.Errorf("head_sha = %q, want the discarded tip %q", out.HeadSHA, tip)
	}
	if _, err := gitTry(t, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("the branch survived its discard")
	}
}

// The user's own cleanup gets there first sometimes: a branch that is already
// gone is the end state discard was asked for, not an error.
func TestDiscardBranchToleratesAMissingBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)

	if _, err := DiscardBranch(BranchActionParams{LocalPath: dir, Branch: "agent/j/already-gone"}, worktreeTestLogger()); err != nil {
		t.Fatalf("discard of a missing branch: %v", err)
	}
}

// A leftover task worktree still holding the branch is removed with it — the
// Finalize that failed to do so is exactly why discard exists.
func TestDiscardBranchRemovesTheRegisteredWorktree(t *testing.T) {
	dir, branch := branchActionRepo(t)
	wt := filepath.Join(t.TempDir(), "task-worktree")
	gitRun(t, dir, "worktree", "add", wt, branch)

	if _, err := DiscardBranch(BranchActionParams{LocalPath: dir, Branch: branch}, worktreeTestLogger()); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, statErr := os.Lstat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("the task worktree survived its discard (stat err = %v)", statErr)
	}
	if _, err := gitTry(t, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("the branch survived its discard")
	}
	if out := gitRun(t, dir, "worktree", "list", "--porcelain"); strings.Contains(out, wt) {
		t.Fatalf("the worktree registration survived:\n%s", out)
	}
}

// Discard must never move the user's own checkout out from under them.
func TestDiscardBranchRefusesABranchCheckedOutInTheMainTree(t *testing.T) {
	dir, branch := branchActionRepo(t)
	gitRun(t, dir, "checkout", branch)
	t.Cleanup(func() { gitRun(t, dir, "checkout", "main") })

	_, err := DiscardBranch(BranchActionParams{LocalPath: dir, Branch: branch}, worktreeTestLogger())
	if !errors.Is(err, ErrBranchActionRefused) {
		t.Fatalf("err = %v, want ErrBranchActionRefused", err)
	}
	if _, err := gitTry(t, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		t.Fatal("a refused discard deleted the branch anyway")
	}
}

func TestDiscardBranchRefusesTheDefaultBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)

	_, err := DiscardBranch(BranchActionParams{LocalPath: dir, Branch: "main"}, worktreeTestLogger())
	if !errors.Is(err, ErrBranchActionRefused) {
		t.Fatalf("err = %v, want ErrBranchActionRefused", err)
	}
}

// A clone's default branch comes from origin/HEAD, not from whatever the main
// tree happens to have checked out.
func TestDefaultBranchOfPrefersOriginHead(t *testing.T) {
	dir, _ := branchActionRepo(t)
	remote := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(remote); err == nil {
		remote = resolved
	}
	gitRun(t, remote, "init", "--bare", "-b", "trunk")
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "push", "origin", "main:trunk")
	gitRun(t, dir, "remote", "set-head", "origin", "trunk")

	if got := defaultBranchOf(dir); got != "trunk" {
		t.Fatalf("defaultBranchOf = %q, want trunk (origin/HEAD), not the checked-out main", got)
	}
}
