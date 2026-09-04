package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Merge readiness (F10): a read-only aggregation of the signals that already
// exist per issue — PR snapshots, CI counts, open review threads, markdown
// todos and blocking issues — answering "can this be merged, and what stops
// it". Nothing is stored: the snapshots refresh themselves.

// prStackMaxDepth bounds the blocker walk of the PR stack.
const prStackMaxDepth = 10

// Blocker kinds. A client that meets a kind it does not know must treat it as
// a generic blocker and keep ready = false.
const (
	blockerChecksFailing     = "checks_failing"
	blockerChecksPending     = "checks_pending"
	blockerMergeConflict     = "merge_conflict"
	blockerMergeNotClean     = "merge_not_clean"
	blockerStaleSnapshot     = "stale_snapshot"
	blockerUnresolvedThreads = "unresolved_threads"
	blockerOpenTodos         = "open_todos"
	blockerBlockingIssue     = "blocking_issue"
	blockerNoPR              = "no_pr"
)

type MergeReadinessChecks struct {
	Total   int64 `json:"total"`
	Passed  int64 `json:"passed"`
	Failed  int64 `json:"failed"`
	Pending int64 `json:"pending"`
}

type MergeReadinessPR struct {
	ID            string               `json:"id"`
	Source        string               `json:"source"`
	Number        int32                `json:"number"`
	Title         string               `json:"title"`
	HtmlURL       string               `json:"html_url"`
	State         string               `json:"state"`
	Mergeable     *string              `json:"mergeable"`
	MergeState    *string              `json:"merge_state"`
	Checks        MergeReadinessChecks `json:"checks"`
	StaleSnapshot bool                 `json:"stale_snapshot"`
	Ready         bool                 `json:"ready"`
}

type MergeBlocker struct {
	Kind            string `json:"kind"`
	Label           string `json:"label"`
	Count           int    `json:"count,omitempty"`
	IssueIdentifier string `json:"issue_identifier,omitempty"`
	PRNumber        int32  `json:"pr_number,omitempty"`
}

type MergeReadinessResponse struct {
	PRs               []MergeReadinessPR `json:"prs"`
	Blockers          []MergeBlocker     `json:"blockers"`
	UnresolvedThreads int64              `json:"unresolved_threads"`
	OpenTodos         int                `json:"open_todos"`
	Ready             bool               `json:"ready"`
}

type PRStackNode struct {
	IssueID    string             `json:"issue_id"`
	Identifier string             `json:"identifier"`
	Title      string             `json:"title"`
	Status     string             `json:"status"`
	Depth      int                `json:"depth"`
	PRs        []MergeReadinessPR `json:"prs"`
	Ready      bool               `json:"ready"`
}

type PRStackResponse struct {
	Nodes     []PRStackNode `json:"nodes"`
	Truncated bool          `json:"truncated"`
	Cyclic    bool          `json:"cyclic"`
}

func (h *Handler) GetIssueMergeReadiness(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	out, err := h.mergeReadinessFor(r.Context(), issue)
	if err != nil {
		slog.Warn("merge readiness failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// mergeReadinessFor computes the readiness verdict; the review cockpit (K16)
// shares it with the endpoint above.
func (h *Handler) mergeReadinessFor(ctx context.Context, issue db.Issue) (MergeReadinessResponse, error) {
	prs, err := h.loadReadinessPRs(ctx, issue.ID)
	if err != nil {
		return MergeReadinessResponse{}, fmt.Errorf("failed to load pull requests: %w", err)
	}
	threads, err := h.Queries.CountUnresolvedThreadsByIssue(ctx, issue.ID)
	if err != nil {
		return MergeReadinessResponse{}, fmt.Errorf("failed to count review threads: %w", err)
	}
	bodies, err := h.Queries.ListCommentContentsByIssue(ctx, issue.ID)
	if err != nil {
		return MergeReadinessResponse{}, fmt.Errorf("failed to scan comments: %w", err)
	}
	todos := 0
	for _, body := range bodies {
		todos += countOpenTodos(body)
	}
	blockingIssues, err := h.blockingIssues(ctx, issue)
	if err != nil {
		return MergeReadinessResponse{}, fmt.Errorf("failed to load blocking issues: %w", err)
	}

	blockers := prBlockers(prs)
	if threads > 0 {
		blockers = append(blockers, MergeBlocker{Kind: blockerUnresolvedThreads, Label: "Unresolved review threads", Count: int(threads)})
	}
	if todos > 0 {
		blockers = append(blockers, MergeBlocker{Kind: blockerOpenTodos, Label: "Open todos in comments", Count: todos})
	}
	blockers = append(blockers, blockingIssues...)

	return MergeReadinessResponse{
		PRs:               prs,
		Blockers:          blockers,
		UnresolvedThreads: threads,
		OpenTodos:         todos,
		Ready:             len(blockers) == 0,
	}, nil
}

func (h *Handler) GetIssuePRStack(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)

	// One level past the cap only to know whether the stack was cut.
	rows, err := h.Queries.ListIssueBlockerStack(ctx, db.ListIssueBlockerStackParams{
		IssueID:  issue.ID,
		MaxDepth: prStackMaxDepth + 1,
	})
	if err != nil {
		slog.Warn("pr stack: load blockers failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to load pr stack")
		return
	}

	resp := PRStackResponse{Nodes: []PRStackNode{}}
	node, err := h.prStackNode(ctx, issue, prefix, 0)
	if err != nil {
		slog.Warn("pr stack: load pull requests failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load pull requests")
		return
	}
	resp.Nodes = append(resp.Nodes, node)
	for _, row := range rows {
		if row.Issue.ID == issue.ID {
			resp.Cyclic = true // the issue blocks itself through the chain
			continue
		}
		if int(row.Depth) > prStackMaxDepth {
			resp.Truncated = true
			continue
		}
		node, err := h.prStackNode(ctx, row.Issue, prefix, int(row.Depth))
		if err != nil {
			slog.Warn("pr stack: load pull requests failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load pull requests")
			return
		}
		resp.Nodes = append(resp.Nodes, node)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) prStackNode(ctx context.Context, issue db.Issue, prefix string, depth int) (PRStackNode, error) {
	prs, err := h.loadReadinessPRs(ctx, issue.ID)
	if err != nil {
		return PRStackNode{}, err
	}
	return PRStackNode{
		IssueID:    uuidToString(issue.ID),
		Identifier: fmt.Sprintf("%s-%d", prefix, issue.Number),
		Title:      issue.Title,
		Status:     issue.Status,
		Depth:      depth,
		PRs:        prs,
		Ready:      len(prBlockers(prs)) == 0,
	}, nil
}

// loadReadinessPRs lists an issue's working PRs from both providers, reusing
// the snapshot conversions the PR list already trusts.
func (h *Handler) loadReadinessPRs(ctx context.Context, issueID pgtype.UUID) ([]MergeReadinessPR, error) {
	out := []MergeReadinessPR{}
	ghRows, err := h.Queries.ListPullRequestsByIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list github pull requests: %w", err)
	}
	for _, row := range ghRows {
		out = append(out, summarizePR(issuePullRequestRowToResponse(row, h.PRRefresh.Enabled())))
	}
	vcsRows, err := h.Queries.ListVCSPullRequestsByIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list vcs pull requests: %w", err)
	}
	for _, row := range vcsRows {
		out = append(out, summarizePR(vcsPullRequestRowToResponse(row)))
	}
	return out, nil
}

func summarizePR(p GitHubPullRequestResponse) MergeReadinessPR {
	pr := MergeReadinessPR{
		ID:         p.ID,
		Source:     p.Provider,
		Number:     p.Number,
		Title:      p.Title,
		HtmlURL:    p.HtmlURL,
		State:      p.State,
		Mergeable:  p.Mergeable,
		MergeState: p.MergeStateStatus,
		Checks: MergeReadinessChecks{
			Total:   p.ChecksTotal,
			Passed:  p.ChecksPassed,
			Failed:  p.ChecksFailed,
			Pending: p.ChecksRunning + p.ChecksPending,
		},
		StaleSnapshot: p.SnapshotStale,
	}
	pr.Ready = len(singlePRBlockers(pr)) == 0
	return pr
}

// prBlockers folds every open PR's blockers together; a settled (merged or
// closed) PR contributes nothing, and no open PR at all is itself a blocker.
func prBlockers(prs []MergeReadinessPR) []MergeBlocker {
	var out []MergeBlocker
	open := 0
	for _, pr := range prs {
		if pr.State != "open" && pr.State != "draft" {
			continue
		}
		open++
		out = append(out, singlePRBlockers(pr)...)
	}
	if open == 0 {
		out = append(out, MergeBlocker{Kind: blockerNoPR, Label: "No open pull request"})
	}
	return out
}

// singlePRBlockers is the per-PR matrix. GitHub's "ready" is derived ONLY
// from a clean merge state (migration 222); a missing rollup means "no
// checks", never "passed".
func singlePRBlockers(pr MergeReadinessPR) []MergeBlocker {
	var out []MergeBlocker
	add := func(kind, label string) {
		out = append(out, MergeBlocker{Kind: kind, Label: label, PRNumber: pr.Number})
	}
	mergeState := strings.ToLower(deref(pr.MergeState))
	mergeable := strings.ToLower(deref(pr.Mergeable))

	if mergeable == "conflicting" || mergeState == "dirty" {
		add(blockerMergeConflict, "Merge conflict")
	}
	switch {
	case pr.Checks.Failed > 0:
		add(blockerChecksFailing, fmt.Sprintf("%d failing check(s)", pr.Checks.Failed))
	case pr.Checks.Pending > 0:
		add(blockerChecksPending, fmt.Sprintf("%d check(s) still running", pr.Checks.Pending))
	case pr.Checks.Total == 0:
		add(blockerChecksPending, "No checks reported yet")
	}
	if pr.Source == "github" {
		switch mergeState {
		case "clean", "dirty":
			// clean is the only green; dirty was reported above.
		case "":
			if pr.Checks.Total > 0 && pr.Checks.Failed == 0 && pr.Checks.Pending == 0 {
				add(blockerMergeNotClean, "Merge state unknown")
			}
		default:
			add(blockerMergeNotClean, "Merge state: "+mergeState)
		}
	}
	if pr.StaleSnapshot {
		add(blockerStaleSnapshot, "Snapshot is stale")
	}
	return out
}

// blockingIssues turns each direct, unfinished blocker into a blocker entry
// naming the issue. Finished blockers (done / cancelled, custom statuses
// included) no longer block.
func (h *Handler) blockingIssues(ctx context.Context, issue db.Issue) ([]MergeBlocker, error) {
	rows, err := h.Queries.ListIssueBlockerStack(ctx, db.ListIssueBlockerStackParams{IssueID: issue.ID, MaxDepth: 1})
	if err != nil {
		return nil, fmt.Errorf("list blocking issues: %w", err)
	}
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	fill := h.newStatusCategoryFiller(ctx, issue.WorkspaceID)
	var out []MergeBlocker
	for _, row := range rows {
		resp := issueToResponse(row.Issue, prefix)
		fill(&resp)
		category := resp.StatusCategory
		if category == "" {
			category = resp.Status
		}
		if category == "done" || category == "cancelled" {
			continue
		}
		out = append(out, MergeBlocker{
			Kind:            blockerBlockingIssue,
			Label:           "Blocked by " + resp.Identifier + " — " + row.Issue.Title,
			IssueIdentifier: resp.Identifier,
		})
	}
	return out, nil
}

var todoLine = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+\[( |x|X)\]\s`)

// countOpenTodos counts unchecked markdown task items (`- [ ]`) outside fenced
// code blocks. Checked items (`[x]` / `[X]`) and indented nested items count
// the same way; anything inside ``` fences is ignored.
func countOpenTodos(markdown string) int {
	count := 0
	inFence := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := todoLine.FindStringSubmatch(line + " "); m != nil && m[1] == " " {
			count++
		}
	}
	return count
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
