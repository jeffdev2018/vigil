package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/llm"
)

// Canonical layer for the settings shape and the embedder gate of the shared
// repo index (K47). The SQL behaviour is pinned in
// internal/handler/repo_index_test.go against a real database.

func TestRepoIndexSettingsFromSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		repo     string
		want     bool
	}{
		{"enabled", `{"repo_index":{"git@x:a.git":{"enabled":true}}}`, "git@x:a.git", true},
		{"disabled", `{"repo_index":{"git@x:a.git":{"enabled":false}}}`, "git@x:a.git", false},
		{"other repo", `{"repo_index":{"git@x:a.git":{"enabled":true}}}`, "git@x:b.git", false},
		// Everything below is a state that must read as "no consent". A blob
		// that cannot be parsed is not permission to copy a repository.
		{"empty settings", ``, "git@x:a.git", false},
		{"no repo_index key", `{"workflow_limits":{"max_legs":3}}`, "git@x:a.git", false},
		{"corrupt json", `{"repo_index":`, "git@x:a.git", false},
		{"repo_index is not an object", `{"repo_index":"yes"}`, "git@x:a.git", false},
		{"null entry", `{"repo_index":{"git@x:a.git":null}}`, "git@x:a.git", false},
	}
	for _, tc := range cases {
		if got := RepoIndexEnabled([]byte(tc.settings), tc.repo); got != tc.want {
			t.Errorf("%s: RepoIndexEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Whitespace around a key is not a different repository.
	if got := RepoIndexSettingsFromSettings([]byte(`{"repo_index":{"  ":{"enabled":true}}}`)); len(got) != 0 {
		t.Errorf("a blank repo key was kept: %v", got)
	}
}

// repoIndexFakeEmbedder counts calls and can fail on demand.
type repoIndexFakeEmbedder struct {
	enabled bool
	model   string
	calls   int
	err     error
}

func (e *repoIndexFakeEmbedder) EmbeddingsEnabled() bool { return e.enabled }
func (e *repoIndexFakeEmbedder) EmbeddingModel() string {
	if e.model == "" {
		return "fake-embed-v1"
	}
	return e.model
}
func (e *repoIndexFakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.5, 0.25}
	}
	return out, nil
}

func TestRepoIndexEmbedChunksDegradesInsteadOfFailing(t *testing.T) {
	chunks := []RepoIndexChunk{
		{FilePath: "a.go", Symbol: "A", Content: "func A() {}"},
		{FilePath: "b.go", Content: "package b"},
	}

	// No embedder configured is the DEFAULT deployment, not a broken one: the
	// chunks are still stored, just without vectors.
	if got := (&RepoIndexer{}).embedChunks(context.Background(), chunks); got != nil {
		t.Errorf("an unconfigured embedder produced literals: %v", got)
	}
	off := &repoIndexFakeEmbedder{enabled: false}
	if got := (&RepoIndexer{Embedder: off}).embedChunks(context.Background(), chunks); got != nil || off.calls != 0 {
		t.Errorf("a disabled embedder was called: literals=%v calls=%d", got, off.calls)
	}

	// A failing upstream must not fail the write either: the chunk rows are the
	// durable value, a vector is a ranking improvement on top of them.
	broken := &repoIndexFakeEmbedder{enabled: true, err: errors.New("gateway down")}
	if got := (&RepoIndexer{Embedder: broken}).embedChunks(context.Background(), chunks); got != nil {
		t.Errorf("a failed embedding call produced literals: %v", got)
	}
	if broken.calls != 1 {
		t.Errorf("failing embedder called %d times, want 1", broken.calls)
	}

	// Configured and working: one pgvector literal per chunk, in order.
	ok := &repoIndexFakeEmbedder{enabled: true}
	literals := (&RepoIndexer{Embedder: ok}).embedChunks(context.Background(), chunks)
	if len(literals) != 2 || literals[0] != "[0.5,0.25]" {
		t.Fatalf("literals = %v, want one pgvector literal per chunk", literals)
	}
}

func TestRepoIndexEmbeddingTextLeadsWithPathAndSymbol(t *testing.T) {
	// A query is usually about a name. Embedding the body alone would weigh the
	// language's boilerplate as heavily as the thing the chunk is about.
	got := repoIndexEmbeddingText(RepoIndexChunk{FilePath: "server/claim.go", Symbol: "buildClaim", Content: "func buildClaim() {}"})
	want := "server/claim.go — buildClaim\nfunc buildClaim() {}"
	if got != want {
		t.Errorf("repoIndexEmbeddingText = %q, want %q", got, want)
	}
	got = repoIndexEmbeddingText(RepoIndexChunk{FilePath: "notes.md", Content: "prose"})
	if got != "notes.md\nprose" {
		t.Errorf("symbol-less chunk = %q", got)
	}
}

func TestRepoIndexPruneRefusesEmptyPresentList(t *testing.T) {
	// A walk that produced nothing is a broken checkout far more often than an
	// emptied repository, and obeying it would wipe a working index.
	err := (&RepoIndexer{}).Prune(context.Background(), pgtype.UUID{}, "git@x:a.git", "commit-a", nil)
	if err == nil {
		t.Fatal("Prune accepted an empty present list")
	}
}

// RepoIndexer satisfies the embedder seam with the real client, so a signature
// drift in pkg/llm fails here rather than at wiring time in router.go.
var _ RepoIndexEmbedder = (*llm.Client)(nil)

// A vector is only comparable to a query the same model embedded. The model in
// force is stamped on every chunk and sent with every search; a deployment
// with embeddings turned off names no model at all, so nothing claims to be
// comparable.
func TestRepoIndexerEmbeddingModel(t *testing.T) {
	var none RepoIndexer
	if got := none.embeddingModel(); got.Valid {
		t.Errorf("no embedder must name no model, got %q", got.String)
	}
	off := &RepoIndexer{Embedder: &repoIndexFakeEmbedder{enabled: false, model: "text-embedding-3-small"}}
	if got := off.embeddingModel(); got.Valid {
		t.Errorf("embeddings disabled must name no model, got %q", got.String)
	}
	blank := &RepoIndexer{Embedder: &repoIndexFakeEmbedder{enabled: true, model: "   "}}
	if got := blank.embeddingModel(); got.Valid {
		t.Errorf("a blank model name is no model, got %q", got.String)
	}
	on := &RepoIndexer{Embedder: &repoIndexFakeEmbedder{enabled: true, model: "text-embedding-3-small"}}
	if got := on.embeddingModel(); !got.Valid || got.String != "text-embedding-3-small" {
		t.Errorf("model = %+v, want the configured one", got)
	}
}
