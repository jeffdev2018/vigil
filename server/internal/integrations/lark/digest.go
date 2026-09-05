package lark

import (
	"context"

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
