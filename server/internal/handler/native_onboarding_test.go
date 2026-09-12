package handler

// Native onboarding (OS plan, chantier 5): the server tells the client when
// the browser path exists, seeds the native runtime with the workspace, and
// refuses — audibly — a native-bound trigger it cannot run.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// withNativeAvailable flips the native runtime's availability for one test.
func withNativeAvailable(t *testing.T, available bool) {
	t.Helper()
	prevLLM := testHandler.NativeAgents.LLM
	prevFn := testHandler.TaskService.NativeRuntimeAvailable
	if available {
		testHandler.NativeAgents.LLM = enabledNativeLLM{}
	} else {
		testHandler.NativeAgents.LLM = nil
	}
	testHandler.TaskService.NativeRuntimeAvailable = testHandler.NativeAgents.Available
	t.Cleanup(func() {
		testHandler.NativeAgents.LLM = prevLLM
		testHandler.TaskService.NativeRuntimeAvailable = prevFn
	})
}

// enabledNativeLLM says "a model is configured" and nothing else: the tests
// here never reach Chat. The embedded nil interface makes any such call a
// clear nil dereference rather than a silent fake answer.
type enabledNativeLLM struct{ service.NativeAgentLLM }

func (enabledNativeLLM) Enabled() bool        { return true }
func (enabledNativeLLM) BaseURL() string      { return "https://llm.test" }
func (enabledNativeLLM) DefaultModel() string { return "test-model" }

func TestConfigSaysWhetherTheNativeRuntimeIsAvailable(t *testing.T) {
	for _, available := range []bool{false, true} {
		withNativeAvailable(t, available)
		var cfg AppConfig
		testutil.Call(t, testHandler.GetConfig, httptest.NewRequest(http.MethodGet, "/api/config", nil)).Want(http.StatusOK).JSON(&cfg)
		if cfg.NativeRuntimeAvailable != available {
			t.Fatalf("native_runtime_available = %v, want %v", cfg.NativeRuntimeAvailable, available)
		}
	}
}

func TestCreateWorkspaceSeedsTheNativeRuntimeWhenTheServerCanRunIt(t *testing.T) {
	for _, available := range []bool{false, true} {
		withNativeAvailable(t, available)
		slug := "native-seed-" + uuid.NewString()[:8]
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`, slug)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
		})
		var ws struct {
			ID string `json:"id"`
		}
		testutil.Call(t, testHandler.CreateWorkspace, newRequest(http.MethodPost, "/api/workspaces", map[string]any{"name": "Native seed", "slug": slug})).Want(http.StatusCreated).JSON(&ws)
		n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_runtime WHERE workspace_id = $1 AND runtime_mode = 'native' AND daemon_id = 'native' AND visibility = 'public' AND owner_id = $2`, ws.ID, testUserID)
		if available && n != 1 {
			t.Fatalf("available: native runtime rows = %d, want 1", n)
		}
		if !available && n != 0 {
			t.Fatalf("unavailable: native runtime rows = %d, want 0 (an unusable runtime is worse than none)", n)
		}
	}
}

func TestRoutingRefusesANativeBoundTriggerWithoutAModel(t *testing.T) {
	native := dbfx.Runtime(t, "native "+uuid.NewString()[:8], testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native", "visibility": "public", "owner_id": testUserID})
	agent := dbfx.Agent(t, "browser agent "+uuid.NewString()[:8], native, testutil.Cols{"runtime_mode": "native"})
	issue := dbfx.Issue(t, "browser issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
	})

	withNativeAvailable(t, false)
	if _, err := enqueueForIssue(t, issue); err == nil {
		t.Fatal("a native-bound trigger with no model must be refused, not queued")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("queue rows = %d, want 0", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND details->>'code' = $3`, InboxTypeRoutingAlert, issue, service.RoutingProblemNativeUnconfigured); n < 1 {
		t.Fatal("the refusal must reach the inbox with the native_llm_unconfigured code")
	}

	withNativeAvailable(t, true)
	if _, err := enqueueForIssue(t, issue); err != nil {
		t.Fatalf("with a model the trigger queues: %v", err)
	}
}

func TestOnboardingChecklistReadsTheWorkspace(t *testing.T) {
	withNativeAvailable(t, true)
	agent := dbfx.Agent(t, "checklist agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	dbfx.Issue(t, "checklist issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	var out OnboardingChecklist
	testutil.Call(t, testHandler.GetOnboardingChecklist, newRequest(http.MethodGet, "/api/onboarding/checklist", nil)).Want(http.StatusOK).JSON(&out)
	if !out.NativeAvailable || !out.AgentCreated || !out.IssueCreated || out.Complete {
		t.Fatalf("checklist = %+v", out)
	}
	native := dbfx.Runtime(t, "native "+uuid.NewString()[:8], testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native", "visibility": "public", "owner_id": testUserID})
	_ = native
	testutil.Call(t, testHandler.GetOnboardingChecklist, newRequest(http.MethodGet, "/api/onboarding/checklist", nil)).Want(http.StatusOK).JSON(&out)
	if !out.RuntimeReady || (out.RuntimeKind != "native" && out.RuntimeKind != "daemon") {
		t.Fatalf("checklist with a native runtime = %+v", out)
	}
	withNativeAvailable(t, false)
	testutil.Call(t, testHandler.GetOnboardingChecklist, newRequest(http.MethodGet, "/api/onboarding/checklist", nil)).Want(http.StatusOK).JSON(&out)
	if out.NativeAvailable || (out.RuntimeKind == "native" && out.RuntimeReady) {
		t.Fatalf("checklist without a model = %+v", out)
	}
}
