package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/goalstate"
	"github.com/multica-ai/multica/server/pkg/protocol"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// The native runtime's tool set (lot A): a deliberately closed, small list of
// issue operations. Everything executes with the agent's own authorship, so
// the workspace sees exactly which agent acted. Status changes are
// intentionally absent — they ride the F28 transition rules in a later lot,
// not a bare UPDATE.
//
// All arguments come from an LLM, i.e. an untrusted boundary: every tool
// re-resolves ids against the task's workspace and whitelists enum values.

type nativeToolContext struct {
	task  db.AgentTaskQueue
	agent db.Agent
	// effectful counts this run's state-changing tool calls against
	// nativeMaxEffectfulActions.
	effectful int
	// textStreamed (N04): the closing text was grown in place by the stream;
	// the caller must not write a second copy. streamedMsgID names the row.
	textStreamed  bool
	streamedMsgID pgtype.UUID
	// orgDenies (N08): the deny list of the org unit holding this run's
	// issue. A tool a denied verb matches as NEVER is absent from the specs
	// and refused at dispatch.
	orgDenies []string
	// repeats counts identical tool calls (name + canonical arguments) so a
	// model stuck re-issuing the same call is warned, then refused.
	repeats map[string]int
	// wrapUp asks the loop to spend its next turn on a closing status
	// instead of more tools; wrapUpReason says why, for the model and the
	// transcript.
	wrapUp       bool
	wrapUpReason string
	// issue is the task's own issue, nil for the issue-less kinds (chat,
	// quick-create, autopilot run-only). Tools that default to "the task's
	// issue" require an explicit issue_id when it is nil.
	issue       *db.Issue
	workspaceID pgtype.UUID
	// depth is 0 for a run, 1 for a sub-agent it delegated to; budget is
	// shared down the tree (effectful ceiling, sub-agent count); receipts
	// are a sub-run's journal for the report contract.
	depth    int
	budget   *nativeRunBudget
	receipts []nativeReceipt
}

// chargeEffectful counts one state-changing call against the caller's own
// ceiling and the run's shared one. Non-empty when a ceiling is crossed:
// the reason for the wrap-up.
func (t *nativeToolContext) chargeEffectful() string {
	t.effectful++
	own := nativeMaxEffectfulActions
	if t.depth > 0 {
		own = nativeSubagentMaxEffectful
	}
	if t.effectful > own {
		return fmt.Sprintf("the effectful-action budget (%d state-changing calls) is spent", own)
	}
	if t.budget != nil {
		t.budget.mu.Lock()
		t.budget.effectful++
		n := t.budget.effectful
		t.budget.mu.Unlock()
		if n > nativeMaxEffectfulActions {
			return fmt.Sprintf("the run's effectful-action budget (%d state-changing calls, sub-agents included) is spent", nativeMaxEffectfulActions)
		}
	}
	return ""
}

// requestWrapUp flags the run for a closing turn. The first reason wins:
// it is the one that actually ended the work.
func (t *nativeToolContext) requestWrapUp(reason string) {
	if t.wrapUp {
		return
	}
	t.wrapUp = true
	t.wrapUpReason = reason
}

var nativeIssuePriorities = []string{"urgent", "high", "medium", "low", "none"}

// nativeAgentToolSpecsFor is the tool list for a run at the given depth: a
// sub-agent gets neither delegate (depth is one) nor ask_user (it reports
// to its run, not to the team).
func nativeAgentToolSpecsFor(depth int) []openai.ChatCompletionToolUnionParam {
	all := nativeAgentToolSpecs()
	if depth == 0 {
		return all
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(all))
	for _, t := range all {
		if t.OfFunction != nil && (t.OfFunction.Function.Name == "delegate" || t.OfFunction.Function.Name == "ask_user") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func nativeAgentToolSpecs() []openai.ChatCompletionToolUnionParam {
	return []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "get_issue",
			Description: openai.String("Read one issue with its recent comments. Omit issue_id to read the task's own issue."),
			Parameters: shared.FunctionParameters{
				"type":       "object",
				"properties": shared.FunctionParameters{"issue_id": shared.FunctionParameters{"type": "string"}},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "list_issues",
			Description: openai.String("List issues in the workspace, newest first, optionally filtered by status or priority."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"status":   shared.FunctionParameters{"type": "string"},
					"priority": shared.FunctionParameters{"type": "string"},
					"limit":    shared.FunctionParameters{"type": "integer"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "add_comment",
			Description: openai.String("Post a comment as yourself on an issue (the task's own issue by default)."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"content"},
				"properties": shared.FunctionParameters{
					"content":  shared.FunctionParameters{"type": "string"},
					"issue_id": shared.FunctionParameters{"type": "string"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "update_issue",
			Description: openai.String("Update the title, description, or priority of an issue (the task's own issue by default). To change status, use transition_issue."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"issue_id":    shared.FunctionParameters{"type": "string"},
					"title":       shared.FunctionParameters{"type": "string"},
					"description": shared.FunctionParameters{"type": "string"},
					"priority":    shared.FunctionParameters{"type": "string"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "transition_issue",
			Description: openai.String("Move an issue to another status (the task's own issue by default). Workspace transition rules apply: the move may be refused, or held for human approval — in that case a request is filed and nothing changes yet."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"status"},
				"properties": shared.FunctionParameters{
					"issue_id": shared.FunctionParameters{"type": "string"},
					"status":   shared.FunctionParameters{"type": "string", "description": "Target status key"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "create_sub_issue",
			Description: openai.String("Create a child issue under the task's own issue, authored by you."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"title"},
				"properties": shared.FunctionParameters{
					"title":       shared.FunctionParameters{"type": "string"},
					"description": shared.FunctionParameters{"type": "string"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "search_workspace",
			Description: openai.String("Search the whole workspace by text: matching issues (open or closed) and knowledge notes, in one call. Use it to find past requests, decisions or procedures by words, names or numbers."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"query"},
				"properties": shared.FunctionParameters{
					"query": shared.FunctionParameters{"type": "string", "description": "Words to look for"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "search_notes",
			Description: openai.String("Search the workspace's shared knowledge (notes): full-text query and/or a single tag. This is where procedures, decisions and know-how live — check it before answering or filing."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"query": shared.FunctionParameters{"type": "string"},
					"tag":   shared.FunctionParameters{"type": "string"},
					"limit": shared.FunctionParameters{"type": "integer"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "get_note",
			Description: openai.String("Read one knowledge note in full, by id."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"note_id"},
				"properties": shared.FunctionParameters{
					"note_id": shared.FunctionParameters{"type": "string"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "save_note",
			Description: openai.String("Save a note into the workspace's shared knowledge — a procedure, an answer worth keeping, a decision. Keep the title short and specific."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"title"},
				"properties": shared.FunctionParameters{
					"title":   shared.FunctionParameters{"type": "string"},
					"content": shared.FunctionParameters{"type": "string"},
					"tags":    shared.FunctionParameters{"type": "array", "items": shared.FunctionParameters{"type": "string"}},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "update_note",
			Description: openai.String("Edit an existing knowledge note (title, content, or tags) by id."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"note_id"},
				"properties": shared.FunctionParameters{
					"note_id": shared.FunctionParameters{"type": "string"},
					"title":   shared.FunctionParameters{"type": "string"},
					"content": shared.FunctionParameters{"type": "string"},
					"tags":    shared.FunctionParameters{"type": "array", "items": shared.FunctionParameters{"type": "string"}},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "ask_user",
			Description: openai.String("Ask the team a question you cannot answer yourself (a decision, a missing fact, a permission). The run ends after this call; the goal loop waits for the answer and a follow-up run receives it. kind is text (free answer) or choice (pick one of options)."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"question": shared.FunctionParameters{"type": "string"},
					"kind":     shared.FunctionParameters{"type": "string", "enum": []string{"text", "choice"}},
					"options":  shared.FunctionParameters{"type": "array", "items": shared.FunctionParameters{"type": "string"}},
				},
				"required": []string{"question"},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "delegate",
			Description: openai.String("Hand one bounded piece of this task to a sub-agent with a fresh context: say exactly what to do and what to report. It runs with the same tools (minus delegate and ask_user), its own turn and time budget, and returns a report whose claims cite receipts [rN] of its tool calls; the result tells you which citations were verified. Up to 6 sub-agents per run, 3 at a time. Use it for independent reads or drafts, not for the decision that is yours."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"task":    shared.FunctionParameters{"type": "string"},
					"context": shared.FunctionParameters{"type": "string"},
				},
				"required": []string{"task"},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "create_issue",
			Description: openai.String("File a new top-level issue in the workspace, authored by you. It starts in the default status and is assigned to you unless assign_to_self is false."),
			Parameters: shared.FunctionParameters{
				"type":     "object",
				"required": []string{"title"},
				"properties": shared.FunctionParameters{
					"title":          shared.FunctionParameters{"type": "string"},
					"description":    shared.FunctionParameters{"type": "string"},
					"priority":       shared.FunctionParameters{"type": "string"},
					"assign_to_self": shared.FunctionParameters{"type": "boolean"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "list_events",
			Description: openai.String("Calendar events of the workspace overlapping a window (RFC 3339 from/to, default the coming week), with participants and status. Use agenda=true to also get issues due, cycles and meetings."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"from":   shared.FunctionParameters{"type": "string"},
					"to":     shared.FunctionParameters{"type": "string"},
					"agenda": shared.FunctionParameters{"type": "boolean"},
				},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "find_slot",
			Description: openai.String("Free windows every participant can make (members inside 09:00-18:00 weekdays in tz, agents any time), earliest first. participants: [\"member:<user id>\", \"agent:<agent id>\"]."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"participants":     shared.FunctionParameters{"type": "array", "items": shared.FunctionParameters{"type": "string"}},
					"duration_minutes": shared.FunctionParameters{"type": "integer"},
					"from":             shared.FunctionParameters{"type": "string"},
					"to":               shared.FunctionParameters{"type": "string"},
					"tz":               shared.FunctionParameters{"type": "string"},
				},
				"required": []string{"participants"},
			},
		}),
		openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        "propose_event",
			Description: openai.String("Propose a calendar event on this task's issue. It is filed as proposed with a Decision Card; a person accepts or declines. Times RFC 3339; participants: [{type: member|agent, id}]."),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": shared.FunctionParameters{
					"title":        shared.FunctionParameters{"type": "string"},
					"starts_at":    shared.FunctionParameters{"type": "string"},
					"ends_at":      shared.FunctionParameters{"type": "string"},
					"description":  shared.FunctionParameters{"type": "string"},
					"timezone":     shared.FunctionParameters{"type": "string"},
					"location":     shared.FunctionParameters{"type": "string"},
					"participants": shared.FunctionParameters{"type": "array", "items": shared.FunctionParameters{"type": "object"}},
				},
				"required": []string{"title", "starts_at", "ends_at"},
			},
		}),
	}
}

// nativeFilterToolSpecs removes the tools the org unit denies outright (N08).
// The description rides along because OrgDenyClass matches it too — the same
// rule the CLI catalogue applies (mcpgov.ApplyOrgDeny).
func nativeFilterToolSpecs(specs []openai.ChatCompletionToolUnionParam, denies []string) []openai.ChatCompletionToolUnionParam {
	if len(denies) == 0 {
		return specs
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		name := ""
		description := ""
		// The union carries the function definition through its variant
		// fields; both are plain values on the ChatCompletionFunctionToolParam
		// built here, and the openai.String helper made Description a
		// param.Opt whose string form is the description.
		if fn := spec.GetFunction(); fn != nil {
			name = fn.Name
			description = fn.Description.Value
		}
		if nativeToolDeniedByOrg(name, description, denies) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// callNativeToolRead dispatches one validated tool invocation. Errors are
// values for the model to react to, not run failures. Exported for tests.
func (s *NativeAgentService) callNativeToolRead(ctx context.Context, tctx *nativeToolContext, name string, args map[string]any) (any, error) {
	return s.callNativeTool(ctx, tctx, name, args)
}

// callNativeTool dispatches one validated tool invocation. Errors are values
// for the model to react to, not run failures.
func (s *NativeAgentService) callNativeTool(ctx context.Context, tctx *nativeToolContext, name string, args map[string]any) (any, error) {
	if nativeToolDeniedByOrg(name, name, tctx.orgDenies) {
		return nil, fmt.Errorf("the organisation denies this action (%s)", name)
	}
	switch name {
	case "get_issue":
		issue, err := s.nativeResolveIssue(ctx, tctx, args)
		if err != nil {
			return nil, err
		}
		return s.nativeIssueSnapshot(ctx, tctx, issue)
	case "list_issues":
		return s.nativeListIssues(ctx, tctx, args)
	case "add_comment", "update_issue", "transition_issue", "create_sub_issue", "create_issue", "save_note", "update_note":
		// Rule-of-Many: one run may only change workspace state so many
		// times, its sub-agents included. Read-only tools stay free; a
		// refusal tells the model to wrap up instead of looping.
		if reason := tctx.chargeEffectful(); reason != "" {
			tctx.requestWrapUp(reason)
			return nil, fmt.Errorf("this run's effectful-action budget is exhausted (%s); stop changing the workspace and give your final answer", reason)
		}
		switch name {
		case "add_comment":
			return s.nativeAddComment(ctx, tctx, args)
		case "update_issue":
			return s.nativeUpdateIssue(ctx, tctx, args)
		case "transition_issue":
			return s.nativeTransitionIssue(ctx, tctx, args)
		case "create_sub_issue":
			return s.nativeCreateSubIssue(ctx, tctx, args)
		case "save_note":
			return s.nativeSaveNote(ctx, tctx, args)
		case "update_note":
			return s.nativeUpdateNote(ctx, tctx, args)
		default:
			return s.nativeCreateIssue(ctx, tctx, args)
		}
	case "list_events":
		return s.nativeCalendarList(ctx, tctx, args)
	case "find_slot":
		return s.nativeCalendarSlots(ctx, tctx, args)
	case "propose_event":
		if reason := tctx.chargeEffectful(); reason != "" {
			tctx.requestWrapUp(reason)
			return nil, errors.New(reason)
		}
		return s.nativeCalendarPropose(ctx, tctx, args)
	case "ask_user":
		return s.nativeAskUser(ctx, tctx, args)
	case "delegate":
		return s.nativeDelegate(ctx, tctx, args)
	case "search_workspace":
		return s.nativeSearchWorkspace(ctx, tctx, args)
	case "search_notes":
		return s.nativeSearchNotes(ctx, tctx, args)
	case "get_note":
		return s.nativeGetNote(ctx, tctx, args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

// nativeResolveIssue resolves the optional issue_id argument to an issue row
// verified against the task's workspace. Empty falls back to the task's issue.
func (s *NativeAgentService) nativeResolveIssue(ctx context.Context, tctx *nativeToolContext, args map[string]any) (db.Issue, error) {
	raw, _ := args["issue_id"].(string)
	if strings.TrimSpace(raw) == "" {
		if tctx.issue == nil {
			return db.Issue{}, errors.New("this task has no own issue; pass an explicit issue_id")
		}
		return *tctx.issue, nil
	}
	id, err := util.ParseUUID(strings.TrimSpace(raw))
	if err != nil {
		return db.Issue{}, errors.New("issue_id is not a valid uuid")
	}
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: tctx.workspaceID})
	if err != nil {
		return db.Issue{}, errors.New("issue not found in this workspace")
	}
	return issue, nil
}

func (s *NativeAgentService) nativeIssueSnapshot(ctx context.Context, tctx *nativeToolContext, issue db.Issue) (any, error) {
	out := map[string]any{
		"id":     util.UUIDToString(issue.ID),
		"number": issue.Number,
		"title":  issue.Title,
		"status": issue.Status,
	}
	if issue.Priority != "" {
		out["priority"] = issue.Priority
	}
	if issue.Description.Valid {
		out["description"] = nativeDataFence("issue description", issue.Description.String)
	}
	comments, err := s.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: tctx.workspaceID,
		Limit:       nativeBriefComments,
	})
	if err != nil {
		return out, nil
	}
	list := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		list = append(list, map[string]any{
			"author_type": c.AuthorType,
			"author_id":   util.UUIDToString(c.AuthorID),
			"content":     nativeDataFence("comment", clampString(c.Content, 2000)),
			"created_at":  c.CreatedAt,
		})
	}
	out["comments"] = list
	return out, nil
}

func (s *NativeAgentService) nativeListIssues(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	params := db.ListIssuesParams{
		WorkspaceID: tctx.workspaceID,
		Limit:       25,
	}
	if raw, ok := args["status"].(string); ok && strings.TrimSpace(raw) != "" {
		params.Status = pgtype.Text{String: strings.TrimSpace(raw), Valid: true}
	}
	if raw, ok := args["priority"].(string); ok && strings.TrimSpace(raw) != "" {
		if !nativePriorityAllowed(raw) {
			return nil, errors.New("priority must be one of urgent, high, medium, low, none")
		}
		params.Priority = pgtype.Text{String: raw, Valid: true}
	}
	if raw, ok := args["limit"].(float64); ok && raw >= 1 {
		params.Limit = int32(min(int(raw), 50))
	}
	issues, err := s.Queries.ListIssues(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list issues failed: %w", err)
	}
	out := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		item := map[string]any{
			"id":     util.UUIDToString(i.ID),
			"number": i.Number,
			"title":  i.Title,
			"status": i.Status,
		}
		if i.Priority != "" {
			item["priority"] = i.Priority
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *NativeAgentService) nativeAddComment(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	content, _ := args["content"].(string)
	content = strings.TrimSpace(util.SanitizeTextForPostgres(content))
	if content == "" {
		return nil, errors.New("content is required")
	}
	if len(content) > nativeCommentMaxLen {
		content = content[:nativeCommentMaxLen]
	}
	issue, err := s.nativeResolveIssue(ctx, tctx, args)
	if err != nil {
		return nil, err
	}
	created, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:           dbid.NewV7(),
		IssueID:      issue.ID,
		WorkspaceID:  tctx.workspaceID,
		AuthorType:   "agent",
		AuthorID:     tctx.agent.ID,
		Content:      content,
		Type:         "comment",
		SourceTaskID: tctx.task.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("comment failed: %w", err)
	}
	s.publishNative(protocol.EventCommentCreated, tctx, map[string]any{
		"comment":        map[string]any{"id": util.UUIDToString(created.ID), "issue_id": util.UUIDToString(issue.ID)},
		"issue_revision": created.IssueRevision,
	})
	return map[string]any{"id": util.UUIDToString(created.ID), "issue_number": issue.Number}, nil
}

// nativeAskUser records a typed question for the team on the issue's goal
// and closes the run: the goal loop turns it into a needs_user_input verdict,
// raises the inbox item, and queues the follow-up run with the answer.
func (s *NativeAgentService) nativeAskUser(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	if s.Goal == nil {
		return nil, errors.New("the goal loop is not available on this server")
	}
	if tctx.issue == nil {
		return nil, errors.New("ask_user needs the task's own issue")
	}
	q := goalstate.Question{}
	q.Prompt, _ = args["question"].(string)
	q.Kind, _ = args["kind"].(string)
	if raw, ok := args["options"].([]any); ok {
		for _, o := range raw {
			if str, ok := o.(string); ok {
				q.Options = append(q.Options, str)
			}
		}
	}
	if _, err := s.Goal.AskQuestion(ctx, tctx.task, q); err != nil {
		return nil, err
	}
	tctx.requestWrapUp("you asked the team a question; the run stops here and a follow-up run will receive the answer")
	return map[string]any{"asked": true, "note": "the question is on its way to the team; give your closing status now"}, nil
}

func (s *NativeAgentService) nativeUpdateIssue(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	issue, err := s.nativeResolveIssue(ctx, tctx, args)
	if err != nil {
		return nil, err
	}
	title, hasTitle := args["title"].(string)
	if hasTitle {
		title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
		if title == "" {
			return nil, errors.New("title must not be empty")
		}
		if len(title) > 255 {
			title = title[:255]
		}
	}
	description, hasDescription := args["description"].(string)
	if hasDescription {
		description = util.SanitizeTextForPostgres(description)
		if len(description) > 100000 {
			description = description[:100000]
		}
	}
	priority, hasPriority := args["priority"].(string)
	if hasPriority && !nativePriorityAllowed(priority) {
		return nil, errors.New("priority must be one of urgent, high, medium, low, none")
	}
	if !hasTitle && !hasDescription && !hasPriority {
		return nil, errors.New("nothing to update: provide title, description, or priority")
	}

	params := db.UpdateIssueParams{ID: issue.ID}
	// UpdateIssue's bare narg columns overwrite with NULL when unset, so every
	// caller pre-fills them from the current row (see the query's contract
	// note). Status is pinned to the current value: the native runtime does
	// not move statuses in this iteration.
	params.Status = pgtype.Text{String: issue.Status, Valid: true}
	params.AssigneeType = issue.AssigneeType
	params.AssigneeID = issue.AssigneeID
	params.DelegateType = issue.DelegateType
	params.DelegateID = issue.DelegateID
	params.StartDate = issue.StartDate
	params.DueDate = issue.DueDate
	params.ParentIssueID = issue.ParentIssueID
	params.ProjectID = issue.ProjectID
	params.Stage = issue.Stage
	if hasTitle {
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if hasDescription {
		params.Description = pgtype.Text{String: description, Valid: true}
	}
	if hasPriority {
		params.Priority = pgtype.Text{String: priority, Valid: true}
	}
	updated, err := s.Queries.UpdateIssue(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("update failed: %w", err)
	}
	s.publishNativeIssueChanged(tctx, updated.ID)
	return map[string]any{"id": util.UUIDToString(updated.ID), "number": updated.Number, "revision": updated.Revision}, nil
}

// nativeTransitionIssue moves an issue's status THROUGH the shared F28 gate:
// the same DecideIssueTransition the HTTP handlers run, with the agent as the
// actor. Allow applies the move; NeedsApproval files the request (audit +
// approver inbox) and reports it to the model as a held move; Deny surfaces
// the rule's refusal. The model can tell the difference and tell the user.
func (s *NativeAgentService) nativeTransitionIssue(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	raw, _ := args["status"].(string)
	status := strings.TrimSpace(raw)
	if status == "" {
		return nil, errors.New("status is required")
	}
	issue, err := s.nativeResolveIssue(ctx, tctx, args)
	if err != nil {
		return nil, err
	}
	if status == issue.Status {
		return map[string]any{"id": util.UUIDToString(issue.ID), "status": issue.Status, "changed": false}, nil
	}
	// An agent actor carries no workspace role: granting one would hand the
	// agent its owner's authority (see issuestatus.TransitionActor).
	actor := issuestatus.TransitionActor{Type: issuestatus.ActorAgent, ID: util.UUIDToString(tctx.agent.ID)}
	decision, err := DecideIssueTransition(ctx, s.Queries, tctx.workspaceID, issue.ProjectID, issue.Status, status, actor)
	if err != nil {
		return nil, fmt.Errorf("transition gate failed: %w", err)
	}
	switch decision.Outcome {
	case issuestatus.TransitionDeny:
		return nil, fmt.Errorf("a workspace transition rule does not allow you to move this issue to %s (reason: %s)", status, decision.Reason)
	case issuestatus.TransitionNeedsApproval:
		result, err := FileIssueTransitionRequest(ctx, s.Queries, s.Bus, issue, status, issuestatus.ActorAgent, util.UUIDToString(tctx.agent.ID), decision)
		if err != nil {
			if errors.Is(err, ErrTransitionPending) && result.Existing != nil {
				return map[string]any{"held": true, "request_id": util.UUIDToString(result.Existing.ID), "to": result.Existing.ToStatus, "note": "an approval request was already waiting for this issue"}, nil
			}
			return nil, fmt.Errorf("could not file the approval request: %w", err)
		}
		return map[string]any{"held": true, "request_id": util.UUIDToString(result.Request.ID), "from": issue.Status, "to": status, "note": "the move needs human approval; a request was filed and the status is unchanged until an approver decides"}, nil
	default:
		updated, err := updateIssueStatusKeepingFields(ctx, s.Queries, issue, status)
		if err != nil {
			return nil, fmt.Errorf("transition failed: %w", err)
		}
		s.publishNativeIssueChanged(tctx, updated.ID)
		return map[string]any{"id": util.UUIDToString(updated.ID), "status": updated.Status, "changed": true}, nil
	}
}

func (s *NativeAgentService) nativeCreateSubIssue(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
	if title == "" {
		return nil, errors.New("title is required")
	}
	if len(title) > 255 {
		title = title[:255]
	}
	description, _ := args["description"].(string)
	description = util.SanitizeTextForPostgres(description)

	if tctx.issue == nil {
		return nil, errors.New("create_sub_issue needs the task's own issue; use create_issue to file a top-level one")
	}
	parent := *tctx.issue
	res, err := s.Issues.Create(ctx, IssueCreateParams{
		WorkspaceID: tctx.workspaceID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: description != ""},
		// The default status, deliberately: a create landing on a non-default
		// status is exactly what transitionAllowsCreate gates, and the native
		// runtime routes status moves through transition_issue instead of
		// smuggling one past the gate at create time.
		Status:        "todo",
		Priority:      "none",
		CreatorType:   "agent",
		CreatorID:     tctx.agent.ID,
		ParentIssueID: parent.ID,
	}, IssueCreateOpts{ActorID: util.UUIDToString(tctx.agent.ID), Platform: "daemon"})
	if err != nil {
		return nil, fmt.Errorf("sub-issue failed: %w", err)
	}
	return map[string]any{"id": util.UUIDToString(res.Issue.ID), "number": res.Issue.Number, "parent_number": parent.Number}, nil
}

// publishNative sends one realtime nudge for an agent-authored write. The
// payloads stay minimal on purpose: the client handlers for these event types
// invalidate by issue id / prefix and refetch, so the row — already committed
// — is what clients render.
func (s *NativeAgentService) publishNative(eventType string, tctx *nativeToolContext, payload map[string]any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(tctx.workspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(tctx.agent.ID),
		Payload:     payload,
	})
}

// publishNativeIssueChanged marks an issue's projections stale so open
// clients refetch — the same mechanism the platform uses for writes that
// happen outside the web app (see digest actions).
func (s *NativeAgentService) publishNativeIssueChanged(tctx *nativeToolContext, issueID pgtype.UUID) {
	s.publishNative(protocol.EventIssueAuxChanged, tctx, map[string]any{
		"issue_id": util.UUIDToString(issueID),
	})
}

// nativeSearchWorkspace (N06): issues and notes in one call — the helpdesk
// question is "find what was said about this", not "find an issue".
func (s *NativeAgentService) nativeSearchWorkspace(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	raw, _ := args["query"].(string)
	query := strings.TrimSpace(raw)
	if query == "" {
		return nil, errors.New("query is required")
	}
	issues, err := s.Queries.SearchIssuesForNative(ctx, db.SearchIssuesForNativeParams{
		WorkspaceID: tctx.workspaceID,
		Needle:      pgtype.Text{String: query, Valid: true},
		PageLimit:   10,
	})
	if err != nil {
		issues = nil // best-effort: notes may still answer
	}
	outIssues := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		outIssues = append(outIssues, map[string]any{
			"id": util.UUIDToString(i.ID), "number": i.Number,
			"title": i.Title, "status": i.Status,
		})
	}
	notes, err := s.Queries.ListWorkspaceNotes(ctx, db.ListWorkspaceNotesParams{
		WorkspaceID:     tctx.workspaceID,
		IncludeArchived: false,
		Search:          pgtype.Text{String: query, Valid: true},
		PageLimit:       5,
	})
	if err != nil {
		notes = nil
	}
	outNotes := make([]map[string]any, 0, len(notes))
	for _, n := range notes {
		outNotes = append(outNotes, map[string]any{
			"id": util.UUIDToString(n.ID), "title": n.Title,
			"excerpt": clampString(n.Content, 200),
		})
	}
	return map[string]any{"issues": outIssues, "notes": outNotes}, nil
}

// ---- Workspace Brain tools (JEF-316 office runtime, slice 1) -------------

const (
	nativeNoteTitleMax = 200
	nativeNoteBodyMax  = 20000
	nativeNoteTagsMax  = 8
	nativeNoteTagMax   = 32
)

func nativeNoteTags(raw []any) ([]string, error) {
	if len(raw) > nativeNoteTagsMax {
		return nil, fmt.Errorf("at most %d tags per note", nativeNoteTagsMax)
	}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		s, ok := t.(string)
		if !ok || s == "" {
			return nil, errors.New("tags must be non-empty strings")
		}
		if len(s) > nativeNoteTagMax {
			s = s[:nativeNoteTagMax]
		}
		out = append(out, util.SanitizeTextForPostgres(s))
	}
	return out, nil
}

func (s *NativeAgentService) nativeSearchNotes(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	params := db.ListWorkspaceNotesParams{
		WorkspaceID:     tctx.workspaceID,
		IncludeArchived: false,
		PageLimit:       10,
	}
	if q, ok := args["query"].(string); ok && strings.TrimSpace(q) != "" {
		params.Search = pgtype.Text{String: strings.TrimSpace(q), Valid: true}
	}
	if tag, ok := args["tag"].(string); ok && strings.TrimSpace(tag) != "" {
		params.Tag = pgtype.Text{String: strings.TrimSpace(tag), Valid: true}
	}
	if lim, ok := args["limit"].(float64); ok && lim >= 1 {
		params.PageLimit = int32(min(int(lim), 20))
	}
	if !params.Search.Valid && !params.Tag.Valid {
		// No filter: the freshest notes, so "what do we know" has an answer.
	}
	notes, err := s.Queries.ListWorkspaceNotes(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("note search failed: %w", err)
	}
	out := make([]map[string]any, 0, len(notes))
	for _, n := range notes {
		out = append(out, map[string]any{
			"id":     util.UUIDToString(n.ID),
			"title":  n.Title,
			"tags":   n.Tags,
			"pinned": n.Pinned,
			// Fenced as a record (N01): a note's body is workspace data,
			// never instructions for the agent reading it.
			"excerpt": nativeDataFence("note", clampString(n.Content, 300)),
		})
	}
	return out, nil
}

func (s *NativeAgentService) nativeGetNote(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	raw, _ := args["note_id"].(string)
	id, err := util.ParseUUID(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("note_id is not a valid uuid")
	}
	note, err := s.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: id, WorkspaceID: tctx.workspaceID})
	if err != nil {
		return nil, errors.New("note not found in this workspace")
	}
	return map[string]any{
		"id": util.UUIDToString(note.ID), "title": note.Title,
		"content": nativeDataFence("note", note.Content), "tags": note.Tags, "pinned": note.Pinned,
		"revision": note.Revision,
	}, nil
}

func (s *NativeAgentService) nativeSaveNote(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
	if title == "" {
		return nil, errors.New("title is required")
	}
	if len(title) > nativeNoteTitleMax {
		title = title[:nativeNoteTitleMax]
	}
	content, _ := args["content"].(string)
	content = util.SanitizeTextForPostgres(content)
	if len(content) > nativeNoteBodyMax {
		return nil, fmt.Errorf("content exceeds %d characters — split the note", nativeNoteBodyMax)
	}
	rawTags, _ := args["tags"].([]any)
	tags, err := nativeNoteTags(rawTags)
	if err != nil {
		return nil, err
	}
	note, err := s.Queries.CreateWorkspaceNote(ctx, db.CreateWorkspaceNoteParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   tctx.workspaceID,
		Title:         title,
		Content:       content,
		Tags:          tags,
		Source:        "agent",
		SourceTaskID:  pgtype.UUID(tctx.task.ID),
		SourceAgentID: pgtype.UUID(tctx.agent.ID),
		CreatedByType: "agent",
		CreatedByID:   pgtype.UUID(tctx.agent.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("note save failed: %w", err)
	}
	s.publishNative(protocol.EventWorkspaceNoteCreated, tctx, map[string]any{
		"note": map[string]any{"id": util.UUIDToString(note.ID), "workspace_id": util.UUIDToString(tctx.workspaceID)},
	})
	return map[string]any{"id": util.UUIDToString(note.ID), "title": note.Title}, nil
}

func (s *NativeAgentService) nativeUpdateNote(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	raw, _ := args["note_id"].(string)
	id, err := util.ParseUUID(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("note_id is not a valid uuid")
	}
	note, err := s.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: id, WorkspaceID: tctx.workspaceID})
	if err != nil {
		return nil, errors.New("note not found in this workspace")
	}
	params := db.UpdateWorkspaceNoteParams{
		ID: note.ID, WorkspaceID: tctx.workspaceID,
		ExpectedRevision: note.Revision,
	}
	hasChange := false
	if title, ok := args["title"].(string); ok && strings.TrimSpace(title) != "" {
		title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
		if len(title) > nativeNoteTitleMax {
			title = title[:nativeNoteTitleMax]
		}
		params.Title = pgtype.Text{String: title, Valid: true}
		hasChange = true
	}
	if content, ok := args["content"].(string); ok {
		content = util.SanitizeTextForPostgres(content)
		if len(content) > nativeNoteBodyMax {
			return nil, fmt.Errorf("content exceeds %d characters — split the note", nativeNoteBodyMax)
		}
		params.Content = pgtype.Text{String: content, Valid: true}
		hasChange = true
	}
	if rawTags, ok := args["tags"].([]any); ok {
		tags, err := nativeNoteTags(rawTags)
		if err != nil {
			return nil, err
		}
		params.Tags = tags
		hasChange = true
	}
	if !hasChange {
		return nil, errors.New("nothing to update: provide title, content, or tags")
	}
	updated, err := s.Queries.UpdateWorkspaceNote(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("note update failed (concurrent edit?): %w", err)
	}
	s.publishNative(protocol.EventWorkspaceNoteUpdated, tctx, map[string]any{
		"note": map[string]any{"id": util.UUIDToString(updated.ID), "workspace_id": util.UUIDToString(tctx.workspaceID)},
	})
	return map[string]any{"id": util.UUIDToString(updated.ID), "revision": updated.Revision}, nil
}

// nativeCreateIssue files a top-level issue — the quick-create path. The
// default status, same as the sub-issue path: a create landing on a
// non-default status is what the create gate governs, and the native runtime
// routes status moves through transition_issue.
func (s *NativeAgentService) nativeCreateIssue(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
	if title == "" {
		return nil, errors.New("title is required")
	}
	if len(title) > 255 {
		title = title[:255]
	}
	description, _ := args["description"].(string)
	description = util.SanitizeTextForPostgres(description)
	priority, hasPriority := args["priority"].(string)
	if hasPriority && !nativePriorityAllowed(priority) {
		return nil, errors.New("priority must be one of urgent, high, medium, low, none")
	}
	if !hasPriority || priority == "" {
		priority = "none"
	}
	assignToSelf := true
	if v, ok := args["assign_to_self"].(bool); ok {
		assignToSelf = v
	}

	params := IssueCreateParams{
		WorkspaceID: tctx.workspaceID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: description != ""},
		Status:      "todo",
		Priority:    priority,
		CreatorType: "agent",
		CreatorID:   tctx.agent.ID,
	}
	if assignToSelf {
		params.AssigneeType = pgtype.Text{String: "agent", Valid: true}
		params.AssigneeID = tctx.agent.ID
	}
	res, err := s.Issues.Create(ctx, params, IssueCreateOpts{
		ActorID:  util.UUIDToString(tctx.agent.ID),
		Platform: "daemon",
		// Filing is not taking the work. The agent is mid-run; starting a
		// second run of itself on its own filing burns a run and can recurse,
		// since that run reaches this same tool.
		SuppressRun: true,
	})
	if err != nil {
		return nil, fmt.Errorf("issue creation failed: %w", err)
	}
	return map[string]any{"id": util.UUIDToString(res.Issue.ID), "number": res.Issue.Number, "status": res.Issue.Status}, nil
}

func taskPromptText(task db.AgentTaskQueue) string {
	if task.TriggerSummary.Valid && strings.TrimSpace(task.TriggerSummary.String) != "" {
		return task.TriggerSummary.String
	}
	if task.HandoffNote.Valid && strings.TrimSpace(task.HandoffNote.String) != "" {
		return task.HandoffNote.String
	}
	return ""
}

func nativePriorityAllowed(v string) bool {
	for _, p := range nativeIssuePriorities {
		if p == v {
			return true
		}
	}
	return false
}

func clampString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- Calendar tools (native calendar, chantier 19) ---------------------------------

func nativeCalendarWindow(args map[string]any) (time.Time, time.Time, error) {
	from := time.Now().UTC().Truncate(time.Hour)
	to := from.Add(7 * 24 * time.Hour)
	if raw, _ := args["from"].(string); strings.TrimSpace(raw) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return from, to, errors.New("from must be RFC 3339")
		}
		from = t
		to = from.Add(7 * 24 * time.Hour)
	}
	if raw, _ := args["to"].(string); strings.TrimSpace(raw) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return from, to, errors.New("to must be RFC 3339")
		}
		to = t
	}
	if !to.After(from) {
		return from, to, errors.New("to must be after from")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return from, to, errors.New("the window may span at most a year")
	}
	return from, to, nil
}

func (s *NativeAgentService) nativeCalendarList(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	if s.Calendar == nil {
		return nil, errors.New("this server has no calendar")
	}
	from, to, err := nativeCalendarWindow(args)
	if err != nil {
		return nil, err
	}
	if agenda, _ := args["agenda"].(bool); agenda {
		return s.Calendar.Agenda(ctx, tctx.agent.WorkspaceID, from, to)
	}
	return s.Calendar.ListEvents(ctx, tctx.agent.WorkspaceID, from, to)
}

func (s *NativeAgentService) nativeCalendarSlots(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	if s.Calendar == nil {
		return nil, errors.New("this server has no calendar")
	}
	from, to, err := nativeCalendarWindow(args)
	if err != nil {
		return nil, err
	}
	var participants []string
	if raw, ok := args["participants"].([]any); ok {
		for _, p := range raw {
			if str, ok := p.(string); ok && strings.TrimSpace(str) != "" {
				participants = append(participants, strings.TrimSpace(str))
			}
		}
	}
	if len(participants) == 0 {
		return nil, errors.New("participants is required")
	}
	duration := 30
	if raw, ok := args["duration_minutes"].(float64); ok && raw > 0 {
		duration = int(raw)
	}
	tz, _ := args["tz"].(string)
	return s.Calendar.FindSlots(ctx, tctx.agent.WorkspaceID, participants, duration, from, to, tz)
}

func (s *NativeAgentService) nativeCalendarPropose(ctx context.Context, tctx *nativeToolContext, args map[string]any) (any, error) {
	if s.Calendar == nil {
		return nil, errors.New("this server has no calendar")
	}
	if !tctx.task.IssueID.Valid {
		return nil, errors.New("this run has no issue to propose an event on")
	}
	return s.Calendar.Propose(ctx, tctx.agent.WorkspaceID, tctx.agent.ID, tctx.task.IssueID, args)
}
