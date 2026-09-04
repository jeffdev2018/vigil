package dingtalk

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DigestSender (K64) posts a server-composed text into a DingTalk group
// through the robot API of an installation.
type DigestSender struct {
	client  *Client
	decrypt Decrypter
}

func NewDigestSender(client *Client, decrypt Decrypter) *DigestSender {
	return &DigestSender{client: client, decrypt: decrypt}
}

func (d *DigestSender) SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	creds, err := decodeCredentials(inst.Config, d.decrypt)
	if err != nil {
		return "", err
	}
	s := &sender{client: d.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	return s.send(ctx, sendTarget{ConversationType: convTypeGroup, ConversationID: chatID}, text)
}
