package telegram

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DigestSender (K64) posts a server-composed text (the morning digest) into
// a Telegram chat of an installation, outside any inbound round trip.
type DigestSender struct {
	decrypt Decrypter
	apiBase string
	client  *http.Client
	logger  *slog.Logger
}

func NewDigestSender(decrypt Decrypter, apiBase string, client *http.Client, logger *slog.Logger) *DigestSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DigestSender{decrypt: decrypt, apiBase: apiBase, client: client, logger: logger}
}

func (d *DigestSender) SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return "", err
	}
	res, err := newSender(newBotAPI(d.apiBase, creds.BotToken, d.client), d.logger).Send(ctx, channel.OutboundMessage{ChatID: chatID, Text: text})
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}
