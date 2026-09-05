package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A source's policy is four settings, and until now only one of them was
// reachable: `mode` had an endpoint while auto_accept, cap_per_hour and
// expiry_days sat in the table with nobody able to write them. They are one
// object to a human configuring a source, so they are one PATCH.
const (
	// triageMaxCapPerHour bounds the anti-flood cap at something no human
	// means literally. A cap this high is indistinguishable from none.
	triageMaxCapPerHour = 100000
	// triageMaxExpiryDays bounds retention at a year. Longer is a data
	// retention decision, not a queue setting.
	triageMaxExpiryDays = 365
)

// TriageSourceResponse is one source with its full policy. It is a superset of
// the stats shape the queue header renders, so a settings screen needs one
// endpoint rather than two.
type TriageSourceResponse struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	RefID      string `json:"ref_id"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	AutoAccept bool   `json:"auto_accept"`
	CapPerHour int32  `json:"cap_per_hour"`
	ExpiryDays int32  `json:"expiry_days"`
}

func triageSourceToResponse(src db.TriageSource) TriageSourceResponse {
	return TriageSourceResponse{
		ID:         util.UUIDToString(src.ID),
		Kind:       src.Kind,
		RefID:      util.UUIDToString(src.RefID),
		Name:       src.Name,
		Mode:       src.Mode,
		AutoAccept: triage.AutoAcceptEnabled(src.AutoAccept),
		CapPerHour: src.CapPerHour,
		ExpiryDays: src.ExpiryDays,
	}
}

type updateTriageSourceRequest struct {
	// Every field is optional; an omitted one is left alone. Sending only
	// `mode` must not reset a cap somebody configured.
	Mode       *string `json:"mode"`
	AutoAccept *bool   `json:"auto_accept"`
	CapPerHour *int32  `json:"cap_per_hour"`
	ExpiryDays *int32  `json:"expiry_days"`
}

// UpdateTriageSourceSettings patches one source's admission policy: the
// gate/direct/blocked kill switch, whether the queue may resolve its items
// without a human, the per-hour flood cap, and how long an unresolved item
// survives.
func (h *Handler) UpdateTriageSourceSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req updateTriageSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateTriageSourceSettingsParams{ID: sourceID, WorkspaceID: workspaceID}
	if req.Mode != nil {
		switch *req.Mode {
		case string(triage.ModeGate), string(triage.ModeDirect), string(triage.ModeBlocked):
		default:
			writeError(w, http.StatusBadRequest, "mode must be one of: gate, direct, blocked")
			return
		}
		params.Mode = pgtype.Text{String: *req.Mode, Valid: true}
	}
	if req.AutoAccept != nil {
		// Stored as a policy object so it can grow without a migration.
		raw, err := json.Marshal(map[string]bool{"enabled": *req.AutoAccept})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode auto_accept")
			return
		}
		params.AutoAccept = raw
	}
	if req.CapPerHour != nil {
		if *req.CapPerHour < 0 || *req.CapPerHour > triageMaxCapPerHour {
			writeError(w, http.StatusBadRequest, "cap_per_hour must be between 0 (no cap) and 100000")
			return
		}
		params.CapPerHour = pgtype.Int4{Int32: *req.CapPerHour, Valid: true}
	}
	if req.ExpiryDays != nil {
		if *req.ExpiryDays < 0 || *req.ExpiryDays > triageMaxExpiryDays {
			writeError(w, http.StatusBadRequest, "expiry_days must be between 0 (default retention) and 365")
			return
		}
		params.ExpiryDays = pgtype.Int4{Int32: *req.ExpiryDays, Valid: true}
	}

	src, err := h.Queries.UpdateTriageSourceSettings(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "triage source not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update triage source")
		return
	}
	writeJSON(w, http.StatusOK, triageSourceToResponse(src))
}
