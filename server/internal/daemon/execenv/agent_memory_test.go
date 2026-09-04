package execenv

import (
	"strings"
	"testing"
)

// TestAgentMemorySectionPresent pins the Memory section's placement and copy:
// the facts the agent learned from previous runs (JEF-236) must reach the
// brief verbatim, as bullets, with the re-verify caveat.
func TestAgentMemorySectionPresent(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Memory agent",
		AgentMemories: []string{
			"This repo uses pnpm, never npm.",
			"Run gofmt before committing Go code.",
		},
	})

	for _, want := range []string{
		"## Memory\n",
		"facts you learned from previous tasks",
		"re-verify if the current state contradicts them",
		"- This repo uses pnpm, never npm.\n",
		"- Run gofmt before committing Go code.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief with memories missing %q\n---\n%s", want, out)
		}
	}
	// The section belongs with the agent's identity, ahead of the shared
	// workflow material.
	if strings.Index(out, "## Memory") > strings.Index(out, "## Workflow") {
		t.Errorf("Memory section must precede the Workflow section\n---\n%s", out)
	}
}

// TestAgentMemorySectionAbsentWithoutFacts keeps agents with no memory on a
// byte-identical brief — an empty Memories slice must not even emit the
// heading, or every factless run would pay a prompt-cache prefix change.
func TestAgentMemorySectionAbsentWithoutFacts(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Factless agent",
	})

	if strings.Contains(out, "## Memory") {
		t.Errorf("brief without memories must not emit the Memory heading\n---\n%s", out)
	}
	if strings.Contains(out, "facts you learned from previous tasks") {
		t.Errorf("brief without memories leaked Memory copy\n---\n%s", out)
	}
}
