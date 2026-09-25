package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/decisions"
)

// A run's confidence score decides whether a person is pulled in: below the
// workspace threshold, the cascade tries a stronger runtime and, failing that,
// files a review for the managers. Until now that number came from a chat
// model asked to write `{"score":0.0,"rationale":"..."}` about a run — and the
// file says out loud what is wrong with that:
//
//   - The judge may be the model that produced the run. `JudgeIndependence`
//     exists to record `self` when it is, with the comment "This score decides
//     whether a human is pulled in, and it had no such check". The record was
//     honest about a bias it could not remove.
//   - The score is a figure written in prose. Nothing about "0.8" in a
//     sentence relates it to how often such a run is actually right.
//   - `parseConfidenceScore` can fail, and then nothing is stored — so the run
//     that produced an unreadable reply is also the run with no guardrail.
//
// A decision model answers the same question on ordered levels and returns a
// probability distribution over them. Three properties follow, and they are
// the reason this path exists:
//
//   - It cannot be the producer. A decision model does not run agents, so the
//     judge is independent by construction rather than by luck of the model
//     names differing — including when the producing model was never recorded,
//     where the old comparison could only say `unknown`.
//   - The score comes from the distribution, not from prose.
//   - It cannot reply outside the schema, so the "no score at all" outcome
//     stops being a parse problem.
//
// The rationale is the one thing a decision model cannot produce: it has no
// free text. So it is fetched from the chat model, and only when the score
// lands below the threshold — which is exactly when a person will read it.
// Above the threshold nobody opens the record, so the call is not made, and
// the common case now costs one decision instead of one completion.

// runConfidenceLevels are the ordered levels the decision model scores
// against, lowest first. They are written as what the closing evidence looks
// like, because that is what the previous prompt asked for in prose: "a run
// that claims success but shows no concrete outcome scores low".
//
// The stored score stays a 0..1 number, so the level index is divided by the
// last index. Five levels put the midpoint at 0.5 and give the two failing
// shapes — no evidence at all, and a claim without evidence — their own step
// instead of collapsing them.
var runConfidenceLevels = []string{
	"nothing was done: no recorded action and nothing concrete named, or the run reports failing",
	"the run claims the work is done, but no action was recorded and nothing concrete is named",
	"something was done, but it does not cover what the issue asked",
	"the recorded actions cover most of what the issue asked, with a gap or an unverified part",
	"the recorded actions cover what the issue asked",
}

const runConfidenceInstructions = "How much evidence is there that this completed run addressed the issue? The actions the server recorded are facts and outrank the run's own words, which are a claim: a recorded action that accomplishes the issue counts even when the run describes it poorly. Weigh evidence, not tone. Evidence takes the shape of the work — a comment posted, an issue updated, a note written, a file changed, a test run, a pull request opened."

// requestChangesCap mirrors the rule the previous prompt stated in words: "A
// reviewer verdict of request_changes caps the score below 0.5." A cap belongs
// in code rather than in an instruction — an instruction is advice the model
// may weigh, while the reviewer's verdict is a fact about the change.
const requestChangesCap = 0.49

// scoreRunConfidenceByDecision returns the score and rationale for a finished
// run, or ok=false when the decision could not be had — in which case the
// caller stores nothing, exactly as it did when the chat model failed.
func (s *TaskService) scoreRunConfidenceByDecision(ctx context.Context, issueTitle, output, reviewVerdict string, receipts []runReceipt, threshold float64) (score float64, rationale string, judgeModel string, ok bool) {
	state := map[string]any{
		"issue_title":      issueTitle,
		"run_final_output": nativeHeadTail(output, runConfidenceOutputBudget),
	}
	// The server's own record of what the run did. Without it the only
	// evidence is the run's prose, and a run that did the work but described
	// it badly scored as one that did nothing.
	if len(receipts) > 0 {
		state["actions_the_server_recorded_for_this_run"] = receipts
	}
	if v := strings.TrimSpace(reviewVerdict); v != "" {
		state["independent_reviewer_verdict"] = v
	}

	res, err := s.Decisions.Ask(ctx, state, map[string]decisions.Question{
		"evidence": decisions.Score(runConfidenceInstructions, runConfidenceLevels),
	})
	if err != nil {
		slog.Warn("run confidence: decision unavailable, storing nothing", "error", err)
		return 0, "", "", false
	}
	level, confidence, answered := res.ScoreIn("evidence")
	if !answered {
		slog.Warn("run confidence: decision carried no score, storing nothing")
		return 0, "", "", false
	}

	score = level / float64(len(runConfidenceLevels)-1)
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	if strings.EqualFold(strings.TrimSpace(reviewVerdict), "request_changes") && score > requestChangesCap {
		score = requestChangesCap
	}

	slog.Debug("run confidence by decision",
		"level", level, "score", score, "level_confidence", confidence,
		"cost_usd", res.Usage.Cost)

	// The rationale is only read when someone is pulled in. Asking for it
	// above the threshold would spend a completion on a line nobody opens.
	return score, s.runConfidenceRationale(ctx, score, threshold, issueTitle, output, reviewVerdict), res.Model, true
}

// runConfidenceRationalePrompt writes the line a reviewer reads. It judges
// nothing: the score is given to it.
const runConfidenceRationalePrompt = `You explain, in one sentence, why a completed run of an AI coding agent scored low on delivery confidence. The score is given to you and you must not dispute it.

Write for the person who is about to review that delivery. Name what the run did and did not do, using the recorded actions when there are any and the run's own words otherwise. Work takes many shapes: a comment posted, an issue updated, a note written, a file changed, a test run. No apology, no praise, no advice.

Reply with a JSON object of exactly this shape and nothing else: {"rationale": "one sentence, at most 280 characters"}. Never include secrets, credentials, tokens, file contents or personal data.`

// runConfidenceRationale returns the human-facing line for a below-threshold
// score, and "" above it. A failure here is not an error: the score is already
// decided, and a missing sentence must not cost the run its guardrail.
func (s *TaskService) runConfidenceRationale(ctx context.Context, score, threshold float64, issueTitle, output, reviewVerdict string) string {
	// Above the threshold nobody opens the record, so the sentence would be
	// written for no reader.
	if score >= threshold {
		return ""
	}
	if s.RunConfidence == nil || !s.RunConfidence.Enabled() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Issue: %s\n", nativeDataFence("issue title", issueTitle))
	fmt.Fprintf(&b, "Confidence score already decided: %.2f out of 1.\n", score)
	if v := strings.TrimSpace(reviewVerdict); v != "" {
		fmt.Fprintf(&b, "An independent reviewer returned: %s\n", v)
	}
	b.WriteString("Final output of the run:\n" +
		nativeDataFence("run output", nativeHeadTail(output, runConfidenceOutputBudget)))

	raw, err := s.RunConfidence.GenerateJSON(ctx, "", runConfidenceRationalePrompt, b.String(), 0.2, 256)
	if err != nil {
		slog.Warn("run confidence: rationale unavailable", "error", err)
		return ""
	}
	var parsed struct {
		Rationale string `json:"rationale"`
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start || json.Unmarshal([]byte(raw[start:end+1]), &parsed) != nil {
		// A missing sentence is cosmetic; the score already stands.
		return ""
	}
	rationale := strings.TrimSpace(parsed.Rationale)
	if utf8.RuneCountInString(rationale) > runConfidenceRationaleMaxRunes {
		rationale = string([]rune(rationale)[:runConfidenceRationaleMaxRunes])
	}
	return rationale
}
