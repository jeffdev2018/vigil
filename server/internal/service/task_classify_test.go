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

		// Non-English labels, same exact-match rules.
		{name: "japanese bug label", title: "Something", labels: []string{"バグ"}, want: TaskClassBugfix},
		{name: "korean feature label", title: "Something", labels: []string{"기능"}, want: TaskClassFeature},
		{name: "chinese docs label", title: "Something", labels: []string{"文档"}, want: TaskClassDocs},
		{name: "french chore label", title: "Something", labels: []string{"  Dépendances "}, want: TaskClassChore},

		// Japanese titles: matched as substrings, no word boundary.
		{name: "ja bugfix", title: "ログイン画面のバグを修正", want: TaskClassBugfix},
		{name: "ja feature", title: "新機能: ダークモード", want: TaskClassFeature},
		{name: "ja refactor", title: "クレームパイプラインのリファクタリング", want: TaskClassRefactor},
		{name: "ja docs", title: "ドキュメントを更新", want: TaskClassDocs},
		{name: "ja tests", title: "テストを追加", want: TaskClassTests},
		{name: "ja chore", title: "依存関係を更新", want: TaskClassChore},

		// Korean titles: particles agglutinate, so substrings again.
		{name: "ko bugfix with particle", title: "로그인 버그가 발생", want: TaskClassBugfix},
		{name: "ko feature", title: "다크 모드 기능 추가", want: TaskClassFeature},
		{name: "ko refactor", title: "라우터 리팩터링", want: TaskClassRefactor},
		{name: "ko docs", title: "문서 업데이트", want: TaskClassDocs},
		{name: "ko tests", title: "테스트 커버리지 추가", want: TaskClassTests},
		{name: "ko chore", title: "의존성 업그레이드", want: TaskClassChore},

		// Simplified Chinese titles.
		{name: "zh bugfix", title: "修复登录超时错误", want: TaskClassBugfix},
		{name: "zh feature", title: "新增暗色模式功能", want: TaskClassFeature},
		{name: "zh refactor", title: "重构认领流程", want: TaskClassRefactor},
		{name: "zh docs", title: "更新接口文档", want: TaskClassDocs},
		{name: "zh tests", title: "补充路由测试", want: TaskClassTests},
		{name: "zh chore", title: "升级依赖版本", want: TaskClassChore},

		// French titles: \b works around ASCII edges; accented endings match by prefix.
		{name: "fr bugfix plantage", title: "Corriger le plantage au démarrage", want: TaskClassBugfix},
		{name: "fr bugfix bogue", title: "Bogue sur la page de connexion", want: TaskClassBugfix},
		{name: "fr feature accented ending", title: "Ajouter une fonctionnalité de partage", want: TaskClassFeature},
		{name: "fr feature plural", title: "Nouvelles fonctionnalités du tableau de bord", want: TaskClassFeature},
		{name: "fr refactor", title: "Refactorisation du routeur", want: TaskClassRefactor},
		{name: "fr refactor nettoyage", title: "Nettoyage des handlers hérités", want: TaskClassRefactor},
		{name: "fr docs", title: "Mettre à jour la documentation de l'API", want: TaskClassDocs},
		{name: "fr tests couverture", title: "Améliorer la couverture du routeur", want: TaskClassTests},
		{name: "fr chore", title: "Mise à jour des dépendances", want: TaskClassChore},

		// Priority holds across scripts: a title carrying two class terms takes
		// the higher rule, it does not fall to the later one.
		{name: "ja bug term beats feature term", title: "機能のバグを修正する", want: TaskClassBugfix},
		{name: "ko docs term beats tests term", title: "테스트 문서 작성", want: TaskClassDocs},

		// No false positives on neutral non-English titles.
		{name: "ja neutral title", title: "オンボーディング体験を改善", want: TaskClassGeneral},
		{name: "zh neutral title", title: "改进用户引导流程", want: TaskClassGeneral},
		{name: "ko neutral title", title: "온보딩 흐름 개선", want: TaskClassGeneral},
		{name: "fr neutral title", title: "Améliorer le parcours d'accueil", want: TaskClassGeneral},

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
