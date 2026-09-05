package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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

func TestParseRoutingVerdictToleratesFences(t *testing.T) {
	cases := map[string]bool{
		`{"relevant": false, "reason": "staging"}`:                 false,
		"```json\n{\"relevant\": true, \"reason\": \"prod\"}\n```": true,
	}
	for raw, want := range cases {
		v, ok := parseRoutingVerdict(raw)
		if !ok || v.relevant != want {
			t.Fatalf("parse %q = %+v ok=%v, want relevant=%v", raw, v, ok, want)
		}
	}
	if _, ok := parseRoutingVerdict("I think it is relevant"); ok {
		t.Fatal("prose must not parse as a verdict")
	}
}

func TestScheduleTriggerWindowMinutes(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Window Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	// Only schedule triggers carry a band, bounded to a day.
	testutilCallCreateTrigger(t, apID, map[string]any{"kind": "webhook", "window_minutes": 30}, http.StatusBadRequest)
	testutilCallCreateTrigger(t, apID, map[string]any{"kind": "schedule", "cron_expression": "0 8 * * *", "window_minutes": 1440}, http.StatusBadRequest)
	created := testutilCallCreateTrigger(t, apID, map[string]any{"kind": "schedule", "cron_expression": "0 8 * * *", "timezone": "UTC", "window_minutes": 120}, http.StatusCreated)
	if created["window_minutes"] != float64(120) {
		t.Fatalf("window_minutes = %v", created["window_minutes"])
	}
	// The display-only next run already sits inside the 08:00–10:00 UTC band.
	if createdAt, err := time.Parse(time.RFC3339, created["next_run_at"].(string)); err != nil || createdAt.UTC().Hour() < 8 || createdAt.UTC().Hour() >= 10 {
		t.Fatalf("created next_run_at = %v (%v), want inside 08:00–10:00 UTC", created["next_run_at"], err)
	}
	id, _ := created["id"].(string)
	// PATCH recomputes next_run_at inside the band for this trigger.
	// One route context carrying both params: a second withURLParam would
	// replace the first.
	req := testutil.WithURLParams(newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+id, map[string]any{"window_minutes": 60}), "id", apID, "triggerId", id)
	updated := testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusOK).Map()
	if updated["window_minutes"] != float64(60) {
		t.Fatalf("updated window_minutes = %v", updated["window_minutes"])
	}
	next, _ := updated["next_run_at"].(string)
	at, err := time.Parse(time.RFC3339, next)
	if err != nil {
		t.Fatalf("next_run_at %q: %v", next, err)
	}
	// 08:00 + [0, 60) minutes, judged in the trigger's own timezone.
	if at.UTC().Hour() != 8 {
		t.Fatalf("next_run_at %s is outside the 08:00–09:00 UTC band", at.UTC().Format(time.RFC3339))
	}
}

// A PATCH carrying only the routing rule must apply it: the field used to be
// read only inside the event_filters branch.
func TestWebhookCriteriaPatchAlone(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Criteria Patch Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)
	req := testutil.WithURLParams(newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{"event_match_criteria": "billing incidents only"}), "id", apID, "triggerId", trig.ID)
	updated := testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusOK).Map()
	if updated["event_match_criteria"] != "billing incidents only" {
		t.Fatalf("criteria not applied: %v", updated["event_match_criteria"])
	}
}
