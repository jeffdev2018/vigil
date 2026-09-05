package service

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/blastradius"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue router (K27), pure parts: path extraction, risk from blast radius,
// escalation counting, settings parsing. The DB path is covered in
// internal/handler/issue_router_test.go.

func TestIssuePathsAndRisk(t *testing.T) {
	paths := IssuePaths("Fix typo in README.md", "Touch `server/migrations/010.sql` and apps/web/app/page.tsx, not the e.g. sentence. Also billing/ledger.go.")
	if got := strings.Join(paths, " "); got != "README.md server/migrations/010.sql apps/web/app/page.tsx billing/ledger.go" {
		t.Fatalf("paths = %q", got)
	}
	rules := []blastradius.Rule{{Pattern: "apps/web/**", Level: "autonomous"}, {Pattern: "server/migrations/**", Level: "read_only"}, {Pattern: "billing/**", Level: "dual_approval"}}
	if level, matched := ClassifyRisk(rules, paths); level != RiskHigh || len(matched) != 3 {
		t.Fatalf("risk = %s matched %v", level, matched)
	}
	if level, _ := ClassifyRisk(rules, []string{"apps/web/x.ts"}); level != RiskLow {
		t.Fatalf("autonomous only must be low, got %s", level)
	}
	if level, _ := ClassifyRisk(rules, []string{"docs/a.md"}); level != RiskNormal {
		t.Fatalf("no match must be normal, got %s", level)
	}
	if level, _ := ClassifyRisk(nil, paths); level != RiskNormal {
		t.Fatal("no rules must be normal")
	}
}

func TestEscalationAndSettings(t *testing.T) {
	rows := func(statuses ...string) []db.ListRecentIssueTaskOutcomesRow {
		var out []db.ListRecentIssueTaskOutcomesRow
		for _, s := range statuses {
			out = append(out, db.ListRecentIssueTaskOutcomesRow{Status: s})
		}
		return out
	}
	if n := consecutiveFailures(rows("failed", "failed", "completed", "failed")); n != 2 {
		t.Fatalf("consecutive = %d, want 2", n)
	}
	if n := consecutiveFailures(rows("cancelled", "failed")); n != 1 {
		t.Fatalf("cancelled is skipped: %d", n)
	}
	if escalate(RiskLow) != RiskNormal || escalate(RiskNormal) != RiskHigh || escalate(RiskHigh) != RiskHigh {
		t.Fatal("escalation ladder")
	}
	cfg := RoutingSettings([]byte(`{"routing":{"enabled":true,"pools":{"low":"p1","high":"p3","weird":"x"},"escalation_failures":3}}`))
	if !cfg.Enabled || cfg.Pools["low"] != "p1" || cfg.Pools["high"] != "p3" || len(cfg.Pools) != 2 || cfg.EscalationFailures != 3 {
		t.Fatalf("settings = %+v", cfg)
	}
	if def := RoutingSettings(nil); def.Enabled || def.EscalationFailures != 2 {
		t.Fatalf("defaults = %+v", def)
	}
	_ = pgtype.UUID{}
}
