package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

// Natural-language event routing for webhook triggers. One LLM call decides
// whether a delivery is even plausibly what the trigger's owner meant; the
// autopilot's own run does the real work afterwards. Liberal by design:
// a false positive costs one run, a false negative loses an event.
//
// Prompt derived from Rowboat's events/routing.ts (rowboatlabs/rowboat,
// Apache-2.0), reduced to one candidate.

const (
	webhookRoutingTimeout    = 12 * time.Second
	webhookRoutingPayloadCap = 6_000
)

const webhookRoutingSystemPrompt = `You are a routing classifier for a team's automation workspace.
You receive one incoming event (a webhook delivery: its name, provider and payload) and one automation described by its owner:
- criteria: an explicit description, in the owner's words, of which incoming events should wake this automation.
Decide whether this event MIGHT be relevant to the criteria.
Rules:
- Be LIBERAL. Prefer false positives over false negatives: it is much better to run once too often than to miss an event that mattered.
- Only answer false when the event is CLEARLY and OBVIOUSLY outside the criteria.
- Do not judge whether the event has enough information to act on; the automation does that itself.
- Answer ONLY a JSON object: {"relevant": boolean, "reason": string}. The reason is one short sentence in the same language as the criteria.`

type webhookRoutingVerdict struct {
	relevant bool
	reason   string
}

// webhookEventMatchesCriteria asks the LLM; every failure mode yields
// relevant=true so routing can only ever add a run, never drop one silently.
func (h *Handler) webhookEventMatchesCriteria(ctx context.Context, criteria, provider string, envelope WebhookEnvelope) webhookRoutingVerdict {
	if h.LLM == nil || !h.LLM.Enabled() {
		return webhookRoutingVerdict{relevant: true, reason: "no classifier configured"}
	}
	payload := string(envelope.EventPayload)
	if len(payload) > webhookRoutingPayloadCap {
		payload = payload[:webhookRoutingPayloadCap] + "…"
	}
	var user strings.Builder
	user.WriteString("## Event\nName: ")
	user.WriteString(envelope.Event)
	user.WriteString("\nProvider: ")
	user.WriteString(provider)
	user.WriteString("\nPayload:\n")
	user.WriteString(payload)
	user.WriteString("\n\n## Automation\ncriteria: ")
	user.WriteString(criteria)

	ctx, cancel := context.WithTimeout(ctx, webhookRoutingTimeout)
	defer cancel()
	raw, err := h.LLM.GenerateJSON(ctx, "", webhookRoutingSystemPrompt, user.String(), 0, 200)
	if err != nil {
		slog.Warn("webhook routing classifier failed; letting the event through", "error", err)
		return webhookRoutingVerdict{relevant: true, reason: "classifier unavailable"}
	}
	var parsed struct {
		Relevant *bool  `json:"relevant"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil || parsed.Relevant == nil {
		slog.Warn("webhook routing classifier answered unexpectedly; letting the event through", "answer", raw)
		return webhookRoutingVerdict{relevant: true, reason: "classifier answer unreadable"}
	}
	return webhookRoutingVerdict{relevant: *parsed.Relevant, reason: strings.TrimSpace(parsed.Reason)}
}
