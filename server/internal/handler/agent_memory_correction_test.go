package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAgentMemoryDeliveryCorrectionCandidateLifecycle(t *testing.T) {
	issue, agent, task, _, review := correctionFixture(t)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM agent_memory_version WHERE agent_id=$1`, agent)
		dbfx.Exec(t, `DELETE FROM agent_memory WHERE agent_id=$1`, agent)
	})
	body := map[string]any{"content": "Preserve the current project when opening an issue form.", "source_review_id": review.ID, "source_task_id": task}
	type result struct {
		code   int
		memory AgentMemoryResponse
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			response := testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body))
			var memory AgentMemoryResponse
			response.JSON(&memory)
			results <- result{response.Code, memory}
		}()
	}
	a, b := <-results, <-results
	codes := []int{a.code, b.code}
	sort.Ints(codes)
	if codes[0] != 200 || codes[1] != 201 || a.memory.ID == "" || a.memory.ID != b.memory.ID {
		t.Fatalf("not one candidate: %+v %+v", a, b)
	}
	memory := a.memory
	if memory.Status != "pending" || memory.ReviewedBy != nil {
		t.Fatalf("proposal was approved: %+v", memory)
	}
	var source AgentMemoryCorrectionSource
	if err := json.Unmarshal(memory.SourceReview, &source); err != nil {
		t.Fatal(err)
	}
	if source.ReviewID != review.ID || source.IssueID != issue || source.TaskID != task || source.Feedback != review.Feedback || source.Assessments[0].Evidence != review.Assessments[0].Evidence || source.ReviewedBy != testUserID {
		t.Fatalf("lost evidence: %+v", source)
	}
	rows, _, err := testHandler.TaskService.LoadAgentMemories(context.Background(), parseUUID(agent), parseUUID(testWorkspaceID))
	if err != nil || len(rows) != 0 {
		t.Fatalf("pending candidate reached the briefing query: %v %v", rows, err)
	}
	var accepted AgentMemoryResponse
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agent, memory.ID, map[string]any{"status": "active", "expected_revision": 1})).Want(http.StatusOK).JSON(&accepted)
	rows, _, err = testHandler.TaskService.LoadAgentMemories(context.Background(), parseUUID(agent), parseUUID(testWorkspaceID))
	if err != nil || len(rows) != 1 || rows[0] != memory.Content {
		t.Fatalf("approved candidate missing: %v %v", rows, err)
	}
	// Provenance belongs to the candidate after capture, so source deletion
	// cannot silently erase the basis of an already approved lesson.
	dbfx.Exec(t, `DELETE FROM issue_delivery_review WHERE id=$1`, review.ID)
	dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, task)
	var retry AgentMemoryResponse
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body)).Want(http.StatusOK).JSON(&retry)
	if retry.ID != memory.ID || retry.Status != "active" || retry.Revision != 2 || !bytes.Equal(retry.SourceReview, memory.SourceReview) {
		t.Fatalf("retry changed memory: %+v", retry)
	}
	var history AgentMemoryHistoryResponse
	testutil.Call(t, testHandler.ListAgentMemoryHistory, agentMemoryRequest("GET", agent, memory.ID, nil)).Want(http.StatusOK).JSON(&history)
	if len(history.Versions) != 2 || !bytes.Equal(history.Versions[1].SourceReview, memory.SourceReview) {
		t.Fatalf("history lost provenance: %+v", history)
	}
	testutil.Call(t, testHandler.UpdateAgentMemory, agentMemoryRequest("PUT", agent, memory.ID, map[string]any{"restore_revision": 1, "expected_revision": 2})).Want(http.StatusOK).JSON(&retry)
	if retry.Status != "pending" || !bytes.Equal(retry.SourceReview, memory.SourceReview) {
		t.Fatal("restore approved or changed source")
	}
	body["content"] = "A different lesson"
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body)).Want(http.StatusConflict)
}

func TestAgentMemoryDeliveryCorrectionBoundariesAndRollback(t *testing.T) {
	issue, agent, task, _, review := correctionFixture(t)
	body := map[string]any{"content": "Keep the project context.", "source_review_id": review.ID}
	other := agentMemoryFixture(t, "Other correction agent")
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", other, "", body)).Want(http.StatusNotFound)
	for _, actor := range []string{"task_token", "cloud_pat"} {
		req := agentMemoryRequest("POST", agent, "", body)
		req.Header.Set("X-Actor-Source", actor)
		testutil.Call(t, testHandler.CreateAgentMemory, req).Want(http.StatusForbidden)
	}
	req := agentMemoryRequest("POST", agent, "", body)
	req.Header.Set("X-Workspace-ID", uuid.NewString())
	testutil.Call(t, testHandler.CreateAgentMemory, req).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", map[string]any{"content": "Invalid source", "source_review_id": "bad"})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", map[string]any{"content": "Mismatched source", "source_review_id": review.ID, "source_task_id": uuid.NewString()})).Want(http.StatusBadRequest)
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_session_id=$2 WHERE id=$1`, task, dbfx.ChatSession(t, agent))
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body)).Want(http.StatusNotFound)
	dbfx.Exec(t, `UPDATE agent_task_queue SET chat_session_id=NULL WHERE id=$1`, task)
	original := testHandler.TxStarter
	t.Cleanup(func() { testHandler.TxStarter = original })
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body)).Want(http.StatusInternalServerError)
	testHandler.TxStarter = original
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory_version WHERE agent_id=$1`, agent).Scan(&count)
	if count != 0 {
		t.Fatal("rollback left candidate history")
	}
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_memory WHERE agent_id=$1`, agent).Scan(&count)
	if count != 0 {
		t.Fatal("rollback left candidate")
	}
	var accepted DeliveryReview
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, reviewRequest(readDelivery(t, issue)))).Want(http.StatusCreated).JSON(&accepted)
	body["source_review_id"] = accepted.ID
	testutil.Call(t, testHandler.CreateAgentMemory, agentMemoryRequest("POST", agent, "", body)).Want(http.StatusConflict)
}
