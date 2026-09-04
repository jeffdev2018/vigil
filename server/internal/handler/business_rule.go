package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Business rules (K53): written in plain language by an owner or admin,
// compiled once into a predicate (by the LLM, or given directly), previewed
// and dry-run before activation, then evaluated deterministically at a fixed
// attach point. A violation blocks the action with the rule's title and the
// observed facts, and is recorded.

const ErrCodeBusinessRuleViolation = "business_rule_violation"

type BusinessRuleResponse struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	Title             string          `json:"title"`
	NaturalLanguage   string          `json:"natural_language"`
	Predicate         json.RawMessage `json:"predicate"`
	Description       string          `json:"description"`
	AttachPoint       string          `json:"attach_point"`
	Action            json.RawMessage `json:"action,omitempty"`
	ActionDescription string          `json:"action_description,omitempty"`
	Status            string          `json:"status"`
	CreatedBy         string          `json:"created_by"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

func businessRuleToResponse(rule db.BusinessRule) BusinessRuleResponse {
	desc := ""
	if p, err := service.ParsePredicate(rule.CompiledPredicate, rule.AttachPoint); err == nil {
		desc = p.Describe()
	}
	out := BusinessRuleResponse{
		ID: uuidToString(rule.ID), WorkspaceID: uuidToString(rule.WorkspaceID), Title: rule.Title,
		NaturalLanguage: rule.NaturalLanguage, Predicate: json.RawMessage(rule.CompiledPredicate), Description: desc,
		AttachPoint: rule.AttachPoint, Status: rule.Status, CreatedBy: uuidToString(rule.CreatedBy),
		CreatedAt: timestampToString(rule.CreatedAt), UpdatedAt: timestampToString(rule.UpdatedAt),
	}
	if len(rule.ActionSpec) > 0 {
		out.Action = json.RawMessage(rule.ActionSpec)
		if a, err := service.ParseActionSpec(rule.ActionSpec, rule.AttachPoint); err == nil && a != nil {
			out.ActionDescription = a.Describe()
		}
	}
	return out
}

type BusinessRuleViolationResponse struct {
	ID          string  `json:"id"`
	RuleID      string  `json:"rule_id"`
	SubjectType string  `json:"subject_type"`
	SubjectID   string  `json:"subject_id"`
	Detail      *string `json:"detail"`
	CreatedAt   string  `json:"created_at"`
}

func violationToResponse(v db.BusinessRuleViolation) BusinessRuleViolationResponse {
	return BusinessRuleViolationResponse{ID: uuidToString(v.ID), RuleID: uuidToString(v.RuleID), SubjectType: v.SubjectType, SubjectID: uuidToString(v.SubjectID), Detail: textToPtr(v.Detail), CreatedAt: timestampToString(v.CreatedAt)}
}

// ---- facts ----

func (h *Handler) workspaceFacts(ctx context.Context, wsID pgtype.UUID) (map[string]any, error) {
	projects, err := h.Queries.CountWorkspaceProjects(ctx, wsID)
	if err != nil {
		return nil, err
	}
	members, err := h.Queries.CountWorkspaceMembers(ctx, wsID)
	if err != nil {
		return nil, err
	}
	agents, err := h.Queries.CountWorkspaceAgents(ctx, wsID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"workspace.project_count": projects, "workspace.member_count": members, "workspace.agent_count": agents}, nil
}

// issueFacts describes one issue on top of its workspace. status overrides
// the stored one when the check runs before the write lands.
func (h *Handler) issueFacts(ctx context.Context, issue db.Issue) (map[string]any, error) {
	facts, err := h.workspaceFacts(ctx, issue.WorkspaceID)
	if err != nil {
		return nil, err
	}
	labels, err := h.Queries.CountIssueLabels(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	prs, err := h.Queries.CountIssuePullRequests(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	decisions, err := h.Queries.CountIssueDecisionRecords(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	assignee := "none"
	if issue.AssigneeType.Valid && issue.AssigneeType.String != "" && issue.AssigneeID.Valid {
		assignee = issue.AssigneeType.String
	}
	facts["issue.title_length"] = len([]rune(issue.Title))
	facts["issue.has_description"] = strings.TrimSpace(issue.Description.String) != ""
	facts["issue.acceptance_criteria_count"] = len(parseAcceptanceCriteria(issue.AcceptanceCriteria))
	facts["issue.label_count"] = labels
	facts["issue.pull_request_count"] = prs
	facts["issue.decision_count"] = decisions
	facts["issue.priority"] = issue.Priority
	facts["issue.assignee_type"] = assignee
	return facts, nil
}

// ---- enforcement ----

// enforceBusinessRules evaluates the active rules of an attach point. On the
// first violation it records it, writes 422 business_rule_violation and
// returns false. An evaluation failure is logged and lets the action through:
// a broken rules engine must not lock the product.
func (h *Handler) enforceBusinessRules(w http.ResponseWriter, r *http.Request, wsID pgtype.UUID, attach string, facts map[string]any, subjectType string, subjectID pgtype.UUID) bool {
	rules, err := h.Queries.ListActiveBusinessRules(r.Context(), db.ListActiveBusinessRulesParams{WorkspaceID: wsID, AttachPoint: attach})
	if err != nil {
		slog.Warn("business rules: list failed", append(logger.RequestAttrs(r), "error", err)...)
		return true
	}
	for _, rule := range rules {
		p, err := service.ParsePredicate(rule.CompiledPredicate, rule.AttachPoint)
		if err != nil {
			slog.Warn("business rules: stored predicate invalid", "rule_id", uuidToString(rule.ID), "error", err)
			continue
		}
		ok, detail := p.Evaluate(facts)
		if ok {
			continue
		}
		if _, err := h.Queries.CreateBusinessRuleViolation(r.Context(), db.CreateBusinessRuleViolationParams{
			ID: dbid.NewV7(), RuleID: rule.ID, WorkspaceID: wsID, SubjectType: subjectType, SubjectID: subjectID, Detail: pgtype.Text{String: detail, Valid: true},
		}); err != nil {
			slog.Warn("business rules: record violation failed", "rule_id", uuidToString(rule.ID), "error", err)
		}
		h.audit(r.Context(), wsID, "system", "", AuditBusinessRuleViolated, subjectType, subjectID, map[string]any{"rule_id": uuidToString(rule.ID), "title": rule.Title, "detail": detail}, nil)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"code": ErrCodeBusinessRuleViolation, "error": rule.Title + ": " + detail,
			"rule_id": uuidToString(rule.ID), "title": rule.Title, "detail": detail,
		})
		return false
	}
	return true
}

// projectCreateAllowed runs the project_create rules on the workspace as it
// will be once the project exists.
func (h *Handler) projectCreateAllowed(w http.ResponseWriter, r *http.Request, wsID pgtype.UUID) bool {
	facts, err := h.workspaceFacts(r.Context(), wsID)
	if err != nil {
		slog.Warn("business rules: workspace facts failed", append(logger.RequestAttrs(r), "error", err)...)
		return true
	}
	facts["workspace.project_count"] = facts["workspace.project_count"].(int64) + 1
	return h.enforceBusinessRules(w, r, wsID, service.AttachProjectCreate, facts, "project_create", wsID)
}

// issueSubmitReviewAllowed runs the issue_submit_review rules when an issue
// enters the in_review category from outside it.
func (h *Handler) issueSubmitReviewAllowed(w http.ResponseWriter, r *http.Request, prev db.Issue, statusKey string) bool {
	if statusKey == "" || statusKey == prev.Status {
		return true
	}
	ctx := r.Context()
	if issuestatus.Effective(ctx, h.Queries, prev.WorkspaceID, statusKey) != issuestatus.InReview || issuestatus.Effective(ctx, h.Queries, prev.WorkspaceID, prev.Status) == issuestatus.InReview {
		return true
	}
	facts, err := h.issueFacts(ctx, prev)
	if err != nil {
		slog.Warn("business rules: issue facts failed", append(logger.RequestAttrs(r), "error", err)...)
		return true
	}
	return h.enforceBusinessRules(w, r, prev.WorkspaceID, service.AttachIssueSubmitReview, facts, "issue", prev.ID)
}

// ---- compilation ----

const businessRuleSystemPrompt = `You translate a product manager's rule, written in plain language, into a predicate over a fixed catalog of facts. The predicate must HOLD for the action to be allowed: express what a compliant situation looks like.
Reply with JSON only: {"title":"<short rule title>","predicate":{"all":[{"field":"<field>","op":"<op>","value":<value>}],"any":[...]},"action":<action or null>}.
Operators: eq, ne, lt, lte, gt, gte for numbers; eq, ne for booleans and strings; in with a list for strings; eq, ne, contains, starts_with for text. Use only the fields listed; if the rule cannot be expressed with them, reply {"title":"","predicate":{},"error":"<why>"}.
For the webhook_received attach point the predicate is the MATCH condition and "action" is required: {"kind":"dismiss"} or {"kind":"accept","priority":"urgent|high|medium|low|none"}. Other attach points get "action": null.`

func businessRuleUserPrompt(attach, text string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attach point: %s\nAvailable fields:\n", attach)
	for _, name := range service.FieldsFor(attach) {
		f := service.RuleFields[name]
		fmt.Fprintf(&b, "- %s (%s): %s", name, f.Kind, f.Label)
		if len(f.Values) > 0 {
			fmt.Fprintf(&b, " — one of %s", strings.Join(f.Values, ", "))
		}
		b.WriteString("\n")
	}
	if attach == service.AttachProjectCreate {
		b.WriteString("Note: workspace.project_count already includes the project being created.\n")
	}
	if attach == service.AttachWebhookReceived {
		b.WriteString("Note: the rule describes deliveries to act on; time fields are in the workspace timezone.\n")
	}
	fmt.Fprintf(&b, "Rule: %s\n", text)
	return b.String()
}

func (h *Handler) compileBusinessRule(ctx context.Context, attach, text string) (title string, predicate, action []byte, err error) {
	raw, err := h.LLM.GenerateJSON(ctx, "", businessRuleSystemPrompt, businessRuleUserPrompt(attach, text), 0, 1024)
	if err != nil {
		return "", nil, nil, err
	}
	var out struct {
		Title     string          `json:"title"`
		Predicate json.RawMessage `json:"predicate"`
		Action    json.RawMessage `json:"action"`
		Error     string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", nil, nil, fmt.Errorf("malformed compilation: %w", err)
	}
	if out.Error != "" {
		return "", nil, nil, fmt.Errorf("rule cannot be expressed: %s", out.Error)
	}
	return strings.TrimSpace(out.Title), out.Predicate, out.Action, nil
}

// ---- endpoints ----

func (h *Handler) businessRuleScope(w http.ResponseWriter, r *http.Request, roles ...string) (pgtype.UUID, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, "", false
	}
	if len(roles) == 0 {
		if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
			return pgtype.UUID{}, "", false
		}
	} else if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", roles...); !ok {
		return pgtype.UUID{}, "", false
	}
	return wsUUID, userID, true
}

// GET /api/business-rules
func (h *Handler) ListBusinessRules(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.businessRuleScope(w, r)
	if !ok {
		return
	}
	rules, err := h.Queries.ListBusinessRules(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list business rules")
		return
	}
	out := make([]BusinessRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, businessRuleToResponse(rule))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out, "attach_points": service.AttachPoints})
}

// POST /api/business-rules (owner/admin): compiles and stores a draft.
// A predicate given directly skips the model.
func (h *Handler) CreateBusinessRule(w http.ResponseWriter, r *http.Request) {
	wsUUID, userID, ok := h.businessRuleScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req struct {
		NaturalLanguage string          `json:"natural_language"`
		AttachPoint     string          `json:"attach_point"`
		Title           string          `json:"title"`
		Predicate       json.RawMessage `json:"predicate"`
		Action          json.RawMessage `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.NaturalLanguage = strings.TrimSpace(req.NaturalLanguage)
	if req.NaturalLanguage == "" || len(req.NaturalLanguage) > 2000 {
		writeError(w, http.StatusBadRequest, "natural_language is required (at most 2000 characters)")
		return
	}
	validAttach := false
	for _, a := range service.AttachPoints {
		validAttach = validAttach || a == req.AttachPoint
	}
	if !validAttach {
		writeError(w, http.StatusBadRequest, "attach_point must be one of "+strings.Join(service.AttachPoints, ", "))
		return
	}
	title, predicate, action := strings.TrimSpace(req.Title), []byte(req.Predicate), []byte(req.Action)
	if len(predicate) == 0 {
		if h.LLM == nil || !h.LLM.Enabled() {
			writeErrorCode(w, http.StatusServiceUnavailable, "llm_unavailable", "rule compilation needs an LLM; give a predicate directly instead")
			return
		}
		var err error
		var compiledAction []byte
		title, predicate, compiledAction, err = h.compileBusinessRule(r.Context(), req.AttachPoint, req.NaturalLanguage)
		if len(action) == 0 {
			action = compiledAction
		}
		if err != nil {
			slog.Warn("business rule compilation failed", append(logger.RequestAttrs(r), "error", err)...)
			writeErrorCode(w, http.StatusBadGateway, "rule_malformed", err.Error())
			return
		}
	}
	parsed, err := service.ParsePredicate(predicate, req.AttachPoint)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "rule_malformed", err.Error())
		return
	}
	actionSpec, err := service.ParseActionSpec(action, req.AttachPoint)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "rule_malformed", err.Error())
		return
	}
	var actionRaw []byte
	if actionSpec != nil {
		actionRaw, _ = json.Marshal(actionSpec)
	}
	if title == "" {
		title = req.NaturalLanguage
		if len([]rune(title)) > 80 {
			title = string([]rune(title)[:77]) + "…"
		}
	}
	canonical, _ := json.Marshal(parsed)
	rule, err := h.Queries.CreateBusinessRule(r.Context(), db.CreateBusinessRuleParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: title, NaturalLanguage: req.NaturalLanguage,
		CompiledPredicate: canonical, AttachPoint: req.AttachPoint, CreatedBy: parseUUID(userID), ActionSpec: actionRaw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": businessRuleToResponse(rule)})
}

func (h *Handler) loadBusinessRule(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.BusinessRule, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "rule id")
	if !ok {
		return db.BusinessRule{}, false
	}
	rule, err := h.Queries.GetBusinessRule(r.Context(), db.GetBusinessRuleParams{ID: id, WorkspaceID: wsUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rule not found")
		return db.BusinessRule{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the rule")
		return db.BusinessRule{}, false
	}
	return rule, true
}

type DryRunSubject struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Label       string `json:"label"`
	Detail      string `json:"detail"`
}

// POST /api/business-rules/{id}/dry-run (owner/admin): what the rule would
// do today, without blocking anything.
func (h *Handler) DryRunBusinessRule(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.businessRuleScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	rule, ok := h.loadBusinessRule(w, r, wsUUID)
	if !ok {
		return
	}
	p, err := service.ParsePredicate(rule.CompiledPredicate, rule.AttachPoint)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "rule_malformed", err.Error())
		return
	}
	ctx := r.Context()
	violations := []DryRunSubject{}
	checked := 0
	switch rule.AttachPoint {
	case service.AttachProjectCreate:
		facts, err := h.workspaceFacts(ctx, wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read workspace facts")
			return
		}
		facts["workspace.project_count"] = facts["workspace.project_count"].(int64) + 1
		checked = 1
		if ok, detail := p.Evaluate(facts); !ok {
			violations = append(violations, DryRunSubject{SubjectType: "project_create", SubjectID: uuidToString(wsUUID), Label: "the next project", Detail: detail})
		}
	case service.AttachWebhookReceived:
		items, err := h.Queries.ListRecentTriageItemsForRules(ctx, wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list triage items")
			return
		}
		action, _ := service.ParseActionSpec(rule.ActionSpec, rule.AttachPoint)
		for _, item := range items {
			facts, err := h.triageItemFacts(ctx, item, item.FirstSeenAt.Time)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read triage facts")
				return
			}
			checked++
			if ok, _ := p.Evaluate(facts); ok {
				detail := "would match"
				if action != nil {
					detail = "would " + action.Describe()
				}
				violations = append(violations, DryRunSubject{SubjectType: "triage_item", SubjectID: uuidToString(item.ID), Label: item.Title, Detail: detail})
			}
		}
	case service.AttachIssueSubmitReview, service.AttachAgentRunDispatch:
		issues, err := h.Queries.ListWorkspaceIssuesInReview(ctx, wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list issues")
			return
		}
		prefix := h.getIssuePrefix(ctx, wsUUID)
		for _, issue := range issues {
			facts, err := h.issueFacts(ctx, issue)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read issue facts")
				return
			}
			checked++
			if ok, detail := p.Evaluate(facts); !ok {
				violations = append(violations, DryRunSubject{SubjectType: "issue", SubjectID: uuidToString(issue.ID), Label: fmt.Sprintf("%s-%d %s", prefix, issue.Number, issue.Title), Detail: detail})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": businessRuleToResponse(rule), "checked": checked, "violations": violations})
}

func (h *Handler) setBusinessRuleStatus(w http.ResponseWriter, r *http.Request, status string) {
	wsUUID, userID, ok := h.businessRuleScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	rule, ok := h.loadBusinessRule(w, r, wsUUID)
	if !ok {
		return
	}
	if rule.Status == status {
		writeErrorCode(w, http.StatusConflict, "rule_status_unchanged", "the rule is already "+status)
		return
	}
	updated, err := h.Queries.SetBusinessRuleStatus(r.Context(), db.SetBusinessRuleStatusParams{ID: rule.ID, WorkspaceID: wsUUID, Status: status})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the rule")
		return
	}
	h.audit(r.Context(), wsUUID, "member", userID, "business_rule."+status, "business_rule", rule.ID, map[string]any{"title": rule.Title, "attach_point": rule.AttachPoint}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"rule": businessRuleToResponse(updated)})
}

// PUT /api/business-rules/{id}/activate — only after a human read the preview.
func (h *Handler) ActivateBusinessRule(w http.ResponseWriter, r *http.Request) {
	h.setBusinessRuleStatus(w, r, "active")
}

// PUT /api/business-rules/{id}/disable — stops the rule; violations stay.
func (h *Handler) DisableBusinessRule(w http.ResponseWriter, r *http.Request) {
	h.setBusinessRuleStatus(w, r, "disabled")
}

// DELETE /api/business-rules/{id} — drafts and disabled rules only.
func (h *Handler) DeleteBusinessRule(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.businessRuleScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	rule, ok := h.loadBusinessRule(w, r, wsUUID)
	if !ok {
		return
	}
	if rule.Status == "active" {
		writeErrorCode(w, http.StatusConflict, "rule_active", "disable the rule before deleting it")
		return
	}
	if err := h.Queries.DeleteBusinessRule(r.Context(), db.DeleteBusinessRuleParams{ID: rule.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/business-rules/{id}/violations
func (h *Handler) ListBusinessRuleViolations(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.businessRuleScope(w, r)
	if !ok {
		return
	}
	rule, ok := h.loadBusinessRule(w, r, wsUUID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListBusinessRuleViolations(r.Context(), db.ListBusinessRuleViolationsParams{RuleID: rule.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list violations")
		return
	}
	out := make([]BusinessRuleViolationResponse, 0, len(rows))
	for _, v := range rows {
		out = append(out, violationToResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"violations": out})
}

// ---- triage rules (K62) ----

const AuditBusinessRuleApplied = "business_rule.applied"

var weekdayNames = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// triageItemFacts describes a parked delivery: its source, its text, and
// the workspace-local time it arrived.
func (h *Handler) triageItemFacts(ctx context.Context, item db.TriageItem, at time.Time) (map[string]any, error) {
	facts, err := h.workspaceFacts(ctx, item.WorkspaceID)
	if err != nil {
		return nil, err
	}
	kind, name := "", ""
	if source, err := h.Queries.GetTriageSource(ctx, db.GetTriageSourceParams{ID: item.SourceID, WorkspaceID: item.WorkspaceID}); err == nil {
		kind, name = source.Kind, source.Name
	}
	ws, err := h.Queries.GetWorkspace(ctx, item.WorkspaceID)
	if err != nil {
		return nil, err
	}
	local := at.In(briefingLocation(service.WorkspaceTimezone(ws.Settings)))
	facts["webhook.source_kind"] = kind
	facts["webhook.source_name"] = name
	facts["webhook.title"] = item.Title
	facts["webhook.body"] = item.BodyMarkdown
	facts["webhook.payload"] = string(item.Payload)
	facts["webhook.collapse_count"] = int(item.CollapseCount)
	facts["time.weekday"] = weekdayNames[local.Weekday()]
	facts["time.hour"] = local.Hour()
	return facts, nil
}

// ApplyTriageRules runs the active webhook rules on a freshly parked item:
// the first rule whose condition holds applies its action. Best effort and
// logged; the item stays pending for a human when nothing matches or the
// action fails.
func (h *Handler) ApplyTriageRules(ctx context.Context, item db.TriageItem) {
	if item.State != "pending" || item.Shadow {
		return
	}
	rules, err := h.Queries.ListActiveBusinessRules(ctx, db.ListActiveBusinessRulesParams{WorkspaceID: item.WorkspaceID, AttachPoint: service.AttachWebhookReceived})
	if err != nil || len(rules) == 0 {
		return
	}
	facts, err := h.triageItemFacts(ctx, item, time.Now())
	if err != nil {
		slog.Warn("triage rules: facts failed", "error", err, "item_id", uuidToString(item.ID))
		return
	}
	for _, rule := range rules {
		p, err := service.ParsePredicate(rule.CompiledPredicate, rule.AttachPoint)
		if err != nil {
			continue
		}
		if ok, _ := p.Evaluate(facts); !ok {
			continue
		}
		action, err := service.ParseActionSpec(rule.ActionSpec, rule.AttachPoint)
		if err != nil || action == nil {
			continue
		}
		detail := action.Describe()
		switch action.Kind {
		case "dismiss":
			if _, err := h.Queries.DismissPendingTriageItem(ctx, db.DismissPendingTriageItemParams{
				ID: item.ID, WorkspaceID: item.WorkspaceID, ResolutionReason: pgtype.Text{String: "rule: " + rule.Title, Valid: true}, ResolvedBy: rule.CreatedBy,
			}); err != nil {
				slog.Warn("triage rules: dismiss failed", "error", err, "item_id", uuidToString(item.ID))
				return
			}
			h.publishTriageResolved(item.WorkspaceID, item.ID, "dismissed")
		case "accept":
			res := h.acceptTriageItemCore(ctx, item.WorkspaceID, uuidToString(rule.CreatedBy), item.ID)
			if res.outcome != "accepted" {
				slog.Warn("triage rules: accept did not create an issue", "outcome", res.outcome, "item_id", uuidToString(item.ID))
				return
			}
			params := db.ApplyTriageRuleIssueOverridesParams{ID: res.issue.ID}
			if action.Priority != "" {
				params.Priority = pgtype.Text{String: action.Priority, Valid: true}
			}
			if action.AssigneeID != "" {
				var assignee pgtype.UUID
				if assignee.Scan(action.AssigneeID) == nil {
					params.AssigneeID = assignee
					params.AssigneeType = pgtype.Text{String: action.AssigneeType, Valid: true}
				}
			}
			if err := h.Queries.ApplyTriageRuleIssueOverrides(ctx, params); err != nil {
				slog.Warn("triage rules: overrides failed", "error", err, "issue_id", uuidToString(res.issue.ID))
			}
			h.publishTriageResolved(item.WorkspaceID, item.ID, "accepted")
			detail += " → " + res.prefix + "-" + fmt.Sprint(res.issue.Number)
		}
		if _, err := h.Queries.CreateBusinessRuleViolation(ctx, db.CreateBusinessRuleViolationParams{
			ID: dbid.NewV7(), RuleID: rule.ID, WorkspaceID: item.WorkspaceID, SubjectType: "triage_item", SubjectID: item.ID, Detail: pgtype.Text{String: detail, Valid: true},
		}); err != nil {
			slog.Warn("triage rules: record application failed", "error", err)
		}
		h.audit(ctx, item.WorkspaceID, "system", "", AuditBusinessRuleApplied, "triage_item", item.ID, map[string]any{"rule_id": uuidToString(rule.ID), "title": rule.Title, "action": action.Kind, "detail": detail}, nil)
		return
	}
}
