package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// dryRunWebhook posts a sample payload through the dry-run endpoint.
func dryRunWebhook(t *testing.T, autopilotID, triggerID string, body map[string]any) DryRunWebhookTriggerResponse {
	t.Helper()
	req := testutil.WithURLParams(
		newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers/"+triggerID+"/dry-run", body),
		"id", autopilotID, "triggerId", triggerID,
	)
	return testutil.Decode[DryRunWebhookTriggerResponse](t, testHandler.DryRunAutopilotWebhookTrigger, req, http.StatusOK)
}

func reasonOf(t *testing.T, resp DryRunWebhookTriggerResponse) string {
	t.Helper()
	if resp.ReasonCode == nil {
		return ""
	}
	return *resp.ReasonCode
}

// The dry-run must reach the same verdict as ingress for every step of the
// chain, and must leave no delivery row behind.
func TestDryRunWebhookTrigger(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DryRun Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)

	samplePush := map[string]any{
		"payload": map[string]any{"action": "opened", "number": 7},
		"headers": map[string]string{"X-GitHub-Event": "pull_request"},
	}

	t.Run("no filters, no criteria: runs", func(t *testing.T) {
		resp := dryRunWebhook(t, apID, trig.ID, samplePush)
		if !resp.WouldRun || reasonOf(t, resp) != "" {
			t.Fatalf("would_run=%v reason=%q, want a clean run", resp.WouldRun, reasonOf(t, resp))
		}
		if resp.Event != "github.pull_request.opened" {
			t.Fatalf("event = %q, want the header-inferred name", resp.Event)
		}
		if len(resp.MatchedFilters) != 0 {
			t.Fatalf("matched_filters = %v, want empty when the trigger declares none", resp.MatchedFilters)
		}
	})

	t.Run("event filter not satisfied", func(t *testing.T) {
		if _, err := testPool.Exec(context.Background(),
			`UPDATE autopilot_trigger SET event_filters = $2::jsonb WHERE id = $1`,
			trig.ID, `[{"event":"issues","actions":["opened"]}]`); err != nil {
			t.Fatalf("set filters: %v", err)
		}
		resp := dryRunWebhook(t, apID, trig.ID, samplePush)
		if resp.WouldRun || reasonOf(t, resp) != reasonEventFiltered {
			t.Fatalf("would_run=%v reason=%q, want event_filtered", resp.WouldRun, reasonOf(t, resp))
		}
	})

	t.Run("event filter satisfied names the matching row", func(t *testing.T) {
		if _, err := testPool.Exec(context.Background(),
			`UPDATE autopilot_trigger SET event_filters = $2::jsonb WHERE id = $1`,
			trig.ID, `[{"event":"issues"},{"event":"pull_request","actions":["opened","closed"]}]`); err != nil {
			t.Fatalf("set filters: %v", err)
		}
		resp := dryRunWebhook(t, apID, trig.ID, samplePush)
		if !resp.WouldRun {
			t.Fatalf("would_run=false reason=%q, want the pull_request filter to admit it", reasonOf(t, resp))
		}
		if len(resp.MatchedFilters) != 1 || resp.MatchedFilters[0].Event != "pull_request" {
			t.Fatalf("matched_filters = %+v, want exactly the pull_request row", resp.MatchedFilters)
		}
	})

	t.Run("criteria not satisfied", func(t *testing.T) {
		if _, err := testPool.Exec(context.Background(),
			`UPDATE autopilot_trigger SET event_match_criteria = $2 WHERE id = $1`,
			trig.ID, "only security fixes"); err != nil {
			t.Fatalf("set criteria: %v", err)
		}
		withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"relevant": false, "reason": "an ordinary feature PR"}`))
		resp := dryRunWebhook(t, apID, trig.ID, samplePush)
		if resp.WouldRun || reasonOf(t, resp) != reasonCriteriaNotMatched {
			t.Fatalf("would_run=%v reason=%q, want criteria_not_matched", resp.WouldRun, reasonOf(t, resp))
		}
		if resp.Explanation != "an ordinary feature PR" {
			t.Fatalf("explanation = %q, want the classifier's own sentence", resp.Explanation)
		}
	})

	t.Run("criteria satisfied", func(t *testing.T) {
		withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"relevant": true, "reason": "touches auth"}`))
		resp := dryRunWebhook(t, apID, trig.ID, samplePush)
		if !resp.WouldRun || resp.Explanation != "touches auth" {
			t.Fatalf("would_run=%v explanation=%q, want a run with the classifier's reason", resp.WouldRun, resp.Explanation)
		}
	})

	t.Run("records nothing", func(t *testing.T) {
		var deliveries, runs int
		if err := testPool.QueryRow(context.Background(),
			`SELECT (SELECT count(*) FROM webhook_delivery WHERE autopilot_id = $1),
			        (SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1)`, apID,
		).Scan(&deliveries, &runs); err != nil {
			t.Fatalf("count side effects: %v", err)
		}
		if deliveries != 0 || runs != 0 {
			t.Fatalf("dry-run left %d deliveries and %d runs behind", deliveries, runs)
		}
	})
}

// A paused autopilot loses before the classifier is ever consulted — the
// cheap checks must come first, or a preview costs a model call to say
// something the row already knew.
func TestDryRunWebhookTriggerPausedAutopilot(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DryRun Paused Agent")
	apID := createWebhookTestAutopilot(t, agentID, "paused", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_trigger SET event_match_criteria = $2 WHERE id = $1`,
		trig.ID, "anything at all"); err != nil {
		t.Fatalf("set criteria: %v", err)
	}
	// No stub LLM installed: reaching the classifier would fail open and
	// answer would_run=true, which is exactly the bug this guards.
	resp := dryRunWebhook(t, apID, trig.ID, map[string]any{"payload": map[string]any{"x": 1}})
	if resp.WouldRun || reasonOf(t, resp) != reasonAutopilotPaused {
		t.Fatalf("would_run=%v reason=%q, want autopilot_paused", resp.WouldRun, reasonOf(t, resp))
	}
}

func TestDryRunWebhookTriggerRejections(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DryRun Reject Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	webhookTrig := createWebhookTriggerViaHandler(t, apID)

	// Missing payload.
	req := testutil.WithURLParams(
		newRequest("POST", "/api/autopilots/"+apID+"/triggers/"+webhookTrig.ID+"/dry-run", map[string]any{}),
		"id", apID, "triggerId", webhookTrig.ID,
	)
	testutil.Call(t, testHandler.DryRunAutopilotWebhookTrigger, req).Want(http.StatusBadRequest)

	// A scalar body is not a webhook payload.
	req = testutil.WithURLParams(
		newRequest("POST", "/api/autopilots/"+apID+"/triggers/"+webhookTrig.ID+"/dry-run", map[string]any{"payload": "hello"}),
		"id", apID, "triggerId", webhookTrig.ID,
	)
	testutil.Call(t, testHandler.DryRunAutopilotWebhookTrigger, req).Want(http.StatusBadRequest)

	// Schedule trigger through the webhook verb.
	sched := testutilCallCreateTrigger(t, apID, map[string]any{
		"kind": "schedule", "cron_expression": "0 8 * * *", "timezone": "UTC",
	}, http.StatusCreated)
	schedID, _ := sched["id"].(string)
	req = testutil.WithURLParams(
		newRequest("POST", "/api/autopilots/"+apID+"/triggers/"+schedID+"/dry-run", map[string]any{"payload": map[string]any{"a": 1}}),
		"id", apID, "triggerId", schedID,
	)
	testutil.Call(t, testHandler.DryRunAutopilotWebhookTrigger, req).Want(http.StatusBadRequest)

	// A webhook trigger through the schedule verb.
	req = testutil.WithURLParams(
		newRequest("GET", "/api/autopilots/"+apID+"/triggers/"+webhookTrig.ID+"/dry-run", nil),
		"id", apID, "triggerId", webhookTrig.ID,
	)
	testutil.Call(t, testHandler.DryRunAutopilotScheduleTrigger, req).Want(http.StatusBadRequest)
}

// The schedule preview answers with the instants the scheduler would pick,
// band included, plus whatever would suppress the dispatch.
func TestDryRunScheduleTrigger(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DryRun Schedule Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	created := testutilCallCreateTrigger(t, apID, map[string]any{
		"kind": "schedule", "cron_expression": "0 8 * * *", "timezone": "UTC", "window_minutes": 120,
	}, http.StatusCreated)
	trigID, _ := created["id"].(string)

	get := func() DryRunScheduleTriggerResponse {
		t.Helper()
		req := testutil.WithURLParams(
			newRequest("GET", "/api/autopilots/"+apID+"/triggers/"+trigID+"/dry-run", nil),
			"id", apID, "triggerId", trigID,
		)
		return testutil.Decode[DryRunScheduleTriggerResponse](t, testHandler.DryRunAutopilotScheduleTrigger, req, http.StatusOK)
	}

	resp := get()
	if len(resp.NextRuns) != scheduleDryRunOccurrences {
		t.Fatalf("next_runs = %v, want %d occurrences", resp.NextRuns, scheduleDryRunOccurrences)
	}
	if resp.WindowMinutes != 120 {
		t.Fatalf("window_minutes = %d, want the trigger's own band", resp.WindowMinutes)
	}
	if !resp.WouldRun || resp.ReasonCode != nil {
		t.Fatalf("would_run=%v reason=%v, want an unblocked schedule", resp.WouldRun, resp.ReasonCode)
	}
	var prev time.Time
	for _, raw := range resp.NextRuns {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatalf("next_runs entry %q: %v", raw, err)
		}
		// 08:00 UTC + [0, 120) minutes.
		if h := at.UTC().Hour(); h < 8 || h >= 10 {
			t.Fatalf("occurrence %s is outside the 08:00–10:00 UTC band", raw)
		}
		if !prev.IsZero() && !at.After(prev) {
			t.Fatalf("occurrences are not ascending: %s after %s", raw, prev.Format(time.RFC3339))
		}
		prev = at
	}

	// A disabled trigger still previews its instants — the operator needs to
	// see the schedule they are about to re-enable — but says it would not run.
	req := testutil.WithURLParams(
		newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trigID, map[string]any{"enabled": false}),
		"id", apID, "triggerId", trigID,
	)
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusOK)
	resp = get()
	if resp.WouldRun || resp.ReasonCode == nil || *resp.ReasonCode != reasonTriggerDisabled {
		t.Fatalf("would_run=%v reason=%v, want trigger_disabled", resp.WouldRun, resp.ReasonCode)
	}
	if len(resp.NextRuns) == 0 {
		t.Fatal("a disabled schedule must still preview its instants")
	}
}

// evaluateWebhookDelivery is the one decision both ingress and dry-run run.
// Its matrix lives here, beside the function — the HTTP suites above keep only
// the wiring and the endpoint-specific rejections.
func TestEvaluateWebhookDelivery(t *testing.T) {
	env := WebhookEnvelope{Event: "github.pull_request.opened", EventPayload: []byte(`{"action":"opened"}`)}
	deny := func(string, string, WebhookEnvelope) webhookRoutingVerdict {
		return webhookRoutingVerdict{relevant: false, reason: "nope"}
	}
	allow := func(string, string, WebhookEnvelope) webhookRoutingVerdict {
		return webhookRoutingVerdict{relevant: true, reason: "yep"}
	}
	base := webhookDeliveryState{TriggerEnabled: true, AutopilotStatus: "active", Envelope: env}

	cases := []struct {
		name    string
		mutate  func(*webhookDeliveryState)
		clsfy   classifyWebhookEvent
		wantRun bool
		wantWhy string
	}{
		{"clean", nil, nil, true, ""},
		{"disabled", func(s *webhookDeliveryState) { s.TriggerEnabled = false }, nil, false, reasonTriggerDisabled},
		{"archived", func(s *webhookDeliveryState) { s.AutopilotStatus = "archived" }, nil, false, reasonAutopilotArchived},
		{"paused", func(s *webhookDeliveryState) { s.AutopilotStatus = "paused" }, nil, false, reasonAutopilotPaused},
		{"filtered", func(s *webhookDeliveryState) {
			s.EventFilters = []byte(`[{"event":"issues"}]`)
		}, nil, false, reasonEventFiltered},
		{"criteria denies", func(s *webhookDeliveryState) { s.EventMatchCriteria = "security only" }, deny, false, reasonCriteriaNotMatched},
		{"criteria allows", func(s *webhookDeliveryState) { s.EventMatchCriteria = "security only" }, allow, true, ""},
		// No classifier wired (no LLM configured) must fail open.
		{"criteria without classifier", func(s *webhookDeliveryState) { s.EventMatchCriteria = "security only" }, nil, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := base
			if tc.mutate != nil {
				tc.mutate(&state)
			}
			got := evaluateWebhookDelivery(state, tc.clsfy)
			if got.Run != tc.wantRun || got.ReasonCode != tc.wantWhy {
				t.Fatalf("run=%v reason=%q, want run=%v reason=%q", got.Run, got.ReasonCode, tc.wantRun, tc.wantWhy)
			}
		})
	}
}
