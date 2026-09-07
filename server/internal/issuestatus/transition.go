package issuestatus

import "sort"

// Transition rules (F28).
//
// A rule answers one question: may THIS actor move an issue from one status
// CATEGORY into another, and does the move wait for an approver. Everything in
// this file is pure — no database, no request — so the decision matrix is
// testable in one place and the handler keeps only the plumbing.

// TransitionOutcome is what Decide concluded.
type TransitionOutcome int

const (
	// TransitionAllow: the write proceeds.
	TransitionAllow TransitionOutcome = iota
	// TransitionDeny: the write is refused; the issue does not change.
	TransitionDeny
	// TransitionNeedsApproval: the actor may make this move, but an approver
	// has to confirm it first. The write is held, not applied.
	TransitionNeedsApproval
)

// Actor types a rule can grant to. A squad is never the ACTING type — a
// request is made by a member or an agent — but it is a grantable one: a
// squad grant reaches every actor in that squad's roster.
const (
	ActorMember = "member"
	ActorAgent  = "agent"
	ActorSquad  = "squad"
)

// TransitionActor is who is attempting the move.
type TransitionActor struct {
	// Type is "member" or "agent". Never "squad" — see the constants above.
	Type string
	// ID is the member's user id or the agent's id.
	ID string
	// Role is the workspace role of the acting member (owner, admin, member).
	// Empty for an agent: an agent has no workspace role of its own, so role
	// lists never grant to one. Note that an agent's task token authenticates
	// as its OWNING human, which is exactly why the role must not be read off
	// the request's member row for an agent actor — it would hand the agent
	// its owner's authority.
	Role string
	// SquadIDs are the squads this actor belongs to, for squad grants.
	SquadIDs []string
	// System marks a platform writer (the stuck-issue sweeper, the failed-task
	// handler, a merged PR closing its issue). Always allowed: these writes
	// are the platform keeping its own state consistent, and a workspace rule
	// that could stop them would strand issues rather than govern people.
	System bool
}

// TransitionRuleActor is one nominative grant on a rule.
type TransitionRuleActor struct {
	Type string
	ID   string
}

// TransitionRule is the resolver's view of an issue_transition_rule row plus
// its nominative grants. Disabled rows are filtered out before they get here.
type TransitionRule struct {
	ID string
	// ProjectID is empty for a workspace-wide rule.
	ProjectID string
	// FromCategory is empty when the rule applies whatever the origin.
	FromCategory     string
	ToCategory       string
	AllowedRoles     []string
	AllowActorTypes  []string
	RequiresApproval bool
	ApproverRoles    []string
	RejectStatusKey  string
	Actors           []TransitionRuleActor
}

// TransitionDecision is Decide's answer.
type TransitionDecision struct {
	Outcome TransitionOutcome
	// Rule is the rule that decided, or nil when no rule matched (allow) or
	// the actor was exempt.
	Rule *TransitionRule
	// Reason is a short machine-readable cause, carried to the client so the
	// picker can explain a greyed-out option: "" (allowed), "no_grant",
	// "requires_approval".
	Reason string
}

// Reason values on a TransitionDecision.
const (
	ReasonNoGrant          = "no_grant"
	ReasonRequiresApproval = "requires_approval"
)

// Decide resolves one attempted move.
//
// PRECEDENCE, in order:
//
//  1. A system actor is always allowed. So is a workspace OWNER: the owner is
//     the account that can delete the workspace and rewrite every rule in it,
//     so a rule that could lock them out would only ever be an accident to
//     recover from.
//  2. Candidate rules are those whose ToCategory equals `to` and whose scope
//     covers the issue: a project rule matches only its own project, a
//     workspace rule (empty ProjectID) matches everything. A rule with an
//     explicit FromCategory matches only that origin; an empty one matches any.
//  3. Among the candidates, ONE wins: a project rule beats a workspace rule,
//     and within the same scope an explicit FromCategory beats an empty one.
//     Remaining ties break on rule ID so the decision is deterministic.
//  4. No candidate → allow. Silence is permission; a workspace with no rules
//     behaves exactly as it did before this feature.
//  5. The winning rule grants when ANY of: a nominative actor row names this
//     actor (or one of its squads), the actor's role is in AllowedRoles, or
//     the actor's type is in AllowActorTypes. A rule with all three empty
//     grants nobody, which is how a category is closed off entirely.
//  6. Granted + RequiresApproval → NeedsApproval. Granted otherwise → allow.
//     Not granted → deny.
//
// `from` may be empty, which is how a create (no origin) is evaluated; only
// rules with an empty FromCategory can match it.
func Decide(rules []TransitionRule, actor TransitionActor, from, to, projectID string) TransitionDecision {
	if actor.System || actor.Role == "owner" {
		return TransitionDecision{Outcome: TransitionAllow}
	}

	winner := pickTransitionRule(rules, from, to, projectID)
	if winner == nil {
		return TransitionDecision{Outcome: TransitionAllow}
	}
	if !ruleGrants(*winner, actor) {
		return TransitionDecision{Outcome: TransitionDeny, Rule: winner, Reason: ReasonNoGrant}
	}
	if winner.RequiresApproval {
		return TransitionDecision{Outcome: TransitionNeedsApproval, Rule: winner, Reason: ReasonRequiresApproval}
	}
	return TransitionDecision{Outcome: TransitionAllow, Rule: winner}
}

// pickTransitionRule applies steps 2 and 3 of Decide's precedence.
func pickTransitionRule(rules []TransitionRule, from, to, projectID string) *TransitionRule {
	var best *TransitionRule
	bestScore := -1
	for i := range rules {
		rule := &rules[i]
		if rule.ToCategory != to {
			continue
		}
		if rule.ProjectID != "" && rule.ProjectID != projectID {
			continue
		}
		if rule.FromCategory != "" && rule.FromCategory != from {
			continue
		}
		score := 0
		if rule.ProjectID != "" {
			score += 2
		}
		if rule.FromCategory != "" {
			score++
		}
		if score > bestScore || (score == bestScore && best != nil && rule.ID < best.ID) {
			best, bestScore = rule, score
		}
	}
	return best
}

// ruleGrants applies step 5 of Decide's precedence.
func ruleGrants(rule TransitionRule, actor TransitionActor) bool {
	for _, granted := range rule.Actors {
		switch granted.Type {
		case ActorSquad:
			if contains(actor.SquadIDs, granted.ID) {
				return true
			}
		default:
			if granted.Type == actor.Type && granted.ID == actor.ID && actor.ID != "" {
				return true
			}
		}
	}
	// An agent has no workspace role, so AllowedRoles never reaches one.
	if actor.Type == ActorMember && actor.Role != "" && contains(rule.AllowedRoles, actor.Role) {
		return true
	}
	for _, t := range rule.AllowActorTypes {
		if t == ActorSquad {
			// "any squad member" — the broad form of a nominative squad grant.
			if len(actor.SquadIDs) > 0 {
				return true
			}
			continue
		}
		if t == actor.Type {
			return true
		}
	}
	return false
}

// CanApprove reports whether an actor may decide a request held by rule.
// Owners and admins always can — they can edit the rule itself — and so can
// anyone holding a role the rule listed in ApproverRoles. An agent never
// approves: the gate exists so a human sees the move.
func CanApprove(rule *TransitionRule, actor TransitionActor) bool {
	if actor.Type != ActorMember {
		return false
	}
	if actor.Role == "owner" || actor.Role == "admin" {
		return true
	}
	if rule == nil {
		return false
	}
	return contains(rule.ApproverRoles, actor.Role)
}

// ApproverRolesFor returns the roles that may decide a request held by rule,
// always including owner and admin, sorted for a stable payload.
func ApproverRolesFor(rule *TransitionRule) []string {
	seen := map[string]bool{"owner": true, "admin": true}
	if rule != nil {
		for _, role := range rule.ApproverRoles {
			seen[role] = true
		}
	}
	out := make([]string, 0, len(seen))
	for role := range seen {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
