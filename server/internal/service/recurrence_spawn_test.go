package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// recurrenceTestFixture seeds a workspace, an owner member and a source
// issue for a schedule-mode recurrence rule.
type recurrenceTestFixture struct {
	dbfx      *testutil.Fixture
	queries   *db.Queries
	svc       *RecurrenceService
	workspace string
	sourceID  string
	userID    string
}

func newRecurrenceTestFixture(t *testing.T) recurrenceTestFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := bootstrap.User(t,
		fmt.Sprintf("recurrence-owner-%d", suffix),
		fmt.Sprintf("recurrence-owner-%d@example.com", suffix),
	)
	workspaceID := bootstrap.Workspace(t,
		fmt.Sprintf("recurrence-%d", suffix),
		fmt.Sprintf("recurrence-%d", suffix),
	)
	dbfx := testutil.New(pool, workspaceID, userID)
	dbfx.Member(t, workspaceID, userID, "owner")
	sourceID := dbfx.Issue(t, "Weekly status report")
	// dbfx.Issue numbers the row directly (MAX+1) without advancing
	// workspace.issue_counter, the sequence IssueService.Create actually
	// draws from; without this, Spawn's own Create would collide with the
	// number just inserted above (uq_issue_workspace_number).
	if _, err := pool.Exec(context.Background(),
		`UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`,
		workspaceID); err != nil {
		t.Fatalf("sync issue counter: %v", err)
	}

	queries := db.New(pool)
	issues := NewIssueService(queries, pool, events.New(), nil, nil)
	svc := &RecurrenceService{Queries: queries, Issues: issues, Now: time.Now}
	return recurrenceTestFixture{dbfx: dbfx, queries: queries, svc: svc, workspace: workspaceID, sourceID: sourceID, userID: userID}
}

func (fx recurrenceTestFixture) createRule(t *testing.T, cron string, nextRunAt time.Time) db.IssueRecurrence {
	t.Helper()
	rec, err := fx.queries.CreateIssueRecurrence(context.Background(), db.CreateIssueRecurrenceParams{
		ID:             dbid.NewV7(),
		WorkspaceID:    util.MustParseUUID(fx.workspace),
		IssueID:        util.MustParseUUID(fx.sourceID),
		CronExpression: cron,
		Timezone:       "UTC",
		Mode:           RecurrenceModeSchedule,
		Enabled:        true,
		NextRunAt:      pgtype.Timestamptz{Time: nextRunAt, Valid: true},
		CreatedByType:  "member",
		CreatedByID:    util.MustParseUUID(fx.userID),
	})
	if err != nil {
		t.Fatalf("CreateIssueRecurrence: %v", err)
	}
	return rec
}

func recurrenceIDsInclude(rows []db.IssueRecurrence, id pgtype.UUID) bool {
	want := util.UUIDToString(id)
	for _, r := range rows {
		if util.UUIDToString(r.ID) == want {
			return true
		}
	}
	return false
}

// Spawn must create exactly one occurrence and leave the rule not due again
// at the moment it was spawned.
func TestRecurrenceSpawnCreatesOccurrenceAndClosesTheWindow(t *testing.T) {
	fx := newRecurrenceTestFixture(t)
	now := time.Now().UTC()
	rec := fx.createRule(t, "0 9 * * 1", now.Add(-time.Minute))

	created, err := fx.svc.Spawn(context.Background(), rec)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if created.Title != "Weekly status report" {
		t.Fatalf("created title = %q", created.Title)
	}

	due, err := fx.queries.ListDueIssueRecurrences(context.Background(), db.ListDueIssueRecurrencesParams{
		NextRunAt: pgtype.Timestamptz{Time: now, Valid: true}, Limit: 100,
	})
	if err != nil {
		t.Fatalf("ListDueIssueRecurrences: %v", err)
	}
	if recurrenceIDsInclude(due, rec.ID) {
		t.Fatalf("rule is still due right after a successful Spawn")
	}
}

// This is the regression test for the duplicate-spawn bug: Spawn advances
// next_run_at (via AdvanceIssueRecurrenceNextRunOnly) BEFORE it creates the
// occurrence. Simulating an interruption at exactly that point -- the
// window the audit finding exploited -- must leave the rule not due, so a
// crash or transient DB error there costs one skipped occurrence, never a
// duplicate one from the same due window firing again on the next tick.
func TestRecurrenceEarlyAdvanceClosesTheReFireWindowBeforeCreate(t *testing.T) {
	fx := newRecurrenceTestFixture(t)
	now := time.Now().UTC()
	rec := fx.createRule(t, "0 9 * * 1", now.Add(-time.Minute))

	ctx := context.Background()
	due, err := fx.queries.ListDueIssueRecurrences(ctx, db.ListDueIssueRecurrencesParams{NextRunAt: pgtype.Timestamptz{Time: now, Valid: true}, Limit: 100})
	if err != nil {
		t.Fatalf("ListDueIssueRecurrences (before): %v", err)
	}
	if !recurrenceIDsInclude(due, rec.ID) {
		t.Fatalf("rule was not due before the early advance")
	}

	next, err := NextRecurrenceRun(rec.Mode, rec.CronExpression, rec.Timezone, now)
	if err != nil || !next.Valid {
		t.Fatalf("NextRecurrenceRun: %v %v", next, err)
	}
	// Simulate exactly the step Spawn performs before Issues.Create, then
	// stop -- as if the process crashed or Create failed right after it.
	if err := fx.queries.AdvanceIssueRecurrenceNextRunOnly(ctx, db.AdvanceIssueRecurrenceNextRunOnlyParams{ID: rec.ID, NextRunAt: next}); err != nil {
		t.Fatalf("AdvanceIssueRecurrenceNextRunOnly: %v", err)
	}

	due, err = fx.queries.ListDueIssueRecurrences(ctx, db.ListDueIssueRecurrencesParams{NextRunAt: pgtype.Timestamptz{Time: now, Valid: true}, Limit: 100})
	if err != nil {
		t.Fatalf("ListDueIssueRecurrences (after): %v", err)
	}
	if recurrenceIDsInclude(due, rec.ID) {
		t.Fatalf("rule is still due after the early advance -- a crash here would duplicate-spawn on the next tick")
	}
}
