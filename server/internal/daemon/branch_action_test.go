package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// JEF-255: the daemon half of promote/discard — the heartbeat payload lands,
// git does the work in a real repository, and the terminal report carries the
// outcome (or the named refusal) back through the real Client.

// branchActionReportDaemon records the payloads posted to the result endpoint.
func branchActionReportDaemon(t *testing.T) (*Daemon, func() []map[string]any) {
	t.Helper()
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
	return d, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), reports...)
	}
}

// branchActionRepo builds a repo with one delivered run branch on top of main.
func branchActionRepo(t *testing.T) (dir, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	branchActionGit(t, dir, "init", "-b", "main")
	branchActionGit(t, dir, "config", "user.name", "Test User")
	branchActionGit(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branchActionGit(t, dir, "add", ".")
	branchActionGit(t, dir, "commit", "-m", "base")
	branch = "agent/j/PROJ-1"
	branchActionGit(t, dir, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("the run's work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branchActionGit(t, dir, "add", ".")
	branchActionGit(t, dir, "commit", "-m", "delivered")
	branchActionGit(t, dir, "checkout", "main")
	return dir, branch
}

func TestHandleBranchActionDiscardDeletesTheBranch(t *testing.T) {
	dir, branch := branchActionRepo(t)
	d, reports := branchActionReportDaemon(t)

	d.handleBranchAction(context.Background(), Runtime{ID: "rt-1"}, PendingBranchAction{
		ID: "req-1", TaskID: "task-1", Action: "discard", LocalPath: dir, Branch: branch,
	})

	got := reports()
	if len(got) != 1 {
		t.Fatalf("reports = %#v, want exactly one terminal report", got)
	}
	if got[0]["status"] != "completed" || got[0]["head_sha"] == "" {
		t.Fatalf("report = %#v, want completed with the discarded tip", got[0])
	}
	if _, err := branchActionGitTry(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("the branch survived its discard")
	}
}

func TestHandleBranchActionDiscardToleratesAMissingBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)
	d, reports := branchActionReportDaemon(t)

	d.handleBranchAction(context.Background(), Runtime{ID: "rt-1"}, PendingBranchAction{
		ID: "req-1", TaskID: "task-1", Action: "discard", LocalPath: dir, Branch: "agent/j/already-gone",
	})

	got := reports()
	if len(got) != 1 || got[0]["status"] != "completed" {
		t.Fatalf("reports = %#v, want one completed report for a branch that is already gone", got)
	}
}

func TestHandleBranchActionRefusesTheDefaultBranch(t *testing.T) {
	dir, _ := branchActionRepo(t)
	d, reports := branchActionReportDaemon(t)

	d.handleBranchAction(context.Background(), Runtime{ID: "rt-1"}, PendingBranchAction{
		ID: "req-1", TaskID: "task-1", Action: "discard", LocalPath: dir, Branch: "main",
	})

	got := reports()
	if len(got) != 1 || got[0]["status"] != "failed" {
		t.Fatalf("reports = %#v, want one failed report", got)
	}
	if errText, _ := got[0]["error"].(string); !strings.Contains(errText, "default branch") {
		t.Fatalf("error = %q, want the refusal to name the default branch", errText)
	}
	if _, err := branchActionGitTry(dir, "rev-parse", "--verify", "--quiet", "refs/heads/main"); err != nil {
		t.Fatal("a refused discard deleted the default branch anyway")
	}
}

func TestHandleBranchActionPromotePushesToOrigin(t *testing.T) {
	dir, branch := branchActionRepo(t)
	remote := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(remote); err == nil {
		remote = resolved
	}
	branchActionGit(t, remote, "init", "--bare", "-b", "main")
	branchActionGit(t, dir, "remote", "add", "origin", remote)
	d, reports := branchActionReportDaemon(t)

	d.handleBranchAction(context.Background(), Runtime{ID: "rt-1"}, PendingBranchAction{
		ID: "req-1", TaskID: "task-1", Action: "promote", LocalPath: dir, Branch: branch,
	})

	got := reports()
	if len(got) != 1 || got[0]["status"] != "completed" {
		t.Fatalf("reports = %#v, want one completed report", got)
	}
	if got[0]["remote_url"] != remote {
		t.Fatalf("remote_url = %v, want %q so the server can find the VCS connection", got[0]["remote_url"], remote)
	}
	if _, err := branchActionGitTry(remote, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		t.Fatal("origin does not carry the promoted branch")
	}
}

func TestHandleBranchActionPromoteWithoutOriginReportsNoRemote(t *testing.T) {
	dir, branch := branchActionRepo(t)
	d, reports := branchActionReportDaemon(t)

	d.handleBranchAction(context.Background(), Runtime{ID: "rt-1"}, PendingBranchAction{
		ID: "req-1", TaskID: "task-1", Action: "promote", LocalPath: dir, Branch: branch,
	})

	got := reports()
	if len(got) != 1 || got[0]["status"] != "failed" {
		t.Fatalf("reports = %#v, want one failed report", got)
	}
	if errText, _ := got[0]["error"].(string); !strings.HasPrefix(errText, "no_remote") {
		t.Fatalf("error = %q, want the no_remote refusal", errText)
	}
}

func branchActionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
}

func branchActionGitTry(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
