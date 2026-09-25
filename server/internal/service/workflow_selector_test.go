package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/jackc/pgx/v5/pgtype"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workflow selector tests (JEF-273). The selector sits above the runtime
// router: it picks single / cascade / critique per issue task from the
// workspace policy and the per-(task_class, workflow) run history, with the
// same anti-hallucination discipline as the router — no data, no behavior
// change.

// setWorkflowPolicy switches the fixture workspace's workflow_policy mode.
func setWorkflowPolicy(t *testing.T, fx *testutil.Fixture, workspaceID, mode string) {
	t.Helper()
	fx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb)
		|| jsonb_build_object('workflow_policy', jsonb_build_object('mode', $2::text))
		WHERE id = $1`, workspaceID, mode)
}

// seedWorkflowRuns inserts n terminal tasks stamped with the given workflow in
// their context, `successes` of them completed, in the given task class. Cost
// is seeded through one task_usage row per run; pass costTicks = -1 for runs
// with no usage row at all (they still count as workflow samples but are
// invisible to the routing stats, which require a usage row).
func seedWorkflowRuns(t *testing.T, fx *testutil.Fixture, agentID, runtimeID, taskClass, workflow string, samples, successes int, costTicks int64) {
	t.Helper()
	ctx := fmt.Sprintf(`{"workflow":%q}`, workflow)
	for i := 0; i < samples; i++ {
		status := "failed"
		if i < successes {
			status = "completed"
		}
		taskID := fx.Task(t, agentID, testutil.Cols{
			"runtime_id":   runtimeID,
			"status":       status,
			"task_class":   taskClass,
			"context":      ctx,
			"started_at":   testutil.Raw("now() - interval '2 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
		if costTicks >= 0 {
			fx.Insert(t, "task_usage", testutil.Cols{
				"task_id":        taskID,
				"provider":       "openai",
				"model":          "m-wf",
				"cost_usd_ticks": costTicks,
			})
		}
	}
}

func workflowSelectorSvc(fx routingTestFixture) *TaskService {
	// Seed 1: first Float64 is ~0.60, above the exploration epsilon, so the
	// selector exploits deterministically.
	return &TaskService{Queries: db.New(fx.pool), RoutingRand: rand.New(rand.NewSource(1))}
}

func loadFixtureAgent(t *testing.T, svc *TaskService, agentID string) db.Agent {
	t.Helper()
	agent, err := svc.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return agent
}

// Mode off (the default): history exists and still every task is single.
func TestSelectWorkflowOffModeAlwaysSingle(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 20, 20, 10)

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)
	workflow, reason := svc.selectWorkflow(context.Background(), agent, db.Issue{ID: util.MustParseUUID("00000000-0000-0000-0000-000000000001")}, TaskClassGeneral)
	if workflow != WorkflowSingle {
		t.Errorf("workflow = %q, want single in default off mode", workflow)
	}
	if reason != workflowReasonPolicyOff {
		t.Errorf("reason = %q, want %q", reason, workflowReasonPolicyOff)
	}
}

// Auto without enough history: single, and the reason says why.
func TestSelectWorkflowAutoInsufficientData(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)

	// Cold start: no runs at all.
	if w := svc.SelectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral); w != WorkflowSingle {
		t.Errorf("cold start workflow = %q, want single", w)
	}

	// Under the sample floor: 4 cascade runs are not a track record.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 4, 4, 10)
	workflow, reason := svc.selectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral)
	if workflow != WorkflowSingle {
		t.Errorf("workflow = %q, want single below the sample floor", workflow)
	}
	if reason != workflowReasonInsufficientData {
		t.Errorf("reason = %q, want %q", reason, workflowReasonInsufficientData)
	}
}

// Auto with history: among equally trustworthy workflows the cheapest wins;
// a workflow outside the Wilson bar loses even when it is cheaper.
func TestSelectWorkflowAutoPicksCheapestWithinBar(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)

	// Three eligible workflows on the general class, identical success
	// (19/20 → Wilson lower ~0.764 each), different costs. Cascade is the
	// cheapest and must win.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 19, 10000)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 20, 19, 10)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCritique, 20, 19, 1000)
	// A cheap proven runtime for the cascade degradation check. These runs
	// carry usage rows (so the routing stats see runtime B) and a cascade
	// stamp of their own — an unstamped run would land in the single bucket
	// (the historical default) and skew its Wilson bound.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, WorkflowCascade, 20, 19, 50)

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)
	workflow, reason := svc.selectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral)
	if workflow != WorkflowCascade {
		t.Errorf("workflow = %q, want cascade (cheapest within the Wilson bar)", workflow)
	}
	if reason != workflowReasonPolicyAuto {
		t.Errorf("reason = %q, want %q", reason, workflowReasonPolicyAuto)
	}
}

// A costlier workflow with a clearly better track record wins; the cheap one
// outside the bar is not eligible for the cost tiebreak.
func TestSelectWorkflowAutoBestWilsonOutsideBar(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)

	// Critique 20/20 (Wilson ~0.83) but pricey; single 10/20 (Wilson ~0.30)
	// and cheap — outside the bar, so critique wins despite its cost.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 10, 10)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCritique, 20, 20, 100000)
	// The critique degradation needs an independent reviewer: a second agent
	// on another (runtime, model) pair.
	dbfx.Agent(t, "wf-reviewer", fx.runtimeB, testutil.Cols{"model": "m-b"})

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)
	if w := svc.SelectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral); w != WorkflowCritique {
		t.Errorf("workflow = %q, want critique (best Wilson, cheap one outside the bar)", w)
	}
}

// Cascade degrades to single when no cheap proven runtime exists to start on
// (workflow stats seeded without usage rows, so the routing stats are empty).
func TestSelectWorkflowCascadeDegradesWithoutCheapRuntime(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)

	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 10, -1)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 20, 20, -1)

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)
	if w := svc.SelectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral); w != WorkflowSingle {
		t.Errorf("workflow = %q, want single (cascade has no cheap runtime to start on)", w)
	}
}

// Critique degrades to single when the workspace has no independent reviewer.
func TestSelectWorkflowCritiqueDegradesWithoutReviewer(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)

	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 10, 10)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCritique, 20, 20, 100000)

	svc := workflowSelectorSvc(fx)
	agent := loadFixtureAgent(t, svc, fx.agentID)
	// The fixture workspace has a single agent: no one can review its runs.
	if w := svc.SelectWorkflow(context.Background(), agent, db.Issue{}, TaskClassGeneral); w != WorkflowSingle {
		t.Errorf("workflow = %q, want single (no independent reviewer in the workspace)", w)
	}
}

// Every issue task carries its workflow stamp, off mode included.
func TestEnqueueTaskStampsWorkflowSingleByDefault(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID)
	issueID := dbfx.Issue(t, "Stamp me", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.agentID,
	})

	svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	task, err := svc.EnqueueTaskForIssue(context.Background(), db.Issue{
		ID:           util.MustParseUUID(issueID),
		Title:        "Stamp me",
		AssigneeID:   util.MustParseUUID(fx.agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(fx.user),
		WorkspaceID:  util.MustParseUUID(fx.workspace),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	if got := TaskWorkflow(task.Context); got != WorkflowSingle {
		t.Errorf("stamped workflow = %q, want single", got)
	}
	if TaskForceReview(task.Context) {
		t.Error("force_review must be absent on a single run")
	}
}

// A cascade pick moves the enqueue onto the cheapest proven runtime; a
// critique pick stamps force_review into the context.
func TestEnqueueTaskWorkflowEffects(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)
	dbfx.Agent(t, "wf-reviewer", fx.runtimeB, testutil.Cols{"model": "m-b"})

	enqueue := func(t *testing.T, title string) db.AgentTaskQueue {
		t.Helper()
		issueID := dbfx.Issue(t, title, testutil.Cols{
			"assignee_type": "agent",
			"assignee_id":   fx.agentID,
		})
		svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
		task, err := svc.EnqueueTaskForIssue(context.Background(), db.Issue{
			ID:           util.MustParseUUID(issueID),
			Title:        title,
			AssigneeID:   util.MustParseUUID(fx.agentID),
			Priority:     "medium",
			CreatorType:  "member",
			CreatorID:    util.MustParseUUID(fx.user),
			WorkspaceID:  util.MustParseUUID(fx.workspace),
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
		})
		if err != nil {
			t.Fatalf("EnqueueTaskForIssue: %v", err)
		}
		return task
	}

	// Cascade: cheapest within the bar, and runtime B is the cheap proven
	// candidate. The bound runtime is A — the cascade override must move it.
	// The cheap-runtime evidence carries usage rows (routing stats) and a
	// cascade stamp, so it feeds the cascade bucket instead of single's.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 19, 10000)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 20, 19, 10)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, WorkflowCascade, 20, 19, 50)

	// A fixed-routing agent may not leave its binding: the claim fence would
	// refuse a task stamped with runtime B and it would sit queued forever.
	// The cascade has nowhere to start, so it degrades to single on A — the
	// same guard the escalation hop applies (run_escalation.go).
	task := enqueue(t, "Cascade run bound")
	if got := TaskWorkflow(task.Context); got != WorkflowSingle {
		t.Errorf("fixed agent stamped workflow = %q, want single", got)
	}
	if util.UUIDToString(task.RuntimeID) != fx.runtimeA {
		t.Errorf("fixed agent task runtime = %s, want bound runtime %s", util.UUIDToString(task.RuntimeID), fx.runtimeA)
	}

	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'auto' WHERE id = $1`, fx.agentID)
	task = enqueue(t, "Cascade run")
	if got := TaskWorkflow(task.Context); got != WorkflowCascade {
		t.Errorf("stamped workflow = %q, want cascade", got)
	}
	if util.UUIDToString(task.RuntimeID) != fx.runtimeB {
		t.Errorf("task runtime = %s, want cheap cascade runtime %s", util.UUIDToString(task.RuntimeID), fx.runtimeB)
	}
	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID)

	// Critique: best Wilson by far, reviewer available → force_review stamp.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassDocs, WorkflowSingle, 20, 10, 10)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassDocs, WorkflowCritique, 20, 20, 100000)

	// A "docs" title classifies as TaskClassDocs.
	task = enqueue(t, "Update the README documentation")
	if got := TaskWorkflow(task.Context); got != WorkflowCritique {
		t.Errorf("stamped workflow = %q, want critique (task_class=%s)", got, task.TaskClass)
	}
	if !TaskForceReview(task.Context) {
		t.Error("critique run must carry force_review in its context")
	}
	var rawContext map[string]any
	if err := json.Unmarshal(task.Context, &rawContext); err != nil {
		t.Fatalf("context is not JSON: %v", err)
	}
	if rawContext["force_review"] != true {
		t.Errorf("context force_review = %v, want JSON true", rawContext["force_review"])
	}
}

// The cascade start obeys data residency (K46) like every other routing
// stage: a cheaper runtime the policy rejects is not a place to start.
func TestEnqueueCascadeStartSkipsRuntimeTheResidencyPolicyRejects(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setWorkflowPolicy(t, dbfx, fx.workspace, WorkflowPolicyModeAuto)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowSingle, 20, 19, 10000)
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, WorkflowCascade, 20, 19, 100)
	// B is the cheapest proven runtime, and the policy bans its provider.
	seedWorkflowRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, WorkflowCascade, 20, 19, 5)
	dbfx.Exec(t, `UPDATE agent_runtime SET provider = 'claude' WHERE id = $1`, fx.runtimeB)
	setResidencyPolicy(t, dbfx, fx.workspace, `{"banned_providers":["claude"],"region_allowlist":[],"require_on_prem":false}`)

	issueID := dbfx.Issue(t, "Cascade under residency", testutil.Cols{"assignee_type": "agent", "assignee_id": fx.agentID})
	svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	task, err := svc.EnqueueTaskForIssue(context.Background(), db.Issue{
		ID:           util.MustParseUUID(issueID),
		Title:        "Cascade under residency",
		AssigneeID:   util.MustParseUUID(fx.agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(fx.user),
		WorkspaceID:  util.MustParseUUID(fx.workspace),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	if util.UUIDToString(task.RuntimeID) == fx.runtimeB {
		t.Fatalf("cascade started on runtime %s, which the residency policy rejects", fx.runtimeB)
	}
}
