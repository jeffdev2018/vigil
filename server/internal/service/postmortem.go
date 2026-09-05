package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Postmortem autogen (k68): after a run fails terminally, a best-effort pass
// drafts a postmortem — summary, root cause, impact, and preventive rules.
// When the deployment has an assist-layer LLM it writes the analysis; without
// one (self-hosted, no MULTICA_LLM_*) a deterministic scaffold built from the
// failure facts is stored instead, so the artifact always exists. The whole
// path is a nicety: a disabled or failing LLM must cost the failed run
// nothing.

const (
	postmortemTimeout = 45 * time.Second
	// postmortemMaxConcurrent bounds generation passes in flight process-wide;
	// passes over the ceiling are dropped, not queued.
	postmortemMaxConcurrent = 4
	// postmortemTranscriptBudget bounds how much of the run's transcript is
	// sent upstream. The tail carries what the agent was doing when it failed.
	postmortemTranscriptBudget = 8000
	// postmortemMaxMessages caps the transcript messages considered.
	postmortemMaxMessages = 60
	// postmortemMaxRules / postmortemRuleMaxRunes mirror the storage shape so an
	// over-long model rule is dropped, never persisted then rejected.
	postmortemMaxRules     = 8
	postmortemRuleMaxRunes = 500
)

// postmortemSkipReasons are terminal failures that are pure infrastructure —
// the runtime died or the task never got to run. There is no work to analyze,
// so drafting a postmortem would be noise.
var postmortemSkipReasons = map[string]struct{}{
	"runtime_offline":  {},
	"runtime_recovery": {},
	"queued_expired":   {},
}

// PostmortemLLM is the seam TaskService uses for the drafting pass, satisfied
// by *llm.Client. Same shape as AgentMemoryLLM: an interface so tests can
// drive the pass without an HTTP upstream.
type PostmortemLLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// SubscribePostmortemGeneration wires the drafting pass onto the bus. The
// listener runs INLINE on the publisher's goroutine (Bus.Publish is
// synchronous), so it does the minimum — gate on the payload, hand off to a
// detached worker — and never touches the database or the network itself.
func (s *TaskService) SubscribePostmortemGeneration(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Only terminal failures: an intermediate retry attempt carries
		// retry_pending=true and will get another chance, so no postmortem yet.
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		s.launchPostmortemGeneration(taskID, "failed")
	})
	// Costly-run trigger: a run that SUCCEEDED but cost more than the
	// workspace's threshold. Gated entirely inside the worker, because the
	// threshold and the run's cost both need the database and this listener
	// runs inline on the publisher's goroutine.
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if status, _ := payload["status"].(string); status != "completed" {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		s.launchPostmortemGeneration(taskID, "costly")
	})
}

// launchPostmortemGeneration spawns the detached worker with two admission
// gates: one pass per task (a redelivered event must not race itself) and a
// process-wide ceiling.
func (s *TaskService) launchPostmortemGeneration(taskID pgtype.UUID, trigger string) {
	key := util.UUIDToString(taskID)
	if _, inFlight := s.postmortemInFlight.LoadOrStore(key, struct{}{}); inFlight {
		return
	}
	if s.postmortemRunning.Add(1) > postmortemMaxConcurrent {
		s.postmortemRunning.Add(-1)
		s.postmortemInFlight.Delete(key)
		slog.Warn("postmortem generation shed: process-wide concurrency ceiling reached",
			"ceiling", postmortemMaxConcurrent, "task_id", key)
		return
	}

	go func() {
		defer func() {
			s.postmortemRunning.Add(-1)
			s.postmortemInFlight.Delete(key)
		}()
		// Panic containment: this goroutine is detached from any request, so
		// nothing above it recovers. Generation is a nicety — swallow, log.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("postmortem generation panicked",
					"task_id", key, "panic", rec)
			}
		}()

		// The worker owns its context: the event publisher's is typically an
		// HTTP request context cancelled when the failure callback returns.
		ctx, cancel := context.WithTimeout(context.Background(), postmortemTimeout)
		defer cancel()

		var err error
		if trigger == "costly" {
			err = s.GenerateCostlyPostmortemForTask(ctx, taskID)
		} else {
			err = s.GeneratePostmortemForTask(ctx, taskID)
		}
		if err != nil {
			slog.Warn("postmortem generation failed",
				"task_id", key, "trigger", trigger, "error", err)
		}
	}()
}

// GeneratePostmortemForTask drafts and stores one postmortem for a failed
// task. Synchronous and safe to call directly from tests; the async admission
// path is launchPostmortemGeneration.
func (s *TaskService) GeneratePostmortemForTask(ctx context.Context, taskID pgtype.UUID) error {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The task row vanished between the event and the pass (workspace
			// teardown) — nothing to analyze, nothing to report.
			return nil
		}
		return fmt.Errorf("load failed task: %w", err)
	}
	if task.Status != "failed" {
		return nil
	}
	failureReason := strings.TrimSpace(task.FailureReason.String)
	if _, skip := postmortemSkipReasons[failureReason]; skip {
		return nil
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task agent: %w", err)
	}
	return s.storePostmortem(ctx, task, agent, "failed", failureReason)
}

// GenerateCostlyPostmortemForTask drafts a postmortem for a run that SUCCEEDED
// but cost more than the workspace's postmortem_cost_threshold_usd_ticks. A
// workspace that never set the threshold (NULL, the default) is skipped, so
// the completed-task subscription costs an untouched deployment one cheap read
// and nothing else. Synchronous and safe to call directly from tests.
func (s *TaskService) GenerateCostlyPostmortemForTask(ctx context.Context, taskID pgtype.UUID) error {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load completed task: %w", err)
	}
	if task.Status != "completed" {
		return nil
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task agent: %w", err)
	}

	ws, err := s.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load workspace: %w", err)
	}
	threshold := ws.PostmortemCostThresholdUsdTicks
	if !threshold.Valid || threshold.Int64 <= 0 {
		return nil
	}
	costTicks, _, _ := s.loadPostmortemUsage(ctx, task.ID)
	// Strictly greater: "more than $X" is what the setting says, so a run that
	// lands exactly on the threshold is not costly.
	if costTicks <= threshold.Int64 {
		return nil
	}

	return s.storePostmortem(ctx, task, agent, "costly", "")
}

// storePostmortem is the shared drafting-and-insert body of both triggers.
// Everything above it decides WHETHER this run deserves a postmortem; this
// decides what it says.
// DraftPostmortemForRun drafts a postmortem for any run with a named trigger
// (K75: a dissolved task force). Idempotent per run.
func (s *TaskService) DraftPostmortemForRun(ctx context.Context, task db.AgentTaskQueue, trigger, reason string) error {
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}
	return s.storePostmortem(ctx, task, agent, trigger, reason)
}

func (s *TaskService) storePostmortem(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, trigger, failureReason string) error {
	// Idempotent: a postmortem already exists for this run (redelivered event
	// or a rerun of the pass). CreatePostmortem also guards this via the unique
	// index; the early read keeps the LLM spend out of the duplicate case.
	if existing, err := s.Queries.GetPostmortemBySourceTask(ctx, task.ID); err == nil && existing.ID.Valid {
		return nil
	}

	issueTitle := ""
	if task.IssueID.Valid {
		if issue, ierr := s.Queries.GetIssue(ctx, task.IssueID); ierr == nil {
			issueTitle = issue.Title
		}
	}
	transcript := s.loadPostmortemTranscript(ctx, task.ID)
	costTicks, _, _ := s.loadPostmortemUsage(ctx, task.ID)
	errMsg := strings.TrimSpace(task.Error.String)

	summary, rootCause, impact, rules, llmUsed := s.draftPostmortem(ctx, trigger, issueTitle, failureReason, errMsg, costTicks, task.Attempt, task.MaxAttempts, transcript)

	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		rulesJSON = []byte("[]")
	}
	var costParam pgtype.Int8
	if costTicks > 0 {
		costParam = pgtype.Int8{Int64: costTicks, Valid: true}
	}

	pm, err := s.Queries.CreatePostmortem(ctx, db.CreatePostmortemParams{
		WorkspaceID:     agent.WorkspaceID,
		SourceTaskID:    task.ID,
		IssueID:         task.IssueID,
		AgentID:         task.AgentID,
		Trigger:         trigger,
		FailureReason:   failureReason,
		Summary:         util.SanitizeTextForPostgres(summary),
		RootCause:       util.SanitizeTextForPostgres(rootCause),
		Impact:          util.SanitizeTextForPostgres(impact),
		PreventiveRules: rulesJSON,
		CostUsdTicks:    costParam,
		LlmGenerated:    llmUsed,
	})
	if err != nil {
		return fmt.Errorf("insert postmortem: %w", err)
	}
	if !pm.ID.Valid {
		// ON CONFLICT DO NOTHING: another pass created it first.
		return nil
	}

	s.PublishPostmortemEvent(protocol.EventPostmortemCreated, agent.WorkspaceID, pm)
	s.notifyPostmortemReady(ctx, task, agent, pm, issueTitle)
	return nil
}

// draftPostmortem returns (summary, rootCause, impact, rules, llmUsed). It
// prefers the assist-layer LLM and falls back to the deterministic scaffold.
func (s *TaskService) draftPostmortem(ctx context.Context, trigger, issueTitle, failureReason, errMsg string, costTicks int64, attempt, maxAttempts int32, transcript string) (string, string, string, []string, bool) {
	if s.Postmortem != nil && s.Postmortem.Enabled() {
		raw, err := s.Postmortem.GenerateJSON(ctx,
			"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
			postmortemSystemPrompt,
			renderPostmortemPrompt(trigger, issueTitle, failureReason, errMsg, costTicks, attempt, maxAttempts, transcript),
			0.2,
			2048,
		)
		if err != nil {
			slog.Warn("postmortem LLM drafting failed, falling back to scaffold", "error", err)
		} else if parsed, perr := parsePostmortem(raw); perr == nil && strings.TrimSpace(parsed.Summary) != "" {
			return parsed.Summary, parsed.RootCause, parsed.Impact, parsed.Rules, true
		}
	}
	summary, rootCause, impact, rules := scaffoldPostmortem(trigger, issueTitle, failureReason, errMsg, costTicks, attempt, maxAttempts)
	return summary, rootCause, impact, rules, false
}

// loadPostmortemTranscript renders a bounded tail of the run transcript: text
// and error messages in full, short tool results, everything else dropped.
func (s *TaskService) loadPostmortemTranscript(ctx context.Context, taskID pgtype.UUID) string {
	msgs, err := s.Queries.ListTaskMessages(ctx, taskID)
	if err != nil || len(msgs) == 0 {
		return ""
	}
	if len(msgs) > postmortemMaxMessages {
		msgs = msgs[len(msgs)-postmortemMaxMessages:]
	}
	var sb strings.Builder
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content.String)
		if content == "" {
			continue
		}
		switch m.Type {
		case "text":
			sb.WriteString(content)
			sb.WriteString("\n")
		case "error":
			sb.WriteString("[error] ")
			sb.WriteString(content)
			sb.WriteString("\n")
		case "tool_result":
			if utf8.RuneCountInString(content) <= 300 {
				sb.WriteString("[")
				sb.WriteString(m.Tool.String)
				sb.WriteString("] ")
				sb.WriteString(content)
				sb.WriteString("\n")
			}
		}
	}
	out := sb.String()
	if utf8.RuneCountInString(out) > postmortemTranscriptBudget {
		runes := []rune(out)
		out = string(runes[len(runes)-postmortemTranscriptBudget:])
	}
	return out
}

// loadPostmortemUsage sums the run's provider/model usage rows. Cost is the
// provider-reported cost_usd_ticks (rows without one contribute 0 — they are
// priced client-side elsewhere and are not needed for the postmortem signal).
func (s *TaskService) loadPostmortemUsage(ctx context.Context, taskID pgtype.UUID) (costTicks, inputTokens, outputTokens int64) {
	rows, err := s.Queries.GetTaskUsage(ctx, taskID)
	if err != nil {
		return 0, 0, 0
	}
	for _, r := range rows {
		inputTokens += r.InputTokens
		outputTokens += r.OutputTokens
		if r.CostUsdTicks.Valid {
			costTicks += r.CostUsdTicks.Int64
		}
	}
	return costTicks, inputTokens, outputTokens
}

// scaffoldPostmortem is the deterministic fallback: a factual postmortem built
// from the failure data alone, used when no assist-layer LLM is configured.
func scaffoldPostmortem(trigger, issueTitle, failureReason, errMsg string, costTicks int64, attempt, maxAttempts int32) (summary, rootCause, impact string, rules []string) {
	reason := failureReason
	if reason == "" {
		reason = "unclassified"
	}
	subject := "The agent run"
	if issueTitle != "" {
		subject = fmt.Sprintf("The agent run on %q", issueTitle)
	}
	if trigger == "costly" {
		// The run SUCCEEDED — there is no failure to explain, only a bill.
		summary = fmt.Sprintf("%s completed but cost %s, over the workspace threshold.", subject, formatUsdTicks(costTicks))
		rootCause = "Not classified: the run succeeded, so the cost is what needs explaining, not a failure."
		if issueTitle != "" {
			impact = fmt.Sprintf("Issue %q was advanced, at an unusually high cost for a single run.", issueTitle)
		} else {
			impact = "The work was delivered, at an unusually high cost for a single run."
		}
		return summary, rootCause, impact, []string{
			"Check whether the run re-read the same context repeatedly before acting",
			"Narrow the task scope, or split it, so a single run carries less context",
		}
	}
	summary = fmt.Sprintf("%s failed (reason: %s, attempt %d/%d).", subject, reason, attempt, maxAttempts)
	rootCause = fmt.Sprintf("Classified failure reason: %s.", reason)
	if errMsg != "" {
		rootCause += " Last error: " + truncateRunes(errMsg, 300)
	}
	if issueTitle != "" {
		impact = fmt.Sprintf("Issue %q was not advanced by this run; its status was rolled back for a future attempt.", issueTitle)
	} else {
		impact = "The task did not complete, so none of its intended work was delivered."
	}
	return summary, rootCause, impact, scaffoldRulesForReason(failureReason)
}

// scaffoldRulesForReason maps a classified failure reason to actionable
// preventive rules. Mirrors the taskfailure taxonomy's broad buckets.
func scaffoldRulesForReason(reason string) []string {
	switch {
	case strings.HasPrefix(reason, "agent_error.context_overflow"):
		return []string{
			"Split the task into smaller sub-tasks so each fits the model context",
			"Reduce the context loaded before the run (fewer files, tighter scope)",
		}
	case strings.HasPrefix(reason, "agent_error.provider_quota_limit"):
		return []string{
			"Check the model provider quota before dispatching",
			"Route the task to a provider with remaining headroom",
		}
	case strings.HasPrefix(reason, "agent_error.provider_auth_or_access"), strings.HasPrefix(reason, "agent_error.missing_config"):
		return []string{
			"Verify the agent's credentials and configuration before re-dispatch",
		}
	case reason == "timeout":
		return []string{
			"Narrow the task scope so it completes within the run window",
			"Raise the run timeout if the work is legitimately long",
		}
	case strings.HasPrefix(reason, "agent_error.provider_network"), reason == "provider_server_error":
		return []string{
			"Retry after the provider recovers",
			"Check provider status before re-dispatching",
		}
	default:
		return []string{
			"Review the run transcript to identify where the work broke down",
		}
	}
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

// notifyPostmortemReady writes an inbox item for the human accountable for the
// failed run so the draft gets reviewed. Best-effort: a failure here is logged,
// never propagated — the postmortem itself is already stored.
func (s *TaskService) notifyPostmortemReady(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, pm db.Postmortem, issueTitle string) {
	recipient := task.AccountableUserID
	if !recipient.Valid {
		recipient = task.OriginatorUserID
	}
	if !recipient.Valid {
		recipient = agent.OwnerID
	}
	if !recipient.Valid {
		return
	}

	title := "Run failed"
	if issueTitle != "" {
		title = "Run failed: " + issueTitle
	}
	details, err := json.Marshal(map[string]any{
		"postmortem_id":  util.UUIDToString(pm.ID),
		"source_task_id": util.UUIDToString(pm.SourceTaskID),
		"agent_id":       util.UUIDToString(pm.AgentID),
	})
	if err != nil {
		details = []byte("{}")
	}
	if _, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   agent.WorkspaceID,
		RecipientType: "member",
		RecipientID:   recipient,
		Type:          "postmortem_ready",
		Severity:      "attention",
		IssueID:       pm.IssueID,
		Title:         title,
		Body:          pgtype.Text{String: pm.Summary, Valid: pm.Summary != ""},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		ActorID:       pgtype.UUID{},
		Details:       details,
	}); err != nil {
		slog.Warn("postmortem inbox notification failed",
			"postmortem_id", util.UUIDToString(pm.ID), "error", err)
	}
}

// PublishPostmortemEvent broadcasts a postmortem lifecycle change so live
// queue clients converge. Used for created (service) and resolved (handler).
func (s *TaskService) PublishPostmortemEvent(eventType string, workspaceID pgtype.UUID, pm db.Postmortem) {
	if s.Bus == nil {
		return
	}
	rules := []string{}
	if len(pm.PreventiveRules) > 0 {
		_ = json.Unmarshal(pm.PreventiveRules, &rules)
	}
	var resolvedAt string
	if pm.ResolvedAt.Valid {
		resolvedAt = pm.ResolvedAt.Time.Format(time.RFC3339Nano)
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{"postmortem": map[string]any{
			"id":               util.UUIDToString(pm.ID),
			"workspace_id":     util.UUIDToString(pm.WorkspaceID),
			"source_task_id":   util.UUIDToString(pm.SourceTaskID),
			"issue_id":         util.UUIDToString(pm.IssueID),
			"agent_id":         util.UUIDToString(pm.AgentID),
			"state":            pm.State,
			"failure_reason":   pm.FailureReason,
			"summary":          pm.Summary,
			"preventive_rules": rules,
			"llm_generated":    pm.LlmGenerated,
			"resolved_at":      resolvedAt,
			"created_at":       pm.CreatedAt.Time.Format(time.RFC3339Nano),
		}},
	})
}

// ApplyPostmortemRules copies an approved postmortem's preventive rules into
// the failed agent's memory so its next run is briefed on them. Rules already
// present (same normalized text) are skipped. Returns how many were stored.
// A postmortem without an agent, or without rules, is a no-op.
func (s *TaskService) ApplyPostmortemRules(ctx context.Context, pm db.Postmortem) (int, error) {
	if !pm.AgentID.Valid || len(pm.PreventiveRules) == 0 {
		return 0, nil
	}
	var rules []string
	if err := json.Unmarshal(pm.PreventiveRules, &rules); err != nil {
		return 0, fmt.Errorf("decode preventive rules: %w", err)
	}
	if len(rules) == 0 {
		return 0, nil
	}
	agent, err := s.Queries.GetAgent(ctx, pm.AgentID)
	if err != nil {
		return 0, fmt.Errorf("load postmortem agent: %w", err)
	}
	existing, err := s.Queries.ListAgentMemories(ctx, db.ListAgentMemoriesParams{AgentID: agent.ID, WorkspaceID: agent.WorkspaceID})
	if err != nil {
		return 0, fmt.Errorf("list agent memories: %w", err)
	}
	known := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		known[normalizeAgentMemoryFact(m.Content)] = struct{}{}
	}
	inserted := 0
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		key := normalizeAgentMemoryFact(rule)
		if key == "" || utf8.RuneCountInString(rule) > agentMemoryFactMaxRunes {
			continue
		}
		if _, dup := known[key]; dup {
			continue
		}
		memory, err := s.Queries.CreateAgentMemory(ctx, db.CreateAgentMemoryParams{
			WorkspaceID:  agent.WorkspaceID,
			AgentID:      agent.ID,
			Content:      util.SanitizeTextForPostgres(rule),
			Source:       "postmortem",
			SourceTaskID: pm.SourceTaskID,
		})
		if err != nil {
			return inserted, fmt.Errorf("store postmortem rule as agent memory: %w", err)
		}
		known[key] = struct{}{}
		inserted++
		s.publishAgentMemoryEvent(protocol.EventAgentMemoryCreated, agent, memory)
	}
	return inserted, nil
}

// formatUsdTicks renders a cost_usd_ticks value (1e-10 USD) as a dollar amount
// for the postmortem text. Four decimals: agent runs are routinely priced in
// fractions of a cent, and rounding one to "$0.00" would make the whole
// costly-run postmortem read as nonsense.
func formatUsdTicks(ticks int64) string {
	return fmt.Sprintf("$%.4f", float64(ticks)*costTicksPerUSD)
}
