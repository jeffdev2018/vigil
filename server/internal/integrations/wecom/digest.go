package wecom

import (
	"context"
	"errors"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inline approvals deliberately stop at a deep link here, not a button.
// WeCom's aibot_send_msg does accept a template_card (ws_frame.go), and a
// press comes back as an aibot_event_callback with eventtype
// "template_card_event" — the constant already exists. What could not be
// established offline is the exact body of that event: which field carries
// the pressed button's key and which carries the presser's userid.
// aibotEventCallback decodes only the event type, and guessing the rest
// ships a button that swallows every press. So an approval posted to WeCom
// arrives as text carrying a link into the web issue
// (handler.postApprovalToChannels), and this sender stays text-only: it does
// NOT implement RichDigestSender.
//
// Upgrade path when a tenant is available: confirm the template_card_event
// body, add sendTemplateCardCtx beside sendTextCtx in ws_sender.go, widen
// aibotEventCallback, branch on the event in wecom_channel.go, and route the
// press to handler.DecideApprovalFromChannel — the decide core and the button
// payload already accept it.
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
