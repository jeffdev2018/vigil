package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// compile-time guard: the production wiring hands this seam the shared
// *llm.Client; drift between the two interfaces must fail here, not at a
// runtime type assertion.
var _ RunConfidenceLLM = (*llm.Client)(nil)

// Run confidence scoring (JEF-240): after a run completes successfully, the
// assist-layer LLM self-assesses how confident it is that the delivery is
// correct and complete. The score is stored on the task (agent_task_queue.
// confidence) and broadcast; under the workspace threshold, the linked issue
// goes to human review automatically. Like skill distillation, a score is
// only worth storing when genuinely assessed, so a disabled LLM simply turns
// this pass off.

const (
	runConfidenceTimeout = 45 * time.Second
	// runConfidenceMaxConcurrent bounds scoring passes in flight
	// process-wide; passes over the ceiling are dropped, not queued.
	runConfidenceMaxConcurrent = 4
	// runConfidenceOutputBudget bounds how much of the run's final output is
	// sent upstream (head + tail kept, mirroring memory extraction).
	runConfidenceOutputBudget = 4000
	// runConfidenceRationaleMaxRunes bounds the stored rationale.
	runConfidenceRationaleMaxRunes = 280
	// ConfidenceReviewInboxType is the inbox item type a below-threshold run
	// raises for the workspace's managers.
	ConfidenceReviewInboxType = "confidence_review"
)

// RunConfidenceLLM is the seam TaskService uses for the scoring pass,
// satisfied by *llm.Client. Same shape as AgentMemoryLLM/SkillDistillationLLM,
// plus DefaultModel so the stored record names the model that produced it.
type RunConfidenceLLM interface {
	Enabled() bool
	DefaultModel() string
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// TaskConfidence is the assessment persisted verbatim into
// agent_task_queue.confidence (JSONB). BelowThreshold is recomputed against
// the threshold that applied at scoring time, so a later threshold change
// never silently reclassifies a stored run.
type TaskConfidence struct {
	Score          float64 `json:"score"`
	Rationale      string  `json:"rationale"`
	Model          string  `json:"model"`
	Threshold      float64 `json:"threshold"`
	BelowThreshold bool    `json:"below_threshold"`
}

// SubscribeRunConfidence wires the scoring pass onto the bus. The listener
// runs INLINE on the publisher's goroutine (Bus.Publish is synchronous), so
// it does the minimum — gate on the payload, hand off to a detached worker —
// and never touches the database or the network itself.
func (s *TaskService) SubscribeRunConfidence(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		if s.RunConfidence == nil || !s.RunConfidence.Enabled() {
			return
		}
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Defense in depth: only true successes.
		if status, _ := payload["status"].(string); status != "completed" {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		s.launchRunConfidence(taskID)
	})
}

// launchRunConfidence spawns the detached worker with two admission gates:
// one pass per task (a redelivered event must not race itself) and a
// process-wide ceiling.
func (s *TaskService) launchRunConfidence(taskID pgtype.UUID) {
	key := util.UUIDToString(taskID)
	if _, inFlight := s.runConfidenceInFlight.LoadOrStore(key, struct{}{}); inFlight {
		return
	}
	if s.runConfidenceRunning.Add(1) > runConfidenceMaxConcurrent {
		s.runConfidenceRunning.Add(-1)
		s.runConfidenceInFlight.Delete(key)
		slog.Warn("run confidence shed: process-wide concurrency ceiling reached",
			"ceiling", runConfidenceMaxConcurrent, "task_id", key)
		return
	}

	go func() {
		defer func() {
			s.runConfidenceRunning.Add(-1)
			s.runConfidenceInFlight.Delete(key)
		}()
		// Panic containment: this goroutine is detached from any request, so
		// nothing above it recovers. Scoring is a nicety — swallow, log.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("run confidence panicked",
					"task_id", key, "panic", rec)
			}
		}()

		// The worker owns its context: the event publisher's is typically an
		// HTTP request context cancelled when the completion callback returns.
		ctx, cancel := context.WithTimeout(context.Background(), runConfidenceTimeout)
		defer cancel()

		if err := s.ScoreRunConfidence(ctx, taskID); err != nil {
			slog.Warn("run confidence failed",
				"task_id", key, "error", err)
		}
	}()
}

// ScoreRunConfidence self-assesses a successfully completed task and persists
// the score on its row. Synchronous and safe to call directly from tests; the
// async admission path is launchRunConfidence.
func (s *TaskService) ScoreRunConfidence(ctx context.Context, taskID pgtype.UUID) error {
	if s.RunConfidence == nil || !s.RunConfidence.Enabled() {
		return nil
	}

	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The task row vanished between the event and the pass (workspace
			// teardown) — nothing to score.
			return nil
		}
		return fmt.Errorf("load completed task: %w", err)
	}
	// Only true successes, only runs attached to an issue, and never a review
	// run: a review does not get scored itself.
	if task.Status != "completed" || !task.IssueID.Valid || task.ReviewOfTaskID.Valid {
		return nil
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task agent: %w", err)
	}
	// System agents are product plumbing, not deliveries to review.
	if agent.SystemKey.String != "" {
		return nil
	}

	ws, err := s.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}
	cfg := ConfidenceReviewSettings(ws.Settings)

	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task issue: %w", err)
	}

	// The run's final output is the delivery evidence. It is in
	// agent_task_queue.result, not the event payload.
	var completed protocol.TaskCompletedPayload
	if len(task.Result) > 0 {
		if uerr := json.Unmarshal(task.Result, &completed); uerr != nil {
			return nil
		}
	}

	raw, err := s.RunConfidence.GenerateJSON(ctx,
		"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
		runConfidenceSystemPrompt,
		renderRunConfidencePrompt(issue.Title, completed.Output, s.latestCrossReviewVerdict(ctx, task.ID)),
		0.1,
		512,
	)
	if err != nil {
		// A score needs a real assessment; an LLM failure means nothing worth
		// storing.
		slog.Warn("run confidence LLM call failed, skipping", "error", err)
		return nil
	}
	score, rationale, perr := parseConfidenceScore(raw)
	if perr != nil {
		slog.Warn("run confidence parse failed, skipping", "error", perr)
		return nil
	}
	if utf8.RuneCountInString(rationale) > runConfidenceRationaleMaxRunes {
		rationale = string([]rune(rationale)[:runConfidenceRationaleMaxRunes])
	}

	conf := TaskConfidence{
		Score:          score,
		Rationale:      rationale,
		Model:          s.RunConfidence.DefaultModel(),
		Threshold:      cfg.Threshold,
		BelowThreshold: score < cfg.Threshold,
	}
	encoded, merr := json.Marshal(conf)
	if merr != nil {
		return fmt.Errorf("marshal confidence: %w", merr)
	}
	updated, err := s.Queries.SetTaskConfidence(ctx, db.SetTaskConfidenceParams{ID: task.ID, Confidence: encoded})
	if err != nil {
		return fmt.Errorf("store confidence: %w", err)
	}

	s.publishTaskScored(agent.WorkspaceID, updated, conf)

	if conf.BelowThreshold && cfg.Enabled {
		s.escalateRunToHumanReview(ctx, issue, updated, conf)
	}
	return nil
}

// latestCrossReviewVerdict returns the verdict an independent reviewer left on
// this run's change, when one exists. Best-effort: any failure yields "".
func (s *TaskService) latestCrossReviewVerdict(ctx context.Context, taskID pgtype.UUID) string {
	review, err := s.Queries.GetLatestCrossReviewForTask(ctx, taskID)
	if err != nil {
		return ""
	}
	msg, err := s.Queries.GetLatestReviewReportMessage(ctx, review.ID)
	if err != nil || !msg.Content.Valid {
		return ""
	}
	var report struct {
		Verdict string `json:"verdict"`
	}
	if json.Unmarshal([]byte(msg.Content.String), &report) != nil {
		return ""
	}
	return report.Verdict
}

// publishTaskScored broadcasts the persisted score so live clients can show
// it without refetching the task list.
func (s *TaskService) publishTaskScored(workspaceID pgtype.UUID, task db.AgentTaskQueue, conf TaskConfidence) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskScored,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":         util.UUIDToString(task.ID),
			"issue_id":        util.UUIDToString(task.IssueID),
			"score":           conf.Score,
			"threshold":       conf.Threshold,
			"below_threshold": conf.BelowThreshold,
		},
	})
}

// escalateRunToHumanReview routes a below-threshold run's issue to the
// workspace's effective in_review status and notifies its managers. Terminal
// issues (done / cancelled, resolved through custom statuses) are never
// touched. Best-effort: the score is already stored, so a failure here is
// logged, never propagated.
func (s *TaskService) escalateRunToHumanReview(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, conf TaskConfidence) {
	effective := issuestatus.Effective(ctx, s.Queries, issue.WorkspaceID, issue.Status)
	if effective == issuestatus.Done || effective == issuestatus.Cancelled {
		return
	}
	if effective != issuestatus.InReview {
		updated, err := s.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          issue.ID,
			Status:      issuestatus.InReview,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			slog.Warn("run confidence: move issue to review failed",
				"issue_id", util.UUIDToString(issue.ID), "error", err)
			return
		}
		s.broadcastIssueUpdated(ctx, updated, issue.Status)
		issue = updated
	}

	details, err := json.Marshal(map[string]any{
		"score":     conf.Score,
		"threshold": conf.Threshold,
		"task_id":   util.UUIDToString(task.ID),
	})
	if err != nil {
		details = []byte("{}")
	}
	title := "Run below confidence threshold"
	if issue.Title != "" {
		title = "Run below confidence threshold: " + issue.Title
	}
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, s.Queries, issue.WorkspaceID)
	if err != nil {
		slog.Warn("run confidence: list notification recipients failed",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		return
	}
	for _, rcpt := range recipients {
		item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   issue.WorkspaceID,
			RecipientType: rcpt.Type,
			RecipientID:   rcpt.ID,
			Type:          ConfidenceReviewInboxType,
			Severity:      "attention",
			IssueID:       issue.ID,
			Title:         title,
			Body:          pgtype.Text{String: conf.Rationale, Valid: conf.Rationale != ""},
			ActorType:     pgtype.Text{String: "system", Valid: true},
			Details:       details,
		})
		if err != nil {
			slog.Warn("run confidence: inbox write failed",
				"issue_id", util.UUIDToString(issue.ID), "error", err)
			continue
		}
		if s.Bus != nil {
			s.Bus.Publish(events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: util.UUIDToString(issue.WorkspaceID),
				ActorType:   "system",
				Payload:     map[string]any{"item": inboxItemToMap(item)},
			})
		}
	}
}

// inboxItemToMap is the service-package rendering of an inbox row for the
// inbox:new payload, mirroring the handler's inboxToResponse shape (which
// service cannot import).
func inboxItemToMap(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
}
