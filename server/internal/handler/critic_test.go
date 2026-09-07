package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Adversarial critic (F25 / JEF-18), HTTP + hook surface.
//
// The decision matrix (rounds, cost, degradation, verdict normalization) is
// the gate's and lives in internal/service/critic_gate_test.go. This file
// covers what only a handler can: that a delivery is held and a critic
// enqueued, that a block relaunches the author with the critic's own words,
// that a member is never held, and the wire shape of the policy and verdict
// routes.

type criticFixture struct {
	authorRuntime string
	criticRuntime string
	author        string
	critic        string
	issue         string
}

// newCriticFixture builds an author and a critic on two different providers,
// with every other agent of the workspace archived so the policy's named
// critic is the only one in play.
func newCriticFixture(t *testing.T) criticFixture {
	t.Helper()
	authorRuntime := providerRuntime(t, "f25-author")
	criticRuntime := providerRuntime(t, "f25-critic")
	// Autonomous trust: the F25 hold is the gate under test, and a propose-mode
	// agent would be refused by the Trust Dial before it ever reaches it.
	author := dbfx.Agent(t, "f25 author", authorRuntime, testutil.Cols{"trust_mode": "autonomous"})
	critic := dbfx.Agent(t, "f25 critic", criticRuntime)
	issue := dbfx.Issue(t, "F25 delivery", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": author,
	})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_critic_verdict WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE issue_id = $1)`, issue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM agent_critic_policy WHERE workspace_id = $1`, testWorkspaceID)
	})
	quietOtherAgents(t, author, critic)
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{diff: "--- a\n+++ b\n"}
	t.Cleanup(func() { testHandler.DiffFetcher = prev })
	return criticFixture{authorRuntime, criticRuntime, author, critic, issue}
}

func (f criticFixture) policy(t *testing.T, over testutil.Cols) {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id": testWorkspaceID, "subject_type": "agent", "subject_id": f.author,
		"enabled": true, "critic_agent_id": f.critic, "phases": testutil.Raw("ARRAY['change']::text[]"),
	}
	for k, v := range over {
		cols[k] = v
	}
	dbfx.Insert(t, "agent_critic_policy", cols)
}

// delivery inserts a completed author run on the fixture's issue.
func (f criticFixture) delivery(t *testing.T, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{
		"runtime_id": f.authorRuntime, "issue_id": f.issue,
		"status": "completed", "completed_at": testutil.Raw("now()"),
		"touched_paths": testutil.Raw(`'["src/a.py"]'::jsonb`),
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Task(t, f.author, cols)
}

// criticRunFor returns the critic run queued for a delivery, if any.
func criticRunFor(t *testing.T, issueID string) (string, bool) {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT id FROM agent_task_queue WHERE issue_id = $1 AND context ->> 'critic_of_task_id' IS NOT NULL ORDER BY created_at DESC LIMIT 1`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", false
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, true
}

type verdictRow struct {
	Verdict string
	Reason  string
	Round   int
	Summary string
}

func latestVerdict(t *testing.T, issueID string) (verdictRow, bool) {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT verdict, COALESCE(reason, ''), round, COALESCE(summary, '') FROM agent_critic_verdict WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return verdictRow{}, false
	}
	var v verdictRow
	if err := rows.Scan(&v.Verdict, &v.Reason, &v.Round, &v.Summary); err != nil {
		t.Fatal(err)
	}
	return v, true
}

// Acceptance 1: no policy, nothing changes.
func TestCriticWithoutPolicyLeavesCompletionUnchanged(t *testing.T) {
	f := newCriticFixture(t)
	task := f.delivery(t)

	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "", "")

	if _, ok := criticRunFor(t, f.issue); ok {
		t.Error("no policy must not queue a critic run")
	}
	if _, ok := latestVerdict(t, f.issue); ok {
		t.Error("no policy must not record a verdict")
	}
	if got := issueStatus(t, f.issue); got != "in_progress" {
		t.Errorf("status = %q, want the completion to have left it alone", got)
	}
}

// Acceptance 2: a blocking policy queues the critic and parks the delivery.
func TestCriticBlockingPolicyHoldsIssueAndQueuesCritic(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})
	task := f.delivery(t)

	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/1", "")

	runID, ok := criticRunFor(t, f.issue)
	if !ok {
		t.Fatal("a blocking policy must queue a critic run")
	}
	run := mustTask(t, runID)
	if uuidToString(run.AgentID) != f.critic {
		t.Errorf("critic run agent = %s, want the policy's critic %s", uuidToString(run.AgentID), f.critic)
	}
	if run.LegRole != service.LegRoleCritique {
		t.Errorf("leg_role = %q, want critique so the critique never counts as a sample of the critic's task class", run.LegRole)
	}
	stamp, isCritic := service.TaskCriticOf(run.Context)
	if !isCritic || stamp.OfTaskID != task || stamp.Round != 1 {
		t.Errorf("critic stamp = %+v, want round 1 of %s", stamp, task)
	}
	if got := issueStatus(t, f.issue); got != "in_review" {
		t.Errorf("status = %q, want in_review while the critic reads the change", got)
	}
	// The brief has to carry the verdict contract, or the round is wasted.
	if !strings.Contains(run.HandoffNote.String, "multica review verdict") {
		t.Errorf("brief does not tell the critic how to answer: %q", run.HandoffNote.String)
	}
}

// A non-blocking policy still gets its second opinion; it just does not park
// the issue. That split is the whole reason blocking defaults to false.
func TestCriticNonBlockingPolicyQueuesWithoutHolding(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, nil)
	task := f.delivery(t)

	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/2", "")

	if _, ok := criticRunFor(t, f.issue); !ok {
		t.Fatal("a non-blocking policy still runs its critic")
	}
	if got := issueStatus(t, f.issue); got != "in_progress" {
		t.Errorf("status = %q, want the issue left where the author put it", got)
	}
}

// Acceptance 5: no critic on another provider degrades to pass, never a hold.
func TestCriticWithoutDistinctProviderPassesDegraded(t *testing.T) {
	f := newCriticFixture(t)
	sameProvider := dbfx.Agent(t, "f25 twin", f.authorRuntime)
	dbfx.Exec(t, `UPDATE agent SET archived_at = NULL WHERE id = $1`, sameProvider)
	f.policy(t, testutil.Cols{"blocking": true, "critic_agent_id": sameProvider})
	task := f.delivery(t)

	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/3", "")

	if _, ok := criticRunFor(t, f.issue); ok {
		t.Error("a policy with no distinct-provider critic must not queue a run")
	}
	v, ok := latestVerdict(t, f.issue)
	// An absent reviewer is not a reviewer who approved. It records the same
	// verdict as every other case where the platform has no assessment, and
	// `concerns` costs nothing: only `block` relaunches the author.
	if !ok || v.Verdict != service.CriticVerdictConcerns || v.Reason != service.CriticReasonNoDistinctProvider {
		t.Fatalf("verdict = %+v, want a degraded concerns naming no_distinct_provider", v)
	}
	if got := issueStatus(t, f.issue); got != "in_progress" {
		t.Errorf("status = %q: a policy that cannot be honoured must never hold the issue", got)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = $2`, f.issue, InboxTypeCriticDegraded); n == 0 {
		t.Error("a silently skipped policy must tell the humans")
	}
}

// Acceptance 6: past the round cap the loop stops with concerns, not silence.
func TestCriticStopsOnMaxRounds(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true, "max_rounds": 1})
	first := f.delivery(t)
	dbfx.Insert(t, "agent_critic_verdict", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": f.issue, "subject_task_id": first,
		"verdict": "block", "round": 1, "phase": "change",
	})
	second := f.delivery(t)

	testHandler.triggerCriticReview(context.Background(), mustTask(t, second), "https://github.com/org/repo/pull/4", "")

	if _, ok := criticRunFor(t, f.issue); ok {
		t.Error("past max_rounds no further critic may be queued")
	}
	v, _ := latestVerdict(t, f.issue)
	if v.Verdict != service.CriticVerdictConcerns || v.Reason != service.CriticReasonMaxRounds {
		t.Fatalf("verdict = %+v, want concerns / max_rounds", v)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = $2`, f.issue, InboxTypeCriticBudget); n == 0 {
		t.Error("a loop that stopped on its budget must tell the humans")
	}
}

// A cost past the cap stops the loop the same way the round cap does.
func TestCriticStopsOnMaxCost(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true, "max_rounds": 3, "max_cost_usd_ticks": 10})
	task := f.delivery(t)
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id": task, "provider": "f25", "model": "m", "input_tokens": 1, "output_tokens": 1, "cost_usd_ticks": 5000,
	})

	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/5", "")

	if _, ok := criticRunFor(t, f.issue); ok {
		t.Error("past max_cost no critic may be queued")
	}
	v, _ := latestVerdict(t, f.issue)
	if v.Verdict != service.CriticVerdictConcerns || v.Reason != service.CriticReasonMaxCost {
		t.Fatalf("verdict = %+v, want concerns / max_cost", v)
	}
}

// A chat run and a judging leg are not deliveries: criticising them would
// either critique a conversation or start the loop this feature bounds.
func TestCriticIgnoresNonDeliveryRuns(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})

	review := f.delivery(t, testutil.Cols{"leg_role": service.LegRoleReview})
	testHandler.triggerCriticReview(context.Background(), mustTask(t, review), "https://github.com/org/repo/pull/6", "")
	if _, ok := criticRunFor(t, f.issue); ok {
		t.Error("a review leg is not a delivery")
	}

	// A revision IS a delivery: without it, `block` could never produce a
	// second round.
	revision := f.delivery(t, testutil.Cols{"leg_role": service.LegRoleRevision})
	testHandler.triggerCriticReview(context.Background(), mustTask(t, revision), "https://github.com/org/repo/pull/7", "")
	if _, ok := criticRunFor(t, f.issue); !ok {
		t.Error("a revision leg is a delivery and must be criticised")
	}
}

// Acceptance 3: block relaunches the author, carrying the critic's own words.
func TestCriticBlockRelaunchesAuthorWithSummary(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true, "max_rounds": 3})
	task := f.delivery(t)
	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/8", "")
	runID, ok := criticRunFor(t, f.issue)
	if !ok {
		t.Fatal("expected a critic run")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, runID)

	testHandler.settleCriticRun(context.Background(), mustTask(t, runID),
		"Read the diff.\n\n```critic_verdict\n{\"verdict\":\"block\",\"summary\":\"the retry path drops the error\",\"findings\":[{\"severity\":\"bug\",\"file\":\"src/a.py\",\"line\":7,\"title\":\"swallowed error\"}]}\n```")

	v, _ := latestVerdict(t, f.issue)
	if v.Verdict != service.CriticVerdictBlock || v.Summary != "the retry path drops the error" {
		t.Fatalf("verdict = %+v, want the critic's block", v)
	}
	var note string
	dbfx.QueryRow(t, `SELECT COALESCE(handoff_note, '') FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'pending') ORDER BY created_at DESC LIMIT 1`, f.issue, f.author).Scan(&note)
	if !strings.Contains(note, "the retry path drops the error") {
		t.Errorf("relaunch note = %q, want the critic's summary", note)
	}
	if !strings.Contains(note, "swallowed error") {
		t.Errorf("relaunch note = %q, want the critic's findings", note)
	}
}

// Acceptance 4: pass finalises — nothing is relaunched.
func TestCriticPassFinalizesWithoutRelaunch(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})
	task := f.delivery(t)
	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/9", "")
	runID, _ := criticRunFor(t, f.issue)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, runID)

	testHandler.settleCriticRun(context.Background(), mustTask(t, runID),
		"```critic_verdict\n{\"verdict\":\"pass\",\"summary\":\"reads fine\"}\n```")

	v, _ := latestVerdict(t, f.issue)
	if v.Verdict != service.CriticVerdictPass {
		t.Fatalf("verdict = %+v, want pass", v)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'pending')`, f.issue, f.author); n != 0 {
		t.Error("a pass must not relaunch the author")
	}
}

// Acceptance 8 at the boundary: a critic that ended without a readable answer
// is `concerns` with the reason said out loud — never a pass, never a block.
func TestCriticRunWithoutVerdictRecordsConcerns(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})
	task := f.delivery(t)
	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/10", "")
	runID, _ := criticRunFor(t, f.issue)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, runID)

	testHandler.settleCriticRun(context.Background(), mustTask(t, runID), "I read it and it seemed OK to me.")

	v, _ := latestVerdict(t, f.issue)
	if v.Verdict != service.CriticVerdictConcerns || v.Reason != service.CriticReasonNoVerdict {
		t.Fatalf("verdict = %+v, want concerns / no_verdict", v)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'pending')`, f.issue, f.author); n != 0 {
		t.Error("silence must not relaunch the author")
	}
}

// An invented fourth verdict word is `concerns` too, on the fence path.
func TestCriticUnknownFenceVerdictReadsAsConcerns(t *testing.T) {
	got := parseCriticVerdictFence("```critic_verdict\n{\"verdict\":\"reject\",\"summary\":\"nope\"}\n```")
	if got.Verdict != service.CriticVerdictConcerns {
		t.Fatalf("verdict = %q, want concerns", got.Verdict)
	}
	if got.Summary != "nope" {
		t.Errorf("summary = %q; an unknown verdict must not throw away what the critic wrote", got.Summary)
	}
}

// Acceptance 7: the hold governs agents, never people.
func TestCriticHoldRefusesAgentDoneButNotMember(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})
	task := f.delivery(t)
	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/11", "")
	runID, ok := criticRunFor(t, f.issue)
	if !ok {
		t.Fatal("expected a critic run")
	}

	agentReq := testutil.WithHeaders(newRequest("PUT", "/api/issues/"+f.issue, map[string]any{"status": "done"}),
		"X-Actor-Source", "task_token", "X-Agent-ID", f.author, "X-Task-ID", task)
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(agentReq, "id", f.issue)).Want(http.StatusConflict)
	if got := issueStatus(t, f.issue); got == "done" {
		t.Fatal("the refused move must leave the issue where it was")
	}

	// The member is the escape hatch, and it must not need a flag.
	memberReq := newRequest("PUT", "/api/issues/"+f.issue, map[string]any{"status": "done"})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(memberReq, "id", f.issue)).Want(http.StatusOK)
	if got := issueStatus(t, f.issue); got != "done" {
		t.Fatalf("status = %q, want a member able to force-complete a held issue", got)
	}

	// And the hold releases itself: a critic run that finishes stops holding
	// anything, whatever it answered.
	dbfx.Exec(t, `UPDATE issue SET status = 'in_review' WHERE id = $1`, f.issue)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', completed_at = now() WHERE id = $1`, runID)
	again := testutil.WithHeaders(newRequest("PUT", "/api/issues/"+f.issue, map[string]any{"status": "done"}),
		"X-Actor-Source", "task_token", "X-Agent-ID", f.author, "X-Task-ID", task)
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(again, "id", f.issue)).Want(http.StatusOK)
}

// --- HTTP surface ------------------------------------------------------------

func TestCriticPolicyRoute(t *testing.T) {
	f := newCriticFixture(t)
	path := "/api/critic-policies/agent/" + f.author

	// A subject with no row answers the defaults rather than a 404: the UI
	// renders a form, not an error.
	var got CriticPolicyResponse
	testutil.Call(t, testHandler.GetCriticPolicy,
		testutil.WithURLParams(newRequest("GET", path, nil), "subjectType", "agent", "subjectId", f.author)).
		Want(http.StatusOK).JSON(&got)
	if got.Enabled || !got.RequireDistinctProvider || got.MaxRounds != 1 || len(got.Phases) != 1 {
		t.Fatalf("default policy = %+v", got)
	}

	// Enabling without a critic is refused: "pick one for me" is what K15
	// already does, and this feature exists to be the explicit choice.
	res := testutil.Call(t, testHandler.PutCriticPolicy,
		testutil.WithURLParams(newRequest("PUT", path, map[string]any{"enabled": true}), "subjectType", "agent", "subjectId", f.author)).
		Want(http.StatusUnprocessableEntity).Map()
	if res["code"] != ErrCodeCriticRequired {
		t.Errorf("code = %v, want %s", res["code"], ErrCodeCriticRequired)
	}

	// An agent cannot review itself, whatever else the policy says.
	res = testutil.Call(t, testHandler.PutCriticPolicy,
		testutil.WithURLParams(newRequest("PUT", path, map[string]any{"enabled": true, "critic_agent_id": f.author}), "subjectType", "agent", "subjectId", f.author)).
		Want(http.StatusUnprocessableEntity).Map()
	if res["code"] != ErrCodeCriticIsAuthor {
		t.Errorf("code = %v, want %s", res["code"], ErrCodeCriticIsAuthor)
	}

	testutil.Call(t, testHandler.PutCriticPolicy,
		testutil.WithURLParams(newRequest("PUT", path, map[string]any{
			"enabled": true, "critic_agent_id": f.critic, "blocking": true, "max_rounds": 2,
		}), "subjectType", "agent", "subjectId", f.author)).
		Want(http.StatusOK).JSON(&got)
	if !got.Enabled || !got.Blocking || got.MaxRounds != 2 || got.CriticAgentID == nil || *got.CriticAgentID != f.critic {
		t.Fatalf("saved policy = %+v", got)
	}
	// The upsert is keyed on the subject: saving twice must not create a second
	// row the resolver could pick either of.
	testutil.Call(t, testHandler.PutCriticPolicy,
		testutil.WithURLParams(newRequest("PUT", path, map[string]any{"enabled": false}), "subjectType", "agent", "subjectId", f.author)).
		Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_critic_policy WHERE workspace_id = $1 AND subject_id = $2`, testWorkspaceID, f.author); n != 1 {
		t.Fatalf("policy rows = %d, want exactly one per subject", n)
	}
}

func TestCriticVerdictRouteIsTheCriticRunOnly(t *testing.T) {
	f := newCriticFixture(t)
	f.policy(t, testutil.Cols{"blocking": true})
	task := f.delivery(t)
	testHandler.triggerCriticReview(context.Background(), mustTask(t, task), "https://github.com/org/repo/pull/12", "")
	runID, ok := criticRunFor(t, f.issue)
	if !ok {
		t.Fatal("expected a critic run")
	}

	body := map[string]any{"verdict": "concerns", "summary": "two nits", "findings": []map[string]any{{"severity": "info", "title": "naming"}}}

	// A member holding a session cannot write in the critic's name.
	testutil.Call(t, testHandler.CreateCriticVerdict,
		withURLParam(newRequest("POST", "/api/issues/"+f.issue+"/critic-verdicts", body), "id", f.issue)).
		Want(http.StatusForbidden)

	// Neither can another run of the same issue.
	notCritic := testutil.WithHeaders(newRequest("POST", "/api/issues/"+f.issue+"/critic-verdicts", body),
		"X-Actor-Source", "task_token", "X-Agent-ID", f.author, "X-Task-ID", task)
	testutil.Call(t, testHandler.CreateCriticVerdict, withURLParam(notCritic, "id", f.issue)).
		Want(http.StatusForbidden)

	criticReq := func() *http.Request {
		return withURLParam(testutil.WithHeaders(newRequest("POST", "/api/issues/"+f.issue+"/critic-verdicts", body),
			"X-Actor-Source", "task_token", "X-Agent-ID", f.critic, "X-Task-ID", runID), "id", f.issue)
	}
	var created CriticVerdictResponse
	testutil.Call(t, testHandler.CreateCriticVerdict, criticReq()).Want(http.StatusCreated).JSON(&created)
	if created.Verdict != service.CriticVerdictConcerns || created.Round != 1 || len(created.Findings) != 1 {
		t.Fatalf("created = %+v", created)
	}
	// One verdict per run: a second attempt is a conflict, not a silent
	// overwrite of what the critic already said.
	testutil.Call(t, testHandler.CreateCriticVerdict, criticReq()).Want(http.StatusConflict)

	var listed struct {
		Verdicts []CriticVerdictResponse `json:"verdicts"`
	}
	testutil.Call(t, testHandler.ListCriticVerdicts,
		withURLParam(newRequest("GET", "/api/issues/"+f.issue+"/critic-verdicts", nil), "id", f.issue)).
		Want(http.StatusOK).JSON(&listed)
	if len(listed.Verdicts) != 1 || listed.Verdicts[0].ID != created.ID {
		t.Fatalf("listed = %+v", listed.Verdicts)
	}

	// The posted verdict is what settles the run; the fence is only a fallback.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, runID)
	testHandler.settleCriticRun(context.Background(), mustTask(t, runID),
		"```critic_verdict\n{\"verdict\":\"block\",\"summary\":\"ignored\"}\n```")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_critic_verdict WHERE issue_id = $1`, f.issue); n != 1 {
		t.Fatalf("verdict rows = %d, want the posted one only", n)
	}
}
