package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// Walker and throttle for the shared repo index (K47). The chunking rules
// themselves are pinned in repoindex_chunk_test.go — do not re-run that matrix
// through a git repository here.

// repoIndexFakeCache resolves one repo URL to one local git directory.
type repoIndexFakeCache struct {
	url  string
	path string
}

func (c *repoIndexFakeCache) Lookup(_, url string) string {
	if url == c.url {
		return c.path
	}
	return ""
}
func (c *repoIndexFakeCache) BarePath(_, string2 string) string { return c.path }
func (c *repoIndexFakeCache) Sync(string, []repocache.RepoInfo) error {
	return nil
}
func (c *repoIndexFakeCache) WithRepoLock(_ string, fn func() error) error { return fn() }
func (c *repoIndexFakeCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

// repoIndexFakeServer records the three calls a pass makes.
type repoIndexFakeServer struct {
	mu       sync.Mutex
	diffed   []RepoIndexFileHash
	upserted []RepoIndexChunkPayload
	commits  []string
	pruned   []string
	// prunedCommit is what the pass claims it verified the surviving files at.
	prunedCommit string
	// answer is what the diff returns; nil means "everything is missing".
	answer *RepoIndexDiffResult
}

func (s *repoIndexFakeServer) RepoIndexDiff(_ context.Context, _, _ string, files []RepoIndexFileHash) (RepoIndexDiffResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diffed = append(s.diffed, files...)
	if s.answer != nil {
		return *s.answer, nil
	}
	out := RepoIndexDiffResult{}
	for _, f := range files {
		out.Missing = append(out.Missing, f.Path)
	}
	return out, nil
}

func (s *repoIndexFakeServer) RepoIndexUpsert(_ context.Context, _, _, commit string, chunks []RepoIndexChunkPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits, commit)
	s.upserted = append(s.upserted, chunks...)
	return nil
}

func (s *repoIndexFakeServer) RepoIndexPrune(_ context.Context, _, _, commit string, present []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunedCommit = commit
	s.pruned = append(s.pruned, present...)
	return nil
}

func (s *repoIndexFakeServer) paths() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, c := range s.upserted {
		out[c.FilePath]++
	}
	return out
}

// newRepoIndexGitRepo builds a real git repository and returns its git dir.
// Real git rather than a stub: the whole walk is `ls-tree` and `cat-file`
// output parsing, so a stub would only prove the parser agrees with itself.
func newRepoIndexGitRepo(t *testing.T, files map[string][]byte) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "initial")
	return filepath.Join(dir, ".git")
}

func newRepoIndexDaemon(t *testing.T, repoURL, gitDir string) *Daemon {
	t.Helper()
	return &Daemon{
		logger:    slog.New(slog.DiscardHandler),
		repoCache: &repoIndexFakeCache{url: repoURL, path: gitDir},
	}
}

func TestRepoIndexPassUploadsChunksAndPrunes(t *testing.T) {
	const repoURL = "git@example.com:team/app.git"
	gitDir := newRepoIndexGitRepo(t, map[string][]byte{
		"main.go":                   []byte("package main\n\nfunc Run() error {\n\treturn nil\n}\n"),
		"web/app.ts":                []byte("export function boot() {}\n"),
		"node_modules/dep/index.js": []byte("module.exports = 1;\n"),
		"assets/logo.png":           {0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02, 0x03},
		"docs/notes.md":             []byte("# Notes\n\nsome prose\n"),
	})
	d := newRepoIndexDaemon(t, repoURL, gitDir)
	server := &repoIndexFakeServer{}

	if err := d.indexRepoWith(context.Background(), server, "ws-1", repoURL, d.logger); err != nil {
		t.Fatalf("indexRepo: %v", err)
	}

	got := server.paths()
	for _, want := range []string{"main.go", "web/app.ts", "docs/notes.md"} {
		if got[want] == 0 {
			t.Errorf("no chunks uploaded for %q (uploaded: %v)", want, got)
		}
	}
	// A dependency tree and a binary must never be chunked: one buries the
	// repository's own symbols, the other stores bytes as a corrupt string.
	if got["node_modules/dep/index.js"] != 0 {
		t.Error("node_modules was indexed")
	}
	if got["assets/logo.png"] != 0 {
		t.Error("a binary file was indexed")
	}

	// Every upload carries the commit it was produced from, which is what the
	// server compares against to mark a hint stale.
	for _, commit := range server.commits {
		if len(commit) < 7 {
			t.Errorf("upload carried commit %q, want a resolved sha", commit)
		}
	}

	// content_hash is git's blob OID, and the same value the diff advertised —
	// if those two disagreed, every pass would re-upload every file forever.
	hashByPath := map[string]string{}
	for _, f := range server.diffed {
		hashByPath[f.Path] = f.Hash
	}
	for _, chunk := range server.upserted {
		if hashByPath[chunk.FilePath] != chunk.ContentHash {
			t.Errorf("%s: uploaded hash %q, diff advertised %q",
				chunk.FilePath, chunk.ContentHash, hashByPath[chunk.FilePath])
		}
	}

	// The prune list is what still exists, and it must not include the files
	// the walk deliberately skipped: a skipped path is not a deleted one, but it
	// has no chunks either, so listing it is harmless and omitting it is not.
	pruned := map[string]bool{}
	for _, p := range server.pruned {
		pruned[p] = true
	}
	if !pruned["main.go"] || !pruned["docs/notes.md"] {
		t.Errorf("prune list missing live files: %v", server.pruned)
	}
	// The prune carries the commit so the server can stamp the files the pass
	// checked but did not re-upload. Without it every unchanged file would keep
	// an older commit and be labelled stale in every brief from then on.
	if len(server.prunedCommit) < 7 {
		t.Errorf("prune carried commit %q, want the resolved sha", server.prunedCommit)
	}
}

func TestRepoIndexPassSkipsUnchangedFiles(t *testing.T) {
	const repoURL = "git@example.com:team/app.git"
	gitDir := newRepoIndexGitRepo(t, map[string][]byte{
		"a.go": []byte("package a\n\nfunc A() {}\n"),
		"b.go": []byte("package b\n\nfunc B() {}\n"),
	})
	d := newRepoIndexDaemon(t, repoURL, gitDir)
	// The server already has a.go and reports only b.go as changed.
	server := &repoIndexFakeServer{answer: &RepoIndexDiffResult{Stale: []string{"b.go"}}}

	if err := d.indexRepoWith(context.Background(), server, "ws-1", repoURL, d.logger); err != nil {
		t.Fatalf("indexRepo: %v", err)
	}
	got := server.paths()
	if got["a.go"] != 0 {
		t.Errorf("re-uploaded an unchanged file: %v", got)
	}
	if got["b.go"] == 0 {
		t.Errorf("did not upload the changed file: %v", got)
	}
	// The prune list is still the FULL tree, not just the changed files: a
	// prune against the changed set alone would delete every untouched file's
	// chunks on the first incremental pass.
	if len(server.pruned) != 2 {
		t.Errorf("prune list = %v, want both live files", server.pruned)
	}
}

func TestRepoIndexPassSkipsUncachedRepo(t *testing.T) {
	d := newRepoIndexDaemon(t, "git@example.com:team/app.git", "")
	server := &repoIndexFakeServer{}
	if err := d.indexRepoWith(context.Background(), server, "ws-1", "git@example.com:team/other.git", d.logger); err != nil {
		t.Fatalf("indexRepo on an uncached repo: %v", err)
	}
	if len(server.diffed) != 0 || len(server.pruned) != 0 {
		t.Error("a repo with no local clone still hit the server")
	}
}

func TestRepoIndexThrottleAllowsOnePassPerRepo(t *testing.T) {
	d := &Daemon{logger: slog.New(slog.DiscardHandler)}
	if !d.claimRepoIndexSlot("ws-1", "repo-a") {
		t.Fatal("first claim refused")
	}
	if d.claimRepoIndexSlot("ws-1", "repo-a") {
		t.Error("second claim inside the throttle window was allowed")
	}
	// The throttle is per repository and per workspace, not global: a second
	// repo finishing at the same moment must not be blocked by the first.
	if !d.claimRepoIndexSlot("ws-1", "repo-b") {
		t.Error("a different repo was blocked by another repo's pass")
	}
	if !d.claimRepoIndexSlot("ws-2", "repo-a") {
		t.Error("the same repo in another workspace was blocked")
	}
}

func TestRepoIndexPassRefusesRepoNotEnabledByServer(t *testing.T) {
	// The opt-in gate is the server's list, carried on the claim. A repo the
	// task used but the server did not enable must produce no pass at all —
	// the daemon never forms its own opinion about consent.
	d := &Daemon{logger: slog.New(slog.DiscardHandler), repoCache: &repoIndexFakeCache{}}
	task := Task{
		WorkspaceID:      "ws-1",
		Repos:            []RepoData{{URL: "git@example.com:team/app.git"}},
		RepoIndexEnabled: nil,
	}
	d.maybeIndexRepos(task, d.logger)
	if len(d.repoIndexLast) != 0 {
		t.Errorf("a pass was scheduled for a repo the server did not enable: %v", d.repoIndexLast)
	}

	task.RepoIndexEnabled = []string{"git@example.com:team/OTHER.git"}
	d.maybeIndexRepos(task, d.logger)
	if len(d.repoIndexLast) != 0 {
		t.Errorf("a pass was scheduled for a repo outside the enabled list: %v", d.repoIndexLast)
	}
}
