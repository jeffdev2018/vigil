package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	openai "github.com/openai/openai-go/v3"
)

// Native runtime (rowboat borrow, lot A): agents that run in-memory inside the
// Go server instead of through a CLI on a daemon. The server ticks a
// native_agent job that heartbeats the per-workspace native agent_runtime row
// (claim eligibility works exactly like a daemon poll), claims queued tasks
// through the ordinary TaskService path, and executes them with a bounded
// tool-calling loop over the internal LLM layer. Tool calls and the final
// answer land in task_message (tool_use / tool_result / text), so the existing
// run transcript UI renders a native run with zero frontend changes.

// NativeAgentLLM is the slice of *llm.Client the loop needs. An interface so
// tests can drive the loop with a scripted model.
type NativeAgentLLM interface {
	Enabled() bool
	Chat(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
}

const (
	// nativeMaxTurns bounds the tool-calling loop. Generous enough for a real
	// investigation, tight enough that a confused model cannot burn the task.
	nativeMaxTurns = 12
	// nativeRunTimeout bounds one whole run, model calls included.
	nativeRunTimeout = 10 * time.Minute
	// nativeMaxConcurrent bounds in-flight native runs across the whole
	// server. ponytail: a single global cap, not per-workspace fairness —
	// revisit if a busy workspace can starve another one.
	nativeMaxConcurrent = 8
	// nativeBriefComments caps how many recent comments ride the brief.
	nativeBriefComments = 20
	// nativeToolResultCap bounds one tool result before it goes back into the
	// model context (bytes, approximate).
	nativeToolResultCap = 8 * 1024
	// nativeCommentMaxLen bounds an agent-authored comment (characters).
	nativeCommentMaxLen = 30000
)

type NativeAgentService struct {
	Queries *db.Queries
	Tasks   *TaskService
	Issues  *IssueService
	LLM     NativeAgentLLM
	// Bus carries the realtime nudge for the agent's own writes (comments,
	// issue edits, held transitions). Nil skips publishing — rows remain the
	// source of truth and open clients converge on their next fetch.
	Bus     *events.Bus
	limiter chan struct{}
}

func NewNativeAgentService(q *db.Queries, tasks *TaskService, issues *IssueService, llm NativeAgentLLM, bus *events.Bus) *NativeAgentService {
	return &NativeAgentService{
		Queries: q,
		Tasks:   tasks,
		Issues:  issues,
		LLM:     llm,
		Bus:     bus,
		limiter: make(chan struct{}, nativeMaxConcurrent),
	}
}

// Tick seeds + heartbeats the native runtime rows, then claims and dispatches
// as many queued tasks as the concurrency cap allows. It returns the number of
// runs dispatched this tick.
func (s *NativeAgentService) Tick(ctx context.Context) (int, error) {
	// Inert without a configured model — same contract as every other
	// LLM-backed server feature: disabled means off, not failing.
	if s.LLM == nil || !s.LLM.Enabled() {
		return 0, nil
	}
	if _, err := s.Queries.SeedNativeRuntimes(ctx); err != nil {
		return 0, fmt.Errorf("native runtime seed: %w", err)
	}
	if _, err := s.Queries.HeartbeatNativeRuntimes(ctx); err != nil {
		return 0, fmt.Errorf("native runtime heartbeat: %w", err)
	}
	runtimes, err := s.Queries.ListNativeRuntimes(ctx)
	if err != nil {
		return 0, fmt.Errorf("native runtime list: %w", err)
	}
	dispatched := 0
	for _, rt := range runtimes {
		for {
			// Reserve the slot BEFORE claiming: a claimed task with no
			// executor would sit dispatched until stale reclaim.
			select {
			case s.limiter <- struct{}{}:
			default:
				return dispatched, nil
			}
			task, err := s.Tasks.ClaimTaskForRuntime(ctx, rt.ID)
			if err != nil {
				<-s.limiter
				return dispatched, fmt.Errorf("native claim: %w", err)
			}
			if task == nil {
				<-s.limiter
				break
			}
			dispatched++
			go func(task db.AgentTaskQueue) {
				defer func() { <-s.limiter }()
				// The job's ctx dies with the tick; the run owns its own
				// lifetime, detached from the scheduler's request.
				runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nativeRunTimeout)
				defer cancel()
				s.runTask(runCtx, task)
			}(*task)
		}
	}
	return dispatched, nil
}

// runTask executes one claimed task to completion. Every exit path settles the
// task row: CompleteTask on an answer (even a bounded one), FailTask on
// infrastructure trouble.
func (s *NativeAgentService) runTask(ctx context.Context, task db.AgentTaskQueue) {
	taskID := task.ID
	if _, err := s.Tasks.StartTask(ctx, taskID); err != nil {
		slog.Error("native run: start failed", "task_id", util.UUIDToString(taskID), "error", err)
		s.failNativeTask(ctx, task, "native run could not start: "+err.Error())
		return
	}
	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		s.failNativeTask(ctx, task, "native run: assigned agent no longer exists")
		return
	}
	brief, ownIssue, briefErr := s.nativeBriefForTask(ctx, task, agent)
	if briefErr != nil {
		s.failNativeTask(ctx, task, briefErr.Error())
		return
	}

	tctx := nativeToolContext{task: task, agent: agent, issue: ownIssue, workspaceID: agent.WorkspaceID}
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(nativeSystemPrompt(agent)),
		openai.UserMessage(brief),
	}
	tools := nativeAgentToolSpecs()

	var finalText string
	for turn := 0; turn < nativeMaxTurns; turn++ {
		params := openai.ChatCompletionNewParams{
			Messages: messages,
			Tools:    tools,
		}
		completion, err := s.LLM.Chat(ctx, params)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				s.failNativeTask(ctx, task, "native run timed out")
			} else {
				s.failNativeTask(ctx, task, "model call failed: "+err.Error())
			}
			return
		}
		if len(completion.Choices) == 0 {
			s.failNativeTask(ctx, task, "model returned no choices")
			return
		}
		msg := completion.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText = strings.TrimSpace(msg.Content)
			break
		}
		messages = append(messages, msg.ToParam())
		for _, call := range msg.ToolCalls {
			result := s.executeNativeToolCall(ctx, tctx, call)
			messages = append(messages, openai.ToolMessage(nativeClampToolResult(result), call.ID))
		}
	}

	if finalText == "" {
		// Turn budget exhausted without a closing answer: settle rather than
		// retry-loop. The transcript already carries what was done.
		finalText = "Run stopped after reaching the tool-call turn limit without a final answer."
	}
	s.writeNativeMessage(ctx, taskID, "text", "", finalText, nil)

	result, _ := json.Marshal(map[string]any{"summary": finalText})
	if _, err := s.Tasks.CompleteTask(ctx, taskID, result, "", "", "", false, "", ""); err != nil {
		slog.Error("native run: complete failed", "task_id", util.UUIDToString(taskID), "error", err)
	}
}

// executeNativeToolCall runs one tool call, journals it in the transcript, and
// returns the JSON-marshalled result for the model. Tool errors are reported
// to the model (it may correct itself), never to the run.
func (s *NativeAgentService) executeNativeToolCall(ctx context.Context, tctx nativeToolContext, call openai.ChatCompletionMessageToolCallUnion) string {
	name := call.Function.Name
	var args map[string]any
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			args = nil
		}
	}
	inputJSON, _ := json.Marshal(args)
	s.writeNativeMessage(ctx, tctx.task.ID, "tool_use", name, "", inputJSON)

	out, err := s.callNativeTool(ctx, tctx, name, args)
	var payload any = out
	if err != nil {
		payload = map[string]any{"error": err.Error()}
	}
	raw, merr := json.Marshal(payload)
	if merr != nil {
		raw = []byte(`{"error":"tool result could not be serialised"}`)
	}
	s.writeNativeMessage(ctx, tctx.task.ID, "tool_result", name, string(raw), nil)
	return string(raw)
}

// writeNativeMessage appends one transcript row. Failures are logged, not
// fatal: a lost transcript row must not kill the run that produced it.
func (s *NativeAgentService) writeNativeMessage(ctx context.Context, taskID pgtype.UUID, kind, tool, content string, input []byte) {
	if content != "" {
		content = util.SanitizeTextForPostgres(content)
	}
	seq, err := s.Queries.NextTaskMessageSeq(ctx, taskID)
	if err != nil {
		slog.Warn("native run: seq lookup failed", "task_id", util.UUIDToString(taskID), "error", err)
		return
	}
	if _, err := s.Queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
		ID:      dbid.NewV7(),
		TaskID:  taskID,
		Seq:     int32(seq),
		Type:    kind,
		Tool:    textOrNull(tool),
		Content: textOrNull(content),
		Input:   input,
	}); err != nil {
		slog.Warn("native run: transcript write failed", "task_id", util.UUIDToString(taskID), "error", err)
	}
}

func (s *NativeAgentService) failNativeTask(ctx context.Context, task db.AgentTaskQueue, message string) {
	if _, err := s.Tasks.FailTask(ctx, task.ID, message, "", "", "", "agent_error", false, "", ""); err != nil {
		slog.Error("native run: fail settle failed", "task_id", util.UUIDToString(task.ID), "error", err)
	}
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func nativeClampToolResult(s string) string {
	if len(s) <= nativeToolResultCap {
		return s
	}
	return s[:nativeToolResultCap] + `…{"error":"tool result truncated for context"}`
}

// nativeSystemPrompt states the agent's contract. Instructions from the agent
// row ride along so a workspace's custom agent keeps its voice.
func nativeSystemPrompt(agent db.Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, an agent working inside a task management workspace.\n", agent.Name)
	b.WriteString("You operate on issues through the provided tools only. Do not invent issue ids, numbers, or names — look them up.\n")
	b.WriteString("When you have done what the task asked, reply with a short final answer in the task's language; it becomes the run summary.\n")
	if strings.TrimSpace(agent.Instructions) != "" {
		b.WriteString("\nWorkspace instructions for you:\n" + agent.Instructions + "\n")
	}
	return b.String()
}

// nativeBriefForTask assembles the user message for whatever kind of task
// this is, and resolves the task's own issue (nil for the issue-less kinds).
// Every kind is honest: a task whose context cannot be loaded fails the run
// with the reason, rather than executing against a guess.
func (s *NativeAgentService) nativeBriefForTask(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) (string, *db.Issue, error) {
	switch {
	case task.ChatSessionID.Valid:
		msgs, err := s.Queries.ListChatMessages(ctx, task.ChatSessionID)
		if err != nil {
			return "", nil, fmt.Errorf("native run: chat history unavailable: %w", err)
		}
		var b strings.Builder
		b.WriteString("You are replying in a chat conversation. Answer the user's last message; you may use the tools to look at or file issues first.\n\nConversation (oldest first):\n")
		kept := msgs
		if len(kept) > 30 {
			kept = kept[len(kept)-30:]
		}
		for _, m := range kept {
			fmt.Fprintf(&b, "- [%s] %s\n", m.Role, clampString(m.Content, 2000))
		}
		return b.String(), nil, nil

	case task.AutopilotRunID.Valid:
		run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID)
		if err != nil {
			return "", nil, fmt.Errorf("native run: autopilot run unavailable")
		}
		ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
		if err != nil {
			return "", nil, fmt.Errorf("native run: autopilot unavailable")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Autopilot task: %s\n", ap.Title)
		if ap.Description.Valid && strings.TrimSpace(ap.Description.String) != "" {
			b.WriteString("\nInstructions:\n" + ap.Description.String + "\n")
		}
		if len(run.TriggerPayload) > 0 {
			fmt.Fprintf(&b, "\nTrigger payload:\n%s\n", clampString(string(run.TriggerPayload), 2000))
		}
		if task.TriggerSummary.Valid && task.TriggerSummary.String != "" {
			b.WriteString("\nTrigger: " + task.TriggerSummary.String + "\n")
		}
		return b.String(), nil, nil

	case task.Context != nil && !task.IssueID.Valid:
		var qc QuickCreateContext
		if err := json.Unmarshal(task.Context, &qc); err != nil || qc.Type != QuickCreateContextType {
			return "", nil, errors.New("native run: unsupported task kind (no issue, chat, autopilot, or quick-create context)")
		}
		var b strings.Builder
		b.WriteString("Quick-create task: turn the following request into a well-formed issue (create_issue), then summarize what you filed.\n\nRequest:\n" + qc.Prompt + "\n")
		return b.String(), nil, nil

	case task.IssueID.Valid:
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: agent.WorkspaceID})
		if err != nil {
			return "", nil, errors.New("native run: task issue no longer exists")
		}
		tctx := nativeToolContext{task: task, agent: agent, issue: &issue, workspaceID: agent.WorkspaceID}
		return nativeTaskBrief(ctx, s.Queries, tctx), &issue, nil

	default:
		return "", nil, errors.New("native run: unsupported task kind (no issue, chat, autopilot, or quick-create context)")
	}
}

// nativeTaskBrief assembles the user message for an issue task: the issue and
// the recent conversation around it.
func nativeTaskBrief(ctx context.Context, q *db.Queries, tctx nativeToolContext) string {
	issue := *tctx.issue
	var b strings.Builder
	fmt.Fprintf(&b, "Issue #%d: %s\n", issue.Number, issue.Title)
	fmt.Fprintf(&b, "Status: %s\n", issue.Status)
	if issue.Priority != "" {
		fmt.Fprintf(&b, "Priority: %s\n", issue.Priority)
	}
	if issue.Description.Valid && strings.TrimSpace(issue.Description.String) != "" {
		b.WriteString("\nDescription:\n" + issue.Description.String + "\n")
	}
	comments, err := q.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: tctx.workspaceID,
		Limit:       nativeBriefComments,
	})
	if err == nil && len(comments) > 0 {
		b.WriteString("\nRecent comments (oldest first):\n")
		for _, c := range comments {
			author := c.AuthorType
			if c.AuthorID.Valid {
				author += " " + util.UUIDToString(c.AuthorID)
			}
			content := c.Content
			if len(content) > 2000 {
				content = content[:2000] + "…"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", author, content)
		}
	}
	if taskText := taskPromptText(tctx.task); taskText != "" {
		b.WriteString("\nTask:\n" + taskText + "\n")
	}
	return b.String()
}
