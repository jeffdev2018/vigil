package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// Run-group LLM judge (JEF-234 follow-up). POST /api/run-groups/{id}/judge is
// human-only, synchronous, and persists its verdict onto run_group.judgement
// — answered or failed.

// stubJudgeLLM is an enabled-or-not judge seam that never dials an upstream,
// mirroring stubConsultLLM.
type stubJudgeLLM struct {
	enabled bool
	reply   string
	usage   llm.Usage
	err     error
	calls   int
	// gotModel/gotUserPrompt record what the judge pass was given.
	gotModel      *string
	gotUserPrompt *string
}

func (s *stubJudgeLLM) Enabled() bool { return s.enabled }

func (s *stubJudgeLLM) GenerateJSONWithUsage(_ context.Context, model, _, userPrompt string, _ float64, _ int64) (string, llm.Usage, error) {
	s.calls++
	if s.gotModel != nil {
		*s.gotModel = model
	}
	if s.gotUserPrompt != nil {
		*s.gotUserPrompt = userPrompt
	}
	return s.reply, s.usage, s.err
}

func withJudgeLLM(t *testing.T, stub ConsultLLM) {
	t.Helper()
	prev := testHandler.JudgeLLM
	testHandler.JudgeLLM = stub
	t.Cleanup(func() { testHandler.JudgeLLM = prev })
}

// judgeCall posts the judge endpoint as the workspace owner.
func judgeCall(t *testing.T, groupID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.JudgeRunGroup,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+groupID+"/judge", nil), "id", groupID))
}

// judgedRunGroupFixture starts a two-attempt race and completes both attempts
// with recorded diffs and usage, so the judge has real facts to compare.
// Returns (issueID, group, winner-candidate task id, other task id).
func judgedRunGroupFixture(t *testing.T, name string) (string, RunGroupResponse, string, string) {
	t.Helper()
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, name+" agent a", runtime)
	agentB := dbfx.Agent(t, name+" agent b", runtime)
	issue := dbfx.Issue(t, name+" race", testutil.Cols{"description": "the issue body the judge must see"})
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{
		"attempts": []map[string]any{{"agent_id": agentA, "model": "sonnet"}, {"agent_id": agentB, "model": "opus"}},
	}, http.StatusCreated)
	taskA, taskB := group.Attempts[0].TaskID, group.Attempts[1].TaskID

	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = TIMESTAMPTZ '2026-09-01 10:00:00Z', completed_at = TIMESTAMPTZ '2026-09-01 10:02:00Z',
		diff_stat = '{"files":2}'::jsonb, diff_unified = 'patch a' WHERE id = $1`, taskA)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = TIMESTAMPTZ '2026-09-01 10:00:00Z', completed_at = TIMESTAMPTZ '2026-09-01 10:05:00Z',
		diff_stat = '{"files":9}'::jsonb, diff_unified = 'patch b' WHERE id = $1`, taskB)
	dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, updated_at)
		VALUES ($1, 'anthropic', 'sonnet', 10, 20, 0, 0, 5000, now())`, taskA)
	dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, updated_at)
		VALUES ($1, 'anthropic', 'opus', 10, 20, 0, 0, 9000, now())`, taskB)
	return issue, group, taskA, taskB
}

// The whole loop: two completed attempts, the judge answers, the verdict is
// validated against the race's attempts, persisted, and echoed on the group.
func TestRunGroupJudgeAnsweredHappyPath(t *testing.T) {
	_, group, taskA, taskB := judgedRunGroupFixture(t, "judge-happy")

	var gotModel, gotUserPrompt string
	withJudgeLLM(t, &stubJudgeLLM{
		enabled: true,
		reply: `{"winner_task_id":"` + taskA + `","justification":"A is smaller and cheaper",` +
			`"scores":[{"task_id":"` + taskA + `","score":90,"rationale":"tight diff"},{"task_id":"` + taskB + `","score":50,"rationale":"sprawling"}]}`,
		gotModel: &gotModel, gotUserPrompt: &gotUserPrompt,
	})

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	judgeCall(t, group.ID).Want(http.StatusOK).JSON(&out)

	j := out.Group.Judgement
	if j == nil {
		t.Fatalf("judgement missing from response: %+v", out.Group)
	}
	if j.Status != "answered" || j.WinnerTaskID != taskA || j.Justification != "A is smaller and cheaper" {
		t.Fatalf("judgement = %+v, want answered with winner %s", j, taskA)
	}
	if len(j.Scores) != 2 || j.Scores[0].TaskID != taskA || j.Scores[0].Score != 90 {
		t.Fatalf("scores = %+v", j.Scores)
	}
	if j.Model != llm.FallbackModel {
		t.Fatalf("model = %q, want fallback %q (MULTICA_JUDGE_MODEL unset)", j.Model, llm.FallbackModel)
	}
	if j.CostUsdTicks != nil {
		t.Fatalf("cost_usd_ticks = %v, want null — the stub reported no usage", *j.CostUsdTicks)
	}
	if _, err := time.Parse(time.RFC3339, j.JudgedAt); err != nil {
		t.Fatalf("judged_at = %q, want RFC3339: %v", j.JudgedAt, err)
	}
	if gotModel != llm.FallbackModel {
		t.Fatalf("LLM called with model %q, want %q", gotModel, llm.FallbackModel)
	}

	// The prompt carries only real facts: the issue text and both attempts'
	// recorded ids, models, statuses, costs, durations and diffs.
	for _, want := range []string{
		"judge-happy race", "the issue body the judge must see",
		taskA, taskB, "sonnet", "opus",
		"cost_usd_ticks=5000", "cost_usd_ticks=9000",
		"duration_seconds=120", "duration_seconds=300",
		"patch a", "patch b",
	} {
		if !strings.Contains(gotUserPrompt, want) {
			t.Fatalf("judge prompt missing %q:\n%s", want, gotUserPrompt)
		}
	}

	// Persisted: the column round-trips, and the list endpoint echoes it.
	var stored []byte
	dbfx.QueryRow(t, `SELECT judgement FROM run_group WHERE id = $1`, group.ID).Scan(&stored)
	var storedJ RunGroupJudgement
	if err := json.Unmarshal(stored, &storedJ); err != nil || storedJ.Status != "answered" || storedJ.WinnerTaskID != taskA {
		t.Fatalf("stored judgement = %s (err %v)", string(stored), err)
	}

	issueID := group.IssueID
	var listed struct {
		Groups []RunGroupResponse `json:"groups"`
	}
	testutil.Call(t, testHandler.ListIssueRunGroups, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issueID+"/run-groups", nil), "id", issueID)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Groups) != 1 || listed.Groups[0].Judgement == nil || listed.Groups[0].Judgement.WinnerTaskID != taskA {
		t.Fatalf("listed groups = %+v, want the judgement echoed", listed.Groups)
	}
}

// The judge call's own cost comes from its real token usage, priced at the
// model's known rate — the consult rule.
func TestRunGroupJudgeRecordsUsageAndCost(t *testing.T) {
	_, group, taskA, _ := judgedRunGroupFixture(t, "judge-cost")

	withJudgeLLM(t, &stubJudgeLLM{
		enabled: true,
		reply:   `{"winner_task_id":"` + taskA + `","justification":"j","scores":[{"task_id":"` + taskA + `","score":80,"rationale":"r"}]}`,
		usage:   llm.Usage{InputTokens: 1_000_000, OutputTokens: 500_000},
	})

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	judgeCall(t, group.ID).Want(http.StatusOK).JSON(&out)

	// gpt-5.6-luna (the fallback judge model) is priced at $1/$6 per MTok:
	// 1M in + 500k out = $4 = 4e10 ticks.
	wantTicks := int64(4) * pricing.TicksPerUSD
	j := out.Group.Judgement
	if j == nil || j.CostUsdTicks == nil || *j.CostUsdTicks != wantTicks {
		t.Fatalf("judgement cost = %+v, want %d ($4 at luna rates)", j, wantTicks)
	}
}

// A reply that is not the judge's JSON is a failed judgement, persisted and
// answered with 200 — not a 5xx.
func TestRunGroupJudgeGarbageOutputMarksFailed(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-garbage")
	withJudgeLLM(t, &stubJudgeLLM{enabled: true, reply: `not json at all`})

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	judgeCall(t, group.ID).Want(http.StatusOK).JSON(&out)
	j := out.Group.Judgement
	if j == nil || j.Status != "failed" || j.WinnerTaskID != "" || j.Justification == "" {
		t.Fatalf("judgement = %+v, want failed with an explanation and no winner", j)
	}
	var storedStatus string
	dbfx.QueryRow(t, `SELECT judgement->>'status' FROM run_group WHERE id = $1`, group.ID).Scan(&storedStatus)
	if storedStatus != "failed" {
		t.Fatalf("stored judgement status = %q, want failed", storedStatus)
	}
}

// A winner_task_id that is not an attempt of the race is the judge
// hallucinating: failed judgement, 200.
func TestRunGroupJudgeUnknownWinnerMarksFailed(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-stray")
	withJudgeLLM(t, &stubJudgeLLM{enabled: true,
		reply: `{"winner_task_id":"` + uuid.NewString() + `","justification":"j","scores":[]}`})

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	judgeCall(t, group.ID).Want(http.StatusOK).JSON(&out)
	j := out.Group.Judgement
	if j == nil || j.Status != "failed" || j.WinnerTaskID != "" {
		t.Fatalf("judgement = %+v, want failed with no winner", j)
	}
}

// An upstream error stores a failed judgement too: the judging attempt
// happened, and its failure is a fact the UI can show.
func TestRunGroupJudgeLLMErrorMarksFailed(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-error")
	withJudgeLLM(t, &stubJudgeLLM{enabled: true, err: errors.New("upstream exploded")})

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	judgeCall(t, group.ID).Want(http.StatusOK).JSON(&out)
	j := out.Group.Judgement
	if j == nil || j.Status != "failed" || !strings.Contains(j.Justification, "upstream exploded") {
		t.Fatalf("judgement = %+v, want failed with the LLM error recorded", j)
	}
}

// Fewer than two completed attempts: 409 run_group_not_judgeable, and the LLM
// is never called.
func TestRunGroupJudgeNotJudgeable(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "judge-early agent a", runtime)
	agentB := dbfx.Agent(t, "judge-early agent b", runtime)
	issue := dbfx.Issue(t, "race still running")
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{
		"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}},
	}, http.StatusCreated)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = now(), completed_at = now() WHERE id = $1`, group.Attempts[0].TaskID)

	stub := &stubJudgeLLM{enabled: true, reply: `{}`}
	withJudgeLLM(t, stub)
	res := judgeCall(t, group.ID).Want(http.StatusConflict)
	if res.Map()["code"] != ErrCodeRunGroupNotJudgeable {
		t.Fatalf("error body = %v, want code %s", res.Map(), ErrCodeRunGroupNotJudgeable)
	}
	if stub.calls != 0 {
		t.Fatalf("LLM called %d times for an unjudgeable race", stub.calls)
	}
	var judgementSet bool
	dbfx.QueryRow(t, `SELECT judgement IS NOT NULL FROM run_group WHERE id = $1`, group.ID).Scan(&judgementSet)
	if judgementSet {
		t.Fatal("an unjudgeable race must not gain a judgement")
	}
}

// The tenant guard: a group is only reachable from the workspace that owns
// it — the same 404 as an unknown id.
func TestRunGroupJudgeForeignWorkspaceNotFound(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-foreign")
	withJudgeLLM(t, &stubJudgeLLM{enabled: true, reply: `{}`})

	foreign := dbfx.Workspace(t, "Judge foreign", "judge-foreign-"+uuid.NewString())
	dbfx.Member(t, foreign, testUserID, "owner")
	req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/judge", nil), "id", group.ID)
	req.Header.Set("X-Workspace-ID", foreign)
	testutil.Call(t, testHandler.JudgeRunGroup, req).Want(http.StatusNotFound)

	judgeCall(t, uuid.NewString()).Want(http.StatusNotFound)
}

// Judging spends LLM budget on a human's say-so: the route carries
// RequireHumanActor, and a task token is refused before the handler runs.
func TestRunGroupJudgeTaskTokenForbidden(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-token")
	guarded := func(w http.ResponseWriter, r *http.Request) {
		RequireHumanActor(http.HandlerFunc(testHandler.JudgeRunGroup)).ServeHTTP(w, r)
	}
	req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/judge", nil), "id", group.ID)
	req.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, guarded, req).Want(http.StatusForbidden)
}

// No LLM configured: a clean 503, nothing persisted.
func TestRunGroupJudgeLLMDisabled(t *testing.T) {
	_, group, _, _ := judgedRunGroupFixture(t, "judge-disabled")
	withJudgeLLM(t, &stubJudgeLLM{enabled: false})

	judgeCall(t, group.ID).Want(http.StatusServiceUnavailable)
	var judgementSet bool
	dbfx.QueryRow(t, `SELECT judgement IS NOT NULL FROM run_group WHERE id = $1`, group.ID).Scan(&judgementSet)
	if judgementSet {
		t.Fatal("a disabled LLM must not leave a judgement behind")
	}
}
