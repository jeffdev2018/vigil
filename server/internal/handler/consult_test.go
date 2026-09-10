package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// Agent consult (JEF-12). POST /api/consult is task_token-only; the reads
// serve the owning task's token or a member who can see the agent.

// stubConsultLLM is an enabled-or-not ConsultLLM that never dials an upstream,
// mirroring stubChatQuickActionsLLM.
type stubConsultLLM struct {
	enabled bool
	reply   string
	usage   llm.Usage
	err     error
	// gotModel/gotUserPrompt record what the consult pass was given.
	gotModel      *string
	gotUserPrompt *string
}

func (s stubConsultLLM) Enabled() bool { return s.enabled }

func (s stubConsultLLM) GenerateJSONWithUsage(_ context.Context, model, _, userPrompt string, _ float64, _ int64) (string, llm.Usage, error) {
	if s.gotModel != nil {
		*s.gotModel = model
	}
	if s.gotUserPrompt != nil {
		*s.gotUserPrompt = userPrompt
	}
	return s.reply, s.usage, s.err
}

func withConsultLLM(t *testing.T, stub ConsultLLM) {
	t.Helper()
	prev := testHandler.ConsultLLM
	testHandler.ConsultLLM = stub
	t.Cleanup(func() { testHandler.ConsultLLM = prev })
}

// consultTaskFixture seeds a workspace-visible agent with a running task and
// returns (agentID, taskID).
func consultTaskFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	agentID := createHandlerTestAgent(t, name, nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	return agentID, taskID
}

// newConsultTaskRequest builds a request carrying the headers the auth
// middleware stamps for an mat_ task token.
func newConsultTaskRequest(method, path string, body any, taskID, agentID string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Agent-ID", agentID)
	return req
}

func TestConsultRequiresTaskToken(t *testing.T) {
	// A member credential — even the workspace owner's — must not consult:
	// the endpoint exists so a RUNNING TASK can ask, not so a human can.
	req := newRequest(http.MethodPost, "/api/consult", map[string]any{"question": "q"})
	testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusForbidden)
}

func TestConsultAnsweredHappyPath(t *testing.T) {
	agentID, taskID := consultTaskFixture(t, "consult-happy")

	var gotModel, gotUserPrompt string
	withConsultLLM(t, stubConsultLLM{
		enabled: true, reply: `{"answer":"use Postgres"}`,
		gotModel: &gotModel, gotUserPrompt: &gotUserPrompt,
	})

	req := newConsultTaskRequest(http.MethodPost, "/api/consult",
		map[string]any{"question": "which store?", "context": "40M rows"}, taskID, agentID)
	w := testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusOK)
	var resp CreateAgentConsultResponse
	w.JSON(&resp)
	if resp.ConsultID == "" || resp.Answer != "use Postgres" {
		t.Fatalf("response = %+v, want consult_id set and extracted answer", resp)
	}
	if resp.Model != llm.FallbackModel {
		t.Fatalf("model = %q, want fallback %q (MULTICA_CONSULT_MODEL unset)", resp.Model, llm.FallbackModel)
	}
	if resp.CostUSDTicks != nil {
		t.Fatalf("cost_usd_ticks = %v, want null — this LLM path reports no usage", *resp.CostUSDTicks)
	}
	if gotModel != llm.FallbackModel {
		t.Fatalf("LLM called with model %q, want %q", gotModel, llm.FallbackModel)
	}
	if !strings.Contains(gotUserPrompt, "which store?") || !strings.Contains(gotUserPrompt, "40M rows") {
		t.Fatalf("user prompt missing question/context: %q", gotUserPrompt)
	}

	// The row carries the full attribution: workspace + calling agent + task,
	// and reached its terminal state.
	var state, wsID, rowAgent, rowTask string
	dbfx.QueryRow(t,
		`SELECT state, workspace_id::text, agent_id::text, task_id::text FROM agent_consult WHERE id = $1`,
		resp.ConsultID,
	).Scan(&state, &wsID, &rowAgent, &rowTask)
	if state != "answered" || wsID != testWorkspaceID || rowAgent != agentID || rowTask != taskID {
		t.Fatalf("row = (state=%s ws=%s agent=%s task=%s), want answered in this workspace attributed to the caller",
			state, wsID, rowAgent, rowTask)
	}

	// The owning task re-reads it; the execution-log list serves it too.
	getReq := newConsultTaskRequest(http.MethodGet, "/api/consult/"+resp.ConsultID, nil, taskID, agentID)
	getReq = withURLParam(getReq, "id", resp.ConsultID)
	w = testutil.Call(t, testHandler.GetAgentConsultByID, getReq).Want(http.StatusOK)
	var detail AgentConsultResponse
	w.JSON(&detail)
	if detail.State != "answered" || detail.Answer == nil || *detail.Answer != "use Postgres" {
		t.Fatalf("GET by owning task = %+v, want answered row", detail)
	}

	listReq := newConsultTaskRequest(http.MethodGet, "/api/consult?task_id="+taskID, nil, taskID, agentID)
	w = testutil.Call(t, testHandler.ListAgentConsults, listReq).Want(http.StatusOK)
	var list []AgentConsultResponse
	w.JSON(&list)
	if len(list) != 1 || list[0].ConsultID != resp.ConsultID {
		t.Fatalf("list by owning task = %+v, want the one consult", list)
	}

	// A member who can see the agent (the workspace owner here) reads both.
	memberGet := withURLParam(newRequest(http.MethodGet, "/api/consult/"+resp.ConsultID, nil), "id", resp.ConsultID)
	testutil.Call(t, testHandler.GetAgentConsultByID, memberGet).Want(http.StatusOK)
	testutil.Call(t, testHandler.ListAgentConsults,
		newRequest(http.MethodGet, "/api/consult?task_id="+taskID, nil)).Want(http.StatusOK)
}

func TestConsultAnsweredRecordsUsageAndCost(t *testing.T) {
	agentID, taskID := consultTaskFixture(t, "consult-cost")

	withConsultLLM(t, stubConsultLLM{
		enabled: true, reply: `{"answer":"use Postgres"}`,
		usage: llm.Usage{InputTokens: 1_000_000, OutputTokens: 500_000},
	})

	req := newConsultTaskRequest(http.MethodPost, "/api/consult",
		map[string]any{"question": "which store?"}, taskID, agentID)
	w := testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusOK)
	var resp CreateAgentConsultResponse
	w.JSON(&resp)

	// gpt-5.6-luna (the fallback consult model) is priced at $1/$6 per MTok:
	// 1M in + 500k out = $4 = 4e10 ticks.
	wantTicks := int64(4) * pricing.TicksPerUSD
	if resp.CostUSDTicks == nil || *resp.CostUSDTicks != wantTicks {
		t.Fatalf("cost_usd_ticks = %v, want %d ($4 at luna rates)", resp.CostUSDTicks, wantTicks)
	}
	var inTok, outTok, cost int64
	dbfx.QueryRow(t,
		`SELECT input_tokens, output_tokens, cost_usd_ticks FROM agent_consult WHERE id = $1`,
		resp.ConsultID,
	).Scan(&inTok, &outTok, &cost)
	if inTok != 1_000_000 || outTok != 500_000 || cost != wantTicks {
		t.Fatalf("row = (%d in, %d out, %d ticks), want (1000000, 500000, %d)", inTok, outTok, cost, wantTicks)
	}
}

func TestConsultLLMErrorMarksFailed(t *testing.T) {
	agentID, taskID := consultTaskFixture(t, "consult-error")
	withConsultLLM(t, stubConsultLLM{enabled: true, err: errors.New("upstream exploded")})

	req := newConsultTaskRequest(http.MethodPost, "/api/consult", map[string]any{"question": "q"}, taskID, agentID)
	w := testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusInternalServerError)
	var body struct {
		Error     string `json:"error"`
		ConsultID string `json:"consult_id"`
	}
	w.JSON(&body)
	if body.ConsultID == "" {
		t.Fatalf("error body = %+v, want the failed consult's id", body)
	}

	var state, reason string
	var finalized bool
	dbfx.QueryRow(t,
		`SELECT state, refusal_reason, finalized_at IS NOT NULL FROM agent_consult WHERE id = $1`,
		body.ConsultID,
	).Scan(&state, &reason, &finalized)
	if state != "failed" || !strings.Contains(reason, "upstream exploded") || !finalized {
		t.Fatalf("row = (state=%s reason=%q finalized=%v), want failed with the LLM error recorded", state, reason, finalized)
	}
}

func TestConsultBudgetExceededRefuses(t *testing.T) {
	agentID, taskID := consultTaskFixture(t, "consult-budget")
	withConsultLLM(t, stubConsultLLM{enabled: true, reply: `{"answer":"a"}`})

	prev := consultMaxPerTaskPerDay
	consultMaxPerTaskPerDay = 1
	t.Cleanup(func() { consultMaxPerTaskPerDay = prev })

	// The first consult fits the budget and answers...
	req := newConsultTaskRequest(http.MethodPost, "/api/consult", map[string]any{"question": "first"}, taskID, agentID)
	testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusOK)

	// ...the second is refused with the stable reason code, and the refusal is
	// itself persisted as a born-terminal row.
	req = newConsultTaskRequest(http.MethodPost, "/api/consult", map[string]any{"question": "second"}, taskID, agentID)
	w := testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusTooManyRequests)
	var refused consultRefusedResponse
	w.JSON(&refused)
	if refused.ReasonCode != consultReasonBudgetExceeded || refused.ConsultID == "" {
		t.Fatalf("refusal = %+v, want reason %s with a persisted consult id", refused, consultReasonBudgetExceeded)
	}
	var state, reason string
	dbfx.QueryRow(t, `SELECT state, refusal_reason FROM agent_consult WHERE id = $1`, refused.ConsultID).Scan(&state, &reason)
	if state != "refused" || reason != consultReasonBudgetExceeded {
		t.Fatalf("refused row = (state=%s reason=%q)", state, reason)
	}

	// The calling run must not fail: nothing about the consult touches the
	// task row.
	var taskStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus)
	if taskStatus != "running" {
		t.Fatalf("task status = %q, want untouched (running)", taskStatus)
	}
}

func TestConsultLLMDisabledRefuses(t *testing.T) {
	agentID, taskID := consultTaskFixture(t, "consult-disabled")
	withConsultLLM(t, stubConsultLLM{enabled: false})

	req := newConsultTaskRequest(http.MethodPost, "/api/consult", map[string]any{"question": "q"}, taskID, agentID)
	w := testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusServiceUnavailable)
	var refused consultRefusedResponse
	w.JSON(&refused)
	if refused.ReasonCode != consultReasonLLMDisabled || refused.ConsultID == "" {
		t.Fatalf("refusal = %+v, want reason %s with a persisted consult id", refused, consultReasonLLMDisabled)
	}
	var state string
	dbfx.QueryRow(t, `SELECT state FROM agent_consult WHERE id = $1`, refused.ConsultID).Scan(&state)
	if state != "refused" {
		t.Fatalf("row state = %q, want refused", state)
	}
}

func TestConsultCrossWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()

	// A foreign workspace with its own agent, running task, and consult.
	foreignWsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Consult foreign ws", "slug": "consult-foreign", "description": "", "issue_prefix": "CFW",
	})
	var foreignAgentID string
	dbfx.QueryRow(t, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'consult-foreign-agent', '', 'cloud', '{}'::jsonb,
		        'workspace', 'public_to', 1, $2, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, foreignWsID, testUserID).Scan(&foreignAgentID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, foreignAgentID)
	})
	foreignTaskID := dbfx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": foreignAgentID, "status": "running", "priority": 0,
		"runtime_id": handlerTestRuntimeID(t), "started_at": testutil.Raw("now()"),
	})
	foreignConsultID := dbfx.Insert(t, "agent_consult", testutil.Cols{
		"workspace_id": foreignWsID,
		"task_id":      foreignTaskID,
		"agent_id":     foreignAgentID,
		"model":        "m",
		"question":     "foreign q",
		"state":        "answered",
		"answer":       "foreign a",
	})

	// A member of OUR workspace must not read it: not by id, not by task list.
	getReq := withURLParam(newRequest(http.MethodGet, "/api/consult/"+foreignConsultID, nil), "id", foreignConsultID)
	testutil.Call(t, testHandler.GetAgentConsultByID, getReq).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.ListAgentConsults,
		newRequest(http.MethodGet, "/api/consult?task_id="+foreignTaskID, nil)).Want(http.StatusNotFound)

	// A task token whose agent lives in another workspace than the request's
	// stamped workspace is refused before any consult is booked (in production
	// the workspace middleware binds the token's workspace first; this is the
	// in-handler defense in depth behind it).
	agentID, taskID := consultTaskFixture(t, "consult-iso")
	req := newConsultTaskRequest(http.MethodPost, "/api/consult", map[string]any{"question": "q"}, taskID, agentID)
	req.Header.Set("X-Workspace-ID", foreignWsID)
	testutil.Call(t, testHandler.CreateAgentConsult, req).Want(http.StatusForbidden)
}

func TestConsultReadVisibilityForPrivateAgent(t *testing.T) {
	// A consult booked by someone's PRIVATE agent inherits the agent's
	// visibility: the unrelated plain member gets 403, never the answer.
	privateAgentID, _, memberID := privateAgentTestFixture(t)
	taskID := dbfx.Task(t, privateAgentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	consultID := dbfx.Insert(t, "agent_consult", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"task_id":      taskID,
		"agent_id":     privateAgentID,
		"model":        "m",
		"question":     "private q",
		"state":        "answered",
		"answer":       "private a",
	})

	getReq := newRequest(http.MethodGet, "/api/consult/"+consultID, nil)
	getReq.Header.Set("X-User-ID", memberID)
	getReq = withURLParam(getReq, "id", consultID)
	testutil.Call(t, testHandler.GetAgentConsultByID, getReq).Want(http.StatusForbidden)

	listReq := newRequest(http.MethodGet, "/api/consult?task_id="+taskID, nil)
	listReq.Header.Set("X-User-ID", memberID)
	testutil.Call(t, testHandler.ListAgentConsults, listReq).Want(http.StatusForbidden)

	// A task token naming a DIFFERENT task must not read it either.
	otherAgent, otherTask := consultTaskFixture(t, "consult-other-task")
	foreignTokenReq := newConsultTaskRequest(http.MethodGet, "/api/consult/"+consultID, nil, otherTask, otherAgent)
	foreignTokenReq = withURLParam(foreignTokenReq, "id", consultID)
	testutil.Call(t, testHandler.GetAgentConsultByID, foreignTokenReq).Want(http.StatusNotFound)
}
