package lark

import (
	"context"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DigestSender (K64) posts a server-composed text into a Feishu/Lark chat
// of an installation through the tenant API.
type DigestSender struct {
	api   APIClient
	creds CredentialsResolver
}

func NewDigestSender(api APIClient, creds CredentialsResolver) *DigestSender {
	return &DigestSender{api: api, creds: creds}
}

func (d *DigestSender) SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	installation, err := installationFromRow(inst)
	if err != nil {
		return "", err
	}
	creds, err := installationCredentialsFor(installation, d.creds)
	if err != nil {
		return "", err
	}
	return d.api.SendTextMessage(ctx, SendTextParams{InstallationID: creds, ChatID: ChatID(chatID), Text: text})
}

// SendRichDigest posts the digest as an interactive card so its actions are
// real buttons: a link opens directly, a callback comes back on the same
// long-conn socket as a card.action.trigger event.
func (d *DigestSender) SendRichDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string, actions []channel.DigestAction) (string, error) {
	installation, err := installationFromRow(inst)
	if err != nil {
		return "", err
	}
	creds, err := installationCredentialsFor(installation, d.creds)
	if err != nil {
		return "", err
	}
	card, err := approvalCardJSON(text, actions)
	if err != nil {
		return "", err
	}
	return d.api.SendInteractiveCard(ctx, SendCardParams{InstallationID: creds, ChatID: ChatID(chatID), CardJSON: card})
}

// UpdateMessage rewrites a card this sender posted, dropping its buttons.
// chatID is unused: Lark addresses a patch by message id alone.
func (d *DigestSender) UpdateMessage(ctx context.Context, inst db.ChannelInstallation, chatID, messageID, text string) error {
	installation, err := installationFromRow(inst)
	if err != nil {
		return err
	}
	creds, err := installationCredentialsFor(installation, d.creds)
	if err != nil {
		return err
	}
	card, err := approvalCardJSON(text, nil)
	if err != nil {
		return err
	}
	return d.api.PatchInteractiveCard(ctx, PatchCardParams{InstallationID: creds, LarkCardMessageID: messageID, CardJSON: card})
}
