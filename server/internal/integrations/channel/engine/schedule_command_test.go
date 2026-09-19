package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type fakeSchedules struct {
	mu      sync.Mutex
	calls   int
	id      pgtype.UUID
	err     error
	lastWS  pgtype.UUID
	lastAg  pgtype.UUID
	lastBy  pgtype.UUID
	lastTxt string
}

func (f *fakeSchedules) ProposeChannelAutopilot(_ context.Context, workspaceID, agentID, memberUserID pgtype.UUID, text string) (ChannelAutopilotProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastWS, f.lastAg, f.lastBy, f.lastTxt = workspaceID, agentID, memberUserID, text
	if f.err != nil {
		return ChannelAutopilotProposal{}, f.err
	}
	return ChannelAutopilotProposal{AutopilotID: f.id, Title: "Open tickets", Summary: "0 9 * * 1 (Europe/Paris), first run Mon 14 Sep 09:00"}, nil
}
func (f *fakeSchedules) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fakeSchedules) snapshot() (pgtype.UUID, pgtype.UUID, pgtype.UUID, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWS, f.lastAg, f.lastBy, f.lastTxt
}

// Parsing follows the /issue and /capture rules — case-sensitive,
// token-bounded, first non-empty line only — so the three commands can never
// both fire on one message. Unlike /capture there is no link special case:
// everything after the prefix is the sentence the model reads.
func TestParseScheduleCommand(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
		text string
	}{
		{name: "sentence", body: "/schedule every Monday at 9, list the open tickets", ok: true, text: "every Monday at 9, list the open tickets"},
		{name: "multi-line", body: "/schedule every Monday at 9\nlist the open tickets", ok: true, text: "every Monday at 9\nlist the open tickets"},
		{name: "leading blank lines", body: "\n\n/schedule daily at 8", ok: true, text: "daily at 8"},
		{name: "bare command", body: "/schedule", ok: true},
		{name: "bare command with spaces", body: "/schedule   ", ok: true},
		{name: "not a whole token", body: "/scheduled thing", ok: false},
		{name: "wrong case", body: "/Schedule thing", ok: false},
		{name: "not on the first line", body: "hello\n/schedule thing", ok: false},
		{name: "a capture command", body: "/capture thing", ok: false},
		{name: "an issue command", body: "/issue thing", ok: false},
		{name: "empty body", body: "", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := ParseScheduleCommand(tc.body)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if cmd.Text != tc.text {
				t.Errorf("text = %q, want %q", cmd.Text, tc.text)
			}
			if cmd.IsEmpty() != (tc.text == "") {
				t.Errorf("IsEmpty() = %v, want %v", cmd.IsEmpty(), tc.text == "")
			}
		})
	}
}

func TestScheduleCommandTooLongMatchesTheServerLimit(t *testing.T) {
	if (ScheduleCommand{Text: strings.Repeat("a", scheduleCommandMaxRunes)}).TooLong() {
		t.Error("a sentence at the limit was refused")
	}
	if !(ScheduleCommand{Text: strings.Repeat("a", scheduleCommandMaxRunes+1)}).TooLong() {
		t.Error("a sentence over the limit was accepted; the server would refuse it")
	}
}

func scheduleMessage(t *testing.T, body string) channel.InboundMessage {
	msg := p2pMessage(t)
	msg.Text = body
	msg.CommandText = body
	return msg
}

func waitForOutcome(t *testing.T, h *harness, want Outcome, check func(Result) bool) {
	t.Helper()
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == want && (check == nil || check(r)) {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a %s reply, got %+v", want, h.replier.calls())
	}
}

// A /schedule is terminal like /capture: the member asked for an automation to
// be drafted, not for the agent to answer. No chat run, no typing indicator —
// and the proposal carries the workspace, the installation's agent and the
// mapped member.
func TestRouter_ScheduleCommand_ProposesAndAnswers(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), scheduleMessage(t, "/schedule every Monday at 9, list the open tickets")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.schedules.count() != 1 {
		t.Fatalf("propose calls = %d, want 1", h.schedules.count())
	}
	ws, agent, by, text := h.schedules.snapshot()
	if ws != h.inst.inst.WorkspaceID {
		t.Errorf("workspace = %v, want the installation's", ws)
	}
	if agent != h.inst.inst.AgentID {
		t.Errorf("agent = %v, want the installation's agent", agent)
	}
	if by != h.ident.id.UserID {
		t.Errorf("author = %v, want the mapped member", by)
	}
	if text != "every Monday at 9, list the open tickets" {
		t.Errorf("text = %q, want the sentence verbatim", text)
	}
	if h.tasks.calls() != 0 {
		t.Errorf("a schedule must not enqueue a chat run, calls=%d", h.tasks.calls())
	}
	if h.typing.calls() != 0 {
		t.Errorf("a schedule must not start a processing indicator, calls=%d", h.typing.calls())
	}
	waitForOutcome(t, h, OutcomeScheduled, func(r Result) bool {
		return r.AutopilotID == h.schedules.id && r.ScheduleTitle == "Open tickets" && strings.Contains(r.ScheduleSummary, "0 9 * * 1")
	})
}

// A bare /schedule is usage, not a model call: there is nothing to draft.
func TestRouter_ScheduleCommand_BareCommandIsUsage(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), scheduleMessage(t, "/schedule")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.schedules.count() != 0 {
		t.Fatalf("a bare /schedule reached the proposer %d time(s)", h.schedules.count())
	}
	waitForOutcome(t, h, OutcomeScheduleUsage, nil)
}

// The two refusals are replies, not errors: the platform must not retry a
// message whose second attempt costs another model call and answers the same.
func TestRouter_ScheduleCommand_RefusalsAnswerInsteadOfFailing(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want Outcome
	}{
		{name: "no model", err: ErrAutopilotModelUnavailable, want: OutcomeScheduleUnavailable},
		{name: "not a schedule", err: ErrAutopilotNotUnderstood, want: OutcomeScheduleUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.schedules.err = tc.err
			if err := h.router.Handle(context.Background(), scheduleMessage(t, "/schedule sometime maybe")); err != nil {
				t.Fatalf("a refusal must not surface as an error: %v", err)
			}
			waitForOutcome(t, h, tc.want, nil)
		})
	}
}

// Anything else is a real failure and must surface, so the platform retries.
func TestRouter_ScheduleCommand_UnexpectedErrorSurfaces(t *testing.T) {
	h := newHarness(t)
	h.schedules.err = errors.New("database is down")
	if err := h.router.Handle(context.Background(), scheduleMessage(t, "/schedule every Monday at 9")); err == nil {
		t.Fatal("expected the failure to surface")
	}
}

// A plain chat message must reach the agent exactly as before.
func TestRouter_PlainMessageIsNotASchedule(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.schedules.count() != 0 {
		t.Fatalf("a plain message was scheduled (%d calls)", h.schedules.count())
	}
}

// Without a configured proposer the command is not a command: the message is
// an ordinary chat turn rather than a silently swallowed schedule.
func TestRouter_ScheduleCommandWithoutAProposerFallsThroughToChat(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger(), Lifecycle: h.lifecycle})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Typing: h.typing, Media: h.media, OriginType: "lark_chat",
	})
	if err := h.router.Handle(context.Background(), scheduleMessage(t, "/schedule every Monday at 9")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.schedules.count() != 0 {
		t.Fatalf("proposer called %d time(s) although none was configured", h.schedules.count())
	}
	if h.tasks.calls() == 0 {
		t.Error("the message should have been an ordinary chat turn")
	}
}
