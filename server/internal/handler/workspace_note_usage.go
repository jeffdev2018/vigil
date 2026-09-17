package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Brain note usage (JEF-413): the runs that received, retrieved or opened a
// note, and the people who read it. Recording never fails a request.

const (
	workspaceNoteUsageDefaultRuns = 20
	workspaceNoteUsageMaxRuns     = 100
)

// noteRequestChannel names how a run reached the notes API: through the MCP
// server, which stamps X-Client-Platform on the requests it dispatches, or
// directly (the CLI inside a run).
func noteRequestChannel(r *http.Request) string {
	if r.Header.Get(middleware.HeaderClientPlatform) == "mcp" {
		return "mcp"
	}
	return "api"
}

// isMobileClient reports the iOS app, which stamps X-Client-Platform: mobile
// on every request. Client-controlled, so it only picks a counter's channel.
func isMobileClient(r *http.Request) bool {
	return r.Header.Get(middleware.HeaderClientPlatform) == "mobile"
}

// recordRunNoteUsage records kind for notes when the request comes from a run
// (an agent with its task). Member requests record nothing here.
func (h *Handler) recordRunNoteUsage(r *http.Request, workspaceID pgtype.UUID, kind string, notes []db.WorkspaceNote) {
	if len(notes) == 0 {
		return
	}
	actorType, actorID, taskID := h.noteActor(r, requestUserID(r), uuidToString(workspaceID))
	if actorType != "agent" || !taskID.Valid {
		return
	}
	service.RecordRunNoteUsage(r.Context(), h.Queries, taskID, actorID, kind, noteRequestChannel(r), service.NoteVersionsOf(notes...))
}

// recordNoteRead counts a read of one note: opened when a run fetched it,
// viewed when a member did.
func (h *Handler) recordNoteRead(r *http.Request, note db.WorkspaceNote, memberChannel string) {
	actorType, actorID, taskID := h.noteActor(r, requestUserID(r), uuidToString(note.WorkspaceID))
	if actorType == "agent" {
		if taskID.Valid {
			service.RecordRunNoteUsage(r.Context(), h.Queries, taskID, actorID, "opened", noteRequestChannel(r), service.NoteVersionsOf(note))
		}
		return
	}
	if memberChannel == "" || !actorID.Valid {
		return
	}
	if err := h.Queries.RecordMemberNoteView(r.Context(), db.RecordMemberNoteViewParams{
		WorkspaceID: note.WorkspaceID, NoteID: note.ID, NoteRevision: note.Revision,
		Channel: memberChannel, UserID: actorID,
	}); err != nil {
		slog.Warn("brain: record note view failed", "note_id", uuidToString(note.ID), "error", err)
	}
}

// RecordWorkspaceNoteView — POST /api/workspace/notes/{id}/view. A member read
// the note in an app; counted once a day. An agent gets 204 and records
// nothing: a run's reads are counted where it makes them.
func (h *Handler) RecordWorkspaceNoteView(w http.ResponseWriter, r *http.Request) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	channel := "web"
	if isMobileClient(r) {
		channel = "mobile"
	}
	actorType, _, _ := h.noteActor(r, requestUserID(r), uuidToString(note.WorkspaceID))
	if actorType == "member" {
		h.recordNoteRead(r, note, channel)
	}
	w.WriteHeader(http.StatusNoContent)
}

type WorkspaceNoteUsageCounts struct {
	Injected  int64 `json:"injected"`
	Retrieved int64 `json:"retrieved"`
	Opened    int64 `json:"opened"`
	Viewed    int64 `json:"viewed"`
}

// WorkspaceNoteUsageRun is one run that used the note. A private chat run the
// caller is not in keeps only its agent, kinds and date.
type WorkspaceNoteUsageRun struct {
	TaskID          string   `json:"task_id"`
	AgentID         string   `json:"agent_id"`
	AgentName       string   `json:"agent_name"`
	IssueID         string   `json:"issue_id"`
	IssueIdentifier string   `json:"issue_identifier"`
	Kinds           []string `json:"kinds"`
	FirstAt         string   `json:"first_at"`
	Private         bool     `json:"private"`
}

// WorkspaceNoteUsageResponse counts people (viewers_count) but never names
// them.
type WorkspaceNoteUsageResponse struct {
	Counts       WorkspaceNoteUsageCounts `json:"counts"`
	RunsCount    int64                    `json:"runs_count"`
	ViewersCount int64                    `json:"viewers_count"`
	LastUsedAt   *string                  `json:"last_used_at"`
	Runs         []WorkspaceNoteUsageRun  `json:"runs"`
}

// GetWorkspaceNoteUsage — GET /api/workspace/notes/{id}/usage?limit=20.
func (h *Handler) GetWorkspaceNoteUsage(w http.ResponseWriter, r *http.Request) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	limit := workspaceNoteUsageDefaultRuns
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(parsed, workspaceNoteUsageMaxRuns)
	}
	ctx := r.Context()
	summary, err := h.Queries.GetWorkspaceNoteUsageSummary(ctx, db.GetWorkspaceNoteUsageSummaryParams{NoteID: note.ID, WorkspaceID: note.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read note usage")
		return
	}
	rows, err := h.Queries.ListWorkspaceNoteUsageRuns(ctx, db.ListWorkspaceNoteUsageRunsParams{NoteID: note.ID, WorkspaceID: note.WorkspaceID, RowLimit: int32(limit)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read note usage")
		return
	}
	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, uuidToString(note.WorkspaceID))
	resp := WorkspaceNoteUsageResponse{
		Counts:    WorkspaceNoteUsageCounts{Injected: summary.Injected, Retrieved: summary.Retrieved, Opened: summary.Opened, Viewed: summary.Viewed},
		RunsCount: summary.RunsCount, ViewersCount: summary.ViewersCount,
		Runs: make([]WorkspaceNoteUsageRun, 0, len(rows)),
	}
	if summary.LastUsedAt.Valid {
		last := timestampToString(summary.LastUsedAt)
		resp.LastUsedAt = &last
	}
	for _, row := range rows {
		run := WorkspaceNoteUsageRun{
			AgentID: uuidToString(row.AgentID), AgentName: row.AgentName,
			Kinds: row.Kinds, FirstAt: timestampToString(row.FirstAt),
		}
		if row.IsChat && !h.inChatRun(ctx, actorType, userID, note.WorkspaceID, row.ChatSessionID) {
			run.Private = true
		} else {
			run.TaskID = uuidToString(row.TaskID)
			if row.IssueID.Valid {
				run.IssueID = uuidToString(row.IssueID)
				run.IssueIdentifier = row.IssueIdentifier
			}
		}
		resp.Runs = append(resp.Runs, run)
	}
	writeJSON(w, http.StatusOK, resp)
}

// inChatRun applies chatSessionAccess, the one definition of "in this
// conversation": a chat run is visible to the session's creator and
// participants only. A deleted session has no one left in it.
func (h *Handler) inChatRun(ctx context.Context, actorType, userID string, workspaceID, sessionID pgtype.UUID) bool {
	if actorType != "member" || !sessionID.Valid {
		return false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(ctx, db.GetChatSessionInWorkspaceParams{ID: sessionID, WorkspaceID: workspaceID})
	if err != nil {
		return false
	}
	allowed, err := h.chatSessionAccess(ctx, session, userID)
	return err == nil && allowed
}

type TaskNoteUsageItem struct {
	NoteID   string   `json:"note_id"`
	Title    string   `json:"title"`
	Revision int64    `json:"revision"`
	Kinds    []string `json:"kinds"`
	Channels []string `json:"channels"`
	FirstAt  *string  `json:"first_at"`
	Deleted  bool     `json:"deleted"`
}

// ListTaskNoteUsage — GET /api/tasks/{taskId}/note-usage. Same access check
// as ListTaskMessagesByUser: the run must belong to the caller's workspace.
func (h *Handler) ListTaskNoteUsage(w http.ResponseWriter, r *http.Request) {
	taskUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	rows, err := h.Queries.ListTaskNoteUsage(r.Context(), db.ListTaskNoteUsageParams{TaskID: task.ID, WorkspaceID: parseUUID(wsID)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read run note usage")
		return
	}
	items := make([]TaskNoteUsageItem, 0, len(rows))
	for _, row := range rows {
		item := TaskNoteUsageItem{
			NoteID: uuidToString(row.NoteID), Title: row.Title, Revision: row.NoteRevision,
			Kinds: row.Kinds, Channels: row.Channels, Deleted: row.Deleted,
		}
		if row.FirstAt.Valid {
			first := timestampToString(row.FirstAt)
			item.FirstAt = &first
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": items})
}

// knowledgeFilePath matches a Brain note file the daemon wrote into a run's
// workdir; the group is the note id prefix brainknowledge.FileName uses.
var knowledgeFilePath = regexp.MustCompile("\\.multica/knowledge/[^\\s\"'`]*?-([0-9a-f]{8})\\.md")

// knowledgeFileRefs appends the id prefix of every Brain note file named in
// any string of a tool call's input. Pure, so the per-message cost of a batch
// that names no knowledge file is a substring check, never a query.
func knowledgeFileRefs(refs []string, v any) []string {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, ".multica/knowledge/") {
			for _, m := range knowledgeFilePath.FindAllStringSubmatch(x, -1) {
				refs = append(refs, m[1])
			}
		}
	case map[string]any:
		for _, e := range x {
			refs = knowledgeFileRefs(refs, e)
		}
	case []any:
		for _, e := range x {
			refs = knowledgeFileRefs(refs, e)
		}
	}
	return refs
}

// recordKnowledgeFileReads records opened/file_read for the notes of THIS run
// whose files a tool call named. Only notes the claim injected
// (memory_context.workspace_notes) can match: another run's note, README.md or
// an unknown prefix records nothing.
func (h *Handler) recordKnowledgeFileReads(ctx context.Context, task db.AgentTaskQueue, prefixes []string) {
	if len(prefixes) == 0 || len(task.MemoryContext) == 0 {
		return
	}
	var mc service.TaskMemoryContext
	if err := json.Unmarshal(task.MemoryContext, &mc); err != nil {
		return
	}
	named := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		named[p] = true
	}
	var opened []service.NoteVersion
	for _, n := range mc.WorkspaceNotes {
		if named[brainknowledge.IDPrefix(n.ID)] {
			opened = append(opened, n)
		}
	}
	service.RecordRunNoteUsage(ctx, h.Queries, task.ID, task.AgentID, "opened", "file_read", opened)
}
