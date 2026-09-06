package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Bounded workflows (JEF-275).
//
// A single run has been bounded since K03: max cost, duration, turns and tool
// calls per run. The WORKFLOW had no bound at all. A review that requests
// changes queues a revision, whose delivery is reviewed again, which can
// request changes again; a contest answer can be re-critiqued; a failing run
// retries, and its retry fails and retries. Each leg is individually within
// its per-run limits and the workflow still spends without end.
//
// This is the workflow-level ceiling: how many legs one workflow may run, and
// how much the whole workflow may cost. Producers ask before enqueueing, so a
// refused leg is never created — the workflow simply stops growing and the
// refusal is on the record.

const (
	// AuditWorkflowLegRefused is written whenever a leg is refused.
	AuditWorkflowLegRefused = "workflow.leg_refused"

	workflowDefaultMaxLegs = 8
	workflowMinMaxLegs     = 1
	workflowMaxMaxLegs     = 50

	// Refusal reasons, in the vocabulary the audit row records.
	workflowRefusedMaxLegs = "max_legs"
	workflowRefusedMaxCost = "max_cost_usd_ticks"
)

// ReasonWorkflowLimit is the distinct failure reason of a run whose retry was
// refused because its workflow reached the ceiling. It reads differently from
// an exhausted attempt budget: the run itself had attempts left, the workflow
// around it did not.
const ReasonWorkflowLimit = "workflow_limit"

// WorkflowLimits lives under workspace.settings.workflow_limits.
type WorkflowLimits struct {
	// MaxLegs is how many runs one workflow may contain, the primary leg
	// included. Always between workflowMinMaxLegs and workflowMaxMaxLegs.
	MaxLegs int `json:"max_legs"`
	// MaxCostUsdTicks caps what the whole workflow may spend, in 1e-10 USD.
	// Zero — the default — means the workflow is bounded by leg count only.
	MaxCostUsdTicks int64 `json:"max_cost_usd_ticks"`
}

// WorkflowLimitsFromSettings reads the limits off a workspace settings blob.
// Anything missing, unparseable or out of range falls back to the default, so
// a workspace that never configured this still gets a ceiling.
func WorkflowLimitsFromSettings(settings []byte) WorkflowLimits {
	out := WorkflowLimits{MaxLegs: workflowDefaultMaxLegs}
	var s struct {
		WorkflowLimits *WorkflowLimits `json:"workflow_limits"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.WorkflowLimits == nil {
		return out
	}
	if s.WorkflowLimits.MaxLegs >= workflowMinMaxLegs && s.WorkflowLimits.MaxLegs <= workflowMaxMaxLegs {
		out.MaxLegs = s.WorkflowLimits.MaxLegs
	}
	if s.WorkflowLimits.MaxCostUsdTicks > 0 {
		out.MaxCostUsdTicks = s.WorkflowLimits.MaxCostUsdTicks
	}
	return out
}

// ValidWorkflowLimits reports whether a submitted setting is in range.
func ValidWorkflowLimits(l WorkflowLimits) bool {
	return l.MaxLegs >= workflowMinMaxLegs && l.MaxLegs <= workflowMaxMaxLegs && l.MaxCostUsdTicks >= 0
}

// WorkflowLimitsRange is what the settings endpoint tells a client about the
// accepted values, so the form does not hardcode them.
func WorkflowLimitsRange() (minLegs, maxLegs int) {
	return workflowMinMaxLegs, workflowMaxMaxLegs
}

// WorkflowAllowsLeg answers "may this workflow grow by one more run?" for a
// producer about to enqueue a secondary leg. parent is the run the new leg
// hangs off; role is the leg role it would be stamped with.
//
// A zero-value parent has no workflow to bound — the leg would be its own root
// — so it is always allowed. A refusal writes the workflow.leg_refused audit
// entry here rather than at each producer: the counts that justify the refusal
// are already in hand, and one writer keeps every producer's record identical.
//
// Fails OPEN: a workspace or leg lookup that errors allows the leg. A ceiling
// is a safety net, not an admission gate, and a transient DB error must not
// silently stop a review loop that was working.
func (s *TaskService) WorkflowAllowsLeg(ctx context.Context, parent db.AgentTaskQueue, role string) (bool, string) {
	root := WorkflowRoot(parent)
	if !root.Valid {
		return true, ""
	}
	wsIDStr := s.ResolveTaskWorkspaceID(ctx, parent)
	if wsIDStr == "" {
		return true, ""
	}
	wsID, err := util.ParseUUID(wsIDStr)
	if err != nil {
		return true, ""
	}
	ws, err := s.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return true, ""
	}
	limits := WorkflowLimitsFromSettings(ws.Settings)
	legs, err := s.Queries.ListWorkflowLegs(ctx, root)
	if err != nil {
		slog.Warn("workflow limits: list legs failed", "root_task_id", util.UUIDToString(root), "error", err)
		return true, ""
	}
	var cost int64
	for _, leg := range legs {
		cost += leg.CostUsdTicks
	}
	reason := ""
	switch {
	case len(legs) >= limits.MaxLegs:
		reason = workflowRefusedMaxLegs
	case limits.MaxCostUsdTicks > 0 && cost >= limits.MaxCostUsdTicks:
		reason = workflowRefusedMaxCost
	default:
		return true, ""
	}
	s.recordLegRefused(ctx, wsID, root, role, reason, len(legs), cost, limits)
	return false, reason
}

// recordLegRefused logs and audits one refusal.
func (s *TaskService) recordLegRefused(ctx context.Context, wsID, root pgtype.UUID, role, reason string, legs int, cost int64, limits WorkflowLimits) {
	slog.Warn("workflow limits: leg refused",
		"root_task_id", util.UUIDToString(root), "role", role, "reason", reason,
		"legs", legs, "cost_usd_ticks", cost, "max_legs", limits.MaxLegs, "max_cost_usd_ticks", limits.MaxCostUsdTicks)
	details, err := json.Marshal(map[string]any{
		"root_task_id": util.UUIDToString(root), "role": role, "reason": reason,
		"legs": legs, "cost": cost, "max_legs": limits.MaxLegs, "max_cost_usd_ticks": limits.MaxCostUsdTicks,
	})
	if err != nil {
		details = []byte("{}")
	}
	if _, err := s.Queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		WorkspaceID: wsID, ActorType: "system", Action: AuditWorkflowLegRefused,
		EntityType: "task", EntityID: root, Details: details,
	}); err != nil {
		slog.Warn("workflow limits: audit write failed", "root_task_id", util.UUIDToString(root), "error", err)
	}
}

// WorkflowLegRefusedNote is the one-line explanation a producer logs or leaves
// on the record when its leg was refused.
func WorkflowLegRefusedNote(role, reason string) string {
	return fmt.Sprintf("the %s leg was not queued: the workflow reached its %s limit", role, reason)
}
