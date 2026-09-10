package handler

// Workspace doctrine (OS plan, chantier 22): the revision ledger, the
// second-reviewer flow, restore, the line diff, the legacy context door, and
// the reports agents file when a task collides with a rule.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func doctrineCleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var previous *string
	var settings []byte
	dbfx.QueryRow(t, `SELECT context, settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous, &settings)
	reset := func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace_doctrine_report WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace_doctrine_version WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type IN ('doctrine_review', 'doctrine_report')`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `UPDATE workspace SET context = $2, settings = $3, doctrine_revision = 0, doctrine_updated_at = NULL, doctrine_updated_by = NULL WHERE id = $1`, testWorkspaceID, previous, settings)
	}
	reset()
	t.Cleanup(reset)
}

type doctrineEnvelope struct {
	Doctrine DoctrineResponse        `json:"doctrine"`
	Version  DoctrineVersionResponse `json:"version"`
}

func publishDoctrineAs(t *testing.T, userID string, body map[string]any) (doctrineEnvelope, *testutil.Response) {
	t.Helper()
	var out doctrineEnvelope
	resp := testutil.Call(t, testHandler.UpdateWorkspaceDoctrine, newRequestAs(userID, http.MethodPut, "/api/workspace/doctrine", body))
	if resp.Code == http.StatusOK || resp.Code == http.StatusAccepted {
		resp.JSON(&out)
	}
	return out, resp
}

func getDoctrine(t *testing.T) DoctrineResponse {
	t.Helper()
	var out DoctrineResponse
	testutil.Call(t, testHandler.GetWorkspaceDoctrine, newRequest(http.MethodGet, "/api/workspace/doctrine", nil)).Want(http.StatusOK).JSON(&out)
	return out
}

// doctrineAdmin adds a second owner/admin so a review needs another person.
func doctrineAdmin(t *testing.T) string {
	t.Helper()
	admin := dbfx.Insert(t, "user", testutil.Cols{"email": "doc-" + uuid.NewString()[:8] + "@example.com", "name": "Doctrine Admin"})
	dbfx.InsertNoID(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": admin, "role": "admin"}, "workspace_id = $1 AND user_id = $2", testWorkspaceID, admin)
	return admin
}

func TestDoctrinePublishesVersionsRestoresAndDiffs(t *testing.T) {
	doctrineCleanup(t)
	if d := getDoctrine(t); d.Revision != 0 || d.Content != "" || !d.CanPublish || d.ByteLimit != doctrineMaxBytes {
		t.Fatalf("empty doctrine = %+v", d)
	}
	// Revision 1.
	env, resp := publishDoctrineAs(t, testUserID, map[string]any{"content": "Always answer in French.\nNever push to main.\r\n", "expected_revision": 0, "note": "first"})
	resp.Want(http.StatusOK)
	if env.Doctrine.Revision != 1 || env.Doctrine.Content != "Always answer in French.\nNever push to main." || env.Version.Status != DoctrineStatusActive || env.Version.Revision == nil || *env.Version.Revision != 1 || env.Version.Note != "first" {
		t.Fatalf("revision 1 = %+v / %+v", env.Doctrine, env.Version)
	}
	var live string
	dbfx.QueryRow(t, `SELECT context FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&live)
	if live != "Always answer in French.\nNever push to main." {
		t.Fatalf("workspace.context = %q", live)
	}
	// Stale revision, unchanged text, oversize text.
	_, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": "x", "expected_revision": 0})
	resp.Want(http.StatusConflict)
	_, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": "Always answer in French.\nNever push to main.\n", "expected_revision": 1})
	resp.Want(http.StatusBadRequest)
	_, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": strings.Repeat("a", doctrineMaxBytes+1), "expected_revision": 1})
	resp.Want(http.StatusBadRequest)
	// A plain member cannot publish; a run token cannot either.
	memberID := calendarGuest(t)
	_, resp = publishDoctrineAs(t, memberID, map[string]any{"content": "mine", "expected_revision": 1})
	resp.Want(http.StatusForbidden)
	if d := getDoctrine(t); d.CanPublish != true {
		t.Fatalf("owner can_publish = %v", d.CanPublish)
	}
	// Revision 2 replaces a line.
	env, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": "Always answer in French.\nNever push to main.\nAsk before spending.", "expected_revision": 1})
	resp.Want(http.StatusOK)
	rev2 := env.Version.ID
	// History lists both, newest first; the diff of revision 2 against its predecessor is one added line.
	var history struct {
		Versions   []DoctrineVersionResponse `json:"versions"`
		NextCursor *string                   `json:"next_cursor"`
	}
	testutil.Call(t, testHandler.ListDoctrineVersions, newRequest(http.MethodGet, "/api/workspace/doctrine/versions?limit=1", nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 1 || history.Versions[0].ID != rev2 || history.NextCursor == nil {
		t.Fatalf("history page 1 = %+v", history)
	}
	testutil.Call(t, testHandler.ListDoctrineVersions, newRequest(http.MethodGet, "/api/workspace/doctrine/versions?limit=1&cursor="+*history.NextCursor, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 1 || history.Versions[0].Status != DoctrineStatusSuperseded || history.NextCursor != nil {
		t.Fatalf("history page 2 = %+v", history)
	}
	var diff struct {
		From    *DoctrineVersionResponse `json:"from"`
		To      DoctrineVersionResponse  `json:"to"`
		Lines   []DoctrineDiffLine       `json:"lines"`
		Added   int                      `json:"added"`
		Removed int                      `json:"removed"`
	}
	testutil.Call(t, testHandler.DiffDoctrineVersion, withURLParam(newRequest(http.MethodGet, "/api/workspace/doctrine/versions/"+rev2+"/diff", nil), "id", rev2)).Want(http.StatusOK).JSON(&diff)
	if diff.From == nil || diff.From.Revision == nil || *diff.From.Revision != 1 || diff.Added != 1 || diff.Removed != 0 || len(diff.Lines) != 3 || diff.Lines[2].Kind != "add" {
		t.Fatalf("diff = %+v", diff)
	}
	// Restoring revision 1 publishes revision 3 with its text.
	rev1 := diff.From.ID
	env, resp = publishRestore(t, testUserID, rev1, map[string]any{"expected_revision": 2, "note": "back"})
	resp.Want(http.StatusOK)
	if env.Doctrine.Revision != 3 || env.Doctrine.Content != "Always answer in French.\nNever push to main." || env.Version.RestoredFromRevision == nil || *env.Version.RestoredFromRevision != 1 {
		t.Fatalf("restore = %+v / %+v", env.Doctrine, env.Version)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditDoctrinePublished); n != 3 {
		t.Fatalf("published audits = %d, want 3", n)
	}
}

func publishRestore(t *testing.T, userID, versionID string, body map[string]any) (doctrineEnvelope, *testutil.Response) {
	t.Helper()
	var out doctrineEnvelope
	resp := testutil.Call(t, testHandler.RestoreDoctrineVersion, withURLParam(newRequestAs(userID, http.MethodPost, "/api/workspace/doctrine/versions/"+versionID+"/restore", body), "id", versionID))
	if resp.Code == http.StatusOK || resp.Code == http.StatusAccepted {
		resp.JSON(&out)
	}
	return out, resp
}

func reviewDoctrine(t *testing.T, userID string, approve bool, versionID string, body map[string]any) (doctrineEnvelope, *testutil.Response) {
	t.Helper()
	h, verb := testHandler.RejectDoctrineVersion, "reject"
	if approve {
		h, verb = testHandler.ApproveDoctrineVersion, "approve"
	}
	var out doctrineEnvelope
	resp := testutil.Call(t, h, withURLParam(newRequestAs(userID, http.MethodPost, "/api/workspace/doctrine/versions/"+versionID+"/"+verb, body), "id", versionID))
	if resp.Code == http.StatusOK {
		resp.JSON(&out)
	}
	return out, resp
}

func TestDoctrineReviewNeedsASecondManager(t *testing.T) {
	doctrineCleanup(t)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"doctrine": {"require_review": true}}'::jsonb WHERE id = $1`, testWorkspaceID)
	// Alone, the owner self-approves: review on, nobody else to ask.
	env, resp := publishDoctrineAs(t, testUserID, map[string]any{"content": "Rule one.", "expected_revision": 0})
	resp.Want(http.StatusOK)
	if env.Doctrine.Revision != 1 || !env.Doctrine.RequireReview {
		t.Fatalf("solo publish = %+v", env.Doctrine)
	}
	// With a second admin the publication is held.
	admin := doctrineAdmin(t)
	env, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": "Rule one.\nRule two.", "expected_revision": 1, "note": "adds two"})
	resp.Want(http.StatusAccepted)
	pending := env.Version
	if pending.Status != DoctrineStatusPending || pending.Revision != nil || env.Doctrine.Revision != 1 || env.Doctrine.Pending == nil || env.Doctrine.Pending.ID != pending.ID {
		t.Fatalf("held publish = %+v / %+v", env.Doctrine, env.Version)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'outcome' = 'proposed'`, InboxTypeDoctrineReview, admin); n != 1 {
		t.Fatalf("review requests for the admin = %d, want 1", n)
	}
	// A second proposal waits its turn; the author cannot review their own.
	_, resp = publishDoctrineAs(t, testUserID, map[string]any{"content": "Rule three.", "expected_revision": 1})
	resp.Want(http.StatusConflict)
	_, resp = reviewDoctrine(t, testUserID, true, pending.ID, nil)
	resp.Want(http.StatusForbidden)
	// The admin rejects; the author hears about it; the text did not move.
	env, resp = reviewDoctrine(t, admin, false, pending.ID, map[string]any{"note": "too vague"})
	resp.Want(http.StatusOK)
	if env.Version.Status != DoctrineStatusRejected || env.Version.ReviewNote != "too vague" || env.Doctrine.Revision != 1 || env.Doctrine.Pending != nil {
		t.Fatalf("rejected = %+v / %+v", env.Doctrine, env.Version)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'outcome' = 'rejected'`, InboxTypeDoctrineReview, testUserID); n != 1 {
		t.Fatalf("rejection notices for the author = %d, want 1", n)
	}
	_, resp = reviewDoctrine(t, admin, true, pending.ID, nil)
	resp.Want(http.StatusConflict)
	// Proposed again and approved: revision 2 goes live with the reviewer on it.
	env, _ = publishDoctrineAs(t, testUserID, map[string]any{"content": "Rule one.\nRule two.", "expected_revision": 1})
	env, resp = reviewDoctrine(t, admin, true, env.Version.ID, map[string]any{"note": "fine"})
	resp.Want(http.StatusOK)
	if env.Doctrine.Revision != 2 || env.Version.Status != DoctrineStatusActive || env.Version.ReviewedBy == nil || *env.Version.ReviewedBy != admin || env.Doctrine.Content != "Rule one.\nRule two." {
		t.Fatalf("approved = %+v / %+v", env.Doctrine, env.Version)
	}
	var superseded int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM workspace_doctrine_version WHERE workspace_id = $1 AND status = 'superseded'`, testWorkspaceID).Scan(&superseded)
	if superseded != 1 {
		t.Fatalf("superseded versions = %d, want 1", superseded)
	}
}

func TestDoctrineLegacyContextDoorVersionsAndRefusesMachines(t *testing.T) {
	doctrineCleanup(t)
	req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{"context": "From the old field."}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK)
	if d := getDoctrine(t); d.Revision != 1 || d.Content != "From the old field." {
		t.Fatalf("after legacy update = %+v", d)
	}
	// Re-sending the same text is not an error on this door.
	req = withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{"context": "From the old field.", "name": "Handler Test Workspace"}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK)
	if d := getDoctrine(t); d.Revision != 1 {
		t.Fatalf("unchanged legacy update bumped the revision: %+v", d)
	}
	// A run token cannot rewrite the doctrine through the old field.
	_, task, agent := runningAgentRun(t, "doctrine door")
	req = withURLParam(testutil.WithHeaders(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID, map[string]any{"context": "agent wrote this"}), gateHeaders(task, agent)...), "id", testWorkspaceID)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusForbidden)
}

func TestDoctrineReportsReachTheManagersAndAreResolved(t *testing.T) {
	doctrineCleanup(t)
	publishDoctrineAs(t, testUserID, map[string]any{"content": "Never contact customers directly.", "expected_revision": 0})
	admin := doctrineAdmin(t)
	issue, task, agent := runningAgentRun(t, "doctrine report")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	var out struct {
		Report DoctrineReportResponse `json:"report"`
	}
	req := testutil.WithHeaders(newRequest(http.MethodPost, "/api/workspace/doctrine/reports", map[string]any{"kind": "conflict", "summary": "The issue asks me to email the customer.", "passage": "Never contact customers directly."}), gateHeaders(task, agent)...)
	testutil.Call(t, testHandler.CreateDoctrineReport, req).Want(http.StatusCreated).JSON(&out)
	r := out.Report
	if r.Kind != "conflict" || r.ReporterType != "agent" || r.ReporterID != agent || r.TaskID == nil || *r.TaskID != task || r.IssueID == nil || *r.IssueID != issue || r.DoctrineRevision != 1 || r.Status != DoctrineReportOpen {
		t.Fatalf("report = %+v", r)
	}
	for _, uid := range []string{testUserID, admin} {
		if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2 AND details->>'report_id' = $3`, InboxTypeDoctrineReport, uid, r.ID); n != 1 {
			t.Fatalf("report notices for %s = %d, want 1", uid, n)
		}
	}
	if d := getDoctrine(t); d.OpenReports != 1 {
		t.Fatalf("open_reports = %d", d.OpenReports)
	}
	// Bad kind, empty summary.
	testutil.Call(t, testHandler.CreateDoctrineReport, newRequest(http.MethodPost, "/api/workspace/doctrine/reports", map[string]any{"kind": "rant", "summary": "x"})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.CreateDoctrineReport, newRequest(http.MethodPost, "/api/workspace/doctrine/reports", map[string]any{"kind": "ambiguity", "summary": "  "})).Want(http.StatusBadRequest)
	// A member files one on an issue explicitly.
	testutil.Call(t, testHandler.CreateDoctrineReport, newRequest(http.MethodPost, "/api/workspace/doctrine/reports", map[string]any{"kind": "ambiguity", "summary": "Which customers?", "issue_id": issue})).Want(http.StatusCreated)
	var list struct {
		Reports []DoctrineReportResponse `json:"reports"`
	}
	testutil.Call(t, testHandler.ListDoctrineReports, newRequest(http.MethodGet, "/api/workspace/doctrine/reports", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Reports) != 2 || list.Reports[0].ReporterType != "member" {
		t.Fatalf("open reports = %+v", list.Reports)
	}
	// Acknowledge once; a second time conflicts; a member cannot.
	ack := withURLParam(newRequest(http.MethodPost, "/api/workspace/doctrine/reports/"+r.ID+"/acknowledge", map[string]any{"note": "rule amended"}), "id", r.ID)
	testutil.Call(t, testHandler.AcknowledgeDoctrineReport, ack).Want(http.StatusOK).JSON(&out)
	if out.Report.Status != DoctrineReportAcknowledged || out.Report.ResolutionNote != "rule amended" || out.Report.ResolvedBy == nil {
		t.Fatalf("acknowledged = %+v", out.Report)
	}
	testutil.Call(t, testHandler.AcknowledgeDoctrineReport, withURLParam(newRequest(http.MethodPost, "/api/workspace/doctrine/reports/"+r.ID+"/acknowledge", nil), "id", r.ID)).Want(http.StatusConflict)
	member := calendarGuest(t)
	testutil.Call(t, testHandler.DismissDoctrineReport, withURLParam(newRequestAs(member, http.MethodPost, "/api/workspace/doctrine/reports/"+list.Reports[0].ID+"/dismiss", nil), "id", list.Reports[0].ID)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ListDoctrineReports, newRequest(http.MethodGet, "/api/workspace/doctrine/reports?status=all", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Reports) != 2 {
		t.Fatalf("all reports = %d", len(list.Reports))
	}
	testutil.Call(t, testHandler.ListDoctrineReports, newRequest(http.MethodGet, "/api/workspace/doctrine/reports?status=weird", nil)).Want(http.StatusBadRequest)
}

func TestDiffDoctrineLines(t *testing.T) {
	lines, added, removed := diffDoctrineLines("a\nb\nc", "a\nc\nd")
	if added != 1 || removed != 1 || len(lines) != 4 || lines[1].Kind != "del" || lines[1].Text != "b" || lines[3].Kind != "add" || lines[3].Text != "d" {
		t.Fatalf("diff = %+v (%d/%d)", lines, added, removed)
	}
	if lines, added, removed := diffDoctrineLines("", ""); len(lines) != 0 || added != 0 || removed != 0 {
		t.Fatalf("empty diff = %+v", lines)
	}
}
