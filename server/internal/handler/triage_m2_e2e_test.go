package handler

// M2 end-to-end coverage through the REAL webhook path: gating parks
// deliveries as pending items instead of issues, accept promotes an item
// through the ordinary issue funnel, dismiss records the refusal, and the
// list/batch/source endpoints drive the queue UI.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func setTriageSourceMode(t *testing.T, apID, mode string) {
	t.Helper()
	ctx := context.Background()
	src, err := testHandler.Queries.UpsertTriageSource(ctx, db.UpsertTriageSourceParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Kind:        triage.SourceAutopilotWebhook,
		RefID:       parseUUID(apID),
		Name:        "gated webhook test",
	})
	if err != nil {
		t.Fatalf("upsert triage source: %v", err)
	}
	if _, err := testHandler.Queries.UpdateTriageSourceMode(ctx, db.UpdateTriageSourceModeParams{
		ID: src.ID, WorkspaceID: parseUUID(testWorkspaceID), Mode: mode,
	}); err != nil {
		t.Fatalf("set source mode %s: %v", mode, err)
	}
}

func setIssueTitleTemplate(t *testing.T, apID, tmpl string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot SET issue_title_template = $1 WHERE id = $2`, tmpl, apID); err != nil {
		t.Fatalf("set title template: %v", err)
	}
}

func countAutopilotIssues(t *testing.T, apID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID,
	).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

func cleanupAcceptedIssues(t *testing.T, apID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, apID)
	})
}

func pendingTriageItemIDs(t *testing.T, apID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT id FROM triage_item
		WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2 AND state = 'pending'
		ORDER BY first_seen_at
	`, testWorkspaceID, apID)
	if err != nil {
		t.Fatalf("query pending items: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan item id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func gatedWebhookSetup(t *testing.T, name, mode string) (apID string, token string) {
	t.Helper()
	agentID := createWebhookTestAgent(t, name+" Agent")
	apID = createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	cleanupTriageRowsForAutopilot(t, apID)
	cleanupAcceptedIssues(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)
	setTriageSourceMode(t, apID, mode)
	return apID, *trig.WebhookToken
}

func TestWebhookGatedDeliveryParksItemInsteadOfIssue(t *testing.T) {
	apID, token := gatedWebhookSetup(t, "TriageGate", "gate")

	w := postWebhook(t, token, map[string]any{"event": "triage.gated"}, nil)
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))

	if got := countAutopilotIssues(t, apID); got != 0 {
		t.Fatalf("issues created by a gated delivery = %d, want 0", got)
	}

	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), delivery.AutopilotRunID)
	if err != nil {
		t.Fatalf("load gated run: %v", err)
	}
	if run.Status != "skipped" {
		t.Fatalf("gated run status = %q, want skipped (terminal, recovery-safe)", run.Status)
	}
	if !run.ReasonCode.Valid || run.ReasonCode.String != "triage_gated" {
		t.Fatalf("gated run reason = %+v, want triage_gated", run.ReasonCode)
	}

	var n int
	var title string
	var shadow bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*), min(title), bool_or(shadow) FROM triage_item
		WHERE workspace_id = $1 AND origin_id = $2 AND state = 'pending'
	`, testWorkspaceID, apID).Scan(&n, &title, &shadow); err != nil {
		t.Fatalf("count gated items: %v", err)
	}
	if n != 1 {
		t.Fatalf("pending items = %d, want 1", n)
	}
	if shadow {
		t.Fatal("gated item must be real (shadow=false), not measurement")
	}
	if title != "Webhook test active" {
		t.Fatalf("gated item title = %q, want the interpolated autopilot title", title)
	}
}

func TestWebhookBlockedSourceRecordsRealDrop(t *testing.T) {
	apID, token := gatedWebhookSetup(t, "TriageBlock", "blocked")

	w := postWebhook(t, token, map[string]any{"event": "triage.blocked"}, nil)
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))

	if got := countAutopilotIssues(t, apID); got != 0 {
		t.Fatalf("issues created by a blocked delivery = %d, want 0", got)
	}
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), delivery.AutopilotRunID)
	if err != nil {
		t.Fatalf("load blocked run: %v", err)
	}
	if run.Status != "skipped" || !run.ReasonCode.Valid || run.ReasonCode.String != "triage_blocked" {
		t.Fatalf("blocked run = status %q reason %+v, want skipped/triage_blocked", run.Status, run.ReasonCode)
	}

	var n int
	var dropReason string
	var shadow bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*), min(drop_reason), bool_or(shadow) FROM triage_item
		WHERE workspace_id = $1 AND origin_id = $2 AND state = 'dropped'
	`, testWorkspaceID, apID).Scan(&n, &dropReason, &shadow); err != nil {
		t.Fatalf("count blocked drops: %v", err)
	}
	if n != 1 || dropReason != "triage_blocked" {
		t.Fatalf("drops = %d reason %q, want 1 triage_blocked", n, dropReason)
	}
	if shadow {
		t.Fatal("blocked-source drops are real queue data, not shadow measurement")
	}
}

func callAcceptTriageItem(t *testing.T, itemID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.AcceptTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/accept", nil),
		"id", itemID,
	))
}

func TestTriageAcceptCreatesIssueFromItem(t *testing.T) {
	apID, token := gatedWebhookSetup(t, "TriageAccept", "gate")

	w := postWebhook(t, token, map[string]any{"event": "triage.accept"}, nil)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	ids := pendingTriageItemIDs(t, apID)
	if len(ids) != 1 {
		t.Fatalf("pending items = %d, want 1", len(ids))
	}

	var out struct {
		ItemID string `json:"item_id"`
		State  string `json:"state"`
		Issue  struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"issue"`
	}
	callAcceptTriageItem(t, ids[0]).Want(http.StatusOK).JSON(&out)
	if out.State != "accepted" || out.Issue.ID == "" {
		t.Fatalf("accept response = %+v, want accepted with an issue", out)
	}

	// The issue carries the item's origin and the human acceptor as creator.
	var creatorType, creatorID, assigneeType, assigneeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT creator_type, creator_id, COALESCE(assignee_type,''), COALESCE(assignee_id::text,'')
		FROM issue WHERE id = $1
	`, out.Issue.ID).Scan(&creatorType, &creatorID, &assigneeType, &assigneeID); err != nil {
		t.Fatalf("load accepted issue: %v", err)
	}
	if creatorType != "member" || creatorID != testUserID {
		t.Fatalf("creator = %s/%s, want the member who accepted (%s)", creatorType, creatorID, testUserID)
	}
	if assigneeType != "agent" || assigneeID == "" {
		t.Fatalf("assignee = %s/%s, want the autopilot's agent inherited on accept", assigneeType, assigneeID)
	}

	// Item flipped to accepted with the issue linked.
	var state, issueID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state, issue_id FROM triage_item WHERE id = $1`, ids[0],
	).Scan(&state, &issueID); err != nil {
		t.Fatalf("load accepted item: %v", err)
	}
	if state != "accepted" || issueID != out.Issue.ID {
		t.Fatalf("item = %s → %s, want accepted → %s", state, issueID, out.Issue.ID)
	}

	// Second accept is a 409, never a second issue.
	callAcceptTriageItem(t, ids[0]).Want(http.StatusConflict)
	if got := countAutopilotIssues(t, apID); got != 1 {
		t.Fatalf("issues after double accept = %d, want 1", got)
	}
}

func TestTriageDismissMarksItem(t *testing.T) {
	apID, token := gatedWebhookSetup(t, "TriageDismiss", "gate")

	w := postWebhook(t, token, map[string]any{"event": "triage.dismiss"}, nil)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	ids := pendingTriageItemIDs(t, apID)
	if len(ids) != 1 {
		t.Fatalf("pending items = %d, want 1", len(ids))
	}

	testutil.Call(t, testHandler.DismissTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+ids[0]+"/dismiss", map[string]any{"reason": "noise"}),
		"id", ids[0],
	)).Want(http.StatusOK)

	var state, reason string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state, COALESCE(resolution_reason,'') FROM triage_item WHERE id = $1`, ids[0],
	).Scan(&state, &reason); err != nil {
		t.Fatalf("load dismissed item: %v", err)
	}
	if state != "dismissed" || reason != "noise" {
		t.Fatalf("item = %s (%s), want dismissed (noise)", state, reason)
	}
	if got := countAutopilotIssues(t, apID); got != 0 {
		t.Fatalf("dismiss created %d issues, want 0", got)
	}

	// Already resolved → 409.
	testutil.Call(t, testHandler.DismissTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+ids[0]+"/dismiss", nil),
		"id", ids[0],
	)).Want(http.StatusConflict)
}

func TestTriageListAndBatchAccept(t *testing.T) {
	apID, token := gatedWebhookSetup(t, "TriageBatch", "gate")

	// Two deliveries with distinct titles → two pending items.
	w1 := postWebhook(t, token, map[string]any{"event": "triage.batch.one"}, nil)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w1))
	setIssueTitleTemplate(t, apID, "Second gated delivery")
	w2 := postWebhook(t, token, map[string]any{"event": "triage.batch.two"}, nil)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w2))
	ids := pendingTriageItemIDs(t, apID)
	if len(ids) != 2 {
		t.Fatalf("pending items = %d, want 2", len(ids))
	}

	// Shadow measurement rows never leak into the visible list: plant one on
	// the same source and verify it stays invisible.
	if _, err := triage.Capture(context.Background(), testHandler.Queries, triage.CaptureParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SourceKind:  triage.SourceAutopilotWebhook,
		SourceRefID: parseUUID(apID),
		SourceName:  "TriageBatch",
		OriginType:  "autopilot",
		OriginID:    parseUUID(apID),
		Title:       "shadow noise",
		State:       triage.StatePending,
		Shadow:      true,
	}); err != nil {
		t.Fatalf("plant shadow item: %v", err)
	}

	pageOne := struct {
		Items      []TriageItemResponse `json:"items"`
		NextCursor string               `json:"next_cursor"`
	}{}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?state=pending&limit=1", nil),
	).Want(http.StatusOK).JSON(&pageOne)
	if len(pageOne.Items) != 1 || pageOne.NextCursor == "" {
		t.Fatalf("page one = %d items cursor %q, want 1 item + cursor", len(pageOne.Items), pageOne.NextCursor)
	}
	if pageOne.Items[0].Title == "shadow noise" {
		t.Fatal("shadow rows must never be listed")
	}

	pageTwo := struct {
		Items      []TriageItemResponse `json:"items"`
		NextCursor string               `json:"next_cursor"`
	}{}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?state=pending&limit=1&cursor="+pageOne.NextCursor, nil),
	).Want(http.StatusOK).JSON(&pageTwo)
	if len(pageTwo.Items) != 1 {
		t.Fatalf("page two = %d items, want 1", len(pageTwo.Items))
	}
	if pageTwo.Items[0].ID == pageOne.Items[0].ID {
		t.Fatal("cursor returned the same item twice")
	}

	// Batch accept promotes both.
	batchOut := struct {
		Items []BatchAcceptTriageItem `json:"items"`
	}{}
	testutil.Call(t, testHandler.BatchAcceptTriageItems,
		newRequest(http.MethodPost, "/api/triage/items/batch-accept", map[string]any{"ids": ids}),
	).Want(http.StatusOK).JSON(&batchOut)
	if len(batchOut.Items) != 2 {
		t.Fatalf("batch results = %d, want 2", len(batchOut.Items))
	}
	for _, item := range batchOut.Items {
		if item.Outcome != "accepted" || item.IssueID == "" {
			t.Fatalf("batch item %+v, want accepted with an issue", item)
		}
	}
	if got := countAutopilotIssues(t, apID); got != 2 {
		t.Fatalf("issues after batch accept = %d, want 2", got)
	}

	// Empty batch is a 400.
	testutil.Call(t, testHandler.BatchAcceptTriageItems,
		newRequest(http.MethodPost, "/api/triage/items/batch-accept", map[string]any{"ids": []string{}}),
	).Want(http.StatusBadRequest)
}

func TestUpdateTriageSourceMode(t *testing.T) {
	apID, _ := gatedWebhookSetup(t, "TriageMode", "direct")

	var sourceID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM triage_source WHERE workspace_id = $1 AND kind = 'autopilot_webhook' AND ref_id = $2`,
		testWorkspaceID, apID,
	).Scan(&sourceID); err != nil {
		t.Fatalf("load source: %v", err)
	}

	var out TriageSourceStats
	testutil.Call(t, testHandler.UpdateTriageSource, testutil.WithURLParams(
		newRequest(http.MethodPatch, "/api/triage/sources/"+sourceID, map[string]any{"mode": "gate"}),
		"id", sourceID,
	)).Want(http.StatusOK).JSON(&out)
	if out.Mode != "gate" {
		t.Fatalf("mode = %q, want gate", out.Mode)
	}

	testutil.Call(t, testHandler.UpdateTriageSource, testutil.WithURLParams(
		newRequest(http.MethodPatch, "/api/triage/sources/"+sourceID, map[string]any{"mode": "bogus"}),
		"id", sourceID,
	)).Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.UpdateTriageSource, testutil.WithURLParams(
		newRequest(http.MethodPatch, "/api/triage/sources/"+fmt.Sprintf("00000000-0000-0000-0000-%012d", 1), map[string]any{"mode": "gate"}),
		"id", fmt.Sprintf("00000000-0000-0000-0000-%012d", 1),
	)).Want(http.StatusNotFound)
}
