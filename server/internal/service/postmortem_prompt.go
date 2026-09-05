package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/llm"
)

// postmortemSystemPrompt is the stable instruction set for the drafting pass.
// The word "JSON" must stay: response_format=json_object is rejected upstream
// without it (see llm.Client.GenerateJSON).
const postmortemSystemPrompt = `You write a concise postmortem for an AI coding agent run that FAILED.

A postmortem helps the team understand what went wrong and prevent it next time. It is factual and blameless.

You return four fields:
- "summary": one or two sentences stating that the run failed and on what, with the failure reason.
- "root_cause": one to three sentences on the most likely root cause, grounded in the failure reason, error, and transcript. Do not speculate beyond the evidence.
- "impact": one or two sentences on what was NOT delivered because the run failed.
- "preventive_rules": an array of 1 to 5 short, actionable rules that would prevent or catch this failure earlier. Each is a single sentence, at most 500 characters, written as an instruction ("Split large tasks into smaller sub-tasks.").

Rules:
- NEVER include secrets, credentials, tokens, or file contents.
- If the evidence is thin, say so plainly in root_cause rather than inventing causes.
- Keep everything short and concrete.

Output JSON only, exactly this shape:
{"summary":"...","root_cause":"...","impact":"...","preventive_rules":["..."]}
No prose, no markdown, no code fences.`

// postmortemDraft is the parsed LLM output before storage.
type postmortemDraft struct {
	Summary   string
	RootCause string
	Impact    string
	Rules     []string
}

// renderPostmortemPrompt builds the per-call user message: the issue the run
// worked on, the classified failure, the last error, retry context, and a
// bounded transcript tail.
func renderPostmortemPrompt(trigger, issueTitle, failureReason, errMsg string, costTicks int64, attempt, maxAttempts int32, transcript string) string {
	var b strings.Builder
	if strings.TrimSpace(issueTitle) != "" {
		fmt.Fprintf(&b, "The run worked on the issue titled: %s\n\n", issueTitle)
	}
	if trigger == "costly" {
		// No failure to classify: the run succeeded and the question is the
		// bill, so say that plainly instead of asking for a root cause that
		// does not exist.
		fmt.Fprintf(&b, "OUTCOME: the run COMPLETED SUCCESSFULLY but cost %s, over the workspace's per-run cost threshold.\n", formatUsdTicks(costTicks))
		fmt.Fprintf(&b, "ATTEMPT: %d of %d\n", attempt, maxAttempts)
		b.WriteString("\nTRANSCRIPT TAIL (what the agent spent the run doing):\n")
		if strings.TrimSpace(transcript) == "" {
			b.WriteString("(no transcript captured)\n")
		} else {
			b.WriteString(transcript)
			b.WriteString("\n")
		}
		b.WriteString("\nWrite the postmortem for this expensive run: what it spent the money on, and what would make the next one cheaper. There is no failure to explain.")
		return b.String()
	}
	fmt.Fprintf(&b, "FAILURE REASON (classified): %s\n", orNone(failureReason))
	fmt.Fprintf(&b, "ATTEMPT: %d of %d\n", attempt, maxAttempts)
	if strings.TrimSpace(errMsg) != "" {
		fmt.Fprintf(&b, "LAST ERROR:\n%s\n", truncateRunes(errMsg, 600))
	}
	b.WriteString("\nTRANSCRIPT TAIL (what the agent was doing when it failed):\n")
	if strings.TrimSpace(transcript) == "" {
		b.WriteString("(no transcript captured)\n")
	} else {
		b.WriteString(transcript)
		b.WriteString("\n")
	}
	b.WriteString("\nWrite the postmortem for this failed run.")
	return b.String()
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(unclassified)"
	}
	return s
}

// parsePostmortem decodes the model's reply. Anything that is not the expected
// shape yields an error — a malformed pass falls back to the scaffold.
func parsePostmortem(raw string) (postmortemDraft, error) {
	var decoded struct {
		Summary         string   `json:"summary"`
		RootCause       string   `json:"root_cause"`
		Impact          string   `json:"impact"`
		PreventiveRules []string `json:"preventive_rules"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return postmortemDraft{}, err
	}

	rules := make([]string, 0, len(decoded.PreventiveRules))
	for _, r := range decoded.PreventiveRules {
		if len(rules) >= postmortemMaxRules {
			break
		}
		trimmed := strings.TrimSpace(r)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > postmortemRuleMaxRunes {
			continue
		}
		rules = append(rules, trimmed)
	}

	return postmortemDraft{
		Summary:   strings.TrimSpace(decoded.Summary),
		RootCause: strings.TrimSpace(decoded.RootCause),
		Impact:    strings.TrimSpace(decoded.Impact),
		Rules:     rules,
	}, nil
}

// compile-time guard: the production wiring hands this seam the shared
// *llm.Client; drift between the two interfaces must fail here, not at a
// runtime type assertion.
var _ PostmortemLLM = (*llm.Client)(nil)
