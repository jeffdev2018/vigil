package handler

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// routingStatsWindowDays is the lookback for the routing-stats endpoint —
// the same 90-day window the runtime router scores on, so the UI shows
// exactly the data the decisions are made from.
const routingStatsWindowDays = 90

// costTicksPerUSD converts task_usage.cost_usd_ticks (1e-10 USD) to USD.
const costTicksPerUSD = 1e-10

// RuntimeRoutingStatsRow is one (runtime, provider, model, task_class)
// aggregate line of the routing statistics (JEF-237). Averages are pointers:
// null when no run in the bucket carried a cost / a start time, distinct from
// a genuine zero.
type RuntimeRoutingStatsRow struct {
	RuntimeID   string `json:"runtime_id"`
	RuntimeName string `json:"runtime_name"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	TaskClass   string `json:"task_class"`
	Samples     int32  `json:"samples"`
	// BenchmarkSamples is how many of those samples came from a benchmark
	// replay rather than from ordinary work.
	BenchmarkSamples int32    `json:"benchmark_samples"`
	SuccessRate      float64  `json:"success_rate"`
	AvgCostUSD       *float64 `json:"avg_cost_usd"`
	AvgDurationSecs  *float64 `json:"avg_duration_secs"`
}

// RuntimeRoutingStatsResponse wraps the rows with the window they were
// computed on.
type RuntimeRoutingStatsResponse struct {
	WindowDays int32                    `json:"window_days"`
	Rows       []RuntimeRoutingStatsRow `json:"rows"`
}

// GetRuntimeRoutingStats serves GET /api/runtimes/routing-stats: the trailing
// 90-day per-(runtime, provider, model, task_class) run statistics backing
// the runtime router, for any workspace member.
func (h *Handler) GetRuntimeRoutingStats(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	rows, err := h.Queries.GetRoutingStats(r.Context(), db.GetRoutingStatsParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-routingStatsWindowDays * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load routing stats")
		return
	}

	resp := RuntimeRoutingStatsResponse{
		WindowDays: routingStatsWindowDays,
		Rows:       make([]RuntimeRoutingStatsRow, 0, len(rows)),
	}
	for _, row := range rows {
		out := RuntimeRoutingStatsRow{
			RuntimeID:        uuidToString(row.RuntimeID),
			RuntimeName:      row.RuntimeName,
			Provider:         row.Provider,
			Model:            row.Model,
			TaskClass:        row.TaskClass,
			Samples:          row.Samples,
			BenchmarkSamples: row.BenchmarkSamples,
		}
		if row.Samples > 0 {
			out.SuccessRate = float64(row.SuccessCount) / float64(row.Samples)
		}
		if row.CostSamples > 0 {
			avg := row.TotalCostUsdTicks / float64(row.CostSamples) * costTicksPerUSD
			out.AvgCostUSD = &avg
		}
		if row.DurationSamples > 0 {
			avg := row.TotalDurationSecs / float64(row.DurationSamples)
			out.AvgDurationSecs = &avg
		}
		resp.Rows = append(resp.Rows, out)
	}
	writeJSON(w, http.StatusOK, resp)
}
