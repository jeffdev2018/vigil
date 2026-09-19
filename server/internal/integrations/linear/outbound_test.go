package linear

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// events.Bus.Publish calls every listener synchronously, inline on the
// publishing request's goroutine. onComment/onIssueUpdated/onTaskDone push to
// the Linear API, which can stall; if that push ran inline the request that
// published the comment/status/run-outcome event would be blocked on it too.
// These prove each handler hands its push off to another goroutine instead.

func linkedIssue(store *fakeStore, inst db.LinearInstallation) db.LinearIssueLink {
	link := db.LinearIssueLink{
		IssueID:               newUUID(),
		WorkspaceID:           inst.WorkspaceID,
		InstallationID:        inst.ID,
		LinearIssueID:         "iss_1",
		LinearIssueIdentifier: "ENG-7",
		LinearTeamID:          "team_1",
		SyncState:             "active",
	}
	store.issueLinks = append(store.issueLinks, link)
	return link
}

// awaitReturn fails the test if fn does not return within the timeout —
// the symptom of a handler that still runs its push inline.
func awaitReturn(t *testing.T, timeout time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("%s blocked for at least %s instead of handing the Linear push off", what, timeout)
	}
}

func TestOnCommentHandsThePushOffInsteadOfBlockingThePublisher(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	link := linkedIssue(store, inst)
	block := make(chan struct{})
	store.blockIssueLinkLookup = block
	t.Cleanup(func() { close(block) })

	ob := NewOutbound(bridge, nil, nil)
	awaitReturn(t, 200*time.Millisecond, "onComment", func() {
		ob.onComment(events.Event{Type: protocol.EventCommentCreated, Payload: map[string]any{
			"comment": map[string]any{
				"id": util.UUIDToString(newUUID()), "issue_id": util.UUIDToString(link.IssueID),
				"author_type": "member", "content": "hi", "type": "comment",
			},
		}})
	})
}

func TestOnIssueUpdatedHandsThePushOffInsteadOfBlockingThePublisher(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	link := linkedIssue(store, inst)
	block := make(chan struct{})
	store.blockIssueLinkLookup = block
	t.Cleanup(func() { close(block) })

	ob := NewOutbound(bridge, nil, nil)
	awaitReturn(t, 200*time.Millisecond, "onIssueUpdated", func() {
		ob.onIssueUpdated(events.Event{Type: protocol.EventIssueUpdated, Payload: map[string]any{
			"status_changed": true,
			"issue":          map[string]any{"id": util.UUIDToString(link.IssueID), "status": "in_progress"},
		}})
	})
}

func TestOnTaskDoneHandsThePushOffInsteadOfBlockingThePublisher(t *testing.T) {
	bridge, store, _, _, inst := newTestBridge(t)
	link := linkedIssue(store, inst)
	block := make(chan struct{})
	store.blockIssueLinkLookup = block
	t.Cleanup(func() { close(block) })

	ob := NewOutbound(bridge, nil, nil)
	awaitReturn(t, 200*time.Millisecond, "onTaskDone", func() {
		ob.onTaskDone(events.Event{Type: protocol.EventTaskCompleted, Payload: map[string]any{
			"issue_id": util.UUIDToString(link.IssueID),
		}})
	})
}
