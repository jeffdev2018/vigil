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
	{TaskClassBugfix, []string{"bug", "fix", "bugfix", "hotfix", "bogue", "anomalie", "régression", "バグ", "不具合", "버그", "오류", "缺陷", "错误"}},
	{TaskClassFeature, []string{"feature", "enhancement", "fonctionnalité", "amélioration", "機能", "新機能", "기능", "功能", "特性"}},
	{TaskClassRefactor, []string{"refactor", "refactorisation", "リファクタリング", "리팩터링", "리팩토링", "重构"}},
	{TaskClassDocs, []string{"doc", "docs", "documentation", "ドキュメント", "文書", "문서", "文档"}},
	{TaskClassTests, []string{"test", "tests", "testing", "テスト", "테스트", "测试"}},
	{TaskClassChore, []string{"chore", "maintenance", "dépendances", "依存関係", "의존성", "依赖"}},
}

// taskClassTitleRules are keyword patterns matched against the lowercased
// issue title, in priority order: the first matching rule wins.
//
// Latin-script keywords (English, French) keep ASCII word boundaries so
// "prefix" does not classify as bugfix or "attest" as tests. CJK and Korean
// alternatives are deliberately unanchored: Go's \b is ASCII-only, so it
// never fires next to a kana/hanzi/hangul rune, and Korean particles
// agglutinate ("버그가"). They are matched as substrings instead, which is
// why each one is a term specific enough not to collide across classes.
//
// French words ending in an accented rune ("fonctionnalité") cannot carry a
// trailing \b either — the boundary is ASCII-only — so they match by prefix.
var taskClassTitleRules = []struct {
	class   string
	pattern *regexp.Regexp
}{
	{TaskClassBugfix, regexp.MustCompile(`\b(bug|bugs|bugfix|fix|fixes|fixed|hotfix|regression|crash|bogues?|correctifs?|anomalies?|plantages?|régressions?)\b|バグ|不具合|修正|버그|오류|수정|缺陷|错误|修复|故障`)},
	{TaskClassFeature, regexp.MustCompile(`\b(feature|features|feat)\b|\bfonctionnalit|機能|기능|功能|特性`)},
	{TaskClassRefactor, regexp.MustCompile(`\b(refactor|refactoring|cleanup|clean up|nettoyage)\b|\brefacto|リファクタリング|리팩터링|리팩토링|重构|清理`)},
	{TaskClassDocs, regexp.MustCompile(`\b(docs?|documentation|readme)\b|ドキュメント|文書|문서|文档|说明`)},
	{TaskClassTests, regexp.MustCompile(`\b(tests?|testing|coverage|spec|couverture)\b|テスト|테스트|测试|覆盖率`)},
	{TaskClassChore, regexp.MustCompile(`\b(chore|bump|deps|dependencies|dépendances?|maintenance)\b|依存関係|バージョンアップ|의존성|依赖|升级`)},
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
