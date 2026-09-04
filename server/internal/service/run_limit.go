package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run limits (K03): caps on one run while it runs. Period budgets (F21)
// stay in charge of the spend of many runs over a period; this stops a
// single runaway run. Evaluated whenever the run reports usage or messages
// and on every runtime sweeper tick (for duration). Warn once per gate at
// warn_bps, stop once at 100% when enforced (failed, reason
// budget_exceeded — never cancelled, which is a human act), only record
// when observed. The most restrictive cap per gate wins across scopes.

const ReasonBudgetExceeded = "budget_exceeded"

var RunLimitGates = []string{"cost", "duration", "turns", "tool_calls"}

// RunLimitGate is one effective cap: the smallest limit among the policies that apply.
type RunLimitGate struct {
	Gate     string  `json:"gate"`
	Limit    int64   `json:"limit"`
	Observed int64   `json:"observed"`
	Ratio    float64 `json:"ratio"`
	Action   string  `json:"action"`
	WarnBps  int32   `json:"warn_bps"`
	PolicyID string  `json:"policy_id"`
	Scope    string  `json:"scope_type"`
	Level    string  `json:"level"` // "", warn, exceeded, stopped
}

// RunUsage is what a run consumed so far.
type RunUsage struct {
	CostUsdTicks    int64 `json:"cost_usd_ticks"`
	DurationSeconds int64 `json:"duration_seconds"`
	Turns           int64 `json:"turns"`
	ToolCalls       int64 `json:"tool_calls"`
}

func (u RunUsage) observed(gate string) int64 {
	switch gate {
	case "cost":
		return u.CostUsdTicks
	case "duration":
		return u.DurationSeconds
	case "turns":
		return u.Turns
	}
	return u.ToolCalls
}

func policyLimit(p db.RunLimitPolicy, gate string) (int64, bool) {
	switch gate {
	case "cost":
		return p.MaxCostUsdTicks.Int64, p.MaxCostUsdTicks.Valid
	case "duration":
		return int64(p.MaxDurationSeconds.Int32), p.MaxDurationSeconds.Valid
	case "turns":
		return int64(p.MaxTurns.Int32), p.MaxTurns.Valid
	}
	return int64(p.MaxToolCalls.Int32), p.MaxToolCalls.Valid
}

// EffectiveRunLimits picks, per gate, the smallest limit among the
// policies; a tie goes to enforce over observe.
func EffectiveRunLimits(policies []db.RunLimitPolicy) []RunLimitGate {
	var out []RunLimitGate
	for _, gate := range RunLimitGates {
		var best *RunLimitGate
		for _, p := range policies {
			limit, ok := policyLimit(p, gate)
			if !ok {
				continue
			}
			if best == nil || limit < best.Limit || (limit == best.Limit && p.Action == "enforce" && best.Action != "enforce") {
				best = &RunLimitGate{Gate: gate, Limit: limit, Action: p.Action, WarnBps: p.WarnBps, PolicyID: util.UUIDToString(p.ID), Scope: p.ScopeType}
			}
		}
		if best != nil {
			out = append(out, *best)
		}
	}
	return out
}

// runUsage reads what the run consumed: usage rows, message counts, clock.
func (s *TaskService) runUsage(ctx context.Context, task db.AgentTaskQueue, now time.Time) RunUsage {
	var u RunUsage
	u.CostUsdTicks, _ = s.Queries.SumTaskCostTicks(ctx, task.ID)
	if rows, err := s.Queries.CountTaskMessagesByType(ctx, task.ID); err == nil {
		for _, r := range rows {
			switch r.Type {
			case "text":
				u.Turns += r.N
			case "tool_use", "tool-use":
				u.ToolCalls += r.N
			}
		}
	}
	if task.StartedAt.Valid {
		u.DurationSeconds = int64(now.Sub(task.StartedAt.Time).Seconds())
	}
	return u
}

// RunLimitStatus is the run's caps, usage and past events, for the API.
type RunLimitStatus struct {
	Usage  RunUsage           `json:"usage"`
	Gates  []RunLimitGate     `json:"gates"`
	Events []db.RunLimitEvent `json:"-"`
}

func (s *TaskService) runLimitPolicies(ctx context.Context, task db.AgentTaskQueue) ([]db.RunLimitPolicy, pgtype.UUID, error) {
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return nil, pgtype.UUID{}, err
	}
	var projectID pgtype.UUID
	if task.IssueID.Valid {
		if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
			projectID = issue.ProjectID
		}
	}
	policies, err := s.Queries.ListRunLimitPoliciesForRun(ctx, db.ListRunLimitPoliciesForRunParams{WorkspaceID: agent.WorkspaceID, ProjectID: projectID, ScopeID: agent.ID})
	return policies, agent.WorkspaceID, err
}

// RunLimitStatusFor answers GET /api/tasks/{id}/budget-status.
func (s *TaskService) RunLimitStatusFor(ctx context.Context, task db.AgentTaskQueue) (RunLimitStatus, error) {
	policies, _, err := s.runLimitPolicies(ctx, task)
	if err != nil {
		return RunLimitStatus{}, err
	}
	usage := s.runUsage(ctx, task, time.Now())
	gates := EffectiveRunLimits(policies)
	events, _ := s.Queries.ListRunLimitEvents(ctx, task.ID)
	for i := range gates {
		gates[i].Observed = usage.observed(gates[i].Gate)
		gates[i].Ratio = float64(gates[i].Observed) / float64(gates[i].Limit)
		for _, e := range events {
			if e.Gate == gates[i].Gate {
				gates[i].Level = e.Level
			}
		}
	}
	if gates == nil {
		gates = []RunLimitGate{}
	}
	return RunLimitStatus{Usage: usage, Gates: gates, Events: events}, nil
}

// EvaluateRunLimits (K03) is the enforcement point. It returns true when
// the run was stopped. Safe to call often: events dedupe warnings and
// stops, and a run that is no longer running is left alone.
func (s *TaskService) EvaluateRunLimits(ctx context.Context, task db.AgentTaskQueue) bool {
	if task.Status != "running" {
		return false
	}
	policies, wsID, err := s.runLimitPolicies(ctx, task)
	if err != nil || len(policies) == 0 {
		return false
	}
	gates := EffectiveRunLimits(policies)
	if len(gates) == 0 {
		return false
	}
	usage := s.runUsage(ctx, task, time.Now())
	events, _ := s.Queries.ListRunLimitEvents(ctx, task.ID)
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Gate+":"+e.Level] = true
	}
	for _, g := range gates {
		observed := usage.observed(g.Gate)
		if g.Limit <= 0 {
			continue
		}
		bps := observed * 10_000 / g.Limit
		record := func(level string) {
			if seen[g.Gate+":"+level] {
				return
			}
			seen[g.Gate+":"+level] = true
			if _, err := s.Queries.CreateRunLimitEvent(ctx, db.CreateRunLimitEventParams{
				ID: dbid.NewV7(), WorkspaceID: wsID, TaskID: task.ID, PolicyID: mustUUID(g.PolicyID), Gate: g.Gate, Level: level, Observed: observed, LimitValue: g.Limit,
			}); err != nil {
				slog.Warn("run limits: record event failed", "task_id", util.UUIDToString(task.ID), "error", err)
			}
			s.notifyRunLimit(ctx, wsID, task, g, level, observed)
		}
		if bps >= 10_000 {
			if g.Action == "enforce" {
				if seen[g.Gate+":stopped"] {
					continue
				}
				record("stopped")
				msg := fmt.Sprintf("Run stopped by its %s limit: %s of %s (%s policy)", g.Gate, formatGate(g.Gate, observed), formatGate(g.Gate, g.Limit), g.Scope)
				if _, err := s.FailTask(ctx, task.ID, msg, "", "", "", ReasonBudgetExceeded, false, "", ""); err != nil {
					slog.Warn("run limits: stop failed", "task_id", util.UUIDToString(task.ID), "error", err)
					continue
				}
				slog.Info("run limits: run stopped", "task_id", util.UUIDToString(task.ID), "gate", g.Gate, "observed", observed, "limit", g.Limit)
				return true
			}
			record("exceeded")
			continue
		}
		if int32(bps) >= g.WarnBps && g.WarnBps > 0 {
			record("warn")
		}
	}
	return false
}

func mustUUID(s string) pgtype.UUID {
	u, _ := util.ParseUUID(s)
	return u
}

// formatGate renders an observed/limit value for humans.
func formatGate(gate string, v int64) string {
	switch gate {
	case "cost":
		return fmt.Sprintf("$%.2f", float64(v)/1e10)
	case "duration":
		return (time.Duration(v) * time.Second).String()
	}
	return fmt.Sprintf("%d", v)
}

// notifyRunLimit files one inbox item per manager (best effort).
func (s *TaskService) notifyRunLimit(ctx context.Context, wsID pgtype.UUID, task db.AgentTaskQueue, g RunLimitGate, level string, observed int64) {
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, s.Queries, wsID)
	if err != nil {
		return
	}
	title := fmt.Sprintf("Run at %s of its %s limit", formatGate(g.Gate, observed), g.Gate)
	severity := "attention"
	switch level {
	case "stopped":
		title, severity = fmt.Sprintf("Run stopped: %s limit reached (%s)", g.Gate, formatGate(g.Gate, g.Limit)), "action_required"
	case "exceeded":
		title = fmt.Sprintf("Run over its %s limit (%s), observe only", g.Gate, formatGate(g.Gate, g.Limit))
	}
	details, _ := json.Marshal(map[string]any{"task_id": util.UUIDToString(task.ID), "gate": g.Gate, "level": level, "observed": observed, "limit": g.Limit, "policy_id": g.PolicyID})
	for _, rcpt := range recipients {
		item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: "run_limit_" + level, Severity: severity,
			IssueID: task.IssueID, Title: title, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		s.publishRunLimitInbox(item)
	}
}

// SweepRunLimits is the runtime sweeper stage: the clock only moves here.
func (s *TaskService) SweepRunLimits(ctx context.Context, maxPerTick int32) int {
	tasks, err := s.Queries.ListRunningTasksForLimits(ctx, maxPerTick)
	if err != nil {
		return 0
	}
	stopped := 0
	for _, t := range tasks {
		if s.EvaluateRunLimits(ctx, t) {
			stopped++
		}
	}
	return stopped
}

// publishRunLimitInbox mirrors BudgetService.publishBudgetInbox: the item
// reaches the recipient's inbox live.
func (s *TaskService) publishRunLimitInbox(item db.InboxItem) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"item_id": util.UUIDToString(item.ID), "recipient_type": item.RecipientType, "recipient_id": util.UUIDToString(item.RecipientID)},
	})
}
