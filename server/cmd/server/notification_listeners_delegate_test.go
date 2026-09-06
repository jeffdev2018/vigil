package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// F01 (JEF-5): naming a human delegate subscribes them to the issue and drops
// one inbox item on them. Both are listener behaviour, so both are asserted
// against a published issue:updated rather than through HTTP.
//
// Uses the same DATABASE_URL fixture as the rest of the cmd/server suite
// (testPool / testUserID / testWorkspaceID from integration_test.go): no extra
// environment is needed beyond the one the handler suite already uses.

// delegateSubscriberReason returns the reason on the subscriber row for userID,
// or "" when there is none. It reads the reason rather than just asking
// "subscribed?" because the whole point of migration 755 is that a delegate is
// NOT the pre-existing 'delegated' tier.
func delegateSubscriberReason(t *testing.T, issueID, userID string) string {
	t.Helper()
	var reason string
	err := testPool.QueryRow(context.Background(), `
		SELECT reason FROM issue_subscriber
		WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2
		  AND unsubscribed_at IS NULL
	`, issueID, userID).Scan(&reason)
	if err != nil {
		return ""
	}
	return reason
}

func inboxTypesFor(t *testing.T, issueID, userID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT type FROM inbox_item
		WHERE issue_id = $1 AND recipient_type = 'member' AND recipient_id = $2
		ORDER BY created_at
	`, issueID, userID)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var typ string
		if err := rows.Scan(&typ); err != nil {
			t.Fatalf("scan inbox type: %v", err)
		}
		out = append(out, typ)
	}
	return out
}

func hasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// delegateTestUser creates a workspace member distinct from the acting user, so
// the listeners' "do not notify the actor" rules do not swallow the assertion.
func delegateTestUser(t *testing.T) string {
	t.Helper()
	email := fmt.Sprintf("delegate-listener-%d@multica.ai", time.Now().UnixNano())
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add delegate member: %v", err)
	}
	return userID
}

func publishDelegateChanged(t *testing.T, bus *events.Bus, issueID, delegateID string) {
	t.Helper()
	delegateType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "delegate listener issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				DelegateType: &delegateType,
				DelegateID:   &delegateID,
			},
			"delegate_changed": true,
		},
	})
}

func TestDelegateChangedSubscribesTheDelegate(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	delegateID := delegateTestUser(t)

	publishDelegateChanged(t, bus, issueID, delegateID)

	if !isSubscribed(t, queries, issueID, "member", delegateID) {
		t.Fatal("delegate_changed did not subscribe the new delegate")
	}
	// 'delegate', not 'delegated': the latter is MUL-5483's narrowed
	// agent-filed-on-your-behalf tier, which would suppress the inbox item
	// asserted below and be overwritten by the delegate's first comment.
	if reason := delegateSubscriberReason(t, issueID, delegateID); reason != "delegate" {
		t.Fatalf("subscriber reason = %q, want \"delegate\"", reason)
	}
}

func TestDelegateChangedDeliversAnInboxItem(t *testing.T) {
	bus := events.New()
	queries := db.New(testPool)
	registerSubscriberListeners(bus, testPool)
	registerNotificationListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	delegateID := delegateTestUser(t)

	publishDelegateChanged(t, bus, issueID, delegateID)

	types := inboxTypesFor(t, issueID, delegateID)
	if !hasType(types, "delegate_assigned") {
		t.Fatalf("delegate inbox types = %v, want one delegate_assigned", types)
	}
	// The delegate is not the assignee, so nothing may claim they were
	// assigned the issue — that is a different accountability statement.
	if hasType(types, "issue_assigned") {
		t.Fatalf("delegate inbox types = %v; a delegate must not receive issue_assigned", types)
	}
}

// A no-op write (delegate_changed false) must raise nothing: the flag is
// computed from the persisted before/after, so re-sending the delegate an
// issue already has has to stay silent rather than re-notify.
func TestDelegateUnchangedRaisesNothing(t *testing.T) {
	bus := events.New()
	queries := db.New(testPool)
	registerSubscriberListeners(bus, testPool)
	registerNotificationListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })
	delegateID := delegateTestUser(t)

	delegateType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID: issueID, WorkspaceID: testWorkspaceID, Title: "unchanged",
				Status: "todo", Priority: "medium",
				CreatorType: "member", CreatorID: testUserID,
				DelegateType: &delegateType, DelegateID: &delegateID,
			},
			"delegate_changed": false,
		},
	})

	if reason := delegateSubscriberReason(t, issueID, delegateID); reason != "" {
		t.Fatalf("a no-op delegate write subscribed the delegate as %q", reason)
	}
	if types := inboxTypesFor(t, issueID, delegateID); len(types) != 0 {
		t.Fatalf("a no-op delegate write produced inbox items %v", types)
	}
}
