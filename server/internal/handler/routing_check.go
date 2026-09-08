package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Validated routing (JEF-275): the read endpoint, and the alert filed when a
// trigger is refused because the agent is pointed at nothing runnable.

const (
	// InboxTypeRoutingAlert tells the accountable humans that a trigger was
	// refused. Without it the refusal is a server log line nobody reads and
	// the issue simply never moves.
	InboxTypeRoutingAlert = "routing_alert"
	// AuditRoutingBlocked records the refusal itself.
	AuditRoutingBlocked = "routing.blocked"
)

// RoutingCheckResponse is GET /api/agents/{id}/routing-check.
type RoutingCheckResponse struct {
	AgentID  string                   `json:"agent_id"`
	OK       bool                     `json:"ok"`
	Fatal    bool                     `json:"fatal"`
	Problems []service.RoutingProblem `json:"problems"`
}

// GetAgentRoutingCheck: GET /api/agents/{id}/routing-check — would a trigger
// for this agent actually run, and if not, why.
func (h *Handler) GetAgentRoutingCheck(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	problems := h.TaskService.ValidateRouting(r.Context(), agent, agent.WorkspaceID)
	writeJSON(w, http.StatusOK, RoutingCheckResponse{
		AgentID:  uuidToString(agent.ID),
		OK:       len(problems) == 0,
		Fatal:    service.FatalRoutingProblem(problems) != nil,
		Problems: problems,
	})
}

// onRoutingBlocked is TaskService.OnRoutingBlocked: a refused trigger becomes
// one inbox item per accountable human, plus an audit entry.
//
// Accountable means the agent's owner and the workspace's managers: the owner
// is who bound the agent to the runtime that no longer works, the managers are
// who can archive or rebind it. The set is deduplicated, so an owner who is
// also a manager gets one item.
func (h *Handler) onRoutingBlocked(ctx context.Context, agent db.Agent, issue db.Issue, problem service.RoutingProblem) {
	wsID := issue.WorkspaceID
	if !wsID.Valid {
		wsID = agent.WorkspaceID
	}
	// Data residency (K46) files its own audit action and its own inbox type:
	// the reader's next step is to declare a runtime or relax the policy, not
	// to rebind the agent, and a shared type would bury that distinction.
	action, inboxType, title := AuditRoutingBlocked, InboxTypeRoutingAlert,
		"A trigger was not queued: "+agentDisplayName(agent)+" cannot be routed"
	if problem.Code == service.RoutingProblemResidencyNoCompliantRuntime {
		action, inboxType, title = AuditResidencyDispatchBlocked, InboxTypeResidencyBlocked,
			"A run was blocked by the data residency policy: "+agentDisplayName(agent)
	}
	auditDetails := map[string]any{
		"code": problem.Code, "issue_id": uuidToString(issue.ID), "agent_id": uuidToString(agent.ID),
	}
	for k, v := range problem.Details {
		auditDetails[k] = v
	}
	h.audit(ctx, wsID, "system", "", action, "agent", agent.ID, auditDetails, nil)

	seen := map[string]bool{}
	targets := make([]pgtype.UUID, 0, 4)
	if agent.OwnerID.Valid {
		seen[uuidToString(agent.OwnerID)] = true
		targets = append(targets, agent.OwnerID)
	}
	managers, err := h.Queries.ListWorkspaceManagerUserIDs(ctx, wsID)
	if err != nil {
		slog.Warn("routing alert: list managers failed", "workspace_id", uuidToString(wsID), "error", err)
	}
	for _, m := range managers {
		if seen[uuidToString(m)] {
			continue
		}
		seen[uuidToString(m)] = true
		targets = append(targets, m)
	}
	// One alert per recipient, per broken agent, per problem, per day. An
	// archived agent that still owns an issue is re-triggered by every comment
	// on it, and an alert per trigger would be a reason to mute the inbox
	// rather than a reason to fix the agent. The grain includes the agent and
	// the code so a second broken agent is still reported the same day.
	//
	// It rides the `day` field CountInboxItemsForDay already matches on, which
	// is why no new query is needed.
	day := routingAlertDay(agent.ID, problem.Code, time.Now().UTC())
	details, _ := json.Marshal(map[string]any{
		"code": problem.Code, "agent_id": uuidToString(agent.ID), "issue_id": uuidToString(issue.ID),
		"failure_reason": service.RoutingInvalidReason, "day": day,
	})
	for _, userID := range targets {
		already, err := h.Queries.CountInboxItemsForDay(ctx, db.CountInboxItemsForDayParams{
			WorkspaceID: wsID, RecipientID: userID, Type: inboxType, Day: day,
		})
		if err != nil {
			slog.Warn("routing alert: dedup check failed", "error", err)
		}
		if already > 0 {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: "member", RecipientID: userID,
			Type: inboxType, Severity: "action_required", IssueID: issue.ID,
			Title: truncate(title, 120), Body: pgtype.Text{String: truncate(problem.Message, 1000), Valid: true}, Details: details,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("routing alert: inbox failed", "error", err)
			}
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}

// routingAlertDay is the dedup grain: this agent, this problem, this day.
func routingAlertDay(agentID pgtype.UUID, code string, now time.Time) string {
	return uuidToString(agentID) + ":" + code + ":" + now.Format("2006-01-02")
}

func agentDisplayName(agent db.Agent) string {
	if agent.Name == "" {
		return "the agent"
	}
	return agent.Name
}
