package handler

// The chat-bot entry point into autopilot proposals. `/schedule` in a
// connected channel goes through the same code the HTTP endpoint uses —
// drafting, cron validation, audit and realtime event — so a schedule typed
// in Lark is indistinguishable from one proposed in the app. The channel
// engine calls this through its own narrow interface and never learns about
// this package.
//
// There is no issue behind a chat message, so there is no Decision Card
// either: the autopilot is created paused with its schedule disabled and a
// person activates it from the Autopilots page. That is deliberate — a
// sentence typed in a group chat must not be able to start anything.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

var _ engine.AutopilotProposer = (*Handler)(nil)

// ProposeChannelAutopilot drafts the sentence, then files the autopilot as
// the bound member with the installation's agent as its assignee.
func (h *Handler) ProposeChannelAutopilot(ctx context.Context, workspaceID, agentID, memberUserID pgtype.UUID, text string) (engine.ChannelAutopilotProposal, error) {
	if !memberUserID.Valid || !agentID.Valid {
		return engine.ChannelAutopilotProposal{}, errors.New("a schedule needs a bound member and the channel's agent")
	}
	draft, available, err := h.draftAutopilot(ctx, text, "")
	if !available {
		return engine.ChannelAutopilotProposal{}, engine.ErrAutopilotModelUnavailable
	}
	if err != nil {
		// An empty sentence, one over the limit, or a cron the parser
		// rejects: all three are answers to the member, not faults.
		return engine.ChannelAutopilotProposal{}, fmt.Errorf("%w: %s", engine.ErrAutopilotNotUnderstood, err)
	}
	out, err := calendarToolAdapter{h: h}.call(ctx, http.MethodPost, "/api/autopilots/propose", nil, map[string]any{
		"title":                draft.Title,
		"cron_expression":      draft.CronExpression,
		"timezone":             draft.Timezone,
		"description":          draft.Description,
		"execution_mode":       draft.ExecutionMode,
		"issue_title_template": draft.IssueTitleTemplate,
		"assignee_id":          uuidToString(agentID),
	}, h.ProposeAutopilot, workspaceID, "member", uuidToString(memberUserID), nil)
	if err != nil {
		return engine.ChannelAutopilotProposal{}, fmt.Errorf("propose autopilot: %w", err)
	}
	payload, _ := out.(map[string]any)
	ap, _ := payload["autopilot"].(map[string]any)
	id, _ := ap["id"].(string)
	return engine.ChannelAutopilotProposal{
		AutopilotID: parseUUID(id),
		Title:       draft.Title,
		Summary:     channelScheduleSummary(draft.CronExpression, draft.Timezone, draft.NextRuns),
	}, nil
}

// channelScheduleSummary is the schedule in words every platform's reply
// repeats: the cron, its zone, and when it would first fire.
func channelScheduleSummary(cron, tz string, nextRuns []string) string {
	summary := cron + " (" + tz + ")"
	if len(nextRuns) == 0 {
		return summary
	}
	first, err := time.Parse(time.RFC3339, nextRuns[0])
	if err != nil {
		return summary
	}
	return summary + ", first run " + first.In(mustLocation(tz)).Format("Mon 2 Jan 15:04")
}
