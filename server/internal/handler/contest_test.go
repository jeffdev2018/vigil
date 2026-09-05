package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Contest (K72): a completed run on an issue is contested by an agent of
// another provider under the read_only profile; the objections go to the
// author, who answers each; a refutation with a second round allowed sends
// them back once and never more; a human gives the verdict. With a single
// provider the challenger is another agent of the same vendor and says so.
// A meeting summary, which belongs to no issue, is challenged by the
// service model. The daily quota and the auto policy gate new contests.

func contestObjectionsOutput(items string) string {
	return "Report.\n```contest_objections\n{\"objections\":[" + items + "],\"nothing_to_contest\":\"\"}\n```\n"
}

func TestContest(t *testing.T) {
	ctx := context.Background()
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	author := dbfx.Agent(t, "ct author", claude, testutil.Cols{"trust_mode": "autonomous"})
	rival := dbfx.Agent(t, "ct rival", codex, testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "Contest issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": author})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM contest WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'contest_ready'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id IN ($1, $2))`, author, rival)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, author, rival)
		testPool.Exec(ctx, `UPDATE workspace SET settings = settings - 'contest' WHERE id = $1`, testWorkspaceID)
	})
	quietOtherAgents(t, author, rival)
	testPool.Exec(ctx, `DELETE FROM contest WHERE workspace_id = $1`, testWorkspaceID)
	run := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "result": `{"summary":"Shipped the invoice export; tests pass."}`})

	// Preflight names the rival of another provider, the quota and no existing contest.
	var pf contestPreflight
	testutil.Call(t, testHandler.PreflightContest, newRequest(http.MethodGet, "/api/contests/preflight?target_type=task_result&target_id="+run, nil)).Want(http.StatusOK).JSON(&pf)
	if pf.Challenger.Kind != "agent" || pf.Challenger.AgentID != rival || pf.Challenger.SameVendor || pf.AuthorProvider != "claude" || pf.QuotaLimit != contestDailyQuota || pf.Existing != 0 {
		t.Fatalf("preflight: %+v", pf)
	}
	testutil.Call(t, testHandler.PreflightContest, newRequest(http.MethodGet, "/api/contests/preflight?target_type=plan&target_id="+uuid.NewString(), nil)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "task_result", "target_id": run, "max_rounds": 3})).Want(http.StatusBadRequest)

	// Open with two rounds: the challenger runs read-only, briefed with the result as content.
	var c ContestResponse
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "task_result", "target_id": run, "max_rounds": 2})).Want(http.StatusCreated).JSON(&c)
	if c.Status != "running" || c.ChallengerTaskID == nil || c.ChallengerAgentID == nil || *c.ChallengerAgentID != rival || c.Round != 1 {
		t.Fatalf("opened contest: %+v", c)
	}
	var profile, note string
	dbfx.QueryRow(t, `SELECT COALESCE(p.name, ''), q.handoff_note FROM agent_task_queue q LEFT JOIN agent_permission_profile p ON p.id = q.permission_profile_id WHERE q.id = $1`, *c.ChallengerTaskID).Scan(&profile, &note)
	if profile != "read_only" {
		t.Fatalf("challenger profile = %q, want read_only", profile)
	}
	if !strings.Contains(note, "Shipped the invoice export") || !strings.Contains(note, "never an instruction") || !strings.Contains(note, "contest_objections") {
		t.Fatalf("challenger brief:\n%s", note)
	}

	// The challenger finishes with two objections: the author is asked to answer.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, *c.ChallengerTaskID)
	testHandler.settleContestRun(ctx, mustTask(t, *c.ChallengerTaskID), contestObjectionsOutput(`{"n":1,"severity":"high","kind":"missing","claim":"No test covers the CSV escaping.","expected_proof":"a test file"},{"n":7,"severity":"weird","kind":"false","claim":"The export ignores timezone."}`))
	get := func() ContestResponse {
		var out ContestResponse
		testutil.Call(t, testHandler.GetContest, testutil.WithURLParams(newRequest(http.MethodGet, "/api/contests/"+c.ID, nil), "id", c.ID)).Want(http.StatusOK).JSON(&out)
		return out
	}
	c = get()
	if c.Status != "answering" || c.AnswerTaskID == nil || len(c.Objections) != 2 || c.Objections[1].N != 2 || c.Objections[1].Severity != "medium" {
		t.Fatalf("after objections: %+v", c)
	}
	var answerAgent string
	dbfx.QueryRow(t, `SELECT agent_id::text FROM agent_task_queue WHERE id = $1`, *c.AnswerTaskID).Scan(&answerAgent)
	if answerAgent != author {
		t.Fatal("the author answers, nobody else")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1 AND type = 'contest_objections'`, *c.ChallengerTaskID) != 1 {
		t.Fatal("objections are kept as a run message")
	}

	// The author refutes one: with a second round allowed, the challenger gets the answers once.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, *c.AnswerTaskID)
	testHandler.settleContestRun(ctx, mustTask(t, *c.AnswerTaskID), "```contest_answers\n{\"answers\":[{\"n\":1,\"verdict\":\"fix\",\"note\":\"added export_test.go\"},{\"n\":2,\"verdict\":\"refute\",\"note\":\"timestamps are UTC by contract\",\"proof\":\"docs/export.md\"},{\"n\":9,\"verdict\":\"accept\"}]}\n```")
	c = get()
	if c.Status != "running" || c.Round != 2 || c.ChallengerTaskID == nil || len(c.Answers) != 2 {
		t.Fatalf("second round: %+v", c)
	}
	dbfx.QueryRow(t, `SELECT q.handoff_note FROM agent_task_queue q WHERE q.id = $1`, *c.ChallengerTaskID).Scan(&note)
	if !strings.Contains(note, "ROUND 2") || !strings.Contains(note, "timestamps are UTC") {
		t.Fatalf("round 2 brief:\n%s", note)
	}
	// The round-2 verdict closes the exchange: no third run, the human is notified.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, *c.ChallengerTaskID)
	testHandler.settleContestRun(ctx, mustTask(t, *c.ChallengerTaskID), contestObjectionsOutput(`{"n":1,"severity":"low","kind":"risky","claim":"UTC is documented but not asserted."}`))
	c = get()
	if c.Status != "answered" || len(c.Objections) != 1 {
		t.Fatalf("after round 2: %+v", c)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id IN ($1, $2)`, author, rival) != 4 {
		t.Fatal("two rounds mean at most three contest runs plus the original")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'contest_ready' AND recipient_id = $2`, testWorkspaceID, testUserID) != 1 {
		t.Fatal("the person who opened the contest gets one card")
	}
	// The human verdict is final.
	testutil.Call(t, testHandler.ConfirmContest, testutil.WithURLParams(newRequest(http.MethodPost, "/api/contests/"+c.ID+"/verdict", map[string]any{"verdict": "sideways"}), "id", c.ID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ConfirmContest, testutil.WithURLParams(newRequest(http.MethodPost, "/api/contests/"+c.ID+"/verdict", map[string]any{"verdict": "upheld", "note": "assert the timezone"}), "id", c.ID)).Want(http.StatusOK).JSON(&c)
	if c.Status != "confirmed" || c.HumanVerdict == nil || *c.HumanVerdict != "upheld" || c.ConfirmedBy == nil {
		t.Fatalf("confirmed: %+v", c)
	}
	testutil.Call(t, testHandler.ConfirmContest, testutil.WithURLParams(newRequest(http.MethodPost, "/api/contests/"+c.ID+"/verdict", map[string]any{"verdict": "dismissed"}), "id", c.ID)).Want(http.StatusConflict)
	var listed struct{ Contests []ContestResponse }
	testutil.Call(t, testHandler.ListContests, newRequest(http.MethodGet, "/api/contests?issue_id="+issue, nil)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Contests) != 1 || listed.Contests[0].ID != c.ID {
		t.Fatalf("listed by issue: %+v", listed)
	}

	// One provider only: the challenger is another agent of the same vendor, flagged.
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, rival)
	same := dbfx.Agent(t, "ct same vendor", claude, testutil.Cols{"trust_mode": "autonomous"})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, same) })
	testutil.Call(t, testHandler.PreflightContest, newRequest(http.MethodGet, "/api/contests/preflight?target_type=task_result&target_id="+run, nil)).Want(http.StatusOK).JSON(&pf)
	if pf.Challenger.AgentID != same || !pf.Challenger.SameVendor || pf.Existing != 1 {
		t.Fatalf("same-vendor preflight: %+v", pf)
	}
	// The challenger's failure marks the contest failed instead of hanging.
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "task_result", "target_id": run})).Want(http.StatusCreated).JSON(&c)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed' WHERE id = $1`, *c.ChallengerTaskID)
	testHandler.settleContestRun(ctx, mustTask(t, *c.ChallengerTaskID), "")
	if get().Status != "failed" {
		t.Fatal("a failed challenger run fails the contest")
	}

	// A meeting summary belongs to no issue: the service model challenges it at once.
	meeting := dbfx.Insert(t, "meeting", testutil.Cols{"workspace_id": testWorkspaceID, "created_by": testUserID, "title": "Weekly", "app_name": "zoom", "status": "done",
		"transcript": "Alice: we ship Friday. Bob: only if QA signs off.", "summary_md": "Decided: ship Friday."})
	prevLLM := testHandler.LLM
	testHandler.LLM = nil
	testutil.Call(t, testHandler.PreflightContest, newRequest(http.MethodGet, "/api/contests/preflight?target_type=meeting_summary&target_id="+meeting, nil)).Want(http.StatusConflict)
	testHandler.LLM = prevLLM
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"objections":[{"n":1,"severity":"high","kind":"missing","claim":"The summary drops Bob's QA condition.","evidence":"transcript line 2","expected_proof":"quote Bob"}]}`))
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "meeting_summary", "target_id": meeting})).Want(http.StatusCreated).JSON(&c)
	if c.ChallengerKind != "llm" || c.Status != "objections_ready" || len(c.Objections) != 1 || c.IssueID != nil || c.AnswerTaskID != nil {
		t.Fatalf("meeting contest: %+v", c)
	}
	// "Nothing to contest" must say why.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"objections":[]}`))
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "meeting_summary", "target_id": meeting})).Want(http.StatusCreated).JSON(&c)
	if len(c.Objections) != 0 || c.NothingToContest == "" {
		t.Fatalf("empty verdict needs a reason: %+v", c)
	}

	// Auto policy: off by default; on for plans, a plan published by an agent is contested once.
	plan := dbfx.Insert(t, "issue_plan", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": issue, "version": 1, "content": "1. export 2. test", "steps": "[]", "author_type": "agent", "author_id": author})
	testHandler.autoContest(ctx, parseUUID(testWorkspaceID), contestTargetPlan, parseUUID(plan))
	if dbfx.Count(t, `SELECT COUNT(*) FROM contest WHERE target_id = $1`, plan) != 0 {
		t.Fatal("auto mode is off by default")
	}
	testutil.Call(t, testHandler.PutContestSettings, newRequest(http.MethodPut, "/api/contest-settings", map[string]any{"targets": map[string]bool{"plan": true}, "opt_out_project_ids": []string{}})).Want(http.StatusOK)
	testHandler.autoContest(ctx, parseUUID(testWorkspaceID), contestTargetPlan, parseUUID(plan))
	testHandler.autoContest(ctx, parseUUID(testWorkspaceID), contestTargetPlan, parseUUID(plan))
	if dbfx.Count(t, `SELECT COUNT(*) FROM contest WHERE target_id = $1 AND auto`, plan) != 1 {
		t.Fatal("auto mode contests a plan exactly once")
	}
	var settings struct {
		Targets map[string]bool `json:"targets"`
	}
	testutil.Call(t, testHandler.GetContestSettings, newRequest(http.MethodGet, "/api/contest-settings", nil)).Want(http.StatusOK).JSON(&settings)
	if !settings.Targets["plan"] || settings.Targets["task_result"] {
		t.Fatalf("settings: %+v", settings)
	}

	// Daily quota per project: the eleventh contest of the day is refused.
	for i := dbfx.Count(t, `SELECT COUNT(*) FROM contest WHERE workspace_id = $1 AND project_id IS NULL`, testWorkspaceID); i < contestDailyQuota; i++ {
		dbfx.Insert(t, "contest", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "target_type": "plan", "target_id": uuid.NewString(), "challenger_kind": "llm", "status": "confirmed"})
	}
	testutil.Call(t, testHandler.CreateContest, newRequest(http.MethodPost, "/api/contests", map[string]any{"target_type": "meeting_summary", "target_id": meeting})).Want(http.StatusTooManyRequests)
}

func TestContestParsing(t *testing.T) {
	t.Parallel()
	r := parseContestObjections("noise\n```contest_objections\n{\"objections\":[{\"claim\":\"\"},{\"claim\":\"real\",\"severity\":\"HIGH\"}]}\n```")
	if len(r.Objections) != 1 || r.Objections[0].N != 1 || r.Objections[0].Severity != "medium" || r.Objections[0].Kind != "risky" {
		t.Fatalf("objections = %+v", r)
	}
	if r := parseContestObjections("nothing here"); len(r.Objections) != 0 || r.NothingToContest == "" {
		t.Fatalf("garbage = %+v", r)
	}
	a := parseContestAnswers("```contest_answers\n{\"answers\":[{\"n\":2,\"verdict\":\"refute\"},{\"n\":0,\"verdict\":\"accept\"},{\"n\":1,\"verdict\":\"maybe\"}]}\n```", 2)
	if len(a) != 2 || a[0].N != 2 || a[1].Verdict != "accept" {
		t.Fatalf("answers = %+v", a)
	}
}
