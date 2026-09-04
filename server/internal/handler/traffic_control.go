package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Traffic control (K18). A run's edited paths come from its own tool
// calls (server side, every provider); a human's from the daemon's
// heartbeat (git status of the local checkouts it manages). An overlap
// files a conflict, an Attention Inbox item pointing at the run's latest
// handoff packet, and — when the workspace asks for it — a pause.

const (
	AuditTrafficConflict  = "traffic.conflict"
	TrafficInboxType      = "traffic_conflict"
	trafficMaxPathsPerRun = 500
)

type TrafficConflictResponse struct {
	ID              string   `json:"id"`
	TaskID          string   `json:"task_id"`
	Kind            string   `json:"kind"`
	Paths           []string `json:"paths"`
	OtherTaskID     *string  `json:"other_task_id"`
	HandoffPacketID *string  `json:"handoff_packet_id"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	ResolvedAt      *string  `json:"resolved_at"`
}

func trafficConflictToResponse(c db.TrafficConflict) TrafficConflictResponse {
	return TrafficConflictResponse{
		ID: uuidToString(c.ID), TaskID: uuidToString(c.TaskID), Kind: c.Kind, Paths: jsonStrings(c.Paths), OtherTaskID: uuidToPtr(c.OtherTaskID),
		HandoffPacketID: uuidToPtr(c.HandoffPacketID), Status: c.Status, CreatedAt: timestampToString(c.CreatedAt), ResolvedAt: timestampToPtr(c.ResolvedAt),
	}
}

// recordTouchedPaths (K18) reads the editing tool calls of a message batch
// and appends their paths to the run, then looks for conflicts.
func (h *Handler) recordTouchedPaths(ctx context.Context, task db.AgentTaskQueue, messages []db.CreateTaskMessagesRow) {
	var fresh []string
	seen := map[string]bool{}
	for _, m := range messages {
		if (m.Type != "tool_use" && m.Type != "tool-use") || !service.IsEditingTool(m.Tool.String) {
			continue
		}
		for _, p := range service.ToolInputPaths(m.Input) {
			rel := service.RelativePath(p, task.WorkDir.String)
			if rel != "" && !seen[rel] {
				seen[rel] = true
				fresh = append(fresh, rel)
			}
		}
	}
	if len(fresh) == 0 {
		return
	}
	existing := jsonStrings(task.TouchedPaths)
	if len(existing) >= trafficMaxPathsPerRun {
		return
	}
	raw, _ := json.Marshal(fresh)
	updated, err := h.Queries.AppendTaskTouchedPaths(ctx, db.AppendTaskTouchedPathsParams{ID: task.ID, Paths: raw})
	if err != nil {
		slog.Warn("traffic: append touched paths failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	h.detectTrafficConflicts(ctx, updated, fresh)
}

// detectTrafficConflicts compares fresh paths with other active runs and
// with the run's daemon dirty checkouts.
func (h *Handler) detectTrafficConflicts(ctx context.Context, task db.AgentTaskQueue, fresh []string) {
	if !task.IssueID.Valid {
		return
	}
	agent, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return
	}
	// Agent vs agent: disjoint paths never alert.
	if others, err := h.Queries.ListActiveTasksTouchingPaths(ctx, db.ListActiveTasksTouchingPathsParams{WorkspaceID: agent.WorkspaceID, ID: task.ID, Paths: fresh}); err == nil {
		for _, other := range others {
			if overlap := service.IntersectPaths(fresh, jsonStrings(other.TouchedPaths)); len(overlap) > 0 {
				h.fileTrafficConflict(ctx, agent.WorkspaceID, task, "agent", overlap, other.ID)
			}
		}
	}
	// Human: the daemon's checkouts, recent enough.
	if task.RuntimeID.Valid {
		if rt, err := h.Queries.GetAgentRuntime(ctx, task.RuntimeID); err == nil {
			if dirty, ok := recentDirtyPaths(rt.Metadata, time.Now()); ok {
				if overlap := service.OverlapPaths(fresh, dirty); len(overlap) > 0 {
					h.fileTrafficConflict(ctx, agent.WorkspaceID, task, "human", overlap, pgtype.UUID{})
				}
			}
		}
	}
}

// recentDirtyPaths flattens the daemon's report when it is within the window.
func recentDirtyPaths(metadata []byte, now time.Time) ([]string, bool) {
	var meta struct {
		Dirty []service.DirtyCheckout `json:"dirty_checkouts"`
		At    string                  `json:"dirty_checkouts_at"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &meta) != nil || len(meta.Dirty) == 0 {
		return nil, false
	}
	at, err := time.Parse(time.RFC3339Nano, meta.At)
	if err != nil || now.Sub(at) > service.HumanEditWindowSeconds*time.Second {
		return nil, false
	}
	var out []string
	for _, c := range meta.Dirty {
		out = append(out, c.Paths...)
	}
	return out, len(out) > 0
}

func (h *Handler) fileTrafficConflict(ctx context.Context, wsID pgtype.UUID, task db.AgentTaskQueue, kind string, paths []string, other pgtype.UUID) {
	if exists, err := h.Queries.HasActiveTrafficConflict(ctx, db.HasActiveTrafficConflictParams{TaskID: task.ID, Kind: kind, OtherTaskID: other}); err != nil || exists {
		return
	}
	var packetID pgtype.UUID
	if p, err := h.Queries.GetLatestHandoffPacket(ctx, task.IssueID); err == nil {
		packetID = p.ID
	}
	raw, _ := json.Marshal(paths)
	conflict, err := h.Queries.CreateTrafficConflict(ctx, db.CreateTrafficConflictParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, IssueID: task.IssueID, TaskID: task.ID, Kind: kind, Paths: raw, OtherTaskID: other, HandoffPacketID: packetID,
	})
	if err != nil {
		slog.Warn("traffic: create conflict failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	ws, _ := h.Queries.GetWorkspace(ctx, wsID)
	paused := false
	if service.TrafficControlSettings(ws.Settings).PauseOnConflict {
		if _, err := h.Queries.RequestTaskPause(ctx, task.ID); err == nil {
			paused = true
		}
	}
	h.audit(ctx, wsID, "system", "", AuditTrafficConflict, "task", task.ID, map[string]any{"conflict_id": uuidToString(conflict.ID), "kind": kind, "paths": paths, "other_task_id": uuidToPtr(other), "pause_requested": paused}, nil)
	title := "Agent editing files a human is changing: " + strings.Join(paths, ", ")
	if kind == "agent" {
		title = "Two runs editing the same files: " + strings.Join(paths, ", ")
	}
	details, _ := json.Marshal(map[string]any{"conflict_id": uuidToString(conflict.ID), "task_id": uuidToString(task.ID), "kind": kind, "paths": paths, "other_task_id": uuidToPtr(other), "handoff_packet_id": uuidToPtr(packetID), "pause_requested": paused})
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, wsID)
	if err != nil {
		return
	}
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: TrafficInboxType, Severity: "attention",
			IssueID: issue.ID, Title: title, Body: pgtype.Text{String: issue.Title, Valid: true}, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}

// ListTrafficConflicts: GET /api/issues/{id}/traffic-conflicts.
func (h *Handler) ListTrafficConflicts(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	_ = h.Queries.ResolveTrafficConflictsForFinishedRuns(r.Context(), issue.ID)
	rows, err := h.Queries.ListTrafficConflictsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list traffic conflicts")
		return
	}
	out := make([]TrafficConflictResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, trafficConflictToResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"conflicts": out})
}

// IgnoreTrafficConflict: POST /api/issues/{id}/traffic-conflicts/{cid}/ignore.
func (h *Handler) IgnoreTrafficConflict(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	cid, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "cid"), "conflict id")
	if !ok {
		return
	}
	conflict, err := h.Queries.GetTrafficConflict(r.Context(), cid)
	if err != nil || conflict.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "conflict not found")
		return
	}
	updated, err := h.Queries.SetTrafficConflictStatus(r.Context(), db.SetTrafficConflictStatusParams{ID: cid, Status: "ignored"})
	if err != nil {
		writeError(w, http.StatusConflict, "the conflict is no longer active")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditTrafficConflict, "task", conflict.TaskID, map[string]any{"conflict_id": uuidToString(cid), "ignored": true}, nil)
	writeJSON(w, http.StatusOK, trafficConflictToResponse(updated))
}
