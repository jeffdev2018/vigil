package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Epic Mode (F18 / JEF-30): the gate order, the versioning, the whitelist and
// the completion hooks. Applying an approved breakdown lives in
// epic_apply_test.go; the pure gate derivation the client repeats lives in
// packages/core/projects/epic.test.ts.

// epicProject creates a project with a Mika agent, and removes everything the
// pipeline leaves behind — including the host issue the first generate creates,
// which no fixture registered.
func epicProject(t *testing.T) string {
	t.Helper()
	syncIssueCounter(t)
	projectID := dbfx.Project(t, "epic project "+uuid.NewString()[:8])
	agentID := dbfx.Agent(t, "epic mika "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	dbfx.Exec(t, `UPDATE agent SET system_key = $2 WHERE id = $1`, agentID, service.MikaSystemKey)
	t.Cleanup(func() {
		// NOT context.Background(): Go cancels it just before cleanups run.
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM epic_artifact WHERE project_id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM issue_dependency WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1) OR depends_on_issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE workspace_id = $1 AND action LIKE 'epic_%'`, testWorkspaceID)
	})
	return projectID
}

func getEpic(t *testing.T, projectID string) EpicEnvelope {
	t.Helper()
	var out EpicEnvelope
	testutil.Call(t, testHandler.GetProjectEpic, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/projects/"+projectID+"/epic", nil), "id", projectID),
	).Want(http.StatusOK).JSON(&out)
	return out
}

func generateEpicStep(t *testing.T, projectID, kind string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.GenerateProjectEpicStep, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/projects/"+projectID+"/epic/steps/"+kind+"/generate", body),
		"id", projectID, "kind", kind))
}

func putEpicStep(t *testing.T, projectID, kind string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutProjectEpicStep, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/projects/"+projectID+"/epic/steps/"+kind, body),
		"id", projectID, "kind", kind))
}

func approveEpicStep(t *testing.T, projectID, kind string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.ApproveProjectEpicStep, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/projects/"+projectID+"/epic/steps/"+kind+"/approve", nil),
		"id", projectID, "kind", kind))
}

func applyEpicStep(t *testing.T, projectID, kind string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.ApplyProjectEpicTickets, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/projects/"+projectID+"/epic/steps/"+kind+"/apply", nil),
		"id", projectID, "kind", kind))
}

// writeAndApprove is the shortest path to "this step is behind us".
func writeAndApprove(t *testing.T, projectID, kind, content string, payload map[string]any) {
	t.Helper()
	body := map[string]any{"content": content}
	if payload != nil {
		body["payload"] = payload
	}
	putEpicStep(t, projectID, kind, body).Want(http.StatusOK)
	approveEpicStep(t, projectID, kind).Want(http.StatusOK)
}

func completeEpicTask(t *testing.T, taskID, output string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete task %s: %d %s", taskID, w.Code, w.Body.String())
	}
}

func failEpicTask(t *testing.T, taskID, reason string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail",
		map[string]any{"error": reason}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.FailTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fail task %s: %d %s", taskID, w.Code, w.Body.String())
	}
}

// --- the gate --------------------------------------------------------------

func TestEpicGateRefusesAStepWhoseParentIsNotApproved(t *testing.T) {
	projectID := epicProject(t)

	// Acceptance 1: generating tech_plan without an approved PRD is refused,
	// and the refusal names the step that is missing.
	for _, endpoint := range []struct {
		name string
		call func() *testutil.Response
	}{
		{"generate", func() *testutil.Response { return generateEpicStep(t, projectID, epicKindTechPlan, nil) }},
		{"put", func() *testutil.Response {
			return putEpicStep(t, projectID, epicKindTechPlan, map[string]any{"content": "# Plan"})
		}},
		{"approve", func() *testutil.Response { return approveEpicStep(t, projectID, epicKindTechPlan) }},
	} {
		body := endpoint.call().Want(http.StatusConflict).Map()
		if body["code"] != ErrCodeEpicStepLocked {
			t.Errorf("%s: code = %v, want %s", endpoint.name, body["code"], ErrCodeEpicStepLocked)
		}
		if msg, _ := body["error"].(string); msg == "" || !strings.Contains(msg, epicKindPRD) {
			t.Errorf("%s: message %q does not name the missing prd step", endpoint.name, msg)
		}
	}

	// A draft is not an approval: the gate only opens on `approved`.
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "# PRD"}).Want(http.StatusOK)
	generateEpicStep(t, projectID, epicKindTechPlan, nil).Want(http.StatusConflict)
}

func TestEpicApprovingAStepUnlocksTheNextOne(t *testing.T) {
	projectID := epicProject(t)

	if got := getEpic(t, projectID).NextKind; got != epicKindPRD {
		t.Fatalf("next_kind on an empty epic = %q, want %q", got, epicKindPRD)
	}
	// Acceptance 2: approving freezes the step and unlocks its successor.
	writeAndApprove(t, projectID, epicKindPRD, "# PRD\nThe problem.", nil)

	epic := getEpic(t, projectID)
	if epic.Steps[epicKindPRD].Approved == nil {
		t.Fatal("prd has no approved artifact after approval")
	}
	if epic.NextKind != epicKindTechPlan {
		t.Fatalf("next_kind = %q, want %q", epic.NextKind, epicKindTechPlan)
	}
	putEpicStep(t, projectID, epicKindTechPlan, map[string]any{"content": "# Tech plan"}).Want(http.StatusOK)

	// Approving twice is refused: there is no second draft.
	approveEpicStep(t, projectID, epicKindPRD).Want(http.StatusConflict)
}

func TestEpicEditingAnApprovedStepSupersedesItAndReopensLaterSteps(t *testing.T) {
	projectID := epicProject(t)
	writeAndApprove(t, projectID, epicKindPRD, "# PRD v1", nil)
	writeAndApprove(t, projectID, epicKindTechPlan, "# Plan v1", nil)

	// Acceptance 3: editing an approved step creates a draft, marks the old
	// version superseded, and says which later steps it reopened.
	var out EpicStepWriteResponse
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "# PRD v2"}).Want(http.StatusOK).JSON(&out)
	if out.Artifact.Version != 2 || out.Artifact.State != epicStateDraft {
		t.Fatalf("edit produced version %d in state %q; want version 2 in draft", out.Artifact.Version, out.Artifact.State)
	}
	if len(out.ReopenedSteps) != 3 || out.ReopenedSteps[0] != epicKindTechPlan {
		t.Fatalf("reopened_steps = %v; want the three steps after prd", out.ReopenedSteps)
	}

	epic := getEpic(t, projectID)
	if epic.Steps[epicKindPRD].Approved != nil {
		t.Error("the superseded prd still reads as approved")
	}
	if epic.Steps[epicKindTechPlan].Approved != nil {
		t.Error("the tech plan still reads as approved after its premise changed")
	}
	if latest := epic.Steps[epicKindTechPlan].Latest; latest == nil || latest.State != epicStateSuperseded {
		t.Errorf("tech plan latest state = %v, want superseded", latest)
	}
	if epic.NextKind != epicKindPRD {
		t.Errorf("next_kind = %q, want the reopened prd", epic.NextKind)
	}

	// The old version is still readable as what was approved.
	var version1State string
	dbfx.QueryRow(t, `SELECT state FROM epic_artifact WHERE project_id = $1 AND kind = $2 AND version = 1`,
		projectID, epicKindPRD).Scan(&version1State)
	if version1State != epicStateSuperseded {
		t.Errorf("prd v1 state = %q, want superseded", version1State)
	}
}

// --- whitelist -------------------------------------------------------------

func TestEpicUnknownKindIsRefusedBeforeAnyDatabaseAccess(t *testing.T) {
	// Acceptance 6: the whitelist runs before the project is even resolved, so
	// a nonexistent project id with a bad kind still answers 400, not 404.
	missingProject := uuid.NewString()
	for _, call := range []func() *testutil.Response{
		func() *testutil.Response { return generateEpicStep(t, missingProject, "roadmap", nil) },
		func() *testutil.Response {
			return putEpicStep(t, missingProject, "roadmap", map[string]any{"content": "x"})
		},
		func() *testutil.Response { return approveEpicStep(t, missingProject, "roadmap") },
		func() *testutil.Response { return applyEpicStep(t, missingProject, "roadmap") },
	} {
		call().Want(http.StatusBadRequest)
	}
	// Only the tickets step can be applied, and that too is a 400.
	applyEpicStep(t, epicProject(t), epicKindPRD).Want(http.StatusBadRequest)
}

// --- generation ------------------------------------------------------------

func TestEpicGenerateEnqueuesOneRunAndRefusesASecond(t *testing.T) {
	projectID := epicProject(t)

	var out EpicGenerateResponse
	generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusAccepted).JSON(&out)
	if out.TaskID == "" || out.Kind != epicKindPRD {
		t.Fatalf("generate answered %+v; want a task id for the prd step", out)
	}
	var legRole string
	dbfx.QueryRow(t, `SELECT leg_role FROM agent_task_queue WHERE id = $1`, out.TaskID).Scan(&legRole)
	if legRole != service.LegRoleEpicStep {
		t.Errorf("leg_role = %q, want %q — the settle hook keys on it", legRole, service.LegRoleEpicStep)
	}

	epic := getEpic(t, projectID)
	if !epic.Steps[epicKindPRD].Generating {
		t.Error("the prd step does not report generating while its run is out")
	}
	if epic.Steps[epicKindPRD].Latest != nil {
		t.Error("a generation claim leaked into `latest`; it is not an artifact yet")
	}
	if epic.EpicIssueID == "" {
		t.Error("the first generate did not create the epic host issue")
	}

	// A second generate for the same step is refused while the first is out.
	body := generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusConflict).Map()
	if body["code"] != ErrCodeEpicGenerating {
		t.Errorf("code = %v, want %s", body["code"], ErrCodeEpicGenerating)
	}
}

func TestEpicGenerateWithoutAnAgentIsAConflictNotAFault(t *testing.T) {
	syncIssueCounter(t)
	projectID := dbfx.Project(t, "epic no agent "+uuid.NewString()[:8])
	dbfx.Cleanup(t, `DELETE FROM epic_artifact WHERE project_id = $1`, projectID)
	// No Mika in this workspace for the duration of the test.
	dbfx.Exec(t, `UPDATE agent SET system_key = NULL WHERE workspace_id = $1 AND system_key = $2`,
		testWorkspaceID, service.MikaSystemKey)

	body := generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusConflict).Map()
	if body["code"] != ErrCodeEpicNoAgent {
		t.Fatalf("code = %v, want %s", body["code"], ErrCodeEpicNoAgent)
	}
	// Nothing was claimed, so the step is not stuck generating.
	if getEpic(t, projectID).Steps[epicKindPRD].Generating {
		t.Error("a refused generate left the step reporting generating")
	}
}

// --- completion ------------------------------------------------------------

func TestEpicRunCompletionBecomesADraftVersion(t *testing.T) {
	projectID := epicProject(t)
	var gen EpicGenerateResponse
	generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusAccepted).JSON(&gen)

	completeEpicTask(t, gen.TaskID, "Here you go.\n\n```epic_step\n"+
		`{"kind":"prd","content":"# PRD\nThe problem statement.","payload":{"scope":["a"]}}`+
		"\n```\n")

	epic := getEpic(t, projectID)
	step := epic.Steps[epicKindPRD]
	if step.Generating {
		t.Error("the step still reports generating after its run settled")
	}
	if step.Latest == nil || step.Latest.State != epicStateDraft {
		t.Fatalf("latest = %+v; want a draft", step.Latest)
	}
	if step.Latest.Content == "" || step.Latest.GeneratedBy != gen.TaskID {
		t.Errorf("settled artifact = %+v; want the run's content and task id", step.Latest)
	}
	if step.Approved != nil {
		t.Error("a generated step approved itself; the gate exists so a human does that")
	}

	// A replayed completion is inert: the claim is already filled.
	completeEpicTask(t, gen.TaskID, "```epic_step\n"+`{"kind":"prd","content":"# Rewritten"}`+"\n```")
	if got := getEpic(t, projectID).Steps[epicKindPRD].Latest.Content; got == "# Rewritten" {
		t.Error("a replayed completion overwrote a settled artifact")
	}
}

func TestEpicMalformedCompletionReleasesTheClaim(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"no fenced block", "I could not do it."},
		{"unparseable json", "```epic_step\n{not json}\n```"},
		{"empty content", "```epic_step\n" + `{"kind":"prd","content":"   "}` + "\n```"},
		{"a block for another step", "```epic_step\n" + `{"kind":"tech_plan","content":"# Plan"}` + "\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectID := epicProject(t)
			var gen EpicGenerateResponse
			generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusAccepted).JSON(&gen)
			completeEpicTask(t, gen.TaskID, tc.output)

			epic := getEpic(t, projectID)
			if epic.Steps[epicKindPRD].Generating {
				t.Error("the claim survived a malformed answer; the step is stuck generating")
			}
			if epic.Steps[epicKindPRD].Latest != nil {
				t.Errorf("a malformed answer produced an artifact: %+v", epic.Steps[epicKindPRD].Latest)
			}
			var audits int
			dbfx.QueryRow(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND entity_id = $3`,
				testWorkspaceID, AuditEpicStepFailed, projectID).Scan(&audits)
			if audits == 0 {
				t.Error("a malformed answer left no audit entry")
			}
			// The step is generatable again.
			generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusAccepted)
		})
	}
}

func TestEpicCrashedRunReleasesTheClaim(t *testing.T) {
	projectID := epicProject(t)
	var gen EpicGenerateResponse
	generateEpicStep(t, projectID, epicKindPRD, nil).Want(http.StatusAccepted).JSON(&gen)

	failEpicTask(t, gen.TaskID, "the runtime died")

	if getEpic(t, projectID).Steps[epicKindPRD].Generating {
		t.Fatal("a crashed run left the step generating forever")
	}
}

// --- versions --------------------------------------------------------------

func TestEpicVersionsIncrementPerKindAndOnlyTheNewestIsLive(t *testing.T) {
	projectID := epicProject(t)
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "# v1"}).Want(http.StatusOK)
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "# v2"}).Want(http.StatusOK)
	approveEpicStep(t, projectID, epicKindPRD).Want(http.StatusOK)
	putEpicStep(t, projectID, epicKindTechPlan, map[string]any{"content": "# plan v1"}).Want(http.StatusOK)

	// Versions are per (project, kind): the tech plan starts at 1 even though
	// the PRD is already on 2.
	var prdVersion, planVersion int32
	dbfx.QueryRow(t, `SELECT max(version) FROM epic_artifact WHERE project_id = $1 AND kind = $2`, projectID, epicKindPRD).Scan(&prdVersion)
	dbfx.QueryRow(t, `SELECT max(version) FROM epic_artifact WHERE project_id = $1 AND kind = $2`, projectID, epicKindTechPlan).Scan(&planVersion)
	if prdVersion != 2 || planVersion != 1 {
		t.Fatalf("versions: prd = %d, tech_plan = %d; want 2 and 1", prdVersion, planVersion)
	}
	var live int
	dbfx.QueryRow(t, `SELECT count(*) FROM epic_artifact WHERE project_id = $1 AND kind = $2 AND state <> 'superseded'`,
		projectID, epicKindPRD).Scan(&live)
	if live != 1 {
		t.Fatalf("%d prd versions are live; only the newest may be", live)
	}
}

func TestEpicEditRejectsEmptyContentAndOversizeContent(t *testing.T) {
	projectID := epicProject(t)
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "   "}).Want(http.StatusBadRequest)
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": string(make([]byte, epicContentMaxBytes+1))}).
		Want(http.StatusBadRequest)
	// A rejected edit writes nothing.
	if getEpic(t, projectID).Steps[epicKindPRD].Latest != nil {
		t.Error("a rejected edit created an artifact")
	}
}

// --- unknown state ---------------------------------------------------------

func TestEpicUnknownStateIsReturnedAsData(t *testing.T) {
	// Acceptance 7's server half: a state a newer build wrote is served back
	// verbatim rather than mapped onto a state this build knows. The client
	// half — rendering it without breaking — lives in epic-panel.test.tsx.
	projectID := epicProject(t)
	putEpicStep(t, projectID, epicKindPRD, map[string]any{"content": "# PRD"}).Want(http.StatusOK)
	dbfx.Exec(t, `ALTER TABLE epic_artifact DROP CONSTRAINT IF EXISTS epic_artifact_state_check`)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `ALTER TABLE epic_artifact ADD CONSTRAINT epic_artifact_state_check CHECK (state IN ('draft','approved','superseded'))`)
	})
	dbfx.Exec(t, `UPDATE epic_artifact SET state = 'archived' WHERE project_id = $1`, projectID)

	latest := getEpic(t, projectID).Steps[epicKindPRD].Latest
	if latest == nil || latest.State != "archived" {
		t.Fatalf("latest = %+v; want the unknown state served as data", latest)
	}
}

// TestEpicGenerateRejectsMalformedBody guards a real bug: GenerateProjectEpicStep
// discarded the request body's Decode error entirely, so a client that sent a
// non-empty but malformed JSON body (e.g. a typo'd agent_id field) silently
// fell back to the workspace's default Mika agent — the exact same outcome as
// sending no body at all — instead of a 400 telling them the body was rejected.
func TestEpicGenerateRejectsMalformedBody(t *testing.T) {
	projectID := epicProject(t)
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/epic/steps/"+epicKindPRD+"/generate", strings.NewReader(`{"agent_id": `))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = testutil.WithURLParams(req, "id", projectID, "kind", epicKindPRD)
	w := httptest.NewRecorder()
	testHandler.GenerateProjectEpicStep(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("generate with malformed body: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
