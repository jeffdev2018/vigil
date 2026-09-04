package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Module ownership (K33): rules mapping a path pattern or a label to an
// owner member and an optional referent agent. An issue gets the most
// specific matching rule as a suggestion — on its page and in the owner's
// inbox — and a human applies it with a click. The server has no diff file
// list for an issue, so path rules match the paths the issue names: its
// title and description (the scoping assistant's probable files included)
// and the branches of its pull requests.

const (
	ErrCodeInvalidPathPattern = "invalid_path_pattern"
	ownershipMaxRules         = 200
	ownershipMaxPaths         = 200
	ownershipLabelSpecificity = 1000
)

type ModuleOwnershipRule struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	PathPattern     *string `json:"path_pattern"`
	LabelID         *string `json:"label_id"`
	OwnerUserID     string  `json:"owner_user_id"`
	ReferentAgentID *string `json:"referent_agent_id"`
	Priority        int32   `json:"priority"`
	CreatedAt       string  `json:"created_at"`
}

func moduleOwnershipToResponse(m db.ModuleOwnership) ModuleOwnershipRule {
	return ModuleOwnershipRule{
		ID: uuidToString(m.ID), WorkspaceID: uuidToString(m.WorkspaceID),
		PathPattern: textToPtr(m.PathPattern), LabelID: uuidToPtr(m.LabelID),
		OwnerUserID: uuidToString(m.OwnerUserID), ReferentAgentID: uuidToPtr(m.ReferentAgentID),
		Priority: m.Priority, CreatedAt: timestampToString(m.CreatedAt),
	}
}

// OwnershipSuggestion is what a rule yields for an issue.
type OwnershipSuggestion struct {
	RuleID          string  `json:"rule_id"`
	OwnerUserID     string  `json:"owner_user_id"`
	ReferentAgentID *string `json:"referent_agent_id"`
	// Matched names what the rule matched: "label:<id>" or "path:<path>".
	Matched string `json:"matched"`
	Pattern string `json:"pattern"`
}

// compileGlob turns a path pattern into a regexp: `**` crosses directories,
// `*` and `?` stay inside one segment, and a pattern without wildcards is a
// directory prefix (`packages/core/billing` matches everything under it).
func compileGlob(pattern string) (*regexp.Regexp, error) {
	pattern = strings.Trim(strings.TrimSpace(pattern), "/")
	if pattern == "" {
		return nil, errors.New("path_pattern is required")
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return regexp.Compile("^" + regexp.QuoteMeta(pattern) + "(/.*)?$")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			i++
			if i+1 < len(pattern) && pattern[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			return nil, errors.New("character classes are not supported in path_pattern")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

var (
	pathTokenRe = regexp.MustCompile(`[A-Za-z0-9_.@-]+(?:/[A-Za-z0-9_.@-]+)+/?|[A-Za-z0-9_-]+\.[a-z]{1,6}\b`)
	urlRe       = regexp.MustCompile(`[a-z]+://\S+`)
)

// extractPaths pulls path-like tokens out of free text: anything with a
// slash between word characters, or a bare file name with an extension.
func extractPaths(texts ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, text := range texts {
		for _, tok := range pathTokenRe.FindAllString(urlRe.ReplaceAllString(text, " "), -1) {
			tok = strings.Trim(tok, "/.")
			if tok == "" || seen[tok] || len(out) >= ownershipMaxPaths {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

type ownershipMatch struct {
	rule        db.ModuleOwnership
	specificity int
	matched     string
}

// resolveOwnership picks the rule that says the most: priority first, then a
// label over a path, then the longer pattern, then the newest rule.
func resolveOwnership(rules []db.ModuleOwnership, labelIDs []string, paths []string) *ownershipMatch {
	labels := map[string]bool{}
	for _, id := range labelIDs {
		labels[id] = true
	}
	var matches []ownershipMatch
	for _, rule := range rules {
		if rule.LabelID.Valid && labels[uuidToString(rule.LabelID)] {
			matches = append(matches, ownershipMatch{rule: rule, specificity: ownershipLabelSpecificity, matched: "label:" + uuidToString(rule.LabelID)})
			continue
		}
		if !rule.PathPattern.Valid {
			continue
		}
		re, err := compileGlob(rule.PathPattern.String)
		if err != nil {
			continue
		}
		for _, p := range paths {
			if re.MatchString(p) {
				matches = append(matches, ownershipMatch{rule: rule, specificity: len(strings.ReplaceAll(rule.PathPattern.String, "*", "")), matched: "path:" + p})
				break
			}
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.rule.Priority != b.rule.Priority {
			return a.rule.Priority > b.rule.Priority
		}
		if a.specificity != b.specificity {
			return a.specificity > b.specificity
		}
		return a.rule.CreatedAt.Time.After(b.rule.CreatedAt.Time)
	})
	return &matches[0]
}

// issuePaths gathers what the issue names: its text and its PR branches.
func (h *Handler) issuePaths(ctx context.Context, issue db.Issue) []string {
	texts := []string{issue.Title, issue.Description.String}
	if prs, err := h.Queries.ListVCSPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range prs {
			texts = append(texts, pr.Branch.String)
		}
	}
	if prs, err := h.Queries.ListPullRequestsByIssue(ctx, issue.ID); err == nil {
		for _, pr := range prs {
			texts = append(texts, pr.Branch.String)
		}
	}
	return extractPaths(texts...)
}

func (h *Handler) ownershipSuggestionFor(ctx context.Context, issue db.Issue) (*OwnershipSuggestion, error) {
	rules, err := h.Queries.ListModuleOwnership(ctx, issue.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}
	labels, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return nil, err
	}
	labelIDs := make([]string, 0, len(labels))
	for _, l := range labels {
		labelIDs = append(labelIDs, uuidToString(l.ID))
	}
	m := resolveOwnership(rules, labelIDs, h.issuePaths(ctx, issue))
	if m == nil {
		return nil, nil
	}
	pattern := m.rule.PathPattern.String
	if m.rule.LabelID.Valid {
		pattern = "label:" + uuidToString(m.rule.LabelID)
	}
	return &OwnershipSuggestion{
		RuleID: uuidToString(m.rule.ID), OwnerUserID: uuidToString(m.rule.OwnerUserID),
		ReferentAgentID: uuidToPtr(m.rule.ReferentAgentID), Matched: m.matched, Pattern: pattern,
	}, nil
}

// suggestOwnership files an inbox item for the suggested owner unless the
// issue is already theirs. Best effort: the issue exists either way.
func (h *Handler) suggestOwnership(ctx context.Context, issue db.Issue, actorType, actorID string) {
	s, err := h.ownershipSuggestionFor(ctx, issue)
	if err != nil {
		slog.Warn("ownership suggestion failed", "error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	if s == nil || (issue.AssigneeType.String == "member" && uuidToString(issue.AssigneeID) == s.OwnerUserID) {
		return
	}
	details, _ := json.Marshal(s)
	item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID,
		RecipientType: "member", RecipientID: parseUUID(s.OwnerUserID),
		Type: "ownership_suggested", Severity: "info", IssueID: issue.ID, Title: issue.Title,
		Body:      pgtype.Text{String: "Matched " + s.Matched, Valid: true},
		ActorType: pgtype.Text{String: actorType, Valid: true}, ActorID: parseUUID(actorID), Details: details,
	})
	if err != nil {
		slog.Warn("ownership suggestion inbox failed", "error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"item": inboxToResponse(item)})
}

// GetOwnershipSuggestion — GET /api/issues/{id}/ownership-suggestion.
func (h *Handler) GetOwnershipSuggestion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, err := h.ownershipSuggestionFor(r.Context(), issue)
	if err != nil {
		slog.Warn("ownership suggestion failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve ownership")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestion": s})
}

// ListModuleOwnership — GET /api/module-ownership.
func (h *Handler) ListModuleOwnership(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	rules, err := h.Queries.ListModuleOwnership(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list ownership rules")
		return
	}
	out := make([]ModuleOwnershipRule, 0, len(rules))
	for _, m := range rules {
		out = append(out, moduleOwnershipToResponse(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// CreateModuleOwnership — POST /api/module-ownership (owner/admin).
func (h *Handler) CreateModuleOwnership(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var req struct {
		PathPattern     string `json:"path_pattern"`
		LabelID         string `json:"label_id"`
		OwnerUserID     string `json:"owner_user_id"`
		ReferentAgentID string `json:"referent_agent_id"`
		Priority        int32  `json:"priority"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	req.PathPattern = strings.TrimSpace(req.PathPattern)
	if req.PathPattern == "" && req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "a rule needs a path_pattern or a label_id")
		return
	}
	if req.Priority < 0 {
		writeError(w, http.StatusBadRequest, "priority must be zero or more")
		return
	}
	params := db.CreateModuleOwnershipParams{WorkspaceID: wsUUID, Priority: req.Priority}
	if req.PathPattern != "" {
		if _, err := compileGlob(req.PathPattern); err != nil {
			writeErrorCode(w, http.StatusBadRequest, ErrCodeInvalidPathPattern, err.Error())
			return
		}
		params.PathPattern = pgtype.Text{String: req.PathPattern, Valid: true}
	}
	if req.LabelID != "" {
		labelID, err := util.ParseUUID(req.LabelID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid label_id")
			return
		}
		if _, err := h.Queries.GetLabel(ctx, db.GetLabelParams{ID: labelID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		params.LabelID = labelID
	}
	ownerID, err := util.ParseUUID(req.OwnerUserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "owner_user_id is required")
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: ownerID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "owner is not a member of this workspace")
		return
	}
	params.OwnerUserID = ownerID
	if req.ReferentAgentID != "" {
		agentID, err := util.ParseUUID(req.ReferentAgentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid referent_agent_id")
			return
		}
		if _, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusNotFound, "referent agent not found")
			return
		}
		params.ReferentAgentID = agentID
	}
	if existing, err := h.Queries.ListModuleOwnership(ctx, wsUUID); err == nil && len(existing) >= ownershipMaxRules {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d ownership rules", ownershipMaxRules))
		return
	}
	rule, err := h.Queries.CreateModuleOwnership(ctx, params)
	if err != nil {
		slog.Warn("create ownership rule failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": moduleOwnershipToResponse(rule)})
}

// DeleteModuleOwnership — DELETE /api/module-ownership/{id} (owner/admin).
// Issues assigned through the rule keep their assignee: the rule was advice.
func (h *Handler) DeleteModuleOwnership(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "rule id")
	if !ok {
		return
	}
	n, err := h.Queries.DeleteModuleOwnership(r.Context(), db.DeleteModuleOwnershipParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the rule")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
