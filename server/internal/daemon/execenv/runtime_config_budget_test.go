package execenv

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The brief is the cache prefix of every run on every runtime, and it is paid
// once per run whatever the workspace contains. Nothing bounded it: the
// organisation, the goal ancestry, the skill list and the repo hints all grow
// it as a workspace matures, and the static sections grew with the product.
// This pins the floor as a declared cost rather than an observed one, which is
// the whole point — a section added here is a tradeoff someone makes on
// purpose, not a number that drifts.
//
// The remedy when this fails is not a bigger number by default. The Brain
// already ships the pattern in this same package: an index plus a pointer
// (workspace_knowledge.go), with a byte budget and a line saying what was cut.
// Long reference material belongs behind a pointer; only what the agent needs
// before it can act belongs in the prefix.
const briefFloorByteBudget = 15360 // 15 KiB

func TestBriefFloorStaysWithinItsBudget(t *testing.T) {
	t.Parallel()

	// The emptiest context there is: no organisation, no goals, no skills, no
	// hints, no memories. Every run pays at least this.
	base := TaskContextForEnv{IssueID: "issue-1", AgentName: "A", AgentID: "agent-1"}

	sizes := map[string]int{}
	for _, provider := range []string{"claude", "codex", "opencode", "gemini"} {
		brief := buildMetaSkillContent(provider, base)
		sizes[provider] = len(brief)
		if len(brief) > briefFloorByteBudget {
			t.Errorf("%s brief floor is %d bytes over budget (%d of %d, %d lines).\n"+
				"Raising the budget is a decision, not a fix: move reference material behind a\n"+
				"pointer the way workspace_knowledge.go does, or say in the PR why this section\n"+
				"has to sit in the prefix of every run.",
				provider, len(brief)-briefFloorByteBudget, len(brief), briefFloorByteBudget,
				strings.Count(brief, "\n")+1)
		}
	}

	// The floor is provider-independent by construction, and a provider that
	// starts paying more than the others should say so here rather than be
	// discovered in a token bill.
	var distinct []string
	for provider, size := range sizes {
		distinct = append(distinct, fmt.Sprintf("%s=%d", provider, size))
	}
	sort.Strings(distinct)
	first := sizes["claude"]
	for _, size := range sizes {
		if size != first {
			t.Fatalf("the brief floor now differs per provider: %s", strings.Join(distinct, " "))
		}
	}
}
