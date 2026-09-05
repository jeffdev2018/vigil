package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Goals with ancestry (K74): a root goal is the workspace mission, sub-goals
// hang under it, projects link to goals and an issue names one directly or
// inherits its project's. A run's brief carries the chain mission → goal →
// project → issue with the goal's success measure. Agents never create or
// edit a goal; they propose an attachment, settled by a human decision.

const (
	goalStatusDraft   = "draft"
	goalStatusActive  = "active"
	goalStatusDone    = "done"
	goalStatusDropped = "dropped"
	// missionChainMaxDepth caps the goal chain a brief carries, root first.
	missionChainMaxDepth = 8
	// missionChainMaxNodeBytes caps one goal's description in the brief.
	missionChainMaxNodeBytes = 1 << 10
	goalTitleMaxRunes        = 200
	goalProposalOptionPrefix = "goal:"
	goalProposalKeepOption   = "goal_keep"
	whySourceGoal            = "goal"
)

var goalStatuses = []string{goalStatusDraft, goalStatusActive, goalStatusDone, goalStatusDropped}

type GoalResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	ParentGoalID   *string `json:"parent_goal_id"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	SuccessMeasure string  `json:"success_measure"`
	DueDate        *string `json:"due_date"`
	OwnerID        *string `json:"owner_id"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	// IssueCount / DoneCount roll up the goal and every goal under it: an
	// issue counts when it names the goal or inherits it from its project.
	IssueCount int64    `json:"issue_count"`
	DoneCount  int64    `json:"done_count"`
	ProjectIDs []string `json:"project_ids"`
}

func goalToResponse(g db.Goal) GoalResponse {
	return GoalResponse{
		ID: uuidToString(g.ID), WorkspaceID: uuidToString(g.WorkspaceID), ParentGoalID: uuidToPtr(g.ParentGoalID),
		Title: g.Title, Description: g.Description, SuccessMeasure: g.SuccessMeasure, DueDate: dateToPtr(g.DueDate),
		OwnerID: uuidToPtr(g.OwnerID), Status: g.Status, CreatedAt: timestampToString(g.CreatedAt), UpdatedAt: timestampToString(g.UpdatedAt),
		ProjectIDs: []string{},
	}
}

// goalWriteRequest is the create and update body. Absent fields keep their
// value on update; an explicit null clears parent, due date or owner.
type goalWriteRequest struct {
	ParentGoalID   *string `json:"parent_goal_id"`
	Title          *string `json:"title"`
	Description    *string `json:"description"`
	SuccessMeasure *string `json:"success_measure"`
	DueDate        *string `json:"due_date"`
	OwnerID        *string `json:"owner_id"`
	Status         *string `json:"status"`
}

// goalTree indexes a workspace's goals for ancestry walks and rollups.
type goalTree struct {
	byID     map[string]db.Goal
	children map[string][]string
}

func (h *Handler) loadGoalTree(ctx context.Context, wsID pgtype.UUID) (goalTree, []db.Goal, error) {
	goals, err := h.Queries.ListGoals(ctx, wsID)
	if err != nil {
		return goalTree{}, nil, err
	}
	t := goalTree{byID: map[string]db.Goal{}, children: map[string][]string{}}
	for _, g := range goals {
		t.byID[uuidToString(g.ID)] = g
		if g.ParentGoalID.Valid {
			p := uuidToString(g.ParentGoalID)
			t.children[p] = append(t.children[p], uuidToString(g.ID))
		}
	}
	return t, goals, nil
}

// ancestry returns the chain from the root down to goalID, cut at the depth
// cap and at the first repeated id (nothing in the schema forbids a cycle).
func (t goalTree) ancestry(goalID string) []db.Goal {
	var chain []db.Goal
	seen := map[string]bool{}
	for id := goalID; id != "" && !seen[id] && len(chain) < missionChainMaxDepth; {
		g, ok := t.byID[id]
		if !ok {
			break
		}
		seen[id] = true
		chain = append(chain, g)
		id = ""
		if g.ParentGoalID.Valid {
			id = uuidToString(g.ParentGoalID)
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// descends reports whether candidate is goalID or one of its descendants,
// which is what a parent change must never point at.
func (t goalTree) descends(candidate, goalID string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if id == candidate {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, c := range t.children[id] {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(goalID)
}

// rollup sums the own counts of a goal and everything under it.
func (t goalTree) rollup(goalID string, own map[string][2]int64) (int64, int64) {
	seen := map[string]bool{}
	var total, done int64
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		total += own[id][0]
		done += own[id][1]
		for _, c := range t.children[id] {
			walk(c)
		}
	}
	walk(goalID)
	return total, done
}

func (h *Handler) goalResponses(ctx context.Context, wsID pgtype.UUID, tree goalTree, goals []db.Goal) []GoalResponse {
	own := map[string][2]int64{}
	if stats, err := h.Queries.GetGoalIssueStats(ctx, db.GetGoalIssueStatsParams{WorkspaceID: wsID, TerminalStatusKeys: h.projectTerminalIssueStatusKeys(ctx, wsID)}); err == nil {
		for _, s := range stats {
			own[uuidToString(s.GoalID)] = [2]int64{s.TotalCount, s.DoneCount}
		}
	} else {
		slog.Warn("goals: issue stats failed", "error", err)
	}
	links := map[string][]string{}
	if rows, err := h.Queries.ListProjectGoals(ctx, wsID); err == nil {
		for _, l := range rows {
			links[uuidToString(l.GoalID)] = append(links[uuidToString(l.GoalID)], uuidToString(l.ProjectID))
		}
	}
	out := make([]GoalResponse, 0, len(goals))
	for _, g := range goals {
		r := goalToResponse(g)
		r.IssueCount, r.DoneCount = tree.rollup(r.ID, own)
		if p := links[r.ID]; p != nil {
			r.ProjectIDs = p
		}
		out = append(out, r)
	}
	return out
}

// GET /api/goals
func (h *Handler) ListGoals(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	tree, goals, err := h.loadGoalTree(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list goals")
		return
	}
	out := h.goalResponses(r.Context(), wsUUID, tree, goals)
	writeJSON(w, http.StatusOK, map[string]any{"goals": out, "total": len(out)})
}

// GET /api/goals/{id}: the goal with the issues that count for it.
func (h *Handler) GetGoal(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	tree, goals, err := h.loadGoalTree(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load goal")
		return
	}
	goal, found := tree.byID[uuidToString(goalID)]
	if !found {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}
	var resp GoalResponse
	for _, g := range h.goalResponses(r.Context(), wsUUID, tree, goals) {
		if g.ID == uuidToString(goal.ID) {
			resp = g
		}
	}
	issues, err := h.Queries.ListGoalIssues(r.Context(), db.ListGoalIssuesParams{WorkspaceID: wsUUID, GoalID: goalID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list goal issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	issueOut := make([]IssueResponse, 0, len(issues))
	for _, i := range issues {
		issueOut = append(issueOut, issueToResponse(i, prefix))
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": resp, "issues": issueOut})
}

// validateGoalWrite applies the guards shared by create and update: a title,
// a known status, a parent in the workspace that is not the goal itself nor
// one of its descendants, an owner who is a member, and an active goal that
// has an owner (governance: a named human is accountable).
func (h *Handler) validateGoalWrite(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, tree goalTree, selfID string, params *db.UpdateGoalParams) bool {
	params.Title = strings.TrimSpace(params.Title)
	if params.Title == "" || len([]rune(params.Title)) > goalTitleMaxRunes {
		writeError(w, http.StatusBadRequest, "title is required (200 characters max)")
		return false
	}
	if !goalStatusKnown(params.Status) {
		writeError(w, http.StatusBadRequest, "status must be one of: draft, active, done, dropped")
		return false
	}
	if params.ParentGoalID.Valid {
		pid := uuidToString(params.ParentGoalID)
		if _, ok := tree.byID[pid]; !ok {
			writeError(w, http.StatusBadRequest, "parent goal not found in this workspace")
			return false
		}
		if selfID != "" && tree.descends(pid, selfID) {
			writeError(w, http.StatusBadRequest, "a goal cannot descend from itself")
			return false
		}
	}
	if params.OwnerID.Valid {
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: params.OwnerID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "owner must be a member of this workspace")
			return false
		}
	}
	if params.Status == goalStatusActive && !params.OwnerID.Valid {
		writeError(w, http.StatusBadRequest, "an active goal needs a human owner")
		return false
	}
	return true
}

func goalStatusKnown(s string) bool {
	for _, v := range goalStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// applyGoalRequest folds the body onto params; absent fields keep params.
func applyGoalRequest(w http.ResponseWriter, req goalWriteRequest, raw map[string]json.RawMessage, params *db.UpdateGoalParams) bool {
	if req.Title != nil {
		params.Title = *req.Title
	}
	if req.Description != nil {
		params.Description = strings.TrimSpace(*req.Description)
	}
	if req.SuccessMeasure != nil {
		params.SuccessMeasure = strings.TrimSpace(*req.SuccessMeasure)
	}
	if req.Status != nil {
		params.Status = *req.Status
	}
	if _, touched := raw["parent_goal_id"]; touched {
		params.ParentGoalID = pgtype.UUID{}
		if req.ParentGoalID != nil && *req.ParentGoalID != "" {
			id, ok := parseUUIDOrBadRequest(w, *req.ParentGoalID, "parent_goal_id")
			if !ok {
				return false
			}
			params.ParentGoalID = id
		}
	}
	if _, touched := raw["owner_id"]; touched {
		params.OwnerID = pgtype.UUID{}
		if req.OwnerID != nil && *req.OwnerID != "" {
			id, ok := parseUUIDOrBadRequest(w, *req.OwnerID, "owner_id")
			if !ok {
				return false
			}
			params.OwnerID = id
		}
	}
	if _, touched := raw["due_date"]; touched {
		params.DueDate = pgtype.Date{}
		if req.DueDate != nil && *req.DueDate != "" {
			d, err := util.ParseCalendarDate(*req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
				return false
			}
			params.DueDate = d
		}
	}
	return true
}

func decodeGoalRequest(w http.ResponseWriter, r *http.Request) (goalWriteRequest, map[string]json.RawMessage, bool) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return goalWriteRequest{}, nil, false
	}
	body, _ := json.Marshal(raw)
	var req goalWriteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return goalWriteRequest{}, nil, false
	}
	return req, raw, true
}

// POST /api/goals
func (h *Handler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return
	}
	req, raw, ok := decodeGoalRequest(w, r)
	if !ok {
		return
	}
	tree, _, err := h.loadGoalTree(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load goals")
		return
	}
	params := db.UpdateGoalParams{WorkspaceID: wsUUID, Status: goalStatusDraft}
	if !applyGoalRequest(w, req, raw, &params) || !h.validateGoalWrite(w, r, wsUUID, tree, "", &params) {
		return
	}
	goal, err := h.Queries.CreateGoal(r.Context(), db.CreateGoalParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, ParentGoalID: params.ParentGoalID, Title: params.Title, Description: params.Description,
		SuccessMeasure: params.SuccessMeasure, DueDate: params.DueDate, OwnerID: params.OwnerID, Status: params.Status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create goal")
		return
	}
	h.indexWhy(r.Context(), wsUUID, whySourceGoal, goal.ID, pgtype.UUID{}, goalWhyContent(goal))
	h.audit(r.Context(), wsUUID, "member", userID, "goal.created", "goal", goal.ID, map[string]any{"title": goal.Title, "status": goal.Status}, nil)
	writeJSON(w, http.StatusCreated, h.singleGoalResponse(r.Context(), wsUUID, goal.ID))
}

func (h *Handler) singleGoalResponse(ctx context.Context, wsUUID, goalID pgtype.UUID) GoalResponse {
	tree, goals, err := h.loadGoalTree(ctx, wsUUID)
	if err != nil {
		return GoalResponse{ID: uuidToString(goalID)}
	}
	for _, g := range h.goalResponses(ctx, wsUUID, tree, goals) {
		if g.ID == uuidToString(goalID) {
			return g
		}
	}
	return GoalResponse{ID: uuidToString(goalID)}
}

// PUT /api/goals/{id}
func (h *Handler) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	tree, _, err := h.loadGoalTree(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load goals")
		return
	}
	prev, found := tree.byID[uuidToString(goalID)]
	if !found {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}
	req, raw, ok := decodeGoalRequest(w, r)
	if !ok {
		return
	}
	params := db.UpdateGoalParams{
		ID: prev.ID, WorkspaceID: wsUUID, ParentGoalID: prev.ParentGoalID, Title: prev.Title, Description: prev.Description,
		SuccessMeasure: prev.SuccessMeasure, DueDate: prev.DueDate, OwnerID: prev.OwnerID, Status: prev.Status,
	}
	if !applyGoalRequest(w, req, raw, &params) || !h.validateGoalWrite(w, r, wsUUID, tree, uuidToString(prev.ID), &params) {
		return
	}
	goal, err := h.Queries.UpdateGoal(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update goal")
		return
	}
	h.indexWhy(r.Context(), wsUUID, whySourceGoal, goal.ID, pgtype.UUID{}, goalWhyContent(goal))
	h.audit(r.Context(), wsUUID, "member", userID, "goal.updated", "goal", goal.ID, map[string]any{"title": goal.Title, "status": goal.Status, "owner_id": uuidToPtr(goal.OwnerID)}, nil)
	writeJSON(w, http.StatusOK, h.singleGoalResponse(r.Context(), wsUUID, goal.ID))
}

// DELETE /api/goals/{id} (owner/admin): refused while sub-goals remain;
// issues and projects are detached in the same transaction.
func (h *Handler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	requester, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	goal, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{ID: goalID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "goal not found")
		return
	}
	if n, err := h.Queries.CountChildGoals(r.Context(), db.CountChildGoalsParams{ParentGoalID: goal.ID, WorkspaceID: wsUUID}); err != nil || n > 0 {
		writeError(w, http.StatusBadRequest, "move or delete the sub-goals first")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.ClearIssueGoal(r.Context(), db.ClearIssueGoalParams{GoalID: goal.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach issues")
		return
	}
	if err := qtx.DeleteProjectGoalsByGoal(r.Context(), db.DeleteProjectGoalsByGoalParams{GoalID: goal.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach projects")
		return
	}
	if err := qtx.DeleteGoal(r.Context(), db.DeleteGoalParams{ID: goal.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete goal")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit goal delete")
		return
	}
	h.unindexWhy(r.Context(), whySourceGoal, goal.ID)
	h.audit(r.Context(), wsUUID, "member", uuidToString(requester.UserID), "goal.deleted", "goal", goal.ID, map[string]any{"title": goal.Title}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/projects/{id}/goals {goal_ids}: replaces the project's goal links.
func (h *Handler) SetProjectGoals(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsUUID), "workspace not found"); !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var req struct {
		GoalIDs []string `json:"goal_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	goalIDs, ok := parseUUIDSliceOrBadRequest(w, req.GoalIDs, "goal_ids")
	if !ok {
		return
	}
	tree, _, err := h.loadGoalTree(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load goals")
		return
	}
	for _, id := range goalIDs {
		if _, found := tree.byID[uuidToString(id)]; !found {
			writeError(w, http.StatusBadRequest, "goal not found in this workspace")
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteProjectGoalsByProject(r.Context(), db.DeleteProjectGoalsByProjectParams{ProjectID: project.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace project goals")
		return
	}
	for _, id := range goalIDs {
		if err := qtx.AddProjectGoal(r.Context(), db.AddProjectGoalParams{WorkspaceID: wsUUID, ProjectID: project.ID, GoalID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link goal")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project goals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal_ids": h.projectGoalIDs(r.Context(), wsUUID, project.ID)})
}

func (h *Handler) projectGoalIDs(ctx context.Context, wsUUID, projectID pgtype.UUID) []string {
	out := []string{}
	rows, err := h.Queries.ListProjectGoalsByProject(ctx, db.ListProjectGoalsByProjectParams{ProjectID: projectID, WorkspaceID: wsUUID})
	if err != nil {
		return out
	}
	for _, l := range rows {
		out = append(out, uuidToString(l.GoalID))
	}
	return out
}

// projectGoalIDsByProject batches the links for a project list.
func (h *Handler) projectGoalIDsByProject(ctx context.Context, wsUUID pgtype.UUID) map[string][]string {
	out := map[string][]string{}
	rows, err := h.Queries.ListProjectGoals(ctx, wsUUID)
	if err != nil {
		return out
	}
	for _, l := range rows {
		out[uuidToString(l.ProjectID)] = append(out[uuidToString(l.ProjectID)], uuidToString(l.GoalID))
	}
	return out
}

// validateIssueGoal checks a goal_id an issue write names.
func (h *Handler) validateIssueGoal(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	if strings.TrimSpace(raw) == "" {
		return pgtype.UUID{}, true
	}
	id, ok := parseUUIDOrBadRequest(w, raw, "goal_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusBadRequest, "goal not found in this workspace")
		return pgtype.UUID{}, false
	}
	return id, true
}

func goalWhyContent(g db.Goal) string {
	return strings.TrimSpace(strings.Join([]string{g.Title, g.Description, g.SuccessMeasure}, "\n"))
}

// --- Mission chain in the run brief -------------------------------------

// MissionChainNode is one goal of the chain a claim carries, root (the
// mission) first. The daemon renders it under "## Mission and goals".
type MissionChainNode struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	SuccessMeasure string `json:"success_measure,omitempty"`
	Status         string `json:"status"`
	DueDate        string `json:"due_date,omitempty"`
	Depth          int    `json:"depth"`
}

// effectiveIssueGoal is the goal an issue serves: its own, else the first
// goal linked to its project.
func (h *Handler) effectiveIssueGoal(ctx context.Context, issue db.Issue) (pgtype.UUID, bool) {
	if issue.GoalID.Valid {
		return issue.GoalID, true
	}
	if !issue.ProjectID.Valid {
		return pgtype.UUID{}, false
	}
	links, err := h.Queries.ListProjectGoalsByProject(ctx, db.ListProjectGoalsByProjectParams{ProjectID: issue.ProjectID, WorkspaceID: issue.WorkspaceID})
	if err != nil || len(links) == 0 {
		return pgtype.UUID{}, false
	}
	return links[0].GoalID, true
}

// resolveClaimMissionChain builds the chain for a claimed issue. Empty when
// the issue serves no goal; the brief then stays byte-identical to before.
func (h *Handler) resolveClaimMissionChain(ctx context.Context, issue db.Issue) ([]MissionChainNode, error) {
	goalID, ok := h.effectiveIssueGoal(ctx, issue)
	if !ok {
		return nil, nil
	}
	tree, _, err := h.loadGoalTree(ctx, issue.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("load goals: %w", err)
	}
	chain := tree.ancestry(uuidToString(goalID))
	out := make([]MissionChainNode, 0, len(chain))
	for i, g := range chain {
		n := MissionChainNode{ID: uuidToString(g.ID), Title: g.Title, Description: truncateUTF8(g.Description, missionChainMaxNodeBytes), SuccessMeasure: g.SuccessMeasure, Status: g.Status, Depth: i + 1}
		if d := dateToPtr(g.DueDate); d != nil {
			n.DueDate = *d
		}
		out = append(out, n)
	}
	return out, nil
}

// --- Agent proposal, settled by a human decision (K63) -------------------

// POST /api/issues/{id}/goal-proposal (agent only) {goal_id, reason}: files a
// decision "attach this issue to goal X?". The agent never sets the goal.
func (h *Handler) ProposeIssueGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, wsRaw)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only an agent proposes a goal; members set it on the issue")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, actorID, "agent_id")
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	var req struct {
		GoalID string `json:"goal_id"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	goalID, ok := parseUUIDOrBadRequest(w, req.GoalID, "goal_id")
	if !ok {
		return
	}
	goal, err := h.Queries.GetGoalInWorkspace(r.Context(), db.GetGoalInWorkspaceParams{ID: goalID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "goal not found in this workspace")
		return
	}
	if issue.GoalID == goal.ID {
		writeError(w, http.StatusConflict, "the issue already serves this goal")
		return
	}
	reason := truncateUTF8(strings.TrimSpace(req.Reason), 500)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	attachOption := goalProposalOptionPrefix + uuidToString(goal.ID)
	if pending, err := h.Queries.ListIssueDecisions(r.Context(), db.ListIssueDecisionsParams{IssueID: issue.ID, WorkspaceID: wsUUID}); err == nil {
		for _, d := range pending {
			if d.Response == nil && strings.Contains(string(d.Options), `"`+attachOption+`"`) {
				writeError(w, http.StatusConflict, "this attachment is already awaiting a decision")
				return
			}
		}
	}
	question := fmt.Sprintf("Goal · attach this issue to \"%s\"?\n\n%s\n\nSuccess measure: %s", goal.Title, reason, nonEmpty(goal.SuccessMeasure, "not set"))
	options, _ := json.Marshal([]DecisionOption{
		{ID: attachOption, Label: "Attach to the goal", Impact: "the run brief carries the goal chain and the goal's progress counts this issue"},
		{ID: goalProposalKeepOption, Label: "Keep as is", Impact: "nothing changes"},
	})
	var taskID pgtype.UUID
	if raw := r.Header.Get("X-Task-ID"); raw != "" {
		if id, err := util.ParseUUID(raw); err == nil {
			taskID = id
		}
	}
	decision, err := h.Queries.CreateIssueDecision(r.Context(), db.CreateIssueDecisionParams{
		WorkspaceID: wsUUID, IssueID: issue.ID, TaskID: taskID, AskedByType: "agent", AskedByID: agentID,
		Question: question, Options: options, Urgency: "normal", SlaDeadlineAt: h.decisionDeadline(r.Context(), wsUUID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to file the decision")
		return
	}
	h.audit(r.Context(), wsUUID, "agent", actorID, AuditDecisionAsked, "issue_decision", decision.ID, map[string]any{"issue_id": uuidToString(issue.ID), "goal_id": uuidToString(goal.ID), "question": question}, nil)
	h.notifyDecisionRequested(r.Context(), issue, decision, "agent", actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"decision": issueDecisionToResponse(decision)})
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// applyGoalForDecision settles a goal-attachment decision: the chosen goal
// lands on the issue, "keep" leaves it. False when the decision is not one.
func (h *Handler) applyGoalForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	var options []DecisionOption
	if err := json.Unmarshal(decision.Options, &options); err != nil {
		return false
	}
	proposal := false
	for _, o := range options {
		if strings.HasPrefix(o.ID, goalProposalOptionPrefix) {
			proposal = true
		}
	}
	if !proposal {
		return false
	}
	if !strings.HasPrefix(optionID, goalProposalOptionPrefix) {
		return true
	}
	goalID, err := util.ParseUUID(strings.TrimPrefix(optionID, goalProposalOptionPrefix))
	if err != nil {
		return true
	}
	if _, err := h.Queries.GetGoalInWorkspace(ctx, db.GetGoalInWorkspaceParams{ID: goalID, WorkspaceID: decision.WorkspaceID}); err != nil {
		slog.Warn("goal proposal: goal vanished before the answer", "goal_id", uuidToString(goalID))
		return true
	}
	if err := h.Queries.SetIssueGoal(ctx, db.SetIssueGoalParams{ID: decision.IssueID, WorkspaceID: decision.WorkspaceID, GoalID: goalID}); err != nil {
		slog.Warn("goal proposal: attach failed", "error", err, "issue_id", uuidToString(decision.IssueID))
		return true
	}
	h.audit(ctx, decision.WorkspaceID, actorType, actorID, "goal.issue_attached", "issue", decision.IssueID, map[string]any{"goal_id": uuidToString(goalID), "decision_id": uuidToString(decision.ID)}, &auditOpts{ApproverType: actorType, ApproverID: actorID})
	return true
}

// --- Briefing line (K30/K64) --------------------------------------------

type BriefingGoal struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	DueDate    *string `json:"due_date"`
	IssueCount int64   `json:"issue_count"`
	DoneCount  int64   `json:"done_count"`
}

// briefingGoals lists the active goals with their progress, nearest due date
// first, five at most. Best effort: the briefing never fails on it.
func (h *Handler) briefingGoals(ctx context.Context, wsID pgtype.UUID) []BriefingGoal {
	tree, goals, err := h.loadGoalTree(ctx, wsID)
	if err != nil {
		slog.Warn("briefing: load goals failed", "error", err)
		return nil
	}
	var out []BriefingGoal
	for _, g := range h.goalResponses(ctx, wsID, tree, goals) {
		if g.Status != goalStatusActive {
			continue
		}
		out = append(out, BriefingGoal{ID: g.ID, Title: g.Title, DueDate: g.DueDate, IssueCount: g.IssueCount, DoneCount: g.DoneCount})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].DueDate, out[j].DueDate
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return *a < *b
		}
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
