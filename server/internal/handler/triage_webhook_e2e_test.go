package handler

// End-to-end coverage of the M1 shadow capture through the REAL webhook
// path: HTTP delivery → queued delivery worker → dispatchCreateIssue /
// handleDispatchSkip. The capture functions themselves are covered in
// triage_test.go; here the contract is that a webhook-originated
// create_issue dispatch leaves a shadow item (or a drop record) behind
// without changing any routing behavior.

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/triage"
)

// cleanupTriageRowsForAutopilot removes every triage row the dispatch under
// test may have written, regardless of where the test fails — the rows are
// created by the production path, not by the test, so cleanup must be
// registered before any assertion runs.
func cleanupTriageRowsForAutopilot(t *testing.T, apID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM triage_item WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2`,
			testWorkspaceID, apID)
		testPool.Exec(context.Background(),
			`DELETE FROM triage_source WHERE workspace_id = $1 AND kind = 'autopilot_webhook' AND ref_id = $2`,
			testWorkspaceID, apID)
	})
}

func TestWebhookCreateIssueDispatchCapturesShadowTriageItem(t *testing.T) {
	agentID := createWebhookTestAgent(t, "TriageE2E Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"event":        "triage.e2e",
		"eventPayload": map[string]any{"alert": "payment-gateway"},
	}, nil)
	deliveryID := requireAcceptedWebhookResponse(t, w)
	processQueuedWebhookDelivery(t, deliveryID)

	// Routing is unchanged: the issue is still created exactly as before.
	var issueID, issueTitle string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id, title FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID,
	).Scan(&issueID, &issueTitle); err != nil {
		t.Fatalf("issue was not created by the webhook dispatch: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	rows, err := testPool.Query(context.Background(), `
		SELECT i.id, i.source_id, i.state, i.shadow, i.title, i.normalized_title,
		       i.collapse_count, i.payload::text, s.kind, s.ref_id::text, s.mode
		FROM triage_item i
		JOIN triage_source s ON s.id = i.source_id
		WHERE i.workspace_id = $1 AND i.origin_type = 'autopilot' AND i.origin_id = $2
	`, testWorkspaceID, apID)
	if err != nil {
		t.Fatalf("query triage items: %v", err)
	}
	defer rows.Close()

	type captured struct {
		itemID, sourceID, state, title, normalized, payload, kind, refID, mode string
		shadow                                                                 bool
		collapseCount                                                          int32
	}
	var items []captured
	for rows.Next() {
		var c captured
		if err := rows.Scan(&c.itemID, &c.sourceID, &c.state, &c.shadow, &c.title,
			&c.normalized, &c.collapseCount, &c.payload, &c.kind, &c.refID, &c.mode); err != nil {
			t.Fatalf("scan triage item: %v", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate triage items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("captured %d triage items, want exactly 1", len(items))
	}
	got := items[0]

	if got.state != triage.StatePending || !got.shadow {
		t.Fatalf("item state=%q shadow=%v, want pending shadow item", got.state, got.shadow)
	}
	if got.title != issueTitle {
		t.Fatalf("captured title %q, want the created issue's title %q", got.title, issueTitle)
	}
	if got.normalized != triage.NormalizeTitle(issueTitle) {
		t.Fatalf("normalized_title %q, want %q", got.normalized, triage.NormalizeTitle(issueTitle))
	}
	if got.collapseCount != 1 {
		t.Fatalf("collapse_count = %d, want 1 for a single delivery", got.collapseCount)
	}
	if !strings.Contains(got.payload, "payment-gateway") {
		t.Fatalf("stored payload lost the webhook envelope: %s", got.payload)
	}
	if got.kind != triage.SourceAutopilotWebhook || got.refID != apID || got.mode != "direct" {
		t.Fatalf("source kind=%q ref=%q mode=%q, want autopilot_webhook/%s/direct", got.kind, got.refID, got.mode, apID)
	}
}

func TestWebhookCreateIssueDuplicateSkipRecordsTriageDrop(t *testing.T) {
	agentID := createWebhookTestAgent(t, "TriageDropE2E Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)

	// First delivery creates the issue.
	w1 := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "triage.drop"}, nil)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w1))

	var issueCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID,
	).Scan(&issueCount); err != nil || issueCount != 1 {
		t.Fatalf("issues after first delivery = %d (err %v), want 1", issueCount, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID)
	})

	// Second delivery, same token and payload: same interpolated title, so the
	// recent-duplicate guard skips the dispatch after admission. Today the
	// payload would be silently lost; the shadow capture must record the drop.
	w2 := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "triage.drop"}, nil)
	delivery2 := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w2))

	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), delivery2.AutopilotRunID)
	if err != nil {
		t.Fatalf("load second run: %v", err)
	}
	if run.Status != "skipped" {
		t.Fatalf("second run status = %q, want skipped by the duplicate guard", run.Status)
	}

	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID,
	).Scan(&issueCount); err != nil || issueCount != 1 {
		t.Fatalf("issues after second delivery = %d (err %v), want still 1", issueCount, err)
	}

	var dropped, pending int
	var dropReason string
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			COUNT(*) FILTER (WHERE state = 'dropped'),
			COUNT(*) FILTER (WHERE state = 'pending')
		FROM triage_item
		WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2
	`, testWorkspaceID, apID).Scan(&dropped, &pending); err != nil {
		t.Fatalf("count triage items: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending shadow items = %d, want 1 from the first delivery", pending)
	}
	if dropped != 1 {
		t.Fatalf("dropped items = %d, want 1 for the duplicate-skipped delivery", dropped)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT drop_reason FROM triage_item
		WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2 AND state = 'dropped'
	`, testWorkspaceID, apID).Scan(&dropReason); err != nil {
		t.Fatalf("load drop reason: %v", err)
	}
	if dropReason != "already_active" {
		t.Fatalf("drop_reason = %q, want already_active from the duplicate guard", dropReason)
	}
}
