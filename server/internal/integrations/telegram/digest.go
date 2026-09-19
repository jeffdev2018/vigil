package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

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

// telegramCallbackDataCap is Telegram's hard limit on a button's
// callback_data. A value over it is rejected by the API, so a button that
// cannot fit degrades to its URL if it has one and is dropped otherwise —
// never sent as a button that Telegram will refuse.
const telegramCallbackDataCap = 64

// InlineKeyboard renders cross-platform digest actions as a Telegram inline
// keyboard, one button per row. Returns nil when nothing survives, which is
// the shape sendMessage wants for "no keyboard".
func InlineKeyboard(actions []channel.DigestAction) *InlineKeyboardMarkup {
	rows := make([][]InlineKeyboardButton, 0, len(actions))
	for _, a := range actions {
		switch {
		case a.URL != "":
			rows = append(rows, []InlineKeyboardButton{{Text: a.Label, URL: a.URL}})
		case a.Value != "" && len(a.Value) <= telegramCallbackDataCap:
			rows = append(rows, []InlineKeyboardButton{{Text: a.Label, CallbackData: a.Value}})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// SendRichDigest posts the digest with its actions as an inline keyboard.
// Sent as one plain-text message rather than through the chunking sender: a
// keyboard belongs to exactly one message, and a digest split across three
// would leave two of them buttonless.
func (d *DigestSender) SendRichDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string, actions []channel.DigestAction) (string, error) {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return "", err
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("telegram: bad chat id %q: %w", chatID, err)
	}
	msg, err := newBotAPI(d.apiBase, creds.BotToken, d.client).SendMessage(ctx, sendMessageParams{
		ChatID: id, Text: firstChunk(text), ReplyMarkup: InlineKeyboard(actions),
	})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(msg.MessageID, 10), nil
}

// UpdateMessage rewrites a message this sender posted and, by omitting
// reply_markup, retires its buttons.
func (d *DigestSender) UpdateMessage(ctx context.Context, inst db.ChannelInstallation, chatID, messageID, text string) error {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: bad chat id %q: %w", chatID, err)
	}
	msgID, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: bad message id %q: %w", messageID, err)
	}
	return newBotAPI(d.apiBase, creds.BotToken, d.client).EditMessageText(ctx, editMessageTextParams{
		ChatID: id, MessageID: msgID, Text: firstChunk(text),
	})
}

// firstChunk keeps a one-message body inside Telegram's cap. An approval ask
// that long is already unreadable in a chat bubble; the "Open the issue"
// button carries the rest.
func firstChunk(text string) string {
	if utf16Units(text) <= maxMessageUnits {
		return text
	}
	return chunkMessage(text, maxMessageUnits)[0]
}
