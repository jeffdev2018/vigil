package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Triage rules (K62): a parked delivery meets the active webhook rules; the
// first match dismisses it or accepts it with overrides, records the
// application and audits it. Dry-run scans the recent queue.

func parkDelivery(t *testing.T, title, payload string) string {
	t.Helper()
	item, _, err := triage.Capture(context.Background(), testHandler.Queries, triage.CaptureParams{
		WorkspaceID: parseUUID(testWorkspaceID), SourceKind: triage.SourceAutopilotWebhook, SourceRefID: parseUUID(uuid.NewString()),
		SourceName: "Sentry alerts", SourceCreatedBy: parseUUID(testUserID), OriginType: "autopilot", OriginID: parseUUID(uuid.NewString()),
		Title: title, BodyMarkdown: "body", TriggerPayload: []byte(payload), State: triage.StatePending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return uuidToString(item.ID)
}

func TestTriageRulesDismissAndAcceptParkedDeliveries(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM business_rule_violation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM business_rule WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM triage_item WHERE workspace_id = $1 AND title LIKE 'rule %'`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM triage_source WHERE workspace_id = $1 AND name = 'Sentry alerts'`, testWorkspaceID)
	})
	// A webhook rule needs an action; a non-webhook rule refuses one.
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "x", "attach_point": "webhook_received", "predicate": map[string]any{"all": []map[string]any{{"field": "webhook.title", "op": "contains", "value": "dependabot"}}}}).Want(http.StatusUnprocessableEntity)
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "x", "attach_point": "project_create", "predicate": map[string]any{"all": []map[string]any{{"field": "workspace.project_count", "op": "lte", "value": 99}}}, "action": map[string]any{"kind": "dismiss"}}).Want(http.StatusUnprocessableEntity)

	var dismissRule, acceptRule struct{ Rule BusinessRuleResponse }
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{
		"natural_language": "Ignore dependabot", "attach_point": "webhook_received",
		"predicate": map[string]any{"all": []map[string]any{{"field": "webhook.title", "op": "contains", "value": "dependabot"}}},
		"action":    map[string]any{"kind": "dismiss"},
	}).Want(http.StatusCreated).JSON(&dismissRule)
	if dismissRule.Rule.ActionDescription != "dismiss the delivery" {
		t.Fatalf("rule = %+v", dismissRule.Rule)
	}
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{
		"natural_language": "Sentry critical is P0", "attach_point": "webhook_received",
		"predicate": map[string]any{"all": []map[string]any{{"field": "webhook.source_name", "op": "eq", "value": "Sentry alerts"}, {"field": "webhook.payload", "op": "contains", "value": "critical"}}},
		"action":    map[string]any{"kind": "accept", "priority": "urgent"},
	}).Want(http.StatusCreated).JSON(&acceptRule)

	// Drafts do nothing.
	pending := parkDelivery(t, "rule dependabot bump lodash", `{"level":"info"}`)
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, pending))
	if state := triageState(t, pending); state != "pending" {
		t.Fatalf("draft rule must not act, state = %s", state)
	}

	// Dry-run names the recent items the rule would act on.
	var dry struct {
		Checked    int             `json:"checked"`
		Violations []DryRunSubject `json:"violations"`
	}
	ruleCall(t, testHandler.DryRunBusinessRule, http.MethodPost, "/api/business-rules/"+dismissRule.Rule.ID+"/dry-run", nil, "id", dismissRule.Rule.ID).Want(http.StatusOK).JSON(&dry)
	found := false
	for _, v := range dry.Violations {
		found = found || (v.SubjectID == pending && v.Detail == "would dismiss the delivery")
	}
	if dry.Checked == 0 || !found {
		t.Fatalf("dry-run = %+v", dry)
	}

	// Active: dismiss, then accept with the priority override.
	for _, id := range []string{dismissRule.Rule.ID, acceptRule.Rule.ID} {
		ruleCall(t, testHandler.ActivateBusinessRule, http.MethodPut, "/api/business-rules/"+id+"/activate", nil, "id", id).Want(http.StatusOK)
	}
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, pending))
	if state := triageState(t, pending); state != "dismissed" {
		t.Fatalf("dependabot delivery state = %s, want dismissed", state)
	}
	critical := parkDelivery(t, "rule NullPointer in checkout", `{"level":"critical"}`)
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, critical))
	if state := triageState(t, critical); state != "accepted" {
		t.Fatalf("critical delivery state = %s, want accepted", state)
	}
	var priority string
	dbfx.QueryRow(t, `SELECT i.priority FROM issue i JOIN triage_item t ON t.issue_id = i.id WHERE t.id = $1`, critical).Scan(&priority)
	if priority != "urgent" {
		t.Fatalf("accepted issue priority = %s, want urgent", priority)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM business_rule_violation WHERE workspace_id = $1 AND subject_type = 'triage_item'`, testWorkspaceID); n != 2 {
		t.Fatalf("applications = %d, want 2", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`, AuditBusinessRuleApplied, critical); n != 1 {
		t.Fatalf("audit = %d, want 1", n)
	}
	// Nothing matches: stays for a human.
	quiet := parkDelivery(t, "rule quiet delivery", `{"level":"info"}`)
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, quiet))
	if state := triageState(t, quiet); state != "pending" {
		t.Fatalf("quiet delivery state = %s, want pending", state)
	}
}

func triageState(t *testing.T, id string) string {
	t.Helper()
	var s string
	dbfx.QueryRow(t, `SELECT state FROM triage_item WHERE id = $1`, id).Scan(&s)
	return s
}

func mustTriageItem(t *testing.T, id string) db.TriageItem {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT * FROM triage_item WHERE id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[db.TriageItem])
	if err != nil {
		t.Fatal(err)
	}
	return item
}
