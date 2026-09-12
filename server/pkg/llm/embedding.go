package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	openai "github.com/openai/openai-go/v3"
)

// ErrEmbeddingsNotConfigured is returned by Embed when this deployment has no
// embeddings model configured (MULTICA_LLM_EMBEDDING_MODEL). It is distinct
// from ErrNotConfigured on purpose: a deployment can have a working chat
// upstream and still not want an embeddings bill, and the caller's fallback
// differs — the shared repo index (K47) stays lexical rather than turning off.
var ErrEmbeddingsNotConfigured = errors.New("llm: no embedding model configured")

// EmbeddingDimensions is the width every pgvector column in this codebase is
// declared with (repo_index_chunk.embedding, workspace_note_passage.embedding)
// and the largest width the hnsw index accepts is 2000. The request asks the
// upstream for exactly this width; a model that ignores the parameter and
// answers wider (gemini-embedding answers 3072 by default) is cut down to it
// and re-normalised — the Matryoshka truncation those models are trained for.
const EmbeddingDimensions = 1536

var wideEmbeddingWarned sync.Map

// fitEmbedding returns vec at EmbeddingDimensions: shorter vectors are kept
// as they are (the insert fails loudly, which is right: padding would fake a
// vector), wider ones are truncated and re-normalised to unit length.
func fitEmbedding(model string, vec []float32) []float32 {
	if len(vec) <= EmbeddingDimensions {
		return vec
	}
	if _, seen := wideEmbeddingWarned.LoadOrStore(model, true); !seen {
		slog.Warn("llm: embedding model ignores the dimensions parameter; truncating", "model", model, "got", len(vec), "want", EmbeddingDimensions)
	}
	cut := vec[:EmbeddingDimensions]
	var norm float64
	for _, v := range cut {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return cut
	}
	out := make([]float32, EmbeddingDimensions)
	for i, v := range cut {
		out[i] = float32(float64(v) / norm)
	}
	return out
}

// EmbeddingModel returns the configured embeddings model, or "" when none is
// set. Callers use it to record which model a stored vector came from.
func (c *Client) EmbeddingModel() string {
	if c == nil {
		return ""
	}
	return c.embeddingModel
}

// EmbeddingsEnabled reports whether Embed can do any work: the client needs
// credentials AND an explicit embeddings model. There is deliberately no
// built-in fallback model here, unlike chat: silently embedding against a
// guessed model would produce vectors of an unknown width, and a vector of the
// wrong width is worse than no vector — it ranks confidently and wrongly.
func (c *Client) EmbeddingsEnabled() bool {
	return c.Enabled() && c.EmbeddingModel() != ""
}

// Embed turns texts into vectors through the upstream's OpenAI-compatible
// /embeddings endpoint, preserving input order.
//
// The upstream is asked to return `float` encoding explicitly rather than
// leaving the default to the gateway, and the response is checked to carry
// exactly one vector per input: a gateway that silently drops or reorders
// entries would otherwise attach one chunk's vector to another chunk's text,
// which no downstream check could catch.
//
// An empty texts slice is a no-op that makes no upstream request.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	model := c.EmbeddingModel()
	if model == "" {
		return nil, ErrEmbeddingsNotConfigured
	}
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()

	resp, err := c.sdk.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input:          openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model:          model,
		Dimensions:     openai.Int(EmbeddingDimensions),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("llm: embeddings upstream returned %d vectors for %d inputs", len(resp.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for _, item := range resp.Data {
		if item.Index < 0 || int(item.Index) >= len(out) {
			return nil, fmt.Errorf("llm: embeddings upstream returned out-of-range index %d", item.Index)
		}
		if out[item.Index] != nil {
			return nil, fmt.Errorf("llm: embeddings upstream returned index %d twice", item.Index)
		}
		vec := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vec[i] = float32(v)
		}
		out[item.Index] = fitEmbedding(model, vec)
	}
	for i, vec := range out {
		if vec == nil {
			return nil, fmt.Errorf("llm: embeddings upstream returned no vector for input %d", i)
		}
	}
	return out, nil
}

// VectorLiteral renders a vector in pgvector's own text form, `[a,b,c]`.
// Postgres parses that into a `vector` on cast, which is why the repo index
// stores embeddings through a text parameter and needs no pgvector Go type.
// Returns "" for an empty vector so callers can pass NULL uniformly.
func VectorLiteral(vec []float32) string {
	if len(vec) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", v)
	}
	b.WriteByte(']')
	return b.String()
}
