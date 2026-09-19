package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/decisions"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// No database: scoreRunConfidenceByDecision takes the run's text and returns a
// score. The DB-backed tests in run_confidence_test.go cover what the score
// then triggers.

// scoreReply is one decision reply placing the answer on the given level.
func scoreReply(level float64) string {
	body, _ := json.Marshal(map[string]any{
		"model": "typesafe/jev-test",
		"answers": map[string]any{
			"evidence": map[string]any{"type": "score", "score": level, "confidence": 0.9},
		},
	})
	return string(body)
}

// rationaleClient answers the prose call. A nil one stands for a deployment
// with no chat model, where the score must still hold.
type rationaleClient struct {
	reply string
	fail  bool
	calls int
}

func (c *rationaleClient) Enabled() bool        { return true }
func (c *rationaleClient) DefaultModel() string { return "chat-test" }
func (c *rationaleClient) GenerateJSON(ctx context.Context, model, system, user string, temp float64, maxTok int64) (string, error) {
	c.calls++
	if c.fail {
		return "", context.DeadlineExceeded
	}
	return c.reply, nil
}

func decisionTaskService(t *testing.T, reply string, chat RunConfidenceLLM) *TaskService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &TaskService{
		Decisions:     decisions.New(decisions.Config{BaseURL: srv.URL, APIKey: "k"}),
		RunConfidence: chat,
	}
}

// TestLevelMapsToScore pins the conversion. The stored score stays a 0..1
// number because the threshold, the cascade and the review all read it, so the
// level index has to land on the same scale it always used.
func TestLevelMapsToScore(t *testing.T) {
	for _, tc := range []struct{ level, want float64 }{
		{0, 0.0},
		{1, 0.25},
		{2, 0.5},
		{3, 0.75},
		{4, 1.0},
	} {
		chat := &rationaleClient{reply: `{"rationale":"nothing concrete"}`}
		s := decisionTaskService(t, scoreReply(tc.level), chat)
		score, _, _, ok := s.scoreRunConfidenceByDecision(
			context.Background(), "Ship retry logging", "did the thing", "", 0.5)
		if !ok {
			t.Fatalf("level %v: no score", tc.level)
		}
		if score != tc.want {
			t.Errorf("level %v -> score %v, want %v", tc.level, score, tc.want)
		}
	}
}

// TestRequestChangesCapsTheScore moves a rule out of a prompt and into code.
// The previous system prompt asked the model to honour it — "A reviewer verdict
// of request_changes caps the score below 0.5" — which made a fact about the
// change into advice a model could weigh.
func TestRequestChangesCapsTheScore(t *testing.T) {
	chat := &rationaleClient{reply: `{"rationale":"reviewer asked for changes"}`}
	s := decisionTaskService(t, scoreReply(4), chat)
	score, _, _, ok := s.scoreRunConfidenceByDecision(
		context.Background(), "Ship retry logging", "all done", "request_changes", 0.5)
	if !ok {
		t.Fatal("no score")
	}
	if score > requestChangesCap {
		t.Errorf("score %v survived a request_changes verdict", score)
	}
	// Any other verdict leaves the level alone.
	s2 := decisionTaskService(t, scoreReply(4), &rationaleClient{reply: `{"rationale":"x"}`})
	score2, _, _, _ := s2.scoreRunConfidenceByDecision(
		context.Background(), "Ship retry logging", "all done", "approve", 0.5)
	if score2 != 1.0 {
		t.Errorf("an approving verdict changed the score to %v", score2)
	}
}

// TestRationaleOnlyWhenSomeoneWillReadIt is the saving. Above the threshold
// nobody opens the record, so the prose completion is not spent.
func TestRationaleOnlyWhenSomeoneWillReadIt(t *testing.T) {
	// Level 4 -> 1.0, above a 0.5 threshold.
	chat := &rationaleClient{reply: `{"rationale":"should not be asked"}`}
	s := decisionTaskService(t, scoreReply(4), chat)
	_, rationale, _, _ := s.scoreRunConfidenceByDecision(
		context.Background(), "t", "output", "", 0.5)
	if chat.calls != 0 {
		t.Errorf("asked for a rationale nobody reads (%d call(s))", chat.calls)
	}
	if rationale != "" {
		t.Errorf("rationale returned above the threshold: %q", rationale)
	}

	// Level 1 -> 0.25, below it: a person is coming, so the line is written.
	chat2 := &rationaleClient{reply: `{"rationale":"claims done, names nothing"}`}
	s2 := decisionTaskService(t, scoreReply(1), chat2)
	_, rationale2, _, _ := s2.scoreRunConfidenceByDecision(
		context.Background(), "t", "output", "", 0.5)
	if chat2.calls != 1 {
		t.Errorf("below the threshold the rationale was not asked (%d call(s))", chat2.calls)
	}
	if rationale2 != "claims done, names nothing" {
		t.Errorf("rationale = %q", rationale2)
	}
}

// TestScoreSurvivesAProseFailure keeps the guardrail when only the sentence is
// missing. The score decides whether a human is pulled in; losing a line of
// prose must not lose that.
func TestScoreSurvivesAProseFailure(t *testing.T) {
	for name, chat := range map[string]RunConfidenceLLM{
		"prose call fails":     &rationaleClient{fail: true},
		"prose unparseable":    &rationaleClient{reply: "I would rather not"},
		"no chat model at all": nil,
	} {
		s := decisionTaskService(t, scoreReply(0), chat)
		score, rationale, _, ok := s.scoreRunConfidenceByDecision(
			context.Background(), "t", "output", "", 0.5)
		if !ok {
			t.Errorf("%s: the score was dropped with the prose", name)
		}
		if score != 0 {
			t.Errorf("%s: score = %v", name, score)
		}
		if rationale != "" {
			t.Errorf("%s: invented a rationale: %q", name, rationale)
		}
	}
}

// TestNoScoreWithoutADecision keeps the old contract: a score needs a real
// assessment, so a failed decision stores nothing rather than a default.
func TestNoScoreWithoutADecision(t *testing.T) {
	for name, reply := range map[string]string{
		"upstream error": `{"error":{"message":"overloaded"}}`,
		"empty answers":  `{"model":"m","answers":{}}`,
		"wrong question": `{"model":"m","answers":{"other":{"type":"score","score":3}}}`,
		"not a score":    `{"model":"m","answers":{"evidence":{"type":"noul","noul":0.9}}}`,
	} {
		s := decisionTaskService(t, reply, &rationaleClient{reply: `{"rationale":"x"}`})
		if _, _, _, ok := s.scoreRunConfidenceByDecision(
			context.Background(), "t", "output", "", 0.5); ok {
			t.Errorf("%s: returned a score without an assessment", name)
		}
	}
}

// TestRunOutputIsBounded keeps one enormous run output from being sent whole.
func TestRunOutputIsBounded(t *testing.T) {
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = make([]byte, 0)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			sent = append(sent, buf[:n]...)
			if err != nil {
				break
			}
		}
		_, _ = w.Write([]byte(scoreReply(2)))
	}))
	defer srv.Close()
	s := &TaskService{
		Decisions:     decisions.New(decisions.Config{BaseURL: srv.URL, APIKey: "k"}),
		RunConfidence: &rationaleClient{reply: `{"rationale":"x"}`},
	}
	s.scoreRunConfidenceByDecision(context.Background(), "t",
		strings.Repeat("y", 200*1024), "", 0.5)

	if len(sent) > runConfidenceOutputBudget+4096 {
		t.Errorf("request is %d bytes for a %d-byte output budget",
			len(sent), runConfidenceOutputBudget)
	}
}

// compile-time check that the doubles satisfy the seams they stand in for
var (
	_ RunConfidenceLLM = (*rationaleClient)(nil)
	_ RunConfidenceLLM = (*llm.Client)(nil)
)
