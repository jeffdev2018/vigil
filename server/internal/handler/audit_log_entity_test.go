package handler

import (
	"net/http"
	"testing"
)

// Role views (K32): the CTO view reads the audit entries of one issue.
func TestAuditLogFiltersByEntity(t *testing.T) {
	a := dbfx.Issue(t, "audit entity a")
	b := dbfx.Issue(t, "audit entity b")
	for _, id := range []string{a, b} {
		testHandler.audit(t.Context(), parseUUID(testWorkspaceID), "member", testUserID, AuditIssueStatus, "issue", parseUUID(id), map[string]any{"to": "done"}, nil)
	}
	var out struct {
		Entries []AuditLogEntryResponse `json:"entries"`
	}
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?entity_id="+a).Want(http.StatusOK).JSON(&out)
	if len(out.Entries) != 1 || out.Entries[0].EntityID == nil || *out.Entries[0].EntityID != a {
		t.Fatalf("entity filter = %+v, want only issue a", out.Entries)
	}
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?entity_id=nope").Want(http.StatusBadRequest)
}
