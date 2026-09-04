package service

import (
	"regexp"
	"strings"
)

// Task classes the runtime router (JEF-237) segments its statistics on.
// Every enqueued task is stamped with exactly one of these classes; the
// routing score compares (runtime, model) candidates within the same class.
const (
	TaskClassGeneral  = "general"
	TaskClassBugfix   = "bugfix"
	TaskClassFeature  = "feature"
	TaskClassRefactor = "refactor"
	TaskClassDocs     = "docs"
	TaskClassTests    = "tests"
	TaskClassChore    = "chore"
)

// taskClassLabelRules maps lowercased issue-label names to a task class.
// Labels are the strongest signal — a human (or triage) deliberately tagged
// the issue — so they are consulted before any title keyword heuristic.
var taskClassLabelRules = []struct {
	class string
	names []string
}{
	{TaskClassBugfix, []string{"bug", "fix", "bugfix", "hotfix"}},
	{TaskClassFeature, []string{"feature", "enhancement"}},
	{TaskClassRefactor, []string{"refactor"}},
	{TaskClassDocs, []string{"doc", "docs", "documentation"}},
	{TaskClassTests, []string{"test", "tests", "testing"}},
	{TaskClassChore, []string{"chore"}},
}

// taskClassTitleRules are whole-word keyword patterns matched against the
// lowercased issue title, in priority order: the first matching rule wins.
// Word boundaries keep "prefix" from classifying as bugfix or "attest" as
// tests.
var taskClassTitleRules = []struct {
	class   string
	pattern *regexp.Regexp
}{
	{TaskClassBugfix, regexp.MustCompile(`\b(bug|bugs|bugfix|fix|fixes|fixed|hotfix|regression|crash)\b`)},
	{TaskClassFeature, regexp.MustCompile(`\b(feature|feat)\b`)},
	{TaskClassRefactor, regexp.MustCompile(`\b(refactor|refactoring|cleanup|clean up)\b`)},
	{TaskClassDocs, regexp.MustCompile(`\b(docs?|documentation|readme)\b`)},
	{TaskClassTests, regexp.MustCompile(`\b(tests?|testing|coverage|spec)\b`)},
	{TaskClassChore, regexp.MustCompile(`\b(chore|bump|deps|dependencies)\b`)},
}

// ClassifyTask derives the task class for an enqueued issue task. The result
// is deterministic: labels first (any rule hit wins, in rule order), then
// title keywords (first matching rule wins), else TaskClassGeneral. The
// classifier never errors — worst case is the general bucket.
func ClassifyTask(title string, labels []string) string {
	for _, rule := range taskClassLabelRules {
		for _, label := range labels {
			normalized := strings.ToLower(strings.TrimSpace(label))
			for _, name := range rule.names {
				if normalized == name {
					return rule.class
				}
			}
		}
	}
	lowered := strings.ToLower(title)
	for _, rule := range taskClassTitleRules {
		if rule.pattern.MatchString(lowered) {
			return rule.class
		}
	}
	return TaskClassGeneral
}
