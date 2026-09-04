package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// Agent memory extraction (JEF-236): after a run completes, a best-effort
// pass asks the deployment's assist-layer LLM for the durable facts the run
// revealed about the repo / workflow / tooling, and stores the new ones as
// source='run' rows in agent_memory. The whole path is a nicety: a disabled
// or failing LLM must cost the completed task nothing.

const (
	agentMemoryExtractionTimeout = 30 * time.Second
	// One pass yields at most this many facts, whatever the model returned.
	agentMemoryExtractionMaxFacts = 3
	// agentMemoryFactMaxRunes mirrors the REST/table limit so an over-long
	// model fact is dropped, never persisted and then rejected by the CHECK.
	agentMemoryFactMaxRunes = 500
	// agentMemoryExtractionOutputBudget bounds how much of the run's final
	// output is sent upstream. The pass looks for durable conventions, which
	// the head (what the run did) and tail (what it concluded) carry; the
	// middle of a long output is execution detail the prompt forbids anyway.
	agentMemoryExtractionOutputBudget = 6000
	// agentMemoryExtractionMaxConcurrent bounds extraction passes in flight
	// process-wide; passes over the ceiling are dropped, not queued — a fact
	// extracted minutes late would still write, but the spend is not worth a
	// backlog.
	agentMemoryExtractionMaxConcurrent = 8
	// agentMemoryMaxPerAgent mirrors the handler cap: after inserting, the
	// oldest source='run' rows are evicted until the agent is back under it.
	agentMemoryMaxPerAgent = 200
)

// AgentMemoryLLM is the seam TaskService uses for the post-run extraction
// pass, satisfied by *llm.Client. Same shape as ChatQuickActionsLLM: an
// interface so tests can drive the pass without an HTTP upstream.
type AgentMemoryLLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// SubscribeAgentMemoryExtraction wires the extraction pass onto the bus. The
// listener runs INLINE on the publisher's goroutine (Bus.Publish is
// synchronous), so it does the minimum — read the scope hints, gate, hand off
// to a detached worker — and never touches the database or the network itself.
// Bus.Publish already recovers listener panics; the worker recovers its own.
func (s *TaskService) SubscribeAgentMemoryExtraction(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		if s.MemoryExtraction == nil || !s.MemoryExtraction.Enabled() {
			return
		}
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Defense in depth: the subscription is to the completed event, but a
		// payload whose status disagrees (a repurposed publish) must not extract
		// from a run that did not actually succeed. Context-exhausted runs are
		// already re-routed to task:failed by the completion handler, so they
		// never reach this listener at all.
		if status, _ := payload["status"].(string); status != "completed" {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		agentIDRaw, _ := payload["agent_id"].(string)
		s.launchAgentMemoryExtraction(taskID, agentIDRaw)
	})
}

// launchAgentMemoryExtraction spawns the detached worker with the two
// admission gates: one pass per agent (concurrent completions of the same
// agent would race the same dedup read) and a process-wide ceiling.
func (s *TaskService) launchAgentMemoryExtraction(taskID pgtype.UUID, agentID string) {
	if agentID != "" {
		if _, inFlight := s.memoryExtractionInFlight.LoadOrStore(agentID, struct{}{}); inFlight {
			slog.Info("agent memory extraction skipped: pass already running for this agent",
				"agent_id", agentID, "task_id", util.UUIDToString(taskID))
			return
		}
	}
	if s.memoryExtractionRunning.Add(1) > agentMemoryExtractionMaxConcurrent {
		s.memoryExtractionRunning.Add(-1)
		if agentID != "" {
			s.memoryExtractionInFlight.Delete(agentID)
		}
		slog.Warn("agent memory extraction shed: process-wide concurrency ceiling reached",
			"ceiling", agentMemoryExtractionMaxConcurrent, "task_id", util.UUIDToString(taskID))
		return
	}

	go func() {
		defer func() {
			s.memoryExtractionRunning.Add(-1)
			if agentID != "" {
				s.memoryExtractionInFlight.Delete(agentID)
			}
		}()
		// Panic containment: this goroutine is detached from any request, so
		// nothing above it recovers. Extraction is a nicety — swallow, log.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("agent memory extraction panicked",
					"task_id", util.UUIDToString(taskID), "panic", rec)
			}
		}()

		// The worker owns its context: the event publisher's is typically an
		// HTTP request context cancelled when the completion callback returns.
		ctx, cancel := context.WithTimeout(context.Background(), agentMemoryExtractionTimeout)
		defer cancel()

		if err := s.ExtractAgentMemoriesForTask(ctx, taskID); err != nil {
			slog.Warn("agent memory extraction failed",
				"task_id", util.UUIDToString(taskID), "error", err)
		}
	}()
}

// ExtractAgentMemoriesForTask runs one extraction pass for a completed task:
// load the run's output, ask the LLM for durable facts, dedup against the
// agent's existing memory, insert the survivors as source='run', evict the
// oldest run-facts beyond the per-agent cap. Synchronous and safe to call
// directly from tests; the async admission path is launchAgentMemoryExtraction.
func (s *TaskService) ExtractAgentMemoriesForTask(ctx context.Context, taskID pgtype.UUID) error {
	if s.MemoryExtraction == nil || !s.MemoryExtraction.Enabled() {
		return nil
	}

	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The task row vanished between the event and the pass (workspace
			// teardown) — nothing to extract from, and nothing to report.
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
	// System agents run product-owned instruction layers; facts learned by
	// Mika or a builder carrier would be extracted from prompts the workspace
	// never wrote, so their memory stays human-free and empty.
	if agent.SystemKey.String != "" {
		return nil
	}

	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(task.Result, &payload); err != nil {
		return fmt.Errorf("decode task result: %w", err)
	}
	output := strings.TrimSpace(payload.Output)
	if output == "" {
		// A run with no final output (no_action leader turn, silent worker)
		// revealed nothing worth persisting.
		return nil
	}

	existing, err := s.Queries.ListAgentMemories(ctx, db.ListAgentMemoriesParams{
		AgentID:     agent.ID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("list existing agent memories: %w", err)
	}

	issueTitle := ""
	if task.IssueID.Valid {
		if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
			issueTitle = issue.Title
		}
	}

	raw, err := s.MemoryExtraction.GenerateJSON(ctx,
		"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
		agentMemoryExtractionSystemPrompt,
		renderAgentMemoryExtractionPrompt(issueTitle, output, existing),
		0.2,
		1024,
	)
	if err != nil {
		return fmt.Errorf("generate agent memory facts: %w", err)
	}

	facts := parseAgentMemoryFacts(raw)
	if len(facts) == 0 {
		return nil
	}

	known := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		known[normalizeAgentMemoryFact(m.Content)] = struct{}{}
	}

	inserted := 0
	for _, fact := range facts {
		if inserted >= agentMemoryExtractionMaxFacts {
			break
		}
		key := normalizeAgentMemoryFact(fact)
		if key == "" || utf8.RuneCountInString(fact) > agentMemoryFactMaxRunes {
			continue
		}
		if _, dup := known[key]; dup {
			continue
		}
		memory, err := s.Queries.CreateAgentMemory(ctx, db.CreateAgentMemoryParams{
			WorkspaceID:  agent.WorkspaceID,
			AgentID:      agent.ID,
			Content:      util.SanitizeTextForPostgres(fact),
			Source:       "run",
			SourceTaskID: task.ID,
		})
		if err != nil {
			return fmt.Errorf("insert extracted agent memory: %w", err)
		}
		known[key] = struct{}{}
		inserted++
		s.publishAgentMemoryEvent(protocol.EventAgentMemoryCreated, agent, memory)
	}

	if inserted > 0 {
		if err := s.Queries.DeleteOldestRunMemories(ctx, db.DeleteOldestRunMemoriesParams{
			AgentID:   agent.ID,
			KeepLimit: agentMemoryMaxPerAgent,
		}); err != nil {
			slog.Warn("agent memory eviction failed",
				"agent_id", util.UUIDToString(agent.ID), "error", err)
		}
	}
	return nil
}

func (s *TaskService) publishAgentMemoryEvent(eventType string, agent db.Agent, memory db.AgentMemory) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{"memory": map[string]any{
			"id":             util.UUIDToString(memory.ID),
			"workspace_id":   util.UUIDToString(memory.WorkspaceID),
			"agent_id":       util.UUIDToString(memory.AgentID),
			"content":        memory.Content,
			"source":         memory.Source,
			"source_task_id": util.UUIDToString(memory.SourceTaskID),
			"created_at":     memory.CreatedAt.Time.Format(time.RFC3339Nano),
			"updated_at":     memory.UpdatedAt.Time.Format(time.RFC3339Nano),
		}},
	})
}

// agentMemoryExtractionSystemPrompt is the stable instruction set for the
// pass. The word "JSON" must stay: response_format=json_object is rejected
// upstream without it (see llm.Client.GenerateJSON).
const agentMemoryExtractionSystemPrompt = `You extract durable memory facts for an AI coding agent from one of its completed runs.

A memory fact is something that will still be true and useful on the agent's NEXT task in this workspace: repository conventions, tooling and package-manager choices, build/test commands, workflow rules, naming or architecture conventions.

Rules:
- Return 0 to 3 facts. Returning none is a good outcome for an ordinary run.
- Each fact is a single sentence, at most 500 characters, written as a durable statement ("This repo uses pnpm, never npm.").
- NEVER include secrets, credentials, tokens, file contents, or personal data.
- NEVER include ephemeral task details: issue numbers, branch names, PR links, dates, statuses, or what this specific task did.
- Do not restate what the agent's own instructions or the existing memory already say.

Output JSON only, exactly this shape:
{"facts":["..."]}
No prose, no markdown, no code fences.`

// renderAgentMemoryExtractionPrompt builds the per-call user message: the
// issue the run worked on, its final output (bounded), and the agent's
// existing facts so the model can avoid restating them. Server-side dedup
// still runs on the answer; the list exists to save the spend, not to
// guarantee uniqueness.
func renderAgentMemoryExtractionPrompt(issueTitle, output string, existing []db.AgentMemory) string {
	var b strings.Builder
	if strings.TrimSpace(issueTitle) != "" {
		fmt.Fprintf(&b, "The run worked on the issue titled: %s\n\n", issueTitle)
	}
	b.WriteString("FINAL OUTPUT OF THE RUN:\n")
	b.WriteString(truncateAgentMemoryOutput(output))
	b.WriteString("\n\nEXISTING MEMORY (do not restate):\n")
	if len(existing) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, m := range existing {
			fmt.Fprintf(&b, "- %s\n", m.Content)
		}
	}
	b.WriteString("\nExtract the durable facts from this run.")
	return b.String()
}

// truncateAgentMemoryOutput keeps both ends of a long output: the head says
// what the run set out to do, the tail says what it concluded.
func truncateAgentMemoryOutput(output string) string {
	runes := []rune(output)
	if len(runes) <= agentMemoryExtractionOutputBudget {
		return output
	}
	head := string(runes[:agentMemoryExtractionOutputBudget/2])
	tail := string(runes[len(runes)-agentMemoryExtractionOutputBudget/2:])
	return head + "\n…[truncated]…\n" + tail
}

// parseAgentMemoryFacts decodes the model's {"facts":[...]} reply. Anything
// that is not that shape yields nil — a malformed pass costs nothing.
func parseAgentMemoryFacts(raw string) []string {
	var decoded struct {
		Facts []string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	facts := make([]string, 0, len(decoded.Facts))
	for _, f := range decoded.Facts {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			facts = append(facts, trimmed)
		}
	}
	return facts
}

// normalizeAgentMemoryFact is the dedup key: case- and whitespace-insensitive
// so "Uses pnpm." and "uses pnpm." do not both persist.
func normalizeAgentMemoryFact(content string) string {
	return strings.ToLower(strings.Join(strings.Fields(content), " "))
}

// compile-time guard: the production wiring hands this seam the shared
// *llm.Client; drift between the two interfaces must fail here, not at a
// runtime type assertion.
var _ AgentMemoryLLM = (*llm.Client)(nil)
