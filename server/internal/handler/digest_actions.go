package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	slackintegration "github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/slack-go/slack"
)

// Digest buttons (K64). The Slack digest carries buttons: one click answers a
// waiting ask — a Decision Card's recommended option, a held transition, a
// goal-loop question — through the same path as the web button, plus links to
// open an issue, a review or the briefing. A click comes back over Socket
// Mode; the Slack user must be bound to a Multica member of the workspace,
// otherwise the click is refused.
//
// Deciding itself lives in channel_approvals.go, shared with every other chat
// platform. This file is only Slack's half of the round trip.

const (
	digestMaxDecisionButtons = 5
	AuditDigestAction        = "briefing.digest_action"
)

// briefingDigestActions builds the buttons of a workspace's digest.
func (h *Handler) briefingDigestActions(ctx context.Context, wsID pgtype.UUID, b MorningBriefingResponse, base string) []channel.DigestAction {
	var actions []channel.DigestAction
	n := 0
	if decisions, err := h.Queries.ListPendingDecisionsForWorkspace(ctx, wsID); err == nil {
		for _, d := range decisions {
			if n >= digestMaxDecisionButtons || len(d.Response) > 0 || !d.RecommendedOptionID.Valid || d.RecommendedOptionID.String == "" || d.PlanVersion.Valid || d.InterviewGroupID.Valid {
				continue
			}
			var options []DecisionOption
			_ = json.Unmarshal(d.Options, &options)
			label, index := "", -1
			for i, o := range options {
				if o.ID == d.RecommendedOptionID.String {
					label, index = o.Label, i
				}
			}
			if label == "" {
				continue
			}
			actions = append(actions, channel.DigestAction{Label: "Answer: " + label, Value: encodeApprovalValue(ApprovalSourceDecision, d.IssueID, d.ID, index)})
			n++
		}
	}
	// Held transitions and goal-loop questions are asks too, and the feed
	// already treats them as such; the digest is where they become clickable
	// for a team that lives in chat.
	if reqs, err := h.Queries.ListPendingIssueTransitionRequestsForWorkspace(ctx, wsID); err == nil {
		prefix := h.getIssuePrefix(ctx, wsID)
		for _, req := range reqs {
			if n >= digestMaxDecisionButtons {
				break
			}
			issue, ierr := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: req.IssueID, WorkspaceID: wsID})
			if ierr != nil {
				continue
			}
			label := prefix + "-" + strconv.Itoa(int(issue.Number)) + " → " + req.ToStatus
			actions = append(actions,
				channel.DigestAction{Label: "Approve " + label, Value: encodeApprovalValue(ApprovalSourceTransition, req.IssueID, req.ID, 0)},
				channel.DigestAction{Label: "Reject " + label, Value: encodeApprovalValue(ApprovalSourceTransition, req.IssueID, req.ID, 1)})
			n++
		}
	}
	if goals, err := h.Queries.ListWaitingIssueGoalsForWorkspace(ctx, wsID); err == nil {
		for _, goal := range goals {
			q := service.GoalQuestionOf(goal.Question)
			if n >= digestMaxDecisionButtons || q == nil || q.Answer != "" || len(q.Options) == 0 {
				continue
			}
			for i, opt := range q.Options {
				if i >= approvalMaxOptionButtons {
					break
				}
				actions = append(actions, channel.DigestAction{Label: opt, Value: encodeApprovalValue(ApprovalSourceGoalQuestion, goal.IssueID, goal.ID, i)})
			}
			n++
		}
	}
	for i, it := range b.AwaitingReview {
		if i >= 3 {
			break
		}
		actions = append(actions, channel.DigestAction{Label: "Review " + it.Identifier, URL: base + "/issues/" + it.IssueID})
	}
	actions = append(actions, channel.DigestAction{Label: "Open the briefing", URL: base + "/inbox?view=briefing"})
	return actions
}

// SlackDigestActions is the slack.InteractionHandler: a digest button click.
type SlackDigestActions struct {
	H *Handler
	// Reply posts the ephemeral outcome to the response URL; tests swap it.
	Reply func(responseURL, text string) error
}

func (a *SlackDigestActions) reply(url, text string) {
	if url == "" {
		return
	}
	fn := a.Reply
	if fn == nil {
		fn = func(url, text string) error {
			return slack.PostWebhook(url, &slack.WebhookMessage{Text: text, ResponseType: "ephemeral", ReplaceOriginal: false})
		}
	}
	if err := fn(url, text); err != nil {
		slog.Warn("slack digest: reply failed", "error", err)
	}
}

func (a *SlackDigestActions) HandleInteraction(ctx context.Context, appID string, cb slack.InteractionCallback) {
	for _, act := range cb.ActionCallback.BlockActions {
		if act == nil || act.ActionID != slackintegration.DigestActionID || !strings.HasPrefix(act.Value, approvalValuePrefix) {
			continue
		}
		a.reply(cb.ResponseURL, a.H.DecideApprovalFromChannel(ctx, string(slackintegration.TypeSlack), appID, cb.User.ID, act.Value))
	}
}
