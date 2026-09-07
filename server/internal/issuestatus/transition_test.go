// @vitest-environment does not apply here; this is the canonical Go matrix for
// F28's transition resolver. The handler suite deliberately does NOT replay it:
// it covers wiring (which entry points call the gate, what the HTTP shape is),
// not the origin x target x role x actor-type x project-override grid below.
package issuestatus

import "testing"

func member(role string, squads ...string) TransitionActor {
	return TransitionActor{Type: ActorMember, ID: "u1", Role: role, SquadIDs: squads}
}

func agent(id string, squads ...string) TransitionActor {
	return TransitionActor{Type: ActorAgent, ID: id, SquadIDs: squads}
}

// wsRule is a workspace-wide rule on in_progress -> done granting admins.
func wsRule() TransitionRule {
	return TransitionRule{
		ID:           "r-ws",
		FromCategory: InProgress,
		ToCategory:   Done,
		AllowedRoles: []string{"admin"},
	}
}

func TestDecideAllowsWhenNoRuleMatches(t *testing.T) {
	cases := []struct {
		name      string
		rules     []TransitionRule
		from, to  string
		projectID string
	}{
		{name: "no rules at all", from: InProgress, to: Done},
		{name: "rule targets another category", rules: []TransitionRule{wsRule()}, from: InProgress, to: Cancelled},
		{name: "rule pinned to another project", rules: []TransitionRule{{
			ID: "r", ProjectID: "p-other", ToCategory: Done,
		}}, from: InProgress, to: Done, projectID: "p-mine"},
		{name: "explicit from does not match this origin", rules: []TransitionRule{wsRule()}, from: Todo, to: Done},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.rules, member("member"), tc.from, tc.to, tc.projectID)
			if got.Outcome != TransitionAllow {
				t.Fatalf("outcome = %v, want allow", got.Outcome)
			}
			if got.Rule != nil {
				t.Fatalf("rule = %q, want none — nothing decided this", got.Rule.ID)
			}
		})
	}
}

// The exemptions. Both are deliberate and both are asserted here because a
// regression in either one is a lockout, not a leak.
func TestDecideExemptions(t *testing.T) {
	lockdown := []TransitionRule{{ID: "r", ToCategory: Done}} // grants nobody

	t.Run("owner is always allowed", func(t *testing.T) {
		if got := Decide(lockdown, member("owner"), InProgress, Done, ""); got.Outcome != TransitionAllow {
			t.Fatalf("outcome = %v, want allow for owner", got.Outcome)
		}
	})
	t.Run("owner bypasses an approval gate too", func(t *testing.T) {
		gated := []TransitionRule{{ID: "r", ToCategory: Done, AllowedRoles: []string{"owner"}, RequiresApproval: true}}
		if got := Decide(gated, member("owner"), InProgress, Done, ""); got.Outcome != TransitionAllow {
			t.Fatalf("outcome = %v, want allow — an owner does not queue for their own approval", got.Outcome)
		}
	})
	t.Run("system writer is always allowed", func(t *testing.T) {
		sys := TransitionActor{Type: ActorMember, System: true}
		if got := Decide(lockdown, sys, InProgress, Done, ""); got.Outcome != TransitionAllow {
			t.Fatalf("outcome = %v, want allow for the system actor", got.Outcome)
		}
	})
	t.Run("admin is NOT exempt", func(t *testing.T) {
		if got := Decide(lockdown, member("admin"), InProgress, Done, ""); got.Outcome != TransitionDeny {
			t.Fatalf("outcome = %v, want deny — only owner is exempt", got.Outcome)
		}
	})
}

// The grant matrix: origin x target x role x actor type.
func TestDecideGrantMatrix(t *testing.T) {
	nominativeAgent := TransitionRule{
		ID: "r", FromCategory: InProgress, ToCategory: Done,
		Actors: []TransitionRuleActor{{Type: ActorAgent, ID: "a-release"}},
	}
	squadGrant := TransitionRule{
		ID: "r", FromCategory: InProgress, ToCategory: Done,
		Actors: []TransitionRuleActor{{Type: ActorSquad, ID: "s-1"}},
	}

	cases := []struct {
		name  string
		rule  TransitionRule
		actor TransitionActor
		want  TransitionOutcome
	}{
		{"role listed", wsRule(), member("admin"), TransitionAllow},
		{"role not listed", wsRule(), member("member"), TransitionDeny},
		{"agent never matches a role list", wsRule(), agent("a-1"), TransitionDeny},

		{"actor type member", TransitionRule{ID: "r", ToCategory: Done, AllowActorTypes: []string{ActorMember}}, member("member"), TransitionAllow},
		{"actor type member refuses an agent", TransitionRule{ID: "r", ToCategory: Done, AllowActorTypes: []string{ActorMember}}, agent("a-1"), TransitionDeny},
		{"actor type agent", TransitionRule{ID: "r", ToCategory: Done, AllowActorTypes: []string{ActorAgent}}, agent("a-1"), TransitionAllow},

		{"nominative agent granted", nominativeAgent, agent("a-release"), TransitionAllow},
		{"nominative agent refuses another agent", nominativeAgent, agent("a-other"), TransitionDeny},
		{"nominative agent refuses a member", nominativeAgent, member("member"), TransitionDeny},

		{"squad grant reaches a member of that squad", squadGrant, member("member", "s-1"), TransitionAllow},
		{"squad grant refuses an outsider", squadGrant, member("member", "s-2"), TransitionDeny},
		{"squad grant reaches an agent in that squad", squadGrant, agent("a-1", "s-1"), TransitionAllow},

		{"allow_actor_types squad means any squad member",
			TransitionRule{ID: "r", ToCategory: Done, AllowActorTypes: []string{ActorSquad}}, member("member", "s-9"), TransitionAllow},
		{"allow_actor_types squad refuses someone in no squad",
			TransitionRule{ID: "r", ToCategory: Done, AllowActorTypes: []string{ActorSquad}}, member("member"), TransitionDeny},

		{"empty rule grants nobody", TransitionRule{ID: "r", ToCategory: Done}, member("member"), TransitionDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide([]TransitionRule{tc.rule}, tc.actor, InProgress, Done, "")
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %v, want %v (reason %q)", got.Outcome, tc.want, got.Reason)
			}
			if tc.want == TransitionDeny && got.Reason != ReasonNoGrant {
				t.Fatalf("reason = %q, want %q", got.Reason, ReasonNoGrant)
			}
			if got.Rule == nil || got.Rule.ID != tc.rule.ID {
				t.Fatalf("rule = %+v, want the matching rule reported back", got.Rule)
			}
		})
	}
}

func TestDecideApprovalGate(t *testing.T) {
	gated := []TransitionRule{{
		ID: "r", FromCategory: InProgress, ToCategory: Done,
		AllowedRoles: []string{"member"}, RequiresApproval: true,
		ApproverRoles: []string{"admin"}, RejectStatusKey: "in_progress",
	}}

	t.Run("granted actor is held for approval", func(t *testing.T) {
		got := Decide(gated, member("member"), InProgress, Done, "")
		if got.Outcome != TransitionNeedsApproval {
			t.Fatalf("outcome = %v, want needs approval", got.Outcome)
		}
		if got.Reason != ReasonRequiresApproval {
			t.Fatalf("reason = %q, want %q", got.Reason, ReasonRequiresApproval)
		}
	})
	t.Run("an actor the rule does not grant is denied, not queued", func(t *testing.T) {
		if got := Decide(gated, agent("a-1"), InProgress, Done, ""); got.Outcome != TransitionDeny {
			t.Fatalf("outcome = %v, want deny — approval is for actors the rule allows", got.Outcome)
		}
	})
}

// Precedence: project over workspace, explicit origin over any origin.
func TestDecidePrecedence(t *testing.T) {
	open := TransitionRule{ID: "r-ws-open", ToCategory: Done, AllowActorTypes: []string{ActorMember}}
	closedProject := TransitionRule{ID: "r-proj", ProjectID: "p-1", ToCategory: Done}
	closedFrom := TransitionRule{ID: "r-ws-from", FromCategory: InProgress, ToCategory: Done}

	t.Run("project rule overrides the workspace rule", func(t *testing.T) {
		got := Decide([]TransitionRule{open, closedProject}, member("member"), InProgress, Done, "p-1")
		if got.Outcome != TransitionDeny || got.Rule.ID != "r-proj" {
			t.Fatalf("got %v via %+v, want deny via r-proj", got.Outcome, got.Rule)
		}
	})
	t.Run("project rule does not reach another project", func(t *testing.T) {
		got := Decide([]TransitionRule{open, closedProject}, member("member"), InProgress, Done, "p-2")
		if got.Outcome != TransitionAllow || got.Rule.ID != "r-ws-open" {
			t.Fatalf("got %v via %+v, want allow via r-ws-open", got.Outcome, got.Rule)
		}
	})
	t.Run("explicit origin beats any-origin at the same scope", func(t *testing.T) {
		got := Decide([]TransitionRule{open, closedFrom}, member("member"), InProgress, Done, "")
		if got.Outcome != TransitionDeny || got.Rule.ID != "r-ws-from" {
			t.Fatalf("got %v via %+v, want deny via r-ws-from", got.Outcome, got.Rule)
		}
	})
	t.Run("any-origin still applies to an origin the explicit rule misses", func(t *testing.T) {
		got := Decide([]TransitionRule{open, closedFrom}, member("member"), Todo, Done, "")
		if got.Outcome != TransitionAllow || got.Rule.ID != "r-ws-open" {
			t.Fatalf("got %v via %+v, want allow via r-ws-open", got.Outcome, got.Rule)
		}
	})
	t.Run("a project any-origin rule beats a workspace explicit-origin rule", func(t *testing.T) {
		projOpen := TransitionRule{ID: "r-proj-open", ProjectID: "p-1", ToCategory: Done, AllowActorTypes: []string{ActorMember}}
		got := Decide([]TransitionRule{closedFrom, projOpen}, member("member"), InProgress, Done, "p-1")
		if got.Outcome != TransitionAllow || got.Rule.ID != "r-proj-open" {
			t.Fatalf("got %v via %+v, want allow via r-proj-open — project scope is the stronger signal", got.Outcome, got.Rule)
		}
	})
	t.Run("a tie breaks deterministically on rule id", func(t *testing.T) {
		a := TransitionRule{ID: "aaa", ToCategory: Done, AllowActorTypes: []string{ActorMember}}
		b := TransitionRule{ID: "bbb", ToCategory: Done}
		forward := Decide([]TransitionRule{a, b}, member("member"), InProgress, Done, "")
		backward := Decide([]TransitionRule{b, a}, member("member"), InProgress, Done, "")
		if forward.Rule.ID != "aaa" || backward.Rule.ID != "aaa" {
			t.Fatalf("forward=%q backward=%q, want both aaa", forward.Rule.ID, backward.Rule.ID)
		}
	})
}

// A create has no origin: only an any-origin rule can govern it.
func TestDecideEmptyOriginIsACreate(t *testing.T) {
	explicit := TransitionRule{ID: "r-from", FromCategory: Todo, ToCategory: Done}
	anyOrigin := TransitionRule{ID: "r-any", ToCategory: Done}

	if got := Decide([]TransitionRule{explicit}, member("member"), "", Done, ""); got.Outcome != TransitionAllow {
		t.Fatalf("outcome = %v, want allow — an explicit origin cannot match a create", got.Outcome)
	}
	if got := Decide([]TransitionRule{anyOrigin}, member("member"), "", Done, ""); got.Outcome != TransitionDeny {
		t.Fatalf("outcome = %v, want deny — an any-origin rule governs a create", got.Outcome)
	}
}

func TestCanApprove(t *testing.T) {
	rule := &TransitionRule{ID: "r", ApproverRoles: []string{"member"}}
	cases := []struct {
		name  string
		rule  *TransitionRule
		actor TransitionActor
		want  bool
	}{
		{"owner always", rule, member("owner"), true},
		{"admin always", rule, member("admin"), true},
		{"owner with no rule", nil, member("owner"), true},
		{"listed role", rule, member("member"), true},
		{"unlisted role", &TransitionRule{ID: "r"}, member("member"), false},
		{"an agent never approves", rule, agent("a-1"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanApprove(tc.rule, tc.actor); got != tc.want {
				t.Fatalf("CanApprove = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApproverRolesForAlwaysIncludesOwnerAndAdmin(t *testing.T) {
	got := ApproverRolesFor(&TransitionRule{ApproverRoles: []string{"member"}})
	want := []string{"admin", "member", "owner"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
	if got := ApproverRolesFor(nil); len(got) != 2 {
		t.Fatalf("roles for a nil rule = %v, want owner+admin", got)
	}
}
