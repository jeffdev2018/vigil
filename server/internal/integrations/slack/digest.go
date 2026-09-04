package slack

import (
	"context"
	"log/slog"

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
