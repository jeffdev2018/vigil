package service

import (
	"encoding/json"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Systematic adversarial critic (F25 / JEF-18).
//
// A team can decide that one agent — or one squad — never delivers unreviewed:
// when its run completes, a critic agent on a different provider reads the
// change and records a verdict. This file owns the ONE decision that answers
// "what happens to this delivery", so the completion hook stays a wiring
// function and the matrix that defines the feature lives beside its test.
//
// The rules, in the order they apply:
//
//  1. A disabled policy changes nothing. This is the common case and it must
//     cost one boolean.
//  2. The budget is checked BEFORE the critic is resolved. Past max_rounds or
//     past max_cost the loop has to stop whatever else is true — a run that
//     ends because it ran out of rounds is not a run that could not find a
//     reviewer, and reading the reasons the other way round would leave a
//     workspace whose critic was archived mid-loop enqueueing forever.
//  3. No usable critic degrades to `concerns`, never to a hold. A policy that
//     cannot be honoured must not become a way for issues to get stuck, so the
//     delivery finalises exactly as it would have without the policy — but an
//     absent reviewer is not a reviewer who approved. It lands where every
//     other "the platform has no assessment" case lands: `concerns`, with
//     `no_distinct_provider` on the card. `concerns` costs nothing
//     operationally (only `block` relaunches the author), so telling the truth
//     here is free.
//
// `blocking` is deliberately NOT part of whether the critic runs. A
// non-blocking policy still gets its second opinion; it just does not make the
// issue wait for it.

type CriticAction string

const (
	// CriticFinalize: nothing to do — no policy, or the policy does not cover
	// this phase.
	CriticFinalize CriticAction = "finalize"
	// CriticEnqueue: run the critic. Hold says whether the issue waits.
	CriticEnqueue CriticAction = "enqueue"
	// CriticConcernsDegraded: record a `concerns` naming why no critic ran.
	// Distinct from CriticConcernsBudget so the inbox can tell "nobody could
	// review this" from "the loop ran out of budget".
	CriticConcernsDegraded CriticAction = "concerns_degraded"
	// CriticConcernsBudget: record a `concerns` naming the budget that stopped
	// the loop.
	CriticConcernsBudget CriticAction = "concerns_budget"
)

// Reasons the PLATFORM writes on a verdict it produced itself. A verdict a
// critic actually wrote carries no reason.
const (
	CriticReasonNoDistinctProvider = "no_distinct_provider"
	CriticReasonMaxRounds          = "max_rounds"
	CriticReasonMaxCost            = "max_cost"
	// CriticReasonNoVerdict is written when the critic run finished without a
	// readable verdict. It is always `concerns`: silence is not approval, and
	// it is not grounds to relaunch the author either.
	CriticReasonNoVerdict = "no_verdict"
)

// CriticPhaseChange is the only phase acted on today. The column is an array so
// a later phase can be added without a migration.
const CriticPhaseChange = "change"

// CriticVerdict values.
const (
	CriticVerdictPass     = "pass"
	CriticVerdictConcerns = "concerns"
	CriticVerdictBlock    = "block"
)

// CriticPolicy is the decision's whole input, lifted out of the row so the
// matrix in critic_gate_test.go reads as product rules rather than pgtype.
type CriticPolicy struct {
	Enabled  bool
	Blocking bool
	// MaxRounds below 1 is read as 1: a policy that criticises zero times is
	// a disabled policy, and the column has no way to say that.
	MaxRounds int
	// MaxCostUsdTicks 0 means no cap.
	MaxCostUsdTicks int64
}

// CriticPolicyFrom lifts the stored row. A NULL max_cost_usd_ticks is no cap.
func CriticPolicyFrom(row db.AgentCriticPolicy) CriticPolicy {
	p := CriticPolicy{
		Enabled:   row.Enabled,
		Blocking:  row.Blocking,
		MaxRounds: int(row.MaxRounds),
	}
	if row.MaxCostUsdTicks.Valid {
		p.MaxCostUsdTicks = row.MaxCostUsdTicks.Int64
	}
	return p
}

// CriticPolicyCoversPhase reports whether the stored policy acts on a phase.
// An empty phases array covers nothing: the column defaults to {change}, so
// empty means someone cleared it on purpose.
func CriticPolicyCoversPhase(row db.AgentCriticPolicy, phase string) bool {
	for _, p := range row.Phases {
		if p == phase {
			return true
		}
	}
	return false
}

type CriticDecision struct {
	Action CriticAction
	Reason string
	// Hold is true only for CriticEnqueue on a blocking policy: the issue waits
	// in in_review until the verdict lands.
	Hold bool
}

// DecideCritic answers what a finished delivery gets. round is the 1-based
// number of this critique, costSoFar is everything the issue has spent, and
// hasDistinctCritic is the caller's answer to "is there a live critic agent
// that is not the author and, when the policy asks for it, not on the author's
// provider".
func DecideCritic(policy CriticPolicy, round int, costSoFar int64, hasDistinctCritic bool) CriticDecision {
	if !policy.Enabled {
		return CriticDecision{Action: CriticFinalize}
	}
	maxRounds := policy.MaxRounds
	if maxRounds < 1 {
		maxRounds = 1
	}
	if round > maxRounds {
		return CriticDecision{Action: CriticConcernsBudget, Reason: CriticReasonMaxRounds}
	}
	if policy.MaxCostUsdTicks > 0 && costSoFar > policy.MaxCostUsdTicks {
		return CriticDecision{Action: CriticConcernsBudget, Reason: CriticReasonMaxCost}
	}
	if !hasDistinctCritic {
		return CriticDecision{Action: CriticConcernsDegraded, Reason: CriticReasonNoDistinctProvider}
	}
	return CriticDecision{Action: CriticEnqueue, Hold: policy.Blocking}
}

// NormalizeCriticVerdict maps whatever arrived to one of the three values.
// Anything unknown — a critic that invented a fourth word, a truncated block —
// reads as `concerns`: it is the only value that neither approves nor
// relaunches, which is what "we could not tell" has to mean.
func NormalizeCriticVerdict(v string) string {
	switch v {
	case CriticVerdictPass, CriticVerdictConcerns, CriticVerdictBlock:
		return v
	default:
		return CriticVerdictConcerns
	}
}

// CriticAuthorLeg reports whether a completed run is an author delivery, which
// is what the critic judges. The primary leg is one; so is every leg that is a
// real new attempt at the same work (a retry, a failover, a rerun, a revision
// the critic itself asked for, an escalation) — without those, `block` could
// never produce a second round. Review-like legs are not: a critic critiquing
// a critic is the loop this feature exists to bound.
func CriticAuthorLeg(legRole string) bool {
	switch legRole {
	case "", LegRoleRetry, LegRoleFallback, LegRoleRerun, LegRoleRevision, LegRoleEscalation:
		return true
	default:
		return false
	}
}

// TaskCriticStamp is what a critic run carries in its context: the delivery it
// judges, the round it is, and the phase. Same shape as the JEF-273
// force_review stamp — the run is the only place the pointer can live, because
// the verdict row is written when the answer lands, not when the run starts.
type TaskCriticStamp struct {
	OfTaskID string `json:"critic_of_task_id"`
	Round    int    `json:"critic_round"`
	Phase    string `json:"critic_phase"`
}

// TaskCriticOf reads the stamp. ok=false for every run that is not an F25
// critic — including a K72 contest challenger, which shares the `critique` leg
// role and would otherwise be indistinguishable.
func TaskCriticOf(contextJSON []byte) (TaskCriticStamp, bool) {
	if len(contextJSON) == 0 {
		return TaskCriticStamp{}, false
	}
	var s TaskCriticStamp
	if json.Unmarshal(contextJSON, &s) != nil || s.OfTaskID == "" {
		return TaskCriticStamp{}, false
	}
	if s.Phase == "" {
		s.Phase = CriticPhaseChange
	}
	if s.Round < 1 {
		s.Round = 1
	}
	return s, true
}
