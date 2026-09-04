package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
)

// TriageSourceStats is one inbound source and its 24h activity.
type TriageSourceStats struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	RefID      string `json:"ref_id"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Items24h   int64  `json:"items_24h"`
	Dropped24h int64  `json:"dropped_24h"`
}

// TriageStatsResponse summarizes the triage queue for the workspace. In M1
// every captured item is shadow (routing is unchanged), so pending stays zero
// and the shadow fields carry the measurement.
type TriageStatsResponse struct {
	Pending                 int64               `json:"pending"`
	ShadowPending           int64               `json:"shadow_pending"`
	Dropped24h              int64               `json:"dropped_24h"`
	OldestPendingAgeSeconds int64               `json:"oldest_pending_age_seconds"`
	Sources                 []TriageSourceStats `json:"sources"`
}

// GetTriageStats returns queue volume for the workspace: real pending items,
// the M1 shadow measurement, and per-source 24h counts including dropped
// deliveries — the silent-loss population triage exists to hold.
func (h *Handler) GetTriageStats(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	ctx := r.Context()

	byState, err := h.Queries.CountTriageItemsByState(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	var pending, shadowPending int64
	for _, row := range byState {
		if row.State != triage.StatePending {
			continue
		}
		if row.Shadow {
			shadowPending += row.N
		} else {
			pending += row.N
		}
	}

	recent, err := h.Queries.CountRecentTriageItemsBySource(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	type sourceActivity struct {
		items, dropped int64
	}
	activity := make(map[string]*sourceActivity)
	var dropped24h int64
	for _, row := range recent {
		id := util.UUIDToString(row.SourceID)
		act := activity[id]
		if act == nil {
			act = &sourceActivity{}
			activity[id] = act
		}
		act.items += row.N
		if row.State == triage.StateDropped {
			act.dropped += row.N
			dropped24h += row.N
		}
	}

	age, err := h.Queries.OldestRealPendingTriageAgeSeconds(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}

	sources, err := h.Queries.ListTriageSources(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	resp := TriageStatsResponse{
		Pending:                 pending,
		ShadowPending:           shadowPending,
		Dropped24h:              dropped24h,
		OldestPendingAgeSeconds: age,
		Sources:                 []TriageSourceStats{},
	}
	for _, src := range sources {
		id := util.UUIDToString(src.ID)
		stats := TriageSourceStats{
			ID:    id,
			Kind:  src.Kind,
			RefID: util.UUIDToString(src.RefID),
			Name:  src.Name,
			Mode:  src.Mode,
		}
		if act := activity[id]; act != nil {
			stats.Items24h = act.items
			stats.Dropped24h = act.dropped
		}
		resp.Sources = append(resp.Sources, stats)
	}
	writeJSON(w, http.StatusOK, resp)
}
