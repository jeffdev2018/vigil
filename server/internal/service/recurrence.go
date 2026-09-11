package service

// Recurring issues (OS plan, table stakes): a rule on an issue spawns the
// next occurrence on a cron schedule or when the current one closes. The
// occurrence is a copy of the latest one (title, description with its
// checkboxes reset, priority, assignee, delegate, project, labels,
// properties, acceptance criteria, due date shifted by the same lead), goes
// through IssueService.Create so an assigned agent gets its run like any
// other issue, and carries recurrence_id so the series is one lookup.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	RecurrenceModeSchedule = "schedule"
	RecurrenceModeOnClose  = "on_close"
	recurrenceTickBatch    = 100
	recurrenceOriginType   = "recurrence"
)

var checkedBox = regexp.MustCompile(`(?im)^(\s*[-*+]\s+)\[[xX]\]`)

// ResetChecklist unticks every markdown checkbox so the next occurrence
// starts clean.
func ResetChecklist(markdown string) string {
	return checkedBox.ReplaceAllString(markdown, "${1}[ ]")
}

// ValidateRecurrenceMode reports whether mode is one we run.
func ValidateRecurrenceMode(mode string) error {
	switch mode {
	case RecurrenceModeSchedule, RecurrenceModeOnClose:
		return nil
	default:
		return errors.New("mode must be schedule or on_close")
	}
}

// NextRecurrenceRun computes the next fire time for a schedule rule; nil for
// on_close rules, which fire on status alone.
func NextRecurrenceRun(mode, cron, tz string, after time.Time) (pgtype.Timestamptz, error) {
	if mode != RecurrenceModeSchedule {
		return pgtype.Timestamptz{}, nil
	}
	runs, err := NextOccurrencesAfterUTC(cron, tz, after.UTC(), 1)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	if len(runs) == 0 {
		return pgtype.Timestamptz{}, errors.New("the schedule never fires")
	}
	return pgtype.Timestamptz{Time: runs[0], Valid: true}, nil
}

// RecurrenceService spawns occurrences. Issues is the create path every
// issue takes; Queries is the same pool.
type RecurrenceService struct {
	Queries *db.Queries
	Issues  *IssueService
	// Now is injectable for tests.
	Now func() time.Time
}

func NewRecurrenceService(q *db.Queries, issues *IssueService) *RecurrenceService {
	return &RecurrenceService{Queries: q, Issues: issues, Now: time.Now}
}

// Tick spawns every due occurrence: schedule rules past next_run_at, and
// on_close rules whose current occurrence is done or cancelled. Returns how
// many issues were created.
func (s *RecurrenceService) Tick(ctx context.Context) int {
	now := s.Now()
	created := 0
	due, err := s.Queries.ListDueIssueRecurrences(ctx, db.ListDueIssueRecurrencesParams{NextRunAt: pgtype.Timestamptz{Time: now, Valid: true}, Limit: recurrenceTickBatch})
	if err != nil {
		slog.Warn("recurrence: list due failed", "error", err)
	}
	for _, rec := range due {
		if _, err := s.Spawn(ctx, rec); err != nil {
			slog.Warn("recurrence: spawn failed", "recurrence_id", util.UUIDToString(rec.ID), "error", err)
			continue
		}
		created++
	}
	rows, err := s.Queries.ListOnCloseIssueRecurrences(ctx, recurrenceTickBatch)
	if err != nil {
		slog.Warn("recurrence: list on_close failed", "error", err)
	}
	for _, row := range rows {
		category := issuestatus.Effective(ctx, s.Queries, row.WorkspaceID, row.CurrentStatus)
		if category != issuestatus.Done && category != issuestatus.Cancelled {
			continue
		}
		rec := db.IssueRecurrence{ID: row.ID, WorkspaceID: row.WorkspaceID, IssueID: row.IssueID, CronExpression: row.CronExpression, Timezone: row.Timezone, Mode: row.Mode, Enabled: row.Enabled, NextRunAt: row.NextRunAt, LastOccurrenceID: row.LastOccurrenceID, OccurrenceCount: row.OccurrenceCount, CreatedByType: row.CreatedByType, CreatedByID: row.CreatedByID}
		if _, err := s.Spawn(ctx, rec); err != nil {
			slog.Warn("recurrence: on_close spawn failed", "recurrence_id", util.UUIDToString(rec.ID), "error", err)
			continue
		}
		created++
	}
	return created
}

// Spawn creates the next occurrence of rec from its latest one and advances
// the rule. A rule whose issue is gone is deleted instead.
func (s *RecurrenceService) Spawn(ctx context.Context, rec db.IssueRecurrence) (db.Issue, error) {
	sourceID := rec.IssueID
	if rec.LastOccurrenceID.Valid {
		sourceID = rec.LastOccurrenceID
	}
	source, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: sourceID, WorkspaceID: rec.WorkspaceID})
	if err != nil && rec.LastOccurrenceID.Valid {
		// The latest occurrence was deleted; fall back to the source issue.
		source, err = s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: rec.IssueID, WorkspaceID: rec.WorkspaceID})
	}
	if err != nil {
		if delErr := s.Queries.DeleteIssueRecurrenceByID(ctx, rec.ID); delErr != nil {
			slog.Warn("recurrence: orphan rule not removed", "recurrence_id", util.UUIDToString(rec.ID), "error", delErr)
			return db.Issue{}, fmt.Errorf("recurrence %s: its issue is gone and the rule could not be removed", util.UUIDToString(rec.ID))
		}
		return db.Issue{}, fmt.Errorf("recurrence %s: its issue is gone, rule removed", util.UUIDToString(rec.ID))
	}
	now := s.Now()
	labels, _ := s.Queries.ListLabelsForIssues(ctx, db.ListLabelsForIssuesParams{IssueIds: []pgtype.UUID{source.ID}, WorkspaceID: rec.WorkspaceID})
	labelIDs := make([]pgtype.UUID, 0, len(labels))
	for _, l := range labels {
		labelIDs = append(labelIDs, l.ID)
	}
	params := IssueCreateParams{
		WorkspaceID: rec.WorkspaceID, Title: source.Title, Status: issuestatus.Todo, Priority: source.Priority,
		AssigneeType: source.AssigneeType, AssigneeID: source.AssigneeID, DelegateType: source.DelegateType, DelegateID: source.DelegateID,
		CreatorType: rec.CreatedByType, CreatorID: rec.CreatedByID, ProjectID: source.ProjectID,
		OriginType: pgtype.Text{String: recurrenceOriginType, Valid: true}, OriginID: rec.ID, LabelIDs: labelIDs, AllowDuplicate: true,
	}
	if source.Description.Valid {
		params.Description = pgtype.Text{String: ResetChecklist(source.Description.String), Valid: true}
	}
	if source.DueDate.Valid {
		// Keep the same lead the previous occurrence had between its creation and its due date.
		lead := source.DueDate.Time.Sub(source.CreatedAt.Time.Truncate(24 * time.Hour))
		if lead < 0 {
			lead = 0
		}
		params.DueDate = pgtype.Date{Time: now.Truncate(24 * time.Hour).Add(lead), Valid: true}
	}
	result, err := s.Issues.Create(ctx, params, IssueCreateOpts{})
	if err != nil {
		return db.Issue{}, fmt.Errorf("create occurrence: %w", err)
	}
	created := result.Issue
	if err := s.Queries.SetIssueRecurrenceLink(ctx, db.SetIssueRecurrenceLinkParams{ID: created.ID, RecurrenceID: rec.ID}); err != nil {
		return created, fmt.Errorf("link occurrence: %w", err)
	}
	if len(source.Properties) > 0 {
		var props map[string]json.RawMessage
		if json.Unmarshal(source.Properties, &props) == nil {
			for key, value := range props {
				if _, err := s.Queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{Key: key, Value: value, ID: created.ID, WorkspaceID: rec.WorkspaceID}); err != nil {
					slog.Warn("recurrence: property copy failed", "key", key, "error", err)
				}
			}
		}
	}
	if len(source.AcceptanceCriteria) > 0 && strings.TrimSpace(string(source.AcceptanceCriteria)) != "" && string(source.AcceptanceCriteria) != "null" && string(source.AcceptanceCriteria) != "[]" {
		if _, err := s.Queries.UpdateIssueAcceptanceCriteria(ctx, db.UpdateIssueAcceptanceCriteriaParams{ID: created.ID, AcceptanceCriteria: source.AcceptanceCriteria, WorkspaceID: created.WorkspaceID}); err != nil {
			slog.Warn("recurrence: criteria copy failed", "error", err)
		}
	}
	next, err := NextRecurrenceRun(rec.Mode, rec.CronExpression, rec.Timezone, now)
	if err != nil {
		slog.Warn("recurrence: next run", "recurrence_id", util.UUIDToString(rec.ID), "error", err)
	}
	if _, err := s.Queries.AdvanceIssueRecurrence(ctx, db.AdvanceIssueRecurrenceParams{ID: rec.ID, LastOccurrenceID: created.ID, NextRunAt: next}); err != nil {
		return created, fmt.Errorf("advance rule: %w", err)
	}
	return created, nil
}
