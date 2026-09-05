package engine

// A gated channel must not mint an issue, and the sender must be told why —
// or deliberately not told, when an admin blocked the channel outright.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type fakeTriageGate struct {
	mu       sync.Mutex
	decision TriageDecision
	seen     []ChannelIssueAdmission
}

func (g *fakeTriageGate) AdmitChannelIssue(_ context.Context, in ChannelIssueAdmission) TriageDecision {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen = append(g.seen, in)
	return g.decision
}

func (g *fakeTriageGate) calls() []ChannelIssueAdmission {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]ChannelIssueAdmission(nil), g.seen...)
}

// gatedHarness rebuilds the shared harness's router with a triage gate wired
// in. newHarness deliberately has none: the default for every channel is
// direct, and that path is covered by TestRouter_IssueCommand_Creates.
func gatedHarness(t *testing.T, gate TriageGate) *harness {
	t.Helper()
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{
		Logger: discardLogger(), Lifecycle: h.lifecycle, Triage: gate,
	})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		Media:        h.media,
		OriginType:   "lark_chat",
	})
	return h
}

func TestRouter_IssueCommand_HeldByTriageCreatesNoIssueAndTellsTheSender(t *testing.T) {
	gate := &fakeTriageGate{decision: TriageHeld}
	h := gatedHarness(t, gate)
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login", Description: "details"}}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.issues.called {
		t.Fatal("a held /issue must not reach the issue funnel")
	}
	seen := gate.calls()
	if len(seen) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(seen))
	}
	if seen[0].Title != "Fix login" || seen[0].OriginType != "lark_chat" {
		t.Fatalf("gate input = %+v, want the command title and the set's origin type", seen[0])
	}
	if !seen[0].InstallationID.Valid {
		t.Fatal("the gate keys the source on the installation; it must be passed")
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.IssueHeld && r.IssueTitle == "Fix login" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("expected a held reply carrying the command title")
	}
}

func TestRouter_IssueCommand_RefusedByTriageStaysSilent(t *testing.T) {
	h := gatedHarness(t, &fakeTriageGate{decision: TriageRefused})
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login"}}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.issues.called {
		t.Fatal("a refused /issue must not reach the issue funnel")
	}
	// Silence is the configured behavior for a blocked channel: assert the
	// replier was never asked to say anything about an issue.
	time.Sleep(50 * time.Millisecond)
	for _, r := range h.replier.calls() {
		if r.IssueHeld || r.IssueID.Valid {
			t.Fatalf("blocked channel must not answer the sender, got %+v", r)
		}
	}
}

func TestRouter_IssueCommand_AdmittedByTriageCreatesTheIssue(t *testing.T) {
	h := gatedHarness(t, &fakeTriageGate{decision: TriageAdmit})
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login"}}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.issues.called {
		t.Fatal("an admitted /issue must create the issue exactly as before")
	}
}
