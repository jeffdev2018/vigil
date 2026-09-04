package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Runtime pools (K28): ordered families of interchangeable runtimes with an
// explicit degraded last resort. Failover itself lives in
// service/runtime_pool.go; this file is the CRUD and the history view.

const (
	AuditRuntimePoolChanged  = "runtime_pool.changed"
	AuditRuntimePoolAssigned = "runtime_pool.assigned"
	ErrCodeRuntimeNotInPool  = "runtime_not_in_workspace"
)

type RuntimePoolRequest struct {
	Name              string   `json:"name"`
	RuntimeIDs        []string `json:"runtime_ids"`
	DegradedRuntimeID *string  `json:"degraded_runtime_id"`
}

type RuntimePoolResponse struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	RuntimeIDs        []string `json:"runtime_ids"`
	DegradedRuntimeID *string  `json:"degraded_runtime_id"`
	AgentCount        int64    `json:"agent_count"`
	CreatedAt         string   `json:"created_at"`
}

func (h *Handler) runtimePoolToResponse(r *http.Request, pool db.RuntimePool) RuntimePoolResponse {
	count, _ := h.Queries.CountAgentsUsingRuntimePool(r.Context(), pgtype.UUID{Bytes: pool.ID.Bytes, Valid: true})
	return RuntimePoolResponse{
		ID: uuidToString(pool.ID), Name: pool.Name, RuntimeIDs: jsonStrings(pool.RuntimeIds), DegradedRuntimeID: uuidToPtr(pool.DegradedRuntimeID),
		AgentCount: count, CreatedAt: timestampToString(pool.CreatedAt),
	}
}

// validatePoolRequest checks every runtime belongs to the workspace and
// returns the ordered, deduplicated ids. A missing runtime is a 422.
func (h *Handler) validatePoolRequest(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, req RuntimePoolRequest) (name string, ids []string, degraded pgtype.UUID, ok bool) {
	name = strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return "", nil, pgtype.UUID{}, false
	}
	check := func(id string) (pgtype.UUID, bool) {
		u, ok := parseUUIDOrBadRequest(w, id, "runtime_id")
		if !ok {
			return pgtype.UUID{}, false
		}
		if _, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{ID: u, WorkspaceID: wsUUID}); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeRuntimeNotInPool, "runtime "+id+" is not in this workspace")
			return pgtype.UUID{}, false
		}
		return u, true
	}
	ids = []string{}
	seen := map[string]bool{}
	for _, id := range req.RuntimeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := check(id); !ok {
			return "", nil, pgtype.UUID{}, false
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if req.DegradedRuntimeID != nil && strings.TrimSpace(*req.DegradedRuntimeID) != "" {
		u, ok := check(strings.TrimSpace(*req.DegradedRuntimeID))
		if !ok {
			return "", nil, pgtype.UUID{}, false
		}
		degraded = u
	}
	if len(ids) == 0 && !degraded.Valid {
		writeError(w, http.StatusBadRequest, "a pool needs at least one runtime")
		return "", nil, pgtype.UUID{}, false
	}
	return name, ids, degraded, true
}

func (h *Handler) loadRuntimePool(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.RuntimePool, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "pool id")
	if !ok {
		return db.RuntimePool{}, false
	}
	pool, err := h.Queries.GetRuntimePool(r.Context(), id)
	if err != nil || pool.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "runtime pool not found")
		return db.RuntimePool{}, false
	}
	return pool, true
}

func (h *Handler) ListRuntimePools(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListRuntimePools(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime pools")
		return
	}
	out := make([]RuntimePoolResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, h.runtimePoolToResponse(r, p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pools": out})
}

func (h *Handler) CreateRuntimePool(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req RuntimePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, ids, degraded, ok := h.validatePoolRequest(w, r, wsUUID, req)
	if !ok {
		return
	}
	pool, err := h.Queries.CreateRuntimePool(r.Context(), db.CreateRuntimePoolParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: name, RuntimeIds: profileJSON(ids), DegradedRuntimeID: degraded, CreatedBy: parseUUID(requestUserID(r)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create runtime pool")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRuntimePoolChanged, "runtime_pool", pool.ID, map[string]any{"name": name, "runtime_ids": ids, "degraded_runtime_id": uuidToPtr(degraded), "created": true}, nil)
	writeJSON(w, http.StatusCreated, h.runtimePoolToResponse(r, pool))
}

func (h *Handler) UpdateRuntimePool(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	pool, ok := h.loadRuntimePool(w, r, wsUUID)
	if !ok {
		return
	}
	var req RuntimePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = pool.Name
	}
	name, ids, degraded, ok := h.validatePoolRequest(w, r, wsUUID, req)
	if !ok {
		return
	}
	updated, err := h.Queries.UpdateRuntimePool(r.Context(), db.UpdateRuntimePoolParams{ID: pool.ID, Name: name, RuntimeIds: profileJSON(ids), DegradedRuntimeID: degraded})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update runtime pool")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRuntimePoolChanged, "runtime_pool", pool.ID, map[string]any{"name": name, "runtime_ids": ids, "degraded_runtime_id": uuidToPtr(degraded)}, nil)
	writeJSON(w, http.StatusOK, h.runtimePoolToResponse(r, updated))
}

func (h *Handler) DeleteRuntimePool(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	pool, ok := h.loadRuntimePool(w, r, wsUUID)
	if !ok {
		return
	}
	if n, err := h.Queries.CountAgentsUsingRuntimePool(r.Context(), pgtype.UUID{Bytes: pool.ID.Bytes, Valid: true}); err != nil || n > 0 {
		writeError(w, http.StatusConflict, "agents still target this pool; move them first")
		return
	}
	if err := h.Queries.DeleteRuntimePool(r.Context(), pool.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime pool")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRuntimePoolChanged, "runtime_pool", pool.ID, map[string]any{"name": pool.Name, "deleted": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// SetAgentRuntimePool: PUT /api/agents/{id}/runtime-pool {pool_id|null}.
func (h *Handler) SetAgentRuntimePool(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	var req struct {
		PoolID *string `json:"pool_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var poolID pgtype.UUID
	if req.PoolID != nil && strings.TrimSpace(*req.PoolID) != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.PoolID, "pool_id")
		if !ok {
			return
		}
		pool, err := h.Queries.GetRuntimePool(r.Context(), id)
		if err != nil || pool.WorkspaceID != agent.WorkspaceID {
			writeError(w, http.StatusNotFound, "runtime pool not found")
			return
		}
		poolID = pool.ID
	}
	updated, err := h.Queries.SetAgentRuntimePool(r.Context(), db.SetAgentRuntimePoolParams{ID: agent.ID, RuntimePoolID: poolID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign runtime pool")
		return
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", requestUserID(r), AuditRuntimePoolAssigned, "agent", agent.ID, map[string]any{"pool_id": uuidToPtr(poolID)}, nil)
	writeJSON(w, http.StatusOK, h.agentToResponse(updated))
}

type TaskFailoverResponse struct {
	TaskID   string                  `json:"task_id"`
	Status   string                  `json:"status"`
	Degraded bool                    `json:"degraded"`
	Reason   string                  `json:"failure_reason,omitempty"`
	Moves    []service.FailoverEntry `json:"moves"`
}

// ListIssueFailoverHistory: GET /api/issues/{id}/failover-history.
func (h *Handler) ListIssueFailoverHistory(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssueTaskFailovers(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failover history")
		return
	}
	out := make([]TaskFailoverResponse, 0, len(rows))
	for _, t := range rows {
		var moves []service.FailoverEntry
		_ = json.Unmarshal(t.FailoverHistory, &moves)
		if moves == nil {
			moves = []service.FailoverEntry{}
		}
		out = append(out, TaskFailoverResponse{TaskID: uuidToString(t.ID), Status: t.Status, Degraded: service.TaskDegraded(t.FailoverHistory), Reason: t.FailureReason.String, Moves: moves})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}
