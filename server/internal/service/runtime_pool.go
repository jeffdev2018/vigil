package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Runtime pools (K28). A pool is an ordered list of interchangeable runtimes
// plus an explicit degraded last resort. A task moves to the next online
// runtime of its agent's pool when its runtime is offline (at enqueue or
// while waiting) or when a run fails for an infrastructure reason. Each
// runtime is tried once per task; every move is written to the task.

// ReasonRuntimePoolExhausted is the distinct failure of a run whose whole
// pool, degraded runtime included, was unavailable.
const ReasonRuntimePoolExhausted = "runtime_pool_exhausted"

// failoverReasons are the infrastructure failures a pool answers. An
// application failure (the agent's own error) never moves runtime.
var failoverReasons = map[string]bool{
	string(taskfailure.ReasonRuntimeOffline):                   true,
	string(taskfailure.ReasonRuntimeRecovery):                  true,
	string(taskfailure.ReasonRuntimeReconnectTimeout):          true,
	string(taskfailure.ReasonRuntimeCLITimeout):                true,
	string(taskfailure.ReasonEnvironmentPrepareFailed):         true,
	string(taskfailure.ReasonAgentProviderAuthOrAccess):        true,
	string(taskfailure.ReasonAgentProviderQuotaLimit):          true,
	string(taskfailure.ReasonAgentProviderCapacityOrRateLimit): true,
	string(taskfailure.ReasonAgentProviderServerError):         true,
	string(taskfailure.ReasonAgentProviderNetwork):             true,
}

// FailoverEntry is one move, as stored in agent_task_queue.failover_history.
type FailoverEntry struct {
	From     string `json:"from_runtime_id"`
	To       string `json:"to_runtime_id"`
	Reason   string `json:"reason"`
	Degraded bool   `json:"degraded"`
	At       string `json:"at"`
}

// FailoverTarget is where a task moves next, when the pool has somewhere.
type FailoverTarget struct {
	OK        bool
	RuntimeID pgtype.UUID
	Degraded  bool
	History   []byte
}

func decodeFailoverHistory(raw []byte) []FailoverEntry {
	var out []FailoverEntry
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// TaskDegraded reports whether the task's last move landed on the degraded runtime.
func TaskDegraded(raw []byte) bool {
	h := decodeFailoverHistory(raw)
	return len(h) > 0 && h[len(h)-1].Degraded
}

// poolFailoverTarget picks the next online runtime of the agent's pool the
// task has not tried yet. hasPool is false when the agent has no pool at
// all, so callers can tell "nothing to do" from "pool exhausted".
func (s *TaskService) poolFailoverTarget(ctx context.Context, agent db.Agent, task db.AgentTaskQueue, reason string) (target FailoverTarget, hasPool bool) {
	if !agent.RuntimePoolID.Valid {
		return FailoverTarget{}, false
	}
	pool, err := s.Queries.GetRuntimePool(ctx, agent.RuntimePoolID)
	if err != nil {
		slog.Warn("runtime pool: load failed", "agent_id", util.UUIDToString(agent.ID), "error", err)
		return FailoverTarget{}, false
	}
	// Checkpoints (K20): a run whose work lives in a local worktree can only
	// resume on the daemon that holds it; another runtime would start blind.
	if task.WorkDir.Valid && task.WorkDir.String != "" && task.RuntimeID.Valid {
		if rt, err := s.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: task.RuntimeID, WorkspaceID: pool.WorkspaceID}); err == nil && rt.RuntimeMode == "local" {
			slog.Info("runtime pool: local worktree run stays on its daemon", "task_id", util.UUIDToString(task.ID))
			return FailoverTarget{}, false
		}
	}
	history := decodeFailoverHistory(task.FailoverHistory)
	tried := map[string]bool{util.UUIDToString(task.RuntimeID): true}
	for _, h := range history {
		tried[h.From], tried[h.To] = true, true
	}
	var ids []string
	_ = json.Unmarshal(pool.RuntimeIds, &ids)
	degraded := ""
	if pool.DegradedRuntimeID.Valid {
		degraded = util.UUIDToString(pool.DegradedRuntimeID)
		ids = append(ids, degraded)
	}
	for _, id := range ids {
		if tried[id] {
			continue
		}
		tried[id] = true
		rtID, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		rt, err := s.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: rtID, WorkspaceID: pool.WorkspaceID})
		if err != nil || rt.Status != "online" {
			continue
		}
		entry := FailoverEntry{From: util.UUIDToString(task.RuntimeID), To: id, Reason: reason, Degraded: id == degraded, At: time.Now().UTC().Format(time.RFC3339)}
		raw, _ := json.Marshal(append(history, entry))
		return FailoverTarget{OK: true, RuntimeID: rt.ID, Degraded: entry.Degraded, History: raw}, true
	}
	return FailoverTarget{}, true
}

// failoverForFailedTask answers a failed run: a target when the reason is an
// infrastructure one and the pool has an online runtime left. exhausted is
// true when the agent has a pool, the reason qualified, and nothing is left.
func (s *TaskService) failoverForFailedTask(ctx context.Context, task db.AgentTaskQueue, reason string) (target FailoverTarget, exhausted bool) {
	if !failoverReasons[reason] || task.AutopilotRunID.Valid || (!task.IssueID.Valid && !task.ChatSessionID.Valid) {
		return FailoverTarget{}, false
	}
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return FailoverTarget{}, false
	}
	target, hasPool := s.poolFailoverTarget(ctx, agent, task, reason)
	if target.OK {
		slog.Info("runtime pool: failing over", "task_id", util.UUIDToString(task.ID), "reason", reason, "to_runtime_id", util.UUIDToString(target.RuntimeID), "degraded", target.Degraded)
	}
	return target, hasPool && !target.OK
}

// enqueueRuntimeForAgent (K28) answers "where does a new task go": the
// agent's runtime when it is online (or the agent has no pool), else the
// first online runtime of the pool. history is nil when nothing moved.
func (s *TaskService) enqueueRuntimeForAgent(ctx context.Context, agent db.Agent) (runtimeID pgtype.UUID, history []byte) {
	if !agent.RuntimePoolID.Valid || !agent.RuntimeID.Valid {
		return agent.RuntimeID, nil
	}
	rt, err := s.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: agent.RuntimeID, WorkspaceID: agent.WorkspaceID})
	if err == nil && rt.Status == "online" {
		return agent.RuntimeID, nil
	}
	target, _ := s.poolFailoverTarget(ctx, agent, db.AgentTaskQueue{RuntimeID: agent.RuntimeID}, string(taskfailure.ReasonRuntimeOffline))
	if !target.OK {
		return agent.RuntimeID, nil
	}
	return target.RuntimeID, target.History
}

// MoveWaitingTasksOffOfflineRuntimes (K28) is the sweeper stage for tasks
// nobody can claim: their runtime stayed offline beyond the grace and their
// agent has a pool. Returns how many moved.
func (s *TaskService) MoveWaitingTasksOffOfflineRuntimes(ctx context.Context, reconnectGrace time.Duration, maxPerTick int32) int {
	tasks, err := s.Queries.ListWaitingTasksOnOfflineRuntimesWithPool(ctx, db.ListWaitingTasksOnOfflineRuntimesWithPoolParams{ReconnectGraceSecs: reconnectGrace.Seconds(), MaxPerTick: maxPerTick})
	if err != nil {
		slog.Warn("runtime pool: list waiting tasks failed", "error", err)
		return 0
	}
	moved := 0
	for _, task := range tasks {
		agent, err := s.Queries.GetAgent(ctx, task.AgentID)
		if err != nil {
			continue
		}
		target, _ := s.poolFailoverTarget(ctx, agent, task, string(taskfailure.ReasonRuntimeOffline))
		if !target.OK {
			continue
		}
		if _, err := s.Queries.SetTaskFailover(ctx, db.SetTaskFailoverParams{ID: task.ID, RuntimeID: target.RuntimeID, FailoverHistory: target.History}); err != nil {
			slog.Warn("runtime pool: move waiting task failed", "task_id", util.UUIDToString(task.ID), "error", err)
			continue
		}
		slog.Info("runtime pool: moved a waiting task", "task_id", util.UUIDToString(task.ID), "to_runtime_id", util.UUIDToString(target.RuntimeID), "degraded", target.Degraded)
		moved++
	}
	return moved
}
