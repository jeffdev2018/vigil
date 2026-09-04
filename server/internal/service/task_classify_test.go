package service

import "testing"

func TestClassifyTask(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		labels []string
		want   string
	}{
		// Labels win over everything, case-insensitively.
		{name: "bug label", title: "Add dark mode", labels: []string{"bug"}, want: TaskClassBugfix},
		{name: "fix label", title: "Something", labels: []string{"fix"}, want: TaskClassBugfix},
		{name: "uppercase label", title: "Something", labels: []string{"BUG"}, want: TaskClassBugfix},
		{name: "whitespace label trimmed", title: "Something", labels: []string{"  feature  "}, want: TaskClassFeature},
		{name: "enhancement label", title: "Something", labels: []string{"enhancement"}, want: TaskClassFeature},
		{name: "refactor label", title: "Something", labels: []string{"refactor"}, want: TaskClassRefactor},
		{name: "docs label", title: "Something", labels: []string{"documentation"}, want: TaskClassDocs},
		{name: "doc singular label", title: "Something", labels: []string{"doc"}, want: TaskClassDocs},
		{name: "tests label", title: "Something", labels: []string{"tests"}, want: TaskClassTests},
		{name: "test singular label", title: "Something", labels: []string{"test"}, want: TaskClassTests},
		{name: "chore label", title: "Something", labels: []string{"chore"}, want: TaskClassChore},
		{name: "unknown label falls through to title", title: "Fix the crash", labels: []string{"ui"}, want: TaskClassBugfix},
		{name: "label priority: bugfix rule before feature rule", title: "", labels: []string{"feature", "bug"}, want: TaskClassBugfix},

		// Title keywords, word-boundary matched.
		{name: "fix in title", title: "Fix login timeout", want: TaskClassBugfix},
		{name: "bug in title", title: "Bug: panic on empty input", want: TaskClassBugfix},
		{name: "regression in title", title: "Regression in checkout flow", want: TaskClassBugfix},
		{name: "crash in title", title: "App crash on startup", want: TaskClassBugfix},
		{name: "feature in title", title: "Add feature flags panel", want: TaskClassFeature},
		{name: "feat in title", title: "feat: oauth login", want: TaskClassFeature},
		{name: "refactor in title", title: "Refactor claim pipeline", want: TaskClassRefactor},
		{name: "cleanup in title", title: "Cleanup legacy handlers", want: TaskClassRefactor},
		{name: "docs in title", title: "Update docs for claim API", want: TaskClassDocs},
		{name: "readme in title", title: "Rewrite README quickstart", want: TaskClassDocs},
		{name: "test in title", title: "Add test for wilson bound", want: TaskClassTests},
		{name: "coverage in title", title: "Improve coverage of router", want: TaskClassTests},
		{name: "chore in title", title: "Chore: tidy go.mod", want: TaskClassChore},
		{name: "bump in title", title: "Bump sqlc to v1.31", want: TaskClassChore},
		{name: "dependencies in title", title: "Update dependencies", want: TaskClassChore},
		{name: "title rule priority: bugfix before feature", title: "Fix feature flag regression", want: TaskClassBugfix},

		// Word boundaries: substrings must not classify.
		{name: "prefix is not fix", title: "Add prefix support to router", want: TaskClassGeneral},
		{name: "attest is not test", title: "Attest provenance of artifacts", want: TaskClassGeneral},
		{name: "fixture is not fix", title: "Seed fixture data for demos", want: TaskClassGeneral},

		// Nothing matches.
		{name: "empty", title: "", want: TaskClassGeneral},
		{name: "generic title", title: "Improve onboarding flow", want: TaskClassGeneral},
		{name: "nil labels", title: "Improve onboarding", want: TaskClassGeneral},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyTask(tt.title, tt.labels); got != tt.want {
				t.Errorf("ClassifyTask(%q, %v) = %q, want %q", tt.title, tt.labels, got, tt.want)
			}
		})
	}
}
