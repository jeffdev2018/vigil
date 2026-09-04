package handler

import (
	"context"
	"errors"

	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Merge through the platform API (K42 debt): the head shard's pull request
// is updated with its target and merged by GitHub / Forgejo / GitLab; only
// when no platform can do it does the shard's agent get a merge run.

type mergeOutcome int

const (
	mergeOutcomeUnavailable mergeOutcome = iota // no PR, no platform, or an API error: fall back to the agent
	mergeOutcomeMerged
	mergeOutcomeConflict
)

// PullRequestMerger merges the pull request linked to an issue.
type PullRequestMerger interface {
	MergeIssuePullRequest(ctx context.Context, issue db.Issue) (merged, conflict bool, detail string, err error)
}

func (h *Handler) mergeShardViaAPI(ctx context.Context, child db.Issue) (mergeOutcome, string) {
	var merger PullRequestMerger = h.PRMerger
	if merger == nil {
		merger = builtinMerger{h: h}
	}
	merged, conflict, detail, err := merger.MergeIssuePullRequest(ctx, child)
	switch {
	case err != nil:
		return mergeOutcomeUnavailable, err.Error()
	case merged:
		return mergeOutcomeMerged, ""
	case conflict:
		return mergeOutcomeConflict, detail
	}
	return mergeOutcomeUnavailable, detail
}

type builtinMerger struct{ h *Handler }

func (m builtinMerger) MergeIssuePullRequest(ctx context.Context, issue db.Issue) (bool, bool, string, error) {
	if gh, err := m.h.Queries.ListPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range gh {
			if pr.State != "open" {
				continue
			}
			client := m.h.PRRefresh.Client()
			if client == nil || !client.Enabled() {
				return false, false, "", errors.New("github app not configured")
			}
			out, err := client.MergePullRequest(ctx, pr.InstallationID, pr.RepoOwner, pr.RepoName, int(pr.PrNumber))
			return out.Merged, out.Conflict, out.Detail, err
		}
	}
	if rows, err := m.h.Queries.ListVCSPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range rows {
			if pr.State != "open" {
				continue
			}
			conn, err := m.h.Queries.GetVCSConnectionByID(ctx, pr.ConnectionID)
			if err != nil {
				return false, false, "", err
			}
			provider, ok := vcs.For(conn.Provider)
			if !ok {
				return false, false, "", errors.New("unknown vcs provider " + conn.Provider)
			}
			token, err := m.h.openVCSSecret(conn.AccessTokenEncrypted)
			if err != nil {
				return false, false, "", err
			}
			out, err := provider.MergePullRequest(ctx, conn.InstanceUrl, token, pr.RepoOwner, pr.RepoName, int(pr.PrNumber))
			return out.Merged, out.Conflict, out.Detail, err
		}
	}
	return false, false, "", errors.New("no open pull request linked to the issue")
}
