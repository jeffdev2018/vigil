package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Multichannel digest (K64). The morning briefing (K30) also goes out to
// the chats configured in morning_briefing.channels — one active
// installation of each type — as one text message per channel, narrated by
// the internal LLM when one is configured (token budget capped) and
// structured either way. Each delivery is audited; the day's send record
// lists the channels reached, so a channel gets the digest once per day.

const (
	AuditBriefingChannelSent   = "briefing.channel_sent"
	AuditBriefingChannelFailed = "briefing.channel_failed"
	briefingNarrativeTokens    = 220
	briefingNarrativePrompt    = `You write the two-sentence spoken summary of a team's morning briefing for an engineering lead. Plain, factual, no hype, no bullet points, under 60 words. Mention what got done, what waits for a human, and what is blocked, in that order, skipping empty parts. Reply with one JSON object only: {"narrative":"…"}.`
)

// ChannelDigestSender posts a text into a chat of an installation.
type ChannelDigestSender interface {
	SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error)
}

// narrateBriefing asks the LLM for the short narration; empty without an
// LLM, on error, or when there is nothing to say.
func (h *Handler) narrateBriefing(ctx context.Context, b MorningBriefingResponse) string {
	if h.LLM == nil || !h.LLM.Enabled() || (len(b.Merged)+len(b.AwaitingReview)+len(b.Blocked)) == 0 {
		return ""
	}
	facts, _ := json.Marshal(map[string]any{"date": b.Date, "done": b.Merged, "awaiting_review": b.AwaitingReview, "blocked": b.Blocked})
	raw, err := h.LLM.GenerateJSON(ctx, "", briefingNarrativePrompt, string(facts), 0.2, briefingNarrativeTokens)
	if err != nil {
		slog.Warn("morning briefing: narration failed", "error", err)
		return ""
	}
	var out struct {
		Narrative string `json:"narrative"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return ""
	}
	return strings.TrimSpace(out.Narrative)
}

// formatBriefingDigest renders the message posted to a chat: the narration
// first, then the three sections with links into the app, then where to
// answer the waiting decisions (K63). Plain URLs so every platform links.
func formatBriefingDigest(b MorningBriefingResponse, appURL, slug string) string {
	base := strings.TrimRight(appURL, "/") + "/" + slug
	var sb strings.Builder
	fmt.Fprintf(&sb, "Morning briefing — %s\n", b.Date)
	if b.Narrative != "" {
		sb.WriteString(b.Narrative + "\n")
	}
	section := func(title string, items []BriefingItem, withReason bool) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&sb, "\n%s (%d)\n", title, len(items))
		for _, it := range items {
			fmt.Fprintf(&sb, "• %s %s — %s/issues/%s\n", it.Identifier, it.Title, base, it.IssueID)
			if withReason && it.Reason != "" {
				fmt.Fprintf(&sb, "  ↳ %s\n", it.Reason)
			}
		}
	}
	section("Done in the last 24 hours", b.Merged, false)
	section("Awaiting review", b.AwaitingReview, false)
	section("Blocked, and why", b.Blocked, true)
	if len(b.Merged)+len(b.AwaitingReview)+len(b.Blocked) == 0 {
		sb.WriteString("Nothing done overnight, nothing awaiting review, nothing blocked.\n")
	}
	pending := 0
	for _, it := range b.Blocked {
		pending += it.PendingDecisions
	}
	if pending > 0 {
		fmt.Fprintf(&sb, "\n%d decision(s) wait for you — answer them: %s/inbox?view=decisions\n", pending, base)
	}
	fmt.Fprintf(&sb, "Open the briefing: %s/inbox?view=briefing", base)
	return sb.String()
}

// deliverBriefingToChannels posts the digest to every configured channel
// through its sender and records which ones were reached.
func (h *Handler) deliverBriefingToChannels(ctx context.Context, wsID pgtype.UUID, briefing MorningBriefingResponse, channels []service.BriefingChannel, actorType, actorID string) []string {
	delivered := []string{"inbox"}
	if len(channels) == 0 {
		return delivered
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return delivered
	}
	text := formatBriefingDigest(briefing, h.cfg.AppURL, ws.Slug)
	for _, ch := range channels {
		sender := h.DigestSenders[ch.Type]
		if sender == nil {
			slog.Warn("morning briefing: no digest sender for channel", "type", ch.Type, "workspace_id", uuidToString(wsID))
			continue
		}
		installs, err := h.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{WorkspaceID: wsID, ChannelType: ch.Type})
		var inst *db.ChannelInstallation
		for i := range installs {
			if installs[i].Status == "active" {
				inst = &installs[i]
				break
			}
		}
		if err != nil || inst == nil {
			h.audit(ctx, wsID, actorType, actorID, AuditBriefingChannelFailed, "workspace", wsID, map[string]any{"date": briefing.Date, "type": ch.Type, "chat_id": ch.ChatID, "error": "no active installation"}, nil)
			continue
		}
		msgID, err := sender.SendDigest(ctx, *inst, ch.ChatID, text)
		if err != nil {
			slog.Warn("morning briefing: channel delivery failed", "type", ch.Type, "error", err, "workspace_id", uuidToString(wsID))
			h.audit(ctx, wsID, actorType, actorID, AuditBriefingChannelFailed, "workspace", wsID, map[string]any{"date": briefing.Date, "type": ch.Type, "chat_id": ch.ChatID, "error": err.Error()}, nil)
			continue
		}
		h.audit(ctx, wsID, actorType, actorID, AuditBriefingChannelSent, "workspace", wsID, map[string]any{"date": briefing.Date, "type": ch.Type, "chat_id": ch.ChatID, "message_id": msgID, "narrated": briefing.Narrative != ""}, nil)
		delivered = append(delivered, ch.Type)
	}
	return delivered
}
