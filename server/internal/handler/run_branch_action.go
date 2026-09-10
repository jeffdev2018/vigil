package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Promote / discard for the branch a terminal run delivered (JEF-255).
//
// Every worktree run ends on a branch in the user's own repository. Promote
// pushes that branch to origin and opens a pull request for it; discard
// deletes the branch and any worktree still registered for it. Neither is
// something the server can do itself — the repository lives on the daemon's
// machine — so this is a queue, not a request/response: POST enqueues a row,
// the runtime's next heartbeat claims it, and the daemon posts the outcome
// back. The lifecycle mirrors the worktree-revert channel (F09) exactly:
// pending → claimed → completed/failed, with a stale-claim release so a dead
// daemon cannot wedge the run's actions forever.
//
// The task row carries the terminal facts (promoted_at, promote_pr_url,
// discarded_at); the request row carries the in-flight work. A failure marks
// the request failed and leaves the task untouched, so the user can retry.

const (
	runBranchActionPromote = "promote"
	runBranchActionDiscard = "discard"

	runBranchActionStatusPending   = "pending"
	runBranchActionStatusClaimed   = "claimed"
	runBranchActionStatusCompleted = "completed"
	runBranchActionStatusFailed    = "failed"

	// runBranchActionClaimTimeout is how long a claim may sit before the
	// request goes back to pending. The 409 check counts a claimed request as
	// in flight, so a daemon that died holding one would otherwise block the
	// run until someone edited the database. Generously longer than the work
	// itself (a push or two local git deletions).
	runBranchActionClaimTimeout = 5 * time.Minute

	ErrCodeRunDiffNotFound         = "run_diff_not_found"
	ErrCodeRunNotPromotable        = "run_not_promotable"
	ErrCodeRunNotDiscardable       = "run_not_discardable"
	ErrCodeRunBranchActionInFlight = "run_branch_action_pending"
)

// BranchPullRequestCreator opens the pull request for a promoted branch. The
// URL is empty (with nil error) when no VCS provider covers the pushed remote:
// the push alone already satisfies "promote", so a missing provider is a
// reportable outcome, not a failure.
type BranchPullRequestCreator interface {
	CreateRunPullRequest(ctx context.Context, issue db.Issue, req db.RunBranchActionRequest, remoteURL, baseBranch string) (string, error)
}

// RunBranchActionResponse is what the enqueue endpoints and the daemon's
// result endpoint return.
type RunBranchActionResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`
}

func runBranchActionToResponse(row db.RunBranchActionRequest) RunBranchActionResponse {
	return RunBranchActionResponse{
		RequestID: uuidToString(row.ID),
		Status:    row.Status,
		Error:     row.Error,
		PRURL:     row.PrUrl,
	}
}

// RunDiffResponse is GET /api/tasks/{taskId}/diff. DiffStat is the raw stored
// numstat summary; DiffUnified is null when the run has no stored patch, and
// DiffTruncated tells those apart from "changed nothing" — a stat with no
// patch is exactly the over-the-bound case (see RecordTaskDiff).
type RunDiffResponse struct {
	DiffStat      json.RawMessage `json:"diff_stat"`
	DiffUnified   *string         `json:"diff_unified"`
	DiffTruncated bool            `json:"diff_truncated"`
}

// GetTaskDiff serves the diff a terminal run delivered on its branch.
//
// GET /api/tasks/{taskId}/diff
func (h *Handler) GetTaskDiff(w http.ResponseWriter, r *http.Request) {
	taskUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeErrorCode(w, http.StatusNotFound, ErrCodeRunDiffNotFound, "run not found")
		return
	}
	// Same tenant guard as the run's transcript: the task must belong to the
	// caller's workspace, whatever id the path carried.
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != h.resolveWorkspaceID(r) {
		writeErrorCode(w, http.StatusNotFound, ErrCodeRunDiffNotFound, "run not found")
		return
	}
	if strings.TrimSpace(task.BranchName.String) == "" || len(task.DiffStat) == 0 {
		writeErrorCode(w, http.StatusNotFound, ErrCodeRunDiffNotFound, "this run has no recorded diff yet")
		return
	}
	writeJSON(w, http.StatusOK, RunDiffResponse{
		DiffStat:      json.RawMessage(task.DiffStat),
		DiffUnified:   textToPtr(task.DiffUnified),
		DiffTruncated: !task.DiffUnified.Valid,
	})
}

// PromoteIssueRun enqueues a push of the run's branch to origin, after which
// the server opens a pull request for it.
//
// POST /api/issues/{id}/runs/{taskId}/promote
func (h *Handler) PromoteIssueRun(w http.ResponseWriter, r *http.Request) {
	h.requestRunBranchAction(w, r, runBranchActionPromote)
}

// DiscardIssueRun enqueues the deletion of the run's branch and worktree.
//
// POST /api/issues/{id}/runs/{taskId}/discard
func (h *Handler) DiscardIssueRun(w http.ResponseWriter, r *http.Request) {
	h.requestRunBranchAction(w, r, runBranchActionDiscard)
}

func (h *Handler) requestRunBranchAction(w http.ResponseWriter, r *http.Request, action string) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "taskId")
	if !ok {
		return
	}

	guardCode := ErrCodeRunNotPromotable
	if action == runBranchActionDiscard {
		guardCode = ErrCodeRunNotDiscardable
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	// The run must belong to the issue the caller proved access to. Without
	// this the taskId path segment would act on any run in any workspace.
	if uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	switch task.Status {
	case "completed", "failed", "cancelled":
	default:
		writeErrorCode(w, http.StatusConflict, guardCode, "only a finished run can be "+action+"d")
		return
	}
	branch := strings.TrimSpace(task.BranchName.String)
	if branch == "" {
		writeErrorCode(w, http.StatusConflict, guardCode, "this run has no branch to "+action)
		return
	}
	if task.PromotedAt.Valid || task.DiscardedAt.Valid {
		writeErrorCode(w, http.StatusConflict, guardCode, "this run's branch was already promoted or discarded")
		return
	}
	if !task.RuntimeID.Valid {
		writeErrorCode(w, http.StatusConflict, guardCode, "this run is not bound to a runtime")
		return
	}

	runtime, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID)
	if err != nil {
		writeErrorCode(w, http.StatusConflict, guardCode, "the runtime that produced this run is no longer registered")
		return
	}
	// Fail closed on the capability: a daemon that cannot run branch actions
	// would claim the request, ignore the field it does not know, and never
	// report — the user would watch a spinner that can only end in the
	// stale-claim sweeper.
	if !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityBranchActionV1) {
		writeErrorCode(w, http.StatusConflict, guardCode,
			"the Multica app on that machine does not support this action. Update it and try again")
		return
	}

	// A daemon that died holding a claim would block this run's actions for
	// good, because the in-flight check below counts 'claimed'. Released here
	// rather than on a timer: this is the only moment the staleness matters,
	// and it costs one UPDATE on a table that is normally empty.
	if _, err := h.Queries.ReleaseStaleRunBranchActionClaims(r.Context(),
		pgtype.Interval{Microseconds: runBranchActionClaimTimeout.Microseconds(), Valid: true}); err != nil {
		slog.Warn("release stale run branch action claims failed", "error", err)
	}

	if existing, err := h.Queries.GetPendingRunBranchActionForTask(r.Context(), task.ID); err == nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunBranchActionInFlight,
			"a "+existing.Action+" is already in progress for this run")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not check for an action already in progress")
		return
	}

	row, err := h.Queries.CreateRunBranchActionRequest(r.Context(), db.CreateRunBranchActionRequestParams{
		WorkspaceID: issue.WorkspaceID,
		TaskID:      task.ID,
		RuntimeID:   task.RuntimeID,
		Action:      action,
		BranchName:  branch,
		BaseBranch:  h.runBranchBaseHint(r.Context(), task),
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		slog.Error("create run branch action request failed", "task_id", uuidToString(task.ID), "action", action, "error", err)
		writeError(w, http.StatusInternalServerError, "could not request the "+action)
		return
	}

	// "member", not "user": the audit table's actor_type CHECK allows only
	// member / agent / system, and a rejected write would lose the record of
	// who asked for a destructive action. Written at request time, like F09:
	// "someone asked to push/delete this branch" is the fact being audited.
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditRunBranchAction, "task", task.ID, map[string]any{
		"request_id": uuidToString(row.ID),
		"issue_id":   uuidToString(issue.ID),
		"action":     action,
		"branch":     branch,
	}, nil)

	h.requestDaemonPendingWork(uuidToString(task.RuntimeID), protocol.PendingWorkKindBranchAction)
	h.publishIssueAuxChanged(r, issue, "member", userID)
	writeJSON(w, http.StatusCreated, runBranchActionToResponse(row))
}

// runBranchBaseHint is the base branch the request row records, best-effort:
// the default-branch hint of the issue project's github_repo resource when one
// exists. The daemon re-derives the real default before acting, so an empty
// hint costs nothing.
func (h *Handler) runBranchBaseHint(ctx context.Context, task db.AgentTaskQueue) string {
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil || !issue.ProjectID.Valid {
		return ""
	}
	rows, err := h.Queries.ListProjectResources(ctx, issue.ProjectID)
	if err != nil {
		return ""
	}
	for _, res := range rows {
		if res.ResourceType != "github_repo" {
			continue
		}
		var ref githubRepoRef
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			continue
		}
		if hint := strings.TrimSpace(ref.DefaultBranchHint); hint != "" {
			return hint
		}
	}
	return ""
}

// claimBranchActionForHeartbeat hands the runtime's oldest pending branch
// action to the daemon, with everything it needs to do the work in one
// payload.
//
// Returns nil when there is nothing to do, which is the overwhelmingly common
// case — the probe before it is a single index lookup.
func (h *Handler) claimBranchActionForHeartbeat(ctx context.Context, runtimeID pgtype.UUID) *protocol.DaemonHeartbeatPendingBranchAction {
	row, err := h.Queries.ClaimRunBranchActionRequest(ctx, runtimeID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("claim run branch action failed", "runtime_id", uuidToString(runtimeID), "error", err)
		}
		return nil
	}
	pending, err := h.buildBranchActionPayload(ctx, row)
	if err != nil {
		// The claim already happened, so settle it here rather than leaving the
		// user waiting for a sweeper: the request is unworkable, not delayed.
		h.settleBranchAction(ctx, row.ID, runBranchActionStatusFailed, "", err.Error())
		return nil
	}
	return pending
}

// buildBranchActionPayload resolves everything the daemon needs: the
// repository, the branch, the action and the base hint.
func (h *Handler) buildBranchActionPayload(ctx context.Context, row db.RunBranchActionRequest) (*protocol.DaemonHeartbeatPendingBranchAction, error) {
	task, err := h.Queries.GetAgentTask(ctx, row.TaskID)
	if err != nil {
		return nil, fmt.Errorf("the run this action targets no longer exists")
	}
	branch := strings.TrimSpace(task.BranchName.String)
	if branch == "" || branch != row.BranchName {
		return nil, fmt.Errorf("the run no longer carries the branch this action targets")
	}
	// The repository resolution is the revert channel's: the project's
	// local_directory resource bound to the runtime's own daemon.
	localPath, err := h.resolveWorktreeRevertLocalPath(ctx, task)
	if err != nil {
		return nil, err
	}
	return &protocol.DaemonHeartbeatPendingBranchAction{
		ID:         uuidToString(row.ID),
		TaskID:     uuidToString(row.TaskID),
		Action:     row.Action,
		LocalPath:  localPath,
		Branch:     branch,
		BaseBranch: row.BaseBranch,
	}, nil
}

// ReportBranchActionResult receives the daemon's outcome and, on success,
// records the run-side fact (promoted_at / discarded_at).
//
// POST /api/daemon/runtimes/{runtimeId}/branch-action/{requestId}/result
func (h *Handler) ReportBranchActionResult(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "requestId")
	if !ok {
		return
	}
	row, err := h.Queries.GetRunBranchActionRequest(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusNotFound, "branch action request not found")
		return
	}
	if uuidToString(row.RuntimeID) != uuidToString(runtime.ID) {
		writeError(w, http.StatusNotFound, "branch action request not found")
		return
	}
	// A retried report after the first one landed is not an error: the daemon
	// retries terminal reports, and these actions are destructive, so the
	// second delivery must change nothing.
	if row.Status == runBranchActionStatusCompleted || row.Status == runBranchActionStatusFailed {
		writeJSON(w, http.StatusOK, runBranchActionToResponse(row))
		return
	}

	var body struct {
		Status  string `json:"status"`
		HeadSHA string `json:"head_sha"`
		Error   string `json:"error"`
		// RemoteURL is the origin the daemon pushed to, on a successful
		// promote. The server parses it to find the VCS connection that can
		// open the pull request; empty means "pushed, but no remote URL to
		// attribute" and the PR step is skipped.
		RemoteURL string `json:"remote_url"`
		// DefaultBranch is the repository's default branch as the daemon
		// resolved it; it wins over the request's stored hint for PR creation.
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The issue behind the request's task feeds the PR title and the aux
	// repaint. Best-effort: neither blocks settling the request.
	var issue db.Issue
	var issueErr error
	if task, err := h.Queries.GetAgentTask(r.Context(), row.TaskID); err == nil {
		issue, issueErr = h.Queries.GetIssue(r.Context(), task.IssueID)
	} else {
		issueErr = err
	}

	if body.Status != "completed" {
		msg := strings.TrimSpace(body.Error)
		if msg == "" {
			msg = "the " + row.Action + " did not complete"
		}
		settled := h.settleBranchAction(r.Context(), row.ID, runBranchActionStatusFailed, "", msg)
		// The task row is deliberately NOT marked: a failed action is
		// retryable, and promoted_at/discarded_at are terminal facts.
		h.publishBranchActionChanged(r.Context(), issue, issueErr, row)
		writeJSON(w, http.StatusOK, settled)
		return
	}

	prURL := ""
	switch row.Action {
	case runBranchActionPromote:
		// The push already happened on the daemon; opening the PR is the
		// server's half. A provider problem must not un-push the branch, so a
		// failure here is logged and the promote still lands with an empty URL.
		if u, err := h.createRunPullRequest(r.Context(), issue, issueErr, row, body.RemoteURL, body.DefaultBranch); err != nil {
			slog.Warn("run promote: pushed, but the pull request could not be opened",
				"request_id", uuidToString(row.ID), "error", err)
		} else {
			prURL = u
		}
		if _, err := h.Queries.MarkTaskPromoted(r.Context(), db.MarkTaskPromotedParams{
			ID: row.TaskID, PromotePrUrl: prURL,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("run promote: could not mark the task", "task_id", uuidToString(row.TaskID), "error", err)
		}
	case runBranchActionDiscard:
		if _, err := h.Queries.MarkTaskDiscarded(r.Context(), row.TaskID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("run discard: could not mark the task", "task_id", uuidToString(row.TaskID), "error", err)
		}
	}

	settled := h.settleBranchAction(r.Context(), row.ID, runBranchActionStatusCompleted, prURL, "")
	h.audit(r.Context(), row.WorkspaceID, "member", uuidToString(row.CreatedBy), AuditRunBranchActionResult, "task", row.TaskID, map[string]any{
		"request_id": uuidToString(row.ID),
		"action":     row.Action,
		"branch":     row.BranchName,
		"pr_url":     prURL,
		"head_sha":   body.HeadSHA,
	}, nil)
	h.publishBranchActionChanged(r.Context(), issue, issueErr, row)
	writeJSON(w, http.StatusOK, settled)
}

// publishBranchActionChanged re-emits the issue aux event so every open client
// repaints the run's promote/discard affordance.
func (h *Handler) publishBranchActionChanged(ctx context.Context, issue db.Issue, issueErr error, row db.RunBranchActionRequest) {
	if issueErr != nil || !issue.ID.Valid {
		return
	}
	h.publishIssueAuxChangedCtx(ctx, issue, "member", uuidToString(row.CreatedBy))
}

// createRunPullRequest opens the PR for a promoted branch through the
// workspace's VCS connection. Returns ("", nil) when nothing covers the
// remote — the push alone satisfies promote.
func (h *Handler) createRunPullRequest(ctx context.Context, issue db.Issue, issueErr error, row db.RunBranchActionRequest, remoteURL, defaultBranch string) (string, error) {
	if strings.TrimSpace(remoteURL) == "" {
		return "", nil
	}
	var creator BranchPullRequestCreator = h.BranchPRCreator
	if creator == nil {
		creator = builtinBranchPRCreator{h: h}
	}
	if issueErr != nil {
		return "", fmt.Errorf("could not load the run's issue for the PR title: %w", issueErr)
	}
	base := strings.TrimSpace(defaultBranch)
	if base == "" {
		base = row.BaseBranch
	}
	return creator.CreateRunPullRequest(ctx, issue, row, remoteURL, base)
}

type builtinBranchPRCreator struct{ h *Handler }

// CreateRunPullRequest parses the pushed remote, finds the workspace VCS
// connection on the same host, and asks its provider to open the PR.
func (c builtinBranchPRCreator) CreateRunPullRequest(ctx context.Context, issue db.Issue, req db.RunBranchActionRequest, remoteURL, baseBranch string) (string, error) {
	host, owner, repo, err := parseGitRemoteURL(remoteURL)
	if err != nil {
		return "", err
	}
	conns, err := c.h.Queries.ListVCSConnectionsByWorkspace(ctx, req.WorkspaceID)
	if err != nil {
		return "", err
	}
	for _, conn := range conns {
		u, err := url.Parse(vcs.NormalizeInstanceURL(conn.InstanceUrl))
		if err != nil || !strings.EqualFold(u.Hostname(), host) {
			continue
		}
		provider, ok := vcs.For(conn.Provider)
		if !ok {
			return "", fmt.Errorf("unknown vcs provider %q", conn.Provider)
		}
		token, err := c.h.openVCSSecret(conn.AccessTokenEncrypted)
		if err != nil {
			return "", err
		}
		title := strings.TrimSpace(service.IssueIdentifier(c.h.getIssuePrefix(ctx, issue.WorkspaceID), issue.Number) + " " + issue.Title)
		body := fmt.Sprintf("Branch `%s` delivered by run %s.", req.BranchName, uuidToString(req.TaskID))
		created, err := provider.CreatePullRequest(ctx, conn.InstanceUrl, token, owner, repo, vcs.CreatePullRequestInput{
			Title: title,
			Head:  req.BranchName,
			Base:  baseBranch,
			Body:  body,
		})
		if err != nil {
			return "", err
		}
		return created.HTMLURL, nil
	}
	// No connection on that host: the push alone satisfies promote.
	return "", nil
}

// parseGitRemoteURL reads host, owner and repo name out of the three remote
// forms git accepts: https://host/owner/repo(.git), ssh://git@host/owner/repo
// and the scp-like git@host:owner/repo.git.
func parseGitRemoteURL(raw string) (host, owner, repo string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", "", errors.New("empty remote url")
	}
	if strings.Contains(s, "://") {
		u, perr := url.Parse(s)
		if perr != nil {
			return "", "", "", fmt.Errorf("could not parse remote url %q", perr)
		}
		return splitRemotePath(u.Hostname(), u.Path)
	}
	// scp-like syntax: [user@]host:owner/repo(.git)
	at := strings.LastIndex(s, "@")
	colon := strings.LastIndex(s, ":")
	if colon <= at {
		return "", "", "", fmt.Errorf("could not parse remote url %q", s)
	}
	return splitRemotePath(s[at+1:colon], s[colon+1:])
}

func splitRemotePath(host, path string) (string, string, string, error) {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.SplitN(path, "/", 2)
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("remote url does not name an owner and repository")
	}
	return host, parts[0], parts[1], nil
}

func (h *Handler) settleBranchAction(ctx context.Context, id pgtype.UUID, status, prURL, errMsg string) RunBranchActionResponse {
	row, err := h.Queries.SettleRunBranchActionRequest(ctx, db.SettleRunBranchActionRequestParams{
		ID:     id,
		Status: status,
		PrUrl:  pgtype.Text{String: prURL, Valid: prURL != ""},
		Error:  pgtype.Text{String: errMsg, Valid: true},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("settle run branch action failed", "request_id", uuidToString(id), "error", err)
		}
		return RunBranchActionResponse{RequestID: uuidToString(id), Status: status, Error: errMsg, PRURL: prURL}
	}
	return runBranchActionToResponse(row)
}

// pendingBranchActionByTask loads the in-flight promote/discard request for
// every run of one issue in a single query, so AgentTaskResponse gains
// pending_branch_action without an N+1 over the execution log.
func (h *Handler) pendingBranchActionByTask(ctx context.Context, issueID pgtype.UUID) map[string]string {
	rows, err := h.Queries.ListPendingRunBranchActionsForIssue(ctx, issueID)
	if err != nil {
		slog.Warn("run branch action: could not load pending requests", "issue_id", uuidToString(issueID), "error", err)
		return nil
	}
	byTask := make(map[string]string, len(rows))
	for _, row := range rows {
		byTask[uuidToString(row.TaskID)] = row.Action
	}
	return byTask
}
