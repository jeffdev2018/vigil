package handler

// Recurring issues (OS plan, table stakes): the rule lives on an issue and
// every occurrence carries it. Members set, read and clear it; agents read
// it. The service spawns occurrences from the scheduler tick.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	AuditIssueRecurrenceSet     = "issue.recurrence_set"
	AuditIssueRecurrenceCleared = "issue.recurrence_cleared"
	recurrenceOccurrencesShown  = 20
)

type IssueRecurrenceResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	CronExpression  string  `json:"cron_expression"`
	Timezone        string  `json:"timezone"`
	Mode            string  `json:"mode"`
	Enabled         bool    `json:"enabled"`
	NextRunAt       *string `json:"next_run_at"`
	LastOccurrence  *string `json:"last_occurrence_id"`
	OccurrenceCount int32   `json:"occurrence_count"`
	CreatedByType   string  `json:"created_by_type"`
	CreatedByID     *string `json:"created_by_id"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type IssueRecurrenceOccurrence struct {
	ID         string  `json:"id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	DueDate    *string `json:"due_date"`
}

func issueRecurrenceToResponse(r db.IssueRecurrence) IssueRecurrenceResponse {
	return IssueRecurrenceResponse{
		ID: uuidToString(r.ID), IssueID: uuidToString(r.IssueID), CronExpression: r.CronExpression, Timezone: r.Timezone, Mode: r.Mode, Enabled: r.Enabled,
		NextRunAt: timestampToPtr(r.NextRunAt), LastOccurrence: uuidToPtr(r.LastOccurrenceID), OccurrenceCount: r.OccurrenceCount,
		CreatedByType: r.CreatedByType, CreatedByID: uuidToPtr(r.CreatedByID), CreatedAt: timestampToString(r.CreatedAt), UpdatedAt: timestampToString(r.UpdatedAt),
	}
}

func (h *Handler) issueRecurrencePayload(issue db.Issue, rec db.IssueRecurrence, r *http.Request) map[string]any {
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	occurrences := []IssueRecurrenceOccurrence{}
	if rows, err := h.Queries.ListIssueRecurrenceOccurrences(r.Context(), db.ListIssueRecurrenceOccurrencesParams{RecurrenceID: rec.ID, WorkspaceID: issue.WorkspaceID, Limit: recurrenceOccurrencesShown}); err == nil {
		for _, row := range rows {
			occ := IssueRecurrenceOccurrence{ID: uuidToString(row.ID), Identifier: prefix + "-" + issueNumberString(row.Number), Title: row.Title, Status: row.Status, CreatedAt: timestampToString(row.CreatedAt)}
			if row.DueDate.Valid {
				d := row.DueDate.Time.Format("2006-01-02")
				occ.DueDate = &d
			}
			occurrences = append(occurrences, occ)
		}
	}
	source := map[string]any{"id": uuidToString(rec.IssueID), "identifier": ""}
	if src, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: rec.IssueID, WorkspaceID: issue.WorkspaceID}); err == nil {
		source["identifier"] = prefix + "-" + issueNumberString(src.Number)
		source["title"] = src.Title
	}
	var next []string
	if rec.Mode == service.RecurrenceModeSchedule && rec.Enabled {
		if runs, err := service.NextOccurrencesAfterUTC(rec.CronExpression, rec.Timezone, time.Now().UTC(), 3); err == nil {
			for _, at := range runs {
				next = append(next, at.Format(time.RFC3339))
			}
		}
	}
	if next == nil {
		next = []string{}
	}
	return map[string]any{"recurrence": issueRecurrenceToResponse(rec), "source": source, "occurrences": occurrences, "next_runs": next}
}

// GET /api/issues/{id}/recurrence — the rule this issue belongs to, its
// source, the latest occurrences and the next runs; 404 when none.
func (h *Handler) GetIssueRecurrence(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rec, err := h.Queries.GetIssueRecurrenceByIssue(r.Context(), db.GetIssueRecurrenceByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "this issue does not recur")
		return
	}
	writeJSON(w, http.StatusOK, h.issueRecurrencePayload(issue, rec, r))
}

// PUT /api/issues/{id}/recurrence {cron_expression, timezone, mode, enabled}
// Creates the rule on the issue, or updates the rule of the series the
// issue belongs to. Members only: a rule is a standing order.
func (h *Handler) SetIssueRecurrence(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// K60: the actorType check below is a workspace-role check, not a
	// per-project one — a project-role override still applies.
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	var req struct {
		CronExpression string `json:"cron_expression"`
		Timezone       string `json:"timezone"`
		Mode           string `json:"mode"`
		Enabled        *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = service.RecurrenceModeSchedule
	}
	if err := service.ValidateRecurrenceMode(mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "UTC"
	}
	if err := service.ValidateTimezone(tz); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cron := strings.TrimSpace(req.CronExpression)
	if mode == service.RecurrenceModeSchedule && cron == "" {
		writeError(w, http.StatusBadRequest, "cron_expression is required for a scheduled recurrence")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var next pgtype.Timestamptz
	if enabled {
		n, err := service.NextRecurrenceRun(mode, cron, tz, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cron_expression: "+err.Error())
			return
		}
		next = n
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "only a member sets a recurrence")
		return
	}
	existing, err := h.Queries.GetIssueRecurrenceByIssue(r.Context(), db.GetIssueRecurrenceByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	var rec db.IssueRecurrence
	change := "updated"
	if err == nil {
		rec, err = h.Queries.UpdateIssueRecurrence(r.Context(), db.UpdateIssueRecurrenceParams{ID: existing.ID, WorkspaceID: issue.WorkspaceID, CronExpression: cron, Timezone: tz, Mode: mode, Enabled: enabled, NextRunAt: next})
	} else {
		change = "created"
		rec, err = h.Queries.CreateIssueRecurrence(r.Context(), db.CreateIssueRecurrenceParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, CronExpression: cron, Timezone: tz, Mode: mode, Enabled: enabled, NextRunAt: next,
			CreatedByType: "member", CreatedByID: parseUUID(actorID),
		})
		if err == nil {
			_ = h.Queries.SetIssueRecurrenceLink(r.Context(), db.SetIssueRecurrenceLinkParams{ID: issue.ID, RecurrenceID: rec.ID})
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the recurrence")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditIssueRecurrenceSet, "issue_recurrence", rec.ID, map[string]any{"issue_id": uuidToString(issue.ID), "cron": cron, "timezone": tz, "mode": mode, "enabled": enabled}, nil)
	h.publish(protocol.EventIssueRecurrenceChanged, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "recurrence_id": uuidToString(rec.ID), "change": change})
	writeJSON(w, http.StatusOK, h.issueRecurrencePayload(issue, rec, r))
}

// DELETE /api/issues/{id}/recurrence — the series stops; past occurrences
// stay as ordinary issues.
func (h *Handler) DeleteIssueRecurrence(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// K60: same rationale as SetIssueRecurrence above.
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "only a member clears a recurrence")
		return
	}
	rec, err := h.Queries.GetIssueRecurrenceByIssue(r.Context(), db.GetIssueRecurrenceByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "this issue does not recur")
		return
	}
	if _, err := h.Queries.DeleteIssueRecurrence(r.Context(), db.DeleteIssueRecurrenceParams{ID: rec.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the recurrence")
		return
	}
	_ = h.Queries.ClearIssueRecurrenceLinks(r.Context(), rec.ID)
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditIssueRecurrenceCleared, "issue_recurrence", rec.ID, map[string]any{"issue_id": uuidToString(issue.ID)}, nil)
	h.publish(protocol.EventIssueRecurrenceChanged, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "recurrence_id": uuidToString(rec.ID), "change": "cleared"})
	w.WriteHeader(http.StatusNoContent)
}

// TickIssueRecurrences is the scheduler entry point.
func (h *Handler) TickIssueRecurrences(ctx context.Context) int {
	if h.Recurrence == nil {
		return 0
	}
	return h.Recurrence.Tick(ctx)
}

func issueNumberString(n int32) string { return strconv.Itoa(int(n)) }
