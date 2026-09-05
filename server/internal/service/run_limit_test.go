package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run limits (K03), pure part: the most restrictive cap per gate wins,
// enforce beats observe on a tie, a gate nobody caps is absent. The DB path
// (warn, stop, observe, events) is in internal/handler/run_limit_test.go.

func TestEffectiveRunLimits(t *testing.T) {
	ws := db.RunLimitPolicy{ScopeType: "workspace", Action: "observe", WarnBps: 8000, MaxCostUsdTicks: pgtype.Int8{Int64: 5_0000000000, Valid: true}, MaxDurationSeconds: pgtype.Int4{Int32: 3600, Valid: true}}
	project := db.RunLimitPolicy{ScopeType: "project", Action: "enforce", WarnBps: 9000, MaxCostUsdTicks: pgtype.Int8{Int64: 2_0000000000, Valid: true}, MaxTurns: pgtype.Int4{Int32: 40, Valid: true}}
	agent := db.RunLimitPolicy{ScopeType: "agent", Action: "enforce", WarnBps: 5000, MaxDurationSeconds: pgtype.Int4{Int32: 3600, Valid: true}}
	gates := EffectiveRunLimits([]db.RunLimitPolicy{ws, project, agent})
	byGate := map[string]RunLimitGate{}
	for _, g := range gates {
		byGate[g.Gate] = g
	}
	if len(gates) != 3 || byGate["tool_calls"].Gate != "" {
		t.Fatalf("gates = %+v, want cost, duration, turns only", gates)
	}
	if c := byGate["cost"]; c.Limit != 2_0000000000 || c.Scope != "project" || c.Action != "enforce" || c.WarnBps != 9000 {
		t.Fatalf("cost gate = %+v, want the project's smaller cap", c)
	}
	if d := byGate["duration"]; d.Scope != "agent" || d.Action != "enforce" {
		t.Fatalf("duration tie must go to enforce: %+v", d)
	}
	if formatGate("cost", 1_5000000000) != "$1.50" || formatGate("duration", 90) != "1m30s" || formatGate("turns", 7) != "7" {
		t.Fatal("formatGate")
	}
}
