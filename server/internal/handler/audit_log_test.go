package handler

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Audit log (K08): written where actions happen, read with filters and
// cursors, exported identically, never updated or deleted except with the
// workspace.

func auditCall(t *testing.T, h http.HandlerFunc, path string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, inboxWorkspaceHandler(h), testutil.WithHeaders(newRequest(http.MethodGet, path, nil), "X-Workspace-ID", testWorkspaceID))
}

type auditPage struct {
	Entries    []AuditLogEntryResponse `json:"entries"`
	NextCursor string                  `json:"next_cursor"`
}

func TestAuditLogRecordsActionsFiltersPaginatesAndExports(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `SELECT set_config('multica.audit_purge', 'on', false)`)
		testPool.Exec(t.Context(), `DELETE FROM audit_log_entry WHERE workspace_id = $1`, testWorkspaceID)
	})
	issue := dbfx.Issue(t, "audited issue", testutil.Cols{"status": "todo"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	// Three actions from three features.
	var card decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&card)
	respondDecision(t, issue, card.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK)
	moveIssue(t, issue, "in_progress").Want(http.StatusOK)
	setCriteria(t, issue, []map[string]any{{"text": "Ships"}}).Want(http.StatusOK)
	var crit criteriaEnvelope
	testutil.Call(t, testHandler.ListAcceptanceCriteria, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/acceptance-criteria", nil), "id", issue)).Want(http.StatusOK).JSON(&crit)
	proveCriterion(t, issue, crit.Criteria[0].ID, map[string]any{"proof_type": "human_validation"}).Want(http.StatusOK)

	var page auditPage
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?limit=200").Want(http.StatusOK).JSON(&page)
	actions := map[string]*AuditLogEntryResponse{}
	for i := range page.Entries {
		if page.Entries[i].EntityID != nil && (*page.Entries[i].EntityID == issue || *page.Entries[i].EntityID == card.Decision.ID) {
			actions[page.Entries[i].Action] = &page.Entries[i]
		}
	}
	for _, want := range []string{AuditDecisionAsked, AuditDecisionAnswered, AuditIssueStatus, AuditCriterionProved} {
		if actions[want] == nil {
			t.Fatalf("missing audit action %s in %v", want, actions)
		}
	}
	if a := actions[AuditDecisionAnswered]; a.ApproverType == nil || *a.ApproverType != "member" || a.ApproverID == nil || *a.ApproverID != testUserID {
		t.Fatalf("answered entry approver = %+v", a)
	}
	var status struct{ From, To string }
	_ = json.Unmarshal(actions[AuditIssueStatus].Details, &status)
	if status.From != "todo" || status.To != "in_progress" {
		t.Fatalf("status entry details = %+v", status)
	}

	// Filters, alone and combined.
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?action="+AuditIssueStatus+"&limit=200").Want(http.StatusOK).JSON(&page)
	for _, e := range page.Entries {
		if e.Action != AuditIssueStatus {
			t.Fatalf("action filter leaked %s", e.Action)
		}
	}
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?actor_type=agent&action="+AuditIssueStatus).Want(http.StatusOK).JSON(&page)
	for _, e := range page.Entries {
		if e.ActorType != "agent" || e.Action != AuditIssueStatus {
			t.Fatalf("combined filter leaked %+v", e)
		}
	}
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?actor_type=robot").Want(http.StatusBadRequest)
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?since=yesterday").Want(http.StatusBadRequest)

	// Cursor pagination walks every row once.
	var first auditPage
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?limit=2").Want(http.StatusOK).JSON(&first)
	if len(first.Entries) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	var second auditPage
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?limit=2&cursor="+first.NextCursor).Want(http.StatusOK).JSON(&second)
	if len(second.Entries) == 0 || second.Entries[0].ID == first.Entries[0].ID || second.Entries[0].ID == first.Entries[1].ID {
		t.Fatalf("second page repeats the first: %+v", second.Entries)
	}

	// Exports carry the same rows as the paginated view, in order.
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?action="+AuditIssueStatus+"&limit=200").Want(http.StatusOK).JSON(&page)
	var exported []AuditLogEntryResponse
	auditCall(t, testHandler.ExportAuditLog, "/api/audit-log/export?format=json&action="+AuditIssueStatus).Want(http.StatusOK).JSON(&exported)
	if len(exported) != len(page.Entries) || exported[0].ID != page.Entries[0].ID {
		t.Fatalf("json export = %d rows, view = %d", len(exported), len(page.Entries))
	}
	resp := auditCall(t, testHandler.ExportAuditLog, "/api/audit-log/export?format=csv&action="+AuditIssueStatus).Want(http.StatusOK)
	if !strings.Contains(resp.Header().Get("Content-Disposition"), "audit-log-") {
		t.Fatal("csv export must be a download")
	}
	records, err := csv.NewReader(strings.NewReader(resp.Text())).ReadAll()
	if err != nil || len(records) != len(page.Entries)+1 || records[1][0] != page.Entries[0].ID || records[0][4] != "action" {
		t.Fatalf("csv export = %v (err %v)", records, err)
	}
	auditCall(t, testHandler.ExportAuditLog, "/api/audit-log/export?format=xml").Want(http.StatusBadRequest)

	// Immutable: no update, no delete outside the purge.
	if _, err := testPool.Exec(t.Context(), `UPDATE audit_log_entry SET action = 'tampered' WHERE id = $1`, page.Entries[0].ID); err == nil {
		t.Fatal("an audit entry must not be updatable")
	}
	if _, err := testPool.Exec(t.Context(), `DELETE FROM audit_log_entry WHERE id = $1`, page.Entries[0].ID); err == nil {
		t.Fatal("an audit entry must not be deletable")
	}
	// The workspace purge announces itself on its transaction and works.
	ws := dbfx.Workspace(t, "Audit purge", "audit-purge-"+uuid.NewString())
	dbfx.Insert(t, "audit_log_entry", testutil.Cols{"workspace_id": ws, "actor_type": "system", "action": "x", "entity_type": "workspace"})
	tx, err := testPool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	qtx := testHandler.Queries.WithTx(tx)
	if _, err := qtx.AllowAuditPurge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := qtx.PurgeWorkspaceAuditLog(t.Context(), parseUUID(ws)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1`, ws); n != 0 {
		t.Fatalf("rows after purge = %d", n)
	}
}
