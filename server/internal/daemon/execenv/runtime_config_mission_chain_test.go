package execenv

import (
	"strings"
	"testing"
)

// K74: the Mission and goals section of the brief, mission first, with each
// goal's success measure; absent when the issue serves no goal.

func TestMissionChainSectionRendersMissionFirst(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{IssueID: "issue-1", ProjectTitle: "Billing", MissionChain: []MissionChainForEnv{
		{ID: "g1", Title: "Be profitable", Status: "active", SuccessMeasure: "positive cash flow by Q4", Depth: 1},
		{ID: "g2", Title: "Ship billing", Status: "active", DueDate: "2026-12-31", Description: "Bill every seat.", SuccessMeasure: "every seat invoiced", Depth: 2},
	}}
	out := buildMetaSkillContent("claude", ctx)
	start := strings.Index(out, "## Mission and goals")
	if start == -1 {
		t.Fatalf("section missing:\n%s", out)
	}
	section := out[start:]
	section = section[:strings.Index(section, "\n## ")]
	for _, want := range []string{
		"- Be profitable (active)",
		"  Success measure: positive cash flow by Q4",
		"- Ship billing (active, due 2026-12-31)",
		"  Bill every seat.",
		"  Success measure: every seat invoiced",
		"- Project: Billing",
		"goal-proposal",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section missing %q:\n%s", want, section)
		}
	}
	if strings.Index(section, "Be profitable") > strings.Index(section, "Ship billing") {
		t.Errorf("mission must come first:\n%s", section)
	}
}

func TestMissionChainSectionAbsentWithoutGoal(t *testing.T) {
	t.Parallel()
	without := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(without, "Mission and goals") {
		t.Fatalf("an issue without goal carries no section:\n%s", without)
	}
	if again := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", MissionChain: nil}); again != without {
		t.Fatal("nil chain must render byte-identical")
	}
}
