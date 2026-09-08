package daemon

import (
	"context"
	"fmt"
	"net/url"
)

// Client calls for the shared semantic repo index (K47). The three requests
// close one incremental pass: ask what changed, upload the chunks of what did,
// then drop what no longer exists.

// RepoIndexFileHash pairs a repository-relative path with the sha256 of its
// current contents.
type RepoIndexFileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// RepoIndexDiffResult names the files the server has no chunks for (Missing)
// and the ones whose stored hash disagrees with the local file (Stale).
type RepoIndexDiffResult struct {
	Missing []string `json:"missing"`
	Stale   []string `json:"stale"`
}

// RepoIndexChunkPayload is one chunk on the wire. ContentHash is the sha256 of
// the WHOLE FILE the chunk came from, which is what the diff compares against.
type RepoIndexChunkPayload struct {
	FilePath    string `json:"file_path"`
	Symbol      string `json:"symbol,omitempty"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
}

func repoIndexPath(workspaceID, action string) string {
	return fmt.Sprintf("/api/daemon/workspaces/%s/repo-index/%s", url.PathEscape(workspaceID), action)
}

// RepoIndexDiff asks which of the daemon's files the server needs.
func (c *Client) RepoIndexDiff(ctx context.Context, workspaceID, repoIdentifier string, files []RepoIndexFileHash) (RepoIndexDiffResult, error) {
	var out RepoIndexDiffResult
	err := c.postJSON(ctx, repoIndexPath(workspaceID, "diff"), map[string]any{
		"repo_identifier": repoIdentifier,
		"files":           files,
	}, &out)
	return out, err
}

// RepoIndexUpsert uploads one batch of chunks. The server replaces each file's
// chunks atomically, so a file may be split across batches only if every chunk
// of it is in the same batch — repoIndexUploader guarantees that.
func (c *Client) RepoIndexUpsert(ctx context.Context, workspaceID, repoIdentifier, commit string, chunks []RepoIndexChunkPayload) error {
	return c.postJSON(ctx, repoIndexPath(workspaceID, "upsert"), map[string]any{
		"repo_identifier": repoIdentifier,
		"commit":          commit,
		"chunks":          chunks,
	}, nil)
}

// RepoIndexPrune closes the pass by naming every path still present, and the
// commit the pass verified them at.
func (c *Client) RepoIndexPrune(ctx context.Context, workspaceID, repoIdentifier, commit string, presentPaths []string) error {
	return c.postJSON(ctx, repoIndexPath(workspaceID, "prune"), map[string]any{
		"repo_identifier": repoIdentifier,
		"commit":          commit,
		"present_paths":   presentPaths,
	}, nil)
}
