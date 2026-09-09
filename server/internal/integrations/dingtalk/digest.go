package dingtalk

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inline approvals deliberately stop at a deep link here, not a button.
// DingTalk's robot API (outbound_send.go) renders msgKey "sampleActionCard"
// buttons as URL links only; a button whose press comes BACK to us needs the
// card platform — a card template registered in the DingTalk developer
// console, referenced by cardTemplateId, whose callback arrives on a Stream
// topic this connector does not subscribe to (ws_endpoint.go). None of that
// can be built or verified without a live tenant, and a button that silently
// does nothing is worse than a link that works. So an approval posted to
// DingTalk arrives as text carrying a link into the web issue
// (handler.postApprovalToChannels), and this sender stays text-only: it does
// NOT implement RichDigestSender.
//
// Upgrade path when a tenant is available: register the card template, add
// its callback topic to ws_endpoint.go, branch on it in ws_connector.go, and
// route the press to handler.DecideApprovalFromChannel like every other
// platform — the decide core and the button payload already accept it.
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
