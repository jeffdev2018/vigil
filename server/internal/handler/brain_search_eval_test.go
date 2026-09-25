package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// Brain search reference evaluation (JEF-412). A versioned corpus of notes
// and 50 questions in French, English and Chinese, each naming the notes a
// person would accept as the answer. The test loads the corpus into a fresh
// workspace, asks every question through the same endpoint the palette, the
// Brain page, the CLI and MCP use, and reports recall@5 overall, per language
// and per question kind.
//
// Default runs rank lexically (no embeddings provider) and hold the lexical
// floor. MULTICA_BRAIN_EVAL_REAL_EMBEDDINGS=1 with MULTICA_LLM_API_KEY,
// MULTICA_LLM_BASE_URL and MULTICA_LLM_EMBEDDING_MODEL set embeds the corpus
// through a real provider and holds the hybrid floor; it costs a few cents
// and is never part of CI. MULTICA_BRAIN_EVAL_OUT writes the per-question
// results as JSON.

const (
	brainEvalLexicalMinRecall = 0.85
	brainEvalHybridMinRecall  = 0.85
)

type brainEvalNote struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

type brainEvalQuestion struct {
	ID        string   `json:"id"`
	Lang      string   `json:"lang"`
	Kind      string   `json:"kind"`
	Query     string   `json:"query"`
	Relevant  []string `json:"relevant"`
	Crosslang bool     `json:"crosslang"`
}

type brainEvalResult struct {
	ID     string   `json:"id"`
	Lang   string   `json:"lang"`
	Kind   string   `json:"kind"`
	Query  string   `json:"query"`
	Recall float64  `json:"recall"`
	Top    []string `json:"top"`
	Want   []string `json:"want"`
}

func readBrainEvalJSON(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func TestBrainSearchReferenceRecall(t *testing.T) {
	dir := filepath.Join("..", "service", "testdata", "brain_search_eval")
	var notes []brainEvalNote
	var questions []brainEvalQuestion
	readBrainEvalJSON(t, filepath.Join(dir, "corpus.json"), &notes)
	readBrainEvalJSON(t, filepath.Join(dir, "questions.json"), &questions)
	if len(notes) == 0 || len(questions) == 0 {
		t.Fatal("empty reference set")
	}

	origEmbedder := testHandler.BrainEmbedder
	t.Cleanup(func() { testHandler.BrainEmbedder = origEmbedder })
	real := os.Getenv("MULTICA_BRAIN_EVAL_REAL_EMBEDDINGS") == "1"
	minRecall := brainEvalLexicalMinRecall
	if real {
		cfg := llm.Config{APIKey: os.Getenv("MULTICA_LLM_API_KEY"), BaseURL: os.Getenv("MULTICA_LLM_BASE_URL"), EmbeddingModel: os.Getenv("MULTICA_LLM_EMBEDDING_MODEL")}
		if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.EmbeddingModel == "" {
			t.Fatal("MULTICA_BRAIN_EVAL_REAL_EMBEDDINGS=1 needs MULTICA_LLM_API_KEY, MULTICA_LLM_BASE_URL and MULTICA_LLM_EMBEDDING_MODEL")
		}
		testHandler.BrainEmbedder = service.NewBrainEmbedder(testHandler.Queries, llm.New(cfg))
		minRecall = brainEvalHybridMinRecall
	} else {
		testHandler.BrainEmbedder = nil
	}

	workspaceID := brainWorkspace(t)
	keyByID := map[string]string{}
	for _, n := range notes {
		tags := n.Tags
		if len(tags) > 8 {
			tags = tags[:8]
		}
		created := createNote(t, workspaceID, CreateWorkspaceNoteRequest{Title: n.Title, Content: n.Content, Tags: tags})
		keyByID[created.ID] = n.Key
	}
	if real {
		for i := 0; i < 50; i++ {
			if testHandler.BackfillBrainEmbeddings(context.Background()) == 0 {
				break
			}
		}
	}

	results := make([]brainEvalResult, 0, len(questions))
	for _, q := range questions {
		hits := searchNotes(t, workspaceID, "q="+url.QueryEscape(q.Query)+"&limit=5")
		top := make([]string, 0, len(hits))
		for _, h := range hits {
			top = append(top, keyByID[h.ID])
		}
		found := 0
		for _, want := range q.Relevant {
			for _, got := range top {
				if got == want {
					found++
					break
				}
			}
		}
		results = append(results, brainEvalResult{ID: q.ID, Lang: q.Lang, Kind: q.Kind, Query: q.Query, Recall: float64(found) / float64(len(q.Relevant)), Top: top, Want: q.Relevant})
	}

	mean := func(filter func(brainEvalResult) bool) (float64, int) {
		sum, n := 0.0, 0
		for _, r := range results {
			if filter(r) {
				sum += r.Recall
				n++
			}
		}
		if n == 0 {
			return 0, 0
		}
		return sum / float64(n), n
	}
	overall, _ := mean(func(brainEvalResult) bool { return true })
	mode := "lexical"
	if real {
		mode = "hybrid"
	}
	var report strings.Builder
	fmt.Fprintf(&report, "brain search recall@5 (%s): %.3f over %d questions\n", mode, overall, len(results))
	groups := map[string]bool{}
	for _, r := range results {
		groups["lang="+r.Lang] = true
		groups["kind="+r.Kind] = true
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		field, value, _ := strings.Cut(g, "=")
		m, n := mean(func(r brainEvalResult) bool {
			if field == "lang" {
				return r.Lang == value
			}
			return r.Kind == value
		})
		fmt.Fprintf(&report, "  %-20s %.3f (%d)\n", g, m, n)
	}
	for _, r := range results {
		if r.Recall < 1 {
			fmt.Fprintf(&report, "  miss %s [%s/%s] %q want %v got %v\n", r.ID, r.Lang, r.Kind, r.Query, r.Want, r.Top)
		}
	}
	t.Log(report.String())
	if out := os.Getenv("MULTICA_BRAIN_EVAL_OUT"); out != "" {
		raw, _ := json.MarshalIndent(struct {
			Mode    string            `json:"mode"`
			Overall float64           `json:"overall"`
			Results []brainEvalResult `json:"results"`
		}{mode, overall, results}, "", "  ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Fatalf("write %s: %v", out, err)
		}
	}
	if overall < minRecall {
		t.Errorf("recall@5 %.3f is below the %s floor %.2f", overall, mode, minRecall)
	}
}
