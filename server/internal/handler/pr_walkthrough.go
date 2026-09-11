package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/ghdiff"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Narrative pull request walkthrough (F05 / JEF-16).
//
// Generation is an ordinary read-only agent run, not a new CLI command: the
// same shape as cross review (K15) and the task watchdog (K73). The run is
// enqueued on the ISSUE the pull request is linked to, briefed with the capped
// unified diff, and answers with one fenced ```pr_walkthrough``` block.
//
// A pull request with no linked issue never gets one. That is the whole
// admission rule, and it is deliberate: the issue is what gives the run its
// context, its workspace and its accountable human.
//
// The row is keyed by (pr_source, pr_id, head_sha) — see migration 743. The
// consequences are worth stating because they are the two failure modes this
// feature would otherwise have:
//
//   - the unique index IS the "at most one run per head" fence. Two triggers
//     firing on one push (the GitHub snapshot pipeline and a VCS webhook can
//     both observe the same move) race on the insert and exactly one wins.
//   - a completion for a head the PR has already moved past settles into ITS
//     OWN row. It never overwrites the current head's, because it never looks
//     at which head is current. Readers ask for the current head and get
//     `pending` until that head's own run lands.

const (
	AuditPrWalkthroughConfigured = "pr_walkthrough.configured"
	AuditPrWalkthroughRequested  = "pr_walkthrough.requested"

	// EventPrWalkthroughUpdated is published on workspace scope so an open
	// issue panel re-queries the walkthrough it is showing.
	EventPrWalkthroughUpdated = "pr_walkthrough:updated"

	prWalkthroughSourceGitHub = "github"
	prWalkthroughSourceVCS    = "vcs"
)

var prWalkthroughFence = regexp.MustCompile("(?s)```pr_walkthrough\\s*(\\{.*?\\})\\s*```")

// --- responses -------------------------------------------------------------

// PrWalkthroughResponse is what the issue panel reads. `state` is the only
// thing the UI branches on, and `pending` is a real answer — a run is out.
type PrWalkthroughResponse struct {
	State        string                       `json:"state"`
	HeadSha      string                       `json:"head_sha"`
	Truncated    bool                         `json:"truncated"`
	OmittedFiles int32                        `json:"omitted_files"`
	Groups       []service.PrWalkthroughGroup `json:"groups"`
	GeneratedAt  string                       `json:"generated_at"`
	Error        string                       `json:"error"`
}

type prWalkthroughRefreshResponse struct {
	TaskID string `json:"task_id"`
}

func prWalkthroughToResponse(row db.PrWalkthrough) PrWalkthroughResponse {
	groups := []service.PrWalkthroughGroup{}
	if len(row.Groups) > 0 {
		// Stored groups were normalised on ingest. A decode failure here means
		// the column was written by something else; an empty narrative is a
		// better answer than a 500 on an issue page.
		if err := json.Unmarshal(row.Groups, &groups); err != nil {
			groups = []service.PrWalkthroughGroup{}
		}
		// Re-applying the fixed order on read costs nothing and keeps the
		// contract even for rows written by an older build or by hand.
		groups = service.NormalizePrWalkthroughGroups(groups)
	}
	out := PrWalkthroughResponse{
		State:        row.State,
		HeadSha:      row.HeadSha,
		Truncated:    row.Truncated,
		OmittedFiles: row.OmittedFiles,
		Groups:       groups,
		Error:        row.Error,
	}
	if row.UpdatedAt.Valid {
		out.GeneratedAt = row.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return out
}

// pendingPrWalkthrough is the answer for a head no run has settled yet. It is
// deliberately the same shape as a ready one so the client has one branch.
func pendingPrWalkthrough(headSHA string) PrWalkthroughResponse {
	return PrWalkthroughResponse{State: "pending", HeadSha: headSHA, Groups: []service.PrWalkthroughGroup{}}
}

// --- configuration ---------------------------------------------------------

// GetPrWalkthroughSettings: GET /api/pr-walkthrough/settings.
func (h *Handler) GetPrWalkthroughSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.PrWalkthroughFromSettings(ws.Settings))
}

// PutPrWalkthroughSettings: PUT /api/pr-walkthrough/settings. Owner/admin only:
// this spends agent budget on every push to every linked pull request.
func (h *Handler) PutPrWalkthroughSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.PrWalkthroughSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	if err := service.ValidatePrWalkthroughSettings(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if req.AgentID != "" {
		agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil || agent.WorkspaceID != wsUUID || agent.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "agent_id must be an active agent of this workspace")
			return
		}
	}
	if err := h.savePrWalkthroughSettings(r.Context(), wsUUID, ws.Settings, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the pull request walkthrough settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPrWalkthroughConfigured, "workspace", wsUUID,
		map[string]any{"enabled": req.Enabled, "agent_id": req.AgentID}, nil)
	writeJSON(w, http.StatusOK, req)
}

// savePrWalkthroughSettings writes the block back into the workspace settings
// blob, preserving every other key.
func (h *Handler) savePrWalkthroughSettings(ctx context.Context, wsID pgtype.UUID, current []byte, cfg service.PrWalkthroughSettings) error {
	settings := map[string]any{}
	if len(current) > 0 {
		_ = json.Unmarshal(current, &settings)
	}
	settings["pr_walkthrough"] = cfg
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = h.Queries.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: wsID, Settings: raw})
	return err
}

// --- resolving the pull request --------------------------------------------

// linkedPR is a pull request resolved from a path param, in whichever table it
// lives, already proven to be linked to the issue.
type linkedPR struct {
	source  string
	id      pgtype.UUID
	headSHA string
	// htmlURL is how the shared PullRequestDiffFetcher (K15) picks this exact
	// pull request out of the issue's links.
	htmlURL string
}

// loadLinkedPR resolves {prId} against the issue's linked pull requests. It
// answers only from the issue's OWN link lists, so an id belonging to another
// issue — or another workspace — is indistinguishable from one that does not
// exist, which is why both endpoints return 404 rather than 403.
func (h *Handler) loadLinkedPR(ctx context.Context, issue db.Issue, prID string) (linkedPR, bool) {
	want, err := parseUUIDChecked(prID)
	if err != nil {
		return linkedPR{}, false
	}
	if rows, err := h.Queries.ListPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range rows {
			if pr.ID == want {
				return linkedPR{source: prWalkthroughSourceGitHub, id: pr.ID, headSHA: pr.HeadSha, htmlURL: pr.HtmlUrl}, true
			}
		}
	}
	if rows, err := h.Queries.ListVCSPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range rows {
			if pr.ID == want {
				return linkedPR{source: prWalkthroughSourceVCS, id: pr.ID, headSHA: pr.HeadSha, htmlURL: pr.HtmlUrl}, true
			}
		}
	}
	return linkedPR{}, false
}

// currentHeadSHA reads the head the pull request is on right now, without
// going through an issue's link list.
func (h *Handler) currentHeadSHA(ctx context.Context, source string, prID pgtype.UUID) string {
	switch source {
	case prWalkthroughSourceGitHub:
		if row, err := h.Queries.GetGitHubPullRequestByID(ctx, prID); err == nil {
			return row.HeadSha
		}
	case prWalkthroughSourceVCS:
		if row, err := h.Queries.GetVCSPullRequestByID(ctx, prID); err == nil {
			return row.HeadSha
		}
	}
	return ""
}

// --- read ------------------------------------------------------------------

// GetIssuePrWalkthrough: GET /api/issues/{id}/pull-requests/{prId}/walkthrough.
// Always answers for the PR's CURRENT head, so a stale head's finished
// walkthrough never shows up as the story of a change it does not describe.
func (h *Handler) GetIssuePrWalkthrough(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	pr, ok := h.loadLinkedPR(r.Context(), issue, chi.URLParam(r, "prId"))
	if !ok {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	row, err := h.Queries.GetPrWalkthroughForHead(r.Context(), db.GetPrWalkthroughForHeadParams{
		PrSource: pr.source, PrID: pr.id, HeadSha: pr.headSHA,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, pendingPrWalkthrough(pr.headSHA))
		return
	}
	writeJSON(w, http.StatusOK, prWalkthroughToResponse(row))
}

// RefreshIssuePrWalkthrough: POST …/walkthrough/refresh. Manual regeneration
// for the current head.
func (h *Handler) RefreshIssuePrWalkthrough(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	pr, ok := h.loadLinkedPR(r.Context(), issue, chi.URLParam(r, "prId"))
	if !ok {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.PrWalkthroughFromSettings(ws.Settings)
	if !cfg.Enabled || cfg.AgentID == "" {
		// 409 rather than 403: the caller is allowed, the feature is off.
		writeError(w, http.StatusConflict, "walkthrough_disabled: pull request walkthroughs are switched off for this workspace")
		return
	}
	task, err := h.enqueuePrWalkthrough(r.Context(), issue, cfg, pr, parseUUIDOrZero(requestUserID(r)))
	if err != nil {
		if errors.Is(err, errPrWalkthroughDiffUnavailable) {
			writeErrorCode(w, http.StatusBadGateway, "diff_unavailable", "could not read the pull request diff: "+strings.TrimPrefix(err.Error(), errPrWalkthroughDiffUnavailable.Error()+": "))
			return
		}
		if errors.Is(err, errPrWalkthroughAlreadyRunning) {
			writeError(w, http.StatusConflict, "a walkthrough for this revision is already being generated")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to start the walkthrough run")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditPrWalkthroughRequested, "issue", issue.ID,
		map[string]any{"pr_source": pr.source, "pr_id": uuidToString(pr.id), "head_sha": pr.headSHA, "task_id": uuidToString(task.ID)}, nil)
	writeJSON(w, http.StatusAccepted, prWalkthroughRefreshResponse{TaskID: uuidToString(task.ID)})
}

// --- enqueue ---------------------------------------------------------------

// errPrWalkthroughAlreadyRunning is the unique index refusing a second claim
// for one head, surfaced so the manual endpoint can answer 409 instead of
// pretending it enqueued something.
var errPrWalkthroughAlreadyRunning = errors.New("a walkthrough run for this head is already in flight")

// errPrWalkthroughDiffUnavailable wraps a diff fetch failure so the refresh
// endpoint can answer 502 with the provider's reason instead of a bare 500:
// the row is already settled failed with that reason, and the reviewer can
// retry once the provider (or the app installation) is back.
var errPrWalkthroughDiffUnavailable = errors.New("diff unavailable")

// enqueuePrWalkthrough claims the head, fetches and caps the diff, and queues
// the read-only run.
//
// The claim comes FIRST, before the diff fetch. That ordering is the fence: a
// second trigger for the same push is refused at the index rather than after
// it has already spent a forge API call. When anything downstream then fails
// the row is settled `failed`, so the head is never left claimed-but-silent.
func (h *Handler) enqueuePrWalkthrough(ctx context.Context, issue db.Issue, cfg service.PrWalkthroughSettings, pr linkedPR, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	if pr.headSHA == "" {
		return db.AgentTaskQueue{}, errors.New("pull request has no head commit")
	}
	agentID, err := parseUUIDChecked(cfg.AgentID)
	if err != nil {
		return db.AgentTaskQueue{}, errors.New("walkthrough agent is not a valid id")
	}
	row, err := h.Queries.ClaimPrWalkthroughHead(ctx, db.ClaimPrWalkthroughHeadParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		PrSource: pr.source, PrID: pr.id, HeadSha: pr.headSHA,
	})
	if err != nil {
		// No row came back: a run for this head is still pending.
		return db.AgentTaskQueue{}, errPrWalkthroughAlreadyRunning
	}

	diff, err := h.fetchWalkthroughDiff(ctx, issue, pr)
	if err != nil {
		h.failPrWalkthrough(ctx, row, "could not read the pull request diff: "+err.Error())
		return db.AgentTaskQueue{}, fmt.Errorf("%w: %v", errPrWalkthroughDiffUnavailable, err)
	}
	if diff.Truncated {
		// Recorded now, not at completion: the cap is a property of what the
		// run was GIVEN, and it has to survive a run that never comes back.
		if updated, uerr := h.Queries.StorePrWalkthroughResult(ctx, db.StorePrWalkthroughResultParams{
			ID: row.ID, State: "pending", Groups: row.Groups,
			Truncated: true, OmittedFiles: int32(diff.OmittedFiles), Error: "",
		}); uerr == nil {
			row = updated
		}
	}
	task, err := h.TaskService.EnqueueCrossReviewRun(ctx, issue, agentID, prWalkthroughBrief(diff), actorUserID)
	if err != nil {
		slog.Warn("pr walkthrough: enqueue run failed", "issue_id", uuidToString(issue.ID), "error", err)
		h.failPrWalkthrough(ctx, row, "could not start the walkthrough run: "+err.Error())
		// The queue keeps one pending run per (issue, agent), so a head that
		// moves twice in a row hits that limit rather than a real fault. It is
		// the same "come back in a moment" as our own head fence, and the row
		// is already settled failed so the reviewer can retry.
		if strings.Contains(err.Error(), "already exists") {
			return db.AgentTaskQueue{}, errPrWalkthroughAlreadyRunning
		}
		return db.AgentTaskQueue{}, err
	}
	// Per-leg accounting (JEF-274): a walkthrough describes someone else's
	// work, so it is review-like and must not count as a sample of the
	// worker's task class in the routing statistics.
	if stamped, serr := h.TaskService.StampLeg(ctx, task, service.LegRolePrWalkthrough, db.AgentTaskQueue{}); serr != nil {
		slog.Warn("pr walkthrough: stamp leg failed", "task_id", uuidToString(task.ID), "error", serr)
	} else {
		task = stamped
	}
	if err := h.Queries.SetPrWalkthroughTask(ctx, db.SetPrWalkthroughTaskParams{ID: row.ID, TaskID: task.ID}); err != nil {
		slog.Warn("pr walkthrough: link task failed", "walkthrough_id", uuidToString(row.ID), "error", err)
	}
	h.publishPrWalkthrough(issue, row, "pending")
	return task, nil
}

// fetchWalkthroughDiff reads the diff through the same fetcher cross review
// (K15) uses — GitHub through the App installation token, Forgejo / GitLab
// through the workspace connection — and then bounds it. There is no second
// fetch path: ghdiff only parses and caps what that one returns.
func (h *Handler) fetchWalkthroughDiff(ctx context.Context, issue db.Issue, pr linkedPR) (ghdiff.Diff, error) {
	var fetcher PullRequestDiffFetcher = h.DiffFetcher
	if fetcher == nil {
		fetcher = builtinDiffFetcher{h: h}
	}
	raw, err := fetcher.FetchIssueDiff(ctx, issue, pr.htmlURL)
	if err != nil {
		return ghdiff.Diff{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return ghdiff.Diff{}, errors.New("the pull request diff came back empty")
	}
	return ghdiff.ParseAndCap(raw), nil
}

// prWalkthroughBrief is the run's whole instruction set. The contract it
// states is the same one references/pr-walkthrough.md documents; if you change
// one, change the other.
func prWalkthroughBrief(diff ghdiff.Diff) string {
	var b strings.Builder
	b.WriteString("Write a reviewer's walkthrough of the pull request diff below. Read only — change nothing, run nothing, open nothing.\n\n")
	b.WriteString("Group the change into an ordered narrative. Each group gets a short title, one of the kinds ")
	b.WriteString("`core` (the behaviour change a reviewer must read), `test`, `generated` (lockfiles, generated clients, snapshots) or `noise` (formatting, moves, renames), ")
	b.WriteString("a one-paragraph rationale, and for every hunk an explanation of what that hunk does and why.\n\n")
	if diff.Truncated {
		b.WriteString("This diff was truncated: ")
		b.WriteString(strconv.Itoa(diff.OmittedFiles))
		b.WriteString(" file(s) are not shown. Describe what you were given and do not guess at the rest.\n\n")
	}
	b.WriteString("Answer with exactly one fenced ```pr_walkthrough``` block containing:\n")
	b.WriteString("{\"groups\":[{\"title\":\"…\",\"kind\":\"core\",\"rationale\":\"…\",\"files\":[{\"path\":\"…\",\"hunks\":[{\"old_start\":1,\"new_start\":1,\"lines\":\"…\",\"explanation\":\"…\",\"moved_from\":\"\"}]}]}]}\n\n")
	b.WriteString("Copy `path`, `old_start`, `new_start` and `lines` from the diff verbatim so the explanation stays anchored to it.\n\n")
	b.WriteString("DIFF:\n")
	b.WriteString(ghdiff.Brief(diff))
	return b.String()
}

// --- completion hooks ------------------------------------------------------

// settlePrWalkthroughRun is the CompleteTask hook. The leg role is the cheap
// guard: every other completed run in the system pays one string compare, not
// a database read.
func (h *Handler) settlePrWalkthroughRun(ctx context.Context, task db.AgentTaskQueue, output string) {
	if task.LegRole != service.LegRolePrWalkthrough {
		return
	}
	row, err := h.Queries.GetPrWalkthroughByTask(ctx, task.ID)
	if err != nil {
		return
	}
	report, ok := h.parsePrWalkthroughReport(ctx, task, output)
	if !ok {
		h.failPrWalkthrough(ctx, row, "the run ended without a readable pr_walkthrough block")
		return
	}
	groups := service.NormalizePrWalkthroughGroups(report.Groups)
	raw, err := json.Marshal(groups)
	if err != nil {
		h.failPrWalkthrough(ctx, row, "the walkthrough could not be stored")
		return
	}
	stored, err := h.Queries.StorePrWalkthroughResult(ctx, db.StorePrWalkthroughResultParams{
		ID: row.ID, State: "ready", Groups: raw,
		Truncated: row.Truncated, OmittedFiles: row.OmittedFiles, Error: "",
	})
	if err != nil {
		slog.Warn("pr walkthrough: store failed", "walkthrough_id", uuidToString(row.ID), "error", err)
		return
	}
	h.publishPrWalkthroughRow(ctx, stored)
	h.chasePrWalkthroughHead(ctx, stored)
}

// chasePrWalkthroughHead covers the gap between our head fence and the queue's
// own one-pending-run-per-(issue, agent) rule. A pull request that moves twice
// while the first walkthrough is queued has its second head claimed but no run
// to fill it; without this, that head would sit unexplained until someone
// pushed again or pressed refresh.
//
// Terminating by construction: each hop targets a strictly newer head_sha, and
// a head that already has a pending run is refused at the claim.
func (h *Handler) chasePrWalkthroughHead(ctx context.Context, settled db.PrWalkthrough) {
	head := h.currentHeadSHA(ctx, settled.PrSource, settled.PrID)
	if head == "" || head == settled.HeadSha {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, settled.IssueID)
	if err != nil {
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return
	}
	cfg := service.PrWalkthroughFromSettings(ws.Settings)
	if !cfg.Enabled || cfg.AgentID == "" {
		return
	}
	pr, ok := h.loadLinkedPR(ctx, issue, uuidToString(settled.PrID))
	if !ok || pr.headSHA != head {
		return
	}
	if _, err := h.enqueuePrWalkthrough(ctx, issue, cfg, pr, pgtype.UUID{}); err != nil &&
		!errors.Is(err, errPrWalkthroughAlreadyRunning) {
		slog.Warn("pr walkthrough: chase enqueue failed",
			"pr_id", uuidToString(settled.PrID), "head_sha", head, "error", err)
	}
}

// failPrWalkthroughRun is the FailTask hook: a crashed run must leave its head
// settled, or the PR would sit on `pending` until it moves again.
func (h *Handler) failPrWalkthroughRun(ctx context.Context, task db.AgentTaskQueue, reason string) {
	if task.LegRole != service.LegRolePrWalkthrough {
		return
	}
	row, err := h.Queries.GetPrWalkthroughByTask(ctx, task.ID)
	if err != nil {
		return
	}
	h.failPrWalkthrough(ctx, row, reason)
}

// failPrWalkthrough settles one row as failed, keeping whatever it already had.
func (h *Handler) failPrWalkthrough(ctx context.Context, row db.PrWalkthrough, reason string) {
	groups := row.Groups
	if len(groups) == 0 {
		groups = []byte("[]")
	}
	stored, err := h.Queries.StorePrWalkthroughResult(ctx, db.StorePrWalkthroughResultParams{
		ID: row.ID, State: "failed", Groups: groups,
		Truncated: row.Truncated, OmittedFiles: row.OmittedFiles,
		Error: clipWalkthroughError(reason),
	})
	if err != nil {
		slog.Warn("pr walkthrough: fail failed", "walkthrough_id", uuidToString(row.ID), "error", err)
		return
	}
	h.publishPrWalkthroughRow(ctx, stored)
	h.chasePrWalkthroughHead(ctx, stored)
}

func clipWalkthroughError(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		return util.TruncateUTF8Bytes(reason, 1000)
	}
	return reason
}

// parsePrWalkthroughReport reads the fenced block off the run's answer,
// falling back to its streamed text messages when the final answer does not
// carry it. Same fallback as the drift report (K56) and for the same reason:
// some runtimes end on a tool result rather than the prose that held the block.
func (h *Handler) parsePrWalkthroughReport(ctx context.Context, task db.AgentTaskQueue, output string) (service.PrWalkthroughReport, bool) {
	text := output
	if !prWalkthroughFence.MatchString(text) {
		if msgs, err := h.Queries.ListTaskMessages(ctx, task.ID); err == nil {
			var b strings.Builder
			for _, m := range msgs {
				if m.Type == "text" && m.Content.Valid {
					b.WriteString(m.Content.String + "\n\n")
				}
			}
			if prWalkthroughFence.MatchString(b.String()) {
				text = b.String()
			}
		}
	}
	m := prWalkthroughFence.FindStringSubmatch(text)
	if m == nil {
		return service.PrWalkthroughReport{}, false
	}
	var report service.PrWalkthroughReport
	if err := json.Unmarshal([]byte(m[1]), &report); err != nil {
		return service.PrWalkthroughReport{}, false
	}
	return report, true
}

// --- triggers --------------------------------------------------------------

// maybeEnqueuePrWalkthrough is the shared trigger body: for every issue this
// pull request is linked to, start a walkthrough of its current head. Silent
// when the feature is off, when nothing is linked, or when the head is already
// claimed — all three are ordinary, not failures.
func (h *Handler) maybeEnqueuePrWalkthrough(ctx context.Context, wsID pgtype.UUID, source string, prID pgtype.UUID, headSHA string) {
	if headSHA == "" {
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return
	}
	cfg := service.PrWalkthroughFromSettings(ws.Settings)
	if !cfg.Enabled || cfg.AgentID == "" {
		return
	}
	var issueIDs []pgtype.UUID
	switch source {
	case prWalkthroughSourceGitHub:
		issueIDs, err = h.Queries.ListIssueIDsForPullRequest(ctx, prID)
	case prWalkthroughSourceVCS:
		issueIDs, err = h.Queries.ListIssueIDsForVCSPullRequest(ctx, prID)
	}
	if err != nil {
		return
	}
	for _, issueID := range issueIDs {
		issue, err := h.Queries.GetIssue(ctx, issueID)
		if err != nil {
			continue
		}
		pr, ok := h.loadLinkedPR(ctx, issue, uuidToString(prID))
		if !ok || pr.headSHA != headSHA {
			continue // unlinked or already moved on since this event
		}
		if _, err := h.enqueuePrWalkthrough(ctx, issue, cfg, pr, pgtype.UUID{}); err != nil &&
			!errors.Is(err, errPrWalkthroughAlreadyRunning) {
			slog.Warn("pr walkthrough: enqueue failed",
				"issue_id", uuidToString(issueID), "pr_id", uuidToString(prID), "error", err)
		}
	}
}

// --- realtime --------------------------------------------------------------

func (h *Handler) publishPrWalkthroughRow(ctx context.Context, row db.PrWalkthrough) {
	issue, err := h.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		return
	}
	h.publishPrWalkthrough(issue, row, row.State)
}

func (h *Handler) publishPrWalkthrough(issue db.Issue, row db.PrWalkthrough, state string) {
	h.publish(EventPrWalkthroughUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue_id": uuidToString(issue.ID),
		"pr_id":    uuidToString(row.PrID),
		"state":    state,
	})
}
