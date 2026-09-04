package blastradius

import "testing"

func TestResolveConflictsAndWorst(t *testing.T) {
	rules := []Rule{
		{ID: "a", Pattern: "apps/mobile/**", Level: LevelAutonomous},
		{ID: "b", Pattern: "server/migrations/**", Level: LevelReadOnly},
		{ID: "c", Pattern: "**", Level: LevelDualApproval},
		{ID: "d", Pattern: "server/migrations/README.md", Level: LevelAutonomous},
	}
	for path, want := range map[string]string{
		"apps/mobile/app.tsx":            "a",
		"server/migrations/500_x.up.sql": "b",
		"server/migrations/README.md":    "d",
		"packages/core/index.ts":         "c",
		"/apps/mobile/deep/file.ts":      "a",
	} {
		r, ok := Resolve(rules, path)
		if !ok || r.ID != want {
			t.Fatalf("%s -> %s (%v), want %s", path, r.ID, ok, want)
		}
	}
	if _, ok := Resolve(nil, "x"); ok {
		t.Fatal("no rules, no match")
	}
	if Specificity("apps/mobile/**") != len("apps/mobile/") || Specificity("**") != 0 {
		t.Fatalf("specificity = %d, %d", Specificity("apps/mobile/**"), Specificity("**"))
	}
	// Same specificity, different level, shared paths: refused.
	if r, ok := Conflicts(rules, Rule{Pattern: "apps/mobile/**", Level: LevelReadOnly}); !ok || r.ID != "a" {
		t.Fatalf("conflict = %+v, %v", r, ok)
	}
	if _, ok := Conflicts(rules, Rule{Pattern: "apps/mobile/**", Level: LevelAutonomous}); ok {
		t.Fatal("same level is not a conflict")
	}
	if _, ok := Conflicts(rules, Rule{Pattern: "apps/web/**", Level: LevelReadOnly}); ok {
		t.Fatal("disjoint patterns are not a conflict")
	}
	if _, ok := Conflicts(rules, Rule{Pattern: "apps/mobile/ios/**", Level: LevelReadOnly}); ok {
		t.Fatal("a more specific pattern is not a conflict, it wins")
	}
	if _, err := Compile("src/[ab]"); err == nil {
		t.Fatal("character classes are refused")
	}
	if level, ok := Worst(rules, []string{"apps/mobile/a.ts", "server/migrations/1.sql"}); !ok || level != LevelReadOnly {
		t.Fatalf("worst = %s, %v", level, ok)
	}
	if level, ok := Worst(rules, []string{"apps/mobile/a.ts"}); !ok || level != LevelAutonomous {
		t.Fatalf("worst = %s, %v", level, ok)
	}
	if _, ok := Worst(rules[:1], []string{"docs/x.md"}); ok {
		t.Fatal("no rule for the path means inherit")
	}
}
