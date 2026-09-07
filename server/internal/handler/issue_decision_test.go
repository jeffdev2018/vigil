package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func decisionRequest(method, issue, id string, body any) *http.Request {
	return testutil.WithURLParams(newRequest(method, "/api/issues/"+issue+"/decisions/"+id, body), "id", issue, "decisionID", id)
}

func decisionFixture(t *testing.T) (string, string, CreateIssueDecisionRequest) {
	t.Helper()
	issue := dbfx.Issue(t, "Human decision")
	agent := dbfx.Agent(t, "decision-"+uuid.NewString(), "", testutil.Cols{"owner_id": testUserID})
	source := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "completed", "completed_at": time.Now(), "trigger_evidence_kind": "issue_assignment"})
	t.Cleanup(func() {
		if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: parseUUID(issue), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
			t.Error(err)
		}
	})
	return issue, agent, CreateIssueDecisionRequest{ID: uuid.NewString(), SourceTaskID: source, RecipientID: testUserID, Question: "Which approach should we use?", Context: "A preserves compatibility; B needs a migration.", Options: []string{"A", "B"}}
}

func TestIssueDecisionLifecycleAndConcurrentResume(t *testing.T) {
	issue, agent, request := decisionFixture(t)
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(201)
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(200)
	changed := request
	changed.Context = "Changed"
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", changed)).Want(409)
	testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil)).Want(409)
	answer := map[string]string{"status": "answered", "answer": "A, preserve compatibility."}
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, answer)).Want(200)
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, answer)).Want(200)
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, map[string]string{"status": "cancelled"})).Want(409)
	// Missing runtime must not discard the saved answer.
	testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil)).Want(409)
	var saved IssueDecisionResponse
	testutil.Call(t, testHandler.GetIssueDecision, decisionRequest("GET", issue, request.ID, nil)).Want(200).JSON(&saved)
	if saved.Answer == nil || *saved.Answer != answer["answer"] || saved.ResumeTaskID != nil {
		t.Fatalf("answer lost: %+v", saved)
	}
	runtime := dbfx.Runtime(t, "Decision fixture")
	dbfx.Exec(t, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, agent, runtime)
	var wg sync.WaitGroup
	var codes [2]int
	var receipts [2]IssueDecisionResponse
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil))
			codes[i] = resp.Code
			resp.JSON(&receipts[i])
		}()
	}
	wg.Wait()
	sort.Ints(codes[:])
	if codes != [2]int{200, 201} || receipts[0].ResumeTaskID == nil || receipts[1].ResumeTaskID == nil || *receipts[0].ResumeTaskID != *receipts[1].ResumeTaskID {
		t.Fatalf("duplicate resume: %v %+v", codes, receipts)
	}
	taskID := *receipts[0].ResumeTaskID
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.RerunOfTaskID != parseUUID(request.SourceTaskID) || task.OriginatorUserID != parseUUID(testUserID) || !task.ForceFreshSession || !strings.Contains(task.HandoffNote.String, answer["answer"]) {
		t.Fatalf("lost attribution or handoff: %+v", task)
	}
	// A lost HTTP response cannot create a second run, even after run deletion.
	dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE id=$1`, taskID)
	testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil)).Want(200).JSON(&saved)
	if saved.ResumeTaskID == nil || *saved.ResumeTaskID != taskID {
		t.Fatal("receipt was not durable")
	}
	if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: parseUUID(issue), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_decision WHERE issue_id=$1`, issue).Scan(&count)
	if count != 0 {
		t.Fatal("orphaned decision")
	}
}

func TestIssueDecisionConcurrentAnswers(t *testing.T) {
	issue, _, request := decisionFixture(t)
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(201)
	var wg sync.WaitGroup
	var codes [2]int
	for i, answer := range []string{"A", "B"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, map[string]string{"status": "answered", "answer": answer})).Code
		}()
	}
	wg.Wait()
	sort.Ints(codes[:])
	if codes != [2]int{200, 409} {
		t.Fatalf("answers must have one winner: %v", codes)
	}
}

func TestIssueDecisionHumanAndTenantBoundaries(t *testing.T) {
	issue, agent, request := decisionFixture(t)
	machine := func(taskID string) *http.Request {
		r := decisionRequest("POST", issue, "", request)
		r.Header.Set("X-Actor-Source", "task_token")
		r.Header.Set("X-Agent-ID", agent)
		r.Header.Set("X-Task-ID", taskID)
		return r
	}
	testutil.Call(t, testHandler.CreateIssueDecision, machine(uuid.NewString())).Want(403)
	testutil.Call(t, testHandler.CreateIssueDecision, machine(request.SourceTaskID)).Want(201)
	for _, source := range []string{"task_token", "cloud_pat"} {
		r := decisionRequest("POST", issue, request.ID, map[string]string{"status": "answered", "answer": "A"})
		r.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.AnswerIssueDecision, r).Want(403)
		testutil.Call(t, testHandler.ResumeIssueDecision, r).Want(403)
		testutil.Call(t, testHandler.ListIssueDecisions, r).Want(403)
	}
	r := machine(request.SourceTaskID)
	r = testutil.WithURLParams(r, "id", issue, "decisionID", request.ID)
	testutil.Call(t, testHandler.GetIssueDecision, r).Want(200)
	r.Header.Set("X-Task-ID", uuid.NewString())
	testutil.Call(t, testHandler.GetIssueDecision, r).Want(404)
	other := dbfx.User(t, "Decision other", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, other, "member")
	for _, handler := range []http.HandlerFunc{testHandler.GetIssueDecision, testHandler.AnswerIssueDecision, testHandler.ResumeIssueDecision} {
		r := decisionRequest("POST", issue, request.ID, map[string]string{"status": "answered", "answer": "A"})
		r.Header.Set("X-User-ID", other)
		testutil.Call(t, handler, r).Want(404)
	}
	r = newRequest("GET", "/api/inbox/decisions", nil)
	r.Header.Set("X-User-ID", other)
	var page struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
	}
	testutil.Call(t, testHandler.ListIssueDecisions, r).Want(200).JSON(&page)
	if len(page.Decisions) != 0 {
		t.Fatal("another recipient's decision leaked")
	}
	r = newRequest("GET", "/api/inbox/decisions?before_id="+request.ID, nil)
	r.Header.Set("X-User-ID", other)
	testutil.Call(t, testHandler.ListIssueDecisions, r).Want(404)
	foreignWorkspace := dbfx.Workspace(t, "Decision foreign", "decision-"+uuid.NewString())
	r = decisionRequest("GET", issue, request.ID, nil)
	r.Header.Set("X-Workspace-ID", foreignWorkspace)
	testutil.Call(t, testHandler.GetIssueDecision, r).Want(404)
}

func TestIssueDecisionSourcePrivacyAndCancellation(t *testing.T) {
	issue, agent, request := decisionFixture(t)
	for _, cols := range []testutil.Cols{
		{"trigger_evidence_kind": nil},
		{"trigger_evidence_kind": "chat"},
		{"trigger_evidence_kind": "issue_assignment", "memory_context": `{"is_chat":true}`},
		{"trigger_evidence_kind": "issue_assignment", "memory_context": `{"is_chat":null}`},
		{"trigger_evidence_kind": "issue_assignment", "chat_session_id": dbfx.ChatSession(t, agent)},
	} {
		cols["issue_id"], cols["status"], cols["completed_at"] = issue, "completed", time.Now()
		private := request
		private.SourceTaskID = dbfx.Task(t, agent, cols)
		testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", private)).Want(404)
	}
	bad := request
	bad.RecipientID = dbfx.User(t, "Nonmember", uuid.NewString()+"@example.test")
	machineRequest := decisionRequest("POST", issue, "", bad)
	machineRequest.Header.Set("X-Actor-Source", "task_token")
	machineRequest.Header.Set("X-Agent-ID", agent)
	machineRequest.Header.Set("X-Task-ID", request.SourceTaskID)
	testutil.Call(t, testHandler.CreateIssueDecision, machineRequest).Want(400)
	for _, options := range [][]string{{""}, {"A", " A "}, {strings.Repeat("x", 501)}} {
		bad = request
		bad.Options = options
		testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", bad)).Want(400)
	}
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(201)
	cancel := map[string]string{"status": "cancelled"}
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, cancel)).Want(200)
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, cancel)).Want(200)
	testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil)).Want(409)
}

func TestIssueDecisionPendingHistoryPaginationAndClaim(t *testing.T) {
	issue, agent, request := decisionFixture(t)
	runtime := dbfx.Runtime(t, "Decision claim")
	dbfx.Exec(t, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, agent, runtime)
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(201)
	testutil.Call(t, testHandler.AnswerIssueDecision, decisionRequest("POST", issue, request.ID, map[string]string{"status": "answered", "answer": "Preserve compatibility"})).Want(200)
	var page struct {
		Decisions    []IssueDecisionResponse `json:"decisions"`
		NextBeforeID *string                 `json:"next_before_id"`
	}
	readPage := func(query string) {
		t.Helper()
		testutil.Call(t, testHandler.ListIssueDecisions, newRequest("GET", "/api/inbox/decisions"+query, nil)).Want(200).JSON(&page)
	}
	readPage("")
	if len(page.Decisions) != 1 || page.Decisions[0].Status != "answered" {
		t.Fatalf("saved answer must stay actionable: %+v", page)
	}
	var receipt IssueDecisionResponse
	testutil.Call(t, testHandler.ResumeIssueDecision, decisionRequest("POST", issue, request.ID, nil)).Want(201).JSON(&receipt)
	var claimed struct {
		Task *daemon.Task `json:"task"`
	}
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtime+"/claim", nil, testWorkspaceID, "decision-claim")
	testutil.Call(t, testHandler.ClaimTaskByRuntime, testutil.WithURLParams(req, "runtimeId", runtime)).Want(200).JSON(&claimed)
	if claimed.Task == nil || claimed.Task.ID != *receipt.ResumeTaskID || !strings.Contains(daemon.BuildPrompt(*claimed.Task, "claude"), "Preserve compatibility") {
		t.Fatal("answer did not reach the daemon prompt")
	}
	readPage("")
	if len(page.Decisions) != 0 {
		t.Fatal("resumed decision remains pending")
	}
	// Populate immutable history to exercise page boundaries without 51 HTTP mutations.
	for range 51 {
		dbfx.Exec(t, `INSERT INTO issue_decision SELECT $1,workspace_id,issue_id,agent_id,source_task_id,recipient_id,requested_by,requester_type,question,context,options,input_hash,status,answer,answered_by,answered_at,resume_task_id,clock_timestamp() FROM issue_decision WHERE id=$2`, uuid.NewString(), request.ID)
	}
	readPage("?history=true")
	if len(page.Decisions) != 50 || page.NextBeforeID == nil {
		t.Fatalf("history not paginated: %+v", page)
	}
	cursor := *page.NextBeforeID
	readPage("?history=true&before_id=" + cursor)
	if len(page.Decisions) != 2 || page.NextBeforeID != nil {
		t.Fatalf("history page lost records: %+v", page)
	}
}

func TestIssueDecisionHumanCreateRetryCannotReadAnotherRecipient(t *testing.T) {
	issue, _, request := decisionFixture(t)
	other := dbfx.User(t, "Decision recipient", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, other, "admin")
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(201)
	request.RecipientID = other
	testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(403)
	// Simulate a stored cross-recipient request from an earlier writer. Even its
	// original requester must not recover another human's answer through POST.
	hash, err := deliveryHash(request)
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `UPDATE issue_decision SET recipient_id=$2,input_hash=$3 WHERE id=$1`, request.ID, other, hash)
	answer := decisionRequest("POST", issue, request.ID, map[string]string{"status": "answered", "answer": "Recipient-only response"})
	answer.Header.Set("X-User-ID", other)
	testutil.Call(t, testHandler.AnswerIssueDecision, answer).Want(200)
	response := testutil.Call(t, testHandler.CreateIssueDecision, decisionRequest("POST", issue, "", request)).Want(403)
	if strings.Contains(response.Body.String(), "Recipient-only response") {
		t.Fatal("create retry leaked the answer")
	}
	testutil.Call(t, testHandler.GetIssueDecision, decisionRequest("GET", issue, request.ID, nil)).Want(404)
}
