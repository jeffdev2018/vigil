package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedFailedTask inserts one failed task row for the fixture's agent with the
// given classified reason and error text, mirroring what FailTask persists.
func (f agentMemoryExtractionFixture) seedFailedTask(t *testing.T, pool *pgxpool.Pool, failureReason, errMsg string) string {
	t.Helper()
	var runtimeID string
	if err := pool.QueryRow(context.Background(),
		`SELECT runtime_id::text FROM agent WHERE id = $1`, f.agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, error, failure_reason,
			attempt, max_attempts, originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'failed', 0, $4, $5, 1, 2, $6, $6, 'direct_human')
		RETURNING id`, f.agentID, runtimeID, f.issueID, errMsg, failureReason, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("seed failed task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM postmortem WHERE source_task_id = $1`, taskID)
	})
	return taskID
}

func postmortemService(pool *pgxpool.Pool, bus *events.Bus, client PostmortemLLM) *TaskService {
	return &TaskService{
		Queries:    db.New(pool),
		TxStarter:  pool,
		Bus:        bus,
		Postmortem: client,
	}
}

func loadPostmortemByTask(t *testing.T, pool *pgxpool.Pool, taskID string) (db.Postmortem, bool) {
	t.Helper()
	pm, err := db.New(pool).GetPostmortemBySourceTask(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return db.Postmortem{}, false
		}
		t.Fatalf("load postmortem: %v", err)
	}
	return pm, true
}

const validPostmortemReply = `{"summary":"The run exhausted the model context.","root_cause":"The task loaded too many large files at once.","impact":"The intended change was not delivered.","preventive_rules":["Split large tasks into smaller sub-tasks.","Reduce the context loaded before the run."]}`

func TestGeneratePostmortemWithLLMDrafts(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedFailedTask(t, pool, "agent_error.context_overflow", "context length exceeded")
	svc := postmortemService(pool, events.New(), stubMemoryLLM(t, validPostmortemReply))

	if err := svc.GeneratePostmortemForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("generate: %v", err)
	}

	pm, ok := loadPostmortemByTask(t, pool, taskID)
	if !ok {
		t.Fatal("postmortem was not created")
	}
	if !pm.LlmGenerated {
		t.Errorf("llm_generated = false, want true")
	}
	if pm.Summary != "The run exhausted the model context." {
		t.Errorf("summary = %q", pm.Summary)
	}
	if pm.State != "draft" {
		t.Errorf("state = %q, want draft", pm.State)
	}
	var rules []string
	if err := json.Unmarshal(pm.PreventiveRules, &rules); err != nil {
		t.Fatalf("unmarshal rules: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("rules = %v, want 2", rules)
	}
}

func TestGeneratePostmortemFallsBackToScaffoldWithoutLLM(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedFailedTask(t, pool, "agent_error.context_overflow", "context length exceeded")
	svc := postmortemService(pool, events.New(), nil) // no LLM -> scaffold

	if err := svc.GeneratePostmortemForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("generate: %v", err)
	}

	pm, ok := loadPostmortemByTask(t, pool, taskID)
	if !ok {
		t.Fatal("scaffold postmortem was not created")
	}
	if pm.LlmGenerated {
		t.Errorf("llm_generated = true, want false for the scaffold")
	}
	if pm.Summary == "" || pm.RootCause == "" || pm.Impact == "" {
		t.Errorf("scaffold left fields empty: %+v", pm)
	}
	var rules []string
	if err := json.Unmarshal(pm.PreventiveRules, &rules); err != nil {
		t.Fatalf("unmarshal rules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("scaffold produced no preventive rules")
	}
}

func TestGeneratePostmortemSkipsInfraFailures(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedFailedTask(t, pool, "runtime_offline", "runtime went offline")
	svc := postmortemService(pool, events.New(), nil)

	if err := svc.GeneratePostmortemForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, ok := loadPostmortemByTask(t, pool, taskID); ok {
		t.Fatal("postmortem created for a pure-infra failure, want none")
	}
}

func TestGeneratePostmortemIsIdempotent(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedFailedTask(t, pool, "timeout", "task timed out")
	svc := postmortemService(pool, events.New(), nil)

	for i := 0; i < 2; i++ {
		if err := svc.GeneratePostmortemForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
			t.Fatalf("generate #%d: %v", i+1, err)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM postmortem WHERE source_task_id = $1`, taskID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("postmortems = %d, want exactly 1", n)
	}
}

func TestParsePostmortemRejectsMalformed(t *testing.T) {
	if _, err := parsePostmortem("not json"); err == nil {
		t.Fatal("parsePostmortem accepted non-JSON")
	}
	if _, err := parsePostmortem(`{"summary":""}`); err != nil {
		t.Fatalf("parsePostmortem errored on valid JSON: %v", err)
	}
}

func TestScaffoldRulesForReason(t *testing.T) {
	cases := []struct {
		reason string
		want   string // substring expected in at least one rule
	}{
		{"agent_error.context_overflow", "smaller"},
		{"timeout", "scope"},
		{"agent_error.provider_quota_limit", "quota"},
		{"something_unknown", "transcript"},
	}
	for _, tc := range cases {
		rules := scaffoldRulesForReason(tc.reason)
		if len(rules) == 0 {
			t.Errorf("reason %q produced no rules", tc.reason)
			continue
		}
		found := false
		for _, r := range rules {
			if strings.Contains(strings.ToLower(r), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("reason %q rules %v, want one containing %q", tc.reason, rules, tc.want)
		}
	}
}
