package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Review cockpit (K16): one read that gathers what a reviewer of an agent's
// work otherwise collects from four screens — the run, its pull requests
// and merge readiness, its cost, the questions still open, the acceptance
// criteria with their proofs, the plan verification. Nothing new is stored;
// a source that fails is named in failed_sections and the rest still
// renders.

// ReviewCockpitRun is the reviewer's view of a run: what it did and how it
// ended, without the daemon-only fields the task endpoint carries.
type ReviewCockpitRun struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	AgentID       string  `json:"agent_id"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
	Error         *string `json:"error"`
	FailureReason string  `json:"failure_reason,omitempty"`
	HandoffNote   string  `json:"handoff_note,omitempty"`
}

func reviewCockpitRun(t db.AgentTaskQueue) ReviewCockpitRun {
	return ReviewCockpitRun{
		ID: uuidToString(t.ID), Status: t.Status, AgentID: uuidToString(t.AgentID),
		CreatedAt: timestampToString(t.CreatedAt), StartedAt: timestampToPtr(t.StartedAt), CompletedAt: timestampToPtr(t.CompletedAt),
		Error: textToPtr(t.Error), FailureReason: t.FailureReason.String, HandoffNote: t.HandoffNote.String,
	}
}

type ReviewCockpitUsage struct {
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	CostUsdTicks     *int64 `json:"cost_usd_ticks"`
	// Uncosted is true when some usage rows carry no price: the cost shown is
	// a floor, not the total.
	Uncosted bool `json:"uncosted"`
}

type ReviewCockpitResponse struct {
	Issue            IssueResponse             `json:"issue"`
	Run              *ReviewCockpitRun         `json:"run"`
	Runs             []ReviewCockpitRun        `json:"runs"`
	MergeReadiness   *MergeReadinessResponse   `json:"merge_readiness"`
	Usage            *ReviewCockpitUsage       `json:"usage"`
	OpenQuestions    []IssueDecisionResponse   `json:"open_questions"`
	Criteria         []AcceptanceCriterion     `json:"criteria"`
	PlanVerification *PlanVerificationResponse `json:"plan_verification"`
	// SelfReview is reserved for K15's cross-provider verdict; null until then.
	SelfReview     any      `json:"self_review"`
	FailedSections []string `json:"failed_sections"`
}

const reviewCockpitMaxRuns = 20

// GetReviewCockpit — GET /api/issues/{id}/review-cockpit?run_id=.
func (h *Handler) GetReviewCockpit(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	out := ReviewCockpitResponse{
		Runs:           []ReviewCockpitRun{},
		OpenQuestions:  []IssueDecisionResponse{},
		Criteria:       parseAcceptanceCriteria(issue.AcceptanceCriteria),
		FailedSections: []string{},
	}
	out.Issue = issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, &out.Issue)
	fail := func(section string, err error) {
		slog.Warn("review cockpit: section failed", "section", section, "error", err, "issue_id", uuidToString(issue.ID))
		out.FailedSections = append(out.FailedSections, section)
	}

	// Runs: newest first; the selected one (or the newest) is the run shown.
	var selected *db.AgentTaskQueue
	if tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID); err != nil {
		fail("runs", err)
	} else {
		wanted := r.URL.Query().Get("run_id")
		for i := range tasks {
			t := tasks[i]
			if len(out.Runs) < reviewCockpitMaxRuns {
				out.Runs = append(out.Runs, reviewCockpitRun(t))
			}
			if wanted != "" && uuidToString(t.ID) == wanted {
				selected = &tasks[i]
			}
		}
		if wanted != "" && selected == nil {
			writeError(w, http.StatusNotFound, "run not found on this issue")
			return
		}
		if selected == nil && len(tasks) > 0 {
			selected = &tasks[0]
		}
	}
	if selected != nil {
		run := reviewCockpitRun(*selected)
		out.Run = &run
		if usage, err := h.runUsage(ctx, selected.ID); err != nil {
			fail("usage", err)
		} else {
			out.Usage = &usage
		}
	}

	if mr, err := h.mergeReadinessFor(ctx, issue); err != nil {
		fail("merge_readiness", err)
	} else {
		out.MergeReadiness = &mr
	}

	if decisions, err := h.Queries.ListIssueDecisions(ctx, db.ListIssueDecisionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		fail("open_questions", err)
	} else {
		for _, d := range decisions {
			if len(d.Response) == 0 {
				out.OpenQuestions = append(out.OpenQuestions, issueDecisionToResponse(d))
			}
		}
	}

	if v, err := h.Queries.GetLatestReportedPlanVerification(ctx, issue.ID); err == nil {
		resp := planVerificationToResponse(v)
		out.PlanVerification = &resp
	} else if !errors.Is(err, pgx.ErrNoRows) {
		fail("plan_verification", err)
	}

	writeJSON(w, http.StatusOK, out)
}

// runUsage sums the run's usage rows; a row without a price marks the total
// as a floor.
func (h *Handler) runUsage(ctx context.Context, taskID pgtype.UUID) (ReviewCockpitUsage, error) {
	rows, err := h.Queries.GetTaskUsage(ctx, taskID)
	if err != nil {
		return ReviewCockpitUsage{}, err
	}
	var u ReviewCockpitUsage
	var cost int64
	priced := false
	for _, row := range rows {
		u.InputTokens += row.InputTokens
		u.OutputTokens += row.OutputTokens
		u.CacheReadTokens += row.CacheReadTokens
		u.CacheWriteTokens += row.CacheWriteTokens
		if row.CostUsdTicks.Valid {
			cost += row.CostUsdTicks.Int64
			priced = true
		} else {
			u.Uncosted = true
		}
	}
	if priced {
		u.CostUsdTicks = &cost
	}
	return u, nil
}
