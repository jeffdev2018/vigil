package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Cross-provider self-review (K15). When a code run completes with a diff
// (a PR, a branch or touched files), a second run on a different provider
// reviews that diff — only the diff, never the author's conversation — and
// ends with a structured report stored as a `review_report` task message.
// The reviewer is the least recently used agent of another provider. One
// provider only: nothing happens. The review is a signal beside the human
// review, never a gate.

const (
	AuditCrossReview                = "cross_review"
	crossReviewMessageType          = "review_report"
	crossReviewBrief                = "Cross-provider review. An agent running on %s just delivered a change on this issue. Review ONLY that diff, as an independent reader: do not read, resume or continue the author's conversation, and do not modify any code.\nDiff to review: %s\n%sEnd your run with a fenced block starting with ```review_report that contains one JSON object: {\"verdict\":\"approve\"|\"request_changes\"|\"comment\",\"risks\":[\"…\"],\"questions\":[\"…\"],\"suggestions\":[\"…\"]}."
	crossReviewDiffCap              = 60_000
	AuditCrossReviewSettingsChanged = "cross_review.settings_changed"
)

// PullRequestDiffFetcher returns the unified diff of the pull request the
// review is about; the built-in one reads GitHub through the App and
// Forgejo/GitLab through the workspace connection. Tests swap it.
type PullRequestDiffFetcher interface {
	FetchIssueDiff(ctx context.Context, issue db.Issue, prURL string) (string, error)
}

var crossReviewFence = regexp.MustCompile("(?s)```review_report\\s*(\\{.*?\\})\\s*```")

type CrossReviewReport struct {
	Verdict     string   `json:"verdict"`
	Risks       []string `json:"risks"`
	Questions   []string `json:"questions"`
	Suggestions []string `json:"suggestions"`
	Summary     string   `json:"summary,omitempty"`
}

type CrossReviewResponse struct {
	TaskID           string             `json:"task_id"`
	ReviewOfTaskID   string             `json:"review_of_task_id"`
	ReviewerAgentID  string             `json:"reviewer_agent_id"`
	ReviewerName     string             `json:"reviewer_name"`
	ReviewerProvider string             `json:"reviewer_provider"`
	Status           string             `json:"status"`
	Report           *CrossReviewReport `json:"report"`
	CreatedAt        string             `json:"created_at"`
	CompletedAt      *string            `json:"completed_at"`
}

// parseCrossReviewReport finds the fenced report in free text; without one
// the last non-empty paragraph becomes the summary of a plain comment.
func parseCrossReviewReport(text string) CrossReviewReport {
	report := CrossReviewReport{Verdict: "comment", Risks: []string{}, Questions: []string{}, Suggestions: []string{}}
	if m := crossReviewFence.FindStringSubmatch(text); m != nil {
		var parsed CrossReviewReport
		if json.Unmarshal([]byte(m[1]), &parsed) == nil {
			if parsed.Verdict == "approve" || parsed.Verdict == "request_changes" {
				report.Verdict = parsed.Verdict
			}
			report.Risks, report.Questions, report.Suggestions = nonNil(parsed.Risks), nonNil(parsed.Questions), nonNil(parsed.Suggestions)
			report.Summary = strings.TrimSpace(parsed.Summary)
			return report
		}
	}
	paragraphs := strings.Split(strings.TrimSpace(text), "\n\n")
	for i := len(paragraphs) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(paragraphs[i]); p != "" {
			if len(p) > 1000 {
				p = p[:1000] + "…"
			}
			report.Summary = p
			break
		}
	}
	return report
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// diffReference names what the reviewer must read: a PR, else a branch,
// else the touched files.
func diffReference(prURL, branch string, touched []string) string {
	switch {
	case prURL != "":
		return "pull request " + prURL
	case branch != "":
		return "branch " + branch
	case len(touched) > 0:
		return "uncommitted changes to " + strings.Join(touched, ", ")
	}
	return ""
}

func (h *Handler) runProvider(ctx context.Context, task db.AgentTaskQueue) string {
	if rt, err := h.Queries.GetAgentRuntime(ctx, task.RuntimeID); err == nil {
		return rt.Provider
	}
	return ""
}

// triggerCrossReview queues the review of a completed code run. Review runs
// are never reviewed themselves; a run without a diff has nothing to read;
// the workspace policy may switch the feature off or exclude the project.
func (h *Handler) triggerCrossReview(ctx context.Context, task db.AgentTaskQueue, prURL, branch string) {
	if task.ReviewOfTaskID.Valid || task.Status != "completed" {
		return
	}
	ref := diffReference(prURL, branch, jsonStrings(task.TouchedPaths))
	if ref == "" {
		return
	}
	if _, err := h.Queries.GetLatestCrossReviewForTask(ctx, task.ID); err == nil {
		return // already reviewed (a retry goes through RetryCrossReview)
	}
	h.startCrossReview(ctx, task, ref, prURL)
}

// diffBlock embeds the fetched diff (capped) in the brief; empty when the
// diff could not be read, in which case the reviewer reads it with git.
func (h *Handler) diffBlock(ctx context.Context, issue db.Issue, prURL string) string {
	var fetcher PullRequestDiffFetcher = h.DiffFetcher
	if fetcher == nil {
		fetcher = builtinDiffFetcher{h: h}
	}
	diff, err := fetcher.FetchIssueDiff(ctx, issue, prURL)
	if err != nil {
		slog.Info("cross review: diff not embedded", "issue_id", uuidToString(issue.ID), "error", err)
	}
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return "Read the diff yourself with git.\n"
	}
	if len(diff) > crossReviewDiffCap {
		diff = diff[:crossReviewDiffCap] + "\n… (diff truncated; read the rest with git)"
	}
	return "The diff:\n```diff\n" + diff + "\n```\n"
}

func (h *Handler) startCrossReview(ctx context.Context, task db.AgentTaskQueue, ref, prURL string) (db.AgentTaskQueue, error) {
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	if ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID); err == nil && !service.CrossReviewSettings(ws.Settings).Allows(uuidToString(issue.ProjectID)) {
		return db.AgentTaskQueue{}, errors.New("cross review is switched off for this project")
	}
	provider := h.runProvider(ctx, task)
	candidates, err := h.Queries.ListCrossReviewCandidates(ctx, db.ListCrossReviewCandidatesParams{WorkspaceID: issue.WorkspaceID, AuthorProvider: provider})
	if err != nil || len(candidates) == 0 {
		return db.AgentTaskQueue{}, errors.New("no reviewer on another provider")
	}
	reviewer := candidates[0]
	if provider == "" {
		provider = "another provider"
	}
	review, err := h.TaskService.EnqueueCrossReviewRun(ctx, issue, reviewer.ID, fmt.Sprintf(crossReviewBrief, provider, ref, h.diffBlock(ctx, issue, prURL)), task.OriginatorUserID)
	if err != nil {
		slog.Warn("cross review: enqueue failed", "task_id", uuidToString(task.ID), "error", err)
		return db.AgentTaskQueue{}, err
	}
	if review, err = h.Queries.SetTaskReviewOf(ctx, db.SetTaskReviewOfParams{ID: review.ID, ReviewOfTaskID: task.ID}); err != nil {
		return db.AgentTaskQueue{}, err
	}
	h.audit(ctx, issue.WorkspaceID, "system", "", AuditCrossReview, "issue", issue.ID, map[string]any{"review_task_id": uuidToString(review.ID), "review_of_task_id": uuidToString(task.ID), "reviewer_agent_id": uuidToString(reviewer.ID), "reviewer_provider": reviewer.Provider, "author_provider": provider, "diff": ref}, nil)
	h.publish("cross_review:queued", uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(issue.ID), "review_task_id": uuidToString(review.ID)})
	return review, nil
}

// storeCrossReviewReport turns a finished review run's output into one
// review_report message. Idempotent per run.
func (h *Handler) storeCrossReviewReport(ctx context.Context, task db.AgentTaskQueue, output string) {
	if !task.ReviewOfTaskID.Valid {
		return
	}
	if _, err := h.Queries.GetLatestReviewReportMessage(ctx, task.ID); err == nil {
		return
	}
	text := output
	if !crossReviewFence.MatchString(text) {
		if msgs, err := h.Queries.ListTaskMessages(ctx, task.ID); err == nil {
			var b strings.Builder
			for _, m := range msgs {
				if m.Type == "text" && m.Content.Valid {
					b.WriteString(m.Content.String)
					b.WriteString("\n\n")
				}
			}
			if crossReviewFence.MatchString(b.String()) || text == "" {
				text = b.String()
			}
		}
	}
	report := parseCrossReviewReport(text)
	raw, _ := json.Marshal(report)
	seq, _ := h.Queries.NextTaskMessageSeq(ctx, task.ID)
	if _, err := h.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{ID: dbid.NewV7(), TaskID: task.ID, Seq: int32(seq), Type: crossReviewMessageType, Content: pgtype.Text{String: string(raw), Valid: true}}); err != nil {
		slog.Warn("cross review: store report failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if issue, err := h.Queries.GetIssue(ctx, task.IssueID); err == nil {
		h.publish("cross_review:report", uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(issue.ID), "review_task_id": uuidToString(task.ID), "verdict": report.Verdict})
	}
}

func (h *Handler) crossReviewsToResponse(ctx context.Context, rows []db.ListCrossReviewsForIssueRow) []CrossReviewResponse {
	out := make([]CrossReviewResponse, 0, len(rows))
	for _, r := range rows {
		item := CrossReviewResponse{TaskID: uuidToString(r.ID), ReviewOfTaskID: uuidToString(r.ReviewOfTaskID), ReviewerAgentID: uuidToString(r.AgentID), ReviewerName: r.ReviewerName, ReviewerProvider: r.ReviewerProvider, Status: r.Status, CreatedAt: timestampToString(r.CreatedAt), CompletedAt: timestampToPtr(r.CompletedAt)}
		if m, err := h.Queries.GetLatestReviewReportMessage(ctx, r.ID); err == nil {
			var report CrossReviewReport
			if json.Unmarshal([]byte(m.Content.String), &report) == nil {
				report.Risks, report.Questions, report.Suggestions = nonNil(report.Risks), nonNil(report.Questions), nonNil(report.Suggestions)
				item.Report = &report
			}
		}
		out = append(out, item)
	}
	return out
}

// ListCrossReviews: GET /api/issues/{id}/cross-reviews — newest first.
func (h *Handler) ListCrossReviews(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListCrossReviewsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cross reviews")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": h.crossReviewsToResponse(r.Context(), rows)})
}

// RetryCrossReview: POST /api/issues/{id}/cross-reviews/retry — a new
// review run of the same code run after the previous one failed.
func (h *Handler) RetryCrossReview(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListCrossReviewsForIssue(r.Context(), issue.ID)
	if err != nil || len(rows) == 0 {
		writeError(w, http.StatusNotFound, "no cross review to retry on this issue")
		return
	}
	last := rows[0]
	if last.Status != "failed" && last.Status != "cancelled" {
		writeError(w, http.StatusConflict, "the latest cross review is "+last.Status+", not failed")
		return
	}
	author, err := h.Queries.GetAgentTask(r.Context(), last.ReviewOfTaskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "reviewed run not found")
		return
	}
	packet, _ := h.Queries.GetLatestHandoffPacket(r.Context(), issue.ID)
	prURL := handoffPRURL(packet)
	ref := diffReference(prURL, "", jsonStrings(author.TouchedPaths))
	if ref == "" {
		ref = "the latest change delivered on this issue"
	}
	review, err := h.startCrossReview(r.Context(), author, ref, prURL)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditCrossReview, "issue", issue.ID, map[string]any{"review_task_id": uuidToString(review.ID), "retry_of": uuidToString(last.ID), "retried": true}, nil)
	rows, _ = h.Queries.ListCrossReviewsForIssue(r.Context(), issue.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"reviews": h.crossReviewsToResponse(r.Context(), rows)})
}

// handoffPRURL reads the PR the completion handoff packet (K17) recorded.
func handoffPRURL(p db.HandoffPacket) string {
	for _, e := range jsonStrings(p.Evidence) {
		if strings.HasPrefix(e, "http") && (strings.Contains(e, "/pull") || strings.Contains(e, "/merge_requests/")) {
			return e
		}
	}
	return ""
}

// GetCrossReviewSettings: GET /api/cross-review-settings.
func (h *Handler) GetCrossReviewSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.CrossReviewSettings(ws.Settings))
}

// PutCrossReviewSettings: PUT /api/cross-review-settings {enabled, opt_out_project_ids}.
func (h *Handler) PutCrossReviewSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.CrossReview
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids := make([]string, 0, len(req.OptOutProjectIDs))
	for _, raw := range req.OptOutProjectIDs {
		pid, ok := parseUUIDOrBadRequest(w, raw, "project id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: pid, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "project "+raw+" is not in this workspace")
			return
		}
		ids = append(ids, raw)
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
	next := service.CrossReview{Enabled: req.Enabled, OptOutProjectIDs: ids}
	settings["cross_review"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save cross review settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditCrossReviewSettingsChanged, "workspace", wsUUID, map[string]any{"enabled": next.Enabled, "opt_out_project_ids": ids}, nil)
	writeJSON(w, http.StatusOK, next)
}
