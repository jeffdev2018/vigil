package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Cross-repo mirror issues (K54). The graph rules themselves are covered
// without a database in service/issue_mirror_test.go; this file covers the
// endpoints, the label trigger and the blocking dependency it writes.

func mirrorLabel(t *testing.T, name string) string {
	t.Helper()
	return dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"resource_type": "issue",
		"name":          name,
		"color":         "#111111",
	})
}

func callListMirrorLinks(t *testing.T, projectID string) MirrorLinksResponse {
	t.Helper()
	var out MirrorLinksResponse
	testutil.Call(t, testHandler.ListProjectMirrorLinks, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/projects/"+projectID+"/mirror-links", nil), "id", projectID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

func callCreateMirrorLink(t *testing.T, projectID, targetID, label string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.CreateProjectMirrorLink, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/projects/"+projectID+"/mirror-links", map[string]any{
			"target_project_id": targetID,
			"trigger_label":     label,
		}), "id", projectID,
	))
}

func callAttachLabel(t *testing.T, issueID, labelID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.AttachLabel, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/labels", map[string]any{"label_id": labelID}), "id", issueID,
	))
}

func callIssueMirrors(t *testing.T, issueID string) IssueMirrorsResponse {
	t.Helper()
	var out IssueMirrorsResponse
	testutil.Call(t, testHandler.GetIssueMirrors, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/mirrors", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

// mirrorSetup wires a source project, one target project and a trigger label,
// and returns them plus the created link id.
func mirrorSetup(t *testing.T) (source, target, labelID, labelName, linkID string) {
	t.Helper()
	suffix := uuid.NewString()[:8]
	source = dbfx.Project(t, "mirror source "+suffix)
	target = dbfx.Project(t, "mirror target "+suffix)
	labelName = "mirror-" + suffix
	labelID = mirrorLabel(t, labelName)

	var link MirrorLinkResponse
	callCreateMirrorLink(t, source, target, labelName).Want(http.StatusCreated).JSON(&link)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM project_mirror_link WHERE id = $1`, link.ID) })
	return source, target, labelID, labelName, link.ID
}

func TestMirrorLinkCRUD(t *testing.T) {
	source, target, _, labelName, linkID := mirrorSetup(t)

	links := callListMirrorLinks(t, source)
	if len(links.Links) != 1 {
		t.Fatalf("links after create = %d, want 1", len(links.Links))
	}
	got := links.Links[0]
	if got.ID != linkID || got.TargetProjectID != target || got.TriggerLabel != labelName {
		t.Fatalf("link = %+v, want target %s label %s", got, target, labelName)
	}
	if got.TargetProjectTitle == "" {
		t.Fatalf("link should carry the target project title so the UI needs no second lookup: %+v", got)
	}

	// The same triple twice is a conflict, not a silent duplicate.
	callCreateMirrorLink(t, source, target, labelName).Want(http.StatusConflict)

	testutil.Call(t, testHandler.DeleteProjectMirrorLink, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/x", nil), "id", source, "linkId", linkID,
	)).Want(http.StatusNoContent)
	if n := len(callListMirrorLinks(t, source).Links); n != 0 {
		t.Fatalf("links after delete = %d, want 0", n)
	}
	// Deleting twice is a 404, never a silent success.
	testutil.Call(t, testHandler.DeleteProjectMirrorLink, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/x", nil), "id", source, "linkId", linkID,
	)).Want(http.StatusNotFound)
}

func TestMirrorLinkRejectsSelfAndCycle(t *testing.T) {
	suffix := uuid.NewString()[:8]
	a := dbfx.Project(t, "mirror cycle A "+suffix)
	b := dbfx.Project(t, "mirror cycle B "+suffix)
	c := dbfx.Project(t, "mirror cycle C "+suffix)
	label := "cycle-" + suffix
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM project_mirror_link WHERE source_project_id = ANY($1::uuid[])`, []string{a, b, c})
	})

	// A project mirroring into itself is a bad request, not a cycle.
	callCreateMirrorLink(t, a, a, label).Want(http.StatusBadRequest)
	// An unknown target is a 404 rather than a hint that it exists.
	callCreateMirrorLink(t, a, uuid.NewString(), label).Want(http.StatusNotFound)
	// An empty trigger label has nothing to fire on.
	callCreateMirrorLink(t, a, b, "  ").Want(http.StatusBadRequest)

	callCreateMirrorLink(t, a, b, label).Want(http.StatusCreated)
	callCreateMirrorLink(t, b, c, label).Want(http.StatusCreated)
	// c -> a closes the loop a -> b -> c -> a.
	callCreateMirrorLink(t, c, a, label).Want(http.StatusConflict)
	// b -> a closes the shorter loop a -> b -> a.
	callCreateMirrorLink(t, b, a, label).Want(http.StatusConflict)
}

func TestMirrorLinkWriteNeedsContributor(t *testing.T) {
	suffix := uuid.NewString()[:8]
	source := dbfx.Project(t, "mirror role source "+suffix)
	target := dbfx.Project(t, "mirror role target "+suffix)
	viewer := dbfx.User(t, "mirror viewer", "mirror-viewer-"+suffix+"@example.test")
	viewerMemberID := dbfx.Member(t, testWorkspaceID, viewer, "member")
	dbfx.Insert(t, "project_member_role", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": testWorkspaceID, "project_id": source,
		"subject_type": "member", "subject_id": viewerMemberID, "role": "viewer",
	})

	// The viewer still reads the configuration, but cannot change it.
	testutil.Call(t, testHandler.ListProjectMirrorLinks, testutil.WithURLParams(
		newRequestAs(viewer, http.MethodGet, "/x", nil), "id", source,
	)).Want(http.StatusOK)
	testutil.Call(t, testHandler.CreateProjectMirrorLink, testutil.WithURLParams(
		newRequestAs(viewer, http.MethodPost, "/x", map[string]any{
			"target_project_id": target, "trigger_label": "nope-" + suffix,
		}), "id", source,
	)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteProjectMirrorLink, testutil.WithURLParams(
		newRequestAs(viewer, http.MethodDelete, "/x", nil), "id", source, "linkId", uuid.NewString(),
	)).Want(http.StatusForbidden)
}

func TestAttachTriggerLabelCreatesMirrorPerTarget(t *testing.T) {
	suffix := uuid.NewString()[:8]
	source := dbfx.Project(t, "mirror fanout source "+suffix)
	targetA := dbfx.Project(t, "mirror fanout target A "+suffix)
	targetB := dbfx.Project(t, "mirror fanout target B "+suffix)
	labelName := "fanout-" + suffix
	labelID := mirrorLabel(t, labelName)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM project_mirror_link WHERE source_project_id = $1`, source) })
	callCreateMirrorLink(t, source, targetA, labelName).Want(http.StatusCreated)
	callCreateMirrorLink(t, source, targetB, labelName).Want(http.StatusCreated)

	issue := dbfx.Issue(t, "mirror me "+suffix, testutil.Cols{"project_id": source, "description": "the original body"})
	cleanupMirrorsOf(t, issue)
	callAttachLabel(t, issue, labelID).Want(http.StatusOK)

	mirrors := callIssueMirrors(t, issue).Mirrors
	if len(mirrors) != 2 {
		t.Fatalf("mirrors = %d, want one per target: %+v", len(mirrors), mirrors)
	}
	projects := map[string]bool{}
	for _, m := range mirrors {
		projects[m.ProjectID] = true
		if m.TypeSynced {
			t.Fatalf("a fresh mirror starts unsynced: %+v", m)
		}
		var title, description, originType string
		dbfx.QueryRow(t, `SELECT title, COALESCE(description, ''), COALESCE(origin_type, '') FROM issue WHERE id = $1`, m.MirrorIssueID).
			Scan(&title, &description, &originType)
		if originType != "mirror" {
			t.Fatalf("mirror origin_type = %q, want mirror", originType)
		}
		if !strings.HasPrefix(title, "[Mirror of ") {
			t.Fatalf("mirror title = %q, want the [Mirror of <IDENT>] prefix", title)
		}
		if !strings.Contains(description, "Generated from") || !strings.Contains(description, "the original body") {
			t.Fatalf("mirror description = %q, want provenance then the source body", description)
		}
		// The mirror BLOCKS the source: (issue_id = mirror, depends_on = source).
		if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue_dependency WHERE issue_id = $1 AND depends_on_issue_id = $2 AND type = 'blocks'`,
			m.MirrorIssueID, issue); n != 1 {
			t.Fatalf("blocking dependency mirror->source = %d, want 1", n)
		}
	}
	if !projects[targetA] || !projects[targetB] {
		t.Fatalf("mirrors landed in %v, want both targets", projects)
	}

	// The blocking edge is what the existing merge-readiness path reads: an
	// open mirror keeps the source from being ready to finish.
	var readiness MergeReadinessResponse
	testutil.Call(t, testHandler.GetIssueMergeReadiness, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/merge-readiness", nil), "id", issue,
	)).Want(http.StatusOK).JSON(&readiness)
	if readiness.Ready {
		t.Fatalf("a source with two open mirrors must not be ready: %+v", readiness)
	}
	blocking := 0
	for _, b := range readiness.Blockers {
		if b.Kind == "blocking_issue" {
			blocking++
		}
	}
	if blocking != 2 {
		t.Fatalf("blocking-issue blockers = %d, want 2: %+v", blocking, readiness.Blockers)
	}

	// Closing both mirrors clears the blockers again.
	for _, m := range mirrors {
		dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, m.MirrorIssueID)
	}
	testutil.Call(t, testHandler.GetIssueMergeReadiness, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/merge-readiness", nil), "id", issue,
	)).Want(http.StatusOK).JSON(&readiness)
	for _, b := range readiness.Blockers {
		if b.Kind == "blocking_issue" {
			t.Fatalf("a done mirror must stop blocking: %+v", readiness.Blockers)
		}
	}
}

func TestAttachTriggerLabelIsIdempotentAndScoped(t *testing.T) {
	source, _, labelID, labelName, linkID := mirrorSetup(t)
	suffix := uuid.NewString()[:8]

	issue := dbfx.Issue(t, "mirror idempotent "+suffix, testutil.Cols{"project_id": source})
	cleanupMirrorsOf(t, issue)
	callAttachLabel(t, issue, labelID).Want(http.StatusOK)
	first := callIssueMirrors(t, issue).Mirrors
	if len(first) != 1 {
		t.Fatalf("mirrors after first attach = %d, want 1", len(first))
	}

	// Detach then re-attach: the same source must not gain a second mirror.
	testutil.Call(t, testHandler.DetachLabel, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/x", nil), "id", issue, "labelId", labelID,
	)).Want(http.StatusOK)
	callAttachLabel(t, issue, labelID).Want(http.StatusOK)
	second := callIssueMirrors(t, issue).Mirrors
	if len(second) != 1 || second[0].MirrorIssueID != first[0].MirrorIssueID {
		t.Fatalf("re-attach produced %+v, want the same single mirror as %+v", second, first)
	}

	// An issue in an unrelated project carrying the same label is untouched.
	other := dbfx.Project(t, "mirror unrelated "+suffix)
	otherIssue := dbfx.Issue(t, "mirror unrelated issue "+suffix, testutil.Cols{"project_id": other})
	cleanupMirrorsOf(t, otherIssue)
	callAttachLabel(t, otherIssue, labelID).Want(http.StatusOK)
	if n := len(callIssueMirrors(t, otherIssue).Mirrors); n != 0 {
		t.Fatalf("an unrelated project mirrored %d issues, want 0", n)
	}

	// A different label on the source project is not the trigger either.
	otherLabel := mirrorLabel(t, "not-"+labelName)
	callAttachLabel(t, issue, otherLabel).Want(http.StatusOK)
	if n := len(callIssueMirrors(t, issue).Mirrors); n != 1 {
		t.Fatalf("a non-trigger label produced %d mirrors, want the original 1", n)
	}

	// Deleting the link keeps the mirror it already produced.
	testutil.Call(t, testHandler.DeleteProjectMirrorLink, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/x", nil), "id", source, "linkId", linkID,
	)).Want(http.StatusNoContent)
	if n := len(callIssueMirrors(t, issue).Mirrors); n != 1 {
		t.Fatalf("mirrors after link delete = %d, want the mirror kept", n)
	}
}

func TestMirrorTypeSyncedToggleAndMirrorOfBanner(t *testing.T) {
	source, _, labelID, _, _ := mirrorSetup(t)
	suffix := uuid.NewString()[:8]
	issue := dbfx.Issue(t, "mirror toggle "+suffix, testutil.Cols{"project_id": source})
	cleanupMirrorsOf(t, issue)
	callAttachLabel(t, issue, labelID).Want(http.StatusOK)

	mirrors := callIssueMirrors(t, issue).Mirrors
	if len(mirrors) != 1 {
		t.Fatalf("mirrors = %d, want 1", len(mirrors))
	}
	rec := mirrors[0]

	setSynced := func(issueID, mirrorID string, value bool) *testutil.Response {
		return testutil.Call(t, testHandler.SetIssueMirrorTypeSynced, testutil.WithURLParams(
			newRequest(http.MethodPut, "/x", map[string]any{"value": value}), "id", issueID, "mirrorId", mirrorID,
		))
	}
	setSynced(issue, rec.ID, true).Want(http.StatusOK)
	if !callIssueMirrors(t, issue).Mirrors[0].TypeSynced {
		t.Fatal("type_synced did not stick")
	}
	setSynced(issue, rec.ID, false).Want(http.StatusOK)
	if callIssueMirrors(t, issue).Mirrors[0].TypeSynced {
		t.Fatal("type_synced did not clear")
	}
	// A mirror id that belongs to no pair of this issue is a 404, so the
	// marker cannot be flipped by guessing ids.
	setSynced(issue, uuid.NewString(), true).Want(http.StatusNotFound)

	// The mirror side sees where it came from.
	fromMirror := callIssueMirrors(t, rec.MirrorIssueID)
	if fromMirror.MirrorOf == nil {
		t.Fatal("a mirror must report the source it was generated from")
	}
	if fromMirror.MirrorOf.SourceIssueID != issue {
		t.Fatalf("mirror_of = %+v, want source %s", fromMirror.MirrorOf, issue)
	}
	if len(fromMirror.Mirrors) != 0 {
		t.Fatalf("a mirror has no mirrors of its own: %+v", fromMirror.Mirrors)
	}
	// A plain issue reports neither side.
	plain := dbfx.Issue(t, "mirror plain "+suffix)
	got := callIssueMirrors(t, plain)
	if got.MirrorOf != nil || len(got.Mirrors) != 0 {
		t.Fatalf("a plain issue = %+v, want empty on both sides", got)
	}
}

// cleanupMirrorsOf removes the issues and rows the trigger creates on its own,
// which dbfx cannot register in advance.
func cleanupMirrorsOf(t *testing.T, sourceIssueID string) {
	t.Helper()
	// One statement: every CTE reads the same snapshot, so deleting the
	// issue_mirror rows cannot hide the mirror issues from the same teardown.
	// IssueService.Create numbers from workspace.issue_counter, which dbfx.Issue
	// deliberately does not move: without this the generated mirrors collide on
	// uq_issue_workspace_number (see org_test.go).
	syncIssueCounter(t)
	dbfx.Cleanup(t, `
		WITH m AS (SELECT mirror_issue_id FROM issue_mirror WHERE source_issue_id = $1),
		     d AS (DELETE FROM issue_dependency
		            WHERE depends_on_issue_id = $1 OR issue_id IN (SELECT mirror_issue_id FROM m)),
		     r AS (DELETE FROM issue_mirror WHERE source_issue_id = $1)
		DELETE FROM issue WHERE id IN (SELECT mirror_issue_id FROM m)`, sourceIssueID)
}

func TestOpenMirrorKeepsSourceOutOfDone(t *testing.T) {
	source, _, labelID, _, _ := mirrorSetup(t)
	suffix := uuid.NewString()[:8]
	issue := dbfx.Issue(t, "mirror gate "+suffix, testutil.Cols{"project_id": source, "status": "in_review"})
	cleanupMirrorsOf(t, issue)
	callAttachLabel(t, issue, labelID).Want(http.StatusOK)

	mirrors := callIssueMirrors(t, issue).Mirrors
	if len(mirrors) != 1 {
		t.Fatalf("mirrors = %d, want 1", len(mirrors))
	}

	var refused struct {
		Code    string                `json:"code"`
		Error   string                `json:"error"`
		Mirrors []IssueMirrorResponse `json:"mirrors"`
	}
	moveIssue(t, issue, "done").Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodeOpenMirrors || len(refused.Mirrors) != 1 {
		t.Fatalf("refusal = %+v, want code %s naming the open mirror", refused, ErrCodeOpenMirrors)
	}
	if !strings.Contains(refused.Error, mirrors[0].Identifier) {
		t.Fatalf("refusal message = %q, want it to name %s", refused.Error, mirrors[0].Identifier)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if status != "in_review" {
		t.Fatalf("status after a refused move = %q, want untouched", status)
	}

	// Not-done moves are never gated.
	moveIssue(t, issue, "in_progress").Want(http.StatusOK)
	// The batch path runs the same gate, so it cannot be used to bypass it.
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue},
		"updates":   map[string]any{"status": "done"},
	})).Want(http.StatusConflict)

	// Closing the mirror releases the source.
	moveIssue(t, mirrors[0].MirrorIssueID, "done").Want(http.StatusOK)
	moveIssue(t, issue, "done").Want(http.StatusOK)

	// Cancelled counts as finished too: a cancelled mirror never lands.
	moveIssue(t, mirrors[0].MirrorIssueID, "cancelled").Want(http.StatusOK)
	moveIssue(t, issue, "in_progress").Want(http.StatusOK)
	moveIssue(t, issue, "done").Want(http.StatusOK)

	// An issue with no mirrors at all is untouched by the gate.
	plain := dbfx.Issue(t, "mirror gate plain "+suffix)
	moveIssue(t, plain, "done").Want(http.StatusOK)
}
