package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A meeting action is a parked delivery like a gated webhook: it must
// announce itself with triage:new and go through the workspace triage rules
// (K62) and, failing a match, the auto-classifier (K61). Before this the
// capture wrote the row and stopped there.
func TestMeetingActionsRunTheTriageChain(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM business_rule_violation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM business_rule WHERE workspace_id = $1`, testWorkspaceID)
	})

	var rule struct{ Rule BusinessRuleResponse }
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{
		"natural_language": "Ignore standup chatter", "attach_point": "webhook_received",
		"predicate": map[string]any{"all": []map[string]any{{"field": "webhook.title", "op": "contains", "value": "standup chatter"}}},
		"action":    map[string]any{"kind": "dismiss"},
	}).Want(http.StatusCreated).JSON(&rule)
	ruleCall(t, testHandler.ActivateBusinessRule, http.MethodPut, "/api/business-rules/"+rule.Rule.ID+"/activate", nil, "id", rule.Rule.ID).Want(http.StatusOK)

	var mu sync.Mutex
	var announced []string
	testHandler.Bus.Subscribe(protocol.EventTriageNew, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		id, _ := payload["item_id"].(string)
		mu.Lock()
		announced = append(announced, id)
		mu.Unlock()
	})

	stubSTT(t, "On reparle du standup chatter demain.")
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"summary_markdown":"- standup","actions":[`+
			`{"title":"standup chatter follow-up","owner":"Paul","evidence":"On reparle du standup chatter demain."}]}`))

	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Chain", "app_name": "Zoom"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)).
		Want(http.StatusOK)

	var done MeetingResponse
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&done)

	if len(done.Actions) != 1 {
		t.Fatalf("actions = %+v, want one captured action", done.Actions)
	}
	itemID := done.Actions[0].TriageItemID
	if state := triageState(t, itemID); state != "dismissed" {
		t.Fatalf("item state = %s, want dismissed by the active webhook rule", state)
	}
	// The response must report the state the rule moved it to, not a stale
	// pending that would link the reader at an item no longer in the queue.
	if done.Actions[0].State != "dismissed" {
		t.Fatalf("response action state = %s, want dismissed", done.Actions[0].State)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range announced {
		if id == itemID {
			return
		}
	}
	t.Fatalf("triage:new was not published for %s (saw %v)", itemID, announced)
}
