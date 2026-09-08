package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestConnectedMemoryExecution(t *testing.T) {
	modes := []string{"exact", "json"}
	if image := os.Getenv("MULTICA_EVAL_NODE_TEST_IMAGE"); image != "" {
		t.Setenv("MULTICA_MEMORY_CODE_IMAGE", image)
		modes = append(modes, "javascript")
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			expected, baseline, candidate := "fixed", "baseline", "fixed"
			if mode == "json" {
				expected = `{"queue":"billing","hours":24}`
				baseline = `{"queue":"general","hours":48}`
				candidate = `{"hours":24.0,"queue":"billing"}`
			}
			if mode == "javascript" {
				expected = `[{"args":[2],"expected":4}]`
				baseline = `module.exports = x => x*3`
				candidate = `module.exports = x => x*2`
			}

			agentID, memoryID, _ := evaluationFixture(t)
			runtimeID := cliAuthRuntime(t, "online")
			dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2, metadata='{"capabilities":["memory-evaluation-v1"]}' WHERE id=$1`, runtimeID, "memory-"+runtimeID)
			dbfx.Exec(t, `UPDATE agent SET runtime_id=$2, model='fixture-model', instructions='Frozen instructions' WHERE id=$1`, agentID, runtimeID)
			cases := []memoryeval.ConnectedCase{{ID: "replay", Split: "replay", Prompt: "First prompt", Expected: expected, Check: mode}, {ID: "holdout", Split: "holdout", Prompt: "Different prompt", Expected: expected, Check: mode}}
			var config map[string]any
			testutil.Call(t, testHandler.GetMemoryExecutionConfig, agentMemoryRequest("GET", agentID, memoryID, nil)).Want(200).JSON(&config)
			body := map[string]any{"request_id": "00000000-0000-4000-8000-000000000077", "expected_revision": 1, "config_hash": config["config_hash"], "cases": cases}
			var saved, duplicate AgentMemoryEvaluationResponse
			testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(201).JSON(&saved)
			testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(200).JSON(&duplicate)
			if saved.ID != duplicate.ID || saved.ExecutionStatus != "queued" || saved.Eligible {
				t.Fatal("launch was duplicated or prematurely eligible")
			}
			ack, _, err := testHandler.processHeartbeat(context.Background(), runtimeID, false)
			if err != nil || ack.PendingMemoryEvaluation != saved.ID {
				t.Fatalf("missing wakeup: %+v %v", ack, err)
			}
			daemon := func(handler http.HandlerFunc, rt, ws string, body any) *testutil.Response {
				req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+rt+"/memory-evaluations/"+saved.ID, body, ws, "memory-"+rt)
				return testutil.Call(t, handler, withURLParams(req, "runtimeId", rt, "evaluationId", saved.ID))
			}
			// A workspace credential alone cannot operate another runtime's evaluation.
			viewer := dbfx.User(t, "Evaluation runtime viewer", memoryID+"-runtime@example.test")
			dbfx.Member(t, testWorkspaceID, viewer, "member")
			for _, handler := range []http.HandlerFunc{testHandler.ClaimMemoryExecution, testHandler.ReportMemoryExecution} {
				foreignDaemon := newDaemonTokenRequest("POST", "/evaluation", nil, testWorkspaceID, "other-daemon")
				testutil.Call(t, handler, withURLParams(foreignDaemon, "runtimeId", runtimeID, "evaluationId", saved.ID)).Want(403)
				missingDaemon := newDaemonTokenRequest("POST", "/evaluation", nil, testWorkspaceID, "")
				missingDaemon.Header.Set("X-User-ID", testUserID)
				testutil.Call(t, handler, withURLParams(missingDaemon, "runtimeId", runtimeID, "evaluationId", saved.ID)).Want(403)
				legacyMember := newRequest("POST", "/evaluation", nil)
				legacyMember.Header.Set("X-User-ID", viewer)
				testutil.Call(t, handler, withURLParams(legacyMember, "runtimeId", runtimeID, "evaluationId", saved.ID)).Want(403)
			}
			foreign := cliAuthRuntime(t, "online")
			dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2 WHERE id=$1`, foreign, "memory-"+foreign)
			daemon(testHandler.ClaimMemoryExecution, foreign, testWorkspaceID, nil).Want(409)
			var job memoryeval.ConnectedJob
			daemon(testHandler.ClaimMemoryExecution, runtimeID, testWorkspaceID, nil).Want(200).JSON(&job)
			if job.Config.Instructions != "Frozen instructions" || job.Config.Provider != "codex" || job.Cases[0].Expected != "" {
				t.Fatalf("incorrect snapshot or leaked check: %+v", job)
			}
			raw, _ := json.Marshal(job)
			if strings.Contains(string(raw), "expected") {
				t.Fatal("answer sent to runtime")
			}
			daemon(testHandler.ClaimMemoryExecution, runtimeID, testWorkspaceID, nil).Want(409)
			for i := 0; i < 4; i++ {
				artifact := baseline
				if i%2 == 1 {
					artifact = candidate
				}
				result := memoryeval.ConnectedResult{Index: i, Artifact: artifact, DurationMS: 10, Runtime: &memoryeval.RuntimeEvidence{Provider: "codex", RequestedModel: "fixture-model", Status: "completed", ExecutableHash: strings.Repeat("a", 64), PromptHash: strings.Repeat("b", 64), BriefHash: strings.Repeat("c", 64)}}
				if i == 0 {
					bad := result
					bad.Index = 1
					daemon(testHandler.ReportMemoryExecution, runtimeID, testWorkspaceID, bad).Want(409)
				}
				if i == 1 {
					// Legacy user-token daemons may report for a runtime they own/administer.
					legacyOwner := newRequest("POST", "/evaluation", result)
					testutil.Call(t, testHandler.ReportMemoryExecution, withURLParams(legacyOwner, "runtimeId", runtimeID, "evaluationId", saved.ID)).Want(200)
				} else {
					daemon(testHandler.ReportMemoryExecution, runtimeID, testWorkspaceID, result).Want(200)
				}
				daemon(testHandler.ReportMemoryExecution, runtimeID, testWorkspaceID, result).Want(200)
				result.Artifact = "changed duplicate"
				daemon(testHandler.ReportMemoryExecution, runtimeID, testWorkspaceID, result).Want(409)
			}
			var detail AgentMemoryEvaluationResponse
			testutil.Call(t, testHandler.GetAgentMemoryEvaluation, withURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(200).JSON(&detail)
			if !detail.Eligible || detail.ExecutionStatus != "completed" || detail.BaselinePassed != 0 || detail.CandidatePassed != 2 {
				t.Fatalf("invalid graded report: %+v", detail)
			}
			testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 1, "evaluation_id": saved.ID})).Want(200)
			// Humans must be able to review/export the exact checks; machine actors cannot.
			if detail.Report == nil || len(detail.Report.Suite.TextCases) != 2 || detail.Report.Suite.TextCases[0].Expected != expected {
				t.Fatal("human report lost the expected answers needed for review/export")
			}
			machineDetail := withURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)
			machineDetail.Header.Set("X-Actor-Source", "task_token")
			testutil.Call(t, testHandler.GetAgentMemoryEvaluation, machineDetail).Want(403)
			// A launch with different model settings must refresh its preview before spending.
			dbfx.Exec(t, `UPDATE agent_memory SET status='pending' WHERE id=$1`, memoryID)
			dbfx.Exec(t, `UPDATE agent SET model='changed-model' WHERE id=$1`, agentID)
			body["request_id"] = "00000000-0000-4000-8000-000000000078"
			body["expected_revision"] = 2
			testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(409)
		})
	}
}

func TestConnectedMemoryCancellationAndIdentity(t *testing.T) {
	agentID, memoryID, _ := evaluationFixture(t)
	runtimeID := cliAuthRuntime(t, "online")
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2, metadata='{"capabilities":["memory-evaluation-v1"]}' WHERE id=$1`, runtimeID, "memory-"+runtimeID)
	dbfx.Exec(t, `UPDATE agent SET runtime_id=$2,model='fixture' WHERE id=$1`, agentID, runtimeID)
	var config map[string]any
	testutil.Call(t, testHandler.GetMemoryExecutionConfig, agentMemoryRequest("GET", agentID, memoryID, nil)).Want(200).JSON(&config)
	body := map[string]any{"request_id": "00000000-0000-4000-8000-000000000088", "expected_revision": 1, "config_hash": config["config_hash"], "cases": []memoryeval.ConnectedCase{{ID: "a", Split: "replay", Prompt: "a", Expected: "answer"}, {ID: "b", Split: "holdout", Prompt: "b", Expected: "answer"}}}
	machine := agentMemoryRequest("POST", agentID, memoryID, body)
	machine.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.StartMemoryExecution, machine).Want(403)
	var saved AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(201).JSON(&saved)
	body["request_id"] = "00000000-0000-4000-8000-000000000089"
	testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(409)
	req := withURLParams(agentMemoryRequest("POST", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)
	testutil.Call(t, testHandler.CancelMemoryExecution, req).Want(204)
	_, err := testHandler.Queries.ClaimAgentMemoryEvaluation(context.Background(), db.ClaimAgentMemoryEvaluationParams{ID: parseUUID(saved.ID), ExecutionRuntimeID: parseUUID(runtimeID)})
	if err == nil {
		t.Fatal("cancelled evaluation claimed")
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agentID, memoryID, map[string]any{"status": "active", "expected_revision": 1, "evaluation_id": saved.ID})).Want(409)
}

func TestConnectedMemoryExecutionCatalogCostEstimate(t *testing.T) {
	agentID, memoryID, _ := evaluationFixture(t)
	runtimeID := cliAuthRuntime(t, "online")
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2, metadata='{"capabilities":["memory-evaluation-v1"]}' WHERE id=$1`, runtimeID, "memory-"+runtimeID)
	dbfx.Exec(t, `UPDATE agent SET runtime_id=$2, model='gpt-5.6-luna', instructions='Frozen' WHERE id=$1`, agentID, runtimeID)
	var config map[string]any
	testutil.Call(t, testHandler.GetMemoryExecutionConfig, agentMemoryRequest("GET", agentID, memoryID, nil)).Want(200).JSON(&config)
	body := map[string]any{"request_id": "00000000-0000-4000-8000-000000000099", "expected_revision": 1, "config_hash": config["config_hash"], "cases": []memoryeval.ConnectedCase{{ID: "replay", Split: "replay", Prompt: "First", Expected: "ok"}, {ID: "holdout", Split: "holdout", Prompt: "Second", Expected: "ok"}}}
	var saved AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.StartMemoryExecution, agentMemoryRequest("POST", agentID, memoryID, body)).Want(201).JSON(&saved)
	daemon := func(body any) *testutil.Response {
		req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/memory-evaluations/"+saved.ID, body, testWorkspaceID, "memory-"+runtimeID)
		return testutil.Call(t, testHandler.ReportMemoryExecution, withURLParams(req, "runtimeId", runtimeID, "evaluationId", saved.ID))
	}
	testutil.Call(t, testHandler.ClaimMemoryExecution, withURLParams(newDaemonTokenRequest("POST", "/claim", nil, testWorkspaceID, "memory-"+runtimeID), "runtimeId", runtimeID, "evaluationId", saved.ID)).Want(200)
	usage := map[string]memoryeval.RuntimeUsage{"gpt-5.6-luna": {InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 0, CacheWriteTokens: 0}}
	for i := 0; i < 4; i++ {
		artifact := "no"
		if i%2 == 1 {
			artifact = "ok"
		}
		daemon(memoryeval.ConnectedResult{Index: i, Artifact: artifact, DurationMS: 10, Runtime: &memoryeval.RuntimeEvidence{Provider: "codex", RequestedModel: "gpt-5.6-luna", RequestedEffort: "", Status: "completed", ExecutableHash: strings.Repeat("a", 64), PromptHash: strings.Repeat("b", 64), BriefHash: strings.Repeat("c", 64), Usage: usage}}).Want(200)
	}
	var detail AgentMemoryEvaluationResponse
	testutil.Call(t, testHandler.GetAgentMemoryEvaluation, withURLParams(agentMemoryRequest("GET", agentID, memoryID, nil), "id", agentID, "memoryId", memoryID, "evaluationId", saved.ID)).Want(200).JSON(&detail)
	if detail.CostStatus != "estimated" || detail.EstimatedCostUSD == nil {
		t.Fatalf("expected catalog estimate: %+v", detail)
	}
	// gpt-5.6-luna: $1/M input + $6/M output × 4 runs = 28
	if *detail.EstimatedCostUSD < 27.999 || *detail.EstimatedCostUSD > 28.001 {
		t.Fatalf("unexpected estimate %v", *detail.EstimatedCostUSD)
	}
	if detail.Report == nil || detail.Report.Cases[0].Candidate.CostSource != "catalog_estimate" || detail.Report.Cases[0].Candidate.CostUSD == nil {
		t.Fatal("per-outcome catalog estimate missing")
	}
}
