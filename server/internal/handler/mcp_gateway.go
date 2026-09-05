package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/mcpgov"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// Governed MCP gateway (K77). A workspace MCP server carries a catalogue of
// its tools classified by risk; a binding (agent_mcp_server) carries a policy
// naming, per tool, whether the agent acts alone, asks a human (K05 gate on
// a K63 decision) or never calls it. The claim hands the daemon the effective
// class of every tool so the local gateway decides each call without a round
// trip; every call comes back as an audit event in the run's replay (K70).

const (
	AuditMcpToolCall     = "run.mcp_tool_call"
	AuditMcpCatalogued   = "mcp.catalogued"
	InboxTypeMcpAlert    = "mcp_alert"
	mcpUnusedToolWindow  = 30 * 24 * time.Hour
	mcpReviewDetailsKind = "mcp_binding_review"
)

// mcpCatalog decodes a server's stored catalogue.
func mcpCatalog(raw []byte) []mcpgov.CatalogTool {
	var tools []mcpgov.CatalogTool
	_ = json.Unmarshal(raw, &tools)
	for i := range tools {
		if tools[i].Risk == "" {
			tools[i].Risk, tools[i].RiskSource = mcpgov.Classify(tools[i].Name, tools[i].Description), "auto"
		}
	}
	return tools
}

func mcpPolicy(raw []byte) mcpgov.Policy {
	var p mcpgov.Policy
	_ = json.Unmarshal(raw, &p)
	if p.Tools == nil {
		p.Tools = map[string]string{}
	}
	return p
}

func mcpUsage(raw []byte) map[string]time.Time {
	var m map[string]time.Time
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]time.Time{}
	}
	return m
}

// mcpMergeCatalog folds a discovery into the stored catalogue: a tool keeps a
// risk set by hand, a new tool is classified by pattern, a tool that went
// missing is dropped.
func mcpMergeCatalog(existing []mcpgov.CatalogTool, discovered []remotemcp.Tool) []mcpgov.CatalogTool {
	manual := map[string]string{}
	for _, t := range existing {
		if t.RiskSource == "manual" {
			manual[t.Name] = t.Risk
		}
	}
	out := make([]mcpgov.CatalogTool, 0, len(discovered))
	for _, d := range discovered {
		t := mcpgov.CatalogTool{Name: d.Name, Description: d.Description, SchemaDigest: d.SchemaDigest, Risk: mcpgov.Classify(d.Name, d.Description), RiskSource: "auto"}
		if risk, ok := manual[d.Name]; ok {
			t.Risk, t.RiskSource = risk, "manual"
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mcpDiscoverConfigured lists the tools of an http workspace server from the
// API server. A stdio server runs on the daemon's machine and is catalogued
// by the daemon at the first run.
func mcpDiscoverConfigured(ctx context.Context, config []byte) ([]remotemcp.Tool, error) {
	var entry struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Command string            `json:"command"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(config, &entry); err != nil {
		return nil, errors.New("the server entry is not an object")
	}
	if entry.URL == "" || entry.Command != "" {
		return nil, errors.New("a stdio server is catalogued by the daemon at its first run; add its tools by hand meanwhile")
	}
	headers := http.Header{}
	for k, v := range entry.Headers {
		headers.Set(k, v)
	}
	tools, _, err := remotemcp.DiscoverEndpoint(ctx, entry.URL, headers)
	return tools, err
}

// McpCatalogToolResponse is one catalogued tool as the UI sees it, with the
// class in force for the agent when listed on a binding.
type McpCatalogToolResponse struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	SchemaDigest string `json:"schema_digest,omitempty"`
	Risk         string `json:"risk"`
	RiskSource   string `json:"risk_source"`
	Class        string `json:"class,omitempty"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
}

func mcpCatalogResponse(tools []mcpgov.CatalogTool) []McpCatalogToolResponse {
	out := make([]McpCatalogToolResponse, 0, len(tools))
	for _, t := range tools {
		out = append(out, McpCatalogToolResponse{Name: t.Name, Description: t.Description, SchemaDigest: t.SchemaDigest, Risk: t.Risk, RiskSource: t.RiskSource})
	}
	return out
}

func (h *Handler) loadWorkspaceMcpServerForAdmin(w http.ResponseWriter, r *http.Request) (db.WorkspaceMcpServer, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.WorkspaceMcpServer{}, false
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return db.WorkspaceMcpServer{}, false
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return db.WorkspaceMcpServer{}, false
	}
	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{ID: serverUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return db.WorkspaceMcpServer{}, false
	}
	return server, true
}

// GET /api/workspaces/{id}/mcp-servers/{serverId}/tools
func (h *Handler) ListWorkspaceMcpServerTools(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{ID: serverUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": mcpCatalogResponse(mcpCatalog(server.Tools)), "discovered_at": mcpTsPtr(server.ToolsDiscoveredAt), "risks": mcpgov.Risks})
}

// POST /api/workspaces/{id}/mcp-servers/{serverId}/tools/discover
func (h *Handler) DiscoverWorkspaceMcpServerTools(w http.ResponseWriter, r *http.Request) {
	server, ok := h.loadWorkspaceMcpServerForAdmin(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	discovered, err := mcpDiscoverConfigured(ctx, server.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "discovery failed: "+err.Error())
		return
	}
	tools := mcpMergeCatalog(mcpCatalog(server.Tools), discovered)
	if err := h.saveMcpCatalog(r.Context(), server, tools, "member", requestUserID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the catalogue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": mcpCatalogResponse(tools), "risks": mcpgov.Risks})
}

func (h *Handler) saveMcpCatalog(ctx context.Context, server db.WorkspaceMcpServer, tools []mcpgov.CatalogTool, actorType, actorID string) error {
	raw, _ := json.Marshal(tools)
	if err := h.Queries.SetWorkspaceMcpServerTools(ctx, db.SetWorkspaceMcpServerToolsParams{ID: server.ID, WorkspaceID: server.WorkspaceID, Tools: raw, ToolsDiscoveredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, t := range tools {
		counts[t.Risk]++
	}
	h.audit(ctx, server.WorkspaceID, actorType, actorID, AuditMcpCatalogued, "workspace_mcp_server", server.ID, map[string]any{"server": server.Name, "tools": len(tools), "by_risk": counts}, nil)
	return nil
}

// PUT /api/workspaces/{id}/mcp-servers/{serverId}/tools — set the catalogue by
// hand: adjust a risk, add a tool the server has not been asked for yet.
func (h *Handler) SetWorkspaceMcpServerTools(w http.ResponseWriter, r *http.Request) {
	server, ok := h.loadWorkspaceMcpServerForAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Risk        string `json:"risk"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing := map[string]mcpgov.CatalogTool{}
	for _, t := range mcpCatalog(server.Tools) {
		existing[t.Name] = t
	}
	tools := make([]mcpgov.CatalogTool, 0, len(req.Tools))
	seen := map[string]bool{}
	for _, in := range req.Tools {
		name := strings.TrimSpace(in.Name)
		if name == "" || seen[name] || len(name) > 200 {
			writeError(w, http.StatusBadRequest, "each tool needs a unique name")
			return
		}
		seen[name] = true
		t := existing[name]
		t.Name = name
		if in.Description != "" {
			t.Description = in.Description
		}
		auto := mcpgov.Classify(name, t.Description)
		switch {
		case in.Risk == "" || in.Risk == auto:
			if t.RiskSource != "manual" {
				t.Risk, t.RiskSource = auto, "auto"
			} else if in.Risk != "" {
				t.Risk = in.Risk
			}
		case mcpContains(mcpgov.Risks, in.Risk):
			t.Risk, t.RiskSource = in.Risk, "manual"
		default:
			writeError(w, http.StatusBadRequest, "unknown risk "+in.Risk)
			return
		}
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	if err := h.saveMcpCatalog(r.Context(), server, tools, "member", requestUserID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the catalogue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": mcpCatalogResponse(tools), "risks": mcpgov.Risks})
}

// agentMcpBindings loads the bindings the Rule of Two is checked over, with
// an optional override for the binding being saved.
func (h *Handler) agentMcpBindings(ctx context.Context, agent db.Agent, overrideServer pgtype.UUID, overridePolicy *mcpgov.Policy, overrideEnabled *bool) ([]mcpgov.Binding, error) {
	rows, err := h.Queries.ListAgentMcpBindings(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	var out []mcpgov.Binding
	for _, row := range rows {
		enabled, policy := row.Enabled, mcpPolicy(row.ToolPolicy)
		if row.ServerID == overrideServer {
			if overrideEnabled != nil {
				enabled = *overrideEnabled
			}
			if overridePolicy != nil {
				policy = *overridePolicy
			}
		}
		if !enabled {
			continue
		}
		b := mcpgov.Binding{Server: row.Name}
		for _, t := range mcpCatalog(row.Tools) {
			b.Tools = append(b.Tools, mcpgov.GatewayTool{Name: t.Name, Risk: t.Risk, Class: policy.Effective(t.Name, t.Risk, agent.TrustMode)})
		}
		out = append(out, b)
	}
	return out, nil
}

// validateAgentMcpBinding refuses a save that would trip the Rule of Two or
// ask the dial for more than it allows.
func (h *Handler) validateAgentMcpBinding(ctx context.Context, agent db.Agent, serverID pgtype.UUID, policy *mcpgov.Policy, enabled *bool) error {
	if policy != nil {
		var catalog []mcpgov.CatalogTool
		if server, err := h.Queries.GetWorkspaceMcpServer(ctx, db.GetWorkspaceMcpServerParams{ID: serverID, WorkspaceID: agent.WorkspaceID}); err == nil {
			catalog = mcpCatalog(server.Tools)
		}
		if err := policy.Validate(catalog, agent.TrustMode); err != nil {
			return err
		}
	}
	bindings, err := h.agentMcpBindings(ctx, agent, serverID, policy, enabled)
	if err != nil {
		return err
	}
	return mcpgov.RuleOfTwo(bindings)
}

// PUT /api/agents/{id}/mcp-servers/{serverId}/policy
func (h *Handler) SetAgentMcpServerPolicy(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	var policy mcpgov.Policy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if policy.Tools == nil {
		policy.Tools = map[string]string{}
	}
	if err := h.validateAgentMcpBinding(r.Context(), agent, serverUUID, &policy, nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, _ := json.Marshal(policy)
	rows, err := h.Queries.SetAgentMcpServerPolicy(r.Context(), db.SetAgentMcpServerPolicyParams{AgentID: agent.ID, ServerID: serverUUID, ToolPolicy: raw})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the policy")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "this server is not bound to the agent")
		return
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", requestUserID(r), "mcp.policy_set", "agent", agent.ID, map[string]any{"server_id": uuidToString(serverUUID), "policy": policy}, nil)
	h.writeAgentMcpServers(w, r, agent)
}

// claimMcpGateway builds the gateway payload of a claim from the agent's
// enabled bindings: every catalogued tool with its effective class, tightened
// by the Rule of Two in case a catalogue moved since the bindings were saved.
func claimMcpGateway(agent db.Agent, bound []db.ListEnabledAgentMcpServersRow) *mcpgov.Gateway {
	gw := &mcpgov.Gateway{TrustMode: agent.TrustMode, Servers: []mcpgov.GatewayServer{}}
	bindings := make([]mcpgov.Binding, 0, len(bound))
	for _, row := range bound {
		policy := mcpPolicy(row.ToolPolicy)
		b := mcpgov.Binding{Server: row.Name, Tools: []mcpgov.GatewayTool{}}
		for _, t := range mcpCatalog(row.Tools) {
			b.Tools = append(b.Tools, mcpgov.GatewayTool{Name: t.Name, Risk: t.Risk, Class: policy.Effective(t.Name, t.Risk, agent.TrustMode)})
		}
		bindings = append(bindings, b)
	}
	mcpgov.Tighten(bindings)
	for i, row := range bound {
		def := mcpPolicy(row.ToolPolicy).Default
		if def == "" {
			def = mcpgov.ClassByRisk
		}
		gw.Servers = append(gw.Servers, mcpgov.GatewayServer{Name: row.Name, ServerID: uuidToString(row.ID), Default: def, Tools: bindings[i].Tools})
	}
	return gw
}

// mcpTask resolves the run and its workspace for the task-scoped routes.
func (h *Handler) mcpTask(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, db.Agent, bool) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.AgentTaskQueue{}, db.Agent{}, false
	}
	return task, agent, true
}

// POST /api/tasks/{taskId}/mcp-catalog — the daemon reports the tools it
// listed on a server it brokered (the only way a stdio server is catalogued).
func (h *Handler) ReportMcpCatalog(w http.ResponseWriter, r *http.Request) {
	_, agent, ok := h.mcpTask(w, r)
	if !ok {
		return
	}
	var req struct {
		Server string           `json:"server"`
		Tools  []remotemcp.Tool `json:"tools"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.Server) == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	server, err := h.Queries.GetWorkspaceMcpServerByName(r.Context(), db.GetWorkspaceMcpServerByNameParams{WorkspaceID: agent.WorkspaceID, Name: req.Server})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"catalogued": false})
		return
	}
	tools := mcpMergeCatalog(mcpCatalog(server.Tools), req.Tools)
	if err := h.saveMcpCatalog(r.Context(), server, tools, "agent", uuidToString(agent.ID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the catalogue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalogued": true, "tools": len(tools)})
}

// McpCallReport is what the daemon's gateway reports after each tools/call.
type McpCallReport struct {
	Server     string   `json:"server"`
	ServerID   string   `json:"server_id"`
	Tool       string   `json:"tool"`
	Risk       string   `json:"risk"`
	Class      string   `json:"class"`
	Result     string   `json:"result"`
	GateID     string   `json:"gate_id"`
	DurationMs int64    `json:"duration_ms"`
	First      bool     `json:"first"`
	Flags      []string `json:"flags"`
}

// POST /api/tasks/{taskId}/mcp-calls — attribution: every call through the
// gateway is an audit event in the run's replay, tracks the tool's last use
// on the binding, and alerts the agent's owner the first time a high-risk
// tool succeeds in a run, approved or not.
func (h *Handler) ReportMcpCall(w http.ResponseWriter, r *http.Request) {
	task, agent, ok := h.mcpTask(w, r)
	if !ok {
		return
	}
	var req McpCallReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.Tool == "" || req.Server == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Flags == nil {
		req.Flags = []string{}
	}
	h.audit(r.Context(), agent.WorkspaceID, "agent", uuidToString(agent.ID), AuditMcpToolCall, "task", task.ID, map[string]any{
		"server": req.Server, "tool": req.Tool, "risk": req.Risk, "class": req.Class, "result": req.Result, "gate_id": req.GateID, "duration_ms": req.DurationMs, "flags": req.Flags,
	}, nil)
	if serverID, err := util.ParseUUID(req.ServerID); err == nil && req.Result == "success" {
		_ = h.Queries.TouchAgentMcpToolUsage(r.Context(), db.TouchAgentMcpToolUsageParams{AgentID: agent.ID, ServerID: serverID, Tool: req.Tool, UsedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	}
	if req.Result == "success" && req.First && mcpgov.HighRisk(req.Risk) && agent.OwnerID.Valid {
		how := "ran alone"
		if req.Class == mcpgov.ClassAsk {
			how = "ran after a human approved it"
		}
		h.mcpAlert(r.Context(), agent.WorkspaceID, agent.OwnerID, task.IssueID,
			fmt.Sprintf("%s used %s/%s", agent.Name, req.Server, req.Tool),
			fmt.Sprintf("The tool is classified %s and %s in this run. Review the binding if this is not expected.", req.Risk, how),
			map[string]any{"kind": "high_risk_call", "agent_id": uuidToString(agent.ID), "task_id": uuidToString(task.ID), "server": req.Server, "tool": req.Tool, "risk": req.Risk, "class": req.Class, "gate_id": req.GateID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true})
}

func (h *Handler) mcpAlert(ctx context.Context, wsID, recipient, issueID pgtype.UUID, title, body string, details map[string]any) {
	raw, _ := json.Marshal(details)
	item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: "member", RecipientID: recipient, Type: InboxTypeMcpAlert, Severity: "action_required",
		IssueID: issueID, Title: truncate(title, 120), Body: pgtype.Text{String: truncate(body, 1000), Valid: true}, Details: raw,
	})
	if err != nil {
		slog.Warn("mcp gateway: inbox failed", "error", err)
		return
	}
	h.publish(protocol.EventInboxNew, uuidToString(wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
}

// ReviewMcpBindings is the monthly review: for every enabled binding, the
// tools nobody used for thirty days are proposed for removal to the agent's
// owner. Nothing is changed by the job itself.
func (h *Handler) ReviewMcpBindings(ctx context.Context, now time.Time) (int, error) {
	rows, err := h.Queries.ListAgentMcpBindingsForReview(ctx)
	if err != nil {
		return 0, err
	}
	acted := 0
	for _, row := range rows {
		if !row.OwnerID.Valid || now.Sub(row.CreatedAt.Time) < mcpUnusedToolWindow {
			continue
		}
		policy, usage := mcpPolicy(row.ToolPolicy), mcpUsage(row.ToolUsage)
		var unused []string
		for _, t := range mcpCatalog(row.Tools) {
			if policy.Effective(t.Name, t.Risk, row.TrustMode) == mcpgov.ClassNever {
				continue
			}
			if last, ok := usage[t.Name]; !ok || now.Sub(last) >= mcpUnusedToolWindow {
				unused = append(unused, t.Name)
			}
		}
		if len(unused) == 0 {
			continue
		}
		sort.Strings(unused)
		h.mcpAlert(ctx, row.WorkspaceID, row.OwnerID, pgtype.UUID{},
			fmt.Sprintf("%s: %d unused tools on %s", row.AgentName, len(unused), row.ServerName),
			"These tools were not called for thirty days. Set them to never on the binding to shrink what the agent can reach: "+strings.Join(unused, ", "),
			map[string]any{"kind": mcpReviewDetailsKind, "agent_id": uuidToString(row.AgentID), "server_id": uuidToString(row.ServerID), "server": row.ServerName, "tools": unused})
		acted++
	}
	return acted, nil
}

// mcpBindingResponse decorates an agent's bound server with its policy and
// the catalogue's effective classes.
func mcpBindingResponse(agent db.Agent, row db.ListAgentMcpServersRow) (mcpgov.Policy, []McpCatalogToolResponse) {
	policy, usage := mcpPolicy(row.ToolPolicy), mcpUsage(row.ToolUsage)
	tools := mcpCatalogResponse(mcpCatalog(row.Tools))
	for i := range tools {
		tools[i].Class = policy.Effective(tools[i].Name, tools[i].Risk, agent.TrustMode)
		if last, ok := usage[tools[i].Name]; ok {
			tools[i].LastUsedAt = last.UTC().Format(time.RFC3339)
		}
	}
	return policy, tools
}

func mcpContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func mcpTsPtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	s := ts.Time.UTC().Format(time.RFC3339)
	return &s
}
