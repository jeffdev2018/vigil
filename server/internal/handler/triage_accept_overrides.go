package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

// parseTriageAcceptOverrides validates the optional "Accept as…" body of
// POST /api/triage/items/{id}/accept. The body is optional: an empty or
// absent body means "inherit everything from the origin", which is the
// pre-existing behavior and what the batch endpoint keeps doing.
func parseTriageAcceptOverrides(w http.ResponseWriter, r *http.Request) (triageAcceptOverrides, bool) {
	var req struct {
		AssigneeType string   `json:"assignee_type"`
		AssigneeID   string   `json:"assignee_id"`
		ProjectID    string   `json:"project_id"`
		Priority     string   `json:"priority"`
		Labels       []string `json:"labels"`
	}
	// A missing body is not an error here, but a malformed one is: silently
	// accepting with inherited defaults would hide the caller's mistake.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return triageAcceptOverrides{}, false
	}

	var ov triageAcceptOverrides
	switch req.AssigneeType {
	case "":
		if req.AssigneeID != "" {
			writeError(w, http.StatusBadRequest, "assignee_type is required with assignee_id")
			return ov, false
		}
	case "member", "agent":
		id, ok := parseUUIDOrBadRequest(w, req.AssigneeID, "assignee_id")
		if !ok {
			return ov, false
		}
		ov.AssigneeType = pgtype.Text{String: req.AssigneeType, Valid: true}
		ov.AssigneeID = id
	default:
		writeError(w, http.StatusBadRequest, "assignee_type must be one of: member, agent")
		return ov, false
	}

	if req.ProjectID != "" {
		id, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return ov, false
		}
		ov.ProjectID = id
	}

	switch req.Priority {
	case "":
	case "urgent", "high", "medium", "low", "none":
		ov.Priority = req.Priority
	default:
		writeError(w, http.StatusBadRequest, "priority must be one of: urgent, high, medium, low, none")
		return ov, false
	}

	for _, raw := range req.Labels {
		id, ok := parseUUIDOrBadRequest(w, raw, "labels")
		if !ok {
			return ov, false
		}
		ov.LabelIDs = append(ov.LabelIDs, id)
	}
	return ov, true
}
