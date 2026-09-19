// Package decisions is the server's entry point for typed decisions: a
// question with a closed answer space, answered by a decision model instead of
// by a chat completion.
//
// # Why this is not pkg/llm
//
// A chat model answers in prose, so every caller that needs a machine-readable
// answer has to ask for JSON, hope the model complies, and parse it. That path
// has three failure modes this one does not have: the reply can be malformed
// (pkg/llm's GenerateJSON returns the raw body unvalidated), the number it
// hands back is a figure written in prose rather than a calibrated one, and it
// costs a full completion.
//
// A decision model cannot emit anything outside the schema it was given. The
// answer space is declared in the request, and the reply carries the
// probability distribution over it plus a confidence statistic — which is what
// lets a caller act, ask a human, or fall back, instead of treating every
// answer as equally good.
//
// It is a separate package rather than a method on pkg/llm because it is a
// different endpoint, a different model, and a different wire contract: this
// is not Chat Completions, and the upstream refuses a decision model there
// ("is a decisions model and cannot be used with the chat/completions
// endpoint").
//
// # Scope: the assist layer, same as pkg/llm
//
// This covers decisions the API process asks for on its own behalf. Running an
// agent is a different data path: the daemon executes an AI coding tool under
// that tool's own credentials, and nothing here governs it. Operator-facing
// copy about this layer must not imply otherwise.
//
// # Consumers, and what they send upstream
//
// Keep this list current. TestDocumentedDecisionConsumersAreTheOnlyCallers
// fails until a new call site is reflected here — the test exists, and was
// written with the package, because a list guarded only by a comment is a
// control that is declared and not kept.
//
//   - Auto-triage second read —
//     server/internal/handler/triage_auto_decision.go. Before the triage
//     queue accepts or dismisses an incoming item on its own, sends that
//     item's title, a 4000-byte excerpt of its body, and the titles and
//     outcomes of the resolved items the nearest-neighbour vote matched it
//     against, and asks whether it should become work for this team. The raw
//     delivery payload is never sent: a webhook or email payload carries
//     addresses and customer content that add nothing to that question.
//   - Goal-loop judge — server/internal/service/goal_loop_decision.go. Sends
//     the goal text set on the issue, the issue title, a 6000-rune head+tail
//     excerpt of the description, the acceptance criteria when present
//     (4000 runes), the evidence lines the previous runs of the chain left,
//     and a 4000-rune excerpt of the finishing run's closing status. Asks
//     whether the goal is met and, if not, which blocker applies. No
//     transcript, no code, no comments.
//
// # Failure is the caller's decision, not this package's
//
// Every call can fail: unconfigured, timeout, upstream 429/529, a choice
// outside the declared options. This package reports that plainly and never
// substitutes a guess, because the right reaction differs per caller — a
// router may fall back to its cheapest candidate, while a judge that gates
// spending must stop rather than assume. Callers decide; see each one's
// documented behaviour.
package decisions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned by every call when no endpoint or key is set.
// An unconfigured deployment makes zero upstream requests: the check happens
// before a request is built, which is a contract rather than a side effect
// (TestUnconfiguredClientMakesZeroUpstreamRequests).
var ErrNotConfigured = errors.New("decisions: not configured")

// DefaultTimeout bounds a single call. Decisions are meant to be fast — a
// three-question call measured at ~250ms — so a caller waiting much longer is
// better served by its fallback than by the answer.
const DefaultTimeout = 5 * time.Second

// Config is the deployment's decision endpoint. Empty BaseURL or APIKey
// disables the client.
type Config struct {
	BaseURL    string
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client asks typed questions. Safe for concurrent use; the zero value is a
// disabled client, so a nil-safe Enabled() guard is all a caller needs.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	enabled bool
}

// New builds a client. With no key or no base URL it returns a disabled one
// whose every call fails with ErrNotConfigured before any HTTP request exists.
func New(cfg Config) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	apiKey := strings.TrimSpace(cfg.APIKey)
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   strings.TrimSpace(cfg.Model),
		http:    httpClient,
		enabled: baseURL != "" && apiKey != "",
	}
}

// Enabled reports whether this deployment has a decision endpoint at all.
func (c *Client) Enabled() bool { return c != nil && c.enabled }

// Question is one typed question. Build it with Choice, Score or Noul rather
// than by hand: the wire shape of criteria differs per type — a map for choice
// and noul, an ordered array for score — and getting it wrong is a 400.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Choice asks for one option out of a named set. Each option carries a
// description of when it applies; those descriptions are what the model reads,
// so they do the work a prompt would do elsewhere.
func Choice(instructions string, options map[string]string) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: options}
}

// Score asks for a rating against ordered levels, lowest first. The answer's
// Score is the index, probability-weighted across the levels, and the answer
// carries its own legend mapping index to level.
func Score(instructions string, levels []string) Question {
	return Question{Type: "score", Instructions: instructions, Criteria: levels}
}

// Noul asks a yes/no question and answers with the probability of yes, not a
// boolean: a caller that needs a boolean picks its own threshold, which is the
// point — 0.51 and 0.99 should not collapse into the same "true".
func Noul(instructions, whenTrue, whenFalse string) Question {
	return Question{
		Type:         "noul",
		Instructions: instructions,
		Criteria:     map[string]string{"true": whenTrue, "false": whenFalse},
	}
}

// Answer is one question's answer. Which fields are set follows the question
// type; use the accessors rather than reading them raw.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	ScoreValue    float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Usage is what the call cost, as the upstream reports it.
type Usage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// Result is a whole call's reply.
type Result struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
	ID      string            `json:"id"`
}

// ChoiceIn returns the named question's chosen option and its confidence. It
// reports ok=false when the question is missing, when the answer is not a
// choice, or when the chosen option is not one of `allowed` — an answer
// outside the declared space is a bug upstream, never a value to act on.
func (r Result) ChoiceIn(name string, allowed map[string]string) (choice string, confidence float64, ok bool) {
	a, present := r.Answers[name]
	if !present || a.Type != "choice" || a.Choice == "" {
		return "", 0, false
	}
	if _, valid := allowed[a.Choice]; !valid {
		return "", 0, false
	}
	return a.Choice, a.Confidence, true
}

// NoulIn returns the named question's probability of yes.
func (r Result) NoulIn(name string) (probability float64, ok bool) {
	a, present := r.Answers[name]
	if !present || a.Type != "noul" {
		return 0, false
	}
	return a.Noul, true
}

// ScoreIn returns the named question's score and confidence.
func (r Result) ScoreIn(name string) (score, confidence float64, ok bool) {
	a, present := r.Answers[name]
	if !present || a.Type != "score" {
		return 0, 0, false
	}
	return a.ScoreValue, a.Confidence, true
}

type request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Ask sends one state and every question in a single call — that is the whole
// economy of this endpoint, so a caller with several questions about the same
// state asks them together rather than in a loop.
//
// state is marshalled as-is: a string for plain text, or a struct/map when the
// shape matters (a record, a status, a short list). Send the least that answers
// the question: this is a third party, and the caller's own data doctrine
// applies here exactly as it does to pkg/llm.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (Result, error) {
	if !c.Enabled() {
		return Result{}, ErrNotConfigured
	}
	if len(questions) == 0 {
		return Result{}, errors.New("decisions: no questions asked")
	}
	body, err := json.Marshal(request{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return Result{}, fmt.Errorf("decisions: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("decisions: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("decisions: call: %w", err)
	}
	defer resp.Body.Close()

	// Bounded read: a decision reply is a few hundred bytes, so anything
	// larger is a wrong endpoint or an error page, not an answer.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("decisions: read reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("decisions: upstream %d: %s",
			resp.StatusCode, firstLine(raw))
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("decisions: decode reply: %w", err)
	}
	if len(out.Answers) == 0 {
		return Result{}, errors.New("decisions: reply carried no answers")
	}
	return out, nil
}

// firstLine keeps an upstream error readable in a log line without spilling a
// whole error page into it.
func firstLine(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 300
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
