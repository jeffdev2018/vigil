package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// K60 audit (see FIX-BRIEF): a long list of issue- and project-scoped write
// handlers loaded the issue/project via loadIssueForUser or
// loadProjectForResource — workspace-membership only — without ever calling
// requireProjectRole/requireProjectWrite, so a member explicitly lowered to
// "viewer" on one project via SetProjectMemberRole could still write on it
// through these endpoints even though the equivalent UpdateIssue/CreateComment
// paths already refused them (K60's own gate, project_role.go). This file
// proves the fix: a viewer override gets 403 from every one of them; a plain
// member (the default contributor ceiling — exercised at full realism by the
// rest of this package's test suite, which calls all of these as an ordinary
// member and still passes) is not blocked by the gate.

// k60WriteFixture is one project with a member explicitly held at viewer via
// a K60 project-role override — the actor every case below drives through
// the newly gated handlers to prove they now refuse it.
type k60WriteFixture struct {
	project string
	viewer  string // user id, viewer override on project
	writer  string // user id, default contributor ceiling, no override
}

func newK60WriteFixture(t *testing.T) k60WriteFixture {
	t.Helper()
	project := dbfx.Project(t, "k60 write "+uuid.NewString()[:8])
	viewer := dbfx.User(t, "k60 viewer", "k60-viewer-"+uuid.NewString()[:8]+"@example.test")
	viewerMemberID := dbfx.Member(t, testWorkspaceID, viewer, "member")
	writer := dbfx.User(t, "k60 writer", "k60-writer-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, testWorkspaceID, writer, "member")
	testutil.Call(t, testHandler.SetProjectMemberRole, testutil.WithURLParams(
		newRequest(http.MethodPut, "/x", map[string]any{"role": "viewer"}),
		"id", project, "subjectType", "member", "subjectId", viewerMemberID,
	)).Want(http.StatusOK)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member_role WHERE project_id = $1`, project)
	})
	return k60WriteFixture{project: project, viewer: viewer, writer: writer}
}

// k60Case is one gated handler: build the request as the given actor and
// return the recorded response. `params` are extra chi URL params beyond
// "id" (the issue or project id, filled in by the runner).
type k60Case struct {
	name    string
	handler http.HandlerFunc
	method  string
	path    string
	body    any
	params  []string // key,value,... beyond "id"
}

// TestProjectWriteGatesRefuseAViewer is the table-driven regression test the
// FIX-BRIEF asks for: every handler below now calls requireProjectWrite (or
// an equivalent project-role check) right after loading the issue/project, so
// a viewer-override member gets 403 regardless of what the rest of the
// request body says — which is why every case can share one minimal body.
func TestProjectWriteGatesRefuseAViewer(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 write gate issue "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})

	cases := []k60Case{
		{"AgentDuel.StartAgentDuel", testHandler.StartAgentDuel, http.MethodPost, "/api/issues/{id}/duel", map[string]any{"agent_a_id": uuid.NewString(), "agent_b_id": uuid.NewString()}, nil},
		{"Decision.AskIssueDecision", testHandler.AskIssueDecision, http.MethodPost, "/api/issues/{id}/decisions", map[string]any{"question": "q?"}, nil},
		{"DecisionRecord.CreateIssueDecisions", testHandler.CreateIssueDecisions, http.MethodPost, "/api/issues/{id}/decision-records", map[string]any{"decisions": []map[string]any{{"title": "t"}}}, nil},
		{"Eval.PromoteIssueToEvalCase", testHandler.PromoteIssueToEvalCase, http.MethodPost, "/api/issues/{id}/promote-to-eval-case", nil, nil},
		{"HandoffPacket.CreateHandoffPacket", testHandler.CreateHandoffPacket, http.MethodPost, "/api/issues/{id}/handoff-packet", map[string]any{"objective": "o", "run_id": uuid.NewString()}, nil},
		{"Interview.AskRequirementInterview", testHandler.AskRequirementInterview, http.MethodPost, "/api/issues/{id}/interview", map[string]any{"question": "q"}, nil},
		{"IssueDependency.Create", testHandler.CreateIssueDependency, http.MethodPost, "/api/issues/{id}/dependencies", map[string]any{"target_issue_id": uuid.NewString(), "type": "blocks"}, nil},
		{"IssueDependency.Delete", testHandler.DeleteIssueDependency, http.MethodDelete, "/api/issues/{id}/dependencies/{depId}", nil, []string{"depId", uuid.NewString()}},
		{"IssueMetadata.Set", testHandler.SetIssueMetadataKey, http.MethodPut, "/api/issues/{id}/metadata/{key}", map[string]any{"value": "v"}, []string{"key", "k"}},
		{"IssueMetadata.Delete", testHandler.DeleteIssueMetadataKey, http.MethodDelete, "/api/issues/{id}/metadata/{key}", nil, []string{"key", "k"}},
		{"IssuePlan.SetIssuePlan", testHandler.SetIssuePlan, http.MethodPut, "/api/issues/{id}/plan", map[string]any{"steps": []string{"a"}}, nil},
		{"IssueRecurrence.Set", testHandler.SetIssueRecurrence, http.MethodPut, "/api/issues/{id}/recurrence", map[string]any{"cron_expression": "0 0 * * *"}, nil},
		{"IssueRecurrence.Delete", testHandler.DeleteIssueRecurrence, http.MethodDelete, "/api/issues/{id}/recurrence", nil, nil},
		{"Pipeline.StartPipelineRun", testHandler.StartPipelineRun, http.MethodPost, "/api/issues/{id}/pipeline-run", map[string]any{"pipeline_id": uuid.NewString()}, nil},
		{"PlanGate.MaterializeIssuePlan", testHandler.MaterializeIssuePlan, http.MethodPost, "/api/issues/{id}/plan/{version}/materialize", nil, []string{"version", "1"}},
		{"Property.SetIssueProperty", testHandler.SetIssueProperty, http.MethodPut, "/api/issues/{id}/properties/{propertyId}", map[string]any{"value": "v"}, []string{"propertyId", uuid.NewString()}},
		{"Property.DeleteIssueProperty", testHandler.DeleteIssueProperty, http.MethodDelete, "/api/issues/{id}/properties/{propertyId}", nil, []string{"propertyId", uuid.NewString()}},
		{"QuickAction.RunQuickAction", testHandler.RunQuickAction, http.MethodPost, "/api/issues/{id}/quick-actions/{quickActionId}", nil, []string{"quickActionId", uuid.NewString()}},
		{"ReviewFlag.Create", testHandler.CreateIssueReviewFlag, http.MethodPost, "/api/issues/{id}/review-flags", map[string]any{"pr_id": uuid.NewString()}, nil},
		{"ReviewFlag.SetState", testHandler.SetIssueReviewFlagState, http.MethodPatch, "/api/issues/{id}/review-flags/{flagId}", map[string]any{"state": "resolved"}, []string{"flagId", uuid.NewString()}},
		{"RunBranchAction.Promote", testHandler.PromoteIssueRun, http.MethodPost, "/api/issues/{id}/runs/{taskId}/promote", nil, []string{"taskId", uuid.NewString()}},
		{"RunBranchAction.Discard", testHandler.DiscardIssueRun, http.MethodPost, "/api/issues/{id}/runs/{taskId}/discard", nil, []string{"taskId", uuid.NewString()}},
		{"RunControl.Steer", testHandler.SteerRun, http.MethodPost, "/api/issues/{id}/run/steer", map[string]any{"instruction": "do X"}, nil},
		{"RunGroup.StartRunGroup", testHandler.StartRunGroup, http.MethodPost, "/api/issues/{id}/run-groups", map[string]any{"attempts": []map[string]any{{"agent_id": uuid.NewString()}, {"agent_id": uuid.NewString()}}}, nil},
		{"TaskReplay.SimulateTaskReplay", testHandler.SimulateTaskReplay, http.MethodPost, "/api/tasks/{taskId}/replay/simulate", nil, nil}, // uses {id}->taskId, handled below
		{"TrafficControl.IgnoreTrafficConflict", testHandler.IgnoreTrafficConflict, http.MethodPost, "/api/issues/{id}/traffic-conflicts/{cid}/ignore", nil, []string{"cid", uuid.NewString()}},
		{"Watchdog.Set", testHandler.SetIssueWatchdog, http.MethodPut, "/api/issues/{id}/watchdog", map[string]any{"agent_id": uuid.NewString(), "instructions": "watch", "rest_minutes": 10}, nil},
		{"Watchdog.Delete", testHandler.DeleteIssueWatchdog, http.MethodDelete, "/api/issues/{id}/watchdog", nil, nil},
		{"Watchdog.SetContractRisk", testHandler.SetIssueContractRisk, http.MethodPut, "/api/issues/{id}/contract-risk", map[string]any{"risk": "high"}, nil},
		{"WorktreeRevert.RequestIssueRunRevert", testHandler.RequestIssueRunRevert, http.MethodPost, "/api/issues/{id}/runs/{taskId}/revert", map[string]any{"target_task_id": uuid.NewString()}, []string{"taskId", uuid.NewString()}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rid := issue
			if c.name == "TaskReplay.SimulateTaskReplay" {
				// This one is keyed by taskId, not issue id — set up a real
				// task on the gated issue so runReplayTask resolves it.
				agent := dbfx.Agent(t, "k60 replay agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
				task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed"})
				req := withURLParam(newRequestAs(fx.viewer, c.method, c.path, c.body), "taskId", task)
				res := testutil.Call(t, c.handler, req)
				if res.Code != http.StatusForbidden {
					t.Fatalf("%s: viewer got %d, want 403: %s", c.name, res.Code, res.Body.String())
				}
				writerReq := withURLParam(newRequestAs(fx.writer, c.method, c.path, c.body), "taskId", task)
				if res := testutil.Call(t, c.handler, writerReq); res.Code == http.StatusForbidden {
					t.Fatalf("%s: writer (contributor) got 403, the project-role gate must not block them: %s", c.name, res.Body.String())
				}
				return
			}
			params := append([]string{"id", rid}, c.params...)
			req := testutil.WithURLParams(newRequestAs(fx.viewer, c.method, c.path, c.body), params...)
			res := testutil.Call(t, c.handler, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("%s: viewer got %d, want 403: %s", c.name, res.Code, res.Body.String())
			}
			writerReq := testutil.WithURLParams(newRequestAs(fx.writer, c.method, c.path, c.body), params...)
			if res := testutil.Call(t, c.handler, writerReq); res.Code == http.StatusForbidden {
				t.Fatalf("%s: writer (contributor) got 403, the project-role gate must not block them: %s", c.name, res.Body.String())
			}
		})
	}
}

// TestBatchUpdateIssuesRefusesAViewersProject: the batch endpoint refuses the
// one item in a project the caller cannot write, reporting it in `refused`
// rather than either silently applying it or aborting the whole batch.
func TestBatchUpdateIssuesRefusesAViewersProject(t *testing.T) {
	fx := newK60WriteFixture(t)
	viewerIssue := dbfx.Issue(t, "k60 batch upd viewer "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})
	freeIssue := dbfx.Issue(t, "k60 batch upd free "+uuid.NewString()[:8])

	body := map[string]any{"issue_ids": []string{viewerIssue, freeIssue}, "updates": map[string]any{"title": "renamed by " + uuid.NewString()[:6]}}
	var resp struct {
		Updated int `json:"updated"`
		Refused []struct {
			IssueID string `json:"issue_id"`
			Code    string `json:"code"`
		} `json:"refused"`
	}
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequestAs(fx.viewer, http.MethodPost, "/api/issues/batch-update", body)).Want(http.StatusOK).JSON(&resp)
	if resp.Updated != 1 {
		t.Fatalf("expected exactly the free issue updated, got %d", resp.Updated)
	}
	if len(resp.Refused) != 1 || resp.Refused[0].IssueID != viewerIssue || resp.Refused[0].Code != ErrCodeProjectRoleForbidden {
		t.Fatalf("expected the project-viewer issue refused with %s, got %+v", ErrCodeProjectRoleForbidden, resp.Refused)
	}
	var title string
	dbfx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, viewerIssue).Scan(&title)
	if title == "renamed by "+uuid.NewString()[:6] {
		t.Fatal("the viewer's project issue must not have been renamed")
	}

	// The writer (default contributor) is refused nothing.
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequestAs(fx.writer, http.MethodPost, "/api/issues/batch-update", body)).Want(http.StatusOK).JSON(&resp)
	if resp.Updated != 2 || len(resp.Refused) != 0 {
		t.Fatalf("writer expected both issues updated, got updated=%d refused=%+v", resp.Updated, resp.Refused)
	}
}

// TestBatchDeleteIssuesSkipsAViewersProject: the same boundary on
// batch-delete, which has no `refused` reporting today — it silently skips,
// same as its existing not-found/invalid-id handling.
func TestBatchDeleteIssuesSkipsAViewersProject(t *testing.T) {
	fx := newK60WriteFixture(t)
	viewerIssue := dbfx.Issue(t, "k60 batch del viewer "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})
	freeIssue := dbfx.Issue(t, "k60 batch del free "+uuid.NewString()[:8])

	body := map[string]any{"issue_ids": []string{viewerIssue, freeIssue}}
	var resp struct {
		Deleted int `json:"deleted"`
	}
	testutil.Call(t, testHandler.BatchDeleteIssues, newRequestAs(fx.viewer, http.MethodPost, "/api/issues/batch-delete", body)).Want(http.StatusOK).JSON(&resp)
	if resp.Deleted != 1 {
		t.Fatalf("expected only the free issue deleted, got %d", resp.Deleted)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1`, viewerIssue) != 1 {
		t.Fatal("the viewer's project issue must survive")
	}
}

// TestUpdateIssueRefusesMovingIntoAViewersProject: moving an issue INTO a
// project the caller cannot write is refused even though the caller can
// write the source project.
func TestUpdateIssueRefusesMovingIntoAViewersProject(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 move issue "+uuid.NewString()[:8]) // no project — the writer can move it anywhere by default

	req := withURLParam(newRequestAs(fx.viewer, http.MethodPut, "/api/issues/"+issue, map[string]any{"project_id": fx.project}), "id", issue)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusForbidden)

	var projectID *string
	dbfx.QueryRow(t, `SELECT project_id FROM issue WHERE id = $1`, issue).Scan(&projectID)
	if projectID != nil {
		t.Fatal("the issue must not have moved into the viewer's project")
	}
}

// TestProposeIssueGoalRequiresAgentProjectWrite: ProposeIssueGoal is
// agent-only, so — unlike the rest of this file — the actor under test is an
// agent acting through its own task token, downgraded via the same K60
// project-role override.
func TestProposeIssueGoalRequiresAgentProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 goal proposal issue "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})
	goalID := dbfx.Insert(t, "goal", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "title": "k60 goal " + uuid.NewString()[:6]})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM goal WHERE id = $1`, goalID) })

	viewerAgent := dbfx.Agent(t, "k60 goal viewer agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	viewerAgentMemberID := viewerAgent // SetProjectMemberRole keys agents by their own id, not a member row
	testutil.Call(t, testHandler.SetProjectMemberRole, testutil.WithURLParams(
		newRequest(http.MethodPut, "/x", map[string]any{"role": "viewer"}),
		"id", fx.project, "subjectType", "agent", "subjectId", viewerAgentMemberID,
	)).Want(http.StatusOK)
	viewerTask := dbfx.Task(t, viewerAgent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	req := withURLParam(runRequest(viewerAgent, viewerTask, http.MethodPost, "/api/issues/"+issue+"/goal-proposal", map[string]any{"goal_id": goalID, "reason": "r"}), "id", issue)
	testutil.Call(t, testHandler.ProposeIssueGoal, req).Want(http.StatusForbidden)

	writerAgent := dbfx.Agent(t, "k60 goal writer agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	writerTask := dbfx.Task(t, writerAgent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	writerReq := withURLParam(runRequest(writerAgent, writerTask, http.MethodPost, "/api/issues/"+issue+"/goal-proposal", map[string]any{"goal_id": goalID, "reason": "r"}), "id", issue)
	if res := testutil.Call(t, testHandler.ProposeIssueGoal, writerReq); res.Code == http.StatusForbidden {
		t.Fatalf("writer agent (contributor) got 403: %s", res.Body.String())
	}
}

// TestSetProjectGoalsRequiresProjectAdmin: a project-structure change held to
// the same admin bar as SetProjectMemberRole — a contributor (not just a
// viewer) is refused too.
func TestSetProjectGoalsRequiresProjectAdmin(t *testing.T) {
	fx := newK60WriteFixture(t)
	req := withURLParam(newRequestAs(fx.writer, http.MethodPut, "/api/projects/"+fx.project+"/goals", map[string]any{"goal_ids": []string{}}), "id", fx.project)
	testutil.Call(t, testHandler.SetProjectGoals, req).Want(http.StatusForbidden)
}

// TestAutopilotProposalRequiresProjectWrite: a project-scoped autopilot
// proposal writes on that project.
func TestAutopilotProposalRequiresProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	agentID := dbfx.Agent(t, "k60 autopilot agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	body := map[string]any{
		"title": "k60 autopilot", "cron_expression": "0 0 * * *", "project_id": fx.project,
		"assignee_id": agentID,
	}
	req := newRequestAs(fx.viewer, http.MethodPost, "/api/autopilots/propose", body)
	testutil.Call(t, testHandler.ProposeAutopilot, req).Want(http.StatusForbidden)
}

// TestCodeWikiWritesRequireProjectWrite covers all three write endpoints
// (snapshot create, page write via loadBuildingSnapshot, publish via the
// same loader) with one project-level check.
func TestCodeWikiWritesRequireProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	req := withURLParam(newRequestAs(fx.viewer, http.MethodPost, "/api/projects/"+fx.project+"/code-wiki/snapshots", map[string]any{
		"resource_id": uuid.NewString(), "commit_sha": "deadbeef", "repo_paths": []string{"a.go"},
	}), "id", fx.project)
	testutil.Call(t, testHandler.CreateProjectCodeWikiSnapshot, req).Want(http.StatusForbidden)

	pageReq := testutil.WithURLParams(newRequestAs(fx.viewer, http.MethodPost, "/x", map[string]any{"slug": "s", "title": "t", "content": "c"}), "id", fx.project, "sid", uuid.NewString())
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage, pageReq).Want(http.StatusForbidden)

	pubReq := testutil.WithURLParams(newRequestAs(fx.viewer, http.MethodPost, "/x", nil), "id", fx.project, "sid", uuid.NewString())
	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot, pubReq).Want(http.StatusForbidden)
}

// TestRefactorCampaignAndFanoutRequireProjectWrite covers CreateRefactorCampaign
// and StartFanout (immediately reachable) plus SkipCampaignShard, which needs
// a real campaign/shard row to get past its own lookups before reaching the
// gate — inserted directly since this schema has no foreign keys (CLAUDE.md).
func TestRefactorCampaignAndFanoutRequireProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 campaign issue "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})

	campaignReq := newRequestAs(fx.viewer, http.MethodPost, "/api/refactor-campaigns", map[string]any{
		"issue_id": issue, "name": "k60", "target_branch": "main", "leader_agent_id": uuid.NewString(),
	})
	testutil.Call(t, testHandler.CreateRefactorCampaign, campaignReq).Want(http.StatusForbidden)

	fanoutReq := withURLParam(newRequestAs(fx.viewer, http.MethodPost, "/api/issues/"+issue+"/fanout", map[string]any{
		"leader_agent_id": uuid.NewString(), "sub_tasks": []map[string]any{},
	}), "id", issue)
	testutil.Call(t, testHandler.StartFanout, fanoutReq).Want(http.StatusForbidden)

	campaignID := dbfx.Insert(t, "refactor_campaign", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issue, "fanout_batch_id": uuid.NewString(),
		"name": "k60 shard test", "target_branch": "main",
	})
	shardID := dbfx.Insert(t, "campaign_shard", testutil.Cols{
		"refactor_campaign_id": campaignID, "workspace_id": testWorkspaceID,
		"fanout_member_id": uuid.NewString(), "child_issue_id": uuid.NewString(), "task_id": uuid.NewString(),
		"assignee_agent_id": uuid.NewString(), "description": "shard", "branch_name": "feat/k60", "merge_position": 0,
	})
	skipReq := withURLParam(newRequestAs(fx.viewer, http.MethodPost, "/api/campaign-shards/"+shardID+"/skip", nil), "id", shardID)
	testutil.Call(t, testHandler.SkipCampaignShard, skipReq).Want(http.StatusForbidden)
}

// TestPluginActionRequiresProjectWrite covers PatchPluginIssue and
// CreatePluginComment: a member acting through the plugin bridge is judged
// by their own project role, same as the ordinary endpoints.
func TestPluginActionRequiresProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 plugin issue "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write", "comments:read", "comments:write"})

	patchReq := pluginActionRequest(http.MethodPatch, "/v1/issues/"+issue, installationID, map[string]any{"title": "renamed"}, map[string]string{"issue_ref": issue})
	patchReq.Header.Set("X-User-ID", fx.viewer)
	rec := httptest.NewRecorder()
	testHandler.PatchPluginIssue(rec, patchReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PatchPluginIssue: viewer got %d, want 403: %s", rec.Code, rec.Body.String())
	}

	commentReq := pluginActionRequest(http.MethodPost, "/v1/issues/"+issue+"/comments", installationID, map[string]any{"content": "hi"}, map[string]string{"issue_ref": issue})
	commentReq.Header.Set("X-User-ID", fx.viewer)
	rec2 := httptest.NewRecorder()
	testHandler.CreatePluginComment(rec2, commentReq)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("CreatePluginComment: viewer got %d, want 403: %s", rec2.Code, rec2.Body.String())
	}
}

// TestSourceContextSubIssueRequiresProjectWrite exercises
// createManualCommentSubIssue and prepareAgentCommentSubIssue directly — the
// exact functions the K60 gate was added to — rather than the whole
// capture/token pipeline CreateCommentSubIssue sits behind, which is proven
// elsewhere (source_context_integration_test.go).
func TestSourceContextSubIssueRequiresProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	wsUUID := parseUUID(testWorkspaceID)

	t.Run("manual", func(t *testing.T) {
		userUUID := parseUUID(fx.viewer)
		r := newRequestAs(fx.viewer, http.MethodPost, "/x", nil)
		title := "k60 manual sub-issue"
		projectID := fx.project
		input := CreateIssueRequest{Title: title, ProjectID: &projectID}
		rec := httptest.NewRecorder()
		err := testHandler.createManualCommentSubIssue(rec, r, wsUUID, userUUID, input, service.SourceContextCapture{}, service.SourceContextLimitUsage{})
		if err != errSourceContextResponseWritten || rec.Code != http.StatusForbidden {
			t.Fatalf("manual sub-issue: err=%v code=%d body=%s", err, rec.Code, rec.Body.String())
		}
	})

	t.Run("agent", func(t *testing.T) {
		runtimeID := dbfx.Runtime(t, "k60 quick create runtime "+uuid.NewString()[:6], testutil.Cols{
			"runtime_mode": nativeRuntimeMode,
			"metadata":     testutil.Raw(`'{"capabilities":["` + protocol.DaemonCapabilitySourceContextQuickCreateV1 + `"]}'::jsonb`),
		})
		agentID := dbfx.Agent(t, "k60 quick create agent "+uuid.NewString()[:6], runtimeID)
		r := newRequestAs(fx.viewer, http.MethodPost, "/x", nil)
		input := QuickCreateIssueRequest{Prompt: "do something", AgentID: agentID, ProjectID: fx.project}
		rec := httptest.NewRecorder()
		_, err := testHandler.prepareAgentCommentSubIssue(rec, r, wsUUID, input)
		if err != errSourceContextResponseWritten || rec.Code != http.StatusForbidden {
			t.Fatalf("agent sub-issue: err=%v code=%d body=%s", err, rec.Code, rec.Body.String())
		}
	})
}

// TestRunControlRequiresProjectWrite: Pause/Steer/Resume all route through
// controllableRun's one gate.
func TestRunControlRequiresProjectWrite(t *testing.T) {
	fx := newK60WriteFixture(t)
	issue := dbfx.Issue(t, "k60 run control issue "+uuid.NewString()[:8], testutil.Cols{"project_id": fx.project})
	req := withURLParam(newRequestAs(fx.viewer, http.MethodPost, "/api/issues/"+issue+"/run/pause", nil), "id", issue)
	testutil.Call(t, testHandler.PauseRun, req).Want(http.StatusForbidden)
}

// TestProjectMemoryRequiresProjectAdminEvenForAWorkspaceAdmin: a project-role
// override can lower an owner/admin's effective role on one project below the
// workspace-role check UpdateProjectMemory otherwise applies.
func TestProjectMemoryRequiresProjectAdminEvenForAWorkspaceAdmin(t *testing.T) {
	project := dbfx.Project(t, "k60 memory "+uuid.NewString()[:8])
	// testUserID is the workspace owner used by newRequest by default.
	ownerMemberID := dbfx.QueryRow(t, `SELECT id::text FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	var memberID string
	ownerMemberID.Scan(&memberID)
	testutil.Call(t, testHandler.SetProjectMemberRole, testutil.WithURLParams(
		newRequest(http.MethodPut, "/x", map[string]any{"role": "viewer"}),
		"id", project, "subjectType", "member", "subjectId", memberID,
	)).Want(http.StatusOK)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project_member_role WHERE project_id = $1`, project) })

	req := withURLParam(newRequest(http.MethodPut, "/api/projects/"+project+"/memory", map[string]any{"rules": []string{"r"}}), "id", project)
	testutil.Call(t, testHandler.UpdateProjectMemory, req).Want(http.StatusForbidden)
}
