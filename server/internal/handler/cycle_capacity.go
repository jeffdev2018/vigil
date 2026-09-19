package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-actor cycle capacity and velocity (JEF-246).
//
// The cycle row's human_capacity / agent_capacity are the aggregate plan; this
// file is the per-person, per-agent breakdown: "an agent counts for X velocity
// points" is a declared row in cycle_actor_capacity, and the velocity report
// measures each actor's done load against it.
//
// An actor is the same identity an issue assignee is: actor_type 'member'
// carries a USER id (issue.assignee_id for a member assignee is the user id),
// 'agent' an agent id. Squad-assigned and unassigned work has no single actor
// to credit, so velocity reports it as one `other_done_points` bucket rather
// than smearing it across people who did not do it.

const (
	cycleActorTypeMember = "member"
	cycleActorTypeAgent  = "agent"

	// cycleCapacityMaxPoints mirrors the CHECK on cycle_actor_capacity.points;
	// validated here so an over-limit write is a readable 400, not a constraint
	// violation.
	cycleCapacityMaxPoints = 10000
	// cycleCapacityMaxRows bounds one replace so a runaway client cannot turn
	// a settings write into a bulk insert.
	cycleCapacityMaxRows = 200
)

type CycleActorCapacityEntry struct {
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Name      string `json:"name"`
	Points    int32  `json:"points"`
}

type cycleActorCapacityWrite struct {
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Points    *int32 `json:"points"`
}

func cycleCapacityEntryToResponse(row db.ListCycleActorCapacitiesRow) CycleActorCapacityEntry {
	return CycleActorCapacityEntry{
		ActorType: row.ActorType,
		ActorID:   uuidToString(row.ActorID),
		Name:      row.Name,
		Points:    row.Points,
	}
}

// GET /api/cycles/{id}/capacities: the cycle's declared per-actor capacities.
// Member read — this is a plan anyone on the workspace can look at.
func (h *Handler) GetCycleCapacities(w http.ResponseWriter, r *http.Request) {
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListCycleActorCapacities(r.Context(), db.ListCycleActorCapacitiesParams{
		CycleID: cycle.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cycle capacities")
		return
	}
	out := make([]CycleActorCapacityEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, cycleCapacityEntryToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"capacities": out})
}

// PUT /api/cycles/{id}/capacities: FULL REPLACE of the cycle's declared
// capacities — the body is the new set, so an empty list clears it. Write gate
// is the cycle's own: whoever may edit the cycle may restate its plan.
func (h *Handler) PutCycleCapacities(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Capacities []cycleActorCapacityWrite `json:"capacities"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Capacities == nil {
		body.Capacities = []cycleActorCapacityWrite{}
	}
	if len(body.Capacities) > cycleCapacityMaxRows {
		writeError(w, http.StatusBadRequest, "at most 200 capacity rows per cycle")
		return
	}
	type actorKey struct {
		typ string
		id  pgtype.UUID
	}
	seen := make(map[actorKey]struct{}, len(body.Capacities))
	rows := make([]db.InsertCycleActorCapacityParams, 0, len(body.Capacities))
	for _, entry := range body.Capacities {
		if entry.ActorType != cycleActorTypeMember && entry.ActorType != cycleActorTypeAgent {
			writeError(w, http.StatusBadRequest, "actor_type must be member or agent")
			return
		}
		actorID, ok := parseUUIDOrBadRequest(w, entry.ActorID, "actor_id")
		if !ok {
			return
		}
		if entry.Points == nil {
			writeError(w, http.StatusBadRequest, "points is required")
			return
		}
		if *entry.Points < 0 || *entry.Points > cycleCapacityMaxPoints {
			writeError(w, http.StatusBadRequest, "points must be between 0 and 10000")
			return
		}
		key := actorKey{typ: entry.ActorType, id: actorID}
		if _, dup := seen[key]; dup {
			writeError(w, http.StatusBadRequest, "duplicate actor in capacities")
			return
		}
		seen[key] = struct{}{}
		// A declared capacity for an actor that is not in the workspace would
		// render as a name nobody can read; refuse it rather than storing it.
		switch entry.ActorType {
		case cycleActorTypeMember:
			if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID: actorID, WorkspaceID: wsUUID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "actor is not a member of this workspace")
				return
			}
		case cycleActorTypeAgent:
			if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
				ID: actorID, WorkspaceID: wsUUID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "actor is not an agent of this workspace")
				return
			}
		}
		rows = append(rows, db.InsertCycleActorCapacityParams{
			CycleID: cycle.ID, WorkspaceID: wsUUID,
			ActorType: entry.ActorType, ActorID: actorID, Points: *entry.Points,
		})
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteCycleActorCapacitiesByCycle(r.Context(), db.DeleteCycleActorCapacitiesByCycleParams{
		CycleID: cycle.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace cycle capacities")
		return
	}
	for _, row := range rows {
		if _, err := qtx.InsertCycleActorCapacity(r.Context(), row); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to replace cycle capacities")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit cycle capacities")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "cycle.capacities_updated", "cycle", cycle.ID,
		map[string]any{"name": cycle.Name, "rows": len(rows)}, nil)
	stored, err := h.Queries.ListCycleActorCapacities(r.Context(), db.ListCycleActorCapacitiesParams{
		CycleID: cycle.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cycle capacities")
		return
	}
	out := make([]CycleActorCapacityEntry, 0, len(stored))
	for _, row := range stored {
		out = append(out, cycleCapacityEntryToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"capacities": out})
}

// --- Velocity --------------------------------------------------------------

type CycleVelocityActor struct {
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Name      string `json:"name"`
	// CapacityPoints is the declared capacity; null means undeclared, which is
	// different from zero and must not render as an over-capacity bar.
	CapacityPoints *int32  `json:"capacity_points"`
	DonePoints     float64 `json:"done_points"`
	DoneCount      int64   `json:"done_count"`
}

type CycleVelocityHistoryEntry struct {
	CycleID    string  `json:"cycle_id"`
	Name       string  `json:"name"`
	StartDate  string  `json:"start_date"`
	EndDate    string  `json:"end_date"`
	DonePoints float64 `json:"done_points"`
	DoneCount  int64   `json:"done_count"`
}

type CycleVelocityResponse struct {
	CycleID         string                      `json:"cycle_id"`
	Actors          []CycleVelocityActor        `json:"actors"`
	OtherDonePoints float64                     `json:"other_done_points"`
	History         []CycleVelocityHistoryEntry `json:"history"`
}

// GET /api/cycles/{id}/velocity: done points per actor for this cycle against
// their declared capacity, the squad/unassigned bucket, and the done load of
// the project's recent cycles for cross-cycle comparison. Member read.
func (h *Handler) GetCycleVelocity(w http.ResponseWriter, r *http.Request) {
	cycle, wsUUID, ok := h.loadCycleForUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	terminal := h.projectTerminalIssueStatusKeys(ctx, wsUUID)
	loadKey := cycleLoadPropertyKey(cycle)

	doneRows, err := h.Queries.GetCycleVelocityActors(ctx, db.GetCycleVelocityActorsParams{
		LoadPropertyKey: loadKey, WorkspaceID: wsUUID, CycleID: cycle.ID, TerminalStatusKeys: terminal,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute cycle velocity")
		return
	}
	other, err := h.Queries.GetCycleOtherDonePoints(ctx, db.GetCycleOtherDonePointsParams{
		LoadPropertyKey: loadKey, WorkspaceID: wsUUID, CycleID: cycle.ID, TerminalStatusKeys: terminal,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute cycle velocity")
		return
	}
	capacityRows, err := h.Queries.ListCycleActorCapacities(ctx, db.ListCycleActorCapacitiesParams{
		CycleID: cycle.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load cycle capacities")
		return
	}

	// Actors are the union of "did done work" and "has a declared capacity":
	// either half alone would hide the other — an undeclared contributor would
	// vanish, a declared-but-idle actor would vanish.
	type actorKey struct {
		typ string
		id  string
	}
	actors := map[actorKey]*CycleVelocityActor{}
	var memberIDs, agentIDs []pgtype.UUID
	addNameLookup := func(typ string, id pgtype.UUID) {
		if typ == cycleActorTypeMember {
			memberIDs = append(memberIDs, id)
		} else {
			agentIDs = append(agentIDs, id)
		}
	}
	for _, row := range doneRows {
		if !row.ActorID.Valid {
			continue
		}
		typ := row.ActorType.String
		key := actorKey{typ: typ, id: uuidToString(row.ActorID)}
		actors[key] = &CycleVelocityActor{
			ActorType:  typ,
			ActorID:    key.id,
			DonePoints: numericToFloat(row.DonePoints),
			DoneCount:  row.DoneCount,
		}
		addNameLookup(typ, row.ActorID)
	}
	for _, row := range capacityRows {
		key := actorKey{typ: row.ActorType, id: uuidToString(row.ActorID)}
		actor, ok := actors[key]
		if !ok {
			actor = &CycleVelocityActor{ActorType: row.ActorType, ActorID: key.id, Name: row.Name}
			actors[key] = actor
			if row.Name == "" {
				addNameLookup(row.ActorType, row.ActorID)
			}
		}
		points := row.Points
		actor.CapacityPoints = &points
		if actor.Name == "" {
			actor.Name = row.Name
		}
	}
	if len(memberIDs) > 0 || len(agentIDs) > 0 {
		nameRows, err := h.Queries.ListCycleActorNames(ctx, db.ListCycleActorNamesParams{
			WorkspaceID: wsUUID, MemberIds: memberIDs, AgentIds: agentIDs,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve actor names")
			return
		}
		for _, row := range nameRows {
			if actor, ok := actors[actorKey{typ: row.ActorType, id: uuidToString(row.ActorID)}]; ok && actor.Name == "" {
				actor.Name = row.Name
			}
		}
	}
	out := make([]CycleVelocityActor, 0, len(actors))
	for _, actor := range actors {
		out = append(out, *actor)
	}
	// Same ordering the capacities endpoint freezes: members, then agents,
	// then name.
	sort.Slice(out, func(i, j int) bool {
		if out[i].ActorType != out[j].ActorType {
			return out[i].ActorType == cycleActorTypeMember
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ActorID < out[j].ActorID
	})

	history, err := h.cycleVelocityHistory(ctx, wsUUID, cycle, terminal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute velocity history")
		return
	}
	writeJSON(w, http.StatusOK, CycleVelocityResponse{
		CycleID:         uuidToString(cycle.ID),
		Actors:          out,
		OtherDonePoints: numericToFloat(other),
		History:         history,
	})
}

// cycleVelocityHistory loads the done load of the cycle's sibling cycles (same
// project, or the workspace's other project-less cycles), most recent first.
// Each history cycle's done load uses ITS OWN load property — comparing
// "points" across cycles is only meaningful when each cycle counts in the unit
// it was planned in.
func (h *Handler) cycleVelocityHistory(ctx context.Context, wsUUID pgtype.UUID, cycle db.Cycle, terminal []string) ([]CycleVelocityHistoryEntry, error) {
	siblings, err := h.Queries.ListCycleVelocityHistory(ctx, db.ListCycleVelocityHistoryParams{
		WorkspaceID: wsUUID, ID: cycle.ID, ProjectID: cycle.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	byKey := map[string][]pgtype.UUID{}
	for _, c := range siblings {
		k := cycleLoadPropertyKey(c)
		byKey[k] = append(byKey[k], c.ID)
	}
	stats := map[string]db.GetCycleStatsRow{}
	for key, ids := range byKey {
		rows, err := h.Queries.GetCycleStats(ctx, db.GetCycleStatsParams{
			TerminalStatusKeys: terminal,
			LoadPropertyKey:    key,
			WorkspaceID:        wsUUID,
			CycleIds:           ids,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			stats[uuidToString(row.CycleID)] = row
		}
	}
	out := make([]CycleVelocityHistoryEntry, 0, len(siblings))
	for _, c := range siblings {
		entry := CycleVelocityHistoryEntry{
			CycleID: uuidToString(c.ID),
			Name:    c.Name,
		}
		if d := dateToPtr(c.StartDate); d != nil {
			entry.StartDate = *d
		}
		if d := dateToPtr(c.EndDate); d != nil {
			entry.EndDate = *d
		}
		if row, ok := stats[entry.CycleID]; ok {
			entry.DonePoints = numericToFloat(row.DoneLoad)
			entry.DoneCount = row.DoneCount
		}
		out = append(out, entry)
	}
	return out, nil
}
