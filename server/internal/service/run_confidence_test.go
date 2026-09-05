package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run confidence scoring (JEF-240): a completed run gets a self-assessed
// score; below the workspace threshold the issue escalates to human review.

func runConfidenceService(pool *pgxpool.Pool, bus *events.Bus, client RunConfidenceLLM) *TaskService {
	return &TaskService{
		Queries:       db.New(pool),
		TxStarter:     pool,
		Bus:           bus,
		RunConfidence: client,
	}
}

// collectEvents records every event of the given types published on the bus.
// The bus is synchronous, so a plain slice is race-free in these tests.
func collectEvents(bus *events.Bus, types ...string) *[]events.Event {
	var got []events.Event
	for _, typ := range types {
		bus.Subscribe(typ, func(e events.Event) { got = append(got, e) })
	}
	return &got
}

func loadTaskConfidence(t *testing.T, pool *pgxpool.Pool, taskID string) (TaskConfidence, bool) {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT confidence FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("load confidence: %v", err)
	}
	if len(raw) == 0 {
		return TaskConfidence{}, false
	}
	var conf TaskConfidence
	if err := json.Unmarshal(raw, &conf); err != nil {
		t.Fatalf("unmarshal confidence: %v", err)
	}
	return conf, true
}

func (f agentMemoryExtractionFixture) issueStatus(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`, f.issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	return status
}

func (f agentMemoryExtractionFixture) confidenceInboxCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = $2`, f.issueID, ConfidenceReviewInboxType).Scan(&n); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1 AND type = $2`, f.issueID, ConfidenceReviewInboxType)
	})
	return n
}

func TestRunConfidenceScoresAndPersists(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Migrated the build to pnpm; pnpm typecheck is green.")
	bus := events.New()
	scored := collectEvents(bus, protocol.EventTaskScored)
	svc := runConfidenceService(pool, bus, stubMemoryLLM(t, `{"score":0.9,"rationale":"Concrete outcome with a verified build."}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	conf, ok := loadTaskConfidence(t, pool, taskID)
	if !ok {
		t.Fatal("confidence was not persisted")
	}
	if conf.Score != 0.9 || conf.Rationale == "" || conf.Model == "" {
		t.Errorf("confidence = %+v", conf)
	}
	if conf.Threshold != DefaultConfidenceReview.Threshold {
		t.Errorf("threshold = %v, want default %v", conf.Threshold, DefaultConfidenceReview.Threshold)
	}
	if conf.BelowThreshold {
		t.Error("below_threshold = true for a 0.9 score at the 0.5 default threshold")
	}

	if len(*scored) != 1 {
		t.Fatalf("task:scored events = %d, want 1", len(*scored))
	}
	payload, ok := (*scored)[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", (*scored)[0].Payload)
	}
	if payload["task_id"] != taskID || payload["issue_id"] != fx.issueID {
		t.Errorf("payload ids = %v", payload)
	}
	if payload["score"] != 0.9 || payload["below_threshold"] != false {
		t.Errorf("payload = %v", payload)
	}
	// Above the threshold: no escalation.
	if status := fx.issueStatus(t, pool); status == "in_review" {
		t.Error("issue moved to in_review for an above-threshold score")
	}
	if n := fx.confidenceInboxCount(t, pool); n != 0 {
		t.Errorf("inbox items = %d, want 0", n)
	}
}

func TestRunConfidenceDisabledLLMIsNoop(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "done")
	svc := runConfidenceService(pool, events.New(), llm.New(llm.Config{})) // no API key: disabled

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score with disabled LLM: %v", err)
	}
	if _, ok := loadTaskConfidence(t, pool, taskID); ok {
		t.Fatal("confidence stored with a disabled LLM")
	}
}

func TestRunConfidenceSkipsReviewTask(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	reviewed := fx.seedTerminalTask(t, pool, "completed", "the delivery")
	review := fx.seedTerminalTask(t, pool, "completed", "looks good")
	if _, err := pool.Exec(context.Background(),
		`UPDATE agent_task_queue SET review_of_task_id = $2 WHERE id = $1`, review, reviewed); err != nil {
		t.Fatalf("mark review task: %v", err)
	}
	svc := runConfidenceService(pool, events.New(), stubMemoryLLM(t, `{"score":0.1,"rationale":"x"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(review)); err != nil {
		t.Fatalf("score: %v", err)
	}
	if _, ok := loadTaskConfidence(t, pool, review); ok {
		t.Fatal("a review run must not be scored itself")
	}
}

func TestRunConfidenceSkipsFailedTask(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "failed", "")
	svc := runConfidenceService(pool, events.New(), stubMemoryLLM(t, `{"score":0.1,"rationale":"x"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}
	if _, ok := loadTaskConfidence(t, pool, taskID); ok {
		t.Fatal("a failed run must not be scored")
	}
}

func TestRunConfidenceBelowThresholdEscalatesToReview(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "I think it works, not sure.")
	bus := events.New()
	updates := collectEvents(bus, protocol.EventIssueUpdated, protocol.EventInboxNew)
	svc := runConfidenceService(pool, bus, stubMemoryLLM(t, `{"score":0.2,"rationale":"The run expresses doubt and shows no verification."}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	conf, ok := loadTaskConfidence(t, pool, taskID)
	if !ok || !conf.BelowThreshold {
		t.Fatalf("confidence = %+v ok=%v, want stored below-threshold", conf, ok)
	}
	if status := fx.issueStatus(t, pool); status != "in_review" {
		t.Errorf("issue status = %q, want in_review", status)
	}
	if n := fx.confidenceInboxCount(t, pool); n == 0 {
		t.Error("no confidence_review inbox item created")
	}
	var sawIssueUpdated, sawInboxNew bool
	for _, e := range *updates {
		switch e.Type {
		case protocol.EventIssueUpdated:
			sawIssueUpdated = true
		case protocol.EventInboxNew:
			sawInboxNew = true
		}
	}
	if !sawIssueUpdated || !sawInboxNew {
		t.Errorf("events: issue:updated=%v inbox:new=%v", sawIssueUpdated, sawInboxNew)
	}
}

func TestRunConfidenceNeverTouchesDoneIssue(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE issue SET status = 'done' WHERE id = $1`, fx.issueID); err != nil {
		t.Fatalf("mark issue done: %v", err)
	}
	taskID := fx.seedTerminalTask(t, pool, "completed", "shipped it")
	svc := runConfidenceService(pool, events.New(), stubMemoryLLM(t, `{"score":0.1,"rationale":"no evidence"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	if _, ok := loadTaskConfidence(t, pool, taskID); !ok {
		t.Fatal("the score must still be stored for a done issue")
	}
	if status := fx.issueStatus(t, pool); status != "done" {
		t.Errorf("issue status = %q, want done (untouched)", status)
	}
	if n := fx.confidenceInboxCount(t, pool); n != 0 {
		t.Errorf("inbox items = %d, want 0 for a done issue", n)
	}
}

func TestRunConfidenceDisabledSettingsStoresScoreOnly(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE workspace SET settings = '{"confidence_review":{"enabled":false,"threshold":0.5}}'::jsonb WHERE id = $1`, fx.workspaceID); err != nil {
		t.Fatalf("disable confidence review: %v", err)
	}
	taskID := fx.seedTerminalTask(t, pool, "completed", "maybe done?")
	bus := events.New()
	scored := collectEvents(bus, protocol.EventTaskScored)
	svc := runConfidenceService(pool, bus, stubMemoryLLM(t, `{"score":0.1,"rationale":"no evidence"}`))

	if err := svc.ScoreRunConfidence(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("score: %v", err)
	}

	conf, ok := loadTaskConfidence(t, pool, taskID)
	if !ok || !conf.BelowThreshold {
		t.Fatalf("confidence = %+v ok=%v, want the score stored even when the policy is off", conf, ok)
	}
	if len(*scored) != 1 {
		t.Errorf("task:scored events = %d, want 1 (scoring is independent of the policy)", len(*scored))
	}
	if status := fx.issueStatus(t, pool); status == "in_review" {
		t.Error("issue escalated while confidence_review.enabled = false")
	}
	if n := fx.confidenceInboxCount(t, pool); n != 0 {
		t.Errorf("inbox items = %d, want 0 while the policy is off", n)
	}
}

func TestParseConfidenceScore(t *testing.T) {
	score, rationale, err := parseConfidenceScore(`{"score":1.4,"rationale":"over the top"}`)
	if err != nil || score != 1 || rationale != "over the top" {
		t.Errorf("clamped = %v %q %v", score, rationale, err)
	}
	if _, _, err := parseConfidenceScore(`{"rationale":"no score"}`); err == nil {
		t.Error("a reply without a score must be rejected")
	}
	if _, _, err := parseConfidenceScore(`not json`); err == nil {
		t.Error("a malformed reply must be rejected")
	}
}
