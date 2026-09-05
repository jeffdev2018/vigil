package execenv

import (
	"strings"
	"testing"
)

// TestAgentMemorySectionPresent pins the Memory section's placement and copy:
// the facts the agent learned from previous runs (JEF-236) must reach the
// brief verbatim, as bullets, with the re-verify caveat and the citation rule
// (JEF-269).
func TestAgentMemorySectionPresent(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Memory agent",
		AgentMemories: []AgentMemoryForEnv{
			{Content: "This repo uses pnpm, never npm.", State: "approved"},
			{Content: "Run gofmt before committing Go code.", State: "approved"},
		},
	})

	for _, want := range []string{
		"## Memory\n",
		"facts you learned from previous tasks",
		"re-verify if the current state contradicts them",
		"cite it",
		"say so instead of improvising",
		"- This repo uses pnpm, never npm.\n",
		"- Run gofmt before committing Go code.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief with memories missing %q\n---\n%s", want, out)
		}
	}
	// No drafts were passed, so the quarantine sub-heading must not appear.
	if strings.Contains(out, "Unverified memories") {
		t.Errorf("brief without drafts must not emit the draft sub-heading\n---\n%s", out)
	}
	// The section belongs with the agent's identity, ahead of the shared
	// workflow material.
	if strings.Index(out, "## Memory") > strings.Index(out, "## Workflow") {
		t.Errorf("Memory section must precede the Workflow section\n---\n%s", out)
	}
}

// TestAgentMemoryDraftsRenderApart pins the governance split (JEF-269):
// approved facts list under the Memory heading, drafts follow under a marked
// "unverified" sub-heading, whatever order the input mixed them in.
func TestAgentMemoryDraftsRenderApart(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Memory agent",
		AgentMemories: []AgentMemoryForEnv{
			{Content: "Draft hypothesis learned by extraction.", State: "draft"},
			{Content: "Approved fact a human wrote.", State: "approved"},
			{Content: "Another draft hypothesis.", State: "draft"},
		},
	})

	for _, want := range []string{
		"### Unverified memories (draft)\n",
		"learned automatically and no human has reviewed them yet",
		"Re-verify each one",
		"- Approved fact a human wrote.\n",
		"- Draft hypothesis learned by extraction.\n",
		"- Another draft hypothesis.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief with drafts missing %q\n---\n%s", want, out)
		}
	}

	// Approved facts come first: every approved bullet sits ahead of the
	// draft sub-heading, every draft bullet behind it.
	draftHead := strings.Index(out, "### Unverified memories (draft)")
	if draftHead < 0 {
		t.Fatalf("draft sub-heading missing\n---\n%s", out)
	}
	if idx := strings.Index(out, "- Approved fact a human wrote."); idx > draftHead {
		t.Errorf("approved fact rendered after the draft sub-heading\n---\n%s", out)
	}
	for _, draft := range []string{
		"- Draft hypothesis learned by extraction.",
		"- Another draft hypothesis.",
	} {
		if idx := strings.Index(out, draft); idx < draftHead {
			t.Errorf("draft %q rendered before the draft sub-heading\n---\n%s", draft, out)
		}
	}
}

// TestAgentMemoryEmptyStateIsApproved pins the pre-governance fallback: a
// fact whose state never reached the daemon (older server on the claim wire)
// renders as approved, not quarantined.
func TestAgentMemoryEmptyStateIsApproved(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Memory agent",
		AgentMemories: []AgentMemoryForEnv{
			{Content: "Fact from before governance states existed."},
		},
	})

	if !strings.Contains(out, "- Fact from before governance states existed.\n") {
		t.Errorf("stateless fact missing from the brief\n---\n%s", out)
	}
	if strings.Contains(out, "Unverified memories") {
		t.Errorf("stateless fact must not render as a draft\n---\n%s", out)
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
	if strings.Contains(out, "Unverified memories") {
		t.Errorf("brief without memories leaked the draft sub-heading\n---\n%s", out)
	}
}
