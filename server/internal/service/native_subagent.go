package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	openai "github.com/openai/openai-go/v3"
)

// Native sub-agents (long tasks, brick 5). A run may delegate a bounded piece
// of work to an isolated loop: fresh context, the same tools minus delegate
// and ask_user, its own turn and time budget, its own task row (a 'subagent'
// leg of the parent's workflow, so transcript, usage and cost land where
// every run's do). Depth is one: a sub-agent cannot delegate. A run may start
// six sub-agents, three at a time.
//
// The report contract: a sub-agent's closing report must cite a receipt
// [rN] for every action it claims. Receipts are the sub-run's actual tool
// calls, numbered in order; the parent verifies the citations against that
// journal and hands the model the report together with the receipts, the
// citations that match nothing, and the actions the report never mentioned.
// A claim without a receipt is a claim, not a fact. deer-flow's
// report_contract was the model.

const (
	nativeSubagentMaxTurns      = 8
	nativeSubagentTimeout       = 5 * time.Minute
	nativeSubagentsPerRun       = 6
	nativeSubagentConcurrency   = 3
	nativeSubagentMaxEffectful  = 4
	nativeSubagentTaskCap       = 4000
	nativeSubagentContextCap    = 8000
	nativeSubagentReportCap     = 6000
	nativeSubagentReceiptArgCap = 160
)

// nativeRunBudget is shared by a run and its sub-agents: the effectful
// ceiling is the run's, whoever spends it, and so is the sub-agent count.
type nativeRunBudget struct {
	mu        sync.Mutex
	effectful int
	subagents int
}

// nativeReceipt is one tool call of a sub-run, as the parent verifies it.
type nativeReceipt struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	Args string `json:"args"`
	OK   bool   `json:"ok"`
}

// nativeSubagentReport is what the parent model receives.
type nativeSubagentReport struct {
	TaskID              string          `json:"task_id"`
	Status              string          `json:"status"`
	Report              string          `json:"report"`
	Receipts            []nativeReceipt `json:"receipts"`
	UnverifiedCitations []string        `json:"unverified_citations"`
	UncitedReceipts     []string        `json:"uncited_receipts"`
	Note                string          `json:"note,omitempty"`
}

var nativeCitationRe = regexp.MustCompile(`\[(r\d+)\]`)

// nativeDelegate runs one sub-agent to completion and returns its verified
// report. Called from the loop for each delegate call of a turn, up to
// nativeSubagentConcurrency at a time.
func (s *NativeAgentService) nativeDelegate(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	if tctx.depth > 0 {
		return nil, errors.New("a sub-agent cannot delegate; do the work yourself and report")
	}
	if tctx.issue == nil {
		return nil, errors.New("delegate needs the task's own issue")
	}
	taskText, _ := args["task"].(string)
	taskText = strings.TrimSpace(util.SanitizeTextForPostgres(taskText))
	if taskText == "" {
		return nil, errors.New("task is required: say what the sub-agent must do and what it must report")
	}
	taskText = clampString(taskText, nativeSubagentTaskCap)
	contextText, _ := args["context"].(string)
	contextText = clampString(strings.TrimSpace(util.SanitizeTextForPostgres(contextText)), nativeSubagentContextCap)

	tctx.budget.mu.Lock()
	if tctx.budget.subagents >= nativeSubagentsPerRun {
		tctx.budget.mu.Unlock()
		return nil, fmt.Errorf("this run's sub-agent budget (%d) is spent; finish with what you have", nativeSubagentsPerRun)
	}
	tctx.budget.subagents++
	tctx.budget.mu.Unlock()

	sub, err := s.Queries.CreateSubagentTask(ctx, db.CreateSubagentTaskParams{
		ID:                  dbid.NewV7(),
		AgentID:             tctx.agent.ID,
		IssueID:             tctx.issue.ID,
		RuntimeID:           tctx.task.RuntimeID,
		TriggerSummary:      pgtype.Text{String: "Sub-agent: " + clampString(taskText, 200), Valid: true},
		WorkflowRootTaskID:  WorkflowRoot(tctx.task),
		DelegatedFromTaskID: tctx.task.ID,
		AccountableUserID:   tctx.task.AccountableUserID,
		OriginatorUserID:    tctx.task.OriginatorUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("sub-agent could not be started: %w", err)
	}

	subctx, cancel := context.WithTimeout(ctx, nativeSubagentTimeout)
	defer cancel()
	sub2 := &nativeToolContext{task: sub, agent: tctx.agent, issue: tctx.issue, workspaceID: tctx.workspaceID, depth: 1, budget: tctx.budget}
	cx := newNativeContext(nativeSystemPrompt(tctx.agent)+"\n\n"+nativeSubagentSystemAddendum, nativeSubagentBrief(tctx, taskText, contextText))
	var usage nativeRunUsage
	report, runErr := s.runLoop(subctx, sub2, cx, nativeAgentToolSpecsFor(1), nativeSubagentMaxTurns, &usage)
	s.recordNativeUsage(ctx, sub.ID, usage)

	status, errText := "completed", ""
	if runErr != nil {
		status, errText = "failed", runErr.Error()
		if errors.Is(runErr, context.DeadlineExceeded) {
			errText = fmt.Sprintf("sub-agent timed out after %s", nativeSubagentTimeout)
		}
	}
	if report == "" {
		report = "Sub-agent stopped without a report."
	}
	report = clampString(report, nativeSubagentReportCap)
	s.writeNativeMessage(ctx, sub.ID, "text", "", report, nil)
	result, _ := json.Marshal(map[string]any{"summary": report, "receipts": sub2.receipts})
	if _, err := s.Queries.SettleSubagentTask(ctx, db.SettleSubagentTaskParams{
		ID: sub.ID, Status: status, Result: result, Error: pgtype.Text{String: errText, Valid: errText != ""},
	}); err != nil {
		slog.Warn("native sub-agent: settle failed", "task_id", util.UUIDToString(sub.ID), "error", err)
	}

	out := nativeVerifyReport(report, sub2.receipts)
	out.TaskID = util.UUIDToString(sub.ID)
	out.Status = status
	if errText != "" {
		out.Note = errText
	}
	return out, nil
}

// safeDelegate contains a panic in a sub-run: the parent gets an error
// result for that delegation and keeps going; the server keeps serving.
func (s *NativeAgentService) safeDelegate(ctx context.Context, tctx *nativeToolContext, args map[string]any) (out any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("native sub-agent: panicked", "task_id", util.UUIDToString(tctx.task.ID), "panic", rec)
			out, err = nil, fmt.Errorf("sub-agent crashed: %v", rec)
		}
	}()
	return s.nativeDelegate(ctx, tctx, args)
}

// nativeVerifyReport checks the report's citations against the sub-run's
// receipts: what it cites that never happened, and what happened that it
// never cites.
func nativeVerifyReport(report string, receipts []nativeReceipt) nativeSubagentReport {
	known := map[string]bool{}
	for _, r := range receipts {
		known[r.ID] = true
	}
	cited := map[string]bool{}
	var unverified []string
	for _, m := range nativeCitationRe.FindAllStringSubmatch(report, -1) {
		id := m[1]
		if cited[id] {
			continue
		}
		cited[id] = true
		if !known[id] {
			unverified = append(unverified, id)
		}
	}
	var uncited []string
	for _, r := range receipts {
		if !cited[r.ID] {
			uncited = append(uncited, r.ID)
		}
	}
	sort.Strings(unverified)
	if receipts == nil {
		receipts = []nativeReceipt{}
	}
	if unverified == nil {
		unverified = []string{}
	}
	if uncited == nil {
		uncited = []string{}
	}
	return nativeSubagentReport{Report: report, Receipts: receipts, UnverifiedCitations: unverified, UncitedReceipts: uncited}
}

// recordReceipt journals one tool call of a sub-run for the report contract.
func (t *nativeToolContext) recordReceipt(tool string, args []byte, ok bool) string {
	id := fmt.Sprintf("r%d", len(t.receipts)+1)
	t.receipts = append(t.receipts, nativeReceipt{ID: id, Tool: tool, Args: clampString(string(args), nativeSubagentReceiptArgCap), OK: ok})
	return id
}

const nativeSubagentSystemAddendum = `You are a sub-agent: a run delegated one bounded piece of work to you. Do only that piece, with the tools, then reply with a report for the run that delegated it.
Report contract: every action you claim (read, created, commented, changed) must cite the receipt of the tool call that did it, as [rN] — receipts are numbered in the order of your tool calls, r1 first. A claim without a receipt will be flagged as unverified. State what you could not do. You cannot delegate and you cannot ask the team; if you are blocked, say so in the report.`

func nativeSubagentBrief(parent *nativeToolContext, taskText, contextText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Delegated task on issue #%d (%s):\n%s\n", parent.issue.Number, nativeDataFence("issue title", parent.issue.Title), nativeDataFence("delegated task", taskText))
	if contextText != "" {
		b.WriteString("\nContext from the delegating run:\n" + nativeDataFence("delegation context", contextText) + "\n")
	}
	fmt.Fprintf(&b, "\nYou have at most %d tool-calling turns, %d state-changing calls and %s. Reply with your report when the piece is done or when you are blocked.\n", nativeSubagentMaxTurns, nativeSubagentMaxEffectful, nativeSubagentTimeout)
	return b.String()
}

// executeDelegateCalls runs the delegate calls of one turn, up to
// nativeSubagentConcurrency at a time, and returns their results in call
// order. The transcript is written from the loop's goroutine before and
// after, so message sequence numbers never race.
func (s *NativeAgentService) executeDelegateCalls(ctx context.Context, tctx *nativeToolContext, calls []openai.ChatCompletionMessageToolCallUnion) []string {
	results := make([]string, len(calls))
	argsOf := make([]map[string]any, len(calls))
	for i, call := range calls {
		var args map[string]any
		if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				args = nil
			}
		}
		argsOf[i] = args
		inputJSON, _ := json.Marshal(args)
		s.writeNativeMessage(ctx, tctx.task.ID, "tool_use", call.Function.Name, "", inputJSON)
	}
	sem := make(chan struct{}, nativeSubagentConcurrency)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var payload any
			out, err := s.safeDelegate(ctx, tctx, argsOf[i])
			payload = out
			if err != nil {
				payload = map[string]any{"error": err.Error()}
			}
			raw, merr := json.Marshal(payload)
			if merr != nil {
				raw = []byte(`{"error":"tool result could not be serialised"}`)
			}
			results[i] = string(raw)
		}(i)
	}
	wg.Wait()
	for i, call := range calls {
		s.writeToolResult(ctx, tctx.task.ID, call.Function.Name, results[i])
	}
	return results
}
