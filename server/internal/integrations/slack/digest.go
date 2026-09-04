package slack

import (
	"context"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/slack-go/slack"
)

// DigestSender (K64) posts a server-composed text (the morning digest) into
// a Slack channel of an installation, outside any inbound round trip. It
// reuses the reply sender, so Markdown → mrkdwn and chunking apply.
type DigestSender struct {
	decrypt Decrypter
	logger  *slog.Logger
}

func NewDigestSender(decrypt Decrypter, logger *slog.Logger) *DigestSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DigestSender{decrypt: decrypt, logger: logger}
}

func (d *DigestSender) SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return "", err
	}
	res, err := newSlackSender(creds, slack.New(creds.BotToken), d.logger).Send(ctx, channel.OutboundMessage{ChatID: chatID, Text: text})
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}

// DigestActionID is the action_id of every digest button; the click carries
// the action's Value back.
const DigestActionID = "multica_digest"

// SendRichDigest posts the digest as Block Kit: the text in sections and
// the actions as buttons (links open directly, values come back through
// the Socket Mode interaction).
func (d *DigestSender) SendRichDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string, actions []channel.DigestAction) (string, error) {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return "", err
	}
	blocks := DigestBlocks(text, actions)
	_, ts, err := slack.New(creds.BotToken).PostMessageContext(ctx, chatID, slack.MsgOptionText(text, false), slack.MsgOptionBlocks(blocks...), slack.MsgOptionDisableLinkUnfurl())
	if err != nil {
		return "", err
	}
	return ts, nil
}

// DigestBlocks renders the text as mrkdwn sections (Slack caps a section at
// 3000 characters) followed by one actions block of at most 25 buttons.
func DigestBlocks(text string, actions []channel.DigestAction) []slack.Block {
	var blocks []slack.Block
	for _, chunk := range chunkText(text, 2900) {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, chunk, false, false), nil, nil))
	}
	var buttons []slack.BlockElement
	for i, a := range actions {
		if i >= 25 {
			break
		}
		label := a.Label
		if len(label) > 75 {
			label = label[:72] + "…"
		}
		btn := slack.NewButtonBlockElement(DigestActionID, a.Value, slack.NewTextBlockObject(slack.PlainTextType, label, false, false))
		if a.URL != "" {
			btn.URL = a.URL
		}
		buttons = append(buttons, btn)
	}
	if len(buttons) > 0 {
		blocks = append(blocks, slack.NewActionBlock("multica_digest_actions", buttons...))
	}
	return blocks
}

func chunkText(text string, size int) []string {
	var out []string
	for len(text) > size {
		cut := strings.LastIndex(text[:size], "\n")
		if cut <= 0 {
			cut = size
		}
		out = append(out, text[:cut])
		text = strings.TrimLeft(text[cut:], "\n")
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}
