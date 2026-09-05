package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agent memory (JEF-236): durable per-agent facts ("this repo uses pnpm,
// never npm") injected into every run's brief. Humans manage them over these
// endpoints; the post-run extraction pass writes its own rows with
// source='run'.

const (
	// agentMemoryMaxContentRunes mirrors the CHECK (length(content) <= 500)
	// on the table; validated here so a violation is a clean 400 instead of a
	// 500 from the database.
	agentMemoryMaxContentRunes = 500
	// agentMemoryMaxPerAgent caps the facts one agent accumulates. Manual
	// creation past the cap is refused with 409; the extraction pass evicts
	// its own oldest source='run' rows instead (DeleteOldestRunMemories) and
	// never touches manual ones.
	agentMemoryMaxPerAgent = 200
)

type AgentMemoryResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Content     string `json:"content"`
	Source      string `json:"source"`
	// SourceTaskID is the run that wrote the fact; SourceIssueID is the issue
	// that run worked on, resolved by the list query's join so the UI can link
	// a run-sourced fact back to its origin. Both are null for manual facts,
	// and SourceIssueID is null for a run that carried no issue (chat, duel).
	SourceTaskID  *string `json:"source_task_id"`
	SourceIssueID *string `json:"source_issue_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// ListAgentMemoriesResponse wraps the list so the tab can tell the operator
// two things the rows alone cannot: how many of the facts actually reach a run
// brief (the brief has a character budget past the 200-fact cap), and whether
// runs can write facts back at all (no LLM configured = no extraction pass).
type ListAgentMemoriesResponse struct {
	Memories []AgentMemoryResponse `json:"memories"`
	// BriefedCount is how many of Memories the next run brief would carry.
	// Equal to len(Memories) unless the character budget truncated the set.
	BriefedCount int `json:"briefed_count"`
	// ExtractionEnabled reports whether the deployment has an LLM configured
	// for the post-run extraction pass. False means only manual facts appear.
	ExtractionEnabled bool `json:"extraction_enabled"`
}

type CreateAgentMemoryRequest struct {
	Content string `json:"content"`
}

type UpdateAgentMemoryRequest struct {
	Content *string `json:"content"`
}

func agentMemoryToResponse(m db.AgentMemory) AgentMemoryResponse {
	return AgentMemoryResponse{
		ID:           uuidToString(m.ID),
		WorkspaceID:  uuidToString(m.WorkspaceID),
		AgentID:      uuidToString(m.AgentID),
		Content:      m.Content,
		Source:       m.Source,
		SourceTaskID: uuidToPtr(m.SourceTaskID),
		CreatedAt:    timestampToString(m.CreatedAt),
		UpdatedAt:    timestampToString(m.UpdatedAt),
	}
}

func agentMemoryRowToResponse(m db.ListAgentMemoriesRow) AgentMemoryResponse {
	return AgentMemoryResponse{
		ID:            uuidToString(m.ID),
		WorkspaceID:   uuidToString(m.WorkspaceID),
		AgentID:       uuidToString(m.AgentID),
		Content:       m.Content,
		Source:        m.Source,
		SourceTaskID:  uuidToPtr(m.SourceTaskID),
		SourceIssueID: uuidToPtr(m.SourceIssueID),
		CreatedAt:     timestampToString(m.CreatedAt),
		UpdatedAt:     timestampToString(m.UpdatedAt),
	}
}

// validateAgentMemoryContent normalizes and validates one fact. The returned
// string is the trimmed, Postgres-safe value to persist.
func validateAgentMemoryContent(content string) (string, bool) {
	content = strings.TrimSpace(util.SanitizeTextForPostgres(content))
	if content == "" || utf8.RuneCountInString(content) > agentMemoryMaxContentRunes {
		return "", false
	}
	return content, true
}

func (h *Handler) ListAgentMemories(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	memories, err := h.Queries.ListAgentMemories(r.Context(), db.ListAgentMemoriesParams{
		AgentID:     agent.ID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent memories")
		return
	}

	resp := ListAgentMemoriesResponse{
		Memories:          make([]AgentMemoryResponse, len(memories)),
		ExtractionEnabled: h.TaskService != nil && h.TaskService.MemoryExtraction != nil && h.TaskService.MemoryExtraction.Enabled(),
	}
	// The rows arrive chronologically; the brief budget is spent newest-first,
	// so feed the selection the reverse of the list order.
	facts := make([]service.AgentMemoryFact, 0, len(memories))
	for i, m := range memories {
		resp.Memories[i] = agentMemoryRowToResponse(m)
	}
	for i := len(memories) - 1; i >= 0; i-- {
		facts = append(facts, service.AgentMemoryFact{Content: memories[i].Content, Source: memories[i].Source})
	}
	resp.BriefedCount = len(service.SelectBriefedAgentMemories(facts))
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateAgentMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req CreateAgentMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content, ok := validateAgentMemoryContent(req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "content is required and must be at most 500 characters")
		return
	}

	// Count and insert in one transaction so two concurrent creates cannot
	// both pass the cap check.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	countParams := db.CountAgentMemoriesParams{AgentID: agent.ID, WorkspaceID: agent.WorkspaceID}
	count, err := qtx.CountAgentMemories(r.Context(), countParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count agent memories")
		return
	}
	if count >= agentMemoryMaxPerAgent {
		writeError(w, http.StatusConflict, "agent memory limit reached (200 facts); delete one before adding another")
		return
	}

	memory, err := qtx.CreateAgentMemory(r.Context(), db.CreateAgentMemoryParams{
		WorkspaceID: agent.WorkspaceID,
		AgentID:     agent.ID,
		Content:     content,
		Source:      "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent memory: "+err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := agentMemoryToResponse(memory)
	workspaceID := uuidToString(agent.WorkspaceID)
	userID, _ := requireUserID(w, r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventAgentMemoryCreated, workspaceID, actorType, actorID, map[string]any{"memory": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// loadAgentMemoryForAgent resolves the {memoryId} path param to a row that
// belongs to BOTH the URL agent and the request workspace. Anything else —
// foreign workspace, foreign agent, unknown id — is a 404 so existence leaks
// nothing across tenant or agent boundaries.
func (h *Handler) loadAgentMemoryForAgent(w http.ResponseWriter, r *http.Request, agent db.Agent) (db.AgentMemory, bool) {
	memoryUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memoryId"), "memory id")
	if !ok {
		return db.AgentMemory{}, false
	}
	memory, err := h.Queries.GetAgentMemory(r.Context(), db.GetAgentMemoryParams{
		ID:          memoryUUID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil || memory.AgentID != agent.ID {
		writeError(w, http.StatusNotFound, "agent memory not found")
		return db.AgentMemory{}, false
	}
	return memory, true
}

func (h *Handler) UpdateAgentMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	memory, ok := h.loadAgentMemoryForAgent(w, r, agent)
	if !ok {
		return
	}

	var req UpdateAgentMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == nil {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	content, ok := validateAgentMemoryContent(*req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "content is required and must be at most 500 characters")
		return
	}

	updated, err := h.Queries.UpdateAgentMemoryContent(r.Context(), db.UpdateAgentMemoryContentParams{
		ID:          memory.ID,
		WorkspaceID: agent.WorkspaceID,
		Content:     pgtype.Text{String: content, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent memory not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update agent memory: "+err.Error())
		return
	}

	resp := agentMemoryToResponse(updated)
	workspaceID := uuidToString(agent.WorkspaceID)
	userID, _ := requireUserID(w, r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventAgentMemoryUpdated, workspaceID, actorType, actorID, map[string]any{"memory": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAgentMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	memory, ok := h.loadAgentMemoryForAgent(w, r, agent)
	if !ok {
		return
	}

	rows, err := h.Queries.DeleteAgentMemory(r.Context(), db.DeleteAgentMemoryParams{
		ID:          memory.ID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent memory")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "agent memory not found")
		return
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	userID, _ := requireUserID(w, r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventAgentMemoryDeleted, workspaceID, actorType, actorID, map[string]any{
		"memory": agentMemoryToResponse(memory),
	})
	w.WriteHeader(http.StatusNoContent)
}
