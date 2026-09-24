package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workspace-admin control over which of a plugin's agent-tool hooks an agent
// may actually reach.
//
// Deny by default: an installed, enabled plugin's hooks are not offered to any
// agent until someone binds them here. Mirrors ListAgentMcpServers /
// AddAgentMcpServer / RemoveAgentMcpServer in workspace_mcp_api.go — same
// route shape, same writer permission (agent owner or workspace owner/admin,
// never an agent actor) — because a plugin's tool binding is the same kind of
// per-agent grant a workspace MCP server binding already is.

// requireAgentPluginToolWriter resolves the agent and enforces who may change
// its plugin tool bindings. Same rule as requireAgentMcpWriter.
func (h *Handler) requireAgentPluginToolWriter(w http.ResponseWriter, r *http.Request) (db.Agent, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify plugin tool assignments")
		return db.Agent{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Agent{}, false
	}
	if !canViewAgentSecrets(agent, requestUserID(r), member.Role) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Agent{}, false
	}
	return agent, true
}

// agentPluginToolResponse is one row of GET /api/agents/{id}/plugin-tools.
type agentPluginToolResponse struct {
	InstallationID string `json:"installation_id"`
	PluginKey      string `json:"plugin_key"`
	HookKey        string `json:"hook_key"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Transport      string `json:"transport"`
	Bound          bool   `json:"bound"`
}

// ListAgentPluginTools — GET /api/agents/{id}/plugin-tools
//
// Every agent-trigger hook of an enabled installation in the workspace, with
// whether THIS agent is currently bound to it. A read, so it uses the same
// loader as ListAgentMcpServers rather than the writer guard — seeing what is
// available and bound is not the same permission as changing it.
func (h *Handler) ListAgentPluginTools(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.PluginService == nil || !h.pluginsV1Enabled(r.Context()) {
		writeJSON(w, http.StatusOK, []agentPluginToolResponse{})
		return
	}
	tools, err := h.PluginService.AvailableAgentPluginTools(r.Context(), agent.WorkspaceID, agent.ID)
	if err != nil {
		writePluginError(w, err, "failed to list the agent's plugin tools")
		return
	}
	resp := make([]agentPluginToolResponse, 0, len(tools))
	for _, tool := range tools {
		resp = append(resp, agentPluginToolResponse{
			InstallationID: tool.InstallationID,
			PluginKey:      tool.PluginKey,
			HookKey:        tool.HookKey,
			Name:           tool.Name,
			Description:    tool.Description,
			Transport:      tool.Transport,
			Bound:          tool.Bound,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// BindAgentPluginTool — PUT /api/agents/{id}/plugin-tools/{installationId}/{hookKey}
//
// The only way this binding is created — installing or enabling a plugin
// never implies it. Idempotent: binding an already-bound tool is a 200, not a
// conflict (idx_agent_plugin_tool_binding + ON CONFLICT DO NOTHING).
func (h *Handler) BindAgentPluginTool(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	agent, ok := h.requireAgentPluginToolWriter(w, r)
	if !ok {
		return
	}
	hookKey := chi.URLParam(r, "hookKey")
	installation, err := h.PluginService.InstallationForWorkspace(r.Context(), agent.WorkspaceID, chi.URLParam(r, "installationId"))
	if err != nil {
		writePluginError(w, err, "failed to load the Plugin")
		return
	}
	if _, err := service.FindHook(installation, hookKey); err != nil {
		writePluginError(w, err, "hook not found")
		return
	}
	if _, err := h.Queries.BindAgentPluginTool(r.Context(), db.BindAgentPluginToolParams{
		WorkspaceID:    agent.WorkspaceID,
		AgentID:        agent.ID,
		InstallationID: installation.ID,
		HookKey:        hookKey,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// ErrNoRows here means ON CONFLICT DO NOTHING fired: the binding
		// already existed. RETURNING then has nothing to hand back, which the
		// :one query reports the same way it would report a genuine miss —
		// but a miss is impossible on an INSERT, so this is exactly the
		// "already bound" case the doc comment above promises is a no-op.
		writeError(w, http.StatusInternalServerError, "failed to bind the plugin tool")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bound": true})
}

// UnbindAgentPluginTool — DELETE /api/agents/{id}/plugin-tools/{installationId}/{hookKey}
//
// Removing a binding that does not exist is a no-op 204, same as
// RemoveAgentMcpServer's sibling behavior for an unassigned server would be
// unremarkable — the caller's desired end state (not bound) already holds.
func (h *Handler) UnbindAgentPluginTool(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentPluginToolWriter(w, r)
	if !ok {
		return
	}
	installationUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	if err := h.Queries.UnbindAgentPluginTool(r.Context(), db.UnbindAgentPluginToolParams{
		AgentID:        agent.ID,
		InstallationID: installationUUID,
		HookKey:        chi.URLParam(r, "hookKey"),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind the plugin tool")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
