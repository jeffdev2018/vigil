package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Dead run-branch garbage collection (JEF-388).
//
// A terminal run whose branch was neither promoted nor discarded leaves that
// branch (and usually a worktree) on the daemon's machine forever. Two ways
// out, both riding the JEF-255 branch-action channel — the server never
// touches git itself:
//
//   - GET /api/runs/dead-branches is the dry-run plan: the dead-branch set of
//     the caller's workspace, with per-run actionability so the UI can show
//     why a given branch cannot be collected right now.
//   - POST /api/runs/dead-branches/discard is the human-confirmed batch: each
//     task re-runs the full guard chain of the single-run discard endpoint,
//     and per-run refusals are collected rather than failing the batch.
//   - branch_gc_tick is the automatic half: workspaces that opted in via
//     settings.branch_gc get a periodic sweep that enqueues a discard for
//     every dead branch older than ttl_days.

const (
	// deadBranchPlanLimit bounds one plan response / one sweep pass over a
	// workspace. A workspace with more dead branches than this sees the rest
	// on the next hourly tick.
	deadBranchPlanLimit = 500

	// maxDeadBranchDiscardBatch caps the batch discard body, like the issue
	// GC check batch it mirrors.
	maxDeadBranchDiscardBatch = 200

	DeadBranchSkipRuntimeOffline    = "runtime_offline"
	DeadBranchSkipCapabilityMissing = "capability_missing"
	DeadBranchSkipActionPending     = "action_pending"
)

// DeadBranchEntry is one row of the dead-branch plan. FinishedAt is null for
// a terminal run that never recorded completed_at; SkipReason is set iff the
// run cannot be discarded right now.
type DeadBranchEntry struct {
	TaskID          string  `json:"task_id"`
	IssueID         *string `json:"issue_id"`
	IssueIdentifier *string `json:"issue_identifier"`
	IssueTitle      *string `json:"issue_title"`
	BranchName      string  `json:"branch_name"`
	RuntimeID       string  `json:"runtime_id"`
	RuntimeName     string  `json:"runtime_name"`
	FinishedAt      *string `json:"finished_at"`
	Actionable      bool    `json:"actionable"`
	SkipReason      *string `json:"skip_reason"`
}

// ListDeadBranches serves the dry-run plan: every dead branch of the
// workspace, actionable or not.
//
// GET /api/runs/dead-branches
func (h *Handler) ListDeadBranches(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	entries, err := h.deadBranchEntries(r.Context(), wsUUID, pgtype.Timestamptz{}, time.Now())
	if err != nil {
		slog.Warn("dead branches: plan failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list dead branches")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// DiscardDeadBranchesRequest is the body of the batch discard.
type DiscardDeadBranchesRequest struct {
	TaskIDs []string `json:"task_ids"`
}

// DeadBranchDiscardSkipped is one run the batch refused, with the guard's own
// sentence as the reason.
type DeadBranchDiscardSkipped struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// DiscardDeadBranches enqueues a discard per confirmed run. Per-run refusals
// land in skipped; the batch only fails outright on a malformed body.
//
// POST /api/runs/dead-branches/discard
func (h *Handler) DiscardDeadBranches(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req DiscardDeadBranchesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.TaskIDs) > maxDeadBranchDiscardBatch {
		writeError(w, http.StatusBadRequest, "too many task_ids")
		return
	}

	enqueued := 0
	skipped := make([]DeadBranchDiscardSkipped, 0)
	for _, raw := range req.TaskIDs {
		taskUUID, err := util.ParseUUID(raw)
		if err != nil {
			skipped = append(skipped, DeadBranchDiscardSkipped{TaskID: raw, Reason: "invalid task_id"})
			continue
		}
		task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
		if err != nil {
			skipped = append(skipped, DeadBranchDiscardSkipped{TaskID: raw, Reason: "run not found"})
			continue
		}
		// Same tenant guard as the run's diff: the task must belong to the
		// caller's workspace, whatever id the body carried.
		if taskWS := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task); taskWS == "" || taskWS != uuidToString(wsUUID) {
			skipped = append(skipped, DeadBranchDiscardSkipped{TaskID: raw, Reason: "run not found"})
			continue
		}
		if _, refusal := h.enqueueRunBranchAction(r.Context(), wsUUID, task, runBranchActionDiscard, "member", userID); refusal != nil {
			skipped = append(skipped, DeadBranchDiscardSkipped{TaskID: raw, Reason: refusal.message})
			continue
		}
		enqueued++
	}
	writeJSON(w, http.StatusOK, map[string]any{"enqueued": enqueued, "skipped": skipped})
}

// TickBranchGC is the scheduler entry point: for each workspace that opted
// in, enqueue a discard for every actionable dead branch older than its TTL.
// Idempotent by construction — a run whose discard is already in flight is
// not actionable, and the enqueue's 409 guard is the final arbiter.
func (h *Handler) TickBranchGC(ctx context.Context, now time.Time) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesWithBranchGCEnabled(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	total := 0
	for _, ws := range workspaces {
		cfg := service.BranchGCFromSettings(ws.Settings)
		if !cfg.Enabled {
			continue
		}
		cutoff := pgtype.Timestamptz{Time: now.Add(-time.Duration(cfg.TTLDays) * 24 * time.Hour), Valid: true}
		entries, err := h.deadBranchEntries(ctx, ws.ID, cutoff, now)
		if err != nil {
			slog.Warn("branch gc: candidate scan failed", "workspace_id", uuidToString(ws.ID), "error", err)
			continue
		}
		enqueued, skipped := 0, 0
		for _, entry := range entries {
			if !entry.Actionable {
				skipped++
				continue
			}
			task, err := h.Queries.GetAgentTask(ctx, parseUUID(entry.TaskID))
			if err != nil {
				skipped++
				continue
			}
			// A second tick over an unchanged workspace re-walks the same
			// candidates: the in-flight guard answers 409, which is a skip,
			// not an error.
			if _, refusal := h.enqueueRunBranchAction(ctx, ws.ID, task, runBranchActionDiscard, "system", ""); refusal != nil {
				skipped++
				continue
			}
			enqueued++
		}
		if enqueued > 0 || skipped > 0 {
			slog.Info("branch gc: tick", "workspace_id", uuidToString(ws.ID),
				"ttl_days", cfg.TTLDays, "enqueued", enqueued, "skipped", skipped)
		}
		total += enqueued
	}
	return total, nil
}

// deadBranchEntries is the shared candidate read of the plan endpoint and the
// sweep: the workspace's dead branches, each marked with whether a discard
// could be enqueued for it right now. olderThan invalid lists everything.
func (h *Handler) deadBranchEntries(ctx context.Context, wsUUID pgtype.UUID, olderThan pgtype.Timestamptz, now time.Time) ([]DeadBranchEntry, error) {
	rows, err := h.Queries.ListDeadBranchRuns(ctx, db.ListDeadBranchRunsParams{
		WorkspaceID: wsUUID,
		OlderThan:   olderThan,
		Lim:         deadBranchPlanLimit,
	})
	if err != nil {
		return nil, err
	}
	prefix := h.getIssuePrefix(ctx, wsUUID)
	entries := make([]DeadBranchEntry, 0, len(rows))
	for _, row := range rows {
		entry := DeadBranchEntry{
			TaskID:     uuidToString(row.TaskID),
			BranchName: row.BranchName.String,
		}
		if row.IssueID.Valid {
			issueID := uuidToString(row.IssueID)
			entry.IssueID = &issueID
			if row.IssueNumber.Valid {
				identifier := service.IssueIdentifier(prefix, row.IssueNumber.Int32)
				entry.IssueIdentifier = &identifier
			}
			if row.IssueTitle.Valid {
				entry.IssueTitle = &row.IssueTitle.String
			}
		}
		if row.RuntimeID.Valid {
			entry.RuntimeID = uuidToString(row.RuntimeID)
			entry.RuntimeName = row.RuntimeName.String
		}
		if row.FinishedAt.Valid {
			finished := row.FinishedAt.Time.Format(time.RFC3339)
			entry.FinishedAt = &finished
		}
		if reason := deadBranchSkipReason(row, now); reason != "" {
			entry.SkipReason = &reason
		} else {
			entry.Actionable = true
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// deadBranchSkipReason reports why a discard cannot be enqueued for the run
// right now, or "". Runtime first: a run whose runtime is gone or silent
// cannot act whatever it advertises, and an in-flight action only matters
// once a daemon could actually pick a second one up.
func deadBranchSkipReason(row db.ListDeadBranchRunsRow, now time.Time) string {
	if !row.RuntimeID.Valid || !row.RuntimeStatus.Valid || row.RuntimeStatus.String != "online" ||
		!row.RuntimeLastSeenAt.Valid || now.Sub(row.RuntimeLastSeenAt.Time) > service.RuntimeClaimFreshnessSeconds*time.Second {
		return DeadBranchSkipRuntimeOffline
	}
	if !runtimeHasCapability(row.RuntimeMetadata, protocol.DaemonCapabilityBranchActionV1) {
		return DeadBranchSkipCapabilityMissing
	}
	if row.ActionPending {
		return DeadBranchSkipActionPending
	}
	return ""
}
