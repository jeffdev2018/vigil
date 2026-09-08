package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	// creation past the cap is refused with 409; extraction stops proposing
	// facts at the cap and never evicts reviewed or rejected memories.
	agentMemoryMaxPerAgent = 200
)

type AgentMemoryResponse struct {
	SourceReview json.RawMessage `json:"source_review"`
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	AgentID      string          `json:"agent_id"`
	Content      string          `json:"content"`
	Source       string          `json:"source"`
	// State is the governance state (JEF-269): "draft" for auto-extracted
	// facts awaiting human review, "approved" for human-written or vetted ones.
	State         string  `json:"state"`
	SourceTaskID  *string `json:"source_task_id"`
	SourceIssueID *string `json:"source_issue_id"`
	Status        string  `json:"status"`
	Revision      int32   `json:"revision"`
	ReviewedBy    *string `json:"reviewed_by"`
	ReviewedAt    *string `json:"reviewed_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     *string `json:"expires_at"`
	Expired       bool    `json:"expired"`
}

// ListAgentMemoriesResponse wraps the list so the tab can tell the operator
// two things the rows alone cannot: how many of the facts actually reach a run
// brief (the brief has a character budget past the 200-fact cap), and whether
// runs can write facts back at all (no LLM configured = no extraction pass).
type ListAgentMemoriesResponse struct {
	Memories          []AgentMemoryResponse `json:"memories"`
	BriefedCount      int                   `json:"briefed_count"`
	ExtractionEnabled bool                  `json:"extraction_enabled"`
}

type AgentMemoryVersionResponse struct {
	AgentMemoryResponse
	RestoredFromRevision *int32 `json:"restored_from_revision,omitempty"`
}

// Immutable human evidence, kept even when the issue or source run is deleted.
// It explains the proposal; it is not a replay result or a general quality claim.
type AgentMemoryCorrectionSource struct {
	ReviewID      string               `json:"review_id"`
	IssueID       string               `json:"issue_id"`
	TaskID        string               `json:"task_id"`
	Feedback      string               `json:"feedback"`
	Criteria      []string             `json:"criteria"`
	Assessments   []DeliveryAssessment `json:"assessments"`
	SnapshotToken string               `json:"snapshot_token"`
	ReviewedBy    string               `json:"reviewed_by"`
	ReviewedAt    string               `json:"reviewed_at"`
	InputHash     string               `json:"input_hash"`
}

type AgentMemoryHistoryResponse struct {
	Versions           []AgentMemoryVersionResponse `json:"versions"`
	NextBeforeRevision *int32                       `json:"next_before_revision"`
}

type CreateAgentMemoryRequest struct {
	SourceReviewID *string         `json:"source_review_id"`
	Content        string          `json:"content"`
	SourceTaskID   *string         `json:"source_task_id"`
	ExpiresAt      json.RawMessage `json:"expires_at"`
}

type UpdateAgentMemoryRequest struct {
	EvaluationID    *string `json:"evaluation_id"`
	RestoreRevision *int32  `json:"restore_revision"`
	Content         *string `json:"content"`
	// State is the human approval action (JEF-269): "approved" promotes a
	// draft extraction fact, "draft" sends it back to review.
	State            *string         `json:"state"`
	Status           *string         `json:"status"`
	ExpectedRevision *int32          `json:"expected_revision"`
	ExpiresAt        json.RawMessage `json:"expires_at"`
}

func agentMemoryToResponse(m db.AgentMemory) AgentMemoryResponse {
	return AgentMemoryResponse{
		SourceReview: m.SourceReview,
		ID:           uuidToString(m.ID),
		WorkspaceID:  uuidToString(m.WorkspaceID),
		AgentID:      uuidToString(m.AgentID),
		Content:      m.Content,
		Source:       m.Source,
		State:        m.State,
		SourceTaskID: uuidToPtr(m.SourceTaskID),
		Status:       m.Status,
		Revision:     m.Revision,
		ReviewedBy:   uuidToPtr(m.ReviewedBy),
		ReviewedAt:   timestampToPtr(m.ReviewedAt),
		CreatedAt:    timestampToString(m.CreatedAt),
		UpdatedAt:    timestampToString(m.UpdatedAt),
		ExpiresAt:    timestampToPtr(m.ExpiresAt),
		Expired:      m.ExpiresAt.Valid && !m.ExpiresAt.Time.After(time.Now()),
	}
}

func agentMemoryListRowToResponse(m db.ListAgentMemoriesRow) AgentMemoryResponse {
	resp := agentMemoryToResponse(db.AgentMemory{
		ID:           m.ID,
		WorkspaceID:  m.WorkspaceID,
		AgentID:      m.AgentID,
		Content:      m.Content,
		Source:       m.Source,
		State:        m.State,
		SourceTaskID: m.SourceTaskID,
		Status:       m.Status,
		Revision:     m.Revision,
		ReviewedBy:   m.ReviewedBy,
		ReviewedAt:   m.ReviewedAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		ExpiresAt:    m.ExpiresAt,
		SourceReview: m.SourceReview,
	})
	resp.SourceIssueID = uuidToPtr(m.SourceIssueID)
	return resp
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
	facts := make([]service.AgentMemoryFact, 0, len(memories))
	for i, m := range memories {
		resp.Memories[i] = agentMemoryListRowToResponse(m)
	}
	for i := len(memories) - 1; i >= 0; i-- {
		facts = append(facts, service.AgentMemoryFact{Content: memories[i].Content, Source: memories[i].Source, State: memories[i].State})
	}
	resp.BriefedCount = len(service.SelectBriefedAgentMemories(facts))
	writeJSON(w, http.StatusOK, resp)
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

func (h *Handler) CreateAgentMemory(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "agent memory can only be managed by a human")
		return
	}
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req CreateAgentMemoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content, ok := validateAgentMemoryContent(req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "content is required and must be at most 500 characters")
		return
	}

	status := "active"
	var sourceTaskID pgtype.UUID
	if req.SourceTaskID != nil {
		sourceTaskID, ok = parseUUIDOrBadRequest(w, *req.SourceTaskID, "source_task_id")
		if !ok {
			return
		}
		if req.SourceReviewID == nil {
			task, err := h.Queries.GetAgentTask(r.Context(), sourceTaskID)
			if err != nil || task.AgentID != agent.ID || task.ChatSessionID.Valid {
				writeError(w, http.StatusNotFound, "source run not found")
				return
			}
			if task.Status != "completed" && task.Status != "failed" && task.Status != "cancelled" {
				writeError(w, http.StatusConflict, "wait for the source run to finish before recording a correction")
				return
			}
			status = "pending"
		}
	}
	var sourceReviewID pgtype.UUID
	if req.SourceReviewID != nil {
		sourceReviewID, ok = parseUUIDOrBadRequest(w, *req.SourceReviewID, "source_review_id")
		if !ok {
			return
		}
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
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, "workspace is no longer available")
		return
	}
	if _, err := qtx.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: agent.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, http.StatusConflict, "agent is no longer available")
		return
	}
	var sourceReview []byte
	if sourceReviewID.Valid {
		expiryInput := strings.TrimSpace(string(req.ExpiresAt))
		if expiryInput == "" {
			expiryInput = "null"
		}
		inputHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content+"\x00"+expiryInput)))
		existing, err := qtx.GetAgentMemoryForCorrection(r.Context(), db.GetAgentMemoryForCorrectionParams{AgentID: agent.ID, WorkspaceID: agent.WorkspaceID, ReviewID: uuidToString(sourceReviewID)})
		if err == nil {
			var provenance AgentMemoryCorrectionSource
			if json.Unmarshal(existing.SourceReview, &provenance) != nil {
				writeError(w, http.StatusInternalServerError, "failed to read correction provenance")
				return
			}
			if provenance.InputHash != inputHash || (sourceTaskID.Valid && sourceTaskID != existing.SourceTaskID) {
				writeError(w, http.StatusConflict, "this correction already has a memory; edit the existing memory instead")
				return
			}
			// Recover the original candidate even if its source was deleted or it
			// has since been reviewed. Never reset its content, status or expiry.
			writeJSON(w, http.StatusOK, agentMemoryToResponse(existing))
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to find correction memory")
			return
		}
		row, err := qtx.GetAgentMemoryCorrectionSource(r.Context(), db.GetAgentMemoryCorrectionSourceParams{ID: sourceReviewID, WorkspaceID: agent.WorkspaceID, AgentID: agent.ID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "source review not found for this agent")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to read source review")
			}
			return
		}
		if row.Decision != "changes_requested" {
			writeError(w, http.StatusConflict, "source review did not request a correction")
			return
		}
		if sourceTaskID.Valid && sourceTaskID != row.TaskID {
			writeError(w, http.StatusBadRequest, "source run does not match the review")
			return
		}
		var snapshot DeliverySnapshot
		var assessments []DeliveryAssessment
		if json.Unmarshal(row.Snapshot, &snapshot) != nil || json.Unmarshal(row.Assessments, &assessments) != nil {
			writeError(w, http.StatusInternalServerError, "failed to read correction evidence")
			return
		}
		provenance := AgentMemoryCorrectionSource{ReviewID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), TaskID: uuidToString(row.TaskID), Feedback: row.Feedback, Criteria: snapshot.Criteria, Assessments: assessments, SnapshotToken: row.SnapshotToken, ReviewedBy: uuidToString(row.ReviewedBy), ReviewedAt: timestampToString(row.CreatedAt), InputHash: inputHash}
		sourceReview, err = json.Marshal(provenance)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to preserve correction evidence")
			return
		}
		sourceTaskID, status = row.TaskID, "pending"
	}
	expiresAt := pgtype.Timestamptz{}
	if len(req.ExpiresAt) > 0 {
		var err error
		expiresAt, err = parseMemoryExpiration(req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

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
		SourceReview: sourceReview,
		WorkspaceID:  agent.WorkspaceID,
		AgentID:      agent.ID,
		Content:      content,
		Source:       "manual",
		// A human wrote this fact, so it is approved at write time (JEF-269).
		State:        "approved",
		SourceTaskID: sourceTaskID,
		Status:       pgtype.Text{String: status, Valid: true},
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent memory: "+err.Error())
		return
	}
	if err := qtx.SaveAgentMemoryVersion(r.Context(), db.SaveAgentMemoryVersionParams{MemoryID: memory.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve new memory")
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
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "agent memory can only be managed by a human")
		return
	}
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var evaluationID pgtype.UUID
	if req.EvaluationID != nil {
		if req.Status == nil || *req.Status != "active" || req.ExpectedRevision == nil || req.Content != nil || req.State != nil || len(req.ExpiresAt) != 0 || req.RestoreRevision != nil {
			writeError(w, http.StatusBadRequest, "evaluation adoption requires only active status and expected_revision")
			return
		}
		evaluationID, ok = parseUUIDOrBadRequest(w, *req.EvaluationID, "evaluation_id")
		if !ok {
			return
		}
	}
	if req.Content == nil && req.State == nil && req.Status == nil && len(req.ExpiresAt) == 0 && req.RestoreRevision == nil {
		writeError(w, http.StatusBadRequest, "content, state, status or expires_at is required")
		return
	}
	// State (JEF-269) is the human-vetted flag and is independent of the
	// review status below, which says whether the fact is live.
	if req.State != nil && *req.State != "draft" && *req.State != "approved" {
		writeError(w, http.StatusBadRequest, "state must be \"draft\" or \"approved\"")
		return
	}
	expiresAt := pgtype.Timestamptz{}
	if len(req.ExpiresAt) > 0 {
		if req.ExpectedRevision == nil {
			writeError(w, http.StatusBadRequest, "expected_revision is required when changing expiration")
			return
		}
		var err error
		expiresAt, err = parseMemoryExpiration(req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.RestoreRevision != nil && (*req.RestoreRevision < 1 || req.ExpectedRevision == nil || req.Content != nil || req.State != nil || req.Status != nil || len(req.ExpiresAt) > 0) {
		writeError(w, http.StatusBadRequest, "restoration requires expected_revision and a positive restore_revision, without other changes")
		return
	}
	var content pgtype.Text
	if req.Content != nil {
		value, valid := validateAgentMemoryContent(*req.Content)
		if !valid {
			writeError(w, http.StatusBadRequest, "content is required and must be at most 500 characters")
			return
		}
		content = pgtype.Text{String: value, Valid: true}
	}
	var status pgtype.Text
	if req.Status != nil {
		switch *req.Status {
		case "pending", "active", "rejected":
			status = pgtype.Text{String: *req.Status, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "status must be pending, active or rejected")
			return
		}
		if req.ExpectedRevision == nil {
			writeError(w, http.StatusBadRequest, "expected_revision is required when reviewing a memory")
			return
		}
	}
	// Older installed clients may still submit content-only edits. Even those
	// compare against the row loaded above, so an intervening write is refused.
	revision := memory.Revision
	if req.ExpectedRevision != nil {
		revision = *req.ExpectedRevision
		if revision < 1 || revision >= math.MaxInt32 {
			writeError(w, http.StatusBadRequest, "expected_revision must be positive")
			return
		}
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin memory update")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, "workspace is unavailable")
		return
	}
	if _, err := qtx.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: agent.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, http.StatusConflict, "agent is unavailable")
		return
	}
	current, err := qtx.GetAgentMemory(r.Context(), db.GetAgentMemoryParams{ID: memory.ID, WorkspaceID: agent.WorkspaceID})
	if err != nil || current.Revision != revision {
		writeError(w, http.StatusConflict, "memory changed; reload before reviewing")
		return
	}
	restored := pgtype.Int4{}
	if evaluationID.Valid {
		if err := checkEvaluationAdoption(r.Context(), qtx, agent, current, evaluationID); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	if req.RestoreRevision != nil {
		version, err := qtx.GetAgentMemoryVersion(r.Context(), db.GetAgentMemoryVersionParams{MemoryID: memory.ID, WorkspaceID: agent.WorkspaceID, Revision: *req.RestoreRevision})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "memory version not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read memory version")
			return
		}
		content = pgtype.Text{String: version.Content, Valid: true}
		status = pgtype.Text{String: version.Status, Valid: true}
		expiresAt = version.ExpiresAt
		restored = pgtype.Int4{Int32: version.Revision, Valid: true}
	}
	save := db.SaveAgentMemoryVersionParams{MemoryID: memory.ID, WorkspaceID: agent.WorkspaceID}
	if err := qtx.SaveAgentMemoryVersion(r.Context(), save); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve current memory")
		return
	}
	updated, err := qtx.UpdateAgentMemoryContent(r.Context(), db.UpdateAgentMemoryContentParams{
		ID:               memory.ID,
		WorkspaceID:      agent.WorkspaceID,
		Content:          content,
		Status:           status,
		ExpectedRevision: revision,
		ReviewedBy:       parseUUID(userID),
		UpdateExpiration: len(req.ExpiresAt) > 0 || req.RestoreRevision != nil, ExpiresAt: expiresAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "memory changed; reload before reviewing")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update agent memory: "+err.Error())
		return
	}
	if req.State != nil {
		updated, err = qtx.SetAgentMemoryState(r.Context(), db.SetAgentMemoryStateParams{
			ID:          memory.ID,
			WorkspaceID: agent.WorkspaceID,
			State:       *req.State,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "agent memory not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to update agent memory: "+err.Error())
			return
		}
	}

	save.RestoredFromRevision = restored
	if evaluationID.Valid {
		if err := qtx.MarkAgentMemoryEvaluationAdopted(r.Context(), db.MarkAgentMemoryEvaluationAdoptedParams{ID: evaluationID, WorkspaceID: agent.WorkspaceID, MemoryID: memory.ID, AdoptedRevision: pgtype.Int4{Int32: updated.Revision, Valid: true}}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record evaluated adoption")
			return
		}
	}
	if err := qtx.SaveAgentMemoryVersion(r.Context(), save); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve updated memory")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit memory update")
		return
	}
	resp := agentMemoryToResponse(updated)
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventAgentMemoryUpdated, workspaceID, actorType, actorID, map[string]any{"memory": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAgentMemory(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "agent memory can only be managed by a human")
		return
	}
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

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin memory deletion")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, "workspace is unavailable")
		return
	}
	if _, err := qtx.LockAgentForMemoryUpdate(r.Context(), db.LockAgentForMemoryUpdateParams{ID: agent.ID, WorkspaceID: agent.WorkspaceID}); err != nil {
		writeError(w, http.StatusConflict, "agent is unavailable")
		return
	}
	rows, err := qtx.DeleteAgentMemory(r.Context(), db.DeleteAgentMemoryParams{
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

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit memory deletion")
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

// History follows the same agent/workspace visibility boundary as current memory.
func (h *Handler) ListAgentMemoryHistory(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	memory, ok := h.loadAgentMemoryForAgent(w, r, agent)
	if !ok {
		return
	}
	before := int32(math.MaxInt32)
	if value := r.URL.Query().Get("before_revision"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid before_revision")
			return
		}
		before = int32(parsed)
	}
	rows, err := h.Queries.ListAgentMemoryVersions(r.Context(), db.ListAgentMemoryVersionsParams{MemoryID: memory.ID, WorkspaceID: agent.WorkspaceID, Revision: before})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read memory history")
		return
	}
	resp := AgentMemoryHistoryResponse{Versions: []AgentMemoryVersionResponse{}}
	if len(rows) > 20 {
		rows = rows[:20]
		next := rows[19].Revision
		resp.NextBeforeRevision = &next
	}
	for _, row := range rows {
		version := AgentMemoryVersionResponse{AgentMemoryResponse: agentMemoryToResponse(db.AgentMemory{
			SourceReview: row.SourceReview,
			ID:           row.MemoryID, WorkspaceID: row.WorkspaceID, AgentID: row.AgentID,
			Content: row.Content, Source: row.Source, SourceTaskID: row.SourceTaskID, Status: row.Status,
			// agent_memory_version has no state column: JEF-269 governance is a
			// property of the live row, not of its historical revisions.
			Revision: row.Revision, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ExpiresAt: row.ExpiresAt,
		})}
		if row.RestoredFromRevision.Valid {
			value := row.RestoredFromRevision.Int32
			version.RestoredFromRevision = &value
		}
		resp.Versions = append(resp.Versions, version)
	}
	writeJSON(w, http.StatusOK, resp)
}
