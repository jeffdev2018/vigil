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

// JEF-301: server-side handlers that mutate goals, issue views, decision
// records, and an agent's trust mode must publish a domain event so open
// clients (web/desktop) invalidate their caches instead of waiting on a poll.
// These tests only assert an event fires with an identifying payload — the
// realtime fanout/room routing itself is covered by internal/events and
// internal/realtime.

// subscribeOnce subscribes to eventType and returns a function that blocks
// (via a mutex-guarded slice) until at least one payload matching keep has
// been recorded. The bus is synchronous, so by the time the handler call
// returns, the event (if any) has already been delivered to the callback.
func subscribeOnce(t *testing.T, eventType string, keep func(map[string]any) bool) func() (map[string]any, bool) {
	t.Helper()
	var mu sync.Mutex
	var got map[string]any
	var found bool
	testHandler.Bus.Subscribe(eventType, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || !keep(payload) {
			return
		}
		mu.Lock()
		got = payload
		found = true
		mu.Unlock()
	})
	return func() (map[string]any, bool) {
		mu.Lock()
		defer mu.Unlock()
		return got, found
	}
}

func TestGoalHandlersPublishEvents(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE workspace_id = $1 AND question LIKE 'Goal · %'`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM goal WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM decision_search_chunk WHERE source_type = 'goal' AND workspace_id = $1`, testWorkspaceID)
	})

	var created goalOut
	latestCreate := subscribeOnce(t, protocol.EventGoalCreated, func(map[string]any) bool { return true })
	testutil.Call(t, testHandler.CreateGoal, newRequest(http.MethodPost, "/api/goals",
		map[string]any{"title": "JEF-301 mission", "status": "active", "owner_id": testUserID})).Want(http.StatusCreated).JSON(&created)
	payload, ok := latestCreate()
	if !ok {
		t.Fatal("goal:created was not published")
	}
	if goal, ok := payload["goal"].(GoalResponse); !ok || goal.ID != created.ID {
		t.Fatalf("goal:created payload = %+v, want goal id %s", payload, created.ID)
	}

	latestUpdate := subscribeOnce(t, protocol.EventGoalUpdated, func(map[string]any) bool { return true })
	testutil.Call(t, testHandler.UpdateGoal, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/goals/"+created.ID, map[string]any{"title": "JEF-301 mission v2", "status": "active"}),
		"id", created.ID)).Want(http.StatusOK)
	if _, ok := latestUpdate(); !ok {
		t.Fatal("goal:updated was not published")
	}

	latestDelete := subscribeOnce(t, protocol.EventGoalDeleted, func(payload map[string]any) bool {
		return payload["goal_id"] == created.ID
	})
	testutil.Call(t, testHandler.DeleteGoal, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/goals/"+created.ID, nil), "id", created.ID)).Want(http.StatusNoContent)
	if _, ok := latestDelete(); !ok {
		t.Fatal("goal:deleted was not published")
	}
}

func TestIssueViewHandlersPublishEvents(t *testing.T) {
	latestCreate := subscribeOnce(t, protocol.EventIssueViewCreated, func(map[string]any) bool { return true })
	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "JEF-301 view",
		"scope_type": "workspace",
		"visibility": "workspace",
		"query":      map[string]any{"statusFilters": []string{"in_review"}},
		"display":    map[string]any{"viewMode": "board"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue view: %d %s", code, body)
	}
	payload, ok := latestCreate()
	if !ok {
		t.Fatal("issue_view:created was not published")
	}
	// Only the id travels: the room is the whole workspace, and a private
	// view's query and display settings are its owner's alone.
	if payload["issue_view_id"] != view.ID || payload["issue_view"] != nil {
		t.Fatalf("issue_view:created payload = %+v, want only issue_view_id %s", payload, view.ID)
	}

	latestUpdate := subscribeOnce(t, protocol.EventIssueViewUpdated, func(map[string]any) bool { return true })
	testutil.Call(t, testHandler.UpdateIssueView, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issue-views/"+view.ID, map[string]any{
			"name": "JEF-301 view renamed", "expected_revision": view.Revision,
		}), "id", view.ID)).Want(http.StatusOK)
	if _, ok := latestUpdate(); !ok {
		t.Fatal("issue_view:updated was not published")
	}

	latestDelete := subscribeOnce(t, protocol.EventIssueViewDeleted, func(payload map[string]any) bool {
		return payload["issue_view_id"] == view.ID
	})
	testutil.Call(t, testHandler.DeleteIssueView, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/issue-views/"+view.ID, nil), "id", view.ID)).Want(http.StatusNoContent)
	if _, ok := latestDelete(); !ok {
		t.Fatal("issue_view:deleted was not published")
	}
}

func TestDecisionRecordCreationPublishesEvent(t *testing.T) {
	issueID, taskID, _ := decisionRun(t, "jef-301 decision publish")
	_ = taskID

	latest := subscribeOnce(t, protocol.EventDecisionCreated, func(map[string]any) bool { return true })
	var created struct {
		Decisions []DecisionRecordResponse `json:"decisions"`
	}
	testutil.Call(t, testHandler.CreateIssueDecisions, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/decision-records", map[string]any{
			"decisions": []map[string]any{{"source_message_seq": 2, "title": "JEF-301 decision", "context": "c", "decision": "d"}},
		}), "id", issueID)).Want(http.StatusCreated).JSON(&created)
	if len(created.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %+v", created.Decisions)
	}

	payload, ok := latest()
	if !ok {
		t.Fatal("decision:created was not published")
	}
	dec, ok := payload["decision"].(DecisionRecordResponse)
	if !ok || dec.ID != created.Decisions[0].ID {
		t.Fatalf("decision:created payload = %+v, want decision id %s", payload, created.Decisions[0].ID)
	}
}

func TestSetAgentTrustModePublishesAgentUpdated(t *testing.T) {
	agent := dbfx.Agent(t, "jef-301 trust dial agent", handlerTestRuntimeID(t))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM trust_mode_change WHERE agent_id = $1`, agent)
	})

	latest := subscribeOnce(t, protocol.EventAgentUpdated, func(payload map[string]any) bool {
		resp, ok := payload["agent"].(AgentResponse)
		return ok && resp.ID == agent
	})
	trustCall(t, testHandler.SetAgentTrustMode, http.MethodPut, "/api/agents/"+agent+"/trust-mode", agent,
		map[string]any{"mode": "observer", "reason": "jef-301 test"}).Want(http.StatusOK)

	payload, ok := latest()
	if !ok {
		t.Fatal("agent:updated was not published")
	}
	resp, ok := payload["agent"].(AgentResponse)
	if !ok || resp.TrustMode != "observer" {
		t.Fatalf("agent:updated payload = %+v, want trust_mode=observer", payload)
	}
}
