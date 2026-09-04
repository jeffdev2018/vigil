package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Traffic control (K18), daemon side: on each HTTP heartbeat, report the
// files a human has changed in the local checkouts this daemon knows
// (the durable work dirs of its runs). Best effort: a checkout that is
// not a git repo, or a slow git, is skipped. No file is ever locked.

// DirtyCheckout mirrors service.DirtyCheckout on the wire.
type DirtyCheckout struct {
	Root  string   `json:"root"`
	Paths []string `json:"paths"`
}

const (
	dirtyCheckoutsMax     = 20
	dirtyPathsPerCheckout = 200
	dirtyGitTimeout       = 3 * time.Second
)

func (d *Daemon) rememberCheckout(dir string) {
	if dir = strings.TrimSpace(dir); dir != "" {
		d.durableCheckouts.Store(dir, struct{}{})
	}
}

// collectDirtyCheckouts runs `git status --porcelain` in every known checkout.
func (d *Daemon) collectDirtyCheckouts(ctx context.Context) []DirtyCheckout {
	var out []DirtyCheckout
	d.durableCheckouts.Range(func(key, _ any) bool {
		root := key.(string)
		if paths := gitDirtyPaths(ctx, root); paths != nil {
			out = append(out, DirtyCheckout{Root: root, Paths: paths})
		}
		return len(out) < dirtyCheckoutsMax
	})
	if out == nil {
		return []DirtyCheckout{}
	}
	return out
}

// gitDirtyPaths lists modified tracked files, repo-relative; nil when git
// cannot answer.
func gitDirtyPaths(ctx context.Context, root string) []string {
	gitCtx, cancel := context.WithTimeout(ctx, dirtyGitTimeout)
	defer cancel()
	raw, err := exec.CommandContext(gitCtx, "git", "-C", root, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		return nil
	}
	paths := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if i := strings.LastIndex(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		p = strings.Trim(p, `"`)
		if p != "" {
			paths = append(paths, p)
		}
		if len(paths) >= dirtyPathsPerCheckout {
			break
		}
	}
	return paths
}
