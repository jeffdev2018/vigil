package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Autopilot execution memory (F24 / JEF-15): one document a recurring daemon
// keeps for its own next run. Members read it; only a run of THAT daemon
// writes it, over its task token.
//
// Deliberately not agent memory. An agent can serve several daemons, and the
// queue state a nightly triage job learned is nothing an unrelated run of the
// same agent should be told. Scoping the row to the autopilot is what keeps
// the two apart.

const (
	// autopilotMemoryMaxBytes bounds the document. Past it the write SUCCEEDS
	// with the overflow cut on a line boundary rather than failing: the writer
	// is an agent finishing a run, and a rejected write would either lose the
	// whole document or push the agent into a retry loop it cannot resolve.
	// Truncation is reported back so the agent can see it happened.
	autopilotMemoryMaxBytes = 8 << 10
)

type AutopilotMemoryResponse struct {
	AutopilotID string `json:"autopilot_id"`
	Content     string `json:"content"`
	// Revision is the If-Match token: a PUT must carry the revision it read or
	// the write is refused with 409 memory_revision_stale.
	Revision        int32   `json:"revision"`
	UpdatedByTaskID *string `json:"updated_by_task_id"`
	UpdatedAt       string  `json:"updated_at"`
	// Truncated reports that the stored content is shorter than what was sent,
	// because the document exceeded the byte cap. Only meaningful on a write.
	Truncated bool `json:"truncated,omitempty"`
}

func autopilotMemoryToResponse(m db.AutopilotMemory) AutopilotMemoryResponse {
	return AutopilotMemoryResponse{
		AutopilotID:     uuidToString(m.AutopilotID),
		Content:         m.Content,
		Revision:        m.Revision,
		UpdatedByTaskID: uuidToPtr(m.UpdatedByTaskID),
		UpdatedAt:       timestampToString(m.UpdatedAt),
	}
}

type UpdateAutopilotMemoryRequest struct {
	Content string `json:"content"`
}

// truncateAutopilotMemory cuts content to the byte cap on a line boundary. A
// mid-line cut would leave the next run reading half a sentence as if it were
// whole, which is worse than losing the line outright. When no newline fits,
// the raw byte cut is the only option left; it is still valid UTF-8 because
// the cut is moved back off any continuation byte.
func truncateAutopilotMemory(content string) (string, bool) {
	if len(content) <= autopilotMemoryMaxBytes {
		return content, false
	}
	cut := content[:autopilotMemoryMaxBytes]
	if idx := strings.LastIndexByte(cut, '\n'); idx >= 0 {
		return cut[:idx], true
	}
	for len(cut) > 0 && !utf8StartsBoundary(content[len(cut)]) {
		cut = cut[:len(cut)-1]
	}
	return cut, true
}

// utf8StartsBoundary reports whether b can start a UTF-8 sequence, i.e. it is
// not a 10xxxxxx continuation byte.
func utf8StartsBoundary(b byte) bool { return b&0xC0 != 0x80 }

// GetAutopilotMemory returns the daemon's memory document. Any member who can
// read the autopilot can read it — the memory is what the daemon will be told
// next run, so it must be inspectable by the people accountable for it.
func (h *Handler) GetAutopilotMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	ap, ok := h.loadAutopilotInWorkspace(w, r, chi.URLParam(r, "id"), workspaceID)
	if !ok {
		return
	}
	memory, err := h.Queries.GetAutopilotMemory(r.Context(), db.GetAutopilotMemoryParams{
		AutopilotID: ap.ID,
		WorkspaceID: ap.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// An empty memory is a real state, not a missing resource: the daemon
		// exists and has simply not written anything yet. Returning 404 would
		// make the UI branch on "no daemon" vs "no memory yet".
		writeJSON(w, http.StatusOK, AutopilotMemoryResponse{
			AutopilotID: uuidToString(ap.ID),
			Content:     "",
			Revision:    0,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load autopilot memory")
		return
	}
	writeJSON(w, http.StatusOK, autopilotMemoryToResponse(memory))
}

// UpdateAutopilotMemory replaces the daemon's memory document. Agents only,
// and only from a run of this daemon: the write is authorized by the task
// token's X-Task-ID, whose task must belong to an autopilot_run of this
// autopilot. A member cannot write here — the memory is what the daemon
// learned, and a human editing it would put words in the run's mouth.
func (h *Handler) UpdateAutopilotMemory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	autopilotID := chi.URLParam(r, "id")

	idUUID, ok := parseUUIDOrBadRequest(w, autopilotID, "autopilot id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "autopilot memory is written by the daemon's own runs, not by members")
		return
	}
	task, hasTask := h.taskFromRequestHeader(r)
	if !hasTask {
		writeError(w, http.StatusForbidden, "autopilot memory requires the run's task token")
		return
	}
	ap, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}
	if !h.taskBelongsToAutopilot(r, task, ap.ID) {
		writeError(w, http.StatusForbidden, "this run does not belong to the autopilot whose memory it is writing")
		return
	}

	var req UpdateAutopilotMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	current, err := h.Queries.GetAutopilotMemory(r.Context(), db.GetAutopilotMemoryParams{
		AutopilotID: ap.ID,
		WorkspaceID: ap.WorkspaceID,
	})
	currentRevision := int32(0)
	if err == nil {
		currentRevision = current.Revision
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load autopilot memory")
		return
	}

	// If-Match is optional so a first write does not have to guess a revision,
	// but when present it is authoritative. Two runs of the same daemon can
	// overlap, and a blind last-writer-wins would silently drop what the other
	// one learned.
	if raw := strings.TrimSpace(r.Header.Get("If-Match")); raw != "" {
		want, convErr := strconv.ParseInt(strings.Trim(raw, `"`), 10, 32)
		if convErr != nil {
			writeError(w, http.StatusBadRequest, "If-Match must be the revision number you read")
			return
		}
		if int32(want) != currentRevision {
			writeErrorCode(w, http.StatusConflict, "memory_revision_stale",
				"autopilot memory changed since you read it; re-read it and re-apply your update")
			return
		}
	}

	content, truncated := truncateAutopilotMemory(util.SanitizeTextForPostgres(req.Content))

	updated, err := h.Queries.UpsertAutopilotMemory(r.Context(), db.UpsertAutopilotMemoryParams{
		AutopilotID:      ap.ID,
		WorkspaceID:      ap.WorkspaceID,
		Content:          content,
		UpdatedByTaskID:  task.ID,
		ExpectedRevision: currentRevision,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The ON CONFLICT guard refused: another run committed between our
		// read and our write.
		writeErrorCode(w, http.StatusConflict, "memory_revision_stale",
			"autopilot memory changed since you read it; re-read it and re-apply your update")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save autopilot memory")
		return
	}

	resp := autopilotMemoryToResponse(updated)
	resp.Truncated = truncated
	writeJSON(w, http.StatusOK, resp)
}

// taskBelongsToAutopilot reports whether the run behind this request was
// started by the given autopilot. A task carries autopilot_run_id; the run
// carries autopilot_id.
func (h *Handler) taskBelongsToAutopilot(r *http.Request, task db.AgentTaskQueue, autopilotID pgtype.UUID) bool {
	if !task.AutopilotRunID.Valid {
		return false
	}
	run, err := h.Queries.GetAutopilotRun(r.Context(), task.AutopilotRunID)
	if err != nil {
		return false
	}
	return run.AutopilotID == autopilotID
}
