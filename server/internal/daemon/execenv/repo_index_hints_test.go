package execenv

import (
	"strings"
	"testing"
)

// Shared repo index hints in the brief (K47).

func hint(path, symbol, snippet string, stale bool) RepoIndexHintForEnv {
	return RepoIndexHintForEnv{
		RepoIdentifier: "git@example.com:team/app.git",
		FilePath:       path,
		Symbol:         symbol,
		StartLine:      40,
		EndLine:        70,
		Snippet:        snippet,
		Score:          1.5,
		Stale:          stale,
	}
}

func TestRepoIndexHintsBriefSection(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Index agent",
		RepoIndexHints: []RepoIndexHintForEnv{
			hint("server/scheduler/retry.go", "scheduleRetry", "func scheduleRetry(task Task) {}", false),
			hint("web/list.tsx", "", "export const IssueList = () => null", true),
		},
	})

	for _, want := range []string{
		// The heading itself carries the contract. This section is the one part
		// of the brief that may legitimately disagree with the working tree, so
		// "read the real file" has to be impossible to miss.
		"## Repo index hints (orientation only — read the real file before editing)",
		"read the real lines before you rely on or change anything here",
		"server/scheduler/retry.go:40-70",
		"`scheduleRetry`",
		"func scheduleRetry(task Task) {}",
		"git@example.com:team/app.git",
		// A chunk the server knows is behind the newest indexed commit is
		// labelled rather than dropped: an older pointer still beats none.
		"(indexed at an older commit)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief missing %q\n---\n%s", want, out)
		}
	}
}

func TestRepoIndexHintsAbsentKeepsBriefIdentical(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Index agent",
	}
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, "Repo index hints") {
		t.Errorf("a workspace with no indexed repository must keep a byte-identical brief\n---\n%s", out)
	}
}
