package handler

import (
	"context"
	"encoding/json"
	"fmt"
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

func deliveryRequest(method, id string, body any) *http.Request {
	return testutil.WithURLParams(newRequest(method, "/api/issues/"+id+"/delivery", body), "id", id)
}

func deliveryFixture(t *testing.T) (string, string, string) {
	t.Helper()
	issue := dbfx.Issue(t, "Verified delivery")
	agent := dbfx.Agent(t, "delivery-"+uuid.NewString(), "")
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "completed", "completed_at": time.Now(), "result": `{"summary":"Fixed the empty state"}`})
	t.Cleanup(func() {
		if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: parseUUID(issue), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
			t.Error(err)
		}
	})
	testutil.Call(t, testHandler.UpdateIssueDeliveryCriteria, deliveryRequest("PUT", issue, map[string]any{
		"criteria": []string{"An empty inbox explains what to do next."}, "expected_revision": 0,
	})).Want(http.StatusOK)
	return issue, agent, task
}

func readDelivery(t *testing.T, issue string) IssueDeliveryResponse {
	t.Helper()
	var result IssueDeliveryResponse
	testutil.Call(t, testHandler.GetIssueDelivery, deliveryRequest("GET", issue, nil)).Want(http.StatusOK).JSON(&result)
	return result
}

func reviewRequest(snapshot IssueDeliveryResponse) ReviewIssueDeliveryRequest {
	latest := ""
	if snapshot.LatestReview != nil {
		latest = snapshot.LatestReview.ID
	}
	return ReviewIssueDeliveryRequest{ReviewID: uuid.NewString(), ExpectedReviewID: &latest, SnapshotToken: snapshot.SnapshotToken,
		Decision: "accepted", Assessments: []DeliveryAssessment{{Passed: true, Evidence: "Manually checked the empty inbox at 390 px."}}}
}

func TestIssueDeliveryReviewFreshnessAndRetry(t *testing.T) {
	issue, agent, task := deliveryFixture(t)
	initial := readDelivery(t, issue)
	if initial.Run == nil || initial.Run.ID != task || initial.LatestReview != nil || initial.Revision != 1 {
		t.Fatalf("unexpected delivery: %+v", initial)
	}
	req := reviewRequest(initial)
	var review DeliveryReview
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Want(http.StatusCreated).JSON(&review)
	if review.ReviewedBy != testUserID || review.Snapshot.Run.ID != task || review.Assessments[0].Evidence != req.Assessments[0].Evidence {
		t.Fatalf("review lost attribution or evidence: %+v", review)
	}
	if current := readDelivery(t, issue); current.ReviewStale || current.LatestReview.ID != review.ID {
		t.Fatalf("new acceptance is stale: %+v", current)
	}
	// The same request is idempotent, including after the delivery has changed.
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Want(http.StatusOK)
	dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "failed", "completed_at": time.Now(), "created_at": time.Now().Add(time.Second)})
	current := readDelivery(t, issue)
	if !current.ReviewStale || current.LatestReview.Snapshot.Run.ID != task {
		t.Fatalf("new run must invalidate without rewriting the old evidence: %+v", current)
	}
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Want(http.StatusOK)
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, reviewRequest(current))).Want(http.StatusConflict)
	req.Feedback = "Different request with same identifier"
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Want(http.StatusConflict)
}

func TestIssueDeliveryCriteriaAndHumanBoundary(t *testing.T) {
	issue, _, _ := deliveryFixture(t)
	initial := readDelivery(t, issue)
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := deliveryRequest("POST", issue, reviewRequest(initial))
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.ReviewIssueDelivery, req).Want(http.StatusForbidden)
	}
	outsider := dbfx.User(t, "Delivery outsider", uuid.NewString()+"@example.test")
	for _, method := range []string{"GET", "PUT", "POST"} {
		req := deliveryRequest(method, issue, reviewRequest(initial))
		req.Header.Set("X-User-ID", outsider)
		h := testHandler.GetIssueDelivery
		if method == "PUT" {
			h = testHandler.UpdateIssueDeliveryCriteria
		}
		if method == "POST" {
			h = testHandler.ReviewIssueDelivery
		}
		testutil.Call(t, h, req).Want(http.StatusNotFound)
	}
	for _, body := range []any{
		map[string]any{"criteria": []string{"Changed"}},
		map[string]any{"criteria": []string{""}, "expected_revision": 1},
		map[string]any{"criteria": []string{strings.Repeat("x", 501)}, "expected_revision": 1},
		map[string]any{"criteria": []string{"first\nsecond"}, "expected_revision": 1},
	} {
		testutil.Call(t, testHandler.UpdateIssueDeliveryCriteria, deliveryRequest("PUT", issue, body)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.UpdateIssueDeliveryCriteria, deliveryRequest("PUT", issue, map[string]any{
		"criteria": []string{"Changed"}, "expected_revision": 0,
	})).Want(http.StatusConflict)
	for _, change := range []func(*ReviewIssueDeliveryRequest){
		func(r *ReviewIssueDeliveryRequest) { r.Decision = "unknown" },
		func(r *ReviewIssueDeliveryRequest) { r.ExpectedReviewID = nil },
		func(r *ReviewIssueDeliveryRequest) { r.Assessments[0].Evidence = "" },
		func(r *ReviewIssueDeliveryRequest) { r.Assessments[0].Passed = false },
		func(r *ReviewIssueDeliveryRequest) { r.Decision = "changes_requested" },
	} {
		req := reviewRequest(initial)
		change(&req)
		testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.UpdateIssueDeliveryCriteria, deliveryRequest("PUT", issue, map[string]any{
		"criteria": []string{"Updated requirement"}, "expected_revision": 1,
	})).Want(http.StatusOK)
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, reviewRequest(initial))).Want(http.StatusConflict)
}

func TestIssueDeliveryConcurrentDecisions(t *testing.T) {
	issue, _, _ := deliveryFixture(t)
	snapshot := readDelivery(t, issue)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := range 2 {
		req := reviewRequest(snapshot)
		if i == 1 {
			req.Decision, req.Feedback = "changes_requested", "The empty state still has no link."
			req.Assessments[0].Passed = false
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, req)).Code
		}()
	}
	wg.Wait()
	close(results)
	codes := []int{}
	for code := range results {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	if codes[0] != http.StatusCreated || codes[1] != http.StatusConflict {
		t.Fatalf("two concurrent decisions must have one winner: %v", codes)
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_delivery_review WHERE issue_id=$1`, issue).Scan(&count)
	if count != 1 {
		t.Fatalf("review history contains %d concurrent winners", count)
	}
}

func TestIssueDeliveryPRHeadAndCleanup(t *testing.T) {
	issue, _, _ := deliveryFixture(t)
	pr := dbfx.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": 1, "repo_owner": "fixture", "repo_name": uuid.NewString(),
		"pr_number": 1, "title": "Fix empty state", "state": "open", "html_url": "https://github.com/fixture/app/pull/1",
		"pr_created_at": time.Now(), "pr_updated_at": time.Now(), "head_sha": "head-one",
	})
	dbfx.InsertNoID(t, "issue_pull_request", testutil.Cols{"issue_id": issue, "pull_request_id": pr}, "issue_id=$1 AND pull_request_id=$2", issue, pr)
	before := readDelivery(t, issue)
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, reviewRequest(before))).Want(http.StatusCreated)
	dbfx.Exec(t, `UPDATE github_pull_request SET head_sha='head-two' WHERE id=$1`, pr)
	after := readDelivery(t, issue)
	if !after.ReviewStale || after.LatestReview.Snapshot.PullRequests[0].HeadSHA != "head-one" || after.PullRequests[0].HeadSHA != "head-two" {
		t.Fatalf("commit provenance is not preserved: %+v", after)
	}
	foreign := dbfx.Workspace(t, "Foreign delivery", "foreign-delivery-"+uuid.NewString())
	if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: parseUUID(issue), WorkspaceID: parseUUID(foreign)}); err != nil {
		t.Fatal(err)
	}
	if got := readDelivery(t, issue); got.LatestReview == nil {
		t.Fatal("foreign deletion removed review")
	}
	if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: parseUUID(issue), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	var count int
	dbfx.QueryRow(t, `SELECT (SELECT count(*) FROM issue_delivery_contract WHERE issue_id=$1) + (SELECT count(*) FROM issue_delivery_review WHERE issue_id=$1)`, issue).Scan(&count)
	if count != 0 {
		t.Fatalf("delivery orphaned after issue deletion: %d", count)
	}
}

func TestIssueDeliveryPrivateChatAndCriteriaBrief(t *testing.T) {
	issueID, agentID, taskID := deliveryFixture(t)
	chatID := dbfx.ChatSession(t, agentID)
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "chat_session_id": chatID, "status": "completed",
		"completed_at": time.Now(), "created_at": time.Now().Add(time.Second), "result": `{"summary":"Private chat content"}`})
	if got := readDelivery(t, issueID); got.Run == nil || got.Run.ID != taskID {
		t.Fatal("a private chat replaced the public delivery result")
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	brief, err := testHandler.deliveryCriteriaBrief(context.Background(), issue)
	if err != nil || !strings.Contains(brief, "revision 1") || !strings.Contains(brief, "An empty inbox explains") {
		t.Fatalf("criteria absent from briefing: %q, %v", brief, err)
	}
	issue.WorkspaceID = parseUUID(uuid.NewString())
	if brief, err := testHandler.deliveryCriteriaBrief(context.Background(), issue); err != nil || brief != "" {
		t.Fatalf("criteria escaped workspace: %q, %v", brief, err)
	}
}

func TestIssueDeliveryClaimCarriesCriteria(t *testing.T) {
	issueID, _, _ := deliveryFixture(t)
	runtimeID := dbfx.Runtime(t, "Delivery claim fixture")
	agentID := dbfx.Agent(t, "delivery-claim-"+uuid.NewString(), runtimeID, testutil.Cols{"instructions": "Preserve existing instructions."})
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "delivery-claim")
	var resp struct {
		Task *struct {
			Agent *TaskAgentData `json:"agent"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil || resp.Task.Agent == nil || !strings.Contains(resp.Task.Agent.Instructions, "Preserve existing instructions.") ||
		!strings.Contains(resp.Task.Agent.Instructions, "An empty inbox explains") {
		t.Fatalf("claim lost instructions or delivery criteria: %+v", resp.Task)
	}
}

func correctionFixture(t *testing.T) (issueID, agentID, sourceID, runtimeID string, review DeliveryReview) {
	t.Helper()
	issueID, agentID, sourceID = deliveryFixture(t)
	runtimeID = dbfx.Runtime(t, "Delivery correction fixture")
	dbfx.Exec(t, `UPDATE agent SET runtime_id=$2, owner_id=$3 WHERE id=$1`, agentID, runtimeID, testUserID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_id=$2, session_id='correction-source-session', work_dir='/tmp/correction-source-workdir' WHERE id=$1`, sourceID, runtimeID)
	req := reviewRequest(readDelivery(t, issueID))
	req.Decision, req.Feedback = "changes_requested", "Add a working link to the empty state."
	req.Assessments[0] = DeliveryAssessment{Passed: false, Evidence: "The label is visible but cannot be clicked."}
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issueID, req)).Want(http.StatusCreated).JSON(&review)
	return
}

func TestIssueDeliveryCorrectionConcurrentRetryAndClaim(t *testing.T) {
	issueID, _, sourceID, runtimeID, review := correctionFixture(t)
	body := map[string]string{"review_id": review.ID}
	type receipt struct {
		ReviewID string `json:"review_id"`
		TaskID   string `json:"task_id"`
	}
	var receipts [2]receipt
	var codes [2]int
	var wg sync.WaitGroup
	for i := range receipts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body))
			codes[i] = response.Code
			response.JSON(&receipts[i])
		}()
	}
	wg.Wait()
	sort.Ints(codes[:])
	if codes != [2]int{http.StatusOK, http.StatusCreated} || receipts[0].TaskID == "" || receipts[0] != receipts[1] {
		t.Fatalf("correction must have one durable winner: %v %+v", codes, receipts)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(receipts[0].TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if uuidToString(task.RerunOfTaskID) != sourceID || uuidToString(task.OriginatorUserID) != testUserID ||
		uuidToString(task.AccountableUserID) != testUserID || task.OriginatorSource.String != "direct_human" || !task.ForceFreshSession {
		t.Fatalf("lost source lineage or invoking human: %+v", task)
	}
	current := readDelivery(t, issueID)
	if current.LatestReview.CorrectionTaskID == nil || *current.LatestReview.CorrectionTaskID != receipts[0].TaskID || !current.ReviewStale {
		t.Fatalf("the correction must require a new review: %+v", current)
	}
	// A fresh handler with only the same database can recover a lost response.
	// No process-local idempotency map or live TaskService is involved.
	restarted := &Handler{Queries: db.New(testPool), TxStarter: testPool}
	var recovered receipt
	testutil.Call(t, restarted.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusOK).JSON(&recovered)
	if recovered != receipts[0] {
		t.Fatalf("restart changed receipt: %+v", recovered)
	}
	var claimed struct {
		Task *daemon.Task `json:"task"`
	}
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "delivery-correction")
	testutil.Call(t, testHandler.ClaimTaskByRuntime, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claimed)
	if claimed.Task == nil || claimed.Task.ID != receipts[0].TaskID || claimed.Task.PriorSessionID != "correction-source-session" || claimed.Task.PriorWorkDir != "/tmp/correction-source-workdir" {
		t.Fatalf("correction did not claim the reviewed run's continuation: %+v", claimed.Task)
	}
	if prompt := daemon.BuildPrompt(*claimed.Task, "claude"); !strings.Contains(prompt, review.Feedback) || !strings.Contains(prompt, review.Assessments[0].Evidence) {
		t.Fatalf("correction feedback did not reach the actual prompt: %s", prompt)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now(), result='{"summary":"The link now works"}' WHERE id=$1`, receipts[0].TaskID)
	latest := readDelivery(t, issueID)
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issueID, reviewRequest(latest))).Want(http.StatusCreated)
	testutil.Call(t, restarted.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusOK).JSON(&recovered)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issueID).Scan(&count)
	if count != 2 || readDelivery(t, issueID).ReviewStale {
		t.Fatalf("correction loop did not finish with one accepted successor; count=%d", count)
	}
}

func TestIssueDeliveryCorrectionAdmissionAndFreshness(t *testing.T) {
	issueID, agentID, _, _, review := correctionFixture(t)
	body := map[string]string{"review_id": review.ID}
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := deliveryRequest("POST", issueID, body)
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.StartIssueDeliveryCorrection, req).Want(http.StatusForbidden)
	}
	outsider := dbfx.User(t, "Correction outsider", uuid.NewString()+"@example.test")
	req := deliveryRequest("POST", issueID, body)
	req.Header.Set("X-User-ID", outsider)
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, req).Want(http.StatusNotFound)
	otherIssue := dbfx.Issue(t, "Unrelated delivery")
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", otherIssue, body)).Want(http.StatusNotFound)
	dbfx.Exec(t, `UPDATE agent SET owner_id=$2, permission_mode='private' WHERE id=$1`, agentID, outsider)
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusForbidden)
	dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, agentID, testUserID)
	testutil.Call(t, testHandler.UpdateIssueDeliveryCriteria, deliveryRequest("PUT", issueID, map[string]any{
		"criteria": []string{"The link opens the creation form"}, "expected_revision": 1,
	})).Want(http.StatusOK)
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusConflict)
	if readDelivery(t, issueID).LatestReview.CorrectionTaskID != nil {
		t.Fatal("rejected correction created a run")
	}
}

func TestIssueDeliveryCorrectionRollbackAndPendingWork(t *testing.T) {
	issueID, agentID, _, runtimeID, review := correctionFixture(t)
	body := map[string]string{"review_id": review.ID}
	original := testHandler.TxStarter
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	t.Cleanup(func() { testHandler.TxStarter = original })
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusInternalServerError)
	testHandler.TxStarter = original
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issueID).Scan(&count)
	if count != 1 || readDelivery(t, issueID).LatestReview.CorrectionTaskID != nil {
		t.Fatal("commit failure left a correction task or receipt behind")
	}
	// An older pending task must not be cancelled to make room for a correction.
	pendingID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "created_at": time.Now().Add(-time.Hour)})
	testutil.Call(t, testHandler.StartIssueDeliveryCorrection, deliveryRequest("POST", issueID, body)).Want(http.StatusConflict)
	pending, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(pendingID))
	if err != nil || pending.Status != "queued" || readDelivery(t, issueID).LatestReview.CorrectionTaskID != nil {
		t.Fatal("correction cancelled or consumed unrelated pending work")
	}
}

func TestIssueDeliveryUsageSnapshotSurvivesRepricingAndRetries(t *testing.T) {
	issue, agent, task := deliveryFixture(t)
	older := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "failed", "completed_at": time.Now().Add(-time.Hour), "created_at": time.Now().Add(-2 * time.Hour)})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "fixture", "model": "unmapped", "input_tokens": 1, "cost_usd_ticks": 10_000_000_000})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": older, "provider": "codex", "model": "gpt-5.4-mini", "input_tokens": 1_000_000})
	private := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "chat_session_id": dbfx.ChatSession(t, agent), "status": "completed", "completed_at": time.Now()})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": private, "provider": "fixture", "model": "unmapped", "cost_usd_ticks": 990_000_000_000})
	input := reviewRequest(readDelivery(t, issue))
	original := testHandler.TxStarter
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	t.Cleanup(func() { testHandler.TxStarter = original })
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, input)).Want(http.StatusInternalServerError)
	testHandler.TxStarter = original
	if current := readDelivery(t, issue); current.LatestReview != nil || current.Metrics.ReviewCount != 0 {
		t.Fatal("commit failure left a review or cost snapshot behind")
	}
	var review DeliveryReview
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, input)).Want(http.StatusCreated).JSON(&review)
	if review.UsageSnapshot == nil || review.UsageSnapshot.Status != "estimated" || *review.UsageSnapshot.AvailableUSD != "1.7500000000" || len(review.UsageSnapshot.RunIDs) != 2 || review.ReviewDelaySeconds == nil {
		t.Fatalf("cost snapshot: %+v", review)
	}
	before, _ := json.Marshal(review.UsageSnapshot)
	dbfx.Exec(t, `UPDATE task_usage SET cost_usd_ticks=990000000000 WHERE task_id=$1`, task)
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, input)).Want(http.StatusOK).JSON(&review)
	after, _ := json.Marshal(review.UsageSnapshot)
	if string(before) != string(after) {
		t.Fatal("retry repriced saved review")
	}
	current := readDelivery(t, issue)
	if current.Metrics.AcceptedResults != 1 || current.Metrics.ReviewCount != 1 {
		t.Fatalf("metrics: %+v", current.Metrics)
	}
	correction := reviewRequest(current)
	correction.Decision = "changes_requested"
	correction.Feedback = "A regression was found"
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, correction)).Want(http.StatusCreated)
	current = readDelivery(t, issue)
	if current.Metrics.AcceptedResults != 0 || current.Metrics.ReviewedResults != 1 || current.Metrics.AcceptanceReversals != 1 {
		t.Fatalf("reopened result: %+v", current.Metrics)
	}
}

func TestIssueDeliveryHumanEffortSeconds(t *testing.T) {
	issue, _, _ := deliveryFixture(t)
	input := reviewRequest(readDelivery(t, issue))
	effort := int64(18)
	input.HumanEffortSeconds = &effort
	var review DeliveryReview
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, input)).Want(http.StatusCreated).JSON(&review)
	if review.HumanEffortSeconds == nil || *review.HumanEffortSeconds != 18 {
		t.Fatalf("human effort: %+v", review.HumanEffortSeconds)
	}
	// Idempotent retry without effort still returns the saved value (effort excluded from hash).
	input.HumanEffortSeconds = nil
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, input)).Want(http.StatusOK).JSON(&review)
	if review.HumanEffortSeconds == nil || *review.HumanEffortSeconds != 18 {
		t.Fatalf("retry lost human effort: %+v", review.HumanEffortSeconds)
	}
	bad := reviewRequest(readDelivery(t, issue))
	tooLarge := int64(24*60*60 + 1)
	bad.HumanEffortSeconds = &tooLarge
	testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, bad)).Want(http.StatusBadRequest)
}

func TestIssueDeliveryHistoryPaginationAndTenantBoundary(t *testing.T) {
	issue, _, _ := deliveryFixture(t)
	for i := 0; i < 22; i++ {
		request := reviewRequest(readDelivery(t, issue))
		request.Feedback = fmt.Sprintf("Review %d", i)
		testutil.Call(t, testHandler.ReviewIssueDelivery, deliveryRequest("POST", issue, request)).Want(http.StatusCreated)
	}
	var page struct {
		Reviews      []DeliveryReview `json:"reviews"`
		NextBeforeID *string          `json:"next_before_id"`
	}
	testutil.Call(t, testHandler.ListIssueDeliveryHistory, deliveryRequest("GET", issue, nil)).Want(http.StatusOK).JSON(&page)
	if len(page.Reviews) != 20 || page.NextBeforeID == nil {
		t.Fatalf("first page %+v", page)
	}
	cursor := *page.NextBeforeID
	req := deliveryRequest("GET", issue, nil)
	req.URL.RawQuery = "before_id=" + cursor
	testutil.Call(t, testHandler.ListIssueDeliveryHistory, req).Want(http.StatusOK).JSON(&page)
	if len(page.Reviews) != 2 || page.NextBeforeID != nil || page.Reviews[1].Feedback != "Review 0" {
		t.Fatalf("tail %+v", page)
	}
	current := readDelivery(t, issue)
	if current.Metrics.ReviewCount != 22 || current.Metrics.AcceptedResults != 1 {
		t.Fatalf("repeated acceptance counted twice: %+v", current.Metrics)
	}
	outsider := dbfx.User(t, "History outsider", uuid.NewString()+"@example.test")
	req = deliveryRequest("GET", issue, nil)
	req.Header.Set("X-User-ID", outsider)
	testutil.Call(t, testHandler.ListIssueDeliveryHistory, req).Want(http.StatusNotFound)
	other, _, _ := deliveryFixture(t)
	req = deliveryRequest("GET", other, nil)
	req.URL.RawQuery = "before_id=" + cursor
	testutil.Call(t, testHandler.ListIssueDeliveryHistory, req).Want(http.StatusNotFound)
}
