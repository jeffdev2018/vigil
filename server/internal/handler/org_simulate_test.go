package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// POST /api/org/simulate (K75): the same routing the live path runs, against
// a draft or against the revision in force, and writing nothing. What is
// checked here: a draft answers differently from the revision it edits; a
// unit's refusal shows up when the request names it and the decision moves
// up; the escalation ladder comes back whole and ordered; the matrix picks
// the competent agent off the measured competency; a circle claims the
// request its role's keywords name; another workspace's structure is not
// found and an invalid definition is refused with the save's own message;
// and a simulation leaves org_flow and the run queue untouched.

func orgSimulate(t *testing.T, body map[string]any) OrgSimulation {
	t.Helper()
	var out OrgSimulation
	testutil.Call(t, testHandler.SimulateOrgRequest, newRequest(http.MethodPost, "/api/org/simulate", body)).Want(http.StatusOK).JSON(&out)
	return out
}

func orgSimAgent(t *testing.T, label string) string {
	t.Helper()
	return dbfx.Agent(t, "org sim "+label+" "+uuid.NewString()[:8], providerRuntime(t, "claude"), testutil.Cols{"trust_mode": "autonomous"})
}

func orgSimRequest(title string, labels ...string) map[string]any {
	req := map[string]any{"title": title}
	if len(labels) > 0 {
		req["labels"] = labels
	}
	return req
}

// A draft answers for the edit, the revision for what is in force.
func TestOrgSimulateDraftVersusRevision(t *testing.T) {
	agentA, agentB := orgSimAgent(t, "A"), orgSimAgent(t, "B")
	alpha := orgUnit("alpha", "Alpha", testUserID, OrgMember{Type: "agent", ID: agentA})
	beta := orgUnit("beta", "Beta", testUserID, OrgMember{Type: "agent", ID: agentB})
	rules := []OrgRule{{ID: "r1", Keywords: []string{"export"}, TargetUnit: "alpha", Priority: 1}}
	s := orgCreate(t, map[string]any{
		"project_id": dbfx.Project(t, "org sim project "+uuid.NewString()[:8]), "model": "owner_network", "name": "Sim owners",
		"definition": OrgDefinition{Units: []OrgUnit{alpha, beta}, Rules: rules},
	}, http.StatusCreated)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_revision WHERE structure_id = $1`, s.ID)
		testPool.Exec(context.Background(), `DELETE FROM org_structure WHERE id = $1`, s.ID)
	})

	live := orgSimulate(t, map[string]any{"structure_id": s.ID, "request": orgSimRequest("Fix the export")})
	if live.Basis != "revision" || live.StructureID != s.ID || live.Revision != s.Revision {
		t.Fatalf("revision basis: %+v", live)
	}
	if live.Receives == nil || live.Receives.UnitID != "alpha" || live.Prepares.Kind != "agent" || live.Prepares.ID != agentA {
		t.Fatalf("the revision routes to alpha and agent A: %+v %+v", live.Receives, live.Prepares)
	}
	if live.Prepares.Name == "" {
		t.Fatal("the report names who prepares, not only its id")
	}

	// The same request against a draft that retargets the rule.
	retargeted := []OrgRule{{ID: "r1", Keywords: []string{"export"}, TargetUnit: "beta", Priority: 1}}
	draft := orgSimulate(t, map[string]any{
		"structure_id": s.ID, "model": "owner_network",
		"definition": OrgDefinition{Units: []OrgUnit{alpha, beta}, Rules: retargeted},
		"request":    orgSimRequest("Fix the export"),
	})
	if draft.Basis != "draft" {
		t.Fatalf("a definition wins over the revision: %+v", draft)
	}
	if draft.Receives == nil || draft.Receives.UnitID != "beta" || draft.Prepares.ID != agentB {
		t.Fatalf("the draft routes to beta and agent B: %+v %+v", draft.Receives, draft.Prepares)
	}

	// And against a draft that moves the agent instead of the rule.
	moved := orgUnit("alpha", "Alpha", testUserID, OrgMember{Type: "agent", ID: agentB})
	swapped := orgSimulate(t, map[string]any{
		"structure_id": s.ID, "model": "owner_network",
		"definition": OrgDefinition{Units: []OrgUnit{moved, beta}, Rules: rules},
		"request":    orgSimRequest("Fix the export"),
	})
	if swapped.Receives == nil || swapped.Receives.UnitID != "alpha" || swapped.Prepares.ID != agentB {
		t.Fatalf("the moved agent prepares in the same unit: %+v %+v", swapped.Receives, swapped.Prepares)
	}
}

// A refusal the request runs into, and the decision one level up.
func TestOrgSimulateBlockingDeny(t *testing.T) {
	support := orgUnit("support", "Support", testUserID, OrgMember{Type: "agent", ID: orgSimAgent(t, "support")})
	support.Deny = []string{"rembourser"}
	direction := orgUnit("direction", "Direction", testUserID)
	direction.Deciders = map[string]string{"money": testUserID}

	out := orgSimulate(t, map[string]any{
		"model": "hierarchy",
		"definition": OrgDefinition{
			Units: []OrgUnit{support, direction},
			Edges: []OrgEdge{{From: "support", To: "direction", Kind: "reports_to"}},
			Rules: []OrgRule{{ID: "r1", Keywords: []string{"rembourser"}, TargetUnit: "support", Priority: 1}},
		},
		"request": orgSimRequest("Rembourser le client Dupont"),
	})
	if out.Receives == nil || out.Receives.UnitID != "support" {
		t.Fatalf("the rule routes to support: %+v", out.Receives)
	}
	if !containsStr(out.BlockingDenies, "rembourser") {
		t.Fatalf("the unit's own refusal blocks this request: %+v", out.BlockingDenies)
	}
	// The non-negotiables are merged in at validation but say nothing here.
	if containsStr(out.BlockingDenies, "touch_secrets") {
		t.Fatalf("a refusal the request does not name must stay out: %+v", out.BlockingDenies)
	}
	if out.Decides.Kind != "member" || out.Decides.ID != testUserID {
		t.Fatalf("the superior decides when the unit names nobody: %+v", out.Decides)
	}
}

// Three levels, so the ladder is more than one hop.
func TestOrgSimulateEscalationPath(t *testing.T) {
	team := orgUnit("team", "Team", testUserID, OrgMember{Type: "agent", ID: orgSimAgent(t, "team")})
	out := orgSimulate(t, map[string]any{
		"model": "hierarchy",
		"definition": OrgDefinition{
			Units: []OrgUnit{team, orgUnit("lead", "Lead", testUserID), orgUnit("exec", "Exec", testUserID)},
			Edges: []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}, {From: "lead", To: "exec", Kind: "reports_to"}},
			Rules: []OrgRule{{ID: "r1", Keywords: []string{"deploy"}, TargetUnit: "team", Priority: 1}},
		},
		"request": orgSimRequest("Deploy the new pricing page"),
	})
	if len(out.EscalationPath) != 2 || out.EscalationPath[0].UnitID != "lead" || out.EscalationPath[1].UnitID != "exec" {
		t.Fatalf("escalation path: %+v", out.EscalationPath)
	}
	if out.EscalationPath[0].UnitName != "Lead" {
		t.Fatalf("the path names its units: %+v", out.EscalationPath)
	}
}

// The matrix reads the measured competency for the request's domain.
func TestOrgSimulateMatrixCompetency(t *testing.T) {
	weak, strong := orgSimAgent(t, "weak"), orgSimAgent(t, "strong")
	domain := competencyDomainKey([]string{"billing"}, nil)
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": weak, "domain_key": domain, "success_count": 1, "total_count": 10})
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": strong, "domain_key": domain, "success_count": 9, "total_count": 10})

	pool := orgUnit("pool", "Competence pool", testUserID, OrgMember{Type: "agent", ID: weak}, OrgMember{Type: "agent", ID: strong})
	out := orgSimulate(t, map[string]any{
		"model": "matrix",
		"definition": OrgDefinition{
			Units: []OrgUnit{orgUnit("intake", "Intake", testUserID), pool},
			Rules: []OrgRule{{ID: "r1", Keywords: []string{"invoice"}, TargetUnit: "pool", Priority: 1}},
		},
		"request": orgSimRequest("The invoice total is wrong", "billing"),
	})
	if out.Receives == nil || out.Receives.UnitID != "pool" {
		t.Fatalf("the rule beats the matrix fallback on the first unit: %+v", out.Receives)
	}
	if out.Prepares.Kind != "agent" || out.Prepares.ID != strong {
		t.Fatalf("the competent agent prepares, got %+v (want %s)", out.Prepares, strong)
	}
}

// A circle claims the request its role's keywords name, with no rule at all.
func TestOrgSimulateCircleRouting(t *testing.T) {
	sales := orgUnit("sales", "Sales circle", testUserID, OrgMember{Type: "agent", ID: orgSimAgent(t, "sales")})
	sales.Roles = []OrgRole{{ID: "closer", Name: "Closer", Keywords: []string{"contract"}}}
	support := orgUnit("support", "Support circle", testUserID, OrgMember{Type: "agent", ID: orgSimAgent(t, "support")})
	support.Roles = []OrgRole{{ID: "triager", Name: "Triager", Keywords: []string{"crash"}}}

	out := orgSimulate(t, map[string]any{
		"model":      "circles",
		"definition": OrgDefinition{Units: []OrgUnit{sales, support}},
		"request":    orgSimRequest("The login screen crashes on iOS"),
	})
	if out.Receives == nil || out.Receives.UnitID != "support" {
		t.Fatalf("the role's keyword claims the request: %+v", out.Receives)
	}
	if out.Unit == nil || out.Unit.Model != "circles" {
		t.Fatalf("the unit reports the model it runs under: %+v", out.Unit)
	}
	// Nothing in the structure names this one, and circles has no fallback.
	none := orgSimulate(t, map[string]any{
		"model":      "circles",
		"definition": OrgDefinition{Units: []OrgUnit{sales, support}},
		"request":    orgSimRequest("Reorder the office plants"),
	})
	if none.Receives != nil || none.Prepares.Kind != "none" || len(none.Notes) == 0 {
		t.Fatalf("an unrouted request says so: %+v", none)
	}
}

// Another workspace's structure is not found; an invalid definition comes
// back with the message the save would have given.
func TestOrgSimulateTenantAndValidation(t *testing.T) {
	otherWs := dbfx.Workspace(t, "Org sim other", "org-sim-other-"+uuid.NewString()[:8])
	foreign := dbfx.Insert(t, "org_structure", testutil.Cols{"id": uuid.NewString(), "workspace_id": otherWs, "model": "owner_network", "name": "Foreign", "definition": `{"units":[]}`})
	testutil.Call(t, testHandler.SimulateOrgRequest, newRequest(http.MethodPost, "/api/org/simulate", map[string]any{
		"structure_id": foreign, "request": orgSimRequest("Anything"),
	})).Want(http.StatusNotFound)

	// Neither a definition nor a structure: nothing to simulate against.
	testutil.Call(t, testHandler.SimulateOrgRequest, newRequest(http.MethodPost, "/api/org/simulate", map[string]any{
		"request": orgSimRequest("Anything"),
	})).Want(http.StatusBadRequest)

	// The save's own validation, with the save's own message.
	bad := orgUnit("all", "Everything", testUserID)
	bad.Excludes = []string{}
	bad.Deciders = map[string]string{"money": testUserID, "outbound_data": testUserID, "external_message": testUserID}
	res := testutil.Call(t, testHandler.SimulateOrgRequest, newRequest(http.MethodPost, "/api/org/simulate", map[string]any{
		"model": "hierarchy", "definition": OrgDefinition{Units: []OrgUnit{bad}}, "request": orgSimRequest("Anything"),
	})).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "Rule of Two: unit") {
		t.Fatalf("the simulation refuses what the save refuses: %s", res.Body.String())
	}
}

// A simulation is a question, not an act.
func TestOrgSimulateWritesNothing(t *testing.T) {
	agent := orgSimAgent(t, "silent")
	before := dbfx.Count(t, `SELECT COUNT(*) FROM org_flow WHERE workspace_id = $1`, testWorkspaceID)
	queued := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1`, agent)

	out := orgSimulate(t, map[string]any{
		"model": "hierarchy",
		"definition": OrgDefinition{
			Units: []OrgUnit{orgUnit("team", "Team", testUserID, OrgMember{Type: "agent", ID: agent}), orgUnit("lead", "Lead", testUserID)},
			Edges: []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}},
			Rules: []OrgRule{{ID: "r1", Keywords: []string{"migrate"}, TargetUnit: "team", Priority: 1}},
		},
		"request": map[string]any{"title": "Migrate the billing job", "keywords": []string{"migrate"}},
	})
	if out.Prepares.ID != agent {
		t.Fatalf("the agent would have taken it: %+v", out.Prepares)
	}
	if out.CostEstimateUsdTicks != 0 || len(out.Notes) == 0 {
		t.Fatalf("no structure, no observed spend, and the report says so: %+v", out)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM org_flow WHERE workspace_id = $1`, testWorkspaceID); n != before {
		t.Fatalf("org_flow rows %d -> %d", before, n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1`, agent); n != queued {
		t.Fatalf("queued runs %d -> %d", queued, n)
	}
}

// A refusal is matched by stem, not by exact substring: French inflects
// ("rembourser" / "remboursement"), and hyphens vary ("e-mail" / "email").
func TestOrgBlockingDeniesMatchByStem(t *testing.T) {
	deny := []string{"rembourser", "envoyer e-mail externe", "virement"}
	got := orgBlockingDenies(deny, "Un client demande le remboursement de sa commande 4512\nreçue abîmée")
	if len(got) != 1 || got[0] != "rembourser" {
		t.Fatalf("blocking denies = %v, want [rembourser]", got)
	}
	got = orgBlockingDenies(deny, "Envoyer un email à un client externe pour confirmer")
	if len(got) != 1 || got[0] != "envoyer e-mail externe" {
		t.Fatalf("blocking denies = %v, want [envoyer e-mail externe]", got)
	}
	if got = orgBlockingDenies(deny, "Préparer la relance de la facture 88"); len(got) != 0 {
		t.Fatalf("blocking denies = %v, want none", got)
	}
}
