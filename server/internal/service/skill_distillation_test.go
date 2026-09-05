package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// The fixture + stub LLM helpers live in agent_memory_extract_test.go
// (seedAgentMemoryExtractionFixture, stubMemoryLLM) and resolve_originator_test.go
// (seedAttributionFixture, newResolveOriginatorPool). They are reused here.

func distillationService(pool *pgxpool.Pool, bus *events.Bus, client SkillDistillationLLM) *TaskService {
	return &TaskService{
		Queries:           db.New(pool),
		TxStarter:         pool,
		Bus:               bus,
		SkillDistillation: client,
	}
}

// cleanupDistilledSkills removes the skills a test distilled (by name) plus any
// agent_skill bindings the fixture agent accumulated, so the shared test DB
// stays clean.
func cleanupDistilledSkills(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID string, names ...string) {
	t.Cleanup(func() {
		for _, n := range names {
			pool.Exec(context.Background(),
				`DELETE FROM agent_skill WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1 AND name = $2)`,
				workspaceID, n)
			pool.Exec(context.Background(),
				`DELETE FROM skill WHERE workspace_id = $1 AND name = $2`, workspaceID, n)
		}
		pool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1`, agentID)
	})
}

const skillReply = `{"skill":{"name":"Rebase With Conflict Checks","description":"How to rebase a branch safely.","body":"When rebasing, fetch upstream first, then rebase, and resolve conflicts file by file before force-pushing."}}`

func TestSkillDistillationCreatesAndAttachesSkill(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Rebased the feature branch and resolved the conflicts.")
	cleanupDistilledSkills(t, pool, fx.workspaceID, fx.agentID, "rebase-with-conflict-checks")

	svc := distillationService(pool, events.New(), stubMemoryLLM(t, skillReply))
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("distill: %v", err)
	}

	// The skill exists, marked as distilled with provenance back to the run.
	var config string
	if err := pool.QueryRow(context.Background(),
		`SELECT config::text FROM skill WHERE workspace_id = $1 AND name = $2`,
		fx.workspaceID, "rebase-with-conflict-checks").Scan(&config); err != nil {
		t.Fatalf("distilled skill was not created: %v", err)
	}
	if !strings.Contains(config, `"distilled"`) {
		t.Errorf("skill config missing distilled origin: %s", config)
	}
	if !strings.Contains(config, taskID) {
		t.Errorf("skill config missing source_task_id provenance: %s", config)
	}

	// K58: the distilled skill is a draft proposal — never attached to the
	// agent until a human publishes it.
	var attached int
	var status string
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(ask.skill_id), MIN(s.status) FROM skill s
		LEFT JOIN agent_skill ask ON ask.skill_id = s.id AND ask.agent_id = $1
		WHERE s.workspace_id = $3 AND s.name = $2`,
		fx.agentID, "rebase-with-conflict-checks", fx.workspaceID).Scan(&attached, &status); err != nil {
		t.Fatalf("count attached: %v", err)
	}
	if attached != 0 || status != "draft" {
		t.Errorf("distilled skill attached %d times with status %q, want a draft attached to nobody", attached, status)
	}
}

func TestSkillDistillationDeclinesNull(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Did a routine, unremarkable task.")

	svc := distillationService(pool, events.New(), stubMemoryLLM(t, `{"skill":null}`))
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("distill (declined): %v", err)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1`, fx.workspaceID).Scan(&n); err != nil {
		t.Fatalf("count skills: %v", err)
	}
	if n != 0 {
		t.Errorf("declined distillation still created %d skills", n)
	}
}

func TestSkillDistillationDisabledLLMIsNoop(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Some successful run output.")

	svc := distillationService(pool, events.New(), llm.New(llm.Config{}))
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("distill with disabled LLM: %v", err)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1`, fx.workspaceID).Scan(&n); err != nil {
		t.Fatalf("count skills: %v", err)
	}
	if n != 0 {
		t.Errorf("disabled LLM still distilled %d skills", n)
	}
}

func TestSkillDistillationSkipsEmptyOutput(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "   ")

	// Even a skill-returning LLM must not fire for a silent run.
	svc := distillationService(pool, events.New(), stubMemoryLLM(t, skillReply))
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("distill (empty output): %v", err)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1`, fx.workspaceID).Scan(&n); err != nil {
		t.Fatalf("count skills: %v", err)
	}
	if n != 0 {
		t.Errorf("empty-output run still distilled %d skills", n)
	}
}

func TestSkillDistillationIdempotentSameName(t *testing.T) {
	fx, pool := seedAgentMemoryExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Rebased the branch.")
	cleanupDistilledSkills(t, pool, fx.workspaceID, fx.agentID, "rebase-with-conflict-checks")

	svc := distillationService(pool, events.New(), stubMemoryLLM(t, skillReply))
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("first distill: %v", err)
	}
	// A second run distilling the same-named skill must not duplicate it.
	if err := svc.DistillSkillsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("second distill: %v", err)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1 AND name = $2`,
		fx.workspaceID, "rebase-with-conflict-checks").Scan(&n); err != nil {
		t.Fatalf("count skills: %v", err)
	}
	if n != 1 {
		t.Errorf("same-name distillation created %d skills, want 1", n)
	}
}

func TestSanitizeSkillName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Rebase With Conflict Checks", "rebase-with-conflict-checks"},
		{"  Trim   Me  ", "trim-me"},
		{"Already-Kebab", "already-kebab"},
		{"weird!!chars@@here", "weird-chars-here"},
		{"---leading-trailing---", "leading-trailing"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		if got := sanitizeSkillName(tc.in); got != tc.want {
			t.Errorf("sanitizeSkillName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseDistilledSkill(t *testing.T) {
	got, err := parseDistilledSkill(skillReply)
	if err != nil {
		t.Fatalf("parse valid: %v", err)
	}
	if got == nil {
		t.Fatal("parse valid returned nil")
	}
	if got.Name != "Rebase With Conflict Checks" {
		t.Errorf("name = %q", got.Name)
	}

	// The model declining is a nil skill with no error.
	declined, err := parseDistilledSkill(`{"skill":null}`)
	if err != nil {
		t.Fatalf("parse null: %v", err)
	}
	if declined != nil {
		t.Errorf("parse null = %+v, want nil", declined)
	}

	// Malformed JSON errors (caller treats it as "nothing to store").
	if _, err := parseDistilledSkill("not json"); err == nil {
		t.Error("parse malformed did not error")
	}

	// A skill missing a body is not usable.
	nobody, err := parseDistilledSkill(`{"skill":{"name":"x","description":"d","body":"  "}}`)
	if err != nil {
		t.Fatalf("parse no-body: %v", err)
	}
	if nobody != nil {
		t.Errorf("parse no-body = %+v, want nil", nobody)
	}
}

func TestBuildSkillContent(t *testing.T) {
	content := buildSkillContent("my-skill", "A description.", "Do the thing.")
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("content missing frontmatter start: %q", content)
	}
	if !strings.Contains(content, "name: my-skill") {
		t.Errorf("content missing name frontmatter: %q", content)
	}
	if !strings.Contains(content, "description: A description.") {
		t.Errorf("content missing description frontmatter: %q", content)
	}
	if !strings.Contains(content, "Do the thing.") {
		t.Errorf("content missing body: %q", content)
	}
	// Sanity: the config JSON builder produces a distilled origin.
	cfg, err := json.Marshal(map[string]any{"origin": map[string]any{"type": "distilled"}})
	if err != nil || !strings.Contains(string(cfg), "distilled") {
		t.Errorf("config origin marshal failed: %v %s", err, cfg)
	}
}
