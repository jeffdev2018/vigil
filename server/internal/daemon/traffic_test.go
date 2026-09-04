package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Traffic control (K18): the daemon reports the tracked files a human
// modified in a checkout it knows, nothing for a clean or non-git dir.

func TestCollectDirtyCheckouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go")
	run("commit", "-q", "-m", "init")
	var d Daemon
	d.rememberCheckout(root)
	d.rememberCheckout(t.TempDir()) // not a repo: skipped
	if got := d.collectDirtyCheckouts(context.Background()); len(got) != 1 || len(got[0].Paths) != 0 {
		t.Fatalf("clean checkout = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := d.collectDirtyCheckouts(context.Background())
	if len(got) != 1 || len(got[0].Paths) != 1 || got[0].Paths[0] != "a.go" {
		t.Fatalf("dirty checkout = %+v, want only the tracked change", got)
	}
}
