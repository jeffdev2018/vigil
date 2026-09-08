package insight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// LLM is the one-method seam the translator needs, satisfied by *llm.Client.
// It is an interface so the whole translation path — prompt, parsing,
// validation, retry — is testable without an HTTP upstream, the same way
// ChatQuickActionsLLM is injected into TaskService.
type LLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// ErrNotConfigured means the deployment has no assist-layer LLM. Asking a
// question in plain language is the only thing that needs one; a pinned widget
// refreshes through /run and keeps working.
var ErrNotConfigured = errors.New("insight: no LLM configured")

// ErrUntranslatable means the model answered twice and neither answer
// compiled. The question is returned to the user unchanged so they can rephrase
// it — the UI must never clear the box on this error.
var ErrUntranslatable = errors.New("insight: could not turn this question into a query")

const (
	translateTemperature        = 0
	translateMaxCompletionToken = 900
)

// VocabEntry is one thing the model is allowed to name: a status key, a
// project, a label, a work item type, or a custom property definition.
type VocabEntry struct {
	// ID is what the document must carry — a status key, a UUID for the rest.
	ID string
	// Name is the human word the question will actually use.
	Name string
	// Note carries the extra qualifier that disambiguates (a status category,
	// a property type). Optional.
	Note string
}

// Vocabulary is the workspace-specific half of the prompt. Without it the model
// invents keys: a workspace whose "blocked" status is called `waiting_on_ops`
// gets a document naming `blocked`, which compiles and silently counts zero.
type Vocabulary struct {
	Statuses   []VocabEntry
	Projects   []VocabEntry
	Labels     []VocabEntry
	IssueTypes []VocabEntry
	Properties []VocabEntry
}

// Translator turns a question into a validated document. It never sees SQL and
// never emits SQL: its whole output surface is the Query struct.
type Translator struct {
	LLM   LLM
	Model string
}

// Translate returns a document that has already passed Validate. A document
// that fails validation is retried exactly once, with the compiler's own
// rejection quoted back to the model; a second failure gives up rather than
// looping on a question the vocabulary cannot express.
func (t *Translator) Translate(ctx context.Context, question string, vocab Vocabulary) (*Query, error) {
	if t == nil || t.LLM == nil || !t.LLM.Enabled() {
		return nil, ErrNotConfigured
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, ErrUntranslatable
	}

	system := BuildSystemPrompt(vocab)
	user := question
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := t.LLM.GenerateJSON(ctx, t.Model, system, user, translateTemperature, translateMaxCompletionToken)
		if err != nil {
			// A transport failure is not an untranslatable question; surface
			// it so the handler can say "try again" rather than "rephrase".
			return nil, err
		}
		query, verr := DecodeQuery([]byte(raw))
		if verr == nil {
			return query, nil
		}
		lastErr = verr
		user = fmt.Sprintf(
			"%s\n\nYour previous answer was rejected: %s (%s). Emit a corrected JSON document. Do not explain.",
			question, verr.Reason, verr.Field,
		)
	}
	return nil, fmt.Errorf("%w: %v", ErrUntranslatable, lastErr)
}

// DecodeQuery parses a document and validates it. Unknown JSON members are
// rejected rather than ignored: a document carrying a field the DSL does not
// define is a document its author believed in and the compiler would silently
// drop, and the gap between those two beliefs is exactly where a confidently
// wrong number comes from. A forged `workspace_id` is the case that matters —
// ignoring it would answer for the caller's own workspace while the caller
// believed it had named another, which is a silent success, not a refusal.
//
// Every entry point uses this: the translator on the model's reply, and the
// handler on a document posted to /run or pinned as a widget.
func DecodeQuery(raw []byte) (*Query, *Error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, invalid("query", "empty response")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var query Query
	if err := decoder.Decode(&query); err != nil {
		return nil, invalid("query", "response is not a valid insight document: "+err.Error())
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	return &query, nil
}

// BuildSystemPrompt renders the whole instruction set. It is exported so a
// test can assert what the model is actually told — in particular that the
// workspace's own status keys are in it, and that the word SQL never is.
//
// The grammar is generated from the same maps the compiler reads, so a
// vocabulary the compiler does not accept cannot be advertised here.
func BuildSystemPrompt(vocab Vocabulary) string {
	var b strings.Builder
	b.WriteString(`You turn a question about a team's work into ONE JSON document.
You never write a database query, and nothing you emit is executed as text: a
server compiles your document against a fixed vocabulary and rejects anything
outside it.

Answer with the JSON document and nothing else.

Document shape:
{
  "entity":      one of the entities below,
  "metric":      one of that entity's metrics,
  "group_by":    up to 2 dimensions, [] for a single figure,
  "filters":     [{"field": ..., "op": ..., "values": [...]}],
  "time_range":  {"field": ..., "last_days": N} or {"field": ..., "start": RFC3339, "end": RFC3339},
  "granularity": "day" | "week" | "month", only meaningful with the "time" dimension,
  "limit":       1..200
}

Operators: "is", "is_not", "in", "not_in" take one or more values;
"gt", "gte", "lt", "lte" take exactly one NUMBER and apply only to age_days and
idle_days; "is_empty" and "is_not_empty" take no value.

age_days counts from creation. idle_days counts since the row last moved — it
is the closest this data gets to "has been sitting in this state for N days",
and it is not the same thing. Prefer last_days over absolute dates.

`)
	for _, entity := range Entities() {
		fmt.Fprintf(&b, "Entity %q\n  metrics: %s\n  group_by: %s\n  filter fields: %s\n\n",
			entity,
			strings.Join(MetricsFor(entity), ", "),
			strings.Join(DimensionsFor(entity), ", "),
			strings.Join(FilterFieldsFor(entity), ", "),
		)
	}

	b.WriteString("This workspace's vocabulary. Use these exact values; never invent one.\n")
	writeVocab(&b, "Statuses (use the key)", vocab.Statuses)
	writeVocab(&b, "Projects (use the id)", vocab.Projects)
	writeVocab(&b, "Labels (use the id)", vocab.Labels)
	writeVocab(&b, "Work item types (use the key)", vocab.IssueTypes)
	writeVocab(&b, "Custom properties (address as property:<id>)", vocab.Properties)
	b.WriteString(`
If the question cannot be expressed with the vocabulary above, answer with the
closest document you can and set "limit" as usual — do not invent a field, an
operator or a value.
`)
	return b.String()
}

// maxVocabEntries caps each list so one workspace's catalogue cannot crowd the
// grammar out of the context window. The lists are ordered by the caller, so
// the truncation drops the least prominent entries.
const maxVocabEntries = 60

func writeVocab(b *strings.Builder, title string, entries []VocabEntry) {
	fmt.Fprintf(b, "\n%s:\n", title)
	if len(entries) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	truncated := entries
	if len(truncated) > maxVocabEntries {
		truncated = truncated[:maxVocabEntries]
	}
	for _, e := range truncated {
		line := "  " + e.ID
		if e.Name != "" {
			line += " = " + e.Name
		}
		if e.Note != "" {
			line += " (" + e.Note + ")"
		}
		b.WriteString(line + "\n")
	}
	if len(entries) > len(truncated) {
		fmt.Fprintf(b, "  … and %d more\n", len(entries)-len(truncated))
	}
}
