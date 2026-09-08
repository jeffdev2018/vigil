package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Cascade escalation (JEF-272): a below-threshold run re-enqueues on a
// stronger runtime before going to human review.

// seedCascadeTask inserts one completed, issue-linked task row ready to be
// scored. contextJSON may be nil (no escalation history).
func seedCascadeTask(t *testing.T, pool *pgxpool.Pool, agentID, runtimeID, issueID, userID, taskClass string, contextJSON []byte) string {
	t.Helper()
	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "I think it works, not sure."})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, result, task_class, context,
			originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'completed', 0, $4::jsonb, $5, $6::jsonb, $7, $7, 'direct_human')
		RETURNING id`, agentID, runtimeID, issueID, result, taskClass, contextJSON, userID).Scan(&taskID); err != nil {
		t.Fatalf("seed cascade task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// escalatedTaskForIssue returns the task the cascade enqueued (any task on the
// issue other than the scored one), or "" when none exists.
func escalatedTaskForIssue(t *testing.T, pool *pgxpool.Pool, issueID, scoredTaskID string) (id, runtimeID, status, handoffNote string, contextJSON []byte) {
	t.Helper()
	var note *string
	err := pool.QueryRow(context.Background(), `
		SELECT id::text, runtime_id::text, status, handoff_note::text, context
		FROM agent_task_queue
		WHERE issue_id = $1 AND id <> $2`, issueID, scoredTaskID).
		Scan(&id, &runtimeID, &status, &note, &contextJSON)
	if err != nil {
		return "", "", "", "", nil
	}
	if note != nil {
		handoffNote = *note
	}
	return id, runtimeID, status, handoffNote, contextJSON
}

func cascadeIssueStatus(t *testing.T, pool *pgxpool.Pool, issueID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	return status
}

func cascadeInboxCount(t *testing.T, pool *pgxpool.Pool, issueID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = $2`, issueID, ConfidenceReviewInboxType).Scan(&n); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	return n
}

// cascadeTaskLeg reads the JEF-274 leg columns of one task.
func cascadeTaskLeg(t *testing.T, pool *pgxpool.Pool, taskID string) (role, root string) {
	t.Helper()
	var rootPtr *string
	if err := pool.QueryRow(context.Background(),
		`SELECT leg_role, workflow_root_task_id::text FROM agent_task_queue WHERE id = $1`, taskID).
		Scan(&role, &rootPtr); err != nil {
		t.Fatalf("load task leg: %v", err)
	}
	if rootPtr != nil {
		root = *rootPtr
	}
	return role, root
}

// cascadeLegRefusedAudits counts the workflow.leg_refused entries JEF-275
// writes when a workflow may not grow another leg.
func cascadeLegRefusedAudits(t *testing.T, pool *pgxpool.Pool, rootTaskID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`,
		AuditWorkflowLegRefused, rootTaskID).Scan(&n); err != nil {
		t.Fatalf("count leg refusals: %v", err)
	}
	return n
}

// cascadeFixture builds the JEF-272 playground: the routing fixture's agent
// bound to runtime A, an assigned issue, and (optionally) run history on
// runtime B making it a stronger candidate for the general class.
func cascadeFixture(t *testing.T) (routingTestFixture, *testutil.Fixture, string) {
	t.Helper()
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	issueID := dbfx.Issue(t, "Improve onboarding flow", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.agentID,
		"priority":      "medium",
	})
	return fx, dbfx, issueID
}

func TestRunConfidenceCascadeEscalatesToStrongerRuntime(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	// Runtime B has a strong record on the general class; runtime A (which
	// just failed) has none, so B is the best available hop.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"The run expresses doubt and shows no verification."}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	// A fresh task landed on runtime B with the escalation context.
	newTaskID, runtimeID, status, note, contextJSON := escalatedTaskForIssue(t, fx.pool, issueID, taskID)
	if newTaskID == "" {
		t.Fatal("no escalation task enqueued")
	}
	if runtimeID != fx.runtimeB {
		t.Errorf("escalated task runtime = %s, want stronger %s", runtimeID, fx.runtimeB)
	}
	if status != "queued" {
		t.Errorf("escalated task status = %q, want queued", status)
	}
	var ctxMap struct {
		Escalation TaskEscalation `json:"escalation"`
	}
	if err := json.Unmarshal(contextJSON, &ctxMap); err != nil {
		t.Fatalf("escalated task context is not JSON: %v (%s)", err, contextJSON)
	}
	want := TaskEscalation{
		FromTaskID:    taskID,
		Reason:        escalationReasonBelowThreshold,
		Attempt:       1,
		FromRuntimeID: fx.runtimeA,
	}
	if ctxMap.Escalation != want {
		t.Errorf("escalation context = %+v, want %+v", ctxMap.Escalation, want)
	}
	if !strings.Contains(note, "0.20") || !strings.Contains(note, "escalated to a stronger runtime (attempt 1)") {
		t.Errorf("handoff note missing score or escalation mention: %q", note)
	}

	// Per-leg accounting (JEF-274): the escalation is an escalation leg of the
	// scored run's workflow, rooted at the run it escalated from.
	if role, root := cascadeTaskLeg(t, fx.pool, newTaskID); role != LegRoleEscalation || root != taskID {
		t.Errorf("escalated task leg = (%q, %q), want (%q, %q)", role, root, LegRoleEscalation, taskID)
	}

	// The WS event names both runtimes and the attempt.
	if len(*escalated) != 1 {
		t.Fatalf("task:escalated events = %d, want 1", len(*escalated))
	}
	payload, ok := (*escalated)[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", (*escalated)[0].Payload)
	}
	if payload["task_id"] != newTaskID || payload["from_task_id"] != taskID || payload["issue_id"] != issueID {
		t.Errorf("payload ids = %v", payload)
	}
	if payload["from_runtime_id"] != fx.runtimeA || payload["to_runtime_id"] != fx.runtimeB {
		t.Errorf("payload runtimes = %v", payload)
	}
	if payload["attempt"] != 1 {
		t.Errorf("payload attempt = %v, want 1", payload["attempt"])
	}

	// No human review while the cascade retries.
	if status := cascadeIssueStatus(t, fx.pool, issueID); status == "in_review" {
		t.Error("issue moved to in_review despite a successful escalation")
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n != 0 {
		t.Errorf("inbox items = %d, want 0 (escalation precedes human review)", n)
	}
}

func TestRunConfidenceCascadeCeilingGoesToHumanReview(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	// The scored run is already at the ceiling (attempt == default max 2).
	contextJSON := []byte(`{"escalation":{"from_task_id":"` + util.UUIDToString(dbid.NewV7()) + `","reason":"below_threshold","attempt":2,"from_runtime_id":"` + fx.runtimeA + `"}}`)
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, contextJSON)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"still unsure"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, _, _, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued past the ceiling", newTaskID)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0 at the ceiling", len(*escalated))
	}
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review at the ceiling", status)
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n == 0 {
		t.Error("no confidence_review inbox item at the ceiling")
	}
}

func TestRunConfidenceCascadeNoStrongerRuntimeGoesToHumanReview(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	// Both runtimes are scored on the class: A (the failed one) is strong,
	// B is weaker but above the exclusion floor — so no candidate is
	// strictly better and the cascade must not fire.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, "openai", "m-a", 5, 5)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 10)
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"doubtful"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, _, _, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued without a stronger runtime", newTaskID)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0", len(*escalated))
	}
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review", status)
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n == 0 {
		t.Error("no confidence_review inbox item")
	}
}

func TestRunConfidenceCascadeMaxEscalationsZeroDisables(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	if _, err := fx.pool.Exec(context.Background(),
		`UPDATE workspace SET settings = '{"confidence_review":{"enabled":true,"threshold":0.5,"max_escalations":0}}'::jsonb WHERE id = $1`,
		fx.workspace); err != nil {
		t.Fatalf("set max_escalations=0: %v", err)
	}
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"doubtful"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, _, _, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued with max_escalations=0", newTaskID)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0 with max_escalations=0", len(*escalated))
	}
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review (cascade disabled)", status)
	}
}

// Bounded workflows (JEF-275): max_escalations caps this cascade chain, but
// the workflow ceiling caps every leg the run's workflow already spent. At the
// workflow ceiling the cascade is refused — audited — and a human takes over,
// even though the escalation attempt budget is untouched.
func TestRunConfidenceCascadeWorkflowCeilingGoesToHumanReview(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	// max_legs = 1: the scored run is its own root, so it alone fills the
	// workflow and no second leg may be created.
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('workflow_limits', jsonb_build_object('max_legs', 1)) WHERE id = $1`, fx.workspace)
	// attempt 0: the JEF-272 ceiling is NOT what refuses this one.
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"still unsure"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, _, _, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued past the workflow ceiling", newTaskID)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0 at the workflow ceiling", len(*escalated))
	}
	if n := cascadeLegRefusedAudits(t, fx.pool, taskID); n != 1 {
		t.Errorf("workflow.leg_refused audit entries = %d, want 1", n)
	}
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review at the workflow ceiling", status)
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n == 0 {
		t.Error("no confidence_review inbox item at the workflow ceiling")
	}
}

func TestConfidenceReviewSettingsMaxEscalationsParsing(t *testing.T) {
	if got := ConfidenceReviewSettings(nil); got.MaxEscalations != 2 {
		t.Errorf("empty settings max_escalations = %d, want default 2", got.MaxEscalations)
	}
	got := ConfidenceReviewSettings([]byte(`{"confidence_review":{"enabled":true,"threshold":0.5}}`))
	if got.MaxEscalations != 2 {
		t.Errorf("omitted max_escalations = %d, want default 2 (absent must not read as 0)", got.MaxEscalations)
	}
	got = ConfidenceReviewSettings([]byte(`{"confidence_review":{"enabled":true,"threshold":0.5,"max_escalations":0}}`))
	if got.MaxEscalations != 0 {
		t.Errorf("explicit max_escalations = %d, want 0 (cascade off)", got.MaxEscalations)
	}
	got = ConfidenceReviewSettings([]byte(`{"confidence_review":{"enabled":true,"threshold":0.5,"max_escalations":7}}`))
	if got.MaxEscalations != 2 {
		t.Errorf("out-of-range max_escalations = %d, want default 2", got.MaxEscalations)
	}
}

// Data residency (K46): the cascade may not escalate out of policy.
//
// The failed runtime is COMPLIANT here, so the enqueue-time gate has nothing
// to refuse — it is the escalation's own candidate pick that must exclude the
// stronger-but-non-compliant runtime. That matters because a forced runtime
// (task.go, JEF-272) overrides every routing filter downstream: whatever
// pickEscalationRuntime returns is where the retry runs.
func TestRunConfidenceCascadeResidencyBlocksEscalationToHumanReview(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	// Runtime B has the strong record that would normally win the hop, and no
	// declaration; runtime A — the one that just failed — is declared in the
	// only allowed region, so the run itself is never in doubt.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	dbfx.Exec(t, `INSERT INTO runtime_compliance_profile (runtime_id, region, on_prem) VALUES ($1, 'eu-west-1', false)`, fx.runtimeA)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM runtime_compliance_profile WHERE runtime_id = $1`, fx.runtimeA)
	})
	setResidencyPolicy(t, dbfx, fx.workspace, `{"region_allowlist":["eu-west-1"],"banned_providers":[],"require_on_prem":false}`)
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"The run expresses doubt."}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, runtimeID, _, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued on undeclared runtime %s despite the data residency policy", newTaskID, runtimeID)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0 when no compliant runtime exists", len(*escalated))
	}
	// The run is not lost: with nowhere compliant to hop, a human takes it.
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review when the policy blocks every hop", status)
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n == 0 {
		t.Error("no confidence_review inbox item when the policy blocks every hop")
	}
}

// The cascade stamps its retry with a runtime the agent is not bound to, and
// the claim fence only lets an auto-routed agent execute there. For a fixed
// agent the escalation would sit queued forever — observed in vigil-int: ten
// below_threshold escalations, leg_role 'escalation', runtime_routing 'fixed',
// none ever dispatched — and the human review it replaced would never happen.
func TestRunConfidenceCascadeFixedRoutingGoesToHumanReview(t *testing.T) {
	fx, dbfx, issueID := cascadeFixture(t)
	// Same seed as the escalating case: runtime B is strictly stronger, so
	// only the routing mode decides between a cascade and a human.
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 19)
	if _, err := fx.pool.Exec(context.Background(),
		`UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID); err != nil {
		t.Fatalf("pin the agent to its runtime: %v", err)
	}
	taskID := seedCascadeTask(t, fx.pool, fx.agentID, fx.runtimeA, issueID, fx.user, TaskClassGeneral, nil)

	bus := events.New()
	escalated := collectEvents(bus, protocol.EventTaskEscalated)
	svc := runConfidenceService(fx.pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"doubtful"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if newTaskID, runtimeID, status, _, _ := escalatedTaskForIssue(t, fx.pool, issueID, taskID); newTaskID != "" {
		t.Errorf("escalation task %s enqueued for a fixed agent on runtime %s (status %q); no daemon may claim it",
			newTaskID, runtimeID, status)
	}
	if len(*escalated) != 0 {
		t.Errorf("task:escalated events = %d, want 0 for a fixed agent", len(*escalated))
	}
	if status := cascadeIssueStatus(t, fx.pool, issueID); status != "in_review" {
		t.Errorf("issue status = %q, want in_review", status)
	}
	if n := cascadeInboxCount(t, fx.pool, issueID); n == 0 {
		t.Error("no confidence_review inbox item")
	}
}
