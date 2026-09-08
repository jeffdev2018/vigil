package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run limits (K03), pure part: the most restrictive cap per gate wins,
// enforce beats observe on a tie, and a gate no policy declares falls back to
// the built-in wall. The DB path (warn, stop, observe, events) is in
// internal/handler/run_limit_test.go.

func TestEffectiveRunLimits(t *testing.T) {
	ws := db.RunLimitPolicy{ScopeType: "workspace", Action: "observe", WarnBps: 8000, MaxCostUsdTicks: pgtype.Int8{Int64: 5_0000000000, Valid: true}, MaxDurationSeconds: pgtype.Int4{Int32: 3600, Valid: true}}
	project := db.RunLimitPolicy{ScopeType: "project", Action: "enforce", WarnBps: 9000, MaxCostUsdTicks: pgtype.Int8{Int64: 2_0000000000, Valid: true}, MaxTurns: pgtype.Int4{Int32: 40, Valid: true}}
	agent := db.RunLimitPolicy{ScopeType: "agent", Action: "enforce", WarnBps: 5000, MaxDurationSeconds: pgtype.Int4{Int32: 3600, Valid: true}}
	gates := EffectiveRunLimits([]db.RunLimitPolicy{ws, project, agent})
	byGate := map[string]RunLimitGate{}
	for _, g := range gates {
		byGate[g.Gate] = g
	}
	if len(gates) != 4 {
		t.Fatalf("gates = %+v, want every gate capped", gates)
	}
	if tc := byGate["tool_calls"]; tc.Scope != builtinRunLimitScope || tc.Limit != builtinRunLimitToolCalls {
		t.Fatalf("tool_calls gate = %+v, want the built-in wall: no policy declares it", tc)
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

// A workspace whose administrator never created a policy is still capped: the
// built-in wall applies to every gate, and it enforces. This is the gate that
// nothing closed before — the enforce default of run_limit_policy governed no
// row, so cost, duration, turns and tool calls were all unbounded.
func TestEffectiveRunLimitsBuiltInWall(t *testing.T) {
	gates := EffectiveRunLimits(nil)
	if len(gates) != len(RunLimitGates) {
		t.Fatalf("gates = %+v, want one per gate with no policy at all", gates)
	}
	for _, g := range gates {
		if g.Action != "enforce" {
			t.Errorf("%s: action = %q, want enforce — a wall that only observes is not a wall", g.Gate, g.Action)
		}
		if g.Limit <= 0 {
			t.Errorf("%s: limit = %d, want a positive ceiling", g.Gate, g.Limit)
		}
		if g.Scope != builtinRunLimitScope {
			t.Errorf("%s: scope = %q, want %q so the UI never claims a policy that does not exist", g.Gate, g.Scope, builtinRunLimitScope)
		}
		if _, err := util.ParseUUID(g.PolicyID); err != nil {
			t.Errorf("%s: policy id %q must parse, run_limit_event.policy_id is NOT NULL: %v", g.Gate, g.PolicyID, err)
		}
	}
}

// An explicit policy replaces the built-in for its gate, which is what lets a
// workspace raise a ceiling. The built-in is a floor for coverage, never a
// second policy thrown into the most-restrictive comparison.
func TestEffectiveRunLimitsPolicyRaisesTheWall(t *testing.T) {
	generous := db.RunLimitPolicy{ScopeType: "workspace", Action: "observe", WarnBps: 9000,
		MaxCostUsdTicks: pgtype.Int8{Int64: builtinRunLimitCost * 3, Valid: true}}
	byGate := map[string]RunLimitGate{}
	for _, g := range EffectiveRunLimits([]db.RunLimitPolicy{generous}) {
		byGate[g.Gate] = g
	}
	if c := byGate["cost"]; c.Limit != builtinRunLimitCost*3 || c.Scope != "workspace" || c.Action != "observe" {
		t.Fatalf("cost gate = %+v, want the declared policy to replace the built-in entirely", c)
	}
	if d := byGate["duration"]; d.Scope != builtinRunLimitScope {
		t.Fatalf("duration gate = %+v, want the built-in on a gate the policy leaves undeclared", d)
	}
}
