package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// Shared semantic repo index (K47).
//
// Every agent run of a workspace searches the same repositories from scratch:
// the first thing a run does is grep around to find where a thing lives. This
// is that work done once. The daemon holds the checkout, so it produces the
// chunks; the server stores them and hands the most relevant back at claim
// time, in the brief, before the run's first manual exploration.
//
// The index ORIENTS. It is a pointer to a file and a line range, possibly from
// an older commit, and the brief says so — a run still reads the real file
// before editing it. That framing is the feature's safety property, not a
// disclaimer: a stale chunk that is treated as a pointer costs nothing, and a
// stale chunk that is treated as the file's current contents corrupts an edit.

const (
	// RepoIndexUpsertBatch is the largest chunk batch one upsert request may
	// carry. The daemon splits on it; the handler rejects more.
	RepoIndexUpsertBatch = 200

	// repoIndexMaxChunkRunes caps what one chunk may store. A generated file
	// with a single 40k-line function should cost the table one bounded row,
	// not a megabyte of text nobody will read in a brief.
	repoIndexMaxChunkRunes = 8000

	// repoIndexSnippetRunes bounds the excerpt injected into a brief. Eight
	// hints at this size is roughly a page: enough to recognise the right file,
	// far short of "the agent can skip reading it".
	repoIndexSnippetRunes = 600

	// repoIndexPrefilterFactor widens the lexical prefilter relative to the
	// requested top_k so the vector stage has candidates to reorder. With no
	// embeddings configured the extra rows are simply discarded by the LIMIT.
	repoIndexPrefilterFactor = 8
	repoIndexPrefilterFloor  = 40
)

// ErrRepoIndexDisabled is returned when a daemon tries to write to a repo the
// workspace has not opted in to. It maps to 409 at the API boundary: the
// request was well-formed, the workspace's answer is no.
var ErrRepoIndexDisabled = errors.New("repo index: not enabled for this repository")

// RepoIndexSettings is the per-repo opt-in stored under
// workspace.settings.repo_index, keyed by repo identifier.
//
// Opt-in rather than opt-out on purpose: indexing copies source code out of a
// checkout and into the Multica database, and (with an embeddings model
// configured) sends it to a third party. That is a decision a workspace makes,
// not a default it discovers afterwards.
type RepoIndexSettings struct {
	Enabled bool `json:"enabled"`
}

// RepoIndexSettingsFromSettings reads the whole map off a workspace settings
// blob. Missing or unparseable settings mean "nothing opted in", which is the
// safe reading: a corrupt blob must never be mistaken for consent.
func RepoIndexSettingsFromSettings(settings []byte) map[string]RepoIndexSettings {
	out := map[string]RepoIndexSettings{}
	if len(settings) == 0 {
		return out
	}
	var s struct {
		RepoIndex map[string]RepoIndexSettings `json:"repo_index"`
	}
	if json.Unmarshal(settings, &s) != nil {
		return out
	}
	for repo, cfg := range s.RepoIndex {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		out[repo] = cfg
	}
	return out
}

// RepoIndexEnabled answers the one question every write path asks first.
func RepoIndexEnabled(settings []byte, repoIdentifier string) bool {
	return RepoIndexSettingsFromSettings(settings)[strings.TrimSpace(repoIdentifier)].Enabled
}

// RepoIndexEmbedder is the seam for turning text into vectors, satisfied by
// *llm.Client. Nil, or a client with no embedding model, keeps the index
// lexical — which is a supported steady state, not a degraded one.
type RepoIndexEmbedder interface {
	EmbeddingsEnabled() bool
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// RepoIndexChunk is one unit the daemon produced and the server stores.
type RepoIndexChunk struct {
	FilePath    string `json:"file_path"`
	Symbol      string `json:"symbol,omitempty"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
}

// RepoIndexHint is one retrieved chunk as a run receives it.
type RepoIndexHint struct {
	RepoIdentifier string  `json:"repo_identifier"`
	FilePath       string  `json:"file_path"`
	Symbol         string  `json:"symbol,omitempty"`
	StartLine      int     `json:"start_line"`
	EndLine        int     `json:"end_line"`
	Snippet        string  `json:"snippet"`
	Score          float64 `json:"score"`
	Stale          bool    `json:"stale,omitempty"`
}

// RepoIndexStats is the per-repo summary the Settings block renders.
type RepoIndexStats struct {
	ChunkCount        int64  `json:"chunk_count"`
	FileCount         int64  `json:"file_count"`
	LastIndexedCommit string `json:"last_indexed_commit"`
	LastIndexedAt     string `json:"last_indexed_at,omitempty"`
}

// RepoIndexer owns the index's read and write paths. It is a struct rather than
// free functions only because every method needs the same two dependencies.
type RepoIndexer struct {
	Queries   *db.Queries
	TxStarter TxStarter
	// Embedder is optional. Nil or disabled means every chunk is stored with a
	// NULL embedding and every query ranks lexically.
	Embedder RepoIndexEmbedder
}

// DiffFiles reports, for the (path, hash) pairs the daemon found on disk, which
// paths this repo has no chunks for and which carry a different hash. The
// daemon re-chunks exactly those, so an unchanged repo costs one round trip.
func (x *RepoIndexer) DiffFiles(ctx context.Context, workspaceID pgtype.UUID, repo string, files map[string]string) (missing, stale []string, err error) {
	rows, err := x.Queries.DiffRepoIndexFiles(ctx, db.DiffRepoIndexFilesParams{
		WorkspaceID:    workspaceID,
		RepoIdentifier: repo,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("diff repo index files: %w", err)
	}
	indexed := make(map[string]string, len(rows))
	for _, row := range rows {
		indexed[row.FilePath] = row.ContentHash
	}
	missing = []string{}
	stale = []string{}
	for path, hash := range files {
		have, ok := indexed[path]
		switch {
		case !ok:
			missing = append(missing, path)
		case have != hash:
			stale = append(stale, path)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale, nil
}

// UpsertChunks replaces, per file, the chunks of one repo.
//
// The replace is per (repo, file) and atomic: delete-then-insert inside one
// transaction, so a concurrent reader sees either the old chunk set of a file
// or the new one, never a half-written mix. Files are independent, so a batch
// spanning several files still replaces each one wholly.
//
// Embedding failures are swallowed deliberately. The chunk rows are the durable
// value; a vector is a ranking improvement on top. Failing the write because an
// embeddings gateway was down would throw away a successful repository walk.
func (x *RepoIndexer) UpsertChunks(ctx context.Context, workspaceID pgtype.UUID, repo, commit string, chunks []RepoIndexChunk) (int, error) {
	if len(chunks) == 0 {
		return 0, nil
	}

	embeddings := x.embedChunks(ctx, chunks)

	tx, err := x.TxStarter.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin repo index upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := x.Queries.WithTx(tx)

	// Every file appearing in this batch has its existing chunks dropped once,
	// before any insert: a file whose chunks arrive across two batch entries
	// must not have the first entry deleted by the second.
	seen := map[string]struct{}{}
	for _, chunk := range chunks {
		if _, ok := seen[chunk.FilePath]; ok {
			continue
		}
		seen[chunk.FilePath] = struct{}{}
		if err := qtx.DeleteRepoIndexFileChunks(ctx, db.DeleteRepoIndexFileChunksParams{
			WorkspaceID:    workspaceID,
			RepoIdentifier: repo,
			FilePath:       chunk.FilePath,
		}); err != nil {
			return 0, fmt.Errorf("replace repo index file: %w", err)
		}
	}

	for i, chunk := range chunks {
		embedding := pgtype.Text{}
		if i < len(embeddings) && embeddings[i] != "" {
			embedding = pgtype.Text{String: embeddings[i], Valid: true}
		}
		if err := qtx.InsertRepoIndexChunk(ctx, db.InsertRepoIndexChunkParams{
			WorkspaceID:    workspaceID,
			RepoIdentifier: repo,
			FilePath:       chunk.FilePath,
			Symbol:         chunk.Symbol,
			StartLine:      int32(chunk.StartLine),
			EndLine:        int32(chunk.EndLine),
			Content:        truncateRunes(chunk.Content, repoIndexMaxChunkRunes),
			ContentHash:    chunk.ContentHash,
			Embedding:      embedding,
			IndexedCommit:  commit,
		}); err != nil {
			return 0, fmt.Errorf("insert repo index chunk: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit repo index upsert: %w", err)
	}
	return len(chunks), nil
}

// embedChunks returns one pgvector literal per chunk, or an all-empty slice
// when embeddings are off or the upstream failed. Never returns an error: see
// UpsertChunks for why a missing vector is not a failed write.
func (x *RepoIndexer) embedChunks(ctx context.Context, chunks []RepoIndexChunk) []string {
	if x.Embedder == nil || !x.Embedder.EmbeddingsEnabled() {
		return nil
	}
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = repoIndexEmbeddingText(chunk)
	}
	vectors, err := x.Embedder.Embed(ctx, texts)
	if err != nil {
		slog.Warn("repo index: embedding failed; storing chunks lexical-only", "chunks", len(chunks), "error", err)
		return nil
	}
	out := make([]string, len(vectors))
	for i, vec := range vectors {
		out[i] = llm.VectorLiteral(vec)
	}
	return out
}

// repoIndexEmbeddingText is what a chunk is embedded AS. The path and symbol
// lead because a query is usually about a name ("where is the claim response
// built"), and a bare body would embed the language's boilerplate as strongly
// as the thing the chunk is about.
func repoIndexEmbeddingText(chunk RepoIndexChunk) string {
	var b strings.Builder
	b.WriteString(chunk.FilePath)
	if chunk.Symbol != "" {
		b.WriteString(" — ")
		b.WriteString(chunk.Symbol)
	}
	b.WriteString("\n")
	b.WriteString(truncateRunes(chunk.Content, repoIndexMaxChunkRunes))
	return b.String()
}

// Prune closes an indexing pass: it drops the chunks of files that are no
// longer on the repo's default branch, then stamps every remaining chunk with
// the commit the pass verified them at.
//
// An empty present list is refused rather than obeyed: a walk that produced
// nothing is far more likely to be a broken checkout than an emptied
// repository, and obeying it would silently wipe a working index.
//
// The stamp is what makes the `stale` flag mean anything — see the query's own
// comment. It is best-effort: a failed stamp costs the next brief some
// unnecessary "older commit" labels, which is strictly better than failing a
// pass whose real work (the delete) already committed.
func (x *RepoIndexer) Prune(ctx context.Context, workspaceID pgtype.UUID, repo, commit string, presentPaths []string) error {
	if len(presentPaths) == 0 {
		return errors.New("repo index prune: refusing to prune against an empty file list")
	}
	if err := x.Queries.PruneRepoIndexPaths(ctx, db.PruneRepoIndexPathsParams{
		WorkspaceID:    workspaceID,
		RepoIdentifier: repo,
		PresentPaths:   presentPaths,
	}); err != nil {
		return fmt.Errorf("prune repo index: %w", err)
	}
	if commit = strings.TrimSpace(commit); commit != "" {
		if err := x.Queries.StampRepoIndexCommit(ctx, db.StampRepoIndexCommitParams{
			WorkspaceID:    workspaceID,
			RepoIdentifier: repo,
			IndexedCommit:  commit,
		}); err != nil {
			slog.Warn("repo index: stamping the verified commit failed",
				"repo", repo, "commit", commit, "error", err)
		}
	}
	return nil
}

// Query returns the top_k chunks of one repo for a free-text query.
//
// The query embedding is best-effort for the same reason chunk embeddings are:
// a failed or disabled embeddings call degrades the ranking to lexical, it does
// not fail the claim that asked for hints.
func (x *RepoIndexer) Query(ctx context.Context, workspaceID pgtype.UUID, repo, query string, topK int) ([]RepoIndexHint, error) {
	query = strings.TrimSpace(query)
	if query == "" || topK <= 0 {
		return nil, nil
	}
	prefilter := topK * repoIndexPrefilterFactor
	if prefilter < repoIndexPrefilterFloor {
		prefilter = repoIndexPrefilterFloor
	}

	rows, err := x.Queries.QueryRepoIndex(ctx, db.QueryRepoIndexParams{
		WorkspaceID:    workspaceID,
		RepoIdentifier: repo,
		Query:          query,
		QueryEmbedding: x.embedQuery(ctx, query),
		Prefilter:      int32(prefilter),
		TopK:           int32(topK),
	})
	if err != nil {
		return nil, fmt.Errorf("query repo index: %w", err)
	}

	hints := make([]RepoIndexHint, 0, len(rows))
	for _, row := range rows {
		hints = append(hints, RepoIndexHint{
			RepoIdentifier: repo,
			FilePath:       row.FilePath,
			Symbol:         row.Symbol,
			StartLine:      int(row.StartLine),
			EndLine:        int(row.EndLine),
			Snippet:        truncateRunes(row.Content, repoIndexSnippetRunes),
			Score:          row.Score,
			Stale:          row.Stale,
		})
	}
	return hints, nil
}

func (x *RepoIndexer) embedQuery(ctx context.Context, query string) pgtype.Text {
	if x.Embedder == nil || !x.Embedder.EmbeddingsEnabled() {
		return pgtype.Text{}
	}
	vectors, err := x.Embedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 {
		slog.Debug("repo index: query embedding unavailable; ranking lexically", "error", err)
		return pgtype.Text{}
	}
	literal := llm.VectorLiteral(vectors[0])
	if literal == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: literal, Valid: true}
}

// Stats summarises one repo's index for the Settings block.
func (x *RepoIndexer) Stats(ctx context.Context, workspaceID pgtype.UUID, repo string) (RepoIndexStats, error) {
	row, err := x.Queries.RepoIndexStats(ctx, db.RepoIndexStatsParams{
		WorkspaceID:    workspaceID,
		RepoIdentifier: repo,
	})
	if err != nil {
		return RepoIndexStats{}, fmt.Errorf("repo index stats: %w", err)
	}
	out := RepoIndexStats{
		ChunkCount:        row.ChunkCount,
		FileCount:         row.FileCount,
		LastIndexedCommit: row.LastIndexedCommit,
	}
	// '-infinity' is the COALESCE default for a repo with no rows; report it as
	// "never indexed" rather than as a timestamp from the beginning of time.
	if out.ChunkCount > 0 && row.LastIndexedAt.Valid && row.LastIndexedAt.InfinityModifier == pgtype.Finite {
		out.LastIndexedAt = row.LastIndexedAt.Time.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// PurgeRepo removes every chunk of one repository, used when a workspace turns
// the index off for it.
func (x *RepoIndexer) PurgeRepo(ctx context.Context, workspaceID pgtype.UUID, repo string) error {
	if err := x.Queries.PurgeRepoIndexChunksForRepo(ctx, db.PurgeRepoIndexChunksForRepoParams{
		WorkspaceID:    workspaceID,
		RepoIdentifier: repo,
	}); err != nil {
		return fmt.Errorf("purge repo index: %w", err)
	}
	return nil
}
