package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func evaluationFixture(t *testing.T) (string, string, memoryeval.Report) {
	t.Helper()
	agentID := agentMemoryFixture(t, "memory-evaluation")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_memory_evaluation WHERE agent_id=$1`, agentID)
	})
	memoryID := dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "content": "Use independent assertions", "status": "pending"})
	now := time.Now().UTC()
	r := memoryeval.Report{Version: 1, Kind: "offline_memory_comparison", ServerURL: "http://local.invalid", StartedAt: now.Add(-time.Second), CompletedAt: &now, Candidate: memoryeval.Memory{ID: memoryID, AgentID: agentID, WorkspaceID: testWorkspaceID, Revision: 1, Content: "Use independent assertions", Status: "pending"}, Baseline: []memoryeval.Memory{}, Suite: memoryeval.Suite{Image: "sha256:" + strings.Repeat("a", 64), Worker: []string{"/worker"}, Verifier: []string{"/verifier"}, TimeoutSeconds: 5, Cases: []memoryeval.Case{{ID: "a", Split: "replay", Input: "a", Checks: "checks"}, {ID: "b", Split: "holdout", Input: "b", Checks: "checks"}}}}
	for i, c := range r.Suite.Cases {
		r.Cases = append(r.Cases, memoryeval.Comparison{ID: c.ID, Split: c.Split, InputHash: strings.Repeat(string(rune('a'+i)), 64), ChecksHash: strings.Repeat("c", 64), Baseline: memoryeval.Outcome{Status: "failed", Artifact: "baseline"}, Candidate: memoryeval.Outcome{Status: "passed", Artifact: "fixed"}})
	}
	return agentID, memoryID, r
}

func TestAgentMemoryEvaluationImportAdoptAndBoundaries(t *testing.T) {
	agentID, memoryID, report := evaluationFixture(t)
	var saved, again AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	if !saved.Eligible || saved.BaselinePassed != 0 || saved.CandidatePassed != 2 || saved.Report != nil {
		t.Fatalf("bad summary: %+v", saved)
	}
	if len(saved.ReportHash) != 64 {
		t.Fatalf("missing server report receipt hash: %q", saved.ReportHash)
	}
	for _, r := range saved.ReportHash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("report_hash must be lowercase hex: %q", saved.ReportHash)
		}
	}
	// Identical evidence has one stable receipt; client-supplied gate is ignored.
	report.Eligible = true
	report.Reason = "forged reason"
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(200).JSON(&again)
	if saved.ID != again.ID || saved.ReportHash != again.ReportHash {
		t.Fatal("duplicate import")
	}
	viewer := dbfx.User(t, "Evaluation viewer", memoryID+"@example.test")
	dbfx.Member(t, testWorkspaceID, viewer, "member")
	for _, handler := range []http.HandlerFunc{testHandler.ListAgentMemoryEvaluations, testHandler.GetAgentMemoryEvaluation, testHandler.CreateAgentMemoryEvaluation, testHandler.DeleteAgentMemoryEvaluation} {
		req := testutil.WithURLParams(agentMemoryRequest("POST", agentID, memoryID, report), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)
		req.Header.Set("X-User-ID", viewer)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != 403 && w.Code != 404 {
			t.Fatalf("non-manager accessed evaluation: %d", w.Code)
		}
	}
	foreign := agentMemoryRequest("GET", agentID, memoryID, nil)
	foreign.Header.Set("X-Workspace-ID", "00000000-0000-4000-8000-000000000001")
	testutil.Call(t, testHandler.ListAgentMemoryEvaluations, foreign).Want(404)
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, op := range []struct {
			method  string
			handler http.HandlerFunc
		}{{"POST", testHandler.CreateAgentMemoryEvaluation}, {"GET", testHandler.ListAgentMemoryEvaluations}, {"GET", testHandler.GetAgentMemoryEvaluation}, {"DELETE", testHandler.DeleteAgentMemoryEvaluation}} {
			req := testutil.WithURLParams(agentMemoryRequest(op.method, agentID, memoryID, report), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)
			req.Header.Set("X-Actor-Source", source)
			testutil.Call(t, op.handler, req).Want(http.StatusForbidden)
		}
	}
	var detail AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.GetAgentMemoryEvaluation, testutil.WithURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(200).JSON(&detail)
	if detail.Report == nil || detail.Report.Cases[1].Candidate.Artifact != "fixed" {
		t.Fatal("lost evidence")
	}
	other := agentMemoryFixture(t, "other-evaluation-agent")
	testutil.Call(t, testHandler.GetAgentMemoryEvaluation, testutil.WithURLParams(agentMemoryRequest("GET", other, memoryID, nil), "id", other, "memoryId", memoryID, "evaluationId", saved.ID)).Want(404)
	bad := report
	bad.Candidate.Content = "different"
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, bad)).Want(409)
	bad = report
	bad.Candidate.WorkspaceID = "00000000-0000-4000-8000-000000000001"
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, bad)).Want(400)
	// One adoption wins and records the adopted revision atomically.
	responses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			testHandler.UpdateAgentMemory(w, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 1, "evaluation_id": saved.ID}))
			responses <- w.Code
		}()
	}
	a, b := <-responses, <-responses
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("concurrent adoption %d %d", a, b)
	}
	testutil.Call(t, testHandler.GetAgentMemoryEvaluation, testutil.WithURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(200).JSON(&detail)
	if detail.AdoptedRevision == nil || *detail.AdoptedRevision != 2 {
		t.Fatal("lost adoption receipt")
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"restore_revision": 1, "expected_revision": 2})).Want(200)
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 3, "evaluation_id": saved.ID})).Want(409)
	testutil.Call(t, testHandler.DeleteAgentMemory, agentMemoryRequest("DELETE", agentID, memoryID, nil)).Want(204)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_evaluation WHERE id=$1`, saved.ID).Scan(&count)
	if count != 0 {
		t.Fatal("deleted memory retained its report")
	}
}

func TestAgentMemoryEvaluationLocksBaselineAndDeletesCopies(t *testing.T) {
	agentID, memoryID, report := evaluationFixture(t)
	var saved AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := testHandler.Queries.WithTx(tx)
	if _, err := q.LockAgentForMemoryUpdate(context.Background(), db.LockAgentForMemoryUpdateParams{ID: parseUUID(agentID), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	response := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.UpdateAgentMemory(w, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 1, "evaluation_id": saved.ID}))
		response <- w.Code
	}()
	var baselineID string
	if err := tx.QueryRow(context.Background(), `INSERT INTO agent_memory (workspace_id,agent_id,content,status) VALUES ($1,$2,'Baseline changed','active') RETURNING id::text`, testWorkspaceID, agentID).Scan(&baselineID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := <-response; code != 409 {
		t.Fatalf("racing baseline accepted: %d", code)
	}
	report.Baseline = []memoryeval.Memory{{ID: baselineID, AgentID: agentID, WorkspaceID: testWorkspaceID, Revision: 1, Content: "Baseline changed", Status: "active"}}
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	testutil.Call(t, testHandler.DeleteAgentMemory, agentMemoryRequest("DELETE", agentID, baselineID, nil)).Want(204)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_evaluation WHERE id=$1`, saved.ID).Scan(&count)
	if count != 0 {
		t.Fatal("deleted baseline still recoverable from report")
	}
	// Failed results can be kept, but cannot promote even with an imported true flag.
	report.Baseline = []memoryeval.Memory{}
	report.Cases[1].Candidate.Status = "failed"
	report.Eligible = true
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	if saved.Eligible || saved.CandidatePassed != 1 {
		t.Fatal("trusted client eligibility")
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 1, "evaluation_id": saved.ID})).Want(409)
	testutil.Call(t, testHandler.DeleteAgentMemoryEvaluation, testutil.WithURLParams(agentMemoryRequest("DELETE", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(204)
}

// checkEvaluationVersions batches the current-memory lookup and, for items
// that turn out stale, the version lookup, into one query each instead of
// one GetAgentMemory/GetAgentMemoryVersion call per item (up to 1 candidate +
// 199 baseline); this puts two baseline memories in one report where only
// ONE has since moved to a new revision, so a batching bug that hands
// baseline A's current row or version to baseline B (or vice versa) fails
// this test instead of shipping silently.
func TestAgentMemoryEvaluationBatchesBaselinesWithOneStaleWithoutMixingVersions(t *testing.T) {
	agentID, memoryID, report := evaluationFixture(t)

	baselineA := dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "content": "Baseline A original", "status": "active"})
	baselineB := dbfx.Insert(t, "agent_memory", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agentID, "content": "Baseline B original", "status": "active"})
	report.Baseline = []memoryeval.Memory{
		{ID: baselineA, AgentID: agentID, WorkspaceID: testWorkspaceID, Revision: 1, Content: "Baseline A original", Status: "active"},
		{ID: baselineB, AgentID: agentID, WorkspaceID: testWorkspaceID, Revision: 1, Content: "Baseline B original", Status: "active"},
	}

	// Baseline B moves on before the report is submitted: its live row is now
	// revision 2 with different content, so checkEvaluationVersions must fall
	// back to the archived revision-1 version for B specifically, while A
	// (never touched) is read straight from its current row.
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, baselineB, map[string]any{"content": "Baseline B moved on", "expected_revision": 1})).Want(http.StatusOK)

	var saved AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(http.StatusCreated).JSON(&saved)
	if !saved.Eligible {
		t.Fatalf("evaluation must still be accepted with one stale baseline resolved from its own version: %+v", saved)
	}

	// A report that (wrongly) claims fresh baseline A had different content
	// must be rejected against A's own current row, proving A and B are
	// checked independently rather than one batch entry leaking onto the
	// other.
	tampered := report
	tampered.Baseline = append([]memoryeval.Memory{}, report.Baseline...)
	tampered.Baseline[0].Content = "Baseline A tampered"
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, tampered)).Want(http.StatusConflict)
}

func TestAgentMemoryEvaluationRejectsInvalidEvidence(t *testing.T) {
	agentID, memoryID, r := evaluationFixture(t)
	for _, mutate := range []func(*memoryeval.Report){
		func(r *memoryeval.Report) { r.Suite.Worker = []string{"/bin/sh", "\x00"} },
		func(r *memoryeval.Report) { r.Cases[0].Candidate.Artifact = strings.Repeat("x", 65537) },
		func(r *memoryeval.Report) { cost := 1.0; r.Cases[0].Candidate.CostUSD = &cost },
		func(r *memoryeval.Report) { r.CompletedAt = &time.Time{} },
	} {
		encoded, _ := json.Marshal(r)
		var bad memoryeval.Report
		json.Unmarshal(encoded, &bad)
		mutate(&bad)
		testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, bad)).Want(400)
	}
}

func TestAgentMemoryRuntimeEvaluation(t *testing.T) {
	agentID, memoryID, report := evaluationFixture(t)
	report.Suite.WorkerProtocol = "multica_runtime_v1"
	for i := range report.Cases {
		a := &memoryeval.RuntimeEvidence{Provider: "claude", RequestedModel: "fixture", ExecutableHash: strings.Repeat("a", 64), PromptHash: strings.Repeat("b", 64), BriefHash: strings.Repeat("c", 64), Status: "completed", ToolCalls: 1}
		b := *a
		report.Cases[i].Baseline.Runtime = a
		report.Cases[i].Candidate.Runtime = &b
	}
	var saved AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	if !saved.Eligible {
		t.Fatal("valid runtime evidence rejected")
	}
	var detail AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.GetAgentMemoryEvaluation, testutil.WithURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(200).JSON(&detail)
	if detail.Report.Cases[0].Candidate.Runtime.ToolCalls != 1 {
		t.Fatal("runtime observations lost")
	}
	for _, hash := range []*string{&report.Cases[0].Candidate.Runtime.ExecutableHash, &report.Cases[0].Candidate.Runtime.PromptHash, &report.Cases[0].Candidate.Runtime.BriefHash} {
		original := *hash
		*hash = strings.ToUpper(original)
		testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(400)
		*hash = original
	}
	report.Cases[1].Candidate.Runtime.RequestedModel = "other"
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(201).JSON(&saved)
	if saved.Eligible {
		t.Fatal("changed model accepted")
	}
	report.Cases[1].Candidate.Runtime = nil
	testutil.Call(t, testHandler.CreateAgentMemoryEvaluation, agentMemoryRequest("POST", agentID, memoryID, report)).Want(400)
}
