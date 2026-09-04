package service

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestEnqueueTaskForIssueAutoRoutingChoosesBestRuntime is the end-to-end
// enqueue test for JEF-237: an auto-routed agent with real run statistics
// gets its task stamped with the router-chosen runtime, the task class, and
// the full routing audit trace.
func TestEnqueueTaskForIssueAutoRoutingChoosesBestRuntime(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)

	ctx := context.Background()
	issueID := dbfx.Issue(t, "Improve onboarding flow", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.agentID,
	})

	svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		Title:        "Improve onboarding flow",
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

	if util.UUIDToString(task.RuntimeID) != fx.runtimeB {
		t.Errorf("task runtime = %s, want router-chosen %s", util.UUIDToString(task.RuntimeID), fx.runtimeB)
	}
	if task.TaskClass != TaskClassGeneral {
		t.Errorf("task class = %q, want general", task.TaskClass)
	}
	if len(task.Routing) == 0 {
		t.Fatal("routing trace is empty")
	}
	var trace map[string]any
	if err := json.Unmarshal(task.Routing, &trace); err != nil {
		t.Fatalf("routing trace is not JSON: %v", err)
	}
	if trace["mode"] != RoutingModeAuto {
		t.Errorf("trace mode = %v, want auto", trace["mode"])
	}
	if trace["chosen_runtime_id"] != fx.runtimeB {
		t.Errorf("trace chosen_runtime_id = %v, want %s", trace["chosen_runtime_id"], fx.runtimeB)
	}
	if trace["chosen_model"] != "m-b" {
		t.Errorf("trace chosen_model = %v, want m-b", trace["chosen_model"])
	}
	if trace["reason"] != routingReasonBestScore {
		t.Errorf("trace reason = %v, want best_score", trace["reason"])
	}
	if _, ok := trace["candidates"].([]any); !ok {
		t.Errorf("trace candidates missing or wrong type: %s", task.Routing)
	}
}

// TestEnqueueTaskForIssueFixedModeStampsClassOnly pins the unchanged fixed
// behavior: the task keeps the agent's bound runtime, carries no routing
// trace, but still gets its task class for future statistics.
func TestEnqueueTaskForIssueFixedModeStampsClassOnly(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID)

	// A "bug" label drives the class; the title alone would not.
	labelID := dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id":  fx.workspace,
		"name":          "bug",
		"color":         "#ff0000",
		"resource_type": "issue",
	})
	issueID := dbfx.Issue(t, "Improve onboarding flow", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.agentID,
	})
	dbfx.InsertNoID(t, "issue_to_label",
		testutil.Cols{"issue_id": issueID, "label_id": labelID},
		"issue_id = $1 AND label_id = $2", issueID, labelID)

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		Title:        "Improve onboarding flow",
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

	if util.UUIDToString(task.RuntimeID) != fx.runtimeA {
		t.Errorf("task runtime = %s, want bound %s (fixed mode)", util.UUIDToString(task.RuntimeID), fx.runtimeA)
	}
	if len(task.Routing) != 0 {
		t.Errorf("routing trace = %s, want NULL in fixed mode", task.Routing)
	}
	if task.TaskClass != TaskClassBugfix {
		t.Errorf("task class = %q, want bugfix (label-driven)", task.TaskClass)
	}
}

// TestClaimAgentTaskRuntimeRoutingFence covers the relaxed claim fence:
// a fixed agent can never claim a task stamped with another runtime, while
// an auto-routed agent claims the task the router placed on the chosen
// runtime — through ClaimAgentTask AND both candidate-list queries.
func TestClaimAgentTaskRuntimeRoutingFence(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		runtimeRouting string
		wantClaim      bool
	}{
		{name: "fixed agent cannot claim task on another runtime", runtimeRouting: "fixed", wantClaim: false},
		{name: "auto agent claims task on router-chosen runtime", runtimeRouting: "auto", wantClaim: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newRoutingTestFixture(t)
			dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
			dbfx.Exec(t, `UPDATE agent SET runtime_routing = $1 WHERE id = $2`, tt.runtimeRouting, fx.agentID)
			// The task sits on runtime B while the agent stays bound to A.
			taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
				"runtime_id": fx.runtimeB,
				"status":     "queued",
			})

			q := db.New(fx.pool)
			candidates, err := q.ListQueuedClaimCandidatesByRuntime(ctx, util.MustParseUUID(fx.runtimeB))
			if err != nil {
				t.Fatalf("list candidates: %v", err)
			}
			wantCandidates := 0
			if tt.wantClaim {
				wantCandidates = 1
			}
			if len(candidates) != wantCandidates {
				t.Fatalf("candidates on runtime B = %d, want %d", len(candidates), wantCandidates)
			}
			batchCandidates, err := q.ListQueuedClaimCandidatesByRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(fx.runtimeB)})
			if err != nil {
				t.Fatalf("list batch candidates: %v", err)
			}
			if len(batchCandidates) != wantCandidates {
				t.Fatalf("batch candidates on runtime B = %d, want %d", len(batchCandidates), wantCandidates)
			}

			claimed, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
				AgentID:          util.MustParseUUID(fx.agentID),
				RuntimeID:        util.MustParseUUID(fx.runtimeB),
				PrepareLeaseSecs: 60,
				RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
			})
			if !tt.wantClaim {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("claim error = %v, want no rows", err)
				}
				var status string
				dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
				if status != "queued" {
					t.Fatalf("task status = %q, want queued (unclaimed)", status)
				}
				return
			}
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if util.UUIDToString(claimed.ID) != taskID {
				t.Fatalf("claimed task = %s, want %s", util.UUIDToString(claimed.ID), taskID)
			}
		})
	}
}

// TestEnqueueChatTaskAutoRoutingStampsClassAndRuntime covers the chat enqueue
// path (JEF-237 gap): a chat turn for an auto-routed agent is classified from
// the session title, routed like an issue task, and carries the audit trace.
func TestEnqueueChatTaskAutoRoutingStampsClassAndRuntime(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassBugfix, "openai", "m-b", 20, 19)

	sessionID := dbfx.ChatSession(t, fx.agentID, testutil.Cols{
		"title":      "Fix the crash on export",
		"runtime_id": fx.runtimeA,
	})

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:          util.MustParseUUID(sessionID),
		WorkspaceID: util.MustParseUUID(fx.workspace),
		AgentID:     util.MustParseUUID(fx.agentID),
		CreatorID:   util.MustParseUUID(fx.user),
		Title:       "Fix the crash on export",
		Status:      "active",
	}, util.MustParseUUID(fx.user), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}

	if task.TaskClass != TaskClassBugfix {
		t.Errorf("task class = %q, want bugfix (from session title)", task.TaskClass)
	}
	if util.UUIDToString(task.RuntimeID) != fx.runtimeB {
		t.Errorf("task runtime = %s, want router-chosen %s", util.UUIDToString(task.RuntimeID), fx.runtimeB)
	}
	assertRoutingTrace(t, task.Routing, fx.runtimeB)
}

// TestSendDirectChatMessageAutoRoutingStampsClass pins the direct-send path:
// the classifier reads the member's own message, not the session title.
func TestSendDirectChatMessageAutoRoutingStampsClass(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassDocs, "openai", "m-b", 20, 19)

	sessionID := dbfx.ChatSession(t, fx.agentID, testutil.Cols{
		"title":      "Untitled",
		"runtime_id": fx.runtimeA,
	})
	q := db.New(fx.pool)
	ctx := context.Background()
	agent, err := q.GetAgent(ctx, util.MustParseUUID(fx.agentID))
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	out, err := svc.SendDirectChatMessage(ctx, db.ChatSession{
		ID:          util.MustParseUUID(sessionID),
		WorkspaceID: util.MustParseUUID(fx.workspace),
		AgentID:     util.MustParseUUID(fx.agentID),
		CreatorID:   util.MustParseUUID(fx.user),
		Title:       "Untitled",
		Status:      "active",
	}, agent, util.MustParseUUID(fx.user), "please update the documentation for the export flow", nil, "", pgtype.UUID{})
	if err != nil {
		t.Fatalf("SendDirectChatMessage: %v", err)
	}
	if out.Task.TaskClass != TaskClassDocs {
		t.Errorf("task class = %q, want docs (from the message content)", out.Task.TaskClass)
	}
	if util.UUIDToString(out.Task.RuntimeID) != fx.runtimeB {
		t.Errorf("task runtime = %s, want router-chosen %s", util.UUIDToString(out.Task.RuntimeID), fx.runtimeB)
	}
	assertRoutingTrace(t, out.Task.Routing, fx.runtimeB)
}

// TestDispatchRunOnlyAutoRoutingStampsClassAndRuntime covers the autopilot
// enqueue path (JEF-237 gap): a run_only dispatch is classified from the
// autopilot title and routed like every other enqueue.
func TestDispatchRunOnlyAutoRoutingStampsClassAndRuntime(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassTests, "openai", "m-b", 20, 19)

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    fx.workspace,
		"title":           "Nightly test coverage sweep",
		"assignee_type":   "agent",
		"assignee_id":     fx.agentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   fx.user,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "running",
	})

	ctx := context.Background()
	q := db.New(fx.pool)
	taskSvc := &TaskService{Queries: q, TxStarter: fx.pool, Bus: events.New(), RoutingRand: rand.New(rand.NewSource(1))}
	apSvc := NewAutopilotService(q, fx.pool, events.New(), taskSvc)

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if err := apSvc.dispatchRunOnly(ctx, ap, &run, util.MustParseUUID(fx.user)); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	task, err := q.GetAutopilotTaskByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotTaskByRun: %v", err)
	}
	if task.TaskClass != TaskClassTests {
		t.Errorf("task class = %q, want tests (from the autopilot title)", task.TaskClass)
	}
	if util.UUIDToString(task.RuntimeID) != fx.runtimeB {
		t.Errorf("task runtime = %s, want router-chosen %s", util.UUIDToString(task.RuntimeID), fx.runtimeB)
	}
	assertRoutingTrace(t, task.Routing, fx.runtimeB)
}

// TestCreateRetryTaskInheritsTaskClass pins the retry decision: an automatic
// retry inherits the parent's class instead of falling back to 'general' and
// polluting the per-class statistics the router reads.
func TestCreateRetryTaskInheritsTaskClass(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	parentID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id": fx.runtimeA,
		"status":     "failed",
		"task_class": TaskClassRefactor,
	})

	ctx := context.Background()
	q := db.New(fx.pool)
	child, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		ID:        util.MustParseUUID(parentID),
		NewTaskID: dbid.NewV7(),
	})
	if err != nil {
		t.Fatalf("CreateRetryTask: %v", err)
	}
	if child.TaskClass != TaskClassRefactor {
		t.Errorf("retry task class = %q, want inherited %q", child.TaskClass, TaskClassRefactor)
	}
}

// assertRoutingTrace checks the routing JSONB an auto-routed enqueue persisted.
func assertRoutingTrace(t *testing.T, routing []byte, wantRuntimeID string) {
	t.Helper()
	if len(routing) == 0 {
		t.Fatal("routing trace is empty")
	}
	var trace map[string]any
	if err := json.Unmarshal(routing, &trace); err != nil {
		t.Fatalf("routing trace is not JSON: %v", err)
	}
	if trace["mode"] != RoutingModeAuto {
		t.Errorf("trace mode = %v, want auto", trace["mode"])
	}
	if trace["chosen_runtime_id"] != wantRuntimeID {
		t.Errorf("trace chosen_runtime_id = %v, want %s", trace["chosen_runtime_id"], wantRuntimeID)
	}
}
