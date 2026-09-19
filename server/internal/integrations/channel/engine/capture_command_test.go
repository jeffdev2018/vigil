package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type fakeCaptures struct {
	mu      sync.Mutex
	calls   int
	id      pgtype.UUID
	err     error
	lastWS  pgtype.UUID
	lastBy  pgtype.UUID
	lastTx  string
	lastURL string
}

func (f *fakeCaptures) CreateChannelCapture(_ context.Context, workspaceID, creatorUserID pgtype.UUID, content, rawURL string) (pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastWS, f.lastBy, f.lastTx, f.lastURL = workspaceID, creatorUserID, content, rawURL
	return f.id, f.err
}
func (f *fakeCaptures) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fakeCaptures) snapshot() (pgtype.UUID, pgtype.UUID, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastWS, f.lastBy, f.lastTx, f.lastURL
}

// Parsing follows the /issue rules — case-sensitive, token-bounded, first
// non-empty line only — so /capture and /issue can never both fire on one
// message. The one thing it adds: a message whose whole body is a link is
// captured AS a link, because the capture inbox renders and organizes those
// differently from prose.
func TestParseCaptureCommand(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		ok      bool
		content string
		url     string
	}{
		{name: "text", body: "/capture pgbouncer listens on 6432", ok: true, content: "pgbouncer listens on 6432"},
		{name: "lone link", body: "/capture https://example.com/a", ok: true, url: "https://example.com/a"},
		{name: "link in a sentence stays text", body: "/capture read https://example.com/a", ok: true, content: "read https://example.com/a"},
		{name: "multi-line", body: "/capture the decision\nand why", ok: true, content: "the decision\nand why"},
		{name: "leading blank lines", body: "\n\n/capture something", ok: true, content: "something"},
		{name: "bare command", body: "/capture", ok: true},
		{name: "bare command with spaces", body: "/capture   ", ok: true},
		{name: "not a whole token", body: "/captured thing", ok: false},
		{name: "wrong case", body: "/Capture thing", ok: false},
		{name: "not on the first line", body: "hello\n/capture thing", ok: false},
		{name: "an issue command", body: "/issue thing", ok: false},
		{name: "empty body", body: "", ok: false},
		// A non-http scheme is not a link we can capture; it stays the text
		// the member typed, which is at least honest about what they sent.
		{name: "non-http link stays text", body: "/capture file:///etc/passwd", ok: true, content: "file:///etc/passwd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := ParseCaptureCommand(tc.body)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if cmd.Content != tc.content {
				t.Errorf("content = %q, want %q", cmd.Content, tc.content)
			}
			if cmd.URL != tc.url {
				t.Errorf("url = %q, want %q", cmd.URL, tc.url)
			}
			if wantEmpty := tc.content == "" && tc.url == ""; cmd.IsEmpty() != wantEmpty {
				t.Errorf("IsEmpty() = %v, want %v", cmd.IsEmpty(), wantEmpty)
			}
		})
	}
}

func TestCaptureCommandTooLongMatchesTheServerLimit(t *testing.T) {
	if (CaptureCommand{Content: strings.Repeat("a", captureCommandMaxRunes)}).TooLong() {
		t.Error("a capture at the limit was refused")
	}
	if !(CaptureCommand{Content: strings.Repeat("a", captureCommandMaxRunes+1)}).TooLong() {
		t.Error("a capture over the limit was accepted; the server would refuse it")
	}
}

func captureMessage(t *testing.T, body string) channel.InboundMessage {
	msg := p2pMessage(t)
	msg.Text = body
	msg.CommandText = body
	return msg
}

// A /capture is terminal: the member asked for the thought to be parked, not
// for the agent to answer it. No chat run, no typing indicator — and the
// capture carries the workspace and the mapped member as its author.
func TestRouter_CaptureCommand_FilesAndAnswers(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), captureMessage(t, "/capture pgbouncer listens on 6432")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.captures.count() != 1 {
		t.Fatalf("capture calls = %d, want 1", h.captures.count())
	}
	ws, by, content, rawURL := h.captures.snapshot()
	if ws != h.inst.inst.WorkspaceID {
		t.Errorf("workspace = %v, want the installation's", ws)
	}
	if by != h.ident.id.UserID {
		t.Errorf("author = %v, want the mapped member", by)
	}
	if content != "pgbouncer listens on 6432" || rawURL != "" {
		t.Errorf("captured (%q, %q)", content, rawURL)
	}
	if h.tasks.calls() != 0 {
		t.Errorf("a capture must not enqueue a chat run, calls=%d", h.tasks.calls())
	}
	if h.typing.calls() != 0 {
		t.Errorf("a capture must not start a processing indicator, calls=%d", h.typing.calls())
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeCaptured && r.CaptureID == h.captures.id {
				return true
			}
		}
		return false
	}) {
		t.Fatal("expected a captured reply naming the capture")
	}
}

func TestRouter_CaptureCommand_LoneLinkIsCapturedAsALink(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), captureMessage(t, "/capture https://example.com/locking")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, content, rawURL := h.captures.snapshot()
	if content != "" || rawURL != "https://example.com/locking" {
		t.Fatalf("captured (%q, %q), want the url on the url field", content, rawURL)
	}
}

// A bare /capture is answered with usage rather than writing an empty row: a
// capture nobody can act on later is worse than none.
func TestRouter_CaptureCommand_BareCommandIsUsage(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), captureMessage(t, "/capture")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.captures.count() != 0 {
		t.Fatalf("a bare /capture wrote %d capture(s)", h.captures.count())
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeCaptureUsage {
				return true
			}
		}
		return false
	}) {
		t.Fatal("expected a capture usage reply")
	}
}

// A plain chat message must reach the agent exactly as before.
func TestRouter_PlainMessageIsNotACapture(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.captures.count() != 0 {
		t.Fatalf("a plain message was captured (%d calls)", h.captures.count())
	}
}

// Without a configured creator the command is not a command: the message is
// an ordinary chat turn rather than a silently swallowed capture.
func TestRouter_CaptureCommandWithoutACreatorFallsThroughToChat(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger(), Lifecycle: h.lifecycle})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst, Identity: h.ident, Dedup: h.dedup, Session: h.binder,
		Audit: h.audit, Replier: h.replier, Typing: h.typing, Media: h.media, OriginType: "lark_chat",
	})
	if err := h.router.Handle(context.Background(), captureMessage(t, "/capture something")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.captures.count() != 0 {
		t.Fatalf("capture calls = %d, want 0 with no creator wired", h.captures.count())
	}
	if h.tasks.calls() == 0 {
		t.Fatal("the message should have been handled as an ordinary chat turn")
	}
}
