package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Plan verification (F17): after a run completes on an issue that carries an
// active plan, and when the workspace opted in, a second ordinary run is
// queued on the same issue with the plan in its handoff note. The
// plan_verification row is what marks that run as a verification, so a
// completed verification never spawns another.

// PlanVerificationHandoffPrefix opens every verification handoff note; the
// built-in skill tells the agent to recognise it.
const PlanVerificationHandoffPrefix = "Plan verification"

// planHandoffMaxBytes bounds the plan text carried in the handoff note; the
// agent can always fetch the full plan with `multica issue plan get`.
const planHandoffMaxBytes = 24 << 10

// PlanVerificationGateEnabled reads the workspace setting. Absent means off:
// nobody pays for a verification run they did not ask for.
func PlanVerificationGateEnabled(settings []byte) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		Gate *bool `json:"plan_verification_gate"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return false
	}
	return s.Gate != nil && *s.Gate
}

// PlanVerificationHandoffNote renders the plan for the verification run.
func PlanVerificationHandoffNote(issueIdentifier string, plan db.IssuePlan) string {
	content := strings.TrimSpace(plan.Content)
	if len(content) > planHandoffMaxBytes {
		cut := planHandoffMaxBytes
		for cut > 0 && content[cut]&0xC0 == 0x80 {
			cut--
		}
		content = content[:cut] + "\n…(plan truncated; run `multica issue plan get " + issueIdentifier + "` for the full text)"
	}
	return fmt.Sprintf("%s of %s, plan version %d. Do not change code: compare what the previous run delivered "+
		"(linked pull requests, branch diff) against this plan and report with `multica issue plan report %s --file findings.json`.\n\n%s",
		PlanVerificationHandoffPrefix, issueIdentifier, plan.Version, issueIdentifier, content)
}

// MaybeEnqueuePlanVerification runs after a task completed. It queues at most
// one verification run per completed run and never for a verification run.
// Every early return is a legitimate "nothing to do"; only real failures are
// returned.
func (s *TaskService) MaybeEnqueuePlanVerification(ctx context.Context, taskID pgtype.UUID) error {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}
	if !task.IssueID.Valid {
		return nil
	}
	exists, err := s.Queries.PlanVerificationExistsForSource(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("check verification: %w", err)
	}
	if exists {
		return nil
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return fmt.Errorf("load issue: %w", err)
	}
	ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}
	if !PlanVerificationGateEnabled(ws.Settings) {
		return nil
	}
	plan, err := s.Queries.GetActiveIssuePlan(ctx, db.GetActiveIssuePlanParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return nil // only an agent-assigned issue can run a verification
	}

	identifier := fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
	note := PlanVerificationHandoffNote(identifier, plan)
	verification, err := s.EnqueueTaskForIssueWithHandoff(ctx, issue, note, pgtype.UUID{})
	if err != nil {
		return fmt.Errorf("enqueue verification run: %w", err)
	}
	if _, err := s.Queries.CreatePlanVerification(ctx, db.CreatePlanVerificationParams{
		WorkspaceID:  issue.WorkspaceID,
		IssueID:      issue.ID,
		PlanID:       plan.ID,
		PlanVersion:  plan.Version,
		TaskID:       verification.ID,
		SourceTaskID: task.ID,
	}); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	slog.Info("plan verification queued",
		"issue_id", util.UUIDToString(issue.ID),
		"source_task_id", util.UUIDToString(task.ID),
		"verification_task_id", util.UUIDToString(verification.ID),
		"plan_version", plan.Version)
	return nil
}

// SyncPlanVerificationState mirrors the verification run's lifecycle onto its
// row. Unknown tasks are simply not verification runs.
func (s *TaskService) SyncPlanVerificationState(ctx context.Context, taskID pgtype.UUID, state string) {
	if err := s.Queries.SetPlanVerificationState(ctx, db.SetPlanVerificationStateParams{TaskID: taskID, State: state}); err != nil {
		slog.Warn("plan verification: state sync failed", "task_id", util.UUIDToString(taskID), "state", state, "error", err)
	}
}
