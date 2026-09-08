package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentMemoryUsageVersion struct {
	MemoryID      string `json:"memory_id"`
	Revision      int32  `json:"revision"`
	PreparedRuns  int64  `json:"prepared_runs"`
	LastStartedAt string `json:"last_started_at"`
}

type AgentMemoryUsageResponse struct {
	Since               string                    `json:"since"`
	Until               string                    `json:"until"`
	StartedRuns         int64                     `json:"started_runs"`
	RecordedRuns        int64                     `json:"recorded_runs"`
	UnrecordedRuns      int64                     `json:"unrecorded_runs"`
	LoadFailedRuns      int64                     `json:"load_failed_runs"`
	RunsWithAgentMemory int64                     `json:"runs_with_agent_memory"`
	Versions            []AgentMemoryUsageVersion `json:"versions"`
}

// GetAgentMemoryUsage reports prepared context for retained, started non-chat
// runs. It measures coverage, not receipt by the daemon or learning success.
func (h *Handler) GetAgentMemoryUsage(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	// Match the existing run-history gate, including private agents.
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	until := time.Now().UTC()
	since := until.Add(-30 * 24 * time.Hour)
	row, err := h.Queries.GetAgentMemoryUsage(r.Context(), db.GetAgentMemoryUsageParams{
		AgentID: agent.ID, WorkspaceID: agent.WorkspaceID,
		Since: pgtype.Timestamptz{Time: since, Valid: true}, Until: pgtype.Timestamptz{Time: until, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read memory usage")
		return
	}
	response := AgentMemoryUsageResponse{
		Since: since.Format(time.RFC3339Nano), Until: until.Format(time.RFC3339Nano),
		StartedRuns: row.StartedRuns, RecordedRuns: row.RecordedRuns, UnrecordedRuns: row.UnrecordedRuns,
		LoadFailedRuns: row.LoadFailedRuns, RunsWithAgentMemory: row.RunsWithAgentMemory,
		Versions: []AgentMemoryUsageVersion{},
	}
	if err = json.Unmarshal(row.Versions, &response.Versions); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode memory usage")
		return
	}
	writeJSON(w, http.StatusOK, response)
}
