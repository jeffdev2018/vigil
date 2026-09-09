package lark

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Inline approvals on Lark: an ask renders as an interactive card whose
// callback buttons carry their payload in action.value, and the press comes
// back on the long-conn socket as card.action.trigger.

func TestApprovalCardRendersButtons(t *testing.T) {
	raw, err := approvalCardJSON("*Decision needed* — MUL-3 Ship it\nShould we?", []channel.DigestAction{
		{Label: "Keep it", Value: "decide|d|aaa|bbb|0"},
		{Label: "Drop it", Value: "decide|d|aaa|bbb|1"},
		{Label: "Open the issue", URL: "https://app/acme/issues/i1"},
		{Label: "neither a link nor a callback"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc struct {
		Elements []struct {
			Tag     string                    `json:"tag"`
			Text    *struct{ Content string } `json:"text"`
			Actions []struct {
				Tag   string            `json:"tag"`
				URL   string            `json:"url"`
				Value map[string]string `json:"value"`
			} `json:"actions"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("card is not valid JSON: %v", err)
	}
	if len(doc.Elements) != 2 || doc.Elements[0].Tag != "div" || !strings.Contains(doc.Elements[0].Text.Content, "Should we?") {
		t.Fatalf("card body = %s", raw)
	}
	acts := doc.Elements[1].Actions
	// The fourth action carries neither a link nor a payload, so it renders
	// nothing rather than a button that does nothing.
	if len(acts) != 3 {
		t.Fatalf("buttons = %d: %s", len(acts), raw)
	}
	if acts[0].Value[ApprovalActionKey] != "decide|d|aaa|bbb|0" || acts[0].URL != "" {
		t.Fatalf("callback button = %+v", acts[0])
	}
	if acts[2].URL != "https://app/acme/issues/i1" || len(acts[2].Value) != 0 {
		t.Fatalf("link button = %+v", acts[2])
	}
	// A settled ask is patched to the same card with no buttons at all.
	settled, err := ApprovalOutcomeCardJSON("This ask is settled: approved.")
	if err != nil || strings.Contains(settled, `"action"`) {
		t.Fatalf("settled card = %s (%v)", settled, err)
	}
}

func TestDecodeCardAction(t *testing.T) {
	payload := []byte(`{"schema":"2.0","header":{"event_id":"e1","event_type":"card.action.trigger","app_id":"cli_app"},
		"event":{"operator":{"open_id":"ou_clicker"},"token":"tk","action":{"tag":"button","value":{"multica_approval":"decide|d|aaa|bbb|0"}},
		"context":{"open_message_id":"om_card","open_chat_id":"oc_chat"}}}`)
	act, ok := DecodeCardAction(payload)
	if !ok {
		t.Fatal("a card.action.trigger with our payload must decode")
	}
	if act.AppID != "cli_app" || act.OperatorOpenID != "ou_clicker" || act.MessageID != "om_card" || act.Value != "decide|d|aaa|bbb|0" {
		t.Fatalf("action = %+v", act)
	}
	// A message event, a card action from another feature, and garbage all
	// fall through to the message decoder rather than being answered.
	for name, raw := range map[string]string{
		"message":     `{"header":{"event_type":"im.message.receive_v1"},"event":{}}`,
		"other card":  `{"header":{"event_type":"card.action.trigger"},"event":{"action":{"value":{"other":"x"}}}}`,
		"no event":    `{"header":{"event_type":"card.action.trigger"}}`,
		"garbage":     `not json`,
		"empty frame": ``,
	} {
		if _, ok := DecodeCardAction([]byte(raw)); ok {
			t.Fatalf("%s must not decode as a card action", name)
		}
	}
}

// The connector routes a card press to the CardActionHandler and never lets
// it reach the message decoder — the click is not a message.
func TestWSConnectorRoutesCardActionToHandler(t *testing.T) {
	t.Parallel()
	conn := newFakeWSConn()
	actions := make(chan CardAction, 4)
	decoded := make(chan struct{}, 4)
	decoder := FrameDecoderFunc(func(payload []byte, _ Installation) (InboundMessage, bool, error) {
		decoded <- struct{}{}
		return InboundMessage{}, false, nil
	})
	c, err := NewWSLongConnConnector(WSConnectorConfig{
		Dialer: &fakeWSDialer{conn: conn},
		EndpointFetcher: EndpointFetcherFunc(func(context.Context, InstallationCredentials) (WSEndpoint, error) {
			return WSEndpoint{URL: "wss://test/ignored", ServiceID: 7, PingInterval: time.Hour}, nil
		}),
		FrameDecoder: decoder,
		CredentialsProvider: CredentialsProviderFunc(func(context.Context, Installation) (InstallationCredentials, error) {
			return InstallationCredentials{AppID: "cli_app", AppSecret: "secret"}, nil
		}),
		CardActionHandler: func(_ context.Context, inst Installation, act CardAction) {
			actions <- act
		},
		PingInterval: time.Hour,
		ReadDeadline: time.Second,
		WriteTimeout: time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("connector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, Installation{AppID: "cli_app"}, func(context.Context, InboundMessage) (DispatchResult, error) { return DispatchResult{}, nil })
	}()

	pushDataFrame(conn, []byte(`{"header":{"event_type":"card.action.trigger","app_id":"cli_app","event_id":"e1"},
		"event":{"operator":{"open_id":"ou_clicker"},"action":{"value":{"multica_approval":"decide|d|aaa|bbb|1"}},
		"context":{"open_message_id":"om_card"}}}`), "m1")

	select {
	case act := <-actions:
		if act.Value != "decide|d|aaa|bbb|1" || act.OperatorOpenID != "ou_clicker" || act.MessageID != "om_card" {
			t.Fatalf("routed action = %+v", act)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the card press never reached the handler")
	}
	select {
	case <-decoded:
		t.Fatal("a card press must not reach the message decoder")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// The frame is ACKed before the handler runs, so a slow decide never
	// earns a Lark redelivery of a press already acted on.
	writes := conn.snapshot()
	if len(writes) == 0 {
		t.Fatal("the card frame must be ACKed")
	}
	f, err := UnmarshalFrame(writes[0])
	if err != nil || !strings.Contains(string(f.Payload), `"code":200`) {
		t.Fatalf("ack = %v %s", err, string(f.Payload))
	}
}
