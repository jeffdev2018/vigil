package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	slackintegration "github.com/multica-ai/multica/server/internal/integrations/slack"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/slack-go/slack"
)

// Digest buttons (K64). The Slack digest carries buttons: "Answer …" on a
// waiting Decision Card (the recommended option, one click, the same path
// as the web button) and links to open an issue, a review or the briefing.
// A click comes back over Socket Mode; the Slack user must be bound to a
// Multica member of the workspace, otherwise the click is refused.

const (
	digestDecideValuePrefix  = "decide|"
	digestMaxDecisionButtons = 5
	AuditDigestAction        = "briefing.digest_action"
)

// briefingDigestActions builds the buttons of a workspace's digest.
func (h *Handler) briefingDigestActions(ctx context.Context, wsID pgtype.UUID, b MorningBriefingResponse, base string) []channel.DigestAction {
	var actions []channel.DigestAction
	if decisions, err := h.Queries.ListPendingDecisionsForWorkspace(ctx, wsID); err == nil {
		n := 0
		for _, d := range decisions {
			if n >= digestMaxDecisionButtons || len(d.Response) > 0 || !d.RecommendedOptionID.Valid || d.RecommendedOptionID.String == "" || d.PlanVersion.Valid || d.InterviewGroupID.Valid {
				continue
			}
			var options []DecisionOption
			_ = json.Unmarshal(d.Options, &options)
			label := ""
			for _, o := range options {
				if o.ID == d.RecommendedOptionID.String {
					label = o.Label
				}
			}
			if label == "" {
				continue
			}
			actions = append(actions, channel.DigestAction{Label: "Answer: " + label, Value: digestDecideValuePrefix + uuidToString(d.IssueID) + "|" + uuidToString(d.ID) + "|" + d.RecommendedOptionID.String})
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
		if act == nil || act.ActionID != slackintegration.DigestActionID || !strings.HasPrefix(act.Value, digestDecideValuePrefix) {
			continue
		}
		a.reply(cb.ResponseURL, a.decide(ctx, appID, cb.User.ID, act.Value))
	}
}

// decide answers a Decision Card from a digest button and returns the
// ephemeral text for the clicker. Every refusal is a sentence, not an error.
func (a *SlackDigestActions) decide(ctx context.Context, appID, slackUserID, value string) string {
	h := a.H
	parts := strings.Split(strings.TrimPrefix(value, digestDecideValuePrefix), "|")
	if len(parts) != 3 {
		return "This button is not valid any more."
	}
	inst, err := h.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{ChannelType: string(slackintegration.TypeSlack), AppID: appID})
	if err != nil {
		return "This Slack app is not connected to a Multica workspace."
	}
	binding, err := h.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{InstallationID: inst.ID, ChannelUserID: slackUserID})
	if err != nil {
		return "Link your Slack account to Multica first (`/issue link`), then click again."
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: binding.MulticaUserID, WorkspaceID: inst.WorkspaceID})
	if err != nil {
		return "You are not a member of this workspace any more."
	}
	issueID, ok1 := decisionUUID(parts[0])
	decisionID, ok2 := decisionUUID(parts[1])
	if !ok1 || !ok2 {
		return "This button is not valid any more."
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: inst.WorkspaceID})
	if err != nil {
		return "That issue no longer exists."
	}
	decision, err := h.Queries.GetIssueDecision(ctx, db.GetIssueDecisionParams{ID: decisionID, IssueID: issue.ID})
	if err != nil {
		return "That decision no longer exists."
	}
	if len(decision.Response) > 0 {
		return "Already answered."
	}
	if decision.PlanVersion.Valid || decision.InterviewGroupID.Valid {
		return "This card needs the web: open the issue to answer it."
	}
	var options []DecisionOption
	_ = json.Unmarshal(decision.Options, &options)
	chosen := ""
	for _, o := range options {
		if o.ID == parts[2] {
			chosen = o.Label
		}
	}
	if chosen == "" {
		return "That option is not on the card any more."
	}
	userID := uuidToString(member.UserID)
	updated, code, err := h.answerDecisionCore(ctx, issue, decision, userID, "member", userID, DecisionAnswer{OptionID: parts[2]}, chosen, "", nil)
	if code == "already_decided" {
		return "Already answered."
	}
	if err != nil {
		slog.Warn("slack digest: answer failed", "error", err, "decision_id", parts[1])
		return "The answer could not be recorded. Open the issue in Multica."
	}
	h.audit(ctx, issue.WorkspaceID, "member", userID, AuditDigestAction, "issue_decision", decision.ID, map[string]any{"issue_id": uuidToString(issue.ID), "option_id": parts[2], "channel": "slack", "slack_user_id": slackUserID}, nil)
	if h.Bus != nil {
		// Best effort: the web refreshes its decision lists on this event.
		h.publish("issue:aux_changed", uuidToString(issue.WorkspaceID), "member", userID, map[string]any{"issue_id": uuidToString(issue.ID)})
	}
	suffix := ""
	if updated.ResumeTaskID.Valid {
		suffix = " The agent resumes with your answer."
	}
	return fmt.Sprintf("Answered «%s» with %q.%s", decision.Question, chosen, suffix)
}
