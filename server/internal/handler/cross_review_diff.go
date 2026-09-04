package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// builtinDiffFetcher reads the diff of the pull request linked to the issue:
// the one at prURL when given, else the newest link. GitHub goes through the
// App installation token; Forgejo/GitLab through the workspace connection's
// stored token.
type builtinDiffFetcher struct{ h *Handler }

func (f builtinDiffFetcher) FetchIssueDiff(ctx context.Context, issue db.Issue, prURL string) (string, error) {
	prURL = strings.TrimRight(strings.TrimSpace(prURL), "/")
	if gh, err := f.h.Queries.ListPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range gh {
			if prURL != "" && strings.TrimRight(pr.HtmlUrl, "/") != prURL {
				continue
			}
			client := f.h.PRRefresh.Client()
			if client == nil || !client.Enabled() {
				return "", errors.New("github app not configured")
			}
			return client.PullRequestDiff(ctx, pr.InstallationID, pr.RepoOwner, pr.RepoName, int(pr.PrNumber))
		}
	}
	if rows, err := f.h.Queries.ListVCSPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range rows {
			if prURL != "" && strings.TrimRight(pr.HtmlUrl, "/") != prURL {
				continue
			}
			conn, err := f.h.Queries.GetVCSConnectionByID(ctx, pr.ConnectionID)
			if err != nil {
				return "", err
			}
			provider, ok := vcs.For(conn.Provider)
			if !ok {
				return "", errors.New("unknown vcs provider " + conn.Provider)
			}
			token, err := f.h.openVCSSecret(conn.AccessTokenEncrypted)
			if err != nil {
				return "", err
			}
			return provider.PullRequestDiff(ctx, conn.InstanceUrl, token, pr.RepoOwner, pr.RepoName, int(pr.PrNumber))
		}
	}
	return "", errors.New("no pull request linked to the issue")
}
