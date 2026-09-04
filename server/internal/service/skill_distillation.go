package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// compile-time guard: the production wiring hands this seam the shared
// *llm.Client; drift between the two interfaces must fail here, not at a
// runtime type assertion.
var _ SkillDistillationLLM = (*llm.Client)(nil)

// Skill distillation (k69): after a run completes successfully, distill the
// reusable technique it demonstrated into a real workspace skill and attach it
// to the agent. Because the claim path re-reads agent_skill bindings on every
// dispatch and materializes each skill as a SKILL.md the runtime discovers
// natively, a skill distilled on run N is live for run N+1 with no extra
// plumbing — the agent genuinely improves over time.
//
// Unlike the k68 postmortem (which scaffolds a deterministic artifact when no
// LLM is configured), a skill is only worth storing when it is genuinely
// distilled, so a disabled LLM simply turns this pass off.

const (
	skillDistillationTimeout = 45 * time.Second
	// skillDistillationMaxConcurrent bounds distillation passes in flight
	// process-wide; passes over the ceiling are dropped, not queued.
	skillDistillationMaxConcurrent = 4
	// skillDistillationOutputBudget bounds how much of the run's final output is
	// sent upstream (head + tail kept, mirroring memory extraction).
	skillDistillationOutputBudget = 6000
	// maxDistilledSkillsPerAgent caps how many auto-distilled skills one agent
	// accumulates, so the injected skill set cannot bloat every future run's
	// context without bound. Manual/imported skills are not counted.
	maxDistilledSkillsPerAgent = 30
	// skillNameMaxRunes bounds a distilled skill name.
	skillNameMaxRunes = 64
	skillDescMaxRunes = 500
)

// SkillDistillationLLM is the seam TaskService uses for the distillation pass,
// satisfied by *llm.Client. Same shape as AgentMemoryLLM/PostmortemLLM.
type SkillDistillationLLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// distilledSkill is the parsed LLM output. nil means "nothing worth
// distilling" and is a good, expected outcome for an ordinary run.
type distilledSkill struct {
	Name        string
	Description string
	Body        string
}

// SubscribeSkillDistillation wires the distillation pass onto the bus. The
// listener runs INLINE on the publisher's goroutine (Bus.Publish is
// synchronous), so it does the minimum — gate on the payload, hand off to a
// detached worker — and never touches the database or the network itself.
func (s *TaskService) SubscribeSkillDistillation(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		if s.SkillDistillation == nil || !s.SkillDistillation.Enabled() {
			return
		}
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Defense in depth: only true successes. The context-exhaustion
		// re-route already keeps pseudo-completions off this event.
		if status, _ := payload["status"].(string); status != "completed" {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		s.launchSkillDistillation(taskID)
	})
}

// launchSkillDistillation spawns the detached worker with two admission gates:
// one pass per task (a redelivered event must not race itself) and a
// process-wide ceiling.
func (s *TaskService) launchSkillDistillation(taskID pgtype.UUID) {
	key := util.UUIDToString(taskID)
	if _, inFlight := s.skillDistillationInFlight.LoadOrStore(key, struct{}{}); inFlight {
		return
	}
	if s.skillDistillationRunning.Add(1) > skillDistillationMaxConcurrent {
		s.skillDistillationRunning.Add(-1)
		s.skillDistillationInFlight.Delete(key)
		slog.Warn("skill distillation shed: process-wide concurrency ceiling reached",
			"ceiling", skillDistillationMaxConcurrent, "task_id", key)
		return
	}

	go func() {
		defer func() {
			s.skillDistillationRunning.Add(-1)
			s.skillDistillationInFlight.Delete(key)
		}()
		// Panic containment: this goroutine is detached from any request, so
		// nothing above it recovers. Distillation is a nicety — swallow, log.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("skill distillation panicked",
					"task_id", key, "panic", rec)
			}
		}()

		// The worker owns its context: the event publisher's is typically an
		// HTTP request context cancelled when the completion callback returns.
		ctx, cancel := context.WithTimeout(context.Background(), skillDistillationTimeout)
		defer cancel()

		if err := s.DistillSkillsForTask(ctx, taskID); err != nil {
			slog.Warn("skill distillation failed",
				"task_id", key, "error", err)
		}
	}()
}

// DistillSkillsForTask distills a reusable skill from a successfully completed
// task and attaches it to the agent. Synchronous and safe to call directly
// from tests; the async admission path is launchSkillDistillation.
func (s *TaskService) DistillSkillsForTask(ctx context.Context, taskID pgtype.UUID) error {
	if s.SkillDistillation == nil || !s.SkillDistillation.Enabled() {
		return nil
	}

	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The task row vanished between the event and the pass (workspace
			// teardown) — nothing to distill.
			return nil
		}
		return fmt.Errorf("load completed task: %w", err)
	}
	if task.Status != "completed" {
		return nil
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task agent: %w", err)
	}
	// System agents are product plumbing, not learners.
	if agent.SystemKey.String != "" {
		return nil
	}

	// The run's final output is where the demonstrated technique lives. It is
	// in agent_task_queue.result, not the event payload.
	var completed protocol.TaskCompletedPayload
	if len(task.Result) > 0 {
		if uerr := json.Unmarshal(task.Result, &completed); uerr != nil {
			return nil
		}
	}
	output := strings.TrimSpace(completed.Output)
	if output == "" {
		// A silent/no-output success demonstrates nothing reusable.
		return nil
	}

	// Cap: don't let auto-distilled skills bloat the agent's injected skill
	// set without bound. Manual/imported skills don't count.
	count, cerr := s.countDistilledSkills(ctx, agent.ID)
	if cerr != nil {
		return fmt.Errorf("count distilled skills: %w", cerr)
	}
	if count >= maxDistilledSkillsPerAgent {
		slog.Info("skill distillation skipped: agent at distilled-skill cap",
			"agent_id", util.UUIDToString(agent.ID), "cap", maxDistilledSkillsPerAgent)
		return nil
	}

	issueTitle := ""
	if task.IssueID.Valid {
		if issue, ierr := s.Queries.GetIssue(ctx, task.IssueID); ierr == nil {
			issueTitle = issue.Title
		}
	}

	raw, err := s.SkillDistillation.GenerateJSON(ctx,
		"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
		skillDistillationSystemPrompt,
		renderSkillDistillationPrompt(issueTitle, output),
		0.2,
		2048,
	)
	if err != nil {
		// Unlike the postmortem scaffold, a skill needs real distillation; an
		// LLM failure means nothing worth storing.
		slog.Warn("skill distillation LLM call failed, skipping", "error", err)
		return nil
	}
	distilled, perr := parseDistilledSkill(raw)
	if perr != nil || distilled == nil {
		// "skill": null (or a malformed reply) = nothing worth distilling.
		return nil
	}

	return s.createDistilledSkill(ctx, agent, task, distilled)
}

// createDistilledSkill writes the skill row, attaches it to the agent, and
// publishes the refresh events. Idempotent on (workspace_id, name): a skill
// with the same name already existing short-circuits rather than duplicating.
func (s *TaskService) createDistilledSkill(ctx context.Context, agent db.Agent, task db.AgentTaskQueue, d *distilledSkill) error {
	name := sanitizeSkillName(d.Name)
	if name == "" {
		return nil
	}

	// Idempotency: a same-named skill already exists (previously distilled or
	// human-created). Don't duplicate; leave the existing one authoritative.
	if existing, err := s.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: agent.WorkspaceID,
		Name:        name,
	}); err == nil && existing.ID.Valid {
		// Still make sure the agent has it attached (cheap, ON CONFLICT NOTHING).
		if aerr := s.Queries.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: existing.ID}); aerr != nil {
			slog.Warn("attach existing distilled skill failed", "skill", name, "error", aerr)
		}
		return nil
	}

	description := d.Description
	if utf8.RuneCountInString(description) > skillDescMaxRunes {
		description = string([]rune(description)[:skillDescMaxRunes])
	}

	origin := map[string]any{
		"type":           "distilled",
		"source_task_id": util.UUIDToString(task.ID),
		"agent_id":       util.UUIDToString(agent.ID),
	}
	if task.IssueID.Valid {
		origin["issue_id"] = util.UUIDToString(task.IssueID)
	}
	configJSON, err := json.Marshal(map[string]any{"origin": origin})
	if err != nil {
		configJSON = []byte(`{}`)
	}

	creator := task.AccountableUserID
	if !creator.Valid {
		creator = task.OriginatorUserID
	}
	if !creator.Valid {
		creator = agent.OwnerID
	}

	content := buildSkillContent(name, description, d.Body)
	skill, err := s.Queries.CreateSkill(ctx, db.CreateSkillParams{
		WorkspaceID: agent.WorkspaceID,
		Name:        name,
		Description: description,
		Content:     content,
		Config:      configJSON,
		CreatedBy:   creator,
	})
	if err != nil {
		// UNIQUE(workspace_id, name) race with a concurrent create — treat as
		// already-exists and just attach.
		if existing, gerr := s.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: agent.WorkspaceID,
			Name:        name,
		}); gerr == nil && existing.ID.Valid {
			skill = existing
		} else {
			return fmt.Errorf("create distilled skill: %w", err)
		}
	}

	if err := s.Queries.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: skill.ID}); err != nil {
		return fmt.Errorf("attach distilled skill: %w", err)
	}

	s.publishDistilledSkillEvents(ctx, agent)
	slog.Info("skill distilled from successful run",
		"skill", name,
		"agent_id", util.UUIDToString(agent.ID),
		"task_id", util.UUIDToString(task.ID))
	return nil
}

// publishDistilledSkillEvents refreshes the workspace skill list and the
// agent's skill tab. Both events are change hints — clients invalidate and
// refetch, so the payloads carry summaries for convenience, not authority.
func (s *TaskService) publishDistilledSkillEvents(ctx context.Context, agent db.Agent) {
	if s.Bus == nil {
		return
	}
	wsID := util.UUIDToString(agent.WorkspaceID)

	summaries, err := s.Queries.ListAgentSkillSummaries(ctx, agent.ID)
	if err != nil {
		// Still emit skill:created so the workspace list refreshes; the agent
		// tab will catch up on its next fetch.
		s.Bus.Publish(events.Event{
			Type:        protocol.EventSkillCreated,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload:     map[string]any{"agent_id": util.UUIDToString(agent.ID)},
		})
		return
	}

	skills := make([]map[string]any, 0, len(summaries))
	for _, sm := range summaries {
		enabled := sm.Enabled
		skills = append(skills, map[string]any{
			"id":           util.UUIDToString(sm.ID),
			"workspace_id": util.UUIDToString(sm.WorkspaceID),
			"name":         sm.Name,
			"description":  sm.Description,
			"config":       decodeSkillConfigJSON(sm.Config),
			"created_by":   uuidOrNil(sm.CreatedBy),
			"created_at":   formatTimestamptz(sm.CreatedAt),
			"updated_at":   formatTimestamptz(sm.UpdatedAt),
			"enabled":      &enabled,
		})
	}

	s.Bus.Publish(events.Event{
		Type:        protocol.EventSkillCreated,
		WorkspaceID: wsID,
		ActorType:   "system",
		Payload:     map[string]any{"agent_id": util.UUIDToString(agent.ID)},
	})
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: wsID,
		ActorType:   "system",
		Payload: map[string]any{
			"agent_id": util.UUIDToString(agent.ID),
			"skills":   skills,
		},
	})
}

// countDistilledSkills counts the agent's attached skills whose provenance is
// config.origin.type == "distilled". Only enabled bindings are considered,
// matching what actually gets injected into runs.
func (s *TaskService) countDistilledSkills(ctx context.Context, agentID pgtype.UUID) (int, error) {
	skills, err := s.Queries.ListAgentSkills(ctx, agentID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sk := range skills {
		var cfg struct {
			Origin struct {
				Type string `json:"type"`
			} `json:"origin"`
		}
		if len(sk.Config) > 0 {
			_ = json.Unmarshal(sk.Config, &cfg)
		}
		if cfg.Origin.Type == "distilled" {
			n++
		}
	}
	return n, nil
}

var skillNameInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeSkillName normalizes an LLM-proposed name into a valid workspace
// skill name: lowercased, runs of non-alphanumerics collapsed to a single
// hyphen, trimmed, length-bounded. Empty result means "not a usable name".
func sanitizeSkillName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = skillNameInvalidChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if utf8.RuneCountInString(s) > skillNameMaxRunes {
		s = string([]rune(s)[:skillNameMaxRunes])
		s = strings.Trim(s, "-")
	}
	return s
}

// buildSkillContent assembles the SKILL.md body stored in skill.content:
// YAML frontmatter (name, description) plus the distilled technique. The
// daemon's ensureSkillFrontmatter tolerates an existing frontmatter.
func buildSkillContent(name, description, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	if description != "" {
		b.WriteString("description: " + description + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

// decodeSkillConfigJSON turns the stored config JSONB into a generic value for
// event payloads; malformed or empty config becomes an empty object.
func decodeSkillConfigJSON(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}

func uuidOrNil(u pgtype.UUID) any {
	if !u.Valid {
		return nil
	}
	return util.UUIDToString(u)
}

func formatTimestamptz(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.Format(time.RFC3339Nano)
}
