package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CI auto-fix (K49). When the CI of a pull request that belongs to an
// agent's run goes red, the assignee agent gets one bounded correction run
// on that branch: the failing checks and their logs links in the brief,
// its own budget, one attempt per failing head and a cap per pull request.
// A human-authored pull request is never touched; the cap reached files an
// inbox item instead of another run; the workspace switch is off by default.

const (
	AuditCIAutoFix          = "ci_auto_fix"
	InboxTypeCIAutoFixLimit = "ci_auto_fix_exhausted"
	ErrCodeCIAutoFixOff     = "ci_auto_fix_disabled"
	ciAutoFixBrief          = "CI auto-fix: the checks went red on pull request %s (branch `%s`, head %s) after your last run. Check out that branch, read the failing checks below, fix the cause — never weaken or skip tests to make them pass — run the checks locally, then push to the same branch. Do not open a new pull request, merge, or change the issue.\nFailing checks:\n%s"
)

type CIAutoFixRunResponse struct {
	ID             string  `json:"id"`
	Provider       string  `json:"provider"`
	PullRequestID  string  `json:"pull_request_id"`
	HeadSha        string  `json:"head_sha"`
	IssueID        string  `json:"issue_id"`
	TaskID         *string `json:"task_id"`
	TaskStatus     string  `json:"task_status"`
	Attempt        int32   `json:"attempt"`
	BudgetUsdTicks int64   `json:"budget_usd_ticks"`
	Manual         bool    `json:"manual"`
	CreatedAt      string  `json:"created_at"`
}

// ciPullRequest is the provider-neutral view the dispatcher works on.
type ciPullRequest struct {
	provider string
	id       pgtype.UUID
	wsID     pgtype.UUID
	headSha  string
	branch   string
	htmlURL  string
	state    string
	issueIDs []pgtype.UUID
	failing  []string
}

func (h *Handler) autoFixVCSHead(ctx context.Context, conn db.VcsConnection, sha string) {
	prs, err := h.Queries.ListVCSPullRequestsForHead(ctx, db.ListVCSPullRequestsForHeadParams{ConnectionID: conn.ID, HeadSha: sha})
	if err != nil {
		return
	}
	for _, pr := range prs {
		h.autoFixVCSPR(ctx, pr, false)
	}
}

func (h *Handler) vcsFailingChecks(ctx context.Context, pr db.VcsPullRequest) []string {
	rows, _ := h.Queries.ListFailedVCSCommitStatuses(ctx, db.ListFailedVCSCommitStatusesParams{ConnectionID: pr.ConnectionID, Sha: pr.HeadSha})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		line := "- " + r.Context
		if r.Description.Valid && r.Description.String != "" {
			line += ": " + r.Description.String
		}
		if r.TargetUrl.Valid && r.TargetUrl.String != "" {
			line += " — " + r.TargetUrl.String
		}
		out = append(out, line)
	}
	return out
}

func (h *Handler) autoFixVCSPR(ctx context.Context, pr db.VcsPullRequest, manual bool) (db.CiAutoFixRun, error) {
	issueIDs, _ := h.Queries.ListIssueIDsForVCSPRHead(ctx, db.ListIssueIDsForVCSPRHeadParams{ConnectionID: pr.ConnectionID, HeadSha: pr.HeadSha})
	return h.autoFix(ctx, ciPullRequest{provider: "vcs", id: pr.ID, wsID: pr.WorkspaceID, headSha: pr.HeadSha, branch: pr.Branch.String, htmlURL: pr.HtmlUrl, state: pr.State, issueIDs: issueIDs, failing: h.vcsFailingChecks(ctx, pr)}, manual)
}

func (h *Handler) autoFixGitHubPR(ctx context.Context, pr db.GithubPullRequest) (db.CiAutoFixRun, error) {
	issueIDs, _ := h.Queries.ListIssueIDsForPullRequest(ctx, pr.ID)
	rows, _ := h.Queries.ListFailedGitHubCheckRuns(ctx, db.ListFailedGitHubCheckRunsParams{PrID: pr.ID, HeadSha: pr.HeadSha})
	failing := make([]string, 0, len(rows))
	for _, r := range rows {
		line := "- " + r.Name + ": " + r.Conclusion.String
		if r.DetailsUrl.Valid && r.DetailsUrl.String != "" {
			line += " — " + r.DetailsUrl.String
		}
		failing = append(failing, line)
	}
	return h.autoFix(ctx, ciPullRequest{provider: "github", id: pr.ID, wsID: pr.WorkspaceID, headSha: pr.HeadSha, branch: pr.Branch.String, htmlURL: pr.HtmlUrl, state: pr.State, issueIDs: issueIDs, failing: failing}, false)
}

var errCIAutoFixNotAgent = errors.New("the pull request does not belong to an agent run")
var errCIAutoFixDisabled = errors.New("ci auto-fix is disabled for this workspace")
var errCIAutoFixSeen = errors.New("this head was already handled")
var errCIAutoFixInFlight = errors.New("a correction run is already in flight for this pull request")

// agentRunForPR finds the issue and the agent run that own the branch.
func (h *Handler) agentRunForPR(ctx context.Context, pr ciPullRequest) (db.Issue, pgtype.UUID, bool) {
	for _, issueID := range pr.issueIDs {
		issue, err := h.Queries.GetIssue(ctx, issueID)
		if err != nil || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
			continue
		}
		tasks, _ := h.Queries.ListTaskBranchesForIssue(ctx, issue.ID)
		for _, t := range tasks {
			if pr.branch != "" && t.BranchName.String == pr.branch {
				return issue, t.ID, true
			}
		}
	}
	return db.Issue{}, pgtype.UUID{}, false
}

// autoFix decides and dispatches. manual bypasses the cap and the switch
// (a human asked), never the agent-ownership rule.
func (h *Handler) autoFix(ctx context.Context, pr ciPullRequest, manual bool) (db.CiAutoFixRun, error) {
	if pr.state != "open" || pr.headSha == "" {
		return db.CiAutoFixRun{}, errors.New("the pull request is not open")
	}
	ws, err := h.Queries.GetWorkspace(ctx, pr.wsID)
	if err != nil {
		return db.CiAutoFixRun{}, err
	}
	cfg := service.CIAutoFixSettings(ws.Settings)
	if !cfg.Enabled && !manual {
		return db.CiAutoFixRun{}, errCIAutoFixDisabled
	}
	issue, sourceTask, ok := h.agentRunForPR(ctx, pr)
	if !ok {
		return db.CiAutoFixRun{}, errCIAutoFixNotAgent
	}
	// A correction run still in flight will push a new head itself: wait for it.
	if last, err := h.Queries.GetLatestCIAutoFixRunForPullRequest(ctx, pr.id); err == nil && last.TaskID.Valid {
		if t, err := h.Queries.GetAgentTask(ctx, last.TaskID); err == nil && (t.Status == "queued" || t.Status == "dispatched" || t.Status == "running" || t.Status == "paused" || t.Status == "waiting_local_directory") {
			return db.CiAutoFixRun{}, errCIAutoFixInFlight
		}
	}
	attempts, _ := h.Queries.CountCIAutoFixRunsForPullRequest(ctx, pr.id)
	if int(attempts) >= cfg.MaxAttempts && !manual {
		h.notifyCIAutoFixExhausted(ctx, pr, issue, int(attempts))
		return db.CiAutoFixRun{}, fmt.Errorf("ci auto-fix attempts exhausted (%d)", attempts)
	}
	row, err := h.Queries.CreateCIAutoFixRun(ctx, db.CreateCIAutoFixRunParams{ID: dbid.NewV7(), WorkspaceID: pr.wsID, Provider: pr.provider, PullRequestID: pr.id, HeadSha: pr.headSha, IssueID: issue.ID, SourceTaskID: sourceTask, Attempt: attempts + 1, BudgetUsdTicks: cfg.BudgetUsdTicks, Manual: manual})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CiAutoFixRun{}, errCIAutoFixSeen
	}
	if err != nil {
		return db.CiAutoFixRun{}, err
	}
	checks := strings.Join(pr.failing, "\n")
	if checks == "" {
		checks = "- (the provider reported a failure without a named check; open the pull request page)"
	}
	brief := fmt.Sprintf(ciAutoFixBrief, pr.htmlURL, pr.branch, shortSha(pr.headSha), checks)
	task, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, brief, pgtype.UUID{})
	if err != nil {
		slog.Warn("ci auto-fix: enqueue failed", "issue_id", uuidToString(issue.ID), "error", err)
		_ = h.Queries.DeleteCIAutoFixRun(ctx, row.ID) // the head stays eligible for a later red
		return db.CiAutoFixRun{}, err
	}
	_ = h.Queries.SetCIAutoFixRunTask(ctx, db.SetCIAutoFixRunTaskParams{ID: row.ID, TaskID: task.ID})
	row.TaskID = task.ID
	h.audit(ctx, pr.wsID, "system", "", AuditCIAutoFix, "issue", issue.ID, map[string]any{"run_id": uuidToString(row.ID), "task_id": uuidToString(task.ID), "pull_request_id": uuidToString(pr.id), "provider": pr.provider, "head_sha": pr.headSha, "attempt": attempts + 1, "manual": manual, "budget_usd_ticks": cfg.BudgetUsdTicks}, nil)
	h.publish("ci_auto_fix:queued", uuidToString(pr.wsID), "system", "", map[string]any{"issue_id": uuidToString(issue.ID), "pull_request_id": uuidToString(pr.id), "task_id": uuidToString(task.ID)})
	return row, nil
}

func shortSha(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// notifyCIAutoFixExhausted files one inbox item per manager when the cap
// is reached, once per (pull request, head).
func (h *Handler) notifyCIAutoFixExhausted(ctx context.Context, pr ciPullRequest, issue db.Issue, attempts int) {
	if exists, err := h.Queries.CIAutoFixExhaustedNoteExists(ctx, db.CIAutoFixExhaustedNoteExistsParams{WorkspaceID: pr.wsID, PullRequestID: uuidToString(pr.id), HeadSha: pr.headSha}); err == nil && exists {
		return // one note per (pull request, head)
	}
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, pr.wsID)
	if err != nil {
		return
	}
	details, _ := json.Marshal(map[string]any{"pull_request_id": uuidToString(pr.id), "provider": pr.provider, "head_sha": pr.headSha, "attempts": attempts, "html_url": pr.htmlURL})
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: pr.wsID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: InboxTypeCIAutoFixLimit, Severity: "action_required",
			IssueID: issue.ID, Title: fmt.Sprintf("CI still red after %d auto-fix attempt(s)", attempts),
			Body:      pgtype.Text{String: "The pull request " + pr.htmlURL + " needs a human: retry the auto-fix by hand or fix it yourself.", Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(pr.wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
	h.audit(ctx, pr.wsID, "system", "", AuditCIAutoFix, "issue", issue.ID, map[string]any{"pull_request_id": uuidToString(pr.id), "head_sha": pr.headSha, "attempts": attempts, "exhausted": true}, nil)
}

// enforceCIAutoFixBudget stops a correction run that spent its own budget.
func (h *Handler) enforceCIAutoFixBudget(ctx context.Context, task db.AgentTaskQueue) {
	if task.Status != "running" {
		return
	}
	run, err := h.Queries.GetCIAutoFixRunForTask(ctx, task.ID)
	if err != nil || run.BudgetUsdTicks <= 0 {
		return
	}
	spent, err := h.Queries.SumTaskCostTicks(ctx, task.ID)
	if err != nil || spent < run.BudgetUsdTicks {
		return
	}
	msg := fmt.Sprintf("CI auto-fix run stopped by its budget: %d of %d cost ticks", spent, run.BudgetUsdTicks)
	if _, err := h.TaskService.FailTask(ctx, task.ID, msg, "", "", "", service.ReasonBudgetExceeded, false, "", ""); err != nil {
		slog.Warn("ci auto-fix: budget stop failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	h.audit(ctx, run.WorkspaceID, "system", "", AuditCIAutoFix, "issue", run.IssueID, map[string]any{"run_id": uuidToString(run.ID), "task_id": uuidToString(task.ID), "budget_exceeded": true, "spent": spent, "budget": run.BudgetUsdTicks}, nil)
}

func ciAutoFixToResponse(r db.ListCIAutoFixRunsForIssueRow) CIAutoFixRunResponse {
	return CIAutoFixRunResponse{ID: uuidToString(r.ID), Provider: r.Provider, PullRequestID: uuidToString(r.PullRequestID), HeadSha: r.HeadSha, IssueID: uuidToString(r.IssueID), TaskID: uuidToPtr(r.TaskID), TaskStatus: r.TaskStatus.String, Attempt: r.Attempt, BudgetUsdTicks: r.BudgetUsdTicks, Manual: r.Manual, CreatedAt: timestampToString(r.CreatedAt)}
}

// ListIssueCIAutoFix: GET /api/issues/{id}/ci-auto-fix.
func (h *Handler) ListIssueCIAutoFix(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.CIAutoFixSettings(ws.Settings)
	rows, _ := h.Queries.ListCIAutoFixRunsForIssue(r.Context(), issue.ID)
	out := make([]CIAutoFixRunResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, ciAutoFixToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out, "enabled": cfg.Enabled, "max_attempts": cfg.MaxAttempts})
}

// RetryCIAutoFix: POST /api/pull-requests/{id}/ci-auto-fix/retry — a human
// asks for one more correction run past the cap. The head must not have
// been handled yet by the same request (a fresh push makes a new head).
func (h *Handler) RetryCIAutoFix(w http.ResponseWriter, r *http.Request) {
	prID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "pull request id")
	if !ok {
		return
	}
	var run db.CiAutoFixRun
	var err error
	var wsID pgtype.UUID
	if pr, gerr := h.Queries.GetGitHubPullRequestByID(r.Context(), prID); gerr == nil {
		wsID = pr.WorkspaceID
		if _, ok := h.permissionProfileScope(w, r); !ok {
			return
		}
		run, err = h.autoFixGitHubPRManual(r.Context(), pr)
	} else if pr, verr := h.Queries.GetVCSPullRequestByID(r.Context(), prID); verr == nil {
		wsID = pr.WorkspaceID
		if _, ok := h.permissionProfileScope(w, r); !ok {
			return
		}
		run, err = h.autoFixVCSPR(r.Context(), pr, true)
	} else {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	switch {
	case errors.Is(err, errCIAutoFixNotAgent):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, errCIAutoFixSeen), errors.Is(err, errCIAutoFixInFlight):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(wsID))
	h.audit(r.Context(), wsID, actorType, actorID, AuditCIAutoFix, "issue", run.IssueID, map[string]any{"run_id": uuidToString(run.ID), "pull_request_id": uuidToString(prID), "manual_retry": true}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"run": CIAutoFixRunResponse{ID: uuidToString(run.ID), Provider: run.Provider, PullRequestID: uuidToString(run.PullRequestID), HeadSha: run.HeadSha, IssueID: uuidToString(run.IssueID), TaskID: uuidToPtr(run.TaskID), TaskStatus: "queued", Attempt: run.Attempt, BudgetUsdTicks: run.BudgetUsdTicks, Manual: run.Manual, CreatedAt: timestampToString(run.CreatedAt)}})
}

func (h *Handler) autoFixGitHubPRManual(ctx context.Context, pr db.GithubPullRequest) (db.CiAutoFixRun, error) {
	issueIDs, _ := h.Queries.ListIssueIDsForPullRequest(ctx, pr.ID)
	rows, _ := h.Queries.ListFailedGitHubCheckRuns(ctx, db.ListFailedGitHubCheckRunsParams{PrID: pr.ID, HeadSha: pr.HeadSha})
	failing := make([]string, 0, len(rows))
	for _, r := range rows {
		failing = append(failing, "- "+r.Name+": "+r.Conclusion.String)
	}
	return h.autoFix(ctx, ciPullRequest{provider: "github", id: pr.ID, wsID: pr.WorkspaceID, headSha: pr.HeadSha, branch: pr.Branch.String, htmlURL: pr.HtmlUrl, state: pr.State, issueIDs: issueIDs, failing: failing}, true)
}

// GetCIAutoFixSettings: GET /api/ci-auto-fix-settings.
func (h *Handler) GetCIAutoFixSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.CIAutoFixSettings(ws.Settings))
}

// PutCIAutoFixSettings: PUT /api/ci-auto-fix-settings {enabled, max_attempts, budget_usd_ticks}.
func (h *Handler) PutCIAutoFixSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.CIAutoFix
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MaxAttempts < 1 || req.MaxAttempts > 20 || req.BudgetUsdTicks < 0 {
		writeError(w, http.StatusBadRequest, "max_attempts must be between 1 and 20 and budget_usd_ticks >= 0")
		return
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
	settings["ci_auto_fix"] = req
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save ci auto-fix settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditCIAutoFix, "workspace", wsUUID, map[string]any{"enabled": req.Enabled, "max_attempts": req.MaxAttempts, "budget_usd_ticks": req.BudgetUsdTicks, "settings": true}, nil)
	writeJSON(w, http.StatusOK, req)
}
