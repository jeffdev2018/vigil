package execenv

import (
	"strings"
	"testing"
)

// F22: the Goal Ancestry section of the brief. Ordering, caps and the wire
// shape are the server's (internal/handler/goal_ancestry_test.go); this pins
// what the daemon writes from the chain it received.

func goalAncestryFixture() []GoalAncestryForEnv {
	return []GoalAncestryForEnv{
		{Identifier: "MUL-1", Title: "Ship billing", Description: "Quarter goal:\nbill every seat.", AcceptanceCriteria: []string{"Invoices go out monthly"}, Depth: 2},
		{Identifier: "MUL-7", Title: "Seat counting", Depth: 1},
	}
}

func TestGoalAncestrySectionRendersRootFirst(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{IssueID: "issue-1", GoalAncestry: goalAncestryFixture(), GoalAncestryOmitted: 3}
	out := buildMetaSkillContent("claude", ctx)

	section := out[strings.Index(out, "## Goal Ancestry"):]
	section = section[:strings.Index(section, "\n## ")]
	for _, want := range []string{
		"(3 higher level(s) not shown.)",
		"- MUL-1 — Ship billing",
		"  Quarter goal:\n  bill every seat.",
		"  Acceptance criteria:\n  - Invoices go out monthly",
		"- MUL-7 — Seat counting",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section missing %q:\n%s", want, section)
		}
	}
	if strings.Index(section, "MUL-1") > strings.Index(section, "MUL-7") {
		t.Errorf("root must come before the direct parent:\n%s", section)
	}
	// Between project context and issue metadata, in the durable brief.
	project := strings.Index(out, "## Project Context")
	ancestry := strings.Index(out, "## Goal Ancestry")
	metadata := strings.Index(out, "## Issue Metadata")
	if project != -1 && ancestry < project {
		t.Errorf("ancestry rendered before project context")
	}
	if metadata != -1 && ancestry > metadata {
		t.Errorf("ancestry rendered after issue metadata")
	}
}

func TestGoalAncestrySectionAbsentWithoutChain(t *testing.T) {
	t.Parallel()
	without := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(without, "Goal Ancestry") {
		t.Fatalf("root issue brief must not carry the section:\n%s", without)
	}
	// An older server never sends the field: the brief is byte-identical.
	again := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", GoalAncestry: nil})
	if again != without {
		t.Fatal("nil chain changed brief bytes")
	}
}

func TestGoalAncestrySectionStaysBoundedAtMaximumInput(t *testing.T) {
	t.Parallel()
	// The server caps five nodes at 2 KiB of description each and 8 KiB in
	// total; the rendering overhead on top of that must stay small.
	const serverTotalCap = 8 << 10
	chain := make([]GoalAncestryForEnv, 0, 5)
	for i := 0; i < 5; i++ {
		chain = append(chain, GoalAncestryForEnv{
			Identifier:  "MUL-" + strings.Repeat("9", 6),
			Title:       strings.Repeat("t", 120),
			Description: strings.Repeat("d", (serverTotalCap/5)-200),
			Depth:       5 - i,
		})
	}
	with := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", GoalAncestry: chain, GoalAncestryOmitted: 27})
	without := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"})
	if grew := len(with) - len(without); grew > serverTotalCap+1024 {
		t.Fatalf("section added %d bytes, want at most the server cap plus 1 KiB of layout", grew)
	}
}

func TestGoalAncestrySectionIsByteStableAcrossRenders(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{IssueID: "issue-1", GoalAncestry: goalAncestryFixture()}
	if buildMetaSkillContent("claude", ctx) != buildMetaSkillContent("claude", ctx) {
		t.Fatal("two renders of the same chain differ")
	}
}
