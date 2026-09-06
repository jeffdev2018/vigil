package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/modelkey"
)

// Validated routing (JEF-275).
//
// An agent can be pointed at nothing runnable in several ways, and the
// difference between them is the whole point: an OFFLINE machine comes back on
// its own, so a task queued for it is right to wait. A runtime in another
// workspace, a deleted runtime, an archived agent — those never resolve, and a
// task queued against them sits in the queue until a human notices.
//
// ValidateRouting answers, before anything is enqueued, which of the two this
// is. Fatal problems refuse the trigger and alert the accountable human;
// warnings ride along in the run's routing trace so the wait is explained
// rather than silent.

// Routing problem codes.
const (
	RoutingProblemAgentArchived      = "agent_archived"
	RoutingProblemNoRuntime          = "no_runtime"
	RoutingProblemRuntimeNotInWs     = "runtime_not_in_workspace"
	RoutingProblemRuntimeOfflineOnly = "runtime_offline_no_fallback"
	RoutingProblemModelKeyMissing    = "model_key_missing"
)

// RoutingProblem is one reason a trigger for this agent may not run.
type RoutingProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Fatal marks a problem no amount of waiting resolves. A trigger carrying
	// one must be refused, not queued.
	Fatal bool `json:"fatal"`
}

// FatalRoutingProblem returns the first fatal problem, or nil.
func FatalRoutingProblem(problems []RoutingProblem) *RoutingProblem {
	for i := range problems {
		if problems[i].Fatal {
			return &problems[i]
		}
	}
	return nil
}

// RoutingWarnings returns the non-fatal problem codes, which is what the run's
// routing trace records.
func RoutingWarnings(problems []RoutingProblem) []RoutingProblem {
	out := make([]RoutingProblem, 0, len(problems))
	for _, p := range problems {
		if !p.Fatal {
			out = append(out, p)
		}
	}
	return out
}

// ValidateRouting reports every reason a new run for this agent might not be
// claimed. It reads only what the enqueue path already reads — the agent row,
// its runtime, its pool and the workspace's model keys — so it is cheap enough
// to run on every enqueue.
//
// It stops at the first fatal problem: an archived agent's runtime tells you
// nothing useful, and listing consequences of a cause the user must fix first
// only buries the cause.
func (s *TaskService) ValidateRouting(ctx context.Context, agent db.Agent, wsID pgtype.UUID) []RoutingProblem {
	if agent.ArchivedAt.Valid {
		return []RoutingProblem{{
			Code:    RoutingProblemAgentArchived,
			Message: fmt.Sprintf("%s is archived: it cannot take new work until it is restored.", agentLabel(agent)),
			Fatal:   true,
		}}
	}
	if !agent.RuntimeID.Valid {
		return []RoutingProblem{{
			Code:    RoutingProblemNoRuntime,
			Message: fmt.Sprintf("%s is not bound to a runtime: nothing would ever claim its work.", agentLabel(agent)),
			Fatal:   true,
		}}
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return []RoutingProblem{{
			Code:    RoutingProblemNoRuntime,
			Message: fmt.Sprintf("%s is bound to a runtime that no longer exists.", agentLabel(agent)),
			Fatal:   true,
		}}
	}
	if wsID.Valid && rt.WorkspaceID != wsID {
		return []RoutingProblem{{
			Code:    RoutingProblemRuntimeNotInWs,
			Message: fmt.Sprintf("%s is bound to a runtime of another workspace: no daemon in this workspace can claim its work.", agentLabel(agent)),
			Fatal:   true,
		}}
	}

	problems := make([]RoutingProblem, 0, 2)
	if rt.Status != "online" {
		// K28: an offline runtime with an online pool member is not a problem
		// at all — the enqueue path moves the task there.
		if target, _ := s.poolFailoverTarget(ctx, agent, db.AgentTaskQueue{RuntimeID: agent.RuntimeID}, "routing_check"); !target.OK {
			problems = append(problems, RoutingProblem{
				Code:    RoutingProblemRuntimeOfflineOnly,
				Message: fmt.Sprintf("%s's runtime is %s and no pool member is online: the run waits until it comes back.", agentLabel(agent), rt.Status),
			})
		}
	}
	if p, ok := s.modelKeyProblem(ctx, wsID, rt.Provider); ok {
		problems = append(problems, p)
	}
	return problems
}

// modelKeyProblem reports a workspace that holds BYOK keys for the runtime's
// vendor but has none left active. A workspace with no key at all for that
// vendor is not a problem: the runtime uses its own credentials.
func (s *TaskService) modelKeyProblem(ctx context.Context, wsID pgtype.UUID, runtimeProvider string) (RoutingProblem, bool) {
	vendorID := modelkey.VendorForRuntime(runtimeProvider)
	if vendorID == "" || !wsID.Valid {
		return RoutingProblem{}, false
	}
	keys, err := s.Queries.ListModelKeys(ctx, wsID)
	if err != nil {
		return RoutingProblem{}, false
	}
	found := false
	for _, k := range keys {
		if k.Provider != vendorID {
			continue
		}
		if k.Active {
			return RoutingProblem{}, false
		}
		found = true
	}
	if !found {
		return RoutingProblem{}, false
	}
	label := vendorID
	if vendor, ok := modelkey.VendorByID(vendorID); ok {
		label = vendor.Label
	}
	return RoutingProblem{
		Code:    RoutingProblemModelKeyMissing,
		Message: fmt.Sprintf("Every %s key in this workspace has been retired: runs fall back to the runtime's own credentials.", label),
	}, true
}

// RoutingInvalidReason is the failure reason of a trigger refused because the
// agent is pointed at something no daemon in this workspace can claim.
const RoutingInvalidReason = "routing_invalid"

// reportRoutingBlocked builds the problem for one of the two pre-existing
// early refusals and fires the alert. The refusals predate ValidateRouting and
// return before it runs, so they report their own code rather than re-deriving
// it from an agent row that is already known bad.
func (s *TaskService) reportRoutingBlocked(ctx context.Context, agent db.Agent, issue db.Issue, code string) {
	problem := RoutingProblem{Code: code, Fatal: true}
	switch code {
	case RoutingProblemAgentArchived:
		problem.Message = fmt.Sprintf("%s is archived: it cannot take new work until it is restored.", agentLabel(agent))
	case RoutingProblemNoRuntime:
		problem.Message = fmt.Sprintf("%s is not bound to a runtime: nothing would ever claim its work.", agentLabel(agent))
	default:
		problem.Message = fmt.Sprintf("%s cannot be routed.", agentLabel(agent))
	}
	s.fireRoutingBlocked(ctx, agent, issue, problem)
}

func (s *TaskService) fireRoutingBlocked(ctx context.Context, agent db.Agent, issue db.Issue, problem RoutingProblem) {
	if s.OnRoutingBlocked != nil {
		s.OnRoutingBlocked(ctx, agent, issue, problem)
	}
}

func agentLabel(agent db.Agent) string {
	if agent.Name == "" {
		return "This agent"
	}
	return agent.Name
}

// routingTraceWithWarnings merges the routing warnings into the run's routing
// trace. The trace already carries the router's own decision for an auto-mode
// agent (JEF-237); for a fixed-mode agent it is empty and the warnings become
// the whole trace, which is why they need no column of their own.
func routingTraceWithWarnings(trace []byte, warnings []RoutingProblem) []byte {
	if len(warnings) == 0 {
		return trace
	}
	out := map[string]any{}
	if len(trace) > 0 {
		if err := json.Unmarshal(trace, &out); err != nil {
			out = map[string]any{}
		}
	}
	out["warnings"] = warnings
	raw, err := json.Marshal(out)
	if err != nil {
		return trace
	}
	return raw
}
