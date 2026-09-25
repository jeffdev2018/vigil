package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// followupRequest carries the workspace in context the way the middleware
// does for a member; task-token calls add their own headers on top.
func followupRequest(method, path string, body any) *http.Request {
	return testutil.WithHeaders(newRequest(method, path, body), "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
}

type followupEnvelope struct {
	Followup FollowupResponse `json:"followup"`
}

func TestIssueFollowupsLifecycleAndBudget(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "followup "+uuid.NewString()[:6])
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1 AND trigger_evidence_kind = 'followup'`, issue)
	})
	// A member schedules for the issue's agent, without naming it.
	var out followupEnvelope
	testutil.Call(t, testHandler.CreateIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodPost, "/api/issues/"+issue+"/followups", map[string]any{"when": "+90", "note": "  Relire la réponse du client  "}), "id", issue)).
		Want(http.StatusCreated).JSON(&out)
	if out.Followup.AgentID != agent || out.Followup.Note != "Relire la réponse du client" || out.Followup.ScheduledByType != "member" || out.Followup.ScheduledByID == nil || *out.Followup.ScheduledByID != testUserID {
		t.Fatalf("followup = %+v", out.Followup)
	}
	fires, _ := time.Parse(time.RFC3339, out.Followup.FiresAt)
	if d := time.Until(fires); d < 85*time.Minute || d > 91*time.Minute {
		t.Errorf("fires in %v, want ~90 min", d)
	}
	// The run schedules its own agent through the task-token headers.
	var byAgent followupEnvelope
	req := testutil.WithHeaders(followupRequest(http.MethodPost, "/api/issues/"+issue+"/followups", map[string]any{"when": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}), "X-Actor-Source", "task_token", "X-Agent-ID", agent, "X-Task-ID", task)
	testutil.Call(t, testHandler.CreateIssueFollowup, testutil.WithURLParams(req, "id", issue)).Want(http.StatusCreated).JSON(&byAgent)
	if byAgent.Followup.ScheduledByType != "agent" || byAgent.Followup.Note != "Scheduled follow-up." {
		t.Errorf("agent followup = %+v", byAgent.Followup)
	}
	// Validation: past, too far, garbage.
	for _, when := range []string{"", "+0", "yesterday", time.Now().Add(40 * 24 * time.Hour).UTC().Format(time.RFC3339)} {
		testutil.Call(t, testHandler.CreateIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodPost, "/api/issues/"+issue+"/followups", map[string]any{"when": when}), "id", issue)).Want(http.StatusBadRequest)
	}
	// List: soonest first, budget attached.
	var listed struct {
		Followups []FollowupResponse `json:"followups"`
		Budget    struct {
			MaxPerAgentPerDay int `json:"max_per_agent_per_day"`
		} `json:"budget"`
	}
	testutil.Call(t, testHandler.ListIssueFollowups, testutil.WithURLParams(followupRequest(http.MethodGet, "/api/issues/"+issue+"/followups", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Followups) != 2 || listed.Followups[0].ID != out.Followup.ID || listed.Budget.MaxPerAgentPerDay != 20 {
		t.Fatalf("list = %+v", listed)
	}
	// Agenda shows the wake-ups in the window.
	var agenda CalendarAgendaResponse
	from := time.Now().UTC().Format(time.RFC3339)
	to := time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339)
	testutil.Call(t, testHandler.GetCalendarAgenda, followupRequest(http.MethodGet, "/api/calendar/agenda?from="+from+"&to="+to, nil)).Want(http.StatusOK).JSON(&agenda)
	found := 0
	for _, f := range agenda.Followups {
		if f.IssueID == issue {
			found++
			if f.AgentID != agent || f.Identifier == "" || f.Note == "" {
				t.Errorf("agenda followup = %+v", f)
			}
		}
	}
	if found != 2 {
		t.Errorf("agenda lists %d follow-ups of the issue, want 2", found)
	}
	// Cancel one; cancelling again is a conflict; the list shrinks.
	testutil.Call(t, testHandler.CancelIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodDelete, "/api/issues/"+issue+"/followups/"+out.Followup.ID, nil), "id", issue, "followupId", out.Followup.ID)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.CancelIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodDelete, "/api/issues/"+issue+"/followups/"+out.Followup.ID, nil), "id", issue, "followupId", out.Followup.ID)).Want(http.StatusConflict)
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, out.Followup.ID).Scan(&status); err != nil || status != "cancelled" {
		t.Errorf("cancelled row status = %q (%v)", status, err)
	}
	// Budget: with max 1 per agent per day, the next one is refused.
	if _, err := testPool.Exec(context.Background(), `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"followups":{"max_per_agent_per_day":1}}'::jsonb WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET settings = settings - 'followups' WHERE id = $1`, testWorkspaceID)
	})
	testutil.Call(t, testHandler.CreateIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodPost, "/api/issues/"+issue+"/followups", map[string]any{"when": "+30"}), "id", issue)).Want(http.StatusTooManyRequests)
	// An issue without an agent assignee needs agent_id.
	plain := dbfx.Issue(t, "no agent "+uuid.NewString()[:6])
	testutil.Call(t, testHandler.CreateIssueFollowup, testutil.WithURLParams(followupRequest(http.MethodPost, "/api/issues/"+plain+"/followups", map[string]any{"when": "+30"}), "id", plain)).Want(http.StatusBadRequest)
}

func TestProposeAutopilotFilesAPausedAutopilotBehindADecision(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "autopilot proposal "+uuid.NewString()[:6])
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE assignee_id = $1)`, agent)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE assignee_id = $1`, agent)
	})
	// Without a model, a sentence alone is refused; explicit fields work.
	req := testutil.WithHeaders(followupRequest(http.MethodPost, "/api/autopilots/propose", map[string]any{"text": "chaque lundi 9h, liste les tickets ouverts"}), "X-Actor-Source", "task_token", "X-Agent-ID", agent, "X-Task-ID", task)
	testutil.Call(t, testHandler.ProposeAutopilot, req).Want(http.StatusServiceUnavailable)
	testutil.Call(t, testHandler.DraftAutopilot, followupRequest(http.MethodPost, "/api/autopilots/draft", map[string]any{"text": "chaque lundi 9h"})).Want(http.StatusServiceUnavailable)

	out, err := autopilotToolAdapter{h: testHandler}.Propose(context.Background(), taskRow(t, task), agentRow(t, agent), map[string]any{
		"title": "Tickets ouverts du lundi", "cron_expression": "0 9 * * 1", "timezone": "Europe/Paris", "description": "Liste les tickets ouverts et publie le résumé.", "execution_mode": "create_issue", "issue_title_template": "Tickets ouverts — {{date}}",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	resp := out.(map[string]any)
	ap := resp["autopilot"].(map[string]any)
	if ap["status"] != "paused" || resp["decision_id"] == nil || len(resp["next_runs"].([]any)) != 3 {
		t.Fatalf("proposal = %+v", resp)
	}
	apID := ap["id"].(string)
	var enabled bool
	if err := testPool.QueryRow(context.Background(), `SELECT enabled FROM autopilot_trigger WHERE autopilot_id = $1`, apID).Scan(&enabled); err != nil || enabled {
		t.Fatalf("trigger enabled=%v err=%v, want a disabled schedule", enabled, err)
	}
	// Invalid cron is refused before anything is written.
	adapter := autopilotToolAdapter{h: testHandler}
	if _, err := adapter.Propose(context.Background(), taskRow(t, task), agentRow(t, agent), map[string]any{"title": "x", "cron_expression": "every monday", "description": "y"}); err == nil {
		t.Error("invalid cron accepted")
	}
	// Activate from the card: active + schedule enabled with a next run.
	decisionID := resp["decision_id"].(string)
	// The approvals feed labels the card by its option prefix, the way a
	// calendar proposal is labelled, so a client can say what is being asked
	// without knowing the autopilot tables.
	if card := findApproval(listApprovals(t, "?issue_id="+issue).Approvals, ApprovalSourceDecision, decisionID); card == nil || card.Kind != ApprovalKindAutopilot {
		t.Fatalf("card = %+v, want kind %s", card, ApprovalKindAutopilot)
	}
	answer := followupRequest(http.MethodPost, "/api/issues/"+issue+"/decisions/"+decisionID+"/respond", DecisionAnswer{OptionID: autopilotActivateOption + apID})
	testutil.Call(t, testHandler.RespondIssueDecision, testutil.WithURLParams(answer, "id", issue, "decisionId", decisionID)).Want(http.StatusOK)
	var status string
	var nextRun *time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT a.status, t.next_run_at FROM autopilot a JOIN autopilot_trigger t ON t.autopilot_id = a.id WHERE a.id = $1 AND t.enabled`, apID).Scan(&status, &nextRun); err != nil || status != "active" || nextRun == nil {
		t.Fatalf("after activate: status=%q next=%v err=%v", status, nextRun, err)
	}
	// A second proposal discarded from its card is archived.
	out2, err := autopilotToolAdapter{h: testHandler}.Propose(context.Background(), taskRow(t, task), agentRow(t, agent), map[string]any{"title": "Nightly", "cron_expression": "0 2 * * *", "description": "z"})
	if err != nil {
		t.Fatal(err)
	}
	resp2 := out2.(map[string]any)
	ap2 := resp2["autopilot"].(map[string]any)["id"].(string)
	d2 := resp2["decision_id"].(string)
	answer2 := followupRequest(http.MethodPost, "/api/issues/"+issue+"/decisions/"+d2+"/respond", DecisionAnswer{OptionID: autopilotDiscardOption + ap2})
	testutil.Call(t, testHandler.RespondIssueDecision, testutil.WithURLParams(answer2, "id", issue, "decisionId", d2)).Want(http.StatusOK)
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM autopilot WHERE id = $1`, ap2).Scan(&status); err != nil || status != "archived" {
		t.Errorf("after discard: status=%q err=%v", status, err)
	}
}

func taskRow(t *testing.T, id string) db.AgentTaskQueue {
	t.Helper()
	row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("task %s: %v", id, err)
	}
	return row
}

func agentRow(t *testing.T, id string) db.Agent {
	t.Helper()
	row, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("agent %s: %v", id, err)
	}
	return row
}

func TestProposeAutopilotActivateIsForMembersOnly(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "autopilot activate "+uuid.NewString()[:6])
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE assignee_id = $1)`, agent)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE assignee_id = $1`, agent)
	})
	// A run cannot activate.
	_, err := autopilotToolAdapter{h: testHandler}.Propose(context.Background(), taskRow(t, task), agentRow(t, agent), map[string]any{"title": "x", "cron_expression": "0 9 * * 1", "description": "y", "activate": true})
	if err == nil {
		t.Fatal("a run activated an autopilot")
	}
	// A member can: active, schedule enabled, no card.
	var out struct {
		Autopilot  map[string]any `json:"autopilot"`
		DecisionID *string        `json:"decision_id"`
	}
	testutil.Call(t, testHandler.ProposeAutopilot, followupRequest(http.MethodPost, "/api/autopilots/propose", map[string]any{"title": "Weekly " + uuid.NewString()[:4], "cron_expression": "0 9 * * 1", "timezone": "Europe/Paris", "description": "y", "assignee_id": agent, "issue_id": issue, "activate": true})).
		Want(http.StatusCreated).JSON(&out)
	if out.Autopilot["status"] != "active" || out.DecisionID != nil {
		t.Fatalf("member activate = %+v decision=%v", out.Autopilot, out.DecisionID)
	}
	var enabled bool
	if err := testPool.QueryRow(context.Background(), `SELECT enabled FROM autopilot_trigger WHERE autopilot_id = $1`, out.Autopilot["id"]).Scan(&enabled); err != nil || !enabled {
		t.Errorf("trigger enabled=%v err=%v", enabled, err)
	}
}
