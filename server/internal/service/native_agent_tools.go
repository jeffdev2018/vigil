package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
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
	// issue is the task's own issue, nil for the issue-less kinds (chat,
	// quick-create, autopilot run-only). Tools that default to "the task's
	// issue" require an explicit issue_id when it is nil.
	issue       *db.Issue
	workspaceID pgtype.UUID
}

var nativeIssuePriorities = []string{"urgent", "high", "medium", "low", "none"}

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
	}
}

// callNativeTool dispatches one validated tool invocation. Errors are values
// for the model to react to, not run failures.
func (s *NativeAgentService) callNativeTool(ctx context.Context, tctx *nativeToolContext, name string, args map[string]any) (any, error) {
	switch name {
	case "get_issue":
		issue, err := s.nativeResolveIssue(ctx, tctx, args)
		if err != nil {
			return nil, err
		}
		return s.nativeIssueSnapshot(ctx, tctx, issue)
	case "list_issues":
		return s.nativeListIssues(ctx, tctx, args)
	case "add_comment", "update_issue", "transition_issue", "create_sub_issue", "create_issue":
		// Rule-of-Many: one run may only change workspace state so many
		// times. Read-only tools stay free; a refusal tells the model to
		// wrap up instead of looping.
		tctx.effectful++
		if tctx.effectful > nativeMaxEffectfulActions {
			return nil, fmt.Errorf("this run's effectful-action budget (%d) is exhausted; stop changing the workspace and give your final answer", nativeMaxEffectfulActions)
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
		default:
			return s.nativeCreateIssue(ctx, tctx, args)
		}
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
		out["description"] = issue.Description.String
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
			"content":     clampString(c.Content, 2000),
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
		params := db.UpdateIssueParams{ID: issue.ID}
		// Same bare-narg contract as update_issue: pre-fill every overwrite
		// column from the current row, then set the new status.
		params.Status = pgtype.Text{String: status, Valid: true}
		params.Title = pgtype.Text{String: issue.Title, Valid: true}
		if issue.Description.Valid {
			params.Description = issue.Description
		}
		if issue.Priority != "" {
			params.Priority = pgtype.Text{String: issue.Priority, Valid: true}
		}
		params.AssigneeType = issue.AssigneeType
		params.AssigneeID = issue.AssigneeID
		params.DelegateType = issue.DelegateType
		params.DelegateID = issue.DelegateID
		params.StartDate = issue.StartDate
		params.DueDate = issue.DueDate
		params.ParentIssueID = issue.ParentIssueID
		params.ProjectID = issue.ProjectID
		params.Stage = issue.Stage
		updated, err := s.Queries.UpdateIssue(ctx, params)
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
