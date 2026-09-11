package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Inline approvals in every chat channel. An ask is filed, and the same
// moment it lands in the inbox it lands in the chats the workspace already
// routes its digest to, with one button per option. The click comes back over
// whatever inbound transport the platform has — Slack's Socket Mode, the
// Telegram getUpdates loop, the Lark long-conn — and every platform decides
// through the one core below, so the sentences a clicker reads and the
// records a decision leaves are the same everywhere.
//
// Platforms with no inbound button event (DingTalk, WeCom) get the ask as
// text with a deep link into the web issue. That is a floor, not a callback:
// see postApprovalToChannels.
//
// Every refusal is a sentence, never an error. The clicker reads it in chat.

const (
	// approvalValuePrefix opens every button payload this package understands.
	approvalValuePrefix = "decide|"

	// approvalSourceCodes keep a button payload inside Telegram's 64-byte
	// callback_data cap.
	approvalCodeDecision     = "d"
	approvalCodeTransition   = "t"
	approvalCodeGoalQuestion = "g"

	// approvalMaxOptionButtons caps how many option buttons one ask renders.
	approvalMaxOptionButtons = 4
)

// approvalClick is one decoded button payload.
//
// Two wire forms are accepted. The current one packs the source, the issue,
// the ask and the option's INDEX in the ask's option list, with the two UUIDs
// as 22-character raw base64url — 50-odd bytes all in, which is what lets
// Telegram carry it in callback_data without a server-side token table to
// keep alive across restarts. The legacy form is the three-part Slack digest
// payload (`decide|<issue uuid>|<decision uuid>|<option id>`) still sitting in
// Slack history; it only ever named a Decision Card.
type approvalClick struct {
	Source   string
	IssueID  pgtype.UUID
	AskID    pgtype.UUID
	Index    int    // -1 in the legacy form
	OptionID string // legacy form only
}

func approvalSourceCode(source string) string {
	switch source {
	case ApprovalSourceTransition:
		return approvalCodeTransition
	case ApprovalSourceGoalQuestion:
		return approvalCodeGoalQuestion
	default:
		return approvalCodeDecision
	}
}

func approvalSourceFromCode(code string) (string, bool) {
	switch code {
	case approvalCodeDecision:
		return ApprovalSourceDecision, true
	case approvalCodeTransition:
		return ApprovalSourceTransition, true
	case approvalCodeGoalQuestion:
		return ApprovalSourceGoalQuestion, true
	}
	return "", false
}

func packUUID(u pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString(u.Bytes[:])
}

func unpackUUID(s string) (pgtype.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != 16 {
		return pgtype.UUID{}, false
	}
	var u pgtype.UUID
	copy(u.Bytes[:], raw)
	u.Valid = true
	return u, true
}

// encodeApprovalValue packs one option button of one ask.
func encodeApprovalValue(source string, issueID, askID pgtype.UUID, index int) string {
	return approvalValuePrefix + approvalSourceCode(source) + "|" + packUUID(issueID) + "|" + packUUID(askID) + "|" + strconv.Itoa(index)
}

// decodeApprovalValue reads either wire form. ok=false means the button is
// not one of ours or no longer parses; the caller answers with a sentence.
func decodeApprovalValue(value string) (approvalClick, bool) {
	if !strings.HasPrefix(value, approvalValuePrefix) {
		return approvalClick{}, false
	}
	parts := strings.Split(strings.TrimPrefix(value, approvalValuePrefix), "|")
	switch len(parts) {
	case 3: // legacy Slack digest payload
		issueID, ok1 := decisionUUID(parts[0])
		askID, ok2 := decisionUUID(parts[1])
		if !ok1 || !ok2 || parts[2] == "" {
			return approvalClick{}, false
		}
		return approvalClick{Source: ApprovalSourceDecision, IssueID: issueID, AskID: askID, Index: -1, OptionID: parts[2]}, true
	case 4:
		source, ok := approvalSourceFromCode(parts[0])
		if !ok {
			return approvalClick{}, false
		}
		issueID, ok1 := unpackUUID(parts[1])
		askID, ok2 := unpackUUID(parts[2])
		index, err := strconv.Atoi(parts[3])
		if !ok1 || !ok2 || err != nil || index < 0 {
			return approvalClick{}, false
		}
		return approvalClick{Source: source, IssueID: issueID, AskID: askID, Index: index}, true
	}
	return approvalClick{}, false
}

// channelDisplayName names a platform the way its own users do, so the
// refusal sentences read as if they came from that app.
func channelDisplayName(channelType string) string {
	switch channelType {
	case "slack":
		return "Slack"
	case "telegram":
		return "Telegram"
	case "feishu":
		return "Feishu"
	case "dingtalk":
		return "DingTalk"
	case "wecom":
		return "WeCom"
	}
	return "chat"
}

// DecideApprovalFromChannel settles the ask a chat button names and returns
// the sentence the clicker should read. It is the one decide path for every
// platform: the caller supplies its channel type, the app id the event
// arrived on, and the platform's own user id.
func (h *Handler) DecideApprovalFromChannel(ctx context.Context, channelType, appID, platformUserID, value string) string {
	click, ok := decodeApprovalValue(value)
	if !ok {
		return "This button is not valid any more."
	}
	name := channelDisplayName(channelType)
	inst, err := h.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{ChannelType: channelType, AppID: appID})
	if err != nil {
		return "This " + name + " app is not connected to a Multica workspace."
	}
	binding, err := h.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{InstallationID: inst.ID, ChannelUserID: platformUserID})
	if err != nil {
		return "Link your " + name + " account to Multica first (`/issue link`), then click again."
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: binding.MulticaUserID, WorkspaceID: inst.WorkspaceID})
	if err != nil {
		return "You are not a member of this workspace any more."
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: click.IssueID, WorkspaceID: inst.WorkspaceID})
	if err != nil {
		return "That issue no longer exists."
	}
	userID := uuidToString(member.UserID)
	var reply string
	switch click.Source {
	case ApprovalSourceTransition:
		reply = h.decideTransitionFromChannel(ctx, issue, click, userID, member.Role)
	case ApprovalSourceGoalQuestion:
		reply = h.answerGoalFromChannel(ctx, issue, click, userID)
	default:
		reply = h.decideDecisionFromChannel(ctx, issue, click, userID)
	}
	h.audit(ctx, issue.WorkspaceID, "member", userID, AuditDigestAction, "issue", issue.ID, map[string]any{
		"issue_id": uuidToString(issue.ID), "source": click.Source, "ask_id": uuidToString(click.AskID),
		"channel": channelType, "channel_user_id": platformUserID,
	}, nil)
	return reply
}

// decideDecisionFromChannel answers a Decision Card through the same core the
// web button uses.
func (h *Handler) decideDecisionFromChannel(ctx context.Context, issue db.Issue, click approvalClick, userID string) string {
	decision, err := h.Queries.GetIssueDecision(ctx, db.GetIssueDecisionParams{ID: click.AskID, IssueID: issue.ID})
	if err != nil {
		return "That decision no longer exists."
	}
	if len(decision.Response) > 0 {
		return "Already answered."
	}
	if decision.PlanVersion.Valid || decision.InterviewGroupID.Valid {
		return "This card needs the web: open the issue to answer it."
	}
	var options []DecisionOption
	_ = json.Unmarshal(decision.Options, &options)
	optionID, chosen := "", ""
	switch {
	case click.Index >= 0 && click.Index < len(options):
		optionID, chosen = options[click.Index].ID, options[click.Index].Label
	case click.OptionID != "":
		for _, o := range options {
			if o.ID == click.OptionID {
				optionID, chosen = o.ID, o.Label
			}
		}
	}
	if chosen == "" {
		return "That option is not on the card any more."
	}
	updated, code, err := h.answerDecisionCore(ctx, issue, decision, userID, "member", userID, DecisionAnswer{OptionID: optionID}, chosen, "", nil)
	if code == "already_decided" {
		return "Already answered."
	}
	if err != nil {
		slog.Warn("channel approval: answer failed", "error", err, "decision_id", uuidToString(decision.ID))
		return "The answer could not be recorded. Open the issue in Multica."
	}
	h.publishIssueAuxChangedCtx(ctx, issue, "member", userID)
	suffix := ""
	if updated.ResumeTaskID.Valid {
		suffix = " The agent resumes with your answer."
	}
	return fmt.Sprintf("Answered «%s» with %q.%s", decision.Question, chosen, suffix)
}

// decideTransitionFromChannel approves or rejects a held status move. Option
// 0 is approve, option 1 is reject — the order fromTransition renders.
func (h *Handler) decideTransitionFromChannel(ctx context.Context, issue db.Issue, click approvalClick, userID, role string) string {
	state := "approved"
	if click.Index == 1 {
		state = "rejected"
	} else if click.Index != 0 {
		return "That option is not on this request any more."
	}
	req, err := h.Queries.GetIssueTransitionRequest(ctx, db.GetIssueTransitionRequestParams{ID: click.AskID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return "That transition request no longer exists."
	}
	if req.State != "pending" {
		return "Already decided."
	}
	rule := h.transitionRuleFor(ctx, req)
	actor := issuestatus.TransitionActor{Type: issuestatus.ActorMember, ID: userID, Role: role}
	if !issuestatus.CanApprove(rule, actor) {
		return "You are not an approver for this transition."
	}
	decided, applied, err := h.decideTransitionCore(ctx, issue, req, rule, actor, state, nil,
		pgtype.UUID{}, h.issueTriggerWriteProbeCtx(ctx, actor.Type, actor.ID, issue))
	if err != nil {
		var applyErr transitionApplyError
		var refusal transitionGateRefusal
		switch {
		case errors.As(err, &refusal):
			return "Not decided yet: " + refusal.msg + ". Open the issue in Multica."
		case errors.Is(err, pgx.ErrNoRows):
			return "Already decided."
		case errors.As(err, &applyErr):
			slog.Warn("channel approval: transition applied badly", "error", err, "request_id", uuidToString(req.ID))
			return "Your decision was recorded, but the status could not be applied. Open the issue in Multica."
		}
		slog.Warn("channel approval: transition decide failed", "error", err, "request_id", uuidToString(req.ID))
		return "The decision could not be recorded. Open the issue in Multica."
	}
	if state == "rejected" {
		return fmt.Sprintf("Rejected the move of %q to %s.", issue.Title, decided.ToStatus)
	}
	return fmt.Sprintf("Approved: %q is now %s.", issue.Title, applied.Status)
}

// answerGoalFromChannel answers a goal-loop question with the option's own
// words, the same string the web sends.
func (h *Handler) answerGoalFromChannel(ctx context.Context, issue db.Issue, click approvalClick, userID string) string {
	if h.GoalLoop == nil {
		return "The goal loop is not available on this server."
	}
	goal, err := h.Queries.GetIssueGoal(ctx, db.GetIssueGoalParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil || uuidToString(goal.ID) != uuidToString(click.AskID) {
		return "That question no longer exists."
	}
	q := service.GoalQuestionOf(goal.Question)
	if q == nil || q.Answer != "" || goal.Status != "waiting_user" {
		return "Already answered."
	}
	if click.Index < 0 || click.Index >= len(q.Options) {
		return "That option is not on the question any more."
	}
	answer := q.Options[click.Index]
	name := ""
	if user, uerr := h.Queries.GetUser(ctx, parseUUID(userID)); uerr == nil {
		name = user.Name
	}
	if _, err := h.GoalLoop.Answer(ctx, issue, answer, parseUUID(userID), name); err != nil {
		slog.Warn("channel approval: goal answer failed", "error", err, "goal_id", uuidToString(goal.ID))
		return "The answer could not be recorded. Open the issue in Multica."
	}
	h.audit(ctx, issue.WorkspaceID, "member", userID, AuditGoalAnswered, "issue", issue.ID, nil, nil)
	h.publishApproval(ctx, protocol.EventApprovalDecided, "member", userID, issue.WorkspaceID, issue.ID, ApprovalSourceGoalQuestion, uuidToString(goal.ID), ApprovalKindGoalAsk, answer)
	h.publishIssueAuxChangedCtx(ctx, issue, "member", userID)
	return fmt.Sprintf("Answered «%s» with %q.", q.Prompt, answer)
}

// ---- posting an ask into the chats ----

// ChannelMessageUpdater rewrites a message its sender already posted. A
// platform that cannot edit does not implement it and gets a short follow-up
// message instead.
type ChannelMessageUpdater interface {
	UpdateMessage(ctx context.Context, inst db.ChannelInstallation, chatID, messageID, text string) error
}

// approvalAsk is one pending ask, rendered for a chat.
type approvalAsk struct {
	Source     string
	IssueID    pgtype.UUID
	AskID      pgtype.UUID
	Identifier string
	Title      string
	Question   string
	Options    []DecisionOption
}

func (a approvalAsk) header() string {
	switch a.Source {
	case ApprovalSourceTransition:
		return "Approval needed"
	case ApprovalSourceGoalQuestion:
		return "A run is waiting on an answer"
	}
	return "Decision needed"
}

func (a approvalAsk) text() string {
	head := a.Identifier
	if head != "" {
		head += " "
	}
	return "*" + a.header() + "* — " + head + a.Title + "\n" + a.Question
}

// postApprovalToChannels posts a just-filed ask into every chat the workspace
// routes its digest to, and remembers where, so the message can be rewritten
// when the ask settles. Best effort throughout: the ask exists either way.
//
// A platform whose sender renders buttons (Slack, Telegram, Feishu) gets one
// callback button per option. A platform whose sender cannot (DingTalk,
// WeCom) gets the ask as text with a deep link into the web issue — that is a
// deliberate floor, taken because their inbound card-callback shape could not
// be verified without a live tenant, not a callback in disguise.
func (h *Handler) postApprovalToChannels(ctx context.Context, wsID pgtype.UUID, ask approvalAsk) {
	if len(h.DigestSenders) == 0 || !ask.AskID.Valid {
		return
	}
	senders := h.chatSenders()
	h.goChat(ctx, "post", func(ctx context.Context) { h.postApprovalNow(ctx, senders, wsID, ask) })
}

func (h *Handler) postApprovalNow(ctx context.Context, senders map[string]ChannelDigestSender, wsID pgtype.UUID, ask approvalAsk) {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return
	}
	channels := service.BriefingChannels(ws.Settings)
	if len(channels) == 0 {
		return
	}
	issueURL := strings.TrimRight(h.cfg.AppURL, "/") + "/" + ws.Slug + "/issues/" + uuidToString(ask.IssueID)
	actions := make([]channel.DigestAction, 0, approvalMaxOptionButtons+1)
	for i, o := range ask.Options {
		if i >= approvalMaxOptionButtons {
			break
		}
		actions = append(actions, channel.DigestAction{Label: o.Label, Value: encodeApprovalValue(ask.Source, ask.IssueID, ask.AskID, i)})
	}
	actions = append(actions, channel.DigestAction{Label: "Open the issue", URL: issueURL})

	for _, ch := range channels {
		sender := senders[ch.Type]
		if sender == nil {
			continue
		}
		inst, ok := h.activeChannelInstallation(ctx, wsID, ch.Type)
		if !ok {
			continue
		}
		var msgID string
		if rich, isRich := sender.(RichDigestSender); isRich {
			msgID, err = rich.SendRichDigest(ctx, inst, ch.ChatID, ask.text(), actions)
		} else {
			msgID, err = sender.SendDigest(ctx, inst, ch.ChatID, ask.text()+"\n\nDecide it here: "+issueURL)
		}
		if err != nil {
			slog.Warn("channel approval: post failed", "type", ch.Type, "error", err, "workspace_id", uuidToString(wsID))
			continue
		}
		if err := h.Queries.CreateChannelApprovalMessage(ctx, db.CreateChannelApprovalMessageParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, InstallationID: inst.ID, ChannelType: ch.Type,
			ChatID: ch.ChatID, MessageID: msgID, Source: ask.Source, AskID: ask.AskID,
		}); err != nil {
			slog.Warn("channel approval: post not recorded", "type", ch.Type, "error", err)
		}
	}
}

// settleApprovalMessages rewrites every chat message that still carries a now
// settled ask, so nobody clicks a button that decides nothing. Platforms that
// cannot edit get a one-line follow-up in the same chat.
func (h *Handler) settleApprovalMessages(ctx context.Context, wsID, issueID pgtype.UUID, source, askID, outcome string) {
	askUUID, ok := decisionUUID(askID)
	if !ok || len(h.DigestSenders) == 0 {
		return
	}
	senders := h.chatSenders()
	h.goChat(ctx, "settle", func(ctx context.Context) { h.settleApprovalNow(ctx, senders, wsID, issueID, source, askUUID, outcome) })
}

func (h *Handler) settleApprovalNow(ctx context.Context, senders map[string]ChannelDigestSender, wsID, issueID pgtype.UUID, source string, askUUID pgtype.UUID, outcome string) {
	rows, err := h.Queries.ListPendingChannelApprovalMessages(ctx, db.ListPendingChannelApprovalMessagesParams{WorkspaceID: wsID, Source: source, AskID: askUUID})
	if err != nil || len(rows) == 0 {
		return
	}
	text := "This ask is settled: " + h.approvalOutcomeWords(ctx, source, issueID, askUUID, outcome) + "."
	for _, row := range rows {
		sender := senders[row.ChannelType]
		if sender == nil {
			continue
		}
		inst, ierr := h.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: row.InstallationID, ChannelType: row.ChannelType})
		if ierr != nil {
			continue
		}
		updater, canEdit := sender.(ChannelMessageUpdater)
		switch {
		case canEdit && row.MessageID != "":
			err = updater.UpdateMessage(ctx, inst, row.ChatID, row.MessageID, text)
		default:
			_, err = sender.SendDigest(ctx, inst, row.ChatID, text)
		}
		if err != nil {
			slog.Warn("channel approval: settle update failed", "type", row.ChannelType, "error", err)
			continue
		}
		if err := h.Queries.MarkChannelApprovalMessageSettled(ctx, row.ID); err != nil {
			slog.Warn("channel approval: settle not recorded", "error", err)
		}
	}
}

// activeChannelInstallation picks the workspace's live installation of a type.
func (h *Handler) activeChannelInstallation(ctx context.Context, wsID pgtype.UUID, channelType string) (db.ChannelInstallation, bool) {
	installs, err := h.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{WorkspaceID: wsID, ChannelType: channelType})
	if err != nil {
		return db.ChannelInstallation{}, false
	}
	for i := range installs {
		if installs[i].Status == "active" {
			return installs[i], true
		}
	}
	return db.ChannelInstallation{}, false
}

// chatSenders copies the configured senders on the CALLER's goroutine. The
// delivery goroutine outlives the request and must not read a field the
// process (or a test) may reassign underneath it.
func (h *Handler) chatSenders() map[string]ChannelDigestSender {
	out := make(map[string]ChannelDigestSender, len(h.DigestSenders))
	for k, v := range h.DigestSenders {
		out[k] = v
	}
	return out
}

// approvalChatTimeout bounds one best-effort chat delivery. Generous: it is
// off the request path, and cutting a slow-but-working chat API short would
// leave a live-looking button behind for nothing.
const approvalChatTimeout = 30 * time.Second

// goChat runs a chat delivery off the caller's goroutine on a context that
// outlives the request. Nothing a person is waiting on — answering a card,
// approving a move, the gate sweeper's tick — may block on a chat API, and
// none of them has anything to do if the post fails: the ask is in the inbox
// and the feed either way.
//
// The recover is not decoration: these used to run inside the HTTP handler,
// where chi's middleware caught a panic. On their own goroutine a panic takes
// the process down.
func (h *Handler) goChat(ctx context.Context, what string, fn func(context.Context)) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), approvalChatTimeout)
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("channel approval: "+what+" panicked", "panic", r)
			}
		}()
		fn(ctx)
	}()
}

// approvalOutcomeWords says what settled the ask in the words the reader saw
// on the buttons: a Decision Card's option label rather than its id, a
// transition's verdict, a goal question's answer. An outcome that names no
// option (a gate that expired, a decision answered with free text) falls back
// to its own sentence.
func (h *Handler) approvalOutcomeWords(ctx context.Context, source string, issueID, askID pgtype.UUID, outcome string) string {
	if source == ApprovalSourceDecision && outcome != "" {
		if d, err := h.Queries.GetIssueDecision(ctx, db.GetIssueDecisionParams{ID: askID, IssueID: issueID}); err == nil {
			var options []DecisionOption
			_ = json.Unmarshal(d.Options, &options)
			for _, o := range options {
				if o.ID == outcome {
					return o.Label
				}
			}
		}
	}
	return approvalOutcomeSentence(outcome)
}

// PostApprovalToChannels posts a just-filed ask into the workspace's chat
// channels, resolved through the same feed builder GET /api/approvals uses so
// a chat reader and the web reader are told the same thing about the same ask.
//
// It exists for the two sources filed inside internal/service, which has no
// handler to call: a held status transition and a goal-loop question. Both
// announce themselves on the bus, and cmd/server subscribes on their behalf.
//
// Decisions are NOT posted from here even though they publish the same event.
// notifyDecisionRequested publishes approval:asked BEFORE the learned-rule
// auto-decide runs (K71), so a bus listener would post cards that settle
// themselves a millisecond later; it posts its own, after that check.
func (h *Handler) PostApprovalToChannels(ctx context.Context, source, id, issueID string) {
	if len(h.DigestSenders) == 0 {
		return
	}
	issueUUID, ok1 := decisionUUID(issueID)
	askUUID, ok2 := decisionUUID(id)
	if !ok1 || !ok2 {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, issueUUID)
	if err != nil {
		return
	}
	b := h.newApprovalFeedBuilder(ctx, issue.WorkspaceID, "")
	var item ApprovalItem
	var ok bool
	switch source {
	case ApprovalSourceTransition:
		req, rerr := h.Queries.GetIssueTransitionRequest(ctx, db.GetIssueTransitionRequestParams{ID: askUUID, WorkspaceID: issue.WorkspaceID})
		if rerr != nil || req.State != "pending" {
			return
		}
		// The zero actor only makes CanDecide false, which the chat post does
		// not render: who may settle it is checked when the button is clicked.
		item, ok = b.fromTransition(req, issuestatus.TransitionActor{})
	case ApprovalSourceGoalQuestion:
		goal, gerr := h.Queries.GetIssueGoal(ctx, db.GetIssueGoalParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
		if gerr != nil || uuidToString(goal.ID) != id || goal.Status != "waiting_user" {
			return
		}
		item, ok = b.fromGoal(goal)
	default:
		return
	}
	if !ok {
		return
	}
	h.postApprovalToChannels(ctx, issue.WorkspaceID, approvalAsk{
		Source: source, IssueID: issueUUID, AskID: askUUID,
		Identifier: item.Issue.Identifier, Title: item.Issue.Title,
		Question: item.Question, Options: item.Options,
	})
}
