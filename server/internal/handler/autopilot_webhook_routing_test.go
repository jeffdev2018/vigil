package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The natural-language gate sits between the static event filter and run
// admission: a "not relevant" verdict records an ignored delivery, anything
// else — including no classifier at all — lets the run happen.
func TestWebhookEventMatchCriteriaGate(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Routing Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_trigger SET event_match_criteria = $2 WHERE id = $1`,
		trig.ID, "only production deployment failures"); err != nil {
		t.Fatalf("set criteria: %v", err)
	}
	body := map[string]any{
		"event":        "deploy.finished",
		"eventPayload": map[string]any{"environment": "staging", "status": "ok"},
	}

	// 1. Classifier says no → ignored with the reason, no run admitted.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"relevant": false, "reason": "staging, and it succeeded"}`))
	w := postWebhook(t, *trig.WebhookToken, body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var ignored map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ignored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ignored["status"] != "ignored" || ignored["reason"] != "criteria_not_matched" || ignored["explanation"] != "staging, and it succeeded" {
		t.Fatalf("ignored response = %v", ignored)
	}

	// 2. Classifier says yes → accepted like any other delivery.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"relevant": true, "reason": "production failure"}`))
	w = postWebhook(t, *trig.WebhookToken, map[string]any{
		"event":        "deploy.finished",
		"eventPayload": map[string]any{"environment": "production", "status": "failed"},
	}, nil)
	requireAcceptedWebhookResponse(t, w)

	// 3. Classifier broken (upstream 500) → fails open, still accepted.
	withStubLLM(t, stubLLMCompletion(t, http.StatusInternalServerError, ""))
	w = postWebhook(t, *trig.WebhookToken, map[string]any{
		"event":        "deploy.finished",
		"eventPayload": map[string]any{"environment": "staging", "n": 3},
	}, nil)
	requireAcceptedWebhookResponse(t, w)
}

func TestWebhookEventMatchCriteriaValidation(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Routing Validation Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'x'
	}
	testutilCallCreateTrigger(t, apID, map[string]any{"kind": "webhook", "event_match_criteria": string(long)}, http.StatusBadRequest)
	testutilCallCreateTrigger(t, apID, map[string]any{"kind": "schedule", "cron_expression": "0 9 * * *", "event_match_criteria": "anything"}, http.StatusBadRequest)
	resp := testutilCallCreateTrigger(t, apID, map[string]any{"kind": "webhook", "event_match_criteria": "  billing incidents  "}, http.StatusCreated)
	if resp["event_match_criteria"] != "billing incidents" {
		t.Fatalf("criteria not trimmed/echoed: %v", resp["event_match_criteria"])
	}
}

// testutilCallCreateTrigger posts a trigger body through the handler and
// returns the decoded response after asserting the status.
func testutilCallCreateTrigger(t *testing.T, autopilotID string, body map[string]any, want int) map[string]any {
	t.Helper()
	req := withURLParam(newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers", body), "id", autopilotID)
	return testutil.Call(t, testHandler.CreateAutopilotTrigger, req).Want(want).Map()
}
