package main

import (
	"context"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerApprovalListeners posts a just-filed ask into the workspace's chat
// channels, so an approval button reaches the team the moment the ask exists
// rather than in the next morning's digest.
//
// It exists for the two sources filed inside internal/service, which has no
// handler to call: a held status transition (issue_transition_gate.go) and a
// goal-loop question (goal_loop.go). Both publish approval:asked, so the bus
// is where they surface.
//
// Decisions publish the same event and are deliberately skipped here:
// notifyDecisionRequested publishes approval:asked BEFORE the learned-rule
// auto-decide runs, so posting from this listener would put buttons in chat
// for cards that settle themselves a millisecond later. It posts its own,
// after that check.
//
// The handler's posting is already best effort on its own goroutine, so this
// listener — which runs inline on the publisher's goroutine — never blocks the
// write that filed the ask.
func registerApprovalListeners(bus *events.Bus, h *handler.Handler) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventApprovalAsked, func(e events.Event) {
		if source, id, issueID, ok := approvalAskToPost(e.Payload); ok {
			h.PostApprovalToChannels(context.Background(), source, id, issueID)
		}
	})
}

// approvalAskToPost reads an approval:asked payload and names the ask to
// post, or reports ok=false for an event this listener leaves alone: a
// decision, which notifyDecisionRequested posts itself after the auto-decide
// check, and any payload missing what it takes to load the ask.
func approvalAskToPost(payload any) (source, id, issueID string, ok bool) {
	m, isMap := payload.(map[string]any)
	if !isMap {
		return "", "", "", false
	}
	source, _ = m["source"].(string)
	if source != handler.ApprovalSourceTransition && source != handler.ApprovalSourceGoalQuestion {
		return "", "", "", false
	}
	id, _ = m["id"].(string)
	issueID, _ = m["issue_id"].(string)
	return source, id, issueID, id != "" && issueID != ""
}
