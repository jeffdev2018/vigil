package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
)

// Inline approvals: the bus listener posts the two ask sources filed inside
// internal/service and deliberately leaves decisions to
// notifyDecisionRequested, which runs after the learned-rule auto-decide.
func TestApprovalAskToPost(t *testing.T) {
	ask := func(source string) map[string]any {
		return map[string]any{"source": source, "id": "ask-1", "issue_id": "issue-1", "kind": source}
	}
	for _, source := range []string{handler.ApprovalSourceTransition, handler.ApprovalSourceGoalQuestion} {
		gotSource, id, issueID, ok := approvalAskToPost(ask(source))
		if !ok || gotSource != source || id != "ask-1" || issueID != "issue-1" {
			t.Fatalf("%s = %q %q %q (ok=%v)", source, gotSource, id, issueID, ok)
		}
	}
	// A decision publishes the same event; posting it here would put buttons
	// in chat for a card a learned rule settles a millisecond later.
	if _, _, _, ok := approvalAskToPost(ask(handler.ApprovalSourceDecision)); ok {
		t.Fatal("a decision must not be posted from the listener")
	}
	for name, payload := range map[string]any{
		"not a map":  "approval:asked",
		"no source":  map[string]any{"id": "a", "issue_id": "i"},
		"no id":      map[string]any{"source": handler.ApprovalSourceTransition, "issue_id": "i"},
		"no issue":   map[string]any{"source": handler.ApprovalSourceGoalQuestion, "id": "a"},
		"wrong type": map[string]any{"source": 7, "id": "a", "issue_id": "i"},
	} {
		if _, _, _, ok := approvalAskToPost(payload); ok {
			t.Fatalf("%s must not be posted", name)
		}
	}
}
