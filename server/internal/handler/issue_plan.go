package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Issue plans and verification reports (F17). The plan is a versioned
// artifact; the report is what a verification run posts back. Findings are
// LLM output: stored and rendered as data, never interpreted.

const (
	planContentMaxBytes  = 64 << 10
	planFindingsMaxCount = 200
	planFindingMaxBytes  = 4 << 10
)

type IssuePlanStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Plan Gate (K11): a step comes after other steps (their ids), may name a
	// suggested assignee, and carries the sub-issue it became once approved.
	After        []string `json:"after,omitempty"`
	AssigneeType string   `json:"assignee_type,omitempty"`
	AssigneeID   string   `json:"assignee_id,omitempty"`
	IssueID      string   `json:"issue_id,omitempty"`
}

type IssuePlanResponse struct {
	ID           string          `json:"id"`
	IssueID      string          `json:"issue_id"`
	Version      int32           `json:"version"`
	Content      string          `json:"content"`
	Steps        json.RawMessage `json:"steps"`
	AuthorType   string          `json:"author_type"`
	AuthorID     string          `json:"author_id"`
	SupersededAt *string         `json:"superseded_at"`
	// MaterializedAt is set once the version's steps became sub-issues (K11).
	MaterializedAt *string `json:"materialized_at"`
	CreatedAt      string  `json:"created_at"`
}

type IssuePlanEnvelope struct {
	Plan     *IssuePlanResponse  `json:"plan"`
	Versions []IssuePlanResponse `json:"versions"`
}

type PlanFinding struct {
	Severity   string   `json:"severity"`
	Title      string   `json:"title"`
	Detail     string   `json:"detail,omitempty"`
	Files      []string `json:"files,omitempty"`
	PlanStepID string   `json:"plan_step_id,omitempty"`
}

type PlanVerificationResponse struct {
	ID            string          `json:"id"`
	IssueID       string          `json:"issue_id"`
	PlanID        string          `json:"plan_id"`
	PlanVersion   int32           `json:"plan_version"`
	TaskID        string          `json:"task_id"`
	SourceTaskID  string          `json:"source_task_id"`
	State         string          `json:"state"`
	Findings      json.RawMessage `json:"findings"`
	CriticalCount int32           `json:"critical_count"`
	MajorCount    int32           `json:"major_count"`
	MinorCount    int32           `json:"minor_count"`
	OutdatedCount int32           `json:"outdated_count"`
	Summary       *string         `json:"summary"`
	ReportedAt    *string         `json:"reported_at"`
	CreatedAt     string          `json:"created_at"`
}

func issuePlanToResponse(p db.IssuePlan) IssuePlanResponse {
	steps := json.RawMessage(p.Steps)
	if len(steps) == 0 {
		steps = json.RawMessage("[]")
	}
	return IssuePlanResponse{
		ID:             uuidToString(p.ID),
		IssueID:        uuidToString(p.IssueID),
		Version:        p.Version,
		Content:        p.Content,
		Steps:          steps,
		AuthorType:     p.AuthorType,
		AuthorID:       uuidToString(p.AuthorID),
		SupersededAt:   timestampToPtr(p.SupersededAt),
		MaterializedAt: timestampToPtr(p.MaterializedAt),
		CreatedAt:      timestampToString(p.CreatedAt),
	}
}

func planVerificationToResponse(v db.PlanVerification) PlanVerificationResponse {
	findings := json.RawMessage(v.Findings)
	if len(findings) == 0 {
		findings = json.RawMessage("[]")
	}
	return PlanVerificationResponse{
		ID:            uuidToString(v.ID),
		IssueID:       uuidToString(v.IssueID),
		PlanID:        uuidToString(v.PlanID),
		PlanVersion:   v.PlanVersion,
		TaskID:        uuidToString(v.TaskID),
		SourceTaskID:  uuidToString(v.SourceTaskID),
		State:         v.State,
		Findings:      findings,
		CriticalCount: v.CriticalCount,
		MajorCount:    v.MajorCount,
		MinorCount:    v.MinorCount,
		OutdatedCount: v.OutdatedCount,
		Summary:       textToPtr(v.Summary),
		ReportedAt:    timestampToPtr(v.ReportedAt),
		CreatedAt:     timestampToString(v.CreatedAt),
	}
}

func (h *Handler) GetIssuePlan(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	versions, err := h.Queries.ListIssuePlanVersions(r.Context(), db.ListIssuePlanVersionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("list issue plan versions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load plan")
		return
	}
	out := IssuePlanEnvelope{Versions: []IssuePlanResponse{}}
	for _, v := range versions {
		resp := issuePlanToResponse(v)
		out.Versions = append(out.Versions, resp)
		if !v.SupersededAt.Valid && out.Plan == nil {
			active := resp
			out.Plan = &active
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) SetIssuePlan(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Content string          `json:"content"`
		Steps   []IssuePlanStep `json:"steps"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, planContentMaxBytes*2)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len(req.Content) > planContentMaxBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("content exceeds %d bytes", planContentMaxBytes))
		return
	}
	normalized, err := normalizePlanSteps(req.Steps)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Steps = normalized
	steps, _ := json.Marshal(req.Steps)

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	ctx := r.Context()

	var plan db.IssuePlan
	// The version is computed in the INSERT; the unique index turns a
	// concurrent publish into a conflict worth one retry.
	for attempt := 0; attempt < 2; attempt++ {
		plan, err = h.Queries.CreateIssuePlan(ctx, db.CreateIssuePlanParams{
			WorkspaceID: issue.WorkspaceID,
			IssueID:     issue.ID,
			Content:     req.Content,
			Steps:       steps,
			AuthorType:  actorType,
			AuthorID:    parseUUID(actorID),
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		slog.Warn("create issue plan failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to save plan")
		return
	}
	if err := h.Queries.SupersedeOtherIssuePlans(ctx, db.SupersedeOtherIssuePlansParams{IssueID: issue.ID, ID: plan.ID}); err != nil {
		slog.Warn("supersede issue plans failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	// Plan Gate (K11): a plan with steps published from a run asks a human to
	// approve it before the steps become sub-issues.
	if len(req.Steps) > 0 && isMachineCredentialActor(r) {
		h.askPlanApproval(ctx, r, issue, plan, len(req.Steps), actorType, actorID)
	}
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	created := issuePlanToResponse(plan)
	writeJSON(w, http.StatusOK, IssuePlanEnvelope{Plan: &created, Versions: []IssuePlanResponse{created}})
}

func (h *Handler) ListPlanVerifications(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListPlanVerificationsByIssue(r.Context(), db.ListPlanVerificationsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("list plan verifications failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load verifications")
		return
	}
	out := make([]PlanVerificationResponse, 0, len(rows))
	for _, v := range rows {
		out = append(out, planVerificationToResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"verifications": out})
}

// ReportPlanVerification stores a verification run's findings. Idempotent:
// a repeat for the same run returns the stored report with 200.
func (h *Handler) ReportPlanVerification(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	runID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "runId"), "run id")
	if !ok {
		return
	}
	var req struct {
		Summary  string        `json:"summary"`
		Findings []PlanFinding `json:"findings"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, planFindingsMaxCount*planFindingMaxBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Findings == nil {
		writeError(w, http.StatusBadRequest, "findings is required (an empty array means no divergence)")
		return
	}
	if len(req.Findings) > planFindingsMaxCount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d findings", planFindingsMaxCount))
		return
	}
	var counts struct{ critical, major, minor, outdated int32 }
	for i := range req.Findings {
		f := &req.Findings[i]
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		f.Title = strings.TrimSpace(f.Title)
		if f.Title == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("findings[%d].title is required", i))
			return
		}
		if len(f.Detail) > planFindingMaxBytes {
			f.Detail = f.Detail[:planFindingMaxBytes]
		}
		switch f.Severity {
		case "critical":
			counts.critical++
		case "major":
			counts.major++
		case "minor":
			counts.minor++
		case "outdated":
			counts.outdated++
		default:
			// Unknown severity stays in the findings as data; it counts nowhere.
		}
	}
	findings, _ := json.Marshal(req.Findings)

	ctx := r.Context()
	existing, err := h.Queries.GetPlanVerificationByTask(ctx, runID)
	if err != nil || existing.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "verification run not found")
		return
	}
	reported, err := h.Queries.ReportPlanVerification(ctx, db.ReportPlanVerificationParams{
		TaskID:        runID,
		Findings:      findings,
		CriticalCount: counts.critical,
		MajorCount:    counts.major,
		MinorCount:    counts.minor,
		OutdatedCount: counts.outdated,
		Summary:       pgtype.Text{String: strings.TrimSpace(req.Summary), Valid: strings.TrimSpace(req.Summary) != ""},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Already reported: idempotent replay.
		writeJSON(w, http.StatusOK, map[string]any{"verification": planVerificationToResponse(existing), "replayed": true})
		return
	}
	if err != nil {
		slog.Warn("report plan verification failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to store report")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.postPlanVerificationComment(r, issue, reported, req.Findings, actorType, actorID)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"verification": planVerificationToResponse(reported)})
}

// publishIssueAuxChanged bumps the issue revision and emits issue:updated so
// clients admit the event and refetch the plan and its verifications.
func (h *Handler) publishIssueAuxChanged(r *http.Request, issue db.Issue, actorType, actorID string) {
	ctx := r.Context()
	fresh, err := h.Queries.TouchIssueRevision(ctx, issue.ID)
	if err != nil {
		slog.Warn("touch issue revision failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		return
	}
	resp := issueToResponse(fresh, h.getIssuePrefix(ctx, fresh.WorkspaceID))
	h.fillStatusCategory(ctx, fresh.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(fresh.WorkspaceID), actorType, actorID, map[string]any{
		"issue":        resp,
		"plan_changed": true,
	})
}
