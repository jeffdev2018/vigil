package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// computeRunDiff is what fills a racing attempt's column in the compare view
// (F11). It runs against the user's own repository after Finalize, so these
// tests build a real one rather than faking git.

func runDiffGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

// runDiffRepo builds a repo with one base commit and a branch on top of it
// carrying `lines` added lines plus one removed line, and returns the repo, the
// base commit and the branch name.
func runDiffRepo(t *testing.T, lines int) (dir, base, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	runDiffGitCmd(t, dir, "init", "-b", "main")
	runDiffGitCmd(t, dir, "config", "user.name", "Test User")
	runDiffGitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	runDiffGitCmd(t, dir, "add", ".")
	runDiffGitCmd(t, dir, "commit", "-m", "base")
	base = runDiffGitCmd(t, dir, "rev-parse", "HEAD")

	branch = "agent/attempt"
	runDiffGitCmd(t, dir, "checkout", "-b", branch)
	// One deletion (the original line) and `lines` insertions in a.txt, plus a
	// second file so the file count is not trivially 1.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("changed line of some length\n", lines)), 0o644); err != nil {
		t.Fatalf("rewrite a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	runDiffGitCmd(t, dir, "add", ".")
	runDiffGitCmd(t, dir, "commit", "-m", "attempt")
	runDiffGitCmd(t, dir, "checkout", "main")
	return dir, base, branch
}

func TestComputeRunDiffMeasuresTheAttemptAndCarriesThePatch(t *testing.T) {
	dir, base, branch := runDiffRepo(t, 3)

	diff := computeRunDiff(context.Background(), dir, base, branch, maxRunDiffBytes, nil)
	if diff == nil {
		t.Fatal("expected a diff for a branch that changed files")
	}
	if diff.Stat.Files != 2 {
		t.Errorf("files = %d, want 2 (a.txt rewritten, b.txt added)", diff.Stat.Files)
	}
	if diff.Stat.Insertions != 4 {
		t.Errorf("insertions = %d, want 4 (3 lines in a.txt, 1 in b.txt)", diff.Stat.Insertions)
	}
	if diff.Stat.Deletions != 1 {
		t.Errorf("deletions = %d, want 1 (a.txt's original line)", diff.Stat.Deletions)
	}
	if !strings.Contains(diff.Unified, "+++ b/b.txt") {
		t.Errorf("unified patch does not describe the new file:\n%s", diff.Unified)
	}
}

func TestComputeRunDiffDropsThePatchOverTheBound(t *testing.T) {
	dir, base, branch := runDiffRepo(t, 200)

	// The bound is injected so the test does not have to produce 256 KiB. What
	// it proves is the contract the UI reads: a stat with no patch is the
	// truncated case, not a missing diff.
	diff := computeRunDiff(context.Background(), dir, base, branch, 64, nil)
	if diff == nil {
		t.Fatal("an over-the-bound patch must still report its stat")
	}
	if diff.Unified != "" {
		t.Errorf("unified patch should have been dropped, got %d bytes", len(diff.Unified))
	}
	if diff.Stat.Files != 2 || diff.Stat.Insertions != 201 {
		t.Errorf("stat = %+v, want 2 files and 201 insertions", diff.Stat)
	}
}

func TestComputeRunDiffReturnsNothingWhenGitCannotAnswer(t *testing.T) {
	dir, base, branch := runDiffRepo(t, 1)

	for _, tc := range []struct {
		name            string
		root, base, tip string
	}{
		{name: "not a repository", root: t.TempDir(), base: base, tip: branch},
		{name: "unknown revision", root: dir, base: "0000000000000000000000000000000000000000", tip: branch},
		{name: "unknown branch", root: dir, base: base, tip: "agent/never-existed"},
		{name: "no base", root: dir, base: "", tip: branch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if diff := computeRunDiff(context.Background(), tc.root, tc.base, tc.tip, maxRunDiffBytes, nil); diff != nil {
				t.Errorf("expected no diff, got %+v", diff)
			}
		})
	}
}

func TestParseNumstatCountsBinaryFilesWithoutLines(t *testing.T) {
	stat := parseNumstat("3\t1\tsrc/a.go\n-\t-\tassets/logo.png\n")
	if stat.Files != 2 {
		t.Errorf("files = %d, want 2", stat.Files)
	}
	if stat.Insertions != 3 || stat.Deletions != 1 {
		t.Errorf("stat = %+v, want 3 insertions and 1 deletion", stat)
	}
}
