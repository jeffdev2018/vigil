package wecom

import (
	"context"
	"errors"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DigestSender (K64) posts a server-composed text into a WeCom group
// through the installation's live smart-bot socket; an installation whose
// socket is not connected on this node cannot be reached.
type DigestSender struct{ senders *sendersRegistry }

func NewDigestSender(reg *SendersRegistry) *DigestSender { return &DigestSender{senders: reg} }

func (d *DigestSender) SendDigest(ctx context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	if d.senders == nil {
		return "", errors.New("wecom: no senders registry")
	}
	s := d.senders.get(inst.ID)
	if s == nil {
		return "", errors.New("wecom: installation socket not connected on this node")
	}
	if err := s.sendTextCtx(ctx, chatID, chatTypeGroupInt, text); err != nil {
		return "", err
	}
	return "", nil
}
