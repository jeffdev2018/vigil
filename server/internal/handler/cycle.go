package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Dated cycles (F29): a project's time-boxed iteration.
//
// Two things make this more than a date range on a project:
//
//   - human and agent capacity are declared and computed SEPARATELY. A sprint
//     that fits its humans can still be impossible for its agents, and a single
//     "team capacity" number hides exactly that. Which side an issue loads is
//     the assignee side: `member` is human, `agent`/`squad` are agent, and an
//     unassigned issue loads neither — it is reported on its own so the two
//     bars never quietly absorb it.
//   - the burndown is materialized daily (cycle_snapshot). There is no status
//     history table in this schema, so a burndown cannot be reconstructed after
//     the fact; the snapshot IS the history.
//
// Overlapping cycles are allowed by design (a support cycle beside a feature
// cycle). Rollover targets "the next cycle by start_date", which stays well
// defined under overlap.

const (
	cycleStatusUpcoming = "upcoming"
	cycleStatusActive   = "active"
	cycleStatusClosed   = "closed"

	// cycleLoadUnitIssues is what a cycle without a load property counts: one
	// issue is one unit. Reported so the UI can say so rather than implying a
	// story-point total nobody entered.
	cycleLoadUnitIssues   = "issues"
	cycleLoadUnitProperty = "property"

	cycleNameMaxRunes = 80

	// ErrCodeCycleProjectMismatch is returned when an issue write names a cycle
	// belonging to another project. A cycle plans ONE project's work, so
	// accepting a foreign issue would make its burndown describe nothing.
	ErrCodeCycleProjectMismatch = "cycle_project_mismatch"

	activityCycleRolledOver = "cycle_rolled_over"
	inboxTypeCycleOrphaned  = "cycle_rollover_orphaned"
)

type CycleCapacitySide struct {
	// Capacity is what was declared; null means "not declared", which is
	// different from zero and must not render as an over-capacity bar.
	Capacity *int32  `json:"capacity"`
	Load     float64 `json:"load"`
}

type CycleCapacityResponse struct {
	Human CycleCapacitySide `json:"human"`
	Agent CycleCapacitySide `json:"agent"`
	// UnassignedLoad is work in the cycle with nobody on it. Kept out of both
	// sides: assigning it later moves it into one of them, and folding it into
	// either now would make the bars lie in the meantime.
	UnassignedLoad float64 `json:"unassigned_load"`
}

type CycleResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	Rollover    bool    `json:"rollover"`
	ClosedAt    *string `json:"closed_at"`
	// Status is derived from the dates and closed_at, never stored: a cycle
	// becomes active by the calendar turning over, not by a write.
	Status string `json:"status"`
	// Late is an open cycle whose end date has passed — the rollover job has
	// not swept it yet, or its project lead has not closed it.
	Late           bool                  `json:"late"`
	LoadUnit       string                `json:"load_unit"`
	LoadPropertyID *string               `json:"load_property_id"`
	IssueCount     int64                 `json:"issue_count"`
	DoneCount      int64                 `json:"done_count"`
	Capacity       CycleCapacityResponse `json:"capacity"`
	CreatedAt      string                `json:"created_at"`
	UpdatedAt      string                `json:"updated_at"`
}

// numericToFloat converts a pgtype.Numeric to float64. An unset or NaN value
// is 0: a load column is a sum, and a missing sum means no load.
func numericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN || n.Int == nil {
		return 0
	}
	f := new(big.Float).SetInt(n.Int)
	if n.Exp != 0 {
		f.Mul(f, big.NewFloat(0).SetFloat64(pow10(int(n.Exp))))
	}
	out, _ := f.Float64()
	return out
}

func pow10(exp int) float64 {
	v := 1.0
	if exp >= 0 {
		for i := 0; i < exp; i++ {
			v *= 10
		}
		return v
	}
	for i := 0; i < -exp; i++ {
		v /= 10
	}
	return v
}

func cycleLoadUnit(c db.Cycle) string {
	if c.LoadPropertyID.Valid {
		return cycleLoadUnitProperty
	}
	return cycleLoadUnitIssues
}

// cycleStatus is the calendar view of a cycle relative to `today` (the
// server's UTC calendar day). Closed wins over the dates: a cycle closed early
// is closed, not active.
func cycleStatus(c db.Cycle, today time.Time) string {
	if c.ClosedAt.Valid {
		return cycleStatusClosed
	}
	if c.StartDate.Valid && today.Before(c.StartDate.Time) {
		return cycleStatusUpcoming
	}
	return cycleStatusActive
}

func cycleIsLate(c db.Cycle, today time.Time) bool {
	return !c.ClosedAt.Valid && c.EndDate.Valid && c.EndDate.Time.Before(today)
}

func cycleToResponse(c db.Cycle, today time.Time) CycleResponse {
	start, end := "", ""
	if d := dateToPtr(c.StartDate); d != nil {
		start = *d
	}
	if d := dateToPtr(c.EndDate); d != nil {
		end = *d
	}
	return CycleResponse{
		ID:             uuidToString(c.ID),
		WorkspaceID:    uuidToString(c.WorkspaceID),
		ProjectID:      uuidToString(c.ProjectID),
		Name:           c.Name,
		Description:    c.Description,
		StartDate:      start,
		EndDate:        end,
		Rollover:       c.Rollover,
		ClosedAt:       timestampToPtr(c.ClosedAt),
		Status:         cycleStatus(c, today),
		Late:           cycleIsLate(c, today),
		LoadUnit:       cycleLoadUnit(c),
		LoadPropertyID: uuidToPtr(c.LoadPropertyID),
		Capacity: CycleCapacityResponse{
			Human: CycleCapacitySide{Capacity: int4ToPtr(c.HumanCapacity)},
			Agent: CycleCapacitySide{Capacity: int4ToPtr(c.AgentCapacity)},
		},
		CreatedAt: timestampToString(c.CreatedAt),
		UpdatedAt: timestampToString(c.UpdatedAt),
	}
}

// todayUTC is the calendar day every date comparison in this file uses. Cycle
// dates are calendar days with no timezone (same contract as issue.due_date),
// so the comparison has to happen in one fixed calendar or a cycle would start
// and end at different moments for different readers.
func todayUTC() time.Time {
	n := time.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// cycleLoadPropertyKey is the GetCycleStats parameter: the load property's id
// as text, or "" when the cycle counts issues.
func cycleLoadPropertyKey(c db.Cycle) string {
	if !c.LoadPropertyID.Valid {
		return ""
	}
	return uuidToString(c.LoadPropertyID)
}

// fillCycleStats loads counts and loads for cycles that share ONE load
// property (the property is per-cycle, so the caller groups by it). Best
// effort: a stats failure leaves zeros rather than failing the listing.
func (h *Handler) fillCycleStats(ctx context.Context, wsID pgtype.UUID, cycles []db.Cycle, resp []CycleResponse) {
	byKey := map[string][]pgtype.UUID{}
	for _, c := range cycles {
		k := cycleLoadPropertyKey(c)
		byKey[k] = append(byKey[k], c.ID)
	}
	terminal := h.projectTerminalIssueStatusKeys(ctx, wsID)
	stats := map[string]db.GetCycleStatsRow{}
	for key, ids := range byKey {
		rows, err := h.Queries.GetCycleStats(ctx, db.GetCycleStatsParams{
			TerminalStatusKeys: terminal,
			LoadPropertyKey:    key,
			WorkspaceID:        wsID,
			CycleIds:           ids,
		})
		if err != nil {
			slog.Warn("cycles: stats failed", "error", err, "workspace_id", uuidToString(wsID))
			continue
		}
		for _, row := range rows {
			stats[uuidToString(row.CycleID)] = row
		}
	}
	for i := range resp {
		row, ok := stats[resp[i].ID]
		if !ok {
			continue
		}
		resp[i].IssueCount = row.TotalCount
		resp[i].DoneCount = row.DoneCount
		resp[i].Capacity.Human.Load = numericToFloat(row.HumanLoad)
		resp[i].Capacity.Agent.Load = numericToFloat(row.AgentLoad)
		resp[i].Capacity.UnassignedLoad = numericToFloat(row.UnassignedLoad)
	}
}

// loadCycleForUser resolves the {id} path param inside the request's
// workspace. Cycles have no human-readable id, so this is a uuid lookup — but
// it stays a loader so every write below uses the RESOLVED row's ids rather
// than the raw path string (see CLAUDE.md, Backend UUID Rules).
func (h *Handler) loadCycleForUser(w http.ResponseWriter, r *http.Request) (db.Cycle, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.Cycle{}, pgtype.UUID{}, false
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return db.Cycle{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "cycle id")
	if !ok {
		return db.Cycle{}, pgtype.UUID{}, false
	}
	cycle, err := h.Queries.GetCycleInWorkspace(r.Context(), db.GetCycleInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "cycle not found")
		return db.Cycle{}, pgtype.UUID{}, false
	}
	return cycle, wsUUID, true
}

// GET /api/cycles?project_id=&status=active|upcoming|closed
func (h *Handler) ListCycles(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return
	}
	var projectFilter pgtype.UUID
	if p := r.URL.Query().Get("project_id"); p != "" {
		id, ok := parseUUIDOrBadRequest(w, p, "project_id")
		if !ok {
			return
		}
		projectFilter = id
	}
	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" && statusFilter != cycleStatusActive && statusFilter != cycleStatusUpcoming && statusFilter != cycleStatusClosed {
		writeError(w, http.StatusBadRequest, "invalid status; valid values: active, upcoming, closed")
		return
	}
	cycles, err := h.Queries.ListCycles(r.Context(), db.ListCyclesParams{WorkspaceID: wsUUID, ProjectID: projectFilter})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cycles")
		return
	}
	today := todayUTC()
	// Status is derived, so it is filtered here rather than in SQL. Cycle
	// counts are per project and small; this is not a scan worth an index.
	kept := make([]db.Cycle, 0, len(cycles))
	for _, c := range cycles {
		if statusFilter != "" && cycleStatus(c, today) != statusFilter {
			continue
		}
		kept = append(kept, c)
	}
	resp := make([]CycleResponse, len(kept))
	for i, c := range kept {
		resp[i] = cycleToResponse(c, today)
	}
	h.fillCycleStats(r.Context(), wsUUID, kept, resp)
	writeJSON(w, http.StatusOK, map[string]any{"cycles": resp, "total": len(resp)})
}

// GET /api/cycles/{id}
func (h *Handler) GetCycle(w http.ResponseWriter, r *http.Request) {
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.singleCycleResponse(r.Context(), wsUUID, cycle))
}

func (h *Handler) singleCycleResponse(ctx context.Context, wsUUID pgtype.UUID, cycle db.Cycle) CycleResponse {
	resp := []CycleResponse{cycleToResponse(cycle, todayUTC())}
	h.fillCycleStats(ctx, wsUUID, []db.Cycle{cycle}, resp)
	return resp[0]
}

type cycleWriteRequest struct {
	ProjectID      *string `json:"project_id"`
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	StartDate      *string `json:"start_date"`
	EndDate        *string `json:"end_date"`
	HumanCapacity  *int32  `json:"human_capacity"`
	AgentCapacity  *int32  `json:"agent_capacity"`
	LoadPropertyID *string `json:"load_property_id"`
	Rollover       *bool   `json:"rollover"`
}

func decodeCycleRequest(w http.ResponseWriter, r *http.Request) (cycleWriteRequest, map[string]json.RawMessage, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return cycleWriteRequest{}, nil, false
	}
	body, _ := json.Marshal(raw)
	var req cycleWriteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return cycleWriteRequest{}, nil, false
	}
	return req, raw, true
}

// validateCycleLoadProperty checks a load_property_id names a NUMBER property
// of this workspace. Anything else would make every load 0 with no signal.
func (h *Handler) validateCycleLoadProperty(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	if strings.TrimSpace(raw) == "" {
		return pgtype.UUID{}, true
	}
	id, ok := parseUUIDOrBadRequest(w, raw, "load_property_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	prop, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "load property not found in this workspace")
		return pgtype.UUID{}, false
	}
	if prop.Type != "number" {
		writeError(w, http.StatusBadRequest, "load property must be a number property")
		return pgtype.UUID{}, false
	}
	return id, true
}

// applyCycleRequest folds the body onto params. Absent fields keep params;
// an explicit null clears a nullable one.
func (h *Handler) applyCycleRequest(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, req cycleWriteRequest, raw map[string]json.RawMessage, params *db.UpdateCycleParams) bool {
	if req.Name != nil {
		params.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		params.Description = strings.TrimSpace(*req.Description)
	}
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return false
		}
		params.StartDate = d
	}
	if req.EndDate != nil && *req.EndDate != "" {
		d, err := util.ParseCalendarDate(*req.EndDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid end_date format, expected YYYY-MM-DD")
			return false
		}
		params.EndDate = d
	}
	if req.Rollover != nil {
		params.Rollover = *req.Rollover
	}
	if _, touched := raw["human_capacity"]; touched {
		params.HumanCapacity = pgtype.Int4{}
		if req.HumanCapacity != nil {
			if *req.HumanCapacity < 0 {
				writeError(w, http.StatusBadRequest, "human_capacity must be zero or more")
				return false
			}
			params.HumanCapacity = pgtype.Int4{Int32: *req.HumanCapacity, Valid: true}
		}
	}
	if _, touched := raw["agent_capacity"]; touched {
		params.AgentCapacity = pgtype.Int4{}
		if req.AgentCapacity != nil {
			if *req.AgentCapacity < 0 {
				writeError(w, http.StatusBadRequest, "agent_capacity must be zero or more")
				return false
			}
			params.AgentCapacity = pgtype.Int4{Int32: *req.AgentCapacity, Valid: true}
		}
	}
	if _, touched := raw["load_property_id"]; touched {
		params.LoadPropertyID = pgtype.UUID{}
		if req.LoadPropertyID != nil {
			id, ok := h.validateCycleLoadProperty(w, r, wsUUID, *req.LoadPropertyID)
			if !ok {
				return false
			}
			params.LoadPropertyID = id
		}
	}
	return true
}

// validateCycleParams applies the guards shared by create and update. The
// start <= end rule is also a CHECK; validating here turns it into a readable
// 400 instead of a constraint violation.
func validateCycleParams(w http.ResponseWriter, params db.UpdateCycleParams) bool {
	if params.Name == "" || len([]rune(params.Name)) > cycleNameMaxRunes {
		writeError(w, http.StatusBadRequest, "name is required (80 characters max)")
		return false
	}
	if !params.StartDate.Valid || !params.EndDate.Valid {
		writeError(w, http.StatusBadRequest, "start_date and end_date are required")
		return false
	}
	if params.EndDate.Time.Before(params.StartDate.Time) {
		writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return false
	}
	return true
}

// POST /api/cycles
func (h *Handler) CreateCycle(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	req, raw, ok := decodeCycleRequest(w, r)
	if !ok {
		return
	}
	if req.ProjectID == nil || strings.TrimSpace(*req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "project not found in this workspace")
		return
	}
	// A cycle plans the project's work, so writing one is a project write.
	if !h.requireProjectWrite(w, r, project.ID) {
		return
	}
	params := db.UpdateCycleParams{WorkspaceID: wsUUID, Rollover: true}
	if !h.applyCycleRequest(w, r, wsUUID, req, raw, &params) || !validateCycleParams(w, params) {
		return
	}
	creator, _ := h.parseUserUUIDOrZero(userID)
	cycle, err := h.Queries.CreateCycle(r.Context(), db.CreateCycleParams{
		WorkspaceID: wsUUID, ProjectID: project.ID, Name: params.Name, Description: params.Description,
		StartDate: params.StartDate, EndDate: params.EndDate, HumanCapacity: params.HumanCapacity,
		AgentCapacity: params.AgentCapacity, LoadPropertyID: params.LoadPropertyID, Rollover: params.Rollover,
		CreatedBy: creator,
	})
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "cycle create rejected: a field value failed a database constraint")
			return
		}
		slog.Error("cycle create failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create cycle")
		return
	}
	resp := h.singleCycleResponse(r.Context(), wsUUID, cycle)
	h.audit(r.Context(), wsUUID, "member", userID, "cycle.created", "cycle", cycle.ID, map[string]any{"name": cycle.Name, "project_id": uuidToString(cycle.ProjectID)}, nil)
	h.publish(protocol.EventCycleCreated, workspaceID, "member", userID, map[string]any{"cycle": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// PATCH /api/cycles/{id}
func (h *Handler) UpdateCycle(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, cycle.ProjectID) {
		return
	}
	req, raw, ok := decodeCycleRequest(w, r)
	if !ok {
		return
	}
	params := db.UpdateCycleParams{
		ID: cycle.ID, WorkspaceID: wsUUID, Name: cycle.Name, Description: cycle.Description,
		StartDate: cycle.StartDate, EndDate: cycle.EndDate, HumanCapacity: cycle.HumanCapacity,
		AgentCapacity: cycle.AgentCapacity, LoadPropertyID: cycle.LoadPropertyID, Rollover: cycle.Rollover,
	}
	if !h.applyCycleRequest(w, r, wsUUID, req, raw, &params) || !validateCycleParams(w, params) {
		return
	}
	updated, err := h.Queries.UpdateCycle(r.Context(), params)
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "cycle update rejected: a field value failed a database constraint")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update cycle")
		return
	}
	resp := h.singleCycleResponse(r.Context(), wsUUID, updated)
	h.audit(r.Context(), wsUUID, "member", userID, "cycle.updated", "cycle", updated.ID, map[string]any{"name": updated.Name}, nil)
	h.publish(protocol.EventCycleUpdated, uuidToString(wsUUID), "member", userID, map[string]any{"cycle": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DELETE /api/cycles/{id}: the cycle, its snapshots and its issues' membership
// go in one transaction. Issues themselves survive — a plan is deleted, not
// the work it planned.
func (h *Handler) DeleteCycle(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, cycle.ProjectID) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.ClearIssueCycleByCycle(r.Context(), db.ClearIssueCycleByCycleParams{CycleID: cycle.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach issues")
		return
	}
	if err := qtx.DeleteCycleSnapshotsByCycle(r.Context(), db.DeleteCycleSnapshotsByCycleParams{CycleID: cycle.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cycle history")
		return
	}
	if err := qtx.DeleteIssueViewsByProjectScope(r.Context(), db.DeleteIssueViewsByProjectScopeParams{WorkspaceID: wsUUID, ScopeID: cycle.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cycle views")
		return
	}
	if err := qtx.DeleteCycle(r.Context(), db.DeleteCycleParams{ID: cycle.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cycle")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit cycle delete")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "cycle.deleted", "cycle", cycle.ID, map[string]any{"name": cycle.Name}, nil)
	h.publish(protocol.EventCycleDeleted, uuidToString(wsUUID), "member", userID, map[string]any{"cycle_id": uuidToString(cycle.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/cycles/{id}/close: freeze the burndown and, when the cycle rolls
// over, move its unfinished work ONCE. Closing is idempotent — the SQL keeps
// the original closed_at — but the rollover runs only on the transition, so a
// second call cannot move work twice.
func (h *Handler) CloseCycle(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, cycle.ProjectID) {
		return
	}
	if cycle.ClosedAt.Valid {
		writeError(w, http.StatusConflict, "cycle is already closed")
		return
	}
	// A last snapshot before the freeze, so the burndown's final day is the
	// state the cycle actually closed on rather than the previous night's.
	if err := h.snapshotCycle(r.Context(), cycle, todayUTC()); err != nil {
		slog.Warn("cycle close: final snapshot failed", "error", err, "cycle_id", uuidToString(cycle.ID))
	}
	moved, orphaned := 0, 0
	if cycle.Rollover {
		moved, orphaned = h.rolloverCycle(r.Context(), cycle, "member", userID)
	}
	closed, err := h.Queries.CloseCycle(r.Context(), db.CloseCycleParams{ID: cycle.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close cycle")
		return
	}
	resp := h.singleCycleResponse(r.Context(), wsUUID, closed)
	h.audit(r.Context(), wsUUID, "member", userID, "cycle.closed", "cycle", closed.ID, map[string]any{"rolled_over": moved, "orphaned": orphaned}, nil)
	h.publish(protocol.EventCycleUpdated, uuidToString(wsUUID), "member", userID, map[string]any{"cycle": resp})
	writeJSON(w, http.StatusOK, map[string]any{"cycle": resp, "rolled_over": moved, "orphaned": orphaned})
}

// --- Burndown ------------------------------------------------------------

type CycleBurndownDay struct {
	Date string `json:"date"`
	// Remaining* are null on days the series cannot speak for: the future
	// (nothing has happened yet) and, before the first snapshot, the past. A
	// zero there would read as "everything was done", which is the opposite.
	RemainingCount *int64   `json:"remaining_count"`
	RemainingLoad  *float64 `json:"remaining_load"`
	IdealCount     float64  `json:"ideal_count"`
	IdealLoad      float64  `json:"ideal_load"`
	HumanLoad      *float64 `json:"human_load"`
	AgentLoad      *float64 `json:"agent_load"`
}

type CycleBurndownCapacity struct {
	Human *int32 `json:"human"`
	Agent *int32 `json:"agent"`
}

type CycleBurndownResponse struct {
	Days     []CycleBurndownDay    `json:"days"`
	Capacity CycleBurndownCapacity `json:"capacity"`
	LoadUnit string                `json:"load_unit"`
	// LoadPropertyID is echoed so a client can name the unit it is charting.
	LoadPropertyID *string `json:"load_property_id"`
	// ApproximateBefore is the first day the series has real history for.
	// Earlier days are flat-filled from it. Absent when every day is real.
	ApproximateBefore *string `json:"approximate_before,omitempty"`
}

// GET /api/cycles/{id}/burndown
//
// One entry per calendar day from start_date to end_date, no gaps. The rules,
// which the UI states back to the reader:
//
//   - a PAST day uses its snapshot; a day with no snapshot carries the last
//     known value forward, and days before the FIRST snapshot are flat-filled
//     from it with `approximate_before` naming where real history starts.
//     Snapshots are the only history this schema keeps — a cycle created today
//     genuinely has no yesterday.
//   - TODAY is computed live from the current issues, so the chart agrees with
//     the board even between snapshot runs.
//   - a FUTURE day carries the ideal line only; remaining_* is null.
//
// The ideal line runs from today's total down to zero on the last day. It is
// deliberately anchored to the CURRENT total rather than the total on day one:
// scope added mid-cycle should raise the bar the team is measured against,
// not silently disappear from it.
func (h *Handler) GetCycleBurndown(w http.ResponseWriter, r *http.Request) {
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	if !cycle.StartDate.Valid || !cycle.EndDate.Valid {
		writeError(w, http.StatusInternalServerError, "cycle has no dates")
		return
	}
	snapshots, err := h.Queries.ListCycleSnapshots(r.Context(), db.ListCycleSnapshotsParams{CycleID: cycle.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load cycle history")
		return
	}
	live, err := h.cycleLiveStats(r.Context(), wsUUID, cycle)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute cycle progress")
		return
	}
	resp := buildCycleBurndown(cycle, snapshots, live, todayUTC())
	writeJSON(w, http.StatusOK, resp)
}

// cycleLiveStats is the cycle's state right now. Zero-valued when the cycle
// has no issues (GetCycleStats groups, so an empty cycle returns no row).
func (h *Handler) cycleLiveStats(ctx context.Context, wsID pgtype.UUID, cycle db.Cycle) (db.GetCycleStatsRow, error) {
	rows, err := h.Queries.GetCycleStats(ctx, db.GetCycleStatsParams{
		TerminalStatusKeys: h.projectTerminalIssueStatusKeys(ctx, wsID),
		LoadPropertyKey:    cycleLoadPropertyKey(cycle),
		WorkspaceID:        wsID,
		CycleIds:           []pgtype.UUID{cycle.ID},
	})
	if err != nil {
		return db.GetCycleStatsRow{}, err
	}
	if len(rows) == 0 {
		return db.GetCycleStatsRow{CycleID: cycle.ID}, nil
	}
	return rows[0], nil
}

// buildCycleBurndown is pure: dates in, series out. Kept out of the handler so
// the past/today/future rules can be tested without a database.
func buildCycleBurndown(cycle db.Cycle, snapshots []db.CycleSnapshot, live db.GetCycleStatsRow, today time.Time) CycleBurndownResponse {
	start := cycle.StartDate.Time.UTC()
	end := cycle.EndDate.Time.UTC()
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		days = 1
	}

	byDate := make(map[string]db.CycleSnapshot, len(snapshots))
	firstSnapshot := ""
	for _, s := range snapshots {
		if !s.SnapshotDate.Valid {
			continue
		}
		key := s.SnapshotDate.Time.UTC().Format("2006-01-02")
		byDate[key] = s
		if firstSnapshot == "" || key < firstSnapshot {
			firstSnapshot = key
		}
	}

	totalCount := float64(live.TotalCount)
	totalLoad := numericToFloat(live.TotalLoad)
	liveRemainingCount := live.TotalCount - live.DoneCount
	liveRemainingLoad := numericToFloat(live.TotalLoad) - numericToFloat(live.DoneLoad)
	liveHuman := numericToFloat(live.HumanLoad)
	liveAgent := numericToFloat(live.AgentLoad)

	out := CycleBurndownResponse{
		Days: make([]CycleBurndownDay, 0, days),
		Capacity: CycleBurndownCapacity{
			Human: int4ToPtr(cycle.HumanCapacity),
			Agent: int4ToPtr(cycle.AgentCapacity),
		},
		LoadUnit:       cycleLoadUnit(cycle),
		LoadPropertyID: uuidToPtr(cycle.LoadPropertyID),
	}

	todayKey := today.Format("2006-01-02")
	// A day before the first snapshot has no history at all. Flat-fill it from
	// the first snapshot (or from today, when there is none yet) and say so.
	var approximateBefore *string
	if firstSnapshot != "" {
		if firstSnapshot > start.Format("2006-01-02") {
			v := firstSnapshot
			approximateBefore = &v
		}
	} else if todayKey > start.Format("2006-01-02") {
		v := todayKey
		approximateBefore = &v
	}
	out.ApproximateBefore = approximateBefore

	// Carried value: the last real observation walking forward. Seeded with
	// the earliest one there is so the flat-fill has something to draw.
	var carryCount *int64
	var carryLoad, carryHuman, carryAgent *float64
	if firstSnapshot != "" {
		s := byDate[firstSnapshot]
		c := s.TotalCount - s.DoneCount
		l := numericToFloat(s.TotalLoad) - numericToFloat(s.DoneLoad)
		hu := numericToFloat(s.HumanLoad)
		ag := numericToFloat(s.AgentLoad)
		carryCount, carryLoad, carryHuman, carryAgent = &c, &l, &hu, &ag
	} else {
		c, l, hu, ag := liveRemainingCount, liveRemainingLoad, liveHuman, liveAgent
		carryCount, carryLoad, carryHuman, carryAgent = &c, &l, &hu, &ag
	}

	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i)
		key := date.Format("2006-01-02")
		// Linear ideal: full on the first day, zero on the last.
		frac := 0.0
		if days > 1 {
			frac = float64(days-1-i) / float64(days-1)
		}
		day := CycleBurndownDay{
			Date:       key,
			IdealCount: totalCount * frac,
			IdealLoad:  totalLoad * frac,
		}
		switch {
		case key > todayKey:
			// Future: ideal only.
		case key == todayKey:
			c, l, hu, ag := liveRemainingCount, liveRemainingLoad, liveHuman, liveAgent
			day.RemainingCount, day.RemainingLoad, day.HumanLoad, day.AgentLoad = &c, &l, &hu, &ag
			carryCount, carryLoad, carryHuman, carryAgent = &c, &l, &hu, &ag
		default:
			if s, ok := byDate[key]; ok {
				c := s.TotalCount - s.DoneCount
				l := numericToFloat(s.TotalLoad) - numericToFloat(s.DoneLoad)
				hu := numericToFloat(s.HumanLoad)
				ag := numericToFloat(s.AgentLoad)
				carryCount, carryLoad, carryHuman, carryAgent = &c, &l, &hu, &ag
			}
			c, l, hu, ag := *carryCount, *carryLoad, *carryHuman, *carryAgent
			day.RemainingCount, day.RemainingLoad, day.HumanLoad, day.AgentLoad = &c, &l, &hu, &ag
		}
		out.Days = append(out.Days, day)
	}
	return out
}

// --- Snapshot and rollover ----------------------------------------------

// snapshotCycle writes (or overwrites) today's row for one cycle. Idempotent
// on (cycle_id, snapshot_date), so re-running the job the same day is safe.
func (h *Handler) snapshotCycle(ctx context.Context, cycle db.Cycle, day time.Time) error {
	stats, err := h.cycleLiveStats(ctx, cycle.WorkspaceID, cycle)
	if err != nil {
		return err
	}
	return h.Queries.UpsertCycleSnapshot(ctx, db.UpsertCycleSnapshotParams{
		CycleID:      cycle.ID,
		SnapshotDate: pgtype.Date{Time: day, Valid: true},
		WorkspaceID:  cycle.WorkspaceID,
		TotalCount:   stats.TotalCount,
		DoneCount:    stats.DoneCount,
		TotalLoad:    stats.TotalLoad,
		DoneLoad:     stats.DoneLoad,
		HumanLoad:    stats.HumanLoad,
		AgentLoad:    stats.AgentLoad,
	})
}

// SnapshotCycles is the daily burndown materializer, registered as the
// cycle_snapshot scheduler job. Global: it walks every started, unclosed cycle
// in every workspace, because the burndown of a cycle nobody opened today is
// exactly the one that would otherwise have a hole in it.
func (h *Handler) SnapshotCycles(ctx context.Context) (int, error) {
	today := todayUTC()
	cycles, err := h.Queries.ListOpenCyclesForSnapshot(ctx, pgtype.Date{Time: today, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("list open cycles: %w", err)
	}
	written := 0
	for _, c := range cycles {
		if err := h.snapshotCycle(ctx, c, today); err != nil {
			slog.Warn("cycle snapshot failed", "error", err, "cycle_id", uuidToString(c.ID))
			continue
		}
		written++
	}
	return written, nil
}

// rolloverCycle moves the cycle's unfinished issues to the next cycle of the
// same project, or detaches them when there is none. Returns (moved,
// orphaned). Best effort per issue: one failure does not abandon the rest.
func (h *Handler) rolloverCycle(ctx context.Context, cycle db.Cycle, actorType, actorID string) (int, int) {
	terminal, err := h.terminalIssueStatusKeys(ctx, cycle.WorkspaceID)
	if err != nil {
		terminal = []string{issuestatus.Done, issuestatus.Cancelled}
	}
	issues, err := h.Queries.ListUnfinishedCycleIssues(ctx, db.ListUnfinishedCycleIssuesParams{
		WorkspaceID: cycle.WorkspaceID, CycleID: cycle.ID, TerminalStatusKeys: terminal,
	})
	if err != nil {
		slog.Warn("cycle rollover: list unfinished failed", "error", err, "cycle_id", uuidToString(cycle.ID))
		return 0, 0
	}
	if len(issues) == 0 {
		return 0, 0
	}
	next, nextErr := h.Queries.FindNextCycle(ctx, db.FindNextCycleParams{
		WorkspaceID: cycle.WorkspaceID, ProjectID: cycle.ProjectID, ID: cycle.ID, StartDate: cycle.StartDate,
	})
	target := pgtype.UUID{}
	targetID := ""
	if nextErr == nil {
		target = next.ID
		targetID = uuidToString(next.ID)
	}
	moved, orphaned := 0, 0
	for _, issue := range issues {
		if err := h.Queries.SetIssueCycle(ctx, db.SetIssueCycleParams{ID: issue.ID, WorkspaceID: cycle.WorkspaceID, CycleID: target}); err != nil {
			slog.Warn("cycle rollover: move failed", "error", err, "issue_id", uuidToString(issue.ID))
			continue
		}
		details, _ := json.Marshal(map[string]any{
			"from_cycle_id": uuidToString(cycle.ID),
			"to_cycle_id":   targetID,
			"cycle_name":    cycle.Name,
		})
		if _, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
			ID:          dbid.NewV7(),
			WorkspaceID: cycle.WorkspaceID,
			IssueID:     issue.ID,
			ActorType:   pgtype.Text{String: "system", Valid: true},
			Action:      activityCycleRolledOver,
			Details:     details,
		}); err != nil {
			slog.Warn("cycle rollover: journal failed", "error", err, "issue_id", uuidToString(issue.ID))
		}
		if target.Valid {
			moved++
		} else {
			orphaned++
		}
	}
	if orphaned > 0 {
		h.notifyCycleRolloverOrphaned(ctx, cycle, orphaned)
	}
	h.publish(protocol.EventCycleUpdated, uuidToString(cycle.WorkspaceID), actorType, actorID, map[string]any{
		"cycle_id": uuidToString(cycle.ID), "rolled_over": moved, "orphaned": orphaned,
	})
	return moved, orphaned
}

// notifyCycleRolloverOrphaned tells the project lead that work left a cycle
// with nowhere to go. Silent when the project has no human lead: an inbox item
// addressed to nobody is worse than none.
func (h *Handler) notifyCycleRolloverOrphaned(ctx context.Context, cycle db.Cycle, orphaned int) {
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: cycle.ProjectID, WorkspaceID: cycle.WorkspaceID})
	if err != nil || project.LeadType.String != "member" || !project.LeadID.Valid {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"cycle_id":   uuidToString(cycle.ID),
		"project_id": uuidToString(cycle.ProjectID),
		"count":      orphaned,
	})
	item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   cycle.WorkspaceID,
		RecipientType: "member",
		RecipientID:   project.LeadID,
		Type:          inboxTypeCycleOrphaned,
		Severity:      "attention",
		Title:         fmt.Sprintf("%d unfinished issues left %q with no next cycle", orphaned, cycle.Name),
		Body:          pgtype.Text{String: "They are no longer in a cycle. Create the next cycle and assign them, or plan them elsewhere.", Valid: true},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		Details:       details,
	})
	if err != nil {
		slog.Warn("cycle rollover: inbox failed", "error", err, "cycle_id", uuidToString(cycle.ID))
		return
	}
	h.publish(protocol.EventInboxNew, uuidToString(cycle.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
}

// RolloverCycles is the cycle_rollover scheduler job: every cycle whose end
// date has passed and that nobody closed. A cycle with rollover=false is still
// closed here — a cycle that ended has ended either way — but its work stays
// put.
func (h *Handler) RolloverCycles(ctx context.Context) (int, error) {
	cycles, err := h.Queries.ListCyclesDueForRollover(ctx, pgtype.Date{Time: todayUTC(), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("list cycles due for rollover: %w", err)
	}
	closed := 0
	for _, c := range cycles {
		if c.Rollover {
			h.rolloverCycle(ctx, c, "system", "")
		}
		if _, err := h.Queries.CloseCycle(ctx, db.CloseCycleParams{ID: c.ID, WorkspaceID: c.WorkspaceID}); err != nil {
			slog.Warn("cycle rollover: close failed", "error", err, "cycle_id", uuidToString(c.ID))
			continue
		}
		closed++
	}
	return closed, nil
}

// --- Issue membership -----------------------------------------------------

var errCycleProjectMismatch = errors.New("cycle belongs to another project")

// resolveIssueCycle validates a cycle_id an issue write names against the
// project the issue will be in. A cycle plans one project's work; accepting a
// foreign issue would make its burndown describe something that is not it.
func (h *Handler) resolveIssueCycle(ctx context.Context, wsUUID pgtype.UUID, raw string, projectID pgtype.UUID) (pgtype.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return pgtype.UUID{}, nil
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		return pgtype.UUID{}, err
	}
	cycle, err := h.Queries.GetCycleInWorkspace(ctx, db.GetCycleInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		return pgtype.UUID{}, err
	}
	if !projectID.Valid || cycle.ProjectID != projectID {
		return pgtype.UUID{}, errCycleProjectMismatch
	}
	return cycle.ID, nil
}

// applyIssueCycleWrite settles the cycle_id half of an issue write. Returns
// false after writing the response, like the other issue-write guards.
func (h *Handler) applyIssueCycleWrite(w http.ResponseWriter, wsUUID pgtype.UUID, issue *db.Issue, raw *string, ctx context.Context) bool {
	value := ""
	if raw != nil {
		value = *raw
	}
	cycleID, err := h.resolveIssueCycle(ctx, wsUUID, value, issue.ProjectID)
	switch {
	case errors.Is(err, errCycleProjectMismatch):
		writeErrorCode(w, http.StatusConflict, ErrCodeCycleProjectMismatch,
			"this cycle belongs to another project; move the issue first or pick one of its project's cycles")
		return false
	case err != nil:
		writeError(w, http.StatusBadRequest, "cycle not found in this workspace")
		return false
	}
	if err := h.Queries.SetIssueCycle(ctx, db.SetIssueCycleParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, CycleID: cycleID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set cycle")
		return false
	}
	issue.CycleID = cycleID
	return true
}

// --- Goal progress (F29 initiatives) -------------------------------------

type GoalProjectProgress struct {
	ProjectID  string `json:"project_id"`
	Name       string `json:"name"`
	TotalCount int64  `json:"total_count"`
	DoneCount  int64  `json:"done_count"`
}

// GET /api/goals/{id}/progress
//
// The aggregate a cross-project initiative needs and a goal did not have: one
// row per linked project plus the roll-up. Done is decided by the workspace
// status catalogue, so a custom done-category status counts — never a literal
// 'done' comparison.
func (h *Handler) GetGoalProgress(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	goal, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{ID: goalID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}
	rows, err := h.Queries.GetGoalProjectProgress(r.Context(), db.GetGoalProjectProgressParams{
		TerminalStatusKeys: h.projectTerminalIssueStatusKeys(r.Context(), wsUUID),
		WorkspaceID:        wsUUID,
		GoalID:             goal.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute goal progress")
		return
	}
	projects := make([]GoalProjectProgress, 0, len(rows))
	var total, done int64
	for _, row := range rows {
		projects = append(projects, GoalProjectProgress{
			ProjectID:  uuidToString(row.ProjectID),
			Name:       row.ProjectTitle,
			TotalCount: row.TotalCount,
			DoneCount:  row.DoneCount,
		})
		total += row.TotalCount
		done += row.DoneCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"goal_id":     uuidToString(goal.ID),
		"projects":    projects,
		"total_count": total,
		"done_count":  done,
	})
}
