package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A vector is only comparable to a query the same model embedded. Before the
// embedding_model column, changing MULTICA_LLM_EMBEDDING_MODEL left every
// stored vector in place and cosine distance kept scoring it plausibly: the
// ranking degraded with no error and no way to notice. The model is now
// stamped on write and compared on read, and the chunks a model change
// stranded are counted so "re-index this repo" is visible.
func TestRepoIndexEmbeddingModelMustMatch(t *testing.T) {
	ctx := context.Background()
	repo := repoIndexRepoURL(t)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2`, testWorkspaceID, repo)
	})

	// One unit vector, and the same vector as the query: a matching model
	// scores the full similarity term, a mismatched one scores none of it.
	dims := make([]string, 1536)
	for i := range dims {
		dims[i] = "0"
	}
	dims[0] = "1"
	vector := "[" + strings.Join(dims, ",") + "]"

	insert := func(path, model string) {
		t.Helper()
		if err := testHandler.Queries.InsertRepoIndexChunk(ctx, db.InsertRepoIndexChunkParams{
			WorkspaceID:    parseUUID(testWorkspaceID),
			RepoIdentifier: repo,
			FilePath:       path,
			Symbol:         "parseWidget",
			StartLine:      1,
			EndLine:        9,
			Content:        "func parseWidget() error { return nil }",
			ContentHash:    path,
			Embedding:      pgtype.Text{String: vector, Valid: true},
			EmbeddingModel: pgtype.Text{String: model, Valid: model != ""},
			IndexedCommit:  "abc1234",
		}); err != nil {
			t.Fatalf("insert %s: %v", path, err)
		}
	}
	insert("current.go", "text-embedding-3-small")
	insert("older.go", "text-embedding-ada-002")
	insert("unstamped.go", "")

	score := func(model string) map[string]float64 {
		t.Helper()
		rows, err := testHandler.Queries.QueryRepoIndex(ctx, db.QueryRepoIndexParams{
			WorkspaceID:    parseUUID(testWorkspaceID),
			RepoIdentifier: repo,
			Query:          "parseWidget",
			QueryEmbedding: pgtype.Text{String: vector, Valid: true},
			EmbeddingModel: pgtype.Text{String: model, Valid: model != ""},
			Prefilter:      50,
			TopK:           10,
		})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		out := map[string]float64{}
		for _, row := range rows {
			out[row.FilePath] = row.Score
		}
		return out
	}

	got := score("text-embedding-3-small")
	if len(got) != 3 {
		t.Fatalf("every chunk still answers lexically, got %d: %+v", len(got), got)
	}
	if got["current.go"] <= got["older.go"] {
		t.Errorf("a vector from the model in force must outrank one from another model: %+v", got)
	}
	if got["older.go"] != got["unstamped.go"] {
		t.Errorf("an unstamped vector is as uncomparable as a foreign one: %+v", got)
	}

	// A deployment with no embeddings names no model, so no stored vector is
	// comparable and ranking is purely lexical.
	lexical := score("")
	if lexical["current.go"] != lexical["older.go"] || lexical["current.go"] != lexical["unstamped.go"] {
		t.Errorf("with no model in force nothing may be boosted: %+v", lexical)
	}

	stats, err := testHandler.Queries.RepoIndexStats(ctx, db.RepoIndexStatsParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		RepoIdentifier: repo,
		EmbeddingModel: pgtype.Text{String: "text-embedding-3-small", Valid: true},
	})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.ChunkCount != 3 || stats.UnusableEmbeddingCount != 2 {
		t.Fatalf("stats = %d chunks, %d unusable; want 3 and 2 (the foreign and the unstamped)", stats.ChunkCount, stats.UnusableEmbeddingCount)
	}

	// With embeddings turned off nothing is comparable, so every stored vector
	// counts as unusable rather than silently passing as fine.
	off, err := testHandler.Queries.RepoIndexStats(ctx, db.RepoIndexStatsParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		RepoIdentifier: repo,
	})
	if err != nil {
		t.Fatalf("stats without a model: %v", err)
	}
	if off.UnusableEmbeddingCount != 3 {
		t.Fatalf("unusable without a model in force = %d, want every stored vector", off.UnusableEmbeddingCount)
	}
}
