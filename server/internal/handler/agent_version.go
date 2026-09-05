package handler

import (
	"bytes"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Agent versions (K23): every change to what an agent is — instructions,
// model, enabled skills, tool configuration — leaves an immutable, numbered
// snapshot. The agent row stays the live config; the newest snapshot is the
// active version. A rollback writes the old snapshot onto the agent and
// records that as a new version, never a rewrite. A run's version is the one
// active when the run was created, so nothing is written on the run queue.

const (
	ErrCodeVersionAlreadyActive = "version_already_active"
	agentVersionSnapshotRetries = 2
)

// agentToolConfig is the part of the snapshot beyond instructions, model
// and skills. Secrets (custom_env) are deliberately not versioned.
type agentToolConfig struct {
	McpConfig             json.RawMessage `json:"mcp_config,omitempty"`
	CustomArgs            json.RawMessage `json:"custom_args,omitempty"`
	RuntimeConfig         json.RawMessage `json:"runtime_config,omitempty"`
	DisabledRuntimeSkills json.RawMessage `json:"disabled_runtime_skills,omitempty"`
	ThinkingLevel         string          `json:"thinking_level,omitempty"`
	ServiceTier           string          `json:"service_tier,omitempty"`
	ComposioToolkits      []string        `json:"composio_toolkit_allowlist,omitempty"`
}

type agentSnapshot struct {
	Instructions string
	Model        string
	SkillIDs     []string
	ToolConfig   []byte
}

func versionRaw(b []byte) json.RawMessage {
	if len(bytes.TrimSpace(b)) == 0 || string(b) == "null" {
		return nil
	}
	return json.RawMessage(b)
}

func (h *Handler) snapshotOf(ctx context.Context, agent db.Agent) (agentSnapshot, error) {
	skills, err := h.Queries.ListAgentSkillSummaries(ctx, agent.ID)
	if err != nil {
		return agentSnapshot{}, err
	}
	ids := make([]string, 0, len(skills))
	for _, s := range skills {
		if s.Enabled {
			ids = append(ids, uuidToString(s.ID))
		}
	}
	sort.Strings(ids)
	cfg := agentToolConfig{
		McpConfig: versionRaw(agent.McpConfig), CustomArgs: versionRaw(agent.CustomArgs), RuntimeConfig: versionRaw(agent.RuntimeConfig),
		DisabledRuntimeSkills: versionRaw(agent.DisabledRuntimeSkills), ThinkingLevel: agent.ThinkingLevel.String, ServiceTier: agent.ServiceTier.String,
		ComposioToolkits: agent.ComposioToolkitAllowlist,
	}
	tool, err := json.Marshal(cfg)
	if err != nil {
		return agentSnapshot{}, err
	}
	return agentSnapshot{Instructions: agent.Instructions, Model: agent.Model.String, SkillIDs: ids, ToolConfig: tool}, nil
}

func snapshotEquals(v db.AgentVersion, s agentSnapshot) bool {
	var ids []string
	_ = json.Unmarshal(v.SkillIds, &ids)
	if ids == nil {
		ids = []string{}
	}
	var a, b any
	_ = json.Unmarshal(v.ToolConfig, &a)
	_ = json.Unmarshal(s.ToolConfig, &b)
	ta, _ := json.Marshal(a)
	tb, _ := json.Marshal(b)
	return v.Instructions == s.Instructions && v.Model == s.Model && strings.Join(ids, ",") == strings.Join(s.SkillIDs, ",") && bytes.Equal(ta, tb)
}

func (h *Handler) insertAgentVersion(ctx context.Context, agent db.Agent, s agentSnapshot, note, byType string, byID pgtype.UUID) (db.AgentVersion, error) {
	skillIDs, _ := json.Marshal(s.SkillIDs)
	var v db.AgentVersion
	var err error
	for attempt := 0; attempt < agentVersionSnapshotRetries; attempt++ {
		v, err = h.Queries.CreateAgentVersion(ctx, db.CreateAgentVersionParams{
			WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, Instructions: s.Instructions, Model: s.Model,
			SkillIds: skillIDs, ToolConfig: s.ToolConfig, Note: note, CreatedByType: byType, CreatedByID: byID,
		})
		if err == nil {
			return v, nil
		}
	}
	return v, err
}

// recordAgentVersion is deferred by every handler that changes an agent's
// configuration: it reads the agent back and, when it differs from the
// newest version, records a new one. Without any version yet, the state
// before the change becomes v1 first, so history starts where it began.
func (h *Handler) recordAgentVersion(ctx context.Context, before db.Agent, userID string) {
	if _, err := h.Queries.GetLatestAgentVersion(ctx, before.ID); errors.Is(err, pgx.ErrNoRows) {
		if s, err := h.snapshotOf(ctx, before); err == nil {
			if _, err := h.insertAgentVersion(ctx, before, s, "Initial configuration", "system", pgtype.UUID{}); err != nil {
				slog.Warn("agent version: baseline failed", "error", err, "agent_id", uuidToString(before.ID))
				return
			}
		}
	}
	after, err := h.Queries.GetAgent(ctx, before.ID)
	if err != nil {
		return
	}
	s, err := h.snapshotOf(ctx, after)
	if err != nil {
		slog.Warn("agent version: snapshot failed", "error", err, "agent_id", uuidToString(after.ID))
		return
	}
	latest, err := h.Queries.GetLatestAgentVersion(ctx, after.ID)
	if err == nil && snapshotEquals(latest, s) {
		return
	}
	if _, err := h.insertAgentVersion(ctx, after, s, "", "member", parseUUIDOrZero(userID)); err != nil {
		slog.Warn("agent version: record failed", "error", err, "agent_id", uuidToString(after.ID))
	}
}

type AgentVersionResponse struct {
	ID            string          `json:"id"`
	AgentID       string          `json:"agent_id"`
	VersionNumber int32           `json:"version_number"`
	Instructions  string          `json:"instructions"`
	Model         string          `json:"model"`
	SkillIDs      []string        `json:"skill_ids"`
	ToolConfig    json.RawMessage `json:"tool_config"`
	Note          string          `json:"note,omitempty"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   *string         `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	// Active marks the newest version: what the agent runs with now.
	Active bool `json:"active"`
}

func agentVersionToResponse(v db.AgentVersion, active bool) AgentVersionResponse {
	var ids []string
	_ = json.Unmarshal(v.SkillIds, &ids)
	if ids == nil {
		ids = []string{}
	}
	tool := json.RawMessage(v.ToolConfig)
	if len(tool) == 0 {
		tool = json.RawMessage("{}")
	}
	return AgentVersionResponse{
		ID: uuidToString(v.ID), AgentID: uuidToString(v.AgentID), VersionNumber: v.VersionNumber,
		Instructions: v.Instructions, Model: v.Model, SkillIDs: ids, ToolConfig: tool, Note: v.Note,
		CreatedByType: v.CreatedByType, CreatedByID: uuidToPtr(v.CreatedByID), CreatedAt: timestampToString(v.CreatedAt), Active: active,
	}
}

// ListAgentVersions — GET /api/agents/{id}/versions. An agent without any
// version gets its v1 from its current state, so history always starts.
func (h *Handler) ListAgentVersions(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := h.Queries.GetLatestAgentVersion(ctx, agent.ID); errors.Is(err, pgx.ErrNoRows) {
		if s, err := h.snapshotOf(ctx, agent); err == nil {
			_, _ = h.insertAgentVersion(ctx, agent, s, "Initial configuration", "system", pgtype.UUID{})
		}
	}
	rows, err := h.Queries.ListAgentVersions(ctx, agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	out := make([]AgentVersionResponse, 0, len(rows))
	for i, v := range rows {
		out = append(out, agentVersionToResponse(v, i == 0))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}

// AgentVersionDiff names what changed between two versions; the values
// travel so the client can render them however it likes.
type AgentVersionDiff struct {
	From          AgentVersionResponse `json:"from"`
	To            AgentVersionResponse `json:"to"`
	ChangedFields []string             `json:"changed_fields"`
}

func diffVersions(from, to db.AgentVersion) []string {
	changed := []string{}
	if from.Instructions != to.Instructions {
		changed = append(changed, "instructions")
	}
	if from.Model != to.Model {
		changed = append(changed, "model")
	}
	if !bytes.Equal(from.SkillIds, to.SkillIds) {
		changed = append(changed, "skills")
	}
	var a, b any
	_ = json.Unmarshal(from.ToolConfig, &a)
	_ = json.Unmarshal(to.ToolConfig, &b)
	ta, _ := json.Marshal(a)
	tb, _ := json.Marshal(b)
	if !bytes.Equal(ta, tb) {
		changed = append(changed, "tool_config")
	}
	return changed
}

// GetAgentVersionDiff — GET /api/agents/{id}/versions/{versionId}/diff?against=.
func (h *Handler) GetAgentVersionDiff(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	toID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "versionId"), "version id")
	if !ok {
		return
	}
	fromID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("against"), "against")
	if !ok {
		return
	}
	ctx := r.Context()
	to, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: toID, AgentID: agent.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	from, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: fromID, AgentID: agent.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	latest, _ := h.Queries.GetLatestAgentVersion(ctx, agent.ID)
	writeJSON(w, http.StatusOK, AgentVersionDiff{
		From: agentVersionToResponse(from, from.ID == latest.ID), To: agentVersionToResponse(to, to.ID == latest.ID), ChangedFields: diffVersions(from, to),
	})
}

// RollbackAgentVersion — POST /api/agents/{id}/versions/{versionId}/rollback:
// writes the snapshot onto the agent and records it as a new version.
func (h *Handler) RollbackAgentVersion(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	versionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "versionId"), "version id")
	if !ok {
		return
	}
	ctx := r.Context()
	target, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: versionID, AgentID: agent.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	current, err := h.snapshotOf(ctx, agent)
	if err == nil && snapshotEquals(target, current) {
		writeErrorCode(w, http.StatusConflict, ErrCodeVersionAlreadyActive, fmt.Sprintf("version %d is already what the agent runs with", target.VersionNumber))
		return
	}
	var cfg agentToolConfig
	_ = json.Unmarshal(target.ToolConfig, &cfg)
	orEmpty := func(raw json.RawMessage, empty string) []byte {
		if len(raw) == 0 {
			return []byte(empty)
		}
		return raw
	}
	if _, err := h.Queries.ApplyAgentVersion(ctx, db.ApplyAgentVersionParams{
		ID: agent.ID, Instructions: target.Instructions, Model: target.Model,
		McpConfig: orEmpty(cfg.McpConfig, "null"), CustomArgs: orEmpty(cfg.CustomArgs, "[]"), RuntimeConfig: orEmpty(cfg.RuntimeConfig, "{}"), DisabledRuntimeSkills: orEmpty(cfg.DisabledRuntimeSkills, "[]"),
	}); err != nil {
		slog.Warn("agent rollback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to apply the version")
		return
	}
	var ids []string
	_ = json.Unmarshal(target.SkillIds, &ids)
	if err := h.Queries.SetAgentVersionSkills(ctx, agent.ID); err == nil {
		for _, id := range ids {
			if u, err := util.ParseUUID(id); err == nil {
				_ = h.Queries.AddAgentSkillEnabled(ctx, db.AddAgentSkillEnabledParams{AgentID: agent.ID, SkillID: u})
			}
		}
	}
	after, err := h.Queries.GetAgent(ctx, agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload the agent")
		return
	}
	s, err := h.snapshotOf(ctx, after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snapshot the agent")
		return
	}
	v, err := h.insertAgentVersion(ctx, after, s, fmt.Sprintf("Rollback to v%d", target.VersionNumber), "member", parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the version")
		return
	}
	h.audit(ctx, agent.WorkspaceID, "member", userID, AuditAgentRolledBack, "agent", agent.ID, map[string]any{"to_version": target.VersionNumber, "new_version": v.VersionNumber}, &auditOpts{Model: target.Model})
	writeJSON(w, http.StatusOK, map[string]any{"version": agentVersionToResponse(v, true)})
}

// agentVersionNumberAt resolves the version active at a point in time.
func (h *Handler) agentVersionNumberAt(ctx context.Context, agentID pgtype.UUID, at time.Time) int32 {
	v, err := h.Queries.GetAgentVersionAt(ctx, db.GetAgentVersionAtParams{AgentID: agentID, CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}})
	if err != nil {
		return 0
	}
	return v.VersionNumber
}
