package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTransport answers every request with one canned body.
type stubTransport struct {
	status   int
	body     string
	requests int
	lastPath string
	lastBody map[string]any
}

func (s *stubTransport) Do(req *http.Request) (*http.Response, error) {
	s.requests++
	s.lastPath = req.URL.Path
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &s.lastBody)
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    req,
	}, nil
}

func TestEmbedRefusesWithoutConfiguration(t *testing.T) {
	// Two distinct refusals: no credentials at all, and credentials without an
	// embeddings model. They differ because the caller's fallback differs — the
	// second is a deployment that chose not to pay for embeddings, and the repo
	// index must stay lexical rather than turn off.
	transport := &stubTransport{}
	disabled := New(Config{HTTPClient: transport})
	if _, err := disabled.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("disabled client: got %v, want ErrNotConfigured", err)
	}

	noModel := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", HTTPClient: transport})
	if noModel.EmbeddingsEnabled() {
		t.Error("a client with no embedding model reports embeddings enabled")
	}
	if _, err := noModel.Embed(context.Background(), []string{"x"}); !errors.Is(err, ErrEmbeddingsNotConfigured) {
		t.Errorf("no embedding model: got %v, want ErrEmbeddingsNotConfigured", err)
	}
	if transport.requests != 0 {
		t.Errorf("an unconfigured Embed made %d upstream request(s)", transport.requests)
	}
}

func TestEmbedPreservesInputOrder(t *testing.T) {
	// The upstream is free to return the vectors in any order; a wrong pairing
	// would silently attach one chunk's meaning to another chunk's text, which
	// nothing downstream could detect.
	transport := &stubTransport{body: `{"model":"m","object":"list","usage":{"prompt_tokens":1,"total_tokens":1},
		"data":[{"object":"embedding","index":1,"embedding":[0.5,0.5]},
		        {"object":"embedding","index":0,"embedding":[0.25,0.75]}]}`}
	c := New(Config{APIKey: "k", BaseURL: "https://gateway.test/v1", EmbeddingModel: "text-embedding-3-small", HTTPClient: transport})
	if !c.EmbeddingsEnabled() {
		t.Fatal("a configured client reports embeddings disabled")
	}
	vectors, err := c.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 2 || vectors[0][0] != 0.25 || vectors[1][0] != 0.5 {
		t.Fatalf("vectors were not reordered by index: %v", vectors)
	}
	if !strings.HasSuffix(transport.lastPath, "/embeddings") {
		t.Errorf("Embed hit %q, want the /embeddings surface", transport.lastPath)
	}
	if transport.lastBody["model"] != "text-embedding-3-small" {
		t.Errorf("request model = %v, want the configured embedding model", transport.lastBody["model"])
	}
}

func TestEmbedRejectsMismatchedVectorCount(t *testing.T) {
	// A gateway that drops an entry must be an error, not a short slice the
	// caller silently zips against the wrong inputs.
	transport := &stubTransport{body: `{"model":"m","object":"list","usage":{"prompt_tokens":1,"total_tokens":1},
		"data":[{"object":"embedding","index":0,"embedding":[0.5]}]}`}
	c := New(Config{APIKey: "k", BaseURL: "https://gateway.test/v1", EmbeddingModel: "m", HTTPClient: transport})
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("a short response was accepted")
	}
}

func TestEmbedEmptyInputMakesNoRequest(t *testing.T) {
	transport := &stubTransport{}
	c := New(Config{APIKey: "k", BaseURL: "https://gateway.test/v1", EmbeddingModel: "m", HTTPClient: transport})
	vectors, err := c.Embed(context.Background(), nil)
	if err != nil || vectors != nil {
		t.Fatalf("Embed(nil) = %v, %v; want nil, nil", vectors, err)
	}
	if transport.requests != 0 {
		t.Errorf("Embed(nil) made %d upstream request(s)", transport.requests)
	}
}

func TestVectorLiteral(t *testing.T) {
	// pgvector's own text form is the wire format the repo index stores through,
	// so this has to be exactly parseable by `::vector`.
	if got := VectorLiteral([]float32{0.25, -1, 2}); got != "[0.25,-1,2]" {
		t.Errorf("VectorLiteral = %q", got)
	}
	if got := VectorLiteral(nil); got != "" {
		t.Errorf("VectorLiteral(nil) = %q, want empty so callers can pass NULL", got)
	}
}
