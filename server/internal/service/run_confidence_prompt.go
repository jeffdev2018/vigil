package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// runConfidenceSystemPrompt is the stable instruction set for the pass. The
// word "JSON" must stay: response_format=json_object is rejected upstream
// without it (see llm.Client.GenerateJSON).
const runConfidenceSystemPrompt = `You score an AI coding agent's COMPLETED run for confidence that its delivery is correct and complete.

You are given the issue the run worked on, the run's final output, and — when an independent reviewer already looked at the change — that review's verdict.

Rules:
- "score": your confidence, from 0.0 to 1.0, that the delivery correctly and completely addresses the issue. 1.0 means certain it is right; 0.0 means certain it is wrong or missing.
- Weigh evidence, not tone: a run that claims success but shows no concrete outcome (files changed, tests run, PR opened) scores low. A reviewer verdict of "request_changes" caps the score below 0.5.
- "rationale": one sentence (280 characters max) explaining the score, written for the human who may review the delivery.
- NEVER include secrets, credentials, tokens, file contents, or personal data in the rationale.

Output JSON only, exactly this shape:
{"score":0.0,"rationale":"..."}
No prose, no markdown fences, nothing outside the JSON object.`

// renderRunConfidencePrompt builds the per-call user message: the issue the
// run worked on, its bounded final output (head + tail kept), and the
// cross-review verdict when one exists.
func renderRunConfidencePrompt(issueTitle, output, verdict string) string {
	var b strings.Builder
	if strings.TrimSpace(issueTitle) != "" {
		fmt.Fprintf(&b, "The run worked on the issue titled: %s\n\n", issueTitle)
	}
	b.WriteString("FINAL OUTPUT OF THE COMPLETED RUN:\n")
	b.WriteString(truncateRunConfidenceOutput(output))
	if v := strings.TrimSpace(verdict); v != "" {
		fmt.Fprintf(&b, "\n\nAn independent reviewer of this change returned the verdict: %s", v)
	}
	b.WriteString("\n\nScore your confidence that this delivery is correct and complete.")
	return b.String()
}

// truncateRunConfidenceOutput keeps both ends of a long output: the head says
// what the run set out to do, the tail says what it concluded.
func truncateRunConfidenceOutput(output string) string {
	runes := []rune(output)
	if len(runes) <= runConfidenceOutputBudget {
		return output
	}
	head := string(runes[:runConfidenceOutputBudget/2])
	tail := string(runes[len(runes)-runConfidenceOutputBudget/2:])
	return head + "\n…[truncated]…\n" + tail
}

// parseConfidenceScore decodes the model's reply, clamping the score into
// [0,1]. An out-of-range or malformed reply is an error, which the caller
// treats as "nothing to store".
func parseConfidenceScore(raw string) (score float64, rationale string, err error) {
	var decoded struct {
		Score     *float64 `json:"score"`
		Rationale string   `json:"rationale"`
	}
	if uerr := json.Unmarshal([]byte(raw), &decoded); uerr != nil {
		return 0, "", uerr
	}
	if decoded.Score == nil || math.IsNaN(*decoded.Score) || math.IsInf(*decoded.Score, 0) {
		return 0, "", fmt.Errorf("missing or invalid score")
	}
	s := math.Min(1, math.Max(0, *decoded.Score))
	return s, strings.TrimSpace(decoded.Rationale), nil
}
