// @canonical F25 critic gate matrix. The handler suite wires this decision to
// the completion hook; it must not re-run this matrix through the database.
package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCriticGateDecides(t *testing.T) {
	on := CriticPolicy{Enabled: true, MaxRounds: 1}
	blocking := CriticPolicy{Enabled: true, Blocking: true, MaxRounds: 2}
	budgeted := CriticPolicy{Enabled: true, MaxRounds: 3, MaxCostUsdTicks: 1000}

	cases := []struct {
		name   string
		policy CriticPolicy
		round  int
		cost   int64
		critic bool
		action CriticAction
		reason string
		hold   bool
	}{
		{"disabled policy changes nothing", CriticPolicy{}, 1, 0, true, CriticFinalize, "", false},
		{"disabled policy is not rescued by a critic", CriticPolicy{MaxRounds: 3}, 1, 0, true, CriticFinalize, "", false},
		{"enabled, first round, critic available", on, 1, 0, true, CriticEnqueue, "", false},
		{"a blocking policy holds the issue", blocking, 1, 0, true, CriticEnqueue, "", true},
		{"no distinct critic degrades to concerns, never a hold", blocking, 1, 0, false, CriticConcernsDegraded, CriticReasonNoDistinctProvider, false},
		{"past max_rounds the loop stops with concerns", on, 2, 0, true, CriticConcernsBudget, CriticReasonMaxRounds, false},
		{"the round cap is inclusive", blocking, 2, 0, true, CriticEnqueue, "", true},
		{"max_rounds below 1 is read as 1", CriticPolicy{Enabled: true, MaxRounds: 0}, 2, 0, true, CriticConcernsBudget, CriticReasonMaxRounds, false},
		{"cost over the cap stops the loop", budgeted, 1, 1001, true, CriticConcernsBudget, CriticReasonMaxCost, false},
		{"cost exactly at the cap still runs", budgeted, 1, 1000, true, CriticEnqueue, "", false},
		{"no cap means no cost check", on, 1, 1 << 40, true, CriticEnqueue, "", false},
		// Order matters: a loop that ran out of rounds reads as a budget stop,
		// not as a missing reviewer, so it cannot re-enqueue forever once the
		// critic agent is archived mid-loop.
		{"budget beats a missing critic", on, 2, 0, false, CriticConcernsBudget, CriticReasonMaxRounds, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideCritic(tc.policy, tc.round, tc.cost, tc.critic)
			if got.Action != tc.action || got.Reason != tc.reason || got.Hold != tc.hold {
				t.Errorf("DecideCritic(round=%d, cost=%d, critic=%v) = %+v, want action %q reason %q hold %v",
					tc.round, tc.cost, tc.critic, got, tc.action, tc.reason, tc.hold)
			}
		})
	}
}

func TestCriticPolicyFromRow(t *testing.T) {
	row := db.AgentCriticPolicy{Enabled: true, Blocking: true, MaxRounds: 3}
	if got := CriticPolicyFrom(row); got.MaxCostUsdTicks != 0 {
		t.Errorf("a NULL max_cost_usd_ticks must read as no cap, got %d", got.MaxCostUsdTicks)
	}
	row.MaxCostUsdTicks = pgtype.Int8{Int64: 42, Valid: true}
	if got := CriticPolicyFrom(row); got.MaxCostUsdTicks != 42 || !got.Blocking || got.MaxRounds != 3 {
		t.Errorf("CriticPolicyFrom lost a field: %+v", got)
	}
}

func TestCriticPolicyCoversPhase(t *testing.T) {
	if !CriticPolicyCoversPhase(db.AgentCriticPolicy{Phases: []string{"change"}}, CriticPhaseChange) {
		t.Error("the default phase must be covered")
	}
	// Empty is a deliberate clear, not the default: the column defaults to
	// {change}, so an empty array can only come from someone emptying it.
	if CriticPolicyCoversPhase(db.AgentCriticPolicy{Phases: nil}, CriticPhaseChange) {
		t.Error("an empty phases array must cover nothing")
	}
	if CriticPolicyCoversPhase(db.AgentCriticPolicy{Phases: []string{"plan"}}, CriticPhaseChange) {
		t.Error("a policy scoped to another phase must not act on change")
	}
}

func TestNormalizeCriticVerdict(t *testing.T) {
	for _, v := range []string{CriticVerdictPass, CriticVerdictConcerns, CriticVerdictBlock} {
		if got := NormalizeCriticVerdict(v); got != v {
			t.Errorf("NormalizeCriticVerdict(%q) = %q", v, got)
		}
	}
	// The product rule the card also states: an unknown verdict is never
	// approval and never a relaunch.
	for _, v := range []string{"", "approve", "PASS", "reject", "blocked"} {
		if got := NormalizeCriticVerdict(v); got != CriticVerdictConcerns {
			t.Errorf("NormalizeCriticVerdict(%q) = %q, want concerns", v, got)
		}
	}
}

func TestCriticAuthorLeg(t *testing.T) {
	// A relaunched author is a revision leg; without it `block` could never
	// produce a second round.
	for _, role := range []string{"", LegRoleRetry, LegRoleFallback, LegRoleRerun, LegRoleRevision, LegRoleEscalation} {
		if !CriticAuthorLeg(role) {
			t.Errorf("leg %q is an author delivery", role)
		}
	}
	for _, role := range []string{LegRoleReview, LegRoleCritique, LegRoleAnswer, LegRoleWatchdog, LegRoleEval, LegRolePrWalkthrough, LegRoleEpicStep, LegRoleDuel} {
		if CriticAuthorLeg(role) {
			t.Errorf("leg %q judges or documents work; criticising it loops", role)
		}
	}
}

func TestTaskCriticOf(t *testing.T) {
	if _, ok := TaskCriticOf(nil); ok {
		t.Error("a run with no context is not a critic run")
	}
	// A K72 contest challenger shares the `critique` leg role; without the
	// stamp it must not be mistaken for an F25 critic.
	if _, ok := TaskCriticOf([]byte(`{"force_review":true}`)); ok {
		t.Error("a context without critic_of_task_id is not a critic run")
	}
	if _, ok := TaskCriticOf([]byte(`{"critic_of_task_id":`)); ok {
		t.Error("malformed context must not read as a critic run")
	}
	got, ok := TaskCriticOf([]byte(`{"critic_of_task_id":"abc","critic_round":2,"critic_phase":"change"}`))
	if !ok || got.OfTaskID != "abc" || got.Round != 2 || got.Phase != CriticPhaseChange {
		t.Errorf("TaskCriticOf = %+v ok=%v", got, ok)
	}
	got, ok = TaskCriticOf([]byte(`{"critic_of_task_id":"abc"}`))
	if !ok || got.Round != 1 || got.Phase != CriticPhaseChange {
		t.Errorf("defaults not applied: %+v", got)
	}
}

// The four ways the platform ends a critic loop without a critic's own answer
// must agree: none of them is a pass. Silence, a spent budget, a workflow at
// its ceiling and a workspace with nobody to review are all "we have no
// assessment", and recording any of them as approval would make the feature
// claim a review that never happened.
func TestCriticPlatformVerdictsAreNeverAPass(t *testing.T) {
	blocking := CriticPolicy{Enabled: true, Blocking: true, MaxRounds: 2}
	budgeted := CriticPolicy{Enabled: true, MaxRounds: 1, MaxCostUsdTicks: 100}

	for _, tc := range []struct {
		name   string
		policy CriticPolicy
		round  int
		cost   int64
		critic bool
		reason string
	}{
		{"nobody can review", blocking, 1, 0, false, CriticReasonNoDistinctProvider},
		{"the rounds ran out", budgeted, 2, 0, true, CriticReasonMaxRounds},
		{"the budget ran out", budgeted, 1, 101, true, CriticReasonMaxCost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideCritic(tc.policy, tc.round, tc.cost, tc.critic)
			if d.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", d.Reason, tc.reason)
			}
			if d.Action != CriticConcernsDegraded && d.Action != CriticConcernsBudget {
				t.Fatalf("action = %q: a verdict the platform wrote itself is never a pass", d.Action)
			}
			if d.Hold {
				t.Error("a policy that cannot be honoured must never hold the issue")
			}
		})
	}

	// And the reason a critic wrote nothing readable is already concerns, which
	// is the doctrine the three above now follow.
	if NormalizeCriticVerdict("") != CriticVerdictConcerns {
		t.Error("silence is not approval")
	}
}
