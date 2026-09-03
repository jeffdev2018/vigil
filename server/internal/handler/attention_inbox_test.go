package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Attention Inbox (K02): only human-needed items, decisions leave once
// answered, risk decides the order.

func listAttention(t *testing.T) []AttentionInboxItem {
	t.Helper()
	var out struct {
		Items []AttentionInboxItem `json:"items"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListAttentionInbox),
		inboxRequest(http.MethodGet, "/api/inbox/attention", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
	return out.Items
}

func attentionItem(items []AttentionInboxItem, id string) *AttentionInboxItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func TestAttentionInboxFollowsDecisionCards(t *testing.T) {
	issue := dbfx.Issue(t, "attention decision issue")
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)

	// Asking filed an inbox item for the owner, carrying the decision id.
	var inboxID string
	dbfx.QueryRow(t, `SELECT id FROM inbox_item WHERE recipient_id = $1 AND type = 'decision_request' AND details->>'decision_id' = $2`,
		testUserID, created.Decision.ID).Scan(&inboxID)
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE id = $1`, inboxID) })

	item := attentionItem(listAttention(t), inboxID)
	if item == nil {
		t.Fatal("pending decision must be in the attention inbox")
	}
	if item.Severity != "action_required" || item.Reason != "decision_urgent" || item.RiskScore < attentionWeightActionRequired+attentionWeightDecision+attentionWeightUrgencyHigh {
		t.Fatalf("item = %+v, want an urgent decision scored as such", item)
	}

	respondDecision(t, issue, created.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK)
	if attentionItem(listAttention(t), inboxID) != nil {
		t.Fatal("an answered decision must leave the attention inbox")
	}
	// It stays in the regular inbox as history.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE id = $1 AND archived = false`, inboxID); n != 1 {
		t.Fatalf("regular inbox row = %d, want kept", n)
	}
}

func TestAttentionInboxFiltersAndRanks(t *testing.T) {
	issue := dbfx.Issue(t, "attention ranking issue")
	insert := func(typ, severity string, ageHours int, archived bool) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   testWorkspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           typ,
			"severity":       severity,
			"issue_id":       issue,
			"title":          "attention " + typ,
			"archived":       archived,
			"created_at":     time.Now().Add(-time.Duration(ageHours) * time.Hour),
		})
	}
	failedOld := insert("task_failed", "action_required", 10, false)
	blockedNew := insert("agent_blocked", "attention", 0, false)
	noise := insert("status_changed", "info", 0, false)
	archived := insert("task_failed", "action_required", 0, true)
	// A decision item whose card no longer exists (orphan) is dropped, not a crash.
	orphan := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": testUserID,
		"type": "decision_request", "severity": "attention", "issue_id": issue, "title": "orphan",
		"details": `{"decision_id":"` + uuid.NewString() + `"}`,
	})

	items := listAttention(t)
	for _, id := range []string{noise, archived, orphan} {
		if attentionItem(items, id) != nil {
			t.Fatalf("item %s must not be in the attention inbox", id)
		}
	}
	f, b := attentionItem(items, failedOld), attentionItem(items, blockedNew)
	if f == nil || b == nil {
		t.Fatalf("failed and blocked items must be listed: %+v", items)
	}
	if f.RiskScore <= b.RiskScore {
		t.Fatalf("older action_required (%d) must outrank fresh attention (%d)", f.RiskScore, b.RiskScore)
	}
	// Age caps: a two-week-old item does not outscore the cap.
	old := db.ListInboxItemsRow{Severity: "info", CreatedAt: pgtype.Timestamptz{Time: time.Now().Add(-14 * 24 * time.Hour), Valid: true}}
	if score, _ := attentionScore(old, time.Now()); score != attentionMaxAgeBonus*attentionWeightPerHour {
		t.Fatalf("age bonus = %d, want the cap %d", score, attentionMaxAgeBonus*attentionWeightPerHour)
	}
}

func TestAttentionInboxIsWorkspaceScoped(t *testing.T) {
	foreign := dbfx.Workspace(t, "Attention foreign", "attention-foreign-"+uuid.NewString())
	dbfx.Member(t, foreign, testUserID, "owner")
	foreignIssue := dbfx.Issue(t, "attention foreign issue", testutil.Cols{"workspace_id": foreign})
	id := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": foreign, "recipient_type": "member", "recipient_id": testUserID,
		"type": "task_failed", "severity": "action_required", "issue_id": foreignIssue, "title": "foreign",
	})
	if attentionItem(listAttention(t), id) != nil {
		t.Fatal("another workspace's item leaked into the attention inbox")
	}
}
