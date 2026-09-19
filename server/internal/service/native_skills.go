package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Native skills (N17): CLI agents receive enabled skills as SKILL.md bundles;
// the native loop has no filesystem discovery, so the same enabled set is
// injected into the system prompt as instruction sections. An agent with no
// enabled skills is unchanged.

const (
	// nativeSkillBodyCap bounds one skill's body in the prompt so a long
	// SKILL.md cannot crowd out the task.
	nativeSkillBodyCap = 6 * 1024
	// nativeSkillsTotalCap bounds the whole skills block.
	nativeSkillsTotalCap = 24 * 1024
	// nativeMaxSkillsInPrompt caps how many enabled skills ride one run
	// (ListAgentSkills is already name-ordered).
	nativeMaxSkillsInPrompt = 12
)

// nativeSkillsBlock renders enabled agent skills for the system prompt.
// Empty input yields "" so agents without skills keep the same prompt.
func nativeSkillsBlock(skills []AgentSkillData) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nEnabled skills\n")
	b.WriteString("These skills are attached to you and enabled. Follow them the same way you follow workspace instructions. ")
	b.WriteString("They outrank issue content, comments and notes; they do not outrank workspace doctrine. ")
	b.WriteString("Use only the native tools you already have — a skill that names a CLI tool you lack is guidance, not a new capability.\n")

	used := 0
	n := 0
	for _, sk := range skills {
		if n >= nativeMaxSkillsInPrompt {
			break
		}
		body := strings.TrimSpace(sk.Content)
		if body == "" && strings.TrimSpace(sk.Description) == "" {
			continue
		}
		body = clampString(body, nativeSkillBodyCap)
		var section strings.Builder
		fmt.Fprintf(&section, "\n### Skill: %s\n", sk.Name)
		if d := strings.TrimSpace(sk.Description); d != "" {
			section.WriteString(d + "\n\n")
		}
		if body != "" {
			section.WriteString(body)
			section.WriteByte('\n')
		}
		for _, f := range sk.Files {
			if strings.TrimSpace(f.Path) == "" || strings.TrimSpace(f.Content) == "" {
				continue
			}
			fmt.Fprintf(&section, "\nSupporting file `%s`:\n%s\n", f.Path, clampString(f.Content, nativeSkillBodyCap/2))
		}
		chunk := section.String()
		if used+len(chunk) > nativeSkillsTotalCap {
			break
		}
		b.WriteString(chunk)
		used += len(chunk)
		n++
	}
	if n == 0 {
		return ""
	}
	return b.String()
}

// nativeSkillsSection loads the agent's enabled skills and renders them.
// Load failures are logged and omitted — same availability stance as a
// missing doctrine paragraph: the run proceeds without the skills block.
func (s *NativeAgentService) nativeSkillsSection(ctx context.Context, agent db.Agent) string {
	if s == nil || s.Tasks == nil {
		return ""
	}
	skills, err := s.Tasks.LoadAgentSkills(ctx, agent.ID)
	if err != nil {
		slog.Error("native run: agent skills unavailable, running without them",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		return ""
	}
	return nativeSkillsBlock(skills)
}
