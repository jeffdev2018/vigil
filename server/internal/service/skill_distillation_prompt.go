package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// skillDistillationSystemPrompt is the stable instruction set for the pass.
// The word "JSON" must stay: response_format=json_object is rejected upstream
// without it (see llm.Client.GenerateJSON).
const skillDistillationSystemPrompt = `You distill a reusable skill from an AI coding agent's SUCCESSFUL run.

A skill is a generalizable technique the run demonstrated — an approach, a sequence of steps, a tool usage pattern, or a convention — that the same agent could apply to a DIFFERENT future task. It is not a summary of what this specific task did.

Rules:
- Return a skill ONLY if the run demonstrated a genuinely reusable technique. For an ordinary run with nothing generalizable, return {"skill": null}. Returning null is a good, expected outcome.
- "name": a short kebab-case skill name (2-6 words), e.g. "rebase-with-conflict-checks".
- "description": one sentence on when to use this skill.
- "body": the skill as concise markdown instructions written for the agent reading it on a future task — when it applies and the concrete steps or conventions to follow. Keep it under ~1500 characters.
- NEVER include secrets, credentials, tokens, file contents, or personal data.
- NEVER include ephemeral task details: issue numbers, branch names, PR links, dates, or what this specific task changed.
- Write the body as durable instructions ("Do X", "Prefer Y"), not a narration of the past run.

Output JSON only, exactly this shape:
{"skill":{"name":"...","description":"...","body":"..."}}
or, when nothing is worth distilling:
{"skill":null}
No prose, no markdown fences, nothing outside the JSON object.`

// renderSkillDistillationPrompt builds the per-call user message: the issue
// the run worked on and its bounded final output (head + tail kept).
func renderSkillDistillationPrompt(issueTitle, output string) string {
	var b strings.Builder
	if strings.TrimSpace(issueTitle) != "" {
		fmt.Fprintf(&b, "The run worked on the issue titled: %s\n\n", issueTitle)
	}
	b.WriteString("FINAL OUTPUT OF THE SUCCESSFUL RUN:\n")
	b.WriteString(truncateSkillDistillationOutput(output))
	b.WriteString("\n\nDistill a reusable skill from this run, or return null if nothing is generalizable.")
	return b.String()
}

// truncateSkillDistillationOutput keeps both ends of a long output: the head
// says what the run set out to do, the tail says what it concluded.
func truncateSkillDistillationOutput(output string) string {
	runes := []rune(output)
	if len(runes) <= skillDistillationOutputBudget {
		return output
	}
	head := string(runes[:skillDistillationOutputBudget/2])
	tail := string(runes[len(runes)-skillDistillationOutputBudget/2:])
	return head + "\n…[truncated]…\n" + tail
}

// parseDistilledSkill decodes the model's reply. A nil *distilledSkill (with
// no error) means the model declined to distill — an expected outcome. A
// non-nil error means a malformed reply, which the caller also treats as
// "nothing to store".
func parseDistilledSkill(raw string) (*distilledSkill, error) {
	var decoded struct {
		Skill *struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Body        string `json:"body"`
		} `json:"skill"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	if decoded.Skill == nil {
		return nil, nil
	}
	name := strings.TrimSpace(decoded.Skill.Name)
	body := strings.TrimSpace(decoded.Skill.Body)
	if name == "" || body == "" {
		// A skill without a name or a body is not usable.
		return nil, nil
	}
	return &distilledSkill{
		Name:        name,
		Description: strings.TrimSpace(decoded.Skill.Description),
		Body:        body,
	}, nil
}
