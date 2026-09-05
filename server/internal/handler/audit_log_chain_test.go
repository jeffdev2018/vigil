package handler

import (
	"context"
	"net/http"
	"testing"
)

// Audit log hash chain: entries link to their predecessor, verification
// recomputes everything in the database, and a row altered behind the
// triggers' back is reported at its position.
func TestAuditLogHashChainLinksAndDetectsTampering(t *testing.T) {
	ws := parseUUID(testWorkspaceID)
	for i := 0; i < 3; i++ {
		testHandler.audit(t.Context(), ws, "member", testUserID, AuditWorkspaceSettings, "workspace", ws, map[string]any{"i": i}, nil)
	}
	var page struct {
		Entries []AuditLogEntryResponse `json:"entries"`
	}
	auditCall(t, testHandler.ListAuditLog, "/api/audit-log?limit=3").Want(http.StatusOK).JSON(&page)
	if len(page.Entries) != 3 || page.Entries[0].Hash == "" || page.Entries[0].ChainSeq <= page.Entries[1].ChainSeq {
		t.Fatalf("entries = %+v, want hashed and ordered by chain_seq", page.Entries)
	}
	if page.Entries[0].PrevHash == nil || *page.Entries[0].PrevHash != page.Entries[1].Hash {
		t.Fatalf("prev_hash %v must equal the previous entry's hash %s", page.Entries[0].PrevHash, page.Entries[1].Hash)
	}

	var status AuditChainStatus
	auditCall(t, testHandler.VerifyAuditLog, "/api/audit-log/verify").Want(http.StatusOK).JSON(&status)
	if !status.OK || status.Total < 3 || status.HeadHash != page.Entries[0].Hash {
		t.Fatalf("status = %+v, want an intact chain headed by %s", status, page.Entries[0].Hash)
	}

	// Tamper behind the triggers: the verification names the row.
	victim := page.Entries[1].ID
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := testPool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE audit_log_entry DISABLE TRIGGER trg_audit_log_entry_immutable`)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `ALTER TABLE audit_log_entry ENABLE TRIGGER trg_audit_log_entry_immutable`)
	})
	exec(`UPDATE audit_log_entry SET details = '{"i": 99}'::jsonb WHERE id = $1`, victim)
	auditCall(t, testHandler.VerifyAuditLog, "/api/audit-log/verify").Want(http.StatusOK).JSON(&status)
	if status.OK || status.BrokenID == nil || *status.BrokenID != victim || status.BrokenSeq == nil || *status.BrokenSeq != page.Entries[1].ChainSeq {
		t.Fatalf("status after tampering = %+v, want broken at %s", status, victim)
	}
	// Restore the row so later tests see an intact chain.
	exec(`UPDATE audit_log_entry SET details = '{"i": 1}'::jsonb WHERE id = $1`, victim)
	auditCall(t, testHandler.VerifyAuditLog, "/api/audit-log/verify").Want(http.StatusOK).JSON(&status)
	if !status.OK {
		t.Fatalf("status after restore = %+v, want intact", status)
	}
}
