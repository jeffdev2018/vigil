package handler

// Automations in plain words (OS plan, vague B, réveil programmé): "every
// Monday at 9, send me the open tickets" becomes an autopilot. Draft asks
// the model for title, cron, timezone and prompt and shows the next runs
// without writing anything; propose files the autopilot paused behind a
// Decision Card on the issue, so a person activates it or discards it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"log/slog"
)

const (
	autopilotOptionPrefix   = "autopilot:"
	autopilotActivateOption = "autopilot:activate:"
	autopilotDiscardOption  = "autopilot:discard:"

	AuditAutopilotProposed = "autopilot.proposed"
	AuditAutopilotDecided  = "autopilot.proposal_decided"

	autopilotDraftMaxText = 2000
)

// AutopilotDraft is what the model proposes, validated against the cron parser.
type AutopilotDraft struct {
	Title              string   `json:"title"`
	CronExpression     string   `json:"cron_expression"`
	Timezone           string   `json:"timezone"`
	Description        string   `json:"description"`
	ExecutionMode      string   `json:"execution_mode"`
	IssueTitleTemplate string   `json:"issue_title_template,omitempty"`
	Reason             string   `json:"reason"`
	NextRuns           []string `json:"next_runs"`
	Model              string   `json:"model,omitempty"`
}

const autopilotDraftSystemPrompt = `You turn a sentence into a scheduled automation ("autopilot") for a work-management tool. Return JSON only:
{"title": "<short title, at most 8 words, in the sentence's language>",
 "cron_expression": "<5-field cron: minute hour day-of-month month day-of-week>",
 "timezone": "<IANA timezone; use the given default unless the sentence names one>",
 "description": "<the instruction the agent will follow at each run, written to the agent in the imperative, in the sentence's language; keep the person's words>",
 "execution_mode": "create_issue" | "run_only",
 "issue_title_template": "<title of the issue created at each run, may contain {{date}}; empty for run_only>",
 "reason": "<one sentence on how you read the schedule>"}

Rules: "every weekday" = 1-5, "every Monday" = 1, "weekly" without a day = Monday 09:00, "daily" without a time = 09:00, "monthly" = day 1 at 09:00. create_issue when the sentence asks for something to track or review (a report, a check, a list); run_only when it asks the agent to just do something. Never invent a time the sentence does not imply beyond these defaults.`

// draftAutopilot asks the model and validates the schedule. available is
// false without a model.
func (h *Handler) draftAutopilot(ctx context.Context, text, defaultTZ string) (AutopilotDraft, bool, error) {
	if h.LLM == nil || !h.LLM.Enabled() {
		return AutopilotDraft{}, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return AutopilotDraft{}, true, errors.New("text is required")
	}
	if len([]rune(text)) > autopilotDraftMaxText {
		return AutopilotDraft{}, true, fmt.Errorf("text is limited to %d characters", autopilotDraftMaxText)
	}
	if defaultTZ == "" || service.ValidateTimezone(defaultTZ) != nil {
		defaultTZ = "UTC"
	}
	user := fmt.Sprintf("Default timezone: %s\nToday: %s\nSentence:\n<sentence>\n%s\n</sentence>", defaultTZ, time.Now().In(mustLocation(defaultTZ)).Format("Monday 2 January 2006 15:04"), text)
	// A draft is a small structured task: the routing model (small, fast)
	// answers in seconds where the default model can take the better part of a
	// minute, which the web proxy would not wait for.
	model := h.cfg.LLMRoutingModel
	raw, err := h.LLM.GenerateJSON(ctx, model, autopilotDraftSystemPrompt, user, 0, 600)
	if err != nil {
		return AutopilotDraft{}, true, err
	}
	var d AutopilotDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return AutopilotDraft{}, true, fmt.Errorf("the model did not answer with a draft: %w", err)
	}
	d.Title = strings.TrimSpace(d.Title)
	d.CronExpression = strings.TrimSpace(d.CronExpression)
	d.Description = strings.TrimSpace(d.Description)
	d.Timezone = strings.TrimSpace(d.Timezone)
	if d.Timezone == "" || service.ValidateTimezone(d.Timezone) != nil {
		d.Timezone = defaultTZ
	}
	if d.Title == "" {
		d.Title = firstLine(text, 80)
	}
	if d.Description == "" {
		d.Description = text
	}
	if d.ExecutionMode != "create_issue" && d.ExecutionMode != "run_only" {
		d.ExecutionMode = "run_only"
	}
	if d.ExecutionMode == "run_only" {
		d.IssueTitleTemplate = ""
	} else if d.IssueTitleTemplate != "" {
		if err := service.ValidateIssueTitleTemplate(d.IssueTitleTemplate); err != nil {
			d.IssueTitleTemplate = ""
		}
	}
	runs, err := service.NextOccurrencesAfterUTC(d.CronExpression, d.Timezone, time.Now().UTC(), 3)
	if err != nil {
		return AutopilotDraft{}, true, fmt.Errorf("the model produced an invalid schedule (%q): %w", d.CronExpression, err)
	}
	for _, at := range runs {
		d.NextRuns = append(d.NextRuns, at.Format(time.RFC3339))
	}
	d.Model = model
	if d.Model == "" {
		d.Model = h.LLM.DefaultModel()
	}
	return d, true, nil
}

func mustLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

// POST /api/autopilots/draft {text, timezone?} — members; writes nothing.
func (h *Handler) DraftAutopilot(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	var req struct {
		Text     string `json:"text"`
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	draft, available, err := h.draftAutopilot(r.Context(), req.Text, req.Timezone)
	if !available {
		writeError(w, http.StatusServiceUnavailable, "no model is configured to draft; write the schedule yourself")
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		if strings.HasPrefix(err.Error(), "text ") {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft})
}

// AutopilotProposalRequest files a paused autopilot. Either the fields or
// a sentence the model turns into them.
type AutopilotProposalRequest struct {
	Text               string `json:"text"`
	Title              string `json:"title"`
	CronExpression     string `json:"cron_expression"`
	Timezone           string `json:"timezone"`
	Description        string `json:"description"`
	ExecutionMode      string `json:"execution_mode"`
	IssueTitleTemplate string `json:"issue_title_template"`
	AssigneeID         string `json:"assignee_id"`
	ProjectID          string `json:"project_id"`
	// IssueID is where the Decision Card goes; a run's own issue by default.
	IssueID string `json:"issue_id"`
	// Activate (members only) creates the autopilot active with its schedule
	// enabled and files no card: the person deciding is the caller.
	Activate bool `json:"activate"`
}

// POST /api/autopilots/propose — an agent run (task token) or a member.
// Creates the autopilot paused with its schedule disabled, then a Decision
// Card on the issue whose answer activates or discards it.
func (h *Handler) ProposeAutopilot(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req AutopilotProposalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if strings.TrimSpace(req.CronExpression) == "" || strings.TrimSpace(req.Title) == "" {
		draft, available, err := h.draftAutopilot(r.Context(), req.Text, req.Timezone)
		if !available {
			writeError(w, http.StatusServiceUnavailable, "no model is configured to draft; pass title, cron_expression and description")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if req.Title == "" {
			req.Title = draft.Title
		}
		if req.CronExpression == "" {
			req.CronExpression = draft.CronExpression
		}
		if req.Timezone == "" {
			req.Timezone = draft.Timezone
		}
		if req.Description == "" {
			req.Description = draft.Description
		}
		if req.ExecutionMode == "" {
			req.ExecutionMode = draft.ExecutionMode
		}
		if req.IssueTitleTemplate == "" {
			req.IssueTitleTemplate = draft.IssueTitleTemplate
		}
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if err := service.ValidateTimezone(req.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ExecutionMode == "" {
		req.ExecutionMode = "run_only"
	}
	if req.ExecutionMode != "create_issue" && req.ExecutionMode != "run_only" {
		writeError(w, http.StatusBadRequest, "execution_mode must be create_issue or run_only")
		return
	}
	if req.IssueTitleTemplate != "" {
		if err := service.ValidateIssueTitleTemplate(req.IssueTitleTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	runs, err := service.NextOccurrencesAfterUTC(req.CronExpression, req.Timezone, time.Now().UTC(), 3)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cron_expression: "+err.Error())
		return
	}
	// Who runs it: the proposing agent, or the named assignee for a member.
	assigneeID := req.AssigneeID
	if actorType == "agent" {
		assigneeID = actorID
	}
	assigneeUUID, ok := parseUUIDOrBadRequest(w, assigneeID, "assignee_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: assigneeUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "assignee agent not found in this workspace")
		return
	}
	var projectID pgtype.UUID
	if req.ProjectID != "" {
		id, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return
		}
		projectID = id
	}
	// The issue the card goes on: explicit, else the run's own issue.
	var issue *db.Issue
	issueID := req.IssueID
	if issueID == "" && actorType == "agent" {
		if task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(r.Header.Get("X-Task-ID"))); err == nil && task.IssueID.Valid {
			issueID = uuidToString(task.IssueID)
		}
	}
	if issueID != "" {
		id, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
		if !ok {
			return
		}
		row, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issue = &row
	}
	if req.Activate && actorType != "member" {
		writeError(w, http.StatusForbidden, "only a member can activate; a run proposes and a person decides")
		return
	}
	status := "paused"
	if req.Activate {
		status = "active"
	}
	createdByType, createdByID := actorType, parseUUID(actorID)
	ap, err := h.Queries.CreateAutopilot(r.Context(), db.CreateAutopilotParams{
		WorkspaceID: wsUUID, Title: strings.TrimSpace(req.Title), AssigneeType: "agent", AssigneeID: assigneeUUID, Status: status, ExecutionMode: req.ExecutionMode,
		CreatedByType: createdByType, CreatedByID: createdByID, Description: pgtype.Text{String: strings.TrimSpace(req.Description), Valid: true},
		IssueTitleTemplate: pgtype.Text{String: req.IssueTitleTemplate, Valid: req.IssueTitleTemplate != ""}, ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the autopilot")
		return
	}
	if _, err := h.Queries.CreateAutopilotTrigger(r.Context(), db.CreateAutopilotTriggerParams{
		AutopilotID: ap.ID, Kind: "schedule", Enabled: req.Activate, CronExpression: pgtype.Text{String: req.CronExpression, Valid: true}, Timezone: pgtype.Text{String: req.Timezone, Valid: true},
		NextRunAt: pgtype.Timestamptz{Time: runs[0], Valid: true}, Label: pgtype.Text{String: "Proposed schedule", Valid: true},
		CreatedByType: pgtype.Text{String: createdByType, Valid: true}, CreatedByID: createdByID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the schedule")
		return
	}
	next := make([]string, 0, len(runs))
	for _, at := range runs {
		next = append(next, at.Format(time.RFC3339))
	}
	var decisionID *string
	if issue != nil && !req.Activate {
		options, _ := json.Marshal([]DecisionOption{
			{ID: autopilotActivateOption + uuidToString(ap.ID), Label: "Activate", Impact: "the autopilot runs on its schedule, first on " + runs[0].In(mustLocation(req.Timezone)).Format("Mon 2 Jan 15:04")},
			{ID: autopilotDiscardOption + uuidToString(ap.ID), Label: "Discard", Impact: "the autopilot is archived and never runs"},
		})
		askedByType, askedByID := actorType, createdByID
		decision, err := h.Queries.CreateIssueDecision(r.Context(), db.CreateIssueDecisionParams{
			WorkspaceID: wsUUID, IssueID: issue.ID, AskedByType: askedByType, AskedByID: askedByID,
			Question: "Proposed autopilot · " + ap.Title + " · " + req.CronExpression + " (" + req.Timezone + ")", Options: options, Urgency: "normal", SlaDeadlineAt: h.decisionDeadline(r.Context(), wsUUID),
		})
		if err == nil {
			id := uuidToString(decision.ID)
			decisionID = &id
			h.notifyDecisionRequested(r.Context(), *issue, decision, actorType, actorID)
		}
	}
	h.audit(r.Context(), wsUUID, actorType, actorID, AuditAutopilotProposed, "autopilot", ap.ID, map[string]any{"title": ap.Title, "cron": req.CronExpression, "timezone": req.Timezone, "decision_id": decisionID}, nil)
	h.publish(protocol.EventAutopilotCreated, workspaceID, actorType, actorID, map[string]any{"autopilot": autopilotToResponse(ap, nil)})
	writeJSON(w, http.StatusCreated, map[string]any{"autopilot": autopilotToResponse(ap, nil), "decision_id": decisionID, "next_runs": next})
}

// applyAutopilotForDecision settles a proposal when its card is answered.
func (h *Handler) applyAutopilotForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	if !strings.HasPrefix(optionID, autopilotOptionPrefix) {
		return false
	}
	var apID pgtype.UUID
	activate := false
	switch {
	case strings.HasPrefix(optionID, autopilotActivateOption):
		activate = true
		optionID = strings.TrimPrefix(optionID, autopilotActivateOption)
	case strings.HasPrefix(optionID, autopilotDiscardOption):
		optionID = strings.TrimPrefix(optionID, autopilotDiscardOption)
	default:
		return false
	}
	// The option id comes from the client's answer, not from our own rows:
	// a malformed one is not an autopilot option, never a panic.
	parsed, err := util.ParseUUID(optionID)
	if err != nil {
		slog.Warn("autopilot decision: malformed option id", "option_id", optionID)
		return false
	}
	apID = parsed
	ap, err := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{ID: apID, WorkspaceID: decision.WorkspaceID})
	if err != nil {
		return true
	}
	if activate {
		triggers, _ := h.Queries.ListAutopilotTriggers(ctx, ap.ID)
		for _, tr := range triggers {
			next := pgtype.Timestamptz{}
			if tr.CronExpression.Valid {
				if runs, err := service.NextOccurrencesAfterUTC(tr.CronExpression.String, tr.Timezone.String, time.Now().UTC(), 1); err == nil && len(runs) == 1 {
					next = pgtype.Timestamptz{Time: runs[0], Valid: true}
				}
			}
			_, _ = h.Queries.UpdateAutopilotTrigger(ctx, db.UpdateAutopilotTriggerParams{ID: tr.ID, Enabled: pgtype.Bool{Bool: true, Valid: true}, NextRunAt: next})
		}
		if updated, err := h.Queries.UpdateAutopilot(ctx, db.UpdateAutopilotParams{ID: ap.ID, Status: pgtype.Text{String: "active", Valid: true}}); err == nil {
			ap = updated
		}
	} else {
		_ = h.Queries.ArchiveAutopilot(ctx, ap.ID)
		ap.Status = "archived"
	}
	h.audit(ctx, ap.WorkspaceID, actorType, actorID, AuditAutopilotDecided, "autopilot", ap.ID, map[string]any{"activated": activate, "decision_id": uuidToString(decision.ID)}, nil)
	h.publish(protocol.EventAutopilotUpdated, uuidToString(ap.WorkspaceID), actorType, actorID, map[string]any{"autopilot": autopilotToResponse(ap, nil)})
	return true
}

// autopilotToolAdapter replays ProposeAutopilot for the native tool.
type autopilotToolAdapter struct{ h *Handler }

func (a autopilotToolAdapter) Propose(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, input map[string]any) (any, error) {
	body := map[string]any{}
	for k, v := range input {
		body[k] = v
	}
	return calendarToolAdapter{h: a.h, taskID: uuidToString(task.ID)}.call(ctx, http.MethodPost, "/api/autopilots/propose", nil, body, a.h.ProposeAutopilot, agent.WorkspaceID, "agent", uuidToString(agent.ID), nil)
}
