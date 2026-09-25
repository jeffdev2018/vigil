package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Epic Mode (F18): applying an approved ticket breakdown. This is the only
// step with consequences — it creates real child issues and links them — so
// idempotence, the cap and the cycle refusal are pinned here.

func ticket(key, title string, dependsOn ...string) map[string]any {
	if dependsOn == nil {
		dependsOn = []string{}
	}
	return map[string]any{
		"external_key": key,
		"title":        title,
		"description":  "Do " + title + ".",
		"depends_on":   dependsOn,
	}
}

// approvedTickets walks the whole pipeline up to an approved tickets step.
func approvedTickets(t *testing.T, projectID string, tickets ...map[string]any) {
	t.Helper()
	writeAndApprove(t, projectID, epicKindPRD, "# PRD", nil)
	writeAndApprove(t, projectID, epicKindTechPlan, "# Plan", nil)
	writeAndApprove(t, projectID, epicKindWireframe, "# Wireframe", nil)
	writeAndApprove(t, projectID, epicKindTickets, "# Tickets", map[string]any{"tickets": tickets})
}

func TestEpicApplyCreatesChildIssuesWithTheirDependencies(t *testing.T) {
	projectID := epicProject(t)
	approvedTickets(t, projectID,
		ticket("T1", "Add the endpoint"),
		ticket("T2", "Test it", "T1"),
		ticket("T3", "Document it", "T1", "T2"),
	)

	// Acceptance 4.
	var out EpicApplyResponse
	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusOK).JSON(&out)
	if len(out.Created) != 3 || len(out.Existing) != 0 {
		t.Fatalf("apply created %d and reused %d; want 3 and 0", len(out.Created), len(out.Existing))
	}
	if out.Dependencies != 3 {
		t.Fatalf("dependencies = %d; want the 3 edges the breakdown declares", out.Dependencies)
	}

	// Every ticket is a child of the epic host issue, not a loose issue.
	var children int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE parent_issue_id = $1`, out.EpicIssueID).Scan(&children)
	if children != 3 {
		t.Fatalf("%d issues hang off the epic issue; want 3", children)
	}
	var origin string
	dbfx.QueryRow(t, `SELECT origin_type FROM issue WHERE id = $1`, out.Created[0].ID).Scan(&origin)
	if origin != "epic" {
		t.Errorf("child origin_type = %q, want epic", origin)
	}
	// `depends_on` means "must land first", so T1 BLOCKS T2.
	var blocks int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM issue_dependency d
		JOIN issue a ON a.id = d.issue_id
		JOIN issue b ON b.id = d.depends_on_issue_id
		WHERE d.type = 'blocks' AND a.title = 'Add the endpoint' AND b.title = 'Test it'`).Scan(&blocks)
	if blocks != 1 {
		t.Errorf("T1 does not block T2; the dependency direction is inverted")
	}
}

func TestEpicApplyReplayCreatesNoDuplicate(t *testing.T) {
	projectID := epicProject(t)
	approvedTickets(t, projectID, ticket("T1", "Add the endpoint"), ticket("T2", "Test it", "T1"))

	var first EpicApplyResponse
	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusOK).JSON(&first)

	// Acceptance 5: a replay reuses what the first apply created.
	var second EpicApplyResponse
	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusOK).JSON(&second)
	if len(second.Created) != 0 || len(second.Existing) != 2 {
		t.Fatalf("replay created %d and reused %d; want 0 and 2", len(second.Created), len(second.Existing))
	}
	if second.Dependencies != 0 {
		t.Errorf("replay re-linked %d dependencies; the edges already exist", second.Dependencies)
	}
	var total, edges int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE parent_issue_id = $1`, first.EpicIssueID).Scan(&total)
	dbfx.QueryRow(t, `
		SELECT count(*) FROM issue_dependency d
		WHERE d.issue_id IN (SELECT id FROM issue WHERE parent_issue_id = $1)`, first.EpicIssueID).Scan(&edges)
	if total != 2 || edges != 1 {
		t.Fatalf("after the replay: %d issues, %d edges; want 2 and 1", total, edges)
	}
}

func TestEpicApplyPartiallyReplaysAfterATicketIsAdded(t *testing.T) {
	projectID := epicProject(t)
	approvedTickets(t, projectID, ticket("T1", "Add the endpoint"))
	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusOK)

	// Editing tickets makes a new draft; approving it re-arms apply. The
	// already-created ticket is remembered through payload.applied, which the
	// edit carries forward.
	var applied map[string]string
	dbfx.QueryRow(t, `SELECT payload->'applied' FROM epic_artifact WHERE project_id = $1 AND kind = $2 AND state = 'approved'`,
		projectID, epicKindTickets).Scan(&applied)
	writeAndApprove(t, projectID, epicKindTickets, "# Tickets v2", map[string]any{
		"tickets": []map[string]any{ticket("T1", "Add the endpoint"), ticket("T2", "Test it", "T1")},
		"applied": applied,
	})

	var out EpicApplyResponse
	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusOK).JSON(&out)
	if len(out.Created) != 1 || len(out.Existing) != 1 {
		t.Fatalf("replay created %d and reused %d; want only the new ticket created", len(out.Created), len(out.Existing))
	}
	if out.Created[0].Title != "Test it" {
		t.Errorf("created %q; want the ticket that was not applied yet", out.Created[0].Title)
	}
}

func TestEpicApplyRefusesMoreThanTheCap(t *testing.T) {
	projectID := epicProject(t)
	tickets := make([]map[string]any, 0, epicMaxTickets+1)
	for i := 0; i <= epicMaxTickets; i++ {
		tickets = append(tickets, ticket("T"+uuid.NewString()[:8], "Ticket"))
	}
	approvedTickets(t, projectID, tickets...)

	body := applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusUnprocessableEntity).Map()
	if body["code"] != ErrCodeTooManyTickets {
		t.Fatalf("code = %v, want %s", body["code"], ErrCodeTooManyTickets)
	}
	var created int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE project_id = $1 AND parent_issue_id IS NOT NULL`, projectID).Scan(&created)
	if created != 0 {
		t.Fatalf("a refused apply created %d issues; the cap must be checked before any write", created)
	}
}

func TestEpicApplyRollsBackWhenADependencyIsRefused(t *testing.T) {
	projectID := epicProject(t)
	// A cycle the payload validator cannot see: T1 and T2 each wait on the
	// other, so whichever edge lands second closes the loop.
	writeAndApprove(t, projectID, epicKindPRD, "# PRD", nil)
	writeAndApprove(t, projectID, epicKindTechPlan, "# Plan", nil)
	writeAndApprove(t, projectID, epicKindWireframe, "# Wireframe", nil)
	writeAndApprove(t, projectID, epicKindTickets, "# Tickets", map[string]any{
		"tickets": []map[string]any{
			ticket("T1", "First", "T2"),
			ticket("T2", "Second", "T1"),
		},
	})

	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusConflict)

	// Compensation: nothing survives a refused apply, so a fixed breakdown
	// starts from a clean project rather than from half of the old one.
	var issues, edges int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE project_id = $1 AND parent_issue_id IS NOT NULL`, projectID).Scan(&issues)
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_dependency d WHERE d.issue_id IN (SELECT id FROM issue WHERE project_id = $1)`, projectID).Scan(&edges)
	if issues != 0 || edges != 0 {
		t.Fatalf("a refused apply left %d issues and %d edges behind", issues, edges)
	}
	var applied *string
	dbfx.QueryRow(t, `SELECT payload->>'applied' FROM epic_artifact WHERE project_id = $1 AND kind = $2 AND state = 'approved'`,
		projectID, epicKindTickets).Scan(&applied)
	if applied != nil {
		t.Errorf("a refused apply recorded %s as applied; a replay would then skip issues that do not exist", *applied)
	}
}

// TestEpicApplyAuditsAnOrphanedIssueWhenRollbackDeleteFails guards a real gap:
// applyEpicTickets' rollback closure only slog.Warned when the compensating
// DeleteIssue itself failed, with no further recovery — an operator had no
// way to find the orphaned issue short of grepping logs. A BEFORE DELETE
// trigger forces the same failure the compensating delete would hit in
// production (DB blip, FK from another table), and the test asserts an audit
// row records the orphan.
func TestEpicApplyAuditsAnOrphanedIssueWhenRollbackDeleteFails(t *testing.T) {
	ctx := context.Background()
	projectID := epicProject(t)
	writeAndApprove(t, projectID, epicKindPRD, "# PRD", nil)
	writeAndApprove(t, projectID, epicKindTechPlan, "# Plan", nil)
	writeAndApprove(t, projectID, epicKindWireframe, "# Wireframe", nil)
	writeAndApprove(t, projectID, epicKindTickets, "# Tickets", map[string]any{
		"tickets": []map[string]any{
			ticket("T1", "First", "T2"),
			ticket("T2", "Second", "T1"), // cycle: forces rollback() after both issues exist
		},
	})

	const functionName = "epic_rollback_delete_fail_fn"
	const triggerName = "epic_rollback_delete_fail_trg"
	if _, err := testPool.Exec(ctx, `
CREATE OR REPLACE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF OLD.project_id = '`+projectID+`' THEN
		RAISE EXCEPTION 'forced rollback delete failure';
	END IF;
	RETURN OLD;
END;
$$;`); err != nil {
		t.Fatalf("install failure function: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
CREATE TRIGGER `+triggerName+`
BEFORE DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION `+functionName+`();`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DROP TRIGGER IF EXISTS `+triggerName+` ON issue`)
		testPool.Exec(ctx, `DROP FUNCTION IF EXISTS `+functionName+`()`)
	})

	applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusConflict)

	// The compensating delete failed, so the created issues are still there
	// (that part of the bug is pre-existing and out of scope here) — but an
	// audit row must now exist so an operator can find them.
	var issues int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE project_id = $1 AND parent_issue_id IS NOT NULL`, projectID).Scan(&issues)
	if issues != 2 {
		t.Fatalf("expected the forced trigger to keep both orphaned issues, got %d", issues)
	}
	var orphanAudits int
	dbfx.QueryRow(t, `SELECT count(*) FROM audit_log_entry WHERE action = $1 AND entity_type = 'issue' AND details->>'reason' = 'rollback_delete_failed' AND entity_id IN (SELECT id FROM issue WHERE project_id = $2)`,
		AuditEpicStepFailed, projectID).Scan(&orphanAudits)
	if orphanAudits != 2 {
		t.Fatalf("orphan audit rows = %d, want 2 (one per issue rollback could not delete)", orphanAudits)
	}
}

func TestEpicApplyRefusesAnUnapprovedOrUnusableBreakdown(t *testing.T) {
	projectID := epicProject(t)
	writeAndApprove(t, projectID, epicKindPRD, "# PRD", nil)
	writeAndApprove(t, projectID, epicKindTechPlan, "# Plan", nil)
	writeAndApprove(t, projectID, epicKindWireframe, "# Wireframe", nil)

	// A draft is not enough: apply needs the approval.
	putEpicStep(t, projectID, epicKindTickets, map[string]any{
		"content": "# Tickets",
		"payload": map[string]any{"tickets": []map[string]any{ticket("T1", "One")}},
	}).Want(http.StatusOK)
	body := applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusConflict).Map()
	if body["code"] != ErrCodeEpicNotApproved {
		t.Fatalf("code = %v, want %s", body["code"], ErrCodeEpicNotApproved)
	}

	// A breakdown pointing at a ticket that is not in the list is refused at
	// the edit, before it can ever be approved.
	putEpicStep(t, projectID, epicKindTickets, map[string]any{
		"content": "# Tickets",
		"payload": map[string]any{"tickets": []map[string]any{ticket("T1", "One", "T9")}},
	}).Want(http.StatusBadRequest)
	putEpicStep(t, projectID, epicKindTickets, map[string]any{
		"content": "# Tickets",
		"payload": map[string]any{"tickets": []map[string]any{}},
	}).Want(http.StatusBadRequest)
}

func TestEpicApplyIsLockedUntilTheEarlierStepsAreApproved(t *testing.T) {
	projectID := epicProject(t)
	writeAndApprove(t, projectID, epicKindPRD, "# PRD", nil)

	body := applyEpicStep(t, projectID, epicKindTickets).Want(http.StatusConflict).Map()
	if body["code"] != ErrCodeEpicStepLocked {
		t.Fatalf("code = %v, want %s", body["code"], ErrCodeEpicStepLocked)
	}
}
