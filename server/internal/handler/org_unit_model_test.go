package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Per-unit models (K75 follow-up): a unit runs its own model inside the
// structure's — a hierarchy holds a market team next to a squad. The model
// inherits down reports_to; validation applies each unit's constraints under
// its effective model and refuses a task-force unit; routing picks the unit
// by rule, then the unit's model decides how it takes the issue; the run's
// context names the unit model; the composite template ships the whole shape.

func TestOrgEffectiveModelInherits(t *testing.T) {
	t.Parallel()
	def := OrgDefinition{
		Units: []OrgUnit{{ID: "lead"}, {ID: "team", Model: OrgModelMarket}, {ID: "sub"}, {ID: "loop-a"}, {ID: "loop-b"}},
		Edges: []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}, {From: "sub", To: "team", Kind: "reports_to"}, {From: "loop-a", To: "loop-b", Kind: "reports_to"}, {From: "loop-b", To: "loop-a", Kind: "reports_to"}},
	}
	for id, want := range map[string]string{"lead": OrgModelHierarchy, "team": OrgModelMarket, "sub": OrgModelMarket, "loop-a": OrgModelHierarchy, "missing": OrgModelHierarchy} {
		if got := orgEffectiveModel(&def, id, OrgModelHierarchy); got != want {
			t.Errorf("%s: effective model = %s, want %s", id, got, want)
		}
	}
}

func TestOrgUnitModelComposition(t *testing.T) {
	ctx := context.Background()
	claude := providerRuntime(t, "claude")
	agentA := dbfx.Agent(t, "org unit A "+uuid.NewString()[:8], claude, testutil.Cols{"trust_mode": "autonomous"})
	agentB := dbfx.Agent(t, "org unit B "+uuid.NewString()[:8], claude, testutil.Cols{"trust_mode": "autonomous"})
	project := dbfx.Project(t, "org composed project")
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM org_flow WHERE workspace_id = $1 AND structure_id IN (SELECT id FROM org_structure WHERE project_id = $2)`, testWorkspaceID, project)
		testPool.Exec(ctx, `DELETE FROM org_offer WHERE workspace_id = $1 AND structure_id IN (SELECT id FROM org_structure WHERE project_id = $2)`, testWorkspaceID, project)
		testPool.Exec(ctx, `DELETE FROM org_revision WHERE workspace_id = $1 AND structure_id IN (SELECT id FROM org_structure WHERE project_id = $2)`, testWorkspaceID, project)
		testPool.Exec(ctx, `DELETE FROM org_structure WHERE workspace_id = $1 AND project_id = $2`, testWorkspaceID, project)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(ctx, `DELETE FROM agent_domain_competency WHERE agent_id IN ($1, $2)`, agentA, agentB)
	})
	quietOtherAgents(t, agentA, agentB)
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentA, "domain_key": competencyDomainGeneral, "success_count": 2, "total_count": 10})
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentB, "domain_key": competencyDomainGeneral, "success_count": 9, "total_count": 10})

	lead := orgUnit("lead", "Lead", testUserID)
	team := orgUnit("team", "Market team", testUserID, OrgMember{Type: "agent", ID: agentA}, OrgMember{Type: "agent", ID: agentB})
	team.Model = OrgModelMarket
	edges := []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}}
	rules := []OrgRule{{ID: "r", Keywords: []string{"invoice"}, TargetUnit: "team", Priority: 1}}
	body := func(units []OrgUnit, market OrgMarket) map[string]any {
		return map[string]any{"project_id": project, "model": "hierarchy", "name": "Composed", "definition": OrgDefinition{Units: units, Edges: edges, Rules: rules, Market: market}}
	}

	// A task force is a structure model: it declares its termination on the structure, so a unit cannot be one.
	tf := team
	tf.Model = OrgModelTaskforce
	res := testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", body([]OrgUnit{lead, tf}, OrgMarket{}))).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "taskforce is a structure model") {
		t.Fatalf("task-force unit refused with the reason: %s", res.Body.String())
	}
	// A market unit needs the price cap, even inside a hierarchy.
	res = testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", body([]OrgUnit{lead, team}, OrgMarket{}))).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "price cap") {
		t.Fatalf("market unit needs the cap: %s", res.Body.String())
	}
	s := orgCreate(t, body([]OrgUnit{lead, team}, OrgMarket{PriceCapUsdTicks: 1_000_000}), http.StatusCreated)
	if len(s.Definition.Units) != 2 || s.Definition.Units[1].Model != OrgModelMarket {
		t.Fatalf("unit model round-trips: %+v", s.Definition.Units)
	}
	orgAction(t, s.ID, "activate", map[string]any{"eval_attestation": "eval run #21"}, http.StatusOK)

	// The rule routes to the team; the team's model (market) decides who takes it: the best offer.
	syncIssueCounter(t)
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Fix the invoice export", "project_id": project})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	if created.AssigneeID == nil || *created.AssigneeID != agentB {
		t.Fatalf("market team winner = %v, want %s", created.AssigneeID, agentB)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM org_offer WHERE issue_id = $1`, created.ID); n != 2 {
		t.Fatalf("offers on the composed issue = %d, want 2", n)
	}
	// Unrouted issues fall back to the hierarchy's root, taken by its owner.
	syncIssueCounter(t)
	var other IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Something else", "project_id": project})).Want(http.StatusCreated).JSON(&other)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, other.ID) })
	if other.AssigneeType == nil || *other.AssigneeType != "member" || *other.AssigneeID != testUserID {
		t.Fatalf("root takes the unrouted issue: %v %v", other.AssigneeType, other.AssigneeID)
	}
	// The run's context names the unit's effective model.
	if oc := testHandler.resolveClaimOrgContext(ctx, mustIssue(t, created.ID), parseUUID(agentB)); oc == nil || oc.Model != OrgModelHierarchy || oc.UnitModel != OrgModelMarket || oc.UnitID != "team" {
		t.Fatalf("org context: %+v", oc)
	}
	// Preflight averages the units' models rather than reading the structure's.
	var pre struct {
		Runs   float64 `json:"coordination_runs_per_issue"`
		Review float64 `json:"human_review_items_per_issue"`
	}
	testutil.Call(t, testHandler.PreflightOrgStructure, testutil.WithURLParams(newRequest(http.MethodGet, "/api/org/"+s.ID+"/preflight", nil), "id", s.ID)).Want(http.StatusOK).JSON(&pre)
	if pre.Runs != 0.5 || pre.Review != 0.4 {
		t.Fatalf("preflight averages hierarchy (1 run, 0.5 review) and market (0, 0.3): %+v", pre)
	}
	// The composite template ships the shape, valid for this workspace.
	var tpls struct {
		Templates []OrgTemplate `json:"templates"`
	}
	testutil.Call(t, testHandler.ListOrgTemplates, newRequest(http.MethodGet, "/api/org/templates", nil)).Want(http.StatusOK).JSON(&tpls)
	var composite *OrgTemplate
	for i := range tpls.Templates {
		if tpls.Templates[i].Composite {
			composite = &tpls.Templates[i]
		}
	}
	if composite == nil || composite.Model != OrgModelHierarchy || len(composite.Definition.Units) != 4 {
		t.Fatalf("composite template: %+v", composite)
	}
	def := composite.Definition
	if err := testHandler.validateOrg(ctx, parseUUID(testWorkspaceID), composite.Model, &def); err != nil {
		t.Fatalf("composite template validates: %v", err)
	}
}
