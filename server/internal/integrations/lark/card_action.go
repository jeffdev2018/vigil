package lark

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Inline approval buttons on Lark.
//
// A button click on an interactive card arrives on the SAME long-conn socket
// as a message, as a `card.action.trigger` event. It is not a message, so it
// never reaches LarkJSONFrameDecoder's im.message.receive_v1 path; the
// connector peeks for it first and routes it to the CardActionHandler.
//
// The click is answered by patching the card in place (PatchInteractiveCard)
// rather than by a toast in the ACK body: a patch is the same call the
// streaming status cards already make, so it needs no new transport, and the
// outcome stays readable to the whole chat instead of only to the clicker.

// ApprovalActionKey is the field the approval buttons carry their payload in.
// A card action with any other shape is ignored, so other cards can put their
// own values in `action.value` without colliding.
const ApprovalActionKey = "multica_approval"

// CardAction is one decoded card button press.
type CardAction struct {
	// AppID is the Lark app the event was pushed to — the installation
	// routing key, same as for a message.
	AppID string
	// EventID deduplicates a redelivered event.
	EventID string
	// OperatorOpenID is the clicker, in the app's own id space. It is the
	// same identifier channel_user_binding.channel_user_id holds.
	OperatorOpenID string
	// MessageID is the card that was clicked, for the patch that answers.
	MessageID string
	// Value is the approval payload the button carried.
	Value string
}

// larkCardActionEvent is the documented payload of card.action.trigger.
type larkCardActionEvent struct {
	Operator struct {
		OpenID  string `json:"open_id"`
		UnionID string `json:"union_id"`
		UserID  string `json:"user_id"`
	} `json:"operator"`
	Token  string `json:"token"`
	Action struct {
		Value map[string]string `json:"value"`
		Tag   string            `json:"tag"`
	} `json:"action"`
	Context struct {
		OpenMessageID string `json:"open_message_id"`
		OpenChatID    string `json:"open_chat_id"`
	} `json:"context"`
}

// DecodeCardAction reads a long-conn payload as a card button press. ok=false
// means the payload is something else — a message, a heartbeat, a card action
// carrying no Multica payload — and the caller falls through to the message
// decoder.
func DecodeCardAction(payload []byte) (CardAction, bool) {
	if len(payload) == 0 {
		return CardAction{}, false
	}
	var env larkEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return CardAction{}, false
	}
	if env.Header.EventType != "card.action.trigger" || env.Event == nil {
		return CardAction{}, false
	}
	var evt larkCardActionEvent
	if err := json.Unmarshal(env.Event, &evt); err != nil {
		return CardAction{}, false
	}
	value := evt.Action.Value[ApprovalActionKey]
	if value == "" {
		return CardAction{}, false
	}
	return CardAction{
		AppID:          env.Header.AppID,
		EventID:        env.Header.EventID,
		OperatorOpenID: evt.Operator.OpenID,
		MessageID:      evt.Context.OpenMessageID,
		Value:          value,
	}, true
}

// approvalCardJSON renders a body plus its actions as an interactive card.
// A link action becomes a URL button; a callback action carries its payload
// in action.value, which is what comes back on card.action.trigger. Passing
// no actions renders the body alone — the shape a settled ask is patched to.
func approvalCardJSON(text string, actions []channel.DigestAction) (string, error) {
	elements := []any{map[string]any{
		"tag":  "div",
		"text": map[string]any{"tag": "lark_md", "content": text},
	}}
	buttons := make([]any, 0, len(actions))
	for _, a := range actions {
		btn := map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": a.Label},
			"type": "default",
		}
		switch {
		case a.URL != "":
			btn["url"] = a.URL
		case a.Value != "":
			btn["type"] = "primary"
			btn["value"] = map[string]string{ApprovalActionKey: a.Value}
		default:
			continue
		}
		buttons = append(buttons, btn)
	}
	if len(buttons) > 0 {
		elements = append(elements, map[string]any{"tag": "action", "actions": buttons})
	}
	raw, err := json.Marshal(map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"elements": elements,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ApprovalOutcomeCardJSON renders the sentence a settled ask leaves behind,
// with no buttons: what a clicked card is patched to.
func ApprovalOutcomeCardJSON(text string) (string, error) { return approvalCardJSON(text, nil) }
