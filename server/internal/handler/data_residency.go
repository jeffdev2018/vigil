package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Data residency routing (K46). Two settings, one policy: the workspace
// declares where its work may run, and each runtime declares where it runs.
// The enqueue path (service/residency.go) is what enforces the pair; these
// endpoints only read and write the declarations.

const (
	// AuditDataResidencyPolicy records a change to the workspace policy.
	AuditDataResidencyPolicy = "data_residency.policy"
	// AuditRuntimeCompliance records a runtime's declaration being set or cleared.
	AuditRuntimeCompliance = "data_residency.runtime_declaration"
	// AuditResidencyDispatchBlocked records a trigger refused because the
	// policy left the agent nowhere compliant to run.
	AuditResidencyDispatchBlocked = "residency.dispatch_blocked"
	// InboxTypeResidencyBlocked tells the accountable humans that a run was
	// refused on residency grounds. It is deliberately its own type: a policy
	// refusal is fixed by declaring a runtime or relaxing the policy, not by
	// rebinding the agent like a routing_alert.
	InboxTypeResidencyBlocked = "residency_policy_blocked"

	// maxRuntimeRegionLen caps a declared region name. Cloud region names top
	// out around 20 characters; 64 is generous headroom.
	maxRuntimeRegionLen = 64
)

// dataResidencyResponse is the policy plus the list cap, so the form does not
// carry its own copy of the bound.
type dataResidencyResponse struct {
	service.DataResidencyPolicy
	MaxListLength int `json:"max_list_length"`
}

func dataResidencyBody(policy service.DataResidencyPolicy) dataResidencyResponse {
	return dataResidencyResponse{DataResidencyPolicy: policy, MaxListLength: service.DataResidencyPolicyRange()}
}

// GetDataResidencyPolicy: GET /api/data-residency.
func (h *Handler) GetDataResidencyPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, dataResidencyBody(service.DataResidencyPolicyFromSettings(ws.Settings)))
}

// PutDataResidencyPolicy: PUT /api/data-residency
// {region_allowlist, banned_providers, require_on_prem}.
//
// Tokens are normalized (trimmed, lowercased, deduplicated, sorted) before the
// cap is applied, so twenty spellings of one region are one entry rather than
// twenty and a rejection.
func (h *Handler) PutDataResidencyPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.DataResidencyPolicy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	policy := service.NormalizeDataResidencyPolicy(req)
	if !service.ValidDataResidencyPolicy(policy) {
		writeError(w, http.StatusBadRequest, dataResidencyRangeMessage())
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	settings["data_residency_policy"] = policy
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the data residency policy")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditDataResidencyPolicy, "workspace", wsUUID, map[string]any{
		"region_allowlist": policy.RegionAllowlist,
		"banned_providers": policy.BannedProviders,
		"require_on_prem":  policy.RequireOnPrem,
	}, nil)
	writeJSON(w, http.StatusOK, dataResidencyBody(policy))
}

func dataResidencyRangeMessage() string {
	return "region_allowlist and banned_providers must each hold at most 20 entries"
}

// runtimeComplianceRequest is the body of PUT /api/runtimes/{runtimeId}/compliance.
type runtimeComplianceRequest struct {
	Region string `json:"region"`
	OnPrem bool   `json:"on_prem"`
}

// PutRuntimeCompliance: PUT /api/runtimes/{runtimeId}/compliance {region, on_prem}.
//
// Admin-only, and never verified: the declaration is an operator's statement
// about their own machine. What the platform guarantees is that the statement
// is what routing uses, not that it is true.
func (h *Handler) PutRuntimeCompliance(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.loadRuntimeForComplianceWrite(w, r)
	if !ok {
		return
	}
	var req runtimeComplianceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	region := strings.ToLower(strings.TrimSpace(req.Region))
	if region == "" || len([]rune(region)) > maxRuntimeRegionLen {
		writeError(w, http.StatusBadRequest, "region is required and must be at most 64 characters")
		return
	}
	profile, err := h.Queries.UpsertRuntimeComplianceProfile(r.Context(), db.UpsertRuntimeComplianceProfileParams{
		RuntimeID:  rt.ID,
		Region:     region,
		OnPrem:     req.OnPrem,
		DeclaredBy: parseUUID(requestUserID(r)),
	})
	if err != nil {
		slog.Error("UpsertRuntimeComplianceProfile failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to save the runtime declaration")
		return
	}
	h.audit(r.Context(), rt.WorkspaceID, "member", requestUserID(r), AuditRuntimeCompliance, "runtime", rt.ID, map[string]any{
		"region": region, "on_prem": req.OnPrem,
	}, nil)
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", requestUserID(r), map[string]any{"action": "update"})
	writeJSON(w, http.StatusOK, runtimeToResponseWithCompliance(rt, &profile))
}

// DeleteRuntimeCompliance: DELETE /api/runtimes/{runtimeId}/compliance.
// Clearing a declaration makes the runtime ineligible again under any
// restrictive policy — the fail-closed default.
func (h *Handler) DeleteRuntimeCompliance(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.loadRuntimeForComplianceWrite(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteRuntimeComplianceProfile(r.Context(), rt.ID); err != nil {
		slog.Error("DeleteRuntimeComplianceProfile failed", "error", err, "runtime_id", uuidToString(rt.ID))
		writeError(w, http.StatusInternalServerError, "failed to clear the runtime declaration")
		return
	}
	h.audit(r.Context(), rt.WorkspaceID, "member", requestUserID(r), AuditRuntimeCompliance, "runtime", rt.ID, map[string]any{
		"cleared": true,
	}, nil)
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", requestUserID(r), map[string]any{"action": "update"})
	writeJSON(w, http.StatusOK, runtimeToResponseWithCompliance(rt, nil))
}

// loadRuntimeForComplianceWrite resolves the runtime from the path and gates
// the write on workspace admin. A declaration is a compliance statement for
// the whole workspace, so unlike the sandbox settings it is not the runtime
// owner's to make.
func (h *Handler) loadRuntimeForComplianceWrite(w http.ResponseWriter, r *http.Request) (db.AgentRuntime, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "runtimeId"), "runtime_id")
	if !ok {
		return db.AgentRuntime{}, false
	}
	rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return db.AgentRuntime{}, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(rt.WorkspaceID), "runtime not found", "owner", "admin"); !ok {
		return db.AgentRuntime{}, false
	}
	return rt, true
}

// runtimeComplianceProfiles loads the declarations for a runtime set in one
// query, keyed by runtime id. A read failure degrades to "nothing declared"
// rather than failing the list: the declarations decorate the response, the
// routing decision reads them for itself.
func (h *Handler) runtimeComplianceProfiles(ctx context.Context, runtimes []db.AgentRuntime) map[string]*db.RuntimeComplianceProfile {
	out := map[string]*db.RuntimeComplianceProfile{}
	if len(runtimes) == 0 {
		return out
	}
	ids := make([]pgtype.UUID, 0, len(runtimes))
	for _, rt := range runtimes {
		ids = append(ids, rt.ID)
	}
	rows, err := h.Queries.ListRuntimeComplianceProfiles(ctx, ids)
	if err != nil {
		slog.Warn("ListRuntimeComplianceProfiles failed", "error", err)
		return out
	}
	for i := range rows {
		out[uuidToString(rows[i].RuntimeID)] = &rows[i]
	}
	return out
}

// runtimeResponse builds one runtime's response with its declaration attached.
func (h *Handler) runtimeResponse(ctx context.Context, rt db.AgentRuntime) AgentRuntimeResponse {
	profile, err := h.Queries.GetRuntimeComplianceProfile(ctx, rt.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("GetRuntimeComplianceProfile failed", "error", err, "runtime_id", uuidToString(rt.ID))
		}
		return runtimeToResponseWithCompliance(rt, nil)
	}
	return runtimeToResponseWithCompliance(rt, &profile)
}
