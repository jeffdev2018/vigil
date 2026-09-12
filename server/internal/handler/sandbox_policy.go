package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/sandboxpolicy"
)

// Sandbox policies (JEF-256): declarative confinement rules layered workspace
// (settings->'sandbox_policy', no endpoint) < project (project_sandbox_policy)
// < issue (issue.metadata->'sandbox_policy'), merged most-restrictive-first and
// resolved into the claim's SandboxSpec at claim time. These endpoints manage
// the project and issue layers; the merge itself lives in pkg/sandboxpolicy.

// issueSandboxOverrideMetadataKey is where the per-issue override lives.
const issueSandboxOverrideMetadataKey = "sandbox_policy"

// decodeSandboxPolicy validates the frozen SandboxPolicy shape. The host rule
// is the runtime's own: an allowlist entry must be a host name, and hosts are
// only meaningful — and therefore only storable — under network_mode
// "allowlist".
func decodeSandboxPolicy(w http.ResponseWriter, r *http.Request) (sandboxpolicy.Policy, bool) {
	var req sandboxpolicy.Policy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return sandboxpolicy.Policy{}, false
	}
	switch req.NetworkMode {
	case sandboxpolicy.NetworkUnrestricted, sandboxpolicy.NetworkAllowlist, sandboxpolicy.NetworkNone:
	default:
		writeError(w, http.StatusBadRequest, "network_mode must be unrestricted, allowlist or none")
		return sandboxpolicy.Policy{}, false
	}
	p := sandboxpolicy.Normalize(req)
	if p.NetworkMode != sandboxpolicy.NetworkAllowlist && len(p.AllowedHosts) > 0 {
		writeError(w, http.StatusBadRequest, "allowed_hosts must be empty unless network_mode is allowlist")
		return sandboxpolicy.Policy{}, false
	}
	for _, host := range p.AllowedHosts {
		if !sandboxpolicy.ValidateHost(host) {
			writeError(w, http.StatusBadRequest, host+" is not a host name")
			return sandboxpolicy.Policy{}, false
		}
	}
	if len(p.AllowedHosts) > sandboxpolicy.MaxHosts {
		writeError(w, http.StatusBadRequest, "at most 50 allowed hosts")
		return sandboxpolicy.Policy{}, false
	}
	return p, true
}

// workspaceSandboxLayer reads the workspace default; nil when unset.
func (h *Handler) workspaceSandboxLayer(ctx context.Context, wsID pgtype.UUID) *sandboxpolicy.Policy {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return nil
	}
	return sandboxpolicy.FromSettings(ws.Settings)
}

// projectSandboxLayer reads the project's explicit policy; nil when none.
func (h *Handler) projectSandboxLayer(ctx context.Context, projectID pgtype.UUID) *sandboxpolicy.Policy {
	if !projectID.Valid {
		return nil
	}
	row, err := h.Queries.GetProjectSandboxPolicy(ctx, projectID)
	if err != nil {
		return nil
	}
	p := sandboxpolicy.Policy{NetworkMode: row.NetworkMode, AllowedHosts: sandboxHosts(row.AllowedHosts), BlockSensitiveFiles: row.BlockSensitiveFiles}
	return &p
}

// GET /api/projects/{id}/sandbox-policy — the project's layer and what it
// resolves to over the workspace default.
func (h *Handler) GetProjectSandboxPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, _, ok := h.blastProject(w, r)
	if !ok {
		return
	}
	policy := h.projectSandboxLayer(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{
		"policy":    policy,
		"effective": sandboxpolicy.Merge(h.workspaceSandboxLayer(r.Context(), wsUUID), policy),
	})
}

// PUT /api/projects/{id}/sandbox-policy (owner/admin) — store the layer.
func (h *Handler) PutProjectSandboxPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, userID, ok := h.blastProject(w, r, "owner", "admin")
	if !ok {
		return
	}
	p, ok := decodeSandboxPolicy(w, r)
	if !ok {
		return
	}
	hosts, _ := json.Marshal(p.AllowedHosts)
	if _, err := h.Queries.UpsertProjectSandboxPolicy(r.Context(), db.UpsertProjectSandboxPolicyParams{
		ProjectID: projectID, NetworkMode: p.NetworkMode, AllowedHosts: hosts, BlockSensitiveFiles: p.BlockSensitiveFiles,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the sandbox policy")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "sandbox_policy.updated", "project", projectID, map[string]any{"network_mode": p.NetworkMode, "allowed_hosts": p.AllowedHosts, "block_sensitive_files": p.BlockSensitiveFiles}, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"policy":    p,
		"effective": sandboxpolicy.Merge(h.workspaceSandboxLayer(r.Context(), wsUUID), &p),
	})
}

// DELETE /api/projects/{id}/sandbox-policy (owner/admin) — back to inheriting.
func (h *Handler) DeleteProjectSandboxPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, projectID, userID, ok := h.blastProject(w, r, "owner", "admin")
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteProjectSandboxPolicy(r.Context(), projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the sandbox policy")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "sandbox_policy.deleted", "project", projectID, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// issueSandboxWorkspace gates an issue-scoped override write the same way a
// project policy write is gated: confinement policy is an owner/admin
// decision, reads stay open to every member.
func (h *Handler) issueSandboxWorkspace(w http.ResponseWriter, r *http.Request, roles ...string) (db.Issue, string, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Issue{}, "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Issue{}, "", false
	}
	if len(roles) > 0 {
		if _, ok := h.requireWorkspaceRole(w, r, uuidToString(issue.WorkspaceID), "workspace not found", roles...); !ok {
			return db.Issue{}, "", false
		}
	}
	return issue, userID, true
}

// GET /api/issues/{id}/sandbox-override — the issue's layer and the full
// workspace < project < issue resolution.
func (h *Handler) GetIssueSandboxOverride(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.issueSandboxWorkspace(w, r)
	if !ok {
		return
	}
	override := sandboxpolicy.FromMetadata(issue.Metadata)
	writeJSON(w, http.StatusOK, map[string]any{
		"override":  override,
		"effective": sandboxpolicy.Merge(h.workspaceSandboxLayer(r.Context(), issue.WorkspaceID), h.projectSandboxLayer(r.Context(), issue.ProjectID), override),
	})
}

// PUT /api/issues/{id}/sandbox-override (owner/admin) — store the override.
func (h *Handler) PutIssueSandboxOverride(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.issueSandboxWorkspace(w, r, "owner", "admin")
	if !ok {
		return
	}
	p, ok := decodeSandboxPolicy(w, r)
	if !ok {
		return
	}
	raw, _ := json.Marshal(p)
	if _, err := h.Queries.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{
		Key: issueSandboxOverrideMetadataKey, Value: raw, ID: issue.ID, WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the sandbox override")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, "sandbox_override.updated", "issue", issue.ID, map[string]any{"network_mode": p.NetworkMode, "allowed_hosts": p.AllowedHosts, "block_sensitive_files": p.BlockSensitiveFiles}, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"override":  p,
		"effective": sandboxpolicy.Merge(h.workspaceSandboxLayer(r.Context(), issue.WorkspaceID), h.projectSandboxLayer(r.Context(), issue.ProjectID), &p),
	})
}

// DELETE /api/issues/{id}/sandbox-override (owner/admin) — back to inheriting.
func (h *Handler) DeleteIssueSandboxOverride(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.issueSandboxWorkspace(w, r, "owner", "admin")
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteIssueMetadataKey(r.Context(), db.DeleteIssueMetadataKeyParams{
		Key: issueSandboxOverrideMetadataKey, ID: issue.ID, WorkspaceID: issue.WorkspaceID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to delete the sandbox override")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, "sandbox_override.deleted", "issue", issue.ID, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
