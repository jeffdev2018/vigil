package handler

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/multica-ai/multica/server/internal/service"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const projectMemoryMaxRules = 20

// Immutable human evidence from a delivery correction, kept even when the
// issue or source run is deleted. Explains the publication; not a replay proof.
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

type ProjectMemoryResponse struct {
	Rules                []string          `json:"rules"`
	Revision             int32             `json:"revision"`
	ReviewedBy           *string           `json:"reviewed_by"`
	ReviewedAt           *string           `json:"reviewed_at"`
	ExpiresAt            *string           `json:"expires_at"`
	Expired              bool              `json:"expired"`
	RestoredFromRevision *int32            `json:"restored_from_revision,omitempty"`
	SourceReview         json.RawMessage   `json:"source_review,omitempty"`
}

func projectMemoryResponse(project db.Project) (ProjectMemoryResponse, error) {
	rules := []string{}
	if err := json.Unmarshal(project.MemoryRules, &rules); err != nil {
		return ProjectMemoryResponse{}, err
	}
	return ProjectMemoryResponse{Rules: rules, Revision: project.MemoryRevision,
		ReviewedBy: uuidToPtr(project.MemoryReviewedBy), ReviewedAt: timestampToPtr(project.MemoryReviewedAt),
		ExpiresAt: timestampToPtr(project.MemoryExpiresAt), Expired: project.MemoryExpiresAt.Valid && !project.MemoryExpiresAt.Time.After(time.Now()),
		SourceReview: project.MemorySourceReview}, nil
}

func (h *Handler) loadProjectForMemory(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, false
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "project not found"); !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

func (h *Handler) GetProjectMemory(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForMemory(w, r)
	if !ok {
		return
	}
	resp, err := projectMemoryResponse(project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read project memory")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateProjectMemory(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "project memory can only be published by a human")
		return
	}
	project, ok := h.loadProjectForMemory(w, r)
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(project.WorkspaceID), "project not found")
	if !ok {
		return
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "only workspace administrators can publish project memory")
		return
	}
	var req struct {
		Rules            *[]string       `json:"rules"`
		ExpectedRevision *int32          `json:"expected_revision"`
		RestoreRevision  *int32          `json:"restore_revision"`
		SourceReviewID   *string         `json:"source_review_id"`
		ExpiresAt        json.RawMessage `json:"expires_at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 || *req.ExpectedRevision >= math.MaxInt32 || (req.Rules == nil) == (req.RestoreRevision == nil) {
		writeError(w, http.StatusBadRequest, "expected_revision and either rules or restore_revision are required")
		return
	}
	if req.RestoreRevision != nil && (*req.RestoreRevision < 0 || len(req.ExpiresAt) > 0 || req.SourceReviewID != nil) {
		writeError(w, http.StatusBadRequest, "restore_revision must be non-negative; restoration preserves expiration and cannot attach a new correction source")
		return
	}
	var sourceReviewID pgtype.UUID
	if req.SourceReviewID != nil {
		parsed, ok := parseUUIDOrBadRequest(w, *req.SourceReviewID, "source_review_id")
		if !ok {
			return
		}
		sourceReviewID = parsed
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin memory publication")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	// Coordinate with workspace and project deletion before creating history rows.
	if _, err = q.LockWorkspaceForChatSessionCreate(r.Context(), project.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, "workspace is unavailable")
		return
	}
	if _, err = q.LockProjectForDelete(r.Context(), db.LockProjectForDeleteParams{ID: project.ID, WorkspaceID: project.WorkspaceID}); err != nil {
		writeError(w, http.StatusConflict, "project is unavailable")
		return
	}
	project, err = q.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: project.ID, WorkspaceID: project.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read current memory")
		return
	}
	if project.MemoryRevision != *req.ExpectedRevision {
		writeError(w, http.StatusConflict, "project memory changed; reload before publishing")
		return
	}
	expiresAt := project.MemoryExpiresAt
	var rules []string
	restored := pgtype.Int4{}
	var sourceReview []byte
	if req.RestoreRevision != nil {
		version, err := q.GetProjectMemoryVersion(r.Context(), db.GetProjectMemoryVersionParams{ProjectID: project.ID, WorkspaceID: project.WorkspaceID, Revision: *req.RestoreRevision})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "memory version not found")
			return
		}
		if err != nil || json.Unmarshal(version.Rules, &rules) != nil {
			writeError(w, http.StatusInternalServerError, "failed to read memory version")
			return
		}
		expiresAt = version.ExpiresAt
		restored = pgtype.Int4{Int32: version.Revision, Valid: true}
		sourceReview = version.SourceReview
	} else {
		rules = *req.Rules
		if len(req.ExpiresAt) > 0 {
			var err error
			expiresAt, err = parseMemoryExpiration(req.ExpiresAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if len(rules) > projectMemoryMaxRules {
		writeError(w, http.StatusBadRequest, "at most 20 rules are allowed")
		return
	}
	normalized := make([]string, 0, len(rules))
	for _, raw := range rules {
		rule := strings.TrimSpace(util.SanitizeTextForPostgres(raw))
		if rule == "" || utf8.RuneCountInString(rule) > 500 || strings.ContainsAny(rule, "\r\n") {
			writeError(w, http.StatusBadRequest, "each rule must be one non-empty line of at most 500 characters")
			return
		}
		normalized = append(normalized, rule)
	}
	if sourceReviewID.Valid {
		expiryInput := "null"
		if expiresAt.Valid {
			expiryInput = expiresAt.Time.UTC().Format(time.RFC3339Nano)
		}
		inputHash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(normalized, "\n")+"\x00"+expiryInput)))
		existing, err := q.GetProjectMemoryVersionForCorrection(r.Context(), db.GetProjectMemoryVersionForCorrectionParams{
			ProjectID: project.ID, WorkspaceID: project.WorkspaceID, ReviewID: uuidToString(sourceReviewID),
		})
		if err == nil {
			var provenance AgentMemoryCorrectionSource
			if json.Unmarshal(existing.SourceReview, &provenance) != nil {
				writeError(w, http.StatusInternalServerError, "failed to read correction provenance")
				return
			}
			if provenance.InputHash != inputHash {
				writeError(w, http.StatusConflict, "this correction already promoted project memory; edit the published rules instead")
				return
			}
			// Idempotent retry of a successful promotion — return the live project
			// row even if later publications changed the current revision.
			resp, err := projectMemoryResponse(project)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read project memory")
				return
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to find correction memory")
			return
		}
		row, err := q.GetProjectMemoryCorrectionSource(r.Context(), db.GetProjectMemoryCorrectionSourceParams{
			ID: sourceReviewID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "source review not found for this project")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to read source review")
			}
			return
		}
		if row.Decision != "changes_requested" {
			writeError(w, http.StatusConflict, "source review did not request a correction")
			return
		}
		var snapshot DeliverySnapshot
		var assessments []DeliveryAssessment
		if json.Unmarshal(row.Snapshot, &snapshot) != nil || json.Unmarshal(row.Assessments, &assessments) != nil {
			writeError(w, http.StatusInternalServerError, "failed to read correction evidence")
			return
		}
		provenance := AgentMemoryCorrectionSource{
			ReviewID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), TaskID: uuidToString(row.TaskID),
			Feedback: row.Feedback, Criteria: snapshot.Criteria, Assessments: assessments,
			SnapshotToken: row.SnapshotToken, ReviewedBy: uuidToString(row.ReviewedBy),
			ReviewedAt: timestampToString(row.CreatedAt), InputHash: inputHash,
		}
		sourceReview, err = json.Marshal(provenance)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to preserve correction evidence")
			return
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode project memory")
		return
	}
	save := db.SaveProjectMemoryVersionParams{ProjectID: project.ID, WorkspaceID: project.WorkspaceID}
	if err = q.SaveProjectMemoryVersion(r.Context(), save); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve current memory")
		return
	}
	updated, err := q.UpdateProjectMemory(r.Context(), db.UpdateProjectMemoryParams{
		ID: project.ID, WorkspaceID: project.WorkspaceID, MemoryRules: encoded,
		MemoryRevision: *req.ExpectedRevision, MemoryReviewedBy: member.UserID, ExpiresAt: expiresAt,
		MemorySourceReview: sourceReview,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish project memory")
		return
	}
	save.RestoredFromRevision = restored
	if err = q.SaveProjectMemoryVersion(r.Context(), save); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve published memory")
		return
	}
	resp, err := projectMemoryResponse(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read published memory")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit memory publication")
		return
	}
	projectResp := projectToResponse(updated)
	projectResp.IssueCount, projectResp.DoneCount = h.loadProjectIssueStats(r.Context(), updated.WorkspaceID, updated.ID)
	projectResp.ResourceCount = h.loadProjectResourceCount(r.Context(), updated.ID)
	h.publish(protocol.EventProjectUpdated, uuidToString(updated.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"project": projectResp})
	writeJSON(w, http.StatusOK, resp)
}

// Reuse the project-description wire field so installed daemons also receive
// the rules. This only enriches the claim snapshot, never the stored description.
func projectDescriptionWithMemory(project db.Project) (string, *service.MemoryVersion, error) {
	memory, err := projectMemoryResponse(project)
	if err != nil {
		return "", nil, fmt.Errorf("decode project memory: %w", err)
	}
	if len(memory.Rules) == 0 || memory.Expired {
		return project.Description.String, nil, nil
	}
	var out strings.Builder
	out.WriteString(project.Description.String)
	fmt.Fprintf(&out, "\n\n### Shared project memory (revision %d)\n", memory.Revision)
	out.WriteString("Human-reviewed rules for this project only. If they conflict with other instructions or memories, report the conflict before acting.\n")
	for _, rule := range memory.Rules {
		fmt.Fprintf(&out, "- %s\n", rule)
	}
	return out.String(), &service.MemoryVersion{ID: uuidToString(project.ID), Revision: memory.Revision}, nil
}

// ListProjectMemoryHistory uses an exclusive revision cursor, so new publications
// cannot shift older pages. Existing entries are immutable until project deletion.
func (h *Handler) ListProjectMemoryHistory(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForMemory(w, r)
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
	rows, err := h.Queries.ListProjectMemoryVersions(r.Context(), db.ListProjectMemoryVersionsParams{ProjectID: project.ID, WorkspaceID: project.WorkspaceID, Revision: before})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read memory history")
		return
	}
	response := struct {
		Versions           []ProjectMemoryResponse `json:"versions"`
		NextBeforeRevision *int32                  `json:"next_before_revision"`
	}{Versions: []ProjectMemoryResponse{}}
	if len(rows) > 20 {
		rows = rows[:20]
		next := rows[19].Revision
		response.NextBeforeRevision = &next
	}
	for _, row := range rows {
		version, err := projectMemoryResponse(db.Project{MemoryRules: row.Rules, MemoryRevision: row.Revision,
			MemoryReviewedBy: row.ReviewedBy, MemoryReviewedAt: row.ReviewedAt, MemoryExpiresAt: row.ExpiresAt,
			MemorySourceReview: row.SourceReview})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode memory history")
			return
		}
		if row.RestoredFromRevision.Valid {
			restored := row.RestoredFromRevision.Int32
			version.RestoredFromRevision = &restored
		}
		response.Versions = append(response.Versions, version)
	}
	writeJSON(w, http.StatusOK, response)
}

// Both memory scopes use the same explicit expiration contract. Omission is
// handled by the caller; JSON null clears an expiry, and dates must be future.
func parseMemoryExpiration(raw json.RawMessage) (pgtype.Timestamptz, error) {
	var value *time.Time
	if err := json.Unmarshal(raw, &value); err != nil || (value != nil && !value.After(time.Now())) {
		return pgtype.Timestamptz{}, errors.New("expires_at must be a future RFC3339 timestamp or null")
	}
	if value == nil {
		return pgtype.Timestamptz{}, nil
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}, nil
}
