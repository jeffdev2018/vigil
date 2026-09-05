package handler

import (
	"testing"
)

// Pure layer of the merge readiness matrix (F10): the markdown todo counter
// and the per-PR blocker matrix. DB-backed wiring lives in
// merge_readiness_test.go.

func TestCountOpenTodos(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want int
	}{
		{"none", "just prose\n- a bullet\n", 0},
		{"open and closed", "- [ ] one\n- [x] two\n- [X] three\n- [ ] four\n", 2},
		{"nested and numbered", "  - [ ] nested\n1. [ ] numbered\n2) [x] done\n* [ ] star\n+ [ ] plus\n", 4},
		{"fenced code ignored", "- [ ] real\n```\n- [ ] in code\n```\n~~~\n- [ ] tilde fence\n~~~\n- [ ] real two\n", 2},
		{"not a task", "- [] no space\n-[ ] no gap\n[ ] bare\n", 0},
		{"windows newlines", "- [ ] a\r\n- [x] b\r\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countOpenTodos(c.md); got != c.want {
				t.Fatalf("countOpenTodos = %d, want %d", got, c.want)
			}
		})
	}
}

func strp(s string) *string { return &s }

func TestSinglePRBlockersMatrix(t *testing.T) {
	gh := func(mergeable, state string, total, passed, failed, pending int64, stale bool) MergeReadinessPR {
		pr := MergeReadinessPR{ID: "pr", Source: "github", Number: 7, State: "open", StaleSnapshot: stale,
			Checks: MergeReadinessChecks{Total: total, Passed: passed, Failed: failed, Pending: pending}}
		if mergeable != "" {
			pr.Mergeable = strp(mergeable)
		}
		if state != "" {
			pr.MergeState = strp(state)
		}
		return pr
	}
	kinds := func(bs []MergeBlocker) []string {
		out := make([]string, 0, len(bs))
		for _, b := range bs {
			out = append(out, b.Kind)
		}
		return out
	}
	cases := []struct {
		name string
		pr   MergeReadinessPR
		want []string
	}{
		{"clean with passed checks is ready", gh("mergeable", "clean", 3, 3, 0, 0, false), nil},
		{"no checks is pending, never passed", gh("mergeable", "clean", 0, 0, 0, 0, false), []string{blockerChecksPending}},
		{"running checks", gh("mergeable", "clean", 3, 1, 0, 2, false), []string{blockerChecksPending}},
		{"failing checks win over pending", gh("mergeable", "clean", 3, 1, 1, 1, false), []string{blockerChecksFailing}},
		{"dirty is a conflict", gh("conflicting", "dirty", 3, 3, 0, 0, false), []string{blockerMergeConflict}},
		{"blocked is not clean", gh("mergeable", "blocked", 3, 3, 0, 0, false), []string{blockerMergeNotClean}},
		{"behind is not clean", gh("mergeable", "behind", 3, 3, 0, 0, false), []string{blockerMergeNotClean}},
		{"unknown merge state with green checks is not clean", gh("", "", 3, 3, 0, 0, false), []string{blockerMergeNotClean}},
		{"stale snapshot never reads green", gh("mergeable", "clean", 3, 3, 0, 0, true), []string{blockerStaleSnapshot}},
		{"vcs green", MergeReadinessPR{Source: "gitlab", State: "open", Checks: MergeReadinessChecks{Total: 2, Passed: 2}}, nil},
		{"vcs pending", MergeReadinessPR{Source: "gitlab", State: "open", Checks: MergeReadinessChecks{Total: 2, Passed: 1, Pending: 1}}, []string{blockerChecksPending}},
		{"vcs failed", MergeReadinessPR{Source: "forgejo", State: "open", Checks: MergeReadinessChecks{Total: 2, Passed: 1, Failed: 1}}, []string{blockerChecksFailing}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := kinds(singlePRBlockers(c.pr))
			if len(got) != len(c.want) {
				t.Fatalf("blockers = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("blockers = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestPRBlockersSkipsSettledAndFlagsNoPR(t *testing.T) {
	merged := MergeReadinessPR{Source: "github", State: "merged", Number: 1}
	closed := MergeReadinessPR{Source: "github", State: "closed", Number: 2}
	if got := prBlockers([]MergeReadinessPR{merged, closed}); len(got) != 1 || got[0].Kind != blockerNoPR {
		t.Fatalf("blockers with only settled PRs = %+v, want a single no_pr", got)
	}
	if got := prBlockers(nil); len(got) != 1 || got[0].Kind != blockerNoPR {
		t.Fatalf("blockers with no PR = %+v, want a single no_pr", got)
	}
	green := MergeReadinessPR{Source: "gitlab", State: "open", Number: 3, Checks: MergeReadinessChecks{Total: 1, Passed: 1}}
	if got := prBlockers([]MergeReadinessPR{merged, green}); len(got) != 0 {
		t.Fatalf("blockers with one green open PR = %+v, want none", got)
	}
}
