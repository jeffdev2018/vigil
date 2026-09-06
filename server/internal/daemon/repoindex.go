package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Shared semantic repo index (K47), daemon side.
//
// The server has no checkout; this machine does. After a run finishes, this
// walks the repository's default branch in the bare cache, asks the server what
// changed, chunks only that, and uploads it. The whole pass is best-effort: it
// happens after the task result has already been reported, and nothing about a
// run's outcome depends on it.
//
// Three properties are load-bearing:
//
//   - It never indexes a repository the SERVER did not list as enabled. The
//     opt-in is the workspace's consent to copy its source into Multica's
//     database; the daemon holds no opinion of its own about it and the server
//     refuses the write anyway (409).
//   - It reads the BARE CACHE, not the run's working tree. The cache is the
//     daemon's canonical copy of the default branch. A task's working tree is a
//     disposable worktree that may be mid-edit, on a feature branch, or absent
//     entirely — indexing it would store one run's uncommitted work as the
//     workspace's shared view of the repository.
//   - It is throttled per repository. Ten runs finishing in a minute against the
//     same repo must not produce ten identical walks.

const (
	// repoIndexThrottle is the minimum gap between two passes over the same
	// repository. Repository content changes on the scale of minutes at best,
	// and a pass costs a `git ls-tree` plus one server round trip even when
	// nothing changed.
	repoIndexThrottle = 10 * time.Minute

	// repoIndexPassTimeout bounds one whole pass, including the git commands and
	// every upload batch. A pass that cannot finish inside it is abandoned; the
	// next run's pass starts over from the same incremental diff, so nothing is
	// lost beyond the time already spent.
	repoIndexPassTimeout = 5 * time.Minute

	// repoIndexGitTimeout bounds one git invocation.
	repoIndexGitTimeout = 60 * time.Second

	// repoIndexMaxFiles / repoIndexMaxChunks mirror the server's ceilings. A
	// repository past either is indexed PARTIALLY rather than skipped: a partial
	// index still orients a run, and refusing outright would turn the feature
	// off on exactly the large repositories that need it most.
	repoIndexMaxFiles  = 2000
	repoIndexMaxChunks = 20000

	// repoIndexUploadBatch mirrors the server's per-request chunk ceiling.
	repoIndexUploadBatch = 200
)

// repoIndexServer is the seam for the three server calls, so the pass can be
// driven in tests without an HTTP upstream.
type repoIndexServer interface {
	RepoIndexDiff(ctx context.Context, workspaceID, repoIdentifier string, files []RepoIndexFileHash) (RepoIndexDiffResult, error)
	RepoIndexUpsert(ctx context.Context, workspaceID, repoIdentifier, commit string, chunks []RepoIndexChunkPayload) error
	RepoIndexPrune(ctx context.Context, workspaceID, repoIdentifier, commit string, presentPaths []string) error
}

// repoIndexTreeEntry is one blob on the indexed branch.
type repoIndexTreeEntry struct {
	Path string
	// OID is git's own content hash for the blob. It IS the change-detection
	// hash this feature needs and `ls-tree` hands it over for free, so the diff
	// costs zero file reads — a repository that did not move since the last pass
	// is settled by one git command and one request, without opening a file.
	OID string
}

// maybeIndexRepos runs an indexing pass for every enabled repository of a
// finished task, in the background.
//
// Detached from the caller's context on purpose: this fires at the end of
// handleTask, whose context is cancelled as soon as the daemon starts shutting
// down, and a half-written index is worse than a skipped pass.
func (d *Daemon) maybeIndexRepos(task Task, taskLog *slog.Logger) {
	if d.client == nil || d.repoCache == nil || task.WorkspaceID == "" || len(task.RepoIndexEnabled) == 0 {
		return
	}
	enabled := make(map[string]struct{}, len(task.RepoIndexEnabled))
	for _, repo := range task.RepoIndexEnabled {
		if repo = strings.TrimSpace(repo); repo != "" {
			enabled[repo] = struct{}{}
		}
	}

	var targets []string
	for _, repo := range task.Repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		if _, ok := enabled[url]; !ok {
			continue
		}
		if !d.claimRepoIndexSlot(task.WorkspaceID, url) {
			taskLog.Debug("repo index: throttled", "repo", url)
			continue
		}
		targets = append(targets, url)
	}
	if len(targets) == 0 {
		return
	}

	workspaceID := task.WorkspaceID
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				taskLog.Error("repo index: pass panicked", "panic", rec)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), repoIndexPassTimeout)
		defer cancel()
		for _, repo := range targets {
			if err := d.indexRepoWith(ctx, d.client, workspaceID, repo, taskLog); err != nil {
				taskLog.Warn("repo index: pass failed", "repo", repo, "error", err)
			}
		}
	}()
}

// claimRepoIndexSlot reports whether this repository may be walked now, and
// records the attempt when it may. Claiming BEFORE the pass rather than
// stamping after it is deliberate: several tasks finishing at once must not all
// see a stale timestamp and start the same walk in parallel.
func (d *Daemon) claimRepoIndexSlot(workspaceID, repo string) bool {
	key := workspaceID + "\x00" + repo
	now := time.Now()
	d.repoIndexMu.Lock()
	defer d.repoIndexMu.Unlock()
	if d.repoIndexLast == nil {
		d.repoIndexLast = map[string]time.Time{}
	}
	if last, ok := d.repoIndexLast[key]; ok && now.Sub(last) < repoIndexThrottle {
		return false
	}
	d.repoIndexLast[key] = now
	return true
}

// indexRepoWith runs one incremental pass over one repository against the given
// server. The seam is a parameter rather than a field so a test drives the walk
// without an HTTP upstream; production always passes d.client.
func (d *Daemon) indexRepoWith(ctx context.Context, server repoIndexServer, workspaceID, repo string, log *slog.Logger) error {
	barePath := d.repoCache.Lookup(workspaceID, repo)
	if barePath == "" {
		// Nothing cached locally: the run never checked this repo out. Skipping
		// is correct — cloning a repository purely to index it would turn a
		// best-effort background pass into an unbounded network fetch.
		log.Debug("repo index: no local cache; skipping", "repo", repo)
		return nil
	}

	commit, err := repoIndexGit(ctx, barePath, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	commit = strings.TrimSpace(commit)

	entries, truncated, err := repoIndexListTree(ctx, barePath)
	if err != nil {
		return fmt.Errorf("list tree: %w", err)
	}
	if len(entries) == 0 {
		// An empty listing is either an empty repository or a broken cache. The
		// server refuses a prune against an empty list for the same reason:
		// obeying it would wipe a working index.
		log.Debug("repo index: nothing indexable on the default branch", "repo", repo)
		return nil
	}

	files := make([]RepoIndexFileHash, 0, len(entries))
	oidByPath := make(map[string]string, len(entries))
	presentPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, RepoIndexFileHash{Path: entry.Path, Hash: entry.OID})
		oidByPath[entry.Path] = entry.OID
		presentPaths = append(presentPaths, entry.Path)
	}

	diff, err := server.RepoIndexDiff(ctx, workspaceID, repo, files)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	changed := append(append([]string{}, diff.Missing...), diff.Stale...)
	if len(changed) > 0 {
		if err := uploadRepoIndexChunks(ctx, server, workspaceID, repo, commit, barePath, changed, oidByPath, log); err != nil {
			return err
		}
	}

	// Prune last: a file deleted upstream must lose its chunks even on a pass
	// that had nothing new to upload. It also carries the commit, which stamps
	// every surviving chunk as verified at it — the pass DID check the files it
	// did not re-upload, by comparing their hashes.
	if err := server.RepoIndexPrune(ctx, workspaceID, repo, commit, presentPaths); err != nil {
		return fmt.Errorf("prune: %w", err)
	}

	log.Info("repo index: pass complete",
		"repo", repo, "commit", commit, "files", len(entries),
		"changed", len(changed), "files_truncated", truncated)
	return nil
}

// uploadRepoIndexChunks reads, chunks and uploads the changed files.
//
// A file's chunks are never split across two batches: the server replaces a
// file's chunks atomically per request, so a file spanning two requests would
// have its first half deleted by its second.
func uploadRepoIndexChunks(
	ctx context.Context,
	server repoIndexServer,
	workspaceID, repo, commit, barePath string,
	changed []string,
	oidByPath map[string]string,
	log *slog.Logger,
) error {
	oids := make([]string, 0, len(changed))
	for _, path := range changed {
		if oid, ok := oidByPath[path]; ok {
			oids = append(oids, oid)
		}
	}
	blobs, err := repoIndexReadBlobs(ctx, barePath, oids)
	if err != nil {
		return fmt.Errorf("read blobs: %w", err)
	}

	var batch []RepoIndexChunkPayload
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := server.RepoIndexUpsert(ctx, workspaceID, repo, commit, batch); err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
		batch = batch[:0]
		return nil
	}

	for _, path := range changed {
		oid := oidByPath[path]
		content, ok := blobs[oid]
		if !ok || !RepoIndexIndexableFile(content) {
			continue
		}
		chunks := ChunkSource(string(content))
		if len(chunks) == 0 {
			continue
		}
		if total+len(chunks) > repoIndexMaxChunks {
			log.Warn("repo index: chunk ceiling reached; indexing partially",
				"repo", repo, "ceiling", repoIndexMaxChunks)
			break
		}
		// Flush before a file that would overflow the batch, so this file's
		// chunks travel together.
		if len(batch)+len(chunks) > repoIndexUploadBatch {
			if err := flush(); err != nil {
				return err
			}
		}
		for _, chunk := range chunks {
			batch = append(batch, RepoIndexChunkPayload{
				FilePath:    path,
				Symbol:      chunk.Symbol,
				StartLine:   chunk.StartLine,
				EndLine:     chunk.EndLine,
				ContentHash: oid,
				Content:     chunk.Content,
			})
		}
		total += len(chunks)
		// A single file with more chunks than one batch holds still has to go in
		// one request, or its own chunks would delete each other.
		if len(batch) >= repoIndexUploadBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// repoIndexListTree lists the indexable blobs of the bare repository's HEAD.
// Reports whether the file ceiling truncated the listing.
func repoIndexListTree(ctx context.Context, barePath string) ([]repoIndexTreeEntry, bool, error) {
	out, err := repoIndexGit(ctx, barePath, "ls-tree", "-r", "-l", "--full-tree", "HEAD")
	if err != nil {
		return nil, false, err
	}
	var entries []repoIndexTreeEntry
	truncated := false
	for _, line := range strings.Split(out, "\n") {
		// `<mode> <type> <oid> <size>\t<path>`; size is "-" for non-blobs.
		meta, path, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 4 || fields[1] != "blob" {
			continue
		}
		size, sizeErr := strconv.ParseInt(fields[3], 10, 64)
		if sizeErr != nil || size <= 0 || size > repoIndexMaxFileBytes {
			continue
		}
		// git quotes paths containing control or non-ASCII bytes. Skip those
		// rather than guess at the unquoting: they are rare, and a mis-unquoted
		// path would index one file under another's name.
		if strings.HasPrefix(path, "\"") {
			continue
		}
		if RepoIndexSkipPath(path) {
			continue
		}
		if len(entries) >= repoIndexMaxFiles {
			truncated = true
			break
		}
		entries = append(entries, repoIndexTreeEntry{Path: path, OID: fields[2]})
	}
	return entries, truncated, nil
}

// repoIndexReadBlobs reads several blobs in ONE git process via `cat-file
// --batch`, keyed by OID. Per-blob invocations would spawn one process per
// changed file, which on a cold index is a couple of thousand of them.
func repoIndexReadBlobs(ctx context.Context, barePath string, oids []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(oids) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, repoIndexGitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "--git-dir", barePath, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer stdin.Close()
		for _, oid := range oids {
			if _, err := io.WriteString(stdin, oid+"\n"); err != nil {
				return
			}
		}
	}()

	reader := bufio.NewReader(stdout)
	for range oids {
		header, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		fields := strings.Fields(strings.TrimSpace(header))
		// `<oid> missing` for an unknown object; `<oid> <type> <size>` otherwise.
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		size, sizeErr := strconv.Atoi(fields[2])
		if sizeErr != nil || size < 0 {
			break
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(reader, body); err != nil {
			break
		}
		// Each object is followed by a newline the protocol adds.
		if _, err := reader.Discard(1); err != nil {
			break
		}
		out[fields[0]] = body
	}
	_, _ = io.Copy(io.Discard, reader)
	if err := cmd.Wait(); err != nil {
		return out, err
	}
	return out, nil
}

// repoIndexGit runs one read-only git command against a bare repository.
func repoIndexGit(ctx context.Context, barePath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, repoIndexGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--git-dir", barePath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// compile-time check that the real client satisfies the pass's seam.
var _ repoIndexServer = (*Client)(nil)
