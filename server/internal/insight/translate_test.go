package insight

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubLLM records what it was told and replays scripted answers, so the whole
// translation path can be driven without an upstream.
type stubLLM struct {
	enabled   bool
	answers   []string
	err       error
	systems   []string
	users     []string
	callCount int
}

func (s *stubLLM) Enabled() bool { return s.enabled }

func (s *stubLLM) GenerateJSON(_ context.Context, _, systemPrompt, userPrompt string, _ float64, _ int64) (string, error) {
	s.callCount++
	s.systems = append(s.systems, systemPrompt)
	s.users = append(s.users, userPrompt)
	if s.err != nil {
		return "", s.err
	}
	if len(s.answers) == 0 {
		return "", errors.New("stub ran out of answers")
	}
	answer := s.answers[0]
	s.answers = s.answers[1:]
	return answer, nil
}

func testVocabulary() Vocabulary {
	return Vocabulary{
		Statuses: []VocabEntry{
			{ID: "waiting_on_ops", Name: "Waiting on Ops", Note: "blocked"},
			{ID: "done", Name: "Done", Note: "done"},
		},
		Projects:   []VocabEntry{{ID: "33333333-3333-4333-8333-333333333333", Name: "Payments"}},
		Labels:     []VocabEntry{{ID: "44444444-4444-4444-8444-444444444444", Name: "regression"}},
		IssueTypes: []VocabEntry{{ID: "bug", Name: "Bug"}},
		Properties: []VocabEntry{{ID: "55555555-5555-4555-8555-555555555555", Name: "Severity", Note: "select"}},
	}
}

func TestTranslateReturnsAValidatedDocument(t *testing.T) {
	llm := &stubLLM{
		enabled: true,
		answers: []string{`{"entity":"issue","metric":"count","filters":[{"field":"status","op":"is","values":["waiting_on_ops"]}]}`},
	}
	tr := &Translator{LLM: llm}

	query, err := tr.Translate(context.Background(), "how many issues are waiting on ops", testVocabulary())
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if query.Entity != EntityIssue || query.Metric != MetricCount {
		t.Fatalf("document = %+v", query)
	}
	if llm.callCount != 1 {
		t.Errorf("callCount = %d, want 1", llm.callCount)
	}
}

// The retry contract: exactly one, and the second attempt is told what was
// wrong. A model that keeps guessing costs money and tells the user nothing.
func TestTranslateRetriesOnceThenGivesUp(t *testing.T) {
	llm := &stubLLM{
		enabled: true,
		answers: []string{
			`{"entity":"issue","metric":"median"}`,
			`{"entity":"issue","metric":"count","group_by":["creator"]}`,
		},
	}
	tr := &Translator{LLM: llm}

	_, err := tr.Translate(context.Background(), "how many issues", testVocabulary())
	if !errors.Is(err, ErrUntranslatable) {
		t.Fatalf("err = %v, want ErrUntranslatable", err)
	}
	if llm.callCount != 2 {
		t.Fatalf("callCount = %d, want exactly 2 (one retry)", llm.callCount)
	}
	if !strings.Contains(llm.users[1], "rejected") {
		t.Errorf("the retry did not quote the rejection back: %q", llm.users[1])
	}
}

func TestTranslateRecoversOnTheRetry(t *testing.T) {
	llm := &stubLLM{
		enabled: true,
		answers: []string{
			`{"entity":"issue","metric":"median"}`,
			`{"entity":"issue","metric":"count"}`,
		},
	}
	tr := &Translator{LLM: llm}

	if _, err := tr.Translate(context.Background(), "how many issues", testVocabulary()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if llm.callCount != 2 {
		t.Errorf("callCount = %d, want 2", llm.callCount)
	}
}

func TestTranslateRejectsUnknownDocumentFields(t *testing.T) {
	// A document carrying a field the DSL does not define is a document the
	// model believed in and the compiler would silently ignore; that gap is
	// where a confidently wrong number comes from.
	llm := &stubLLM{
		enabled: true,
		answers: []string{
			`{"entity":"issue","metric":"count","table":"workspace_model_key"}`,
			`{"entity":"issue","metric":"count","table":"workspace_model_key"}`,
		},
	}
	tr := &Translator{LLM: llm}
	if _, err := tr.Translate(context.Background(), "q", testVocabulary()); !errors.Is(err, ErrUntranslatable) {
		t.Fatalf("err = %v, want ErrUntranslatable", err)
	}
}

func TestTranslateWithoutAnLLMIsRefused(t *testing.T) {
	tr := &Translator{LLM: &stubLLM{enabled: false}}
	if _, err := tr.Translate(context.Background(), "q", testVocabulary()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	var nilTranslator *Translator
	if _, err := nilTranslator.Translate(context.Background(), "q", testVocabulary()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil translator err = %v, want ErrNotConfigured", err)
	}
}

func TestTranslateSurfacesTransportErrors(t *testing.T) {
	// A timeout is not an untranslatable question: the user should be told to
	// try again, not to rephrase.
	boom := errors.New("upstream 503")
	tr := &Translator{LLM: &stubLLM{enabled: true, err: boom}}
	_, err := tr.Translate(context.Background(), "q", testVocabulary())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the transport error", err)
	}
	if errors.Is(err, ErrUntranslatable) {
		t.Error("a transport failure was reported as an untranslatable question")
	}
}

// What the model is told. The workspace's own keys must be in the prompt —
// without them a workspace whose blocked status is `waiting_on_ops` gets a
// document naming `blocked`, which compiles and silently counts zero.
func TestSystemPromptCarriesTheWorkspaceVocabulary(t *testing.T) {
	prompt := BuildSystemPrompt(testVocabulary())
	for _, want := range []string{
		"waiting_on_ops",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
		"bug",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt never mentions %q", want)
		}
	}
	// And the grammar it is allowed to emit.
	for _, want := range []string{"issue", "task", "comment", "avg_age_days", "idle_days", "property:<id>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt never mentions %q", want)
		}
	}
}

// The central decision, asserted rather than documented: the model is never
// shown SQL, never asked for SQL, and never told a table name.
func TestSystemPromptNeverMentionsSQL(t *testing.T) {
	prompt := strings.ToLower(BuildSystemPrompt(testVocabulary()))
	for _, forbidden := range []string{"select ", "sql", "from issue", "group by ", "where "} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("prompt contains %q; the model must never see or write SQL", forbidden)
		}
	}
}

func TestSystemPromptTruncatesAHugeCatalogue(t *testing.T) {
	vocab := Vocabulary{}
	for i := 0; i < maxVocabEntries*2; i++ {
		vocab.Statuses = append(vocab.Statuses, VocabEntry{ID: "s", Name: "n"})
	}
	prompt := BuildSystemPrompt(vocab)
	if !strings.Contains(prompt, "and 60 more") {
		t.Errorf("an over-long catalogue was not truncated:\n%s", prompt)
	}
}
