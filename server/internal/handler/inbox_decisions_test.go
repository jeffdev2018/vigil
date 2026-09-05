package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Inbox zero (K63): my pending cards with their options, urgent first, at
// most five with the total; an answered card leaves the list; the list is
// workspace-scoped through the attention inbox query it projects.

func TestInboxDecisionsCapAndOrder(t *testing.T) {
	type payload struct {
		Decisions []InboxDecisionItem `json:"decisions"`
		Total     int                 `json:"total"`
	}
	list := func() payload {
		t.Helper()
		var out payload
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxDecisions), inboxRequest(http.MethodGet, "/api/inbox/decisions", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
		return out
	}
	before := list().Total
	var urgentID string
	for i := 0; i < 6; i++ {
		issue := dbfx.Issue(t, "decision inbox issue")
		body := decisionBody()
		if i == 3 {
			body["urgency"] = "high"
		}
		var created decisionEnvelope
		askDecision(t, issue, body).Want(http.StatusCreated).JSON(&created)
		if i == 3 {
			urgentID = created.Decision.ID
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
			testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		})
	}
	got := list()
	if got.Total != before+6 || len(got.Decisions) != inboxDecisionsCap {
		t.Fatalf("total %d (before %d), listed %d", got.Total, before, len(got.Decisions))
	}
	// Urgent cards come first (other tests may leave urgent cards behind; ours must be among them).
	var urgent *InboxDecisionItem
	for i := range got.Decisions {
		if got.Decisions[i].Decision.ID == urgentID {
			urgent = &got.Decisions[i]
		}
	}
	if got.Decisions[0].Decision.Urgency != "high" || urgent == nil || urgent.RiskScore < got.Decisions[len(got.Decisions)-1].RiskScore {
		t.Fatalf("ordering = %+v", got.Decisions)
	}
	if len(urgent.Decision.Options) == 0 || urgent.IssueIdentifier == "" || urgent.IssueTitle != "decision inbox issue" {
		t.Fatalf("urgent card = %+v", urgent)
	}
	// Answering from the list removes the card and lowers the total.
	respondDecision(t, urgent.IssueID, urgent.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK)
	got = list()
	if got.Total != before+5 {
		t.Fatalf("total after answer = %d, want %d", got.Total, before+5)
	}
	for _, d := range got.Decisions {
		if d.Decision.ID == urgentID {
			t.Fatal("an answered card must leave the list")
		}
	}
}
