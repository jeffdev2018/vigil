package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectMemoryUsageVersion struct {
	ProjectID     string `json:"project_id"`
	Revision      int32  `json:"revision"`
	PreparedRuns  int64  `json:"prepared_runs"`
	LastStartedAt string `json:"last_started_at"`
}

type ProjectMemoryUsageResponse struct {
	Since                 string                      `json:"since"`
	Until                 string                      `json:"until"`
	StartedRuns           int64                       `json:"started_runs"`
	RecordedRuns          int64                       `json:"recorded_runs"`
	UnrecordedRuns        int64                       `json:"unrecorded_runs"`
	RunsWithProjectMemory int64                       `json:"runs_with_project_memory"`
	Versions              []ProjectMemoryUsageVersion `json:"versions"`
}

// GetProjectMemoryUsage reports prepared project-memory context for retained,
// started non-chat runs on issues of this project. It measures coverage of
// claim preparation, not receipt by the daemon or learning success.
func (h *Handler) GetProjectMemoryUsage(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForMemory(w, r)
	if !ok {
		return
	}
	until := time.Now().UTC()
	since := until.Add(-30 * 24 * time.Hour)
	row, err := h.Queries.GetProjectMemoryUsage(r.Context(), db.GetProjectMemoryUsageParams{
		ProjectID: project.ID, WorkspaceID: project.WorkspaceID,
		Since: pgtype.Timestamptz{Time: since, Valid: true}, Until: pgtype.Timestamptz{Time: until, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read project memory usage")
		return
	}
	response := ProjectMemoryUsageResponse{
		Since: since.Format(time.RFC3339Nano), Until: until.Format(time.RFC3339Nano),
		StartedRuns: row.StartedRuns, RecordedRuns: row.RecordedRuns, UnrecordedRuns: row.UnrecordedRuns,
		RunsWithProjectMemory: row.RunsWithProjectMemory,
		Versions:              []ProjectMemoryUsageVersion{},
	}
	if err = json.Unmarshal(row.Versions, &response.Versions); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode project memory usage")
		return
	}
	writeJSON(w, http.StatusOK, response)
}
