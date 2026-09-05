package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Executable org chart (K75): a new workspace starts as an owner network; a
// project picks another model and both coexist; the Rule of Two refuses a
// structure and names the path; the Trust Dial caps a unit's autonomy;
// activation needs an owner, an eval attestation and a termination for a
// task force; a squads project routes by rule to the squad; a market takes
// two offers and picks the best under the cap; a hierarchy escalates up its
// edge under a quota and asks the superior above the risk threshold; a
// task force ends and drafts a postmortem; the breaker pauses a failing
// unit; the living org proposes once; the replay names the revision.

func orgUnit(id, name, owner string, members ...OrgMember) OrgUnit {
	return OrgUnit{ID: id, Name: name, OwnerID: owner, Excludes: []string{"external_effects"}, Autonomy: "draft", Allow: []string{"read"}, Members: members, Roles: []OrgRole{}}
}

func syncIssueCounter(t *testing.T) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
}

func orgCreate(t *testing.T, body map[string]any, want int) OrgStructureResponse {
	t.Helper()
	var out OrgStructureResponse
	res := testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", body)).Want(want)
	if want == http.StatusCreated {
		res.JSON(&out)
	}
	return out
}

func orgAction(t *testing.T, id, action string, body map[string]any, want int) OrgStructureResponse {
	t.Helper()
	var out OrgStructureResponse
	req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/org/"+id+"/"+action, body), "id", id, "action", action)
	res := testutil.Call(t, testHandler.SetOrgStructureStatus, req).Want(want)
	if want == http.StatusOK {
		res.JSON(&out)
	}
	return out
}

func TestOrgChart(t *testing.T) {
	ctx := context.Background()
	ws := parseUUID(testWorkspaceID)
	claude := providerRuntime(t, "claude")
	agentA := dbfx.Agent(t, "org agent A "+uuid.NewString()[:8], claude, testutil.Cols{"trust_mode": "autonomous"})
	agentB := dbfx.Agent(t, "org agent B "+uuid.NewString()[:8], claude, testutil.Cols{"trust_mode": "autonomous"})
	proposeAgent := dbfx.Agent(t, "org propose "+uuid.NewString()[:8], claude)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM org_flow WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM org_offer WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM org_revision WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM org_structure WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'org_alert'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3)`, agentA, agentB, proposeAgent)
		testPool.Exec(ctx, `DELETE FROM agent_domain_competency WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(ctx, `DELETE FROM postmortem WHERE workspace_id = $1 AND trigger = 'taskforce_dissolved'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE workspace_id = $1 AND question LIKE 'Hierarchy · %'`, testWorkspaceID)
	})
	testPool.Exec(ctx, `DELETE FROM org_structure WHERE workspace_id = $1`, testWorkspaceID)
	quietOtherAgents(t, agentA, agentB, proposeAgent)
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)

	// A new workspace starts as an active owner network.
	if err := testHandler.seedDefaultOrg(ctx, testHandler.Queries, ws, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	var templates struct{ Templates []OrgTemplate }
	testutil.Call(t, testHandler.ListOrgTemplates, newRequest(http.MethodGet, "/api/org/templates", nil)).Want(http.StatusOK).JSON(&templates)
	if len(templates.Templates) != 7 {
		t.Fatalf("seven models, got %d", len(templates.Templates))
	}
	var listed struct{ Structures []OrgStructureResponse }
	testutil.Call(t, testHandler.ListOrgStructures, newRequest(http.MethodGet, "/api/org", nil)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Structures) != 1 || listed.Structures[0].Model != "owner_network" || listed.Structures[0].Status != "active" || listed.Structures[0].ProjectID != nil {
		t.Fatalf("default structure: %+v", listed.Structures)
	}
	orgCreate(t, map[string]any{"model": "owner_network", "definition": OrgDefinition{Units: []OrgUnit{orgUnit("u", "U", testUserID)}}}, http.StatusConflict)

	// Rule of Two: a unit holding all three properties, then a path cumulating them.
	all := orgUnit("all", "Everything", testUserID)
	all.Excludes = []string{}
	all.Deciders = map[string]string{"money": testUserID, "outbound_data": testUserID, "external_message": testUserID}
	res := testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", map[string]any{"project_id": dbfx.Project(t, "org p0"), "model": "hierarchy", "definition": OrgDefinition{Units: []OrgUnit{all}}})).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "Rule of Two: unit") || !strings.Contains(res.Body.String(), "Everything") {
		t.Fatalf("rule of two per unit: %s", res.Body.String())
	}
	reader := orgUnit("reader", "Reader", testUserID)
	reader.Excludes = []string{"external_effects"}
	actor := orgUnit("actor", "Actor", testUserID)
	actor.Excludes = []string{"untrusted_input", "sensitive_data"}
	actor.Deciders = map[string]string{"money": testUserID, "outbound_data": testUserID, "external_message": testUserID}
	res = testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", map[string]any{"project_id": dbfx.Project(t, "org p1"), "model": "hierarchy", "definition": OrgDefinition{Units: []OrgUnit{reader, actor}, Edges: []OrgEdge{{From: "reader", To: "actor", Kind: "reports_to"}}}})).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "path Reader → Actor") {
		t.Fatalf("rule of two per path: %s", res.Body.String())
	}
	// The Trust Dial caps a unit: auto for an agent on propose is refused.
	auto := orgUnit("auto", "Auto", testUserID, OrgMember{Type: "agent", ID: proposeAgent})
	auto.Autonomy = "auto"
	res = testutil.Call(t, testHandler.CreateOrgStructure, newRequest(http.MethodPost, "/api/org", map[string]any{"project_id": dbfx.Project(t, "org p2"), "model": "squads", "definition": OrgDefinition{Units: []OrgUnit{auto}}})).Want(http.StatusUnprocessableEntity)
	if !strings.Contains(res.Body.String(), "trust dial is propose") {
		t.Fatalf("trust dial ceiling: %s", res.Body.String())
	}

	// Squads: a project structure coexists with the default; a rule routes to the squad.
	squad := dbfx.Squad(t, "org squad "+uuid.NewString()[:8], agentA)
	squadProject := dbfx.Project(t, "org squads project")
	unit := orgUnit("billing", "Billing squad", testUserID)
	unit.SquadID = squad
	s := orgCreate(t, map[string]any{"project_id": squadProject, "model": "squads", "name": "Squads", "definition": OrgDefinition{Units: []OrgUnit{unit}, Rules: []OrgRule{{ID: "r1", Keywords: []string{"invoice"}, TargetUnit: "billing", Priority: 1}}}}, http.StatusCreated)
	if s.Status != "draft" || s.Revision != 1 {
		t.Fatalf("created structure: %+v", s)
	}
	// Activation needs the eval attestation.
	orgAction(t, s.ID, "activate", map[string]any{}, http.StatusUnprocessableEntity)
	s = orgAction(t, s.ID, "activate", map[string]any{"eval_attestation": "eval run #12: 30 cases, 0 failures"}, http.StatusOK)
	if s.Status != "active" || s.Revision != 2 {
		t.Fatalf("activated: %+v", s)
	}
	var created struct {
		ID           string  `json:"id"`
		AssigneeType *string `json:"assignee_type"`
		AssigneeID   *string `json:"assignee_id"`
	}
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Fix the invoice export", "project_id": squadProject})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	routed := mustIssue(t, created.ID)
	if routed.AssigneeType.String != "squad" || uuidToString(routed.AssigneeID) != squad {
		t.Fatalf("squad routing: %s/%s", routed.AssigneeType.String, uuidToString(routed.AssigneeID))
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM org_flow WHERE structure_id = $1 AND kind = 'routing' AND issue_id = $2`, s.ID, created.ID) != 1 {
		t.Fatal("routing recorded as a flow")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, created.ID, agentA) != 1 {
		t.Fatal("the squad leader's run starts")
	}
	// An issue that matches no rule in a squads project stays unrouted; the default structure does not interfere.
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Unrelated note", "project_id": squadProject})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	if created.AssigneeID != nil {
		t.Fatal("no rule, no squad, no assignee")
	}
	// The run's context names the structure, the unit and the revision; the replay records it.
	octx := testHandler.resolveClaimOrgContext(ctx, routed, parseUUID(agentA))
	if octx == nil || octx.UnitName != "Billing squad" || octx.Revision != 2 || !containsStr(octx.Deny, "commit_money") {
		t.Fatalf("org context: %+v", octx)
	}
	leaderTask := dbfx.Task(t, agentA, testutil.Cols{"runtime_id": claude, "issue_id": routed.ID, "status": "completed"})
	testHandler.recordRunSnapshot(ctx, mustTask(t, leaderTask), testWorkspaceID)
	var rev string
	dbfx.QueryRow(t, `SELECT details->>'org_revision_id' FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.started' ORDER BY occurred_at DESC LIMIT 1`, leaderTask).Scan(&rev)
	if rev == "" || s.RevisionID == nil || rev != *s.RevisionID {
		t.Fatalf("replay snapshot revision = %q, want %v", rev, s.RevisionID)
	}

	// Market: two agents bid; the higher confidence under the cap wins.
	marketProject := dbfx.Project(t, "org market project")
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentA, "domain_key": competencyDomainGeneral, "success_count": 2, "total_count": 10})
	dbfx.Insert(t, "agent_domain_competency", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentB, "domain_key": competencyDomainGeneral, "success_count": 9, "total_count": 10})
	market := orgUnit("market", "Market", testUserID, OrgMember{Type: "agent", ID: agentA}, OrgMember{Type: "agent", ID: agentB})
	m := orgCreate(t, map[string]any{"project_id": marketProject, "model": "market", "definition": OrgDefinition{Units: []OrgUnit{market}, Market: OrgMarket{PriceCapUsdTicks: 1_000_000}}}, http.StatusCreated)
	orgAction(t, m.ID, "activate", map[string]any{"eval_attestation": "eval run #13"}, http.StatusOK)
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Market issue", "project_id": marketProject})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	if created.AssigneeID == nil || *created.AssigneeID != agentB {
		t.Fatalf("market winner = %v, want %s", created.AssigneeID, agentB)
	}
	var offers struct {
		Offers []struct {
			AgentID string `json:"agent_id"`
			Status  string `json:"status"`
		} `json:"offers"`
	}
	testutil.Call(t, testHandler.ListIssueOrgOffers, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+created.ID+"/org-offers", nil), "id", created.ID)).Want(http.StatusOK).JSON(&offers)
	won, lost := 0, 0
	for _, o := range offers.Offers {
		switch o.Status {
		case "won":
			won++
		case "lost":
			lost++
		}
	}
	if len(offers.Offers) != 2 || won != 1 || lost != 1 {
		t.Fatalf("offers: %+v", offers.Offers)
	}
	// Fewer offers than min_offers: the owner is told, nobody is assigned.
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentB)
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Market issue two", "project_id": marketProject})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	dbfx.Exec(t, `UPDATE agent SET archived_at = NULL WHERE id = $1`, agentB)
	if created.AssigneeID != nil || dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'org_alert' AND title LIKE 'Market: not enough offers%'`, testWorkspaceID) != 1 {
		t.Fatal("a short market leaves the issue to the owner")
	}

	// Hierarchy: routed to the team; escalation goes up the edge under a quota;
	// above the risk threshold the superior approves first.
	hierProject := dbfx.Project(t, "org hierarchy project")
	lead := orgUnit("lead", "Lead", testUserID)
	team := orgUnit("team", "Team", testUserID, OrgMember{Type: "agent", ID: agentA})
	team.EscalationQuotaPerDay = 1
	team.ApprovalRisk = "high"
	hier := orgCreate(t, map[string]any{"project_id": hierProject, "model": "hierarchy", "definition": OrgDefinition{Units: []OrgUnit{lead, team}, Edges: []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}}, Rules: []OrgRule{{ID: "r", Paths: []string{"*"}, TargetUnit: "team", Priority: 1}}}}, http.StatusCreated)
	orgAction(t, hier.ID, "activate", map[string]any{"eval_attestation": "eval run #14"}, http.StatusOK)
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Team work", "project_id": hierProject})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	if created.AssigneeID == nil || *created.AssigneeID != agentA {
		t.Fatalf("hierarchy routes to the team agent: %v", created.AssigneeID)
	}
	var esc struct {
		Issue  IssueResponse `json:"issue"`
		ToUnit string        `json:"to_unit"`
	}
	testutil.Call(t, testHandler.EscalateIssue, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+created.ID+"/escalate", map[string]any{"reason": "out of scope"}), "id", created.ID)).Want(http.StatusOK).JSON(&esc)
	if esc.ToUnit != "lead" || esc.Issue.AssigneeType == nil || *esc.Issue.AssigneeType != "member" || *esc.Issue.AssigneeID != testUserID {
		t.Fatalf("escalation: %+v", esc)
	}
	second := created
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Team work two", "project_id": hierProject})).Want(http.StatusCreated).JSON(&second)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, second.ID) })
	testutil.Call(t, testHandler.EscalateIssue, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+second.ID+"/escalate", map[string]any{"reason": "again"}), "id", second.ID)).Want(http.StatusTooManyRequests)
	risky := dbfx.Issue(t, "Risky team work", testutil.Cols{"project_id": hierProject, "contract_risk": "high"})
	testHandler.orgRouteIssue(ctx, mustIssue(t, risky), "member", testUserID)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1 AND assignee_id IS NOT NULL`, risky) != 0 {
		t.Fatal("a high-risk issue waits for the superior")
	}
	var decisionID, optionsRaw string
	dbfx.QueryRow(t, `SELECT id::text, options::text FROM issue_decision WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, risky).Scan(&decisionID, &optionsRaw)
	var opts []DecisionOption
	_ = json.Unmarshal([]byte(optionsRaw), &opts)
	if len(opts) != 2 || !strings.HasPrefix(opts[0].ID, "org_assign:") {
		t.Fatalf("approval decision options: %s", optionsRaw)
	}
	respondDecision(t, risky, decisionID, map[string]any{"option_id": opts[0].ID}).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1 AND assignee_id = $2`, risky, agentA) != 1 {
		t.Fatal("the superior's approval assigns the unit")
	}
	// The living org: the team escalated at twice its quota → a proposal, filed once.
	dbfx.Insert(t, "org_flow", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "structure_id": hier.ID, "unit_id": "team", "kind": "escalation", "details": "{}"})
	hierRow, _ := testHandler.Queries.GetOrgStructure(ctx, db.GetOrgStructureParams{ID: parseUUID(hier.ID), WorkspaceID: ws})
	health := testHandler.orgHealth(ctx, hierRow)
	found := false
	for _, p := range health.Proposals {
		if p.Key == "escalations:team" {
			found = true
		}
	}
	if !found || health.Escalations < 2 {
		t.Fatalf("health: %+v", health)
	}
	if n := testHandler.orgProposeRestructurings(ctx, hierRow, health); n < 1 {
		t.Fatal("proposal filed to the owner")
	}
	if n := testHandler.orgProposeRestructurings(ctx, hierRow, health); n != 0 {
		t.Fatal("the same proposal is not filed twice in a day")
	}
	// The breaker: four failed runs in a day pause the team; routing then skips it.
	for i := 0; i < 4; i++ {
		dbfx.Task(t, agentA, testutil.Cols{"runtime_id": claude, "issue_id": risky, "status": "failed"})
	}
	if !testHandler.orgBreaker(ctx, hierRow, time.Now()) {
		t.Fatal("breaker trips on a failing unit")
	}
	var third struct {
		ID         string  `json:"id"`
		AssigneeID *string `json:"assignee_id"`
	}
	syncIssueCounter(t)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "Team work three", "project_id": hierProject})).Want(http.StatusCreated).JSON(&third)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, third.ID) })
	if third.AssigneeID == nil || *third.AssigneeID != testUserID {
		t.Fatalf("a paused unit receives nothing; the root lead takes over: %v", third.AssigneeID)
	}
	var detail struct {
		Structure OrgStructureResponse `json:"structure"`
		Revisions []map[string]any     `json:"revisions"`
	}
	testutil.Call(t, testHandler.GetOrgStructure, testutil.WithURLParams(newRequest(http.MethodGet, "/api/org/"+hier.ID, nil), "id", hier.ID)).Want(http.StatusOK).JSON(&detail)
	if len(detail.Structure.PausedUnits) != 1 || detail.Structure.PausedUnits[0] != "team" || len(detail.Revisions) != 2 {
		t.Fatalf("paused units / revisions: %+v", detail)
	}

	// Task force: termination is mandatory; at its end it dissolves, drafts a postmortem and tells the owner.
	tfProject := dbfx.Project(t, "org taskforce project")
	tf := orgCreate(t, map[string]any{"project_id": tfProject, "model": "taskforce", "definition": OrgDefinition{Units: []OrgUnit{orgUnit("tf", "Task force", testUserID, OrgMember{Type: "agent", ID: agentA})}}}, http.StatusCreated)
	orgAction(t, tf.ID, "activate", map[string]any{"eval_attestation": "eval run #15"}, http.StatusUnprocessableEntity)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	testutil.Call(t, testHandler.UpdateOrgStructure, testutil.WithURLParams(newRequest(http.MethodPut, "/api/org/"+tf.ID, map[string]any{"dissolve_at": past}), "id", tf.ID)).Want(http.StatusOK)
	orgAction(t, tf.ID, "activate", map[string]any{"eval_attestation": "eval run #15"}, http.StatusOK)
	tfIssue := dbfx.Issue(t, "task force work", testutil.Cols{"project_id": tfProject})
	dbfx.Task(t, agentA, testutil.Cols{"runtime_id": claude, "issue_id": tfIssue, "status": "completed", "completed_at": testutil.Raw("now()")})
	if n, err := testHandler.TickOrgStructures(ctx, time.Now()); err != nil || n < 1 {
		t.Fatalf("tick: n=%d err=%v", n, err)
	}
	var tfStatus string
	dbfx.QueryRow(t, `SELECT status FROM org_structure WHERE id = $1`, tf.ID).Scan(&tfStatus)
	if tfStatus != "dissolved" {
		t.Fatalf("task force status = %s", tfStatus)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM postmortem WHERE workspace_id = $1 AND trigger = 'taskforce_dissolved'`, testWorkspaceID) != 1 {
		t.Fatal("dissolution drafts a postmortem")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'org_alert' AND title LIKE 'Dissolved:%'`, testWorkspaceID) != 1 {
		t.Fatal("the owner is told")
	}
	// A dissolved structure is history; the project can start a new one.
	testutil.Call(t, testHandler.UpdateOrgStructure, testutil.WithURLParams(newRequest(http.MethodPut, "/api/org/"+tf.ID, map[string]any{"name": "x"}), "id", tf.ID)).Want(http.StatusConflict)
	orgCreate(t, map[string]any{"project_id": tfProject, "model": "owner_network", "definition": OrgDefinition{Units: []OrgUnit{orgUnit("o", "Owners", testUserID)}}}, http.StatusCreated)

	// Project template seeds a draft structure.
	var project struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.CreateProject, newRequest(http.MethodPost, "/api/projects", map[string]any{"title": "org templated project", "org_template": "circles"})).Want(http.StatusCreated).JSON(&project)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, project.ID) })
	var seeded string
	dbfx.QueryRow(t, `SELECT COALESCE((SELECT model FROM org_structure WHERE project_id = $1), '')`, project.ID).Scan(&seeded)
	if seeded != "circles" {
		t.Fatalf("project template seeded %q", seeded)
	}
	// Preflight names the coordination cost before activation.
	var pf map[string]any
	testutil.Call(t, testHandler.PreflightOrgStructure, testutil.WithURLParams(newRequest(http.MethodGet, "/api/org/"+hier.ID+"/preflight", nil), "id", hier.ID)).Want(http.StatusOK).JSON(&pf)
	if pf["pattern"] != "supervisor" || pf["coordination_runs_per_issue"] != 1.0 {
		t.Fatalf("preflight: %+v", pf)
	}
}
