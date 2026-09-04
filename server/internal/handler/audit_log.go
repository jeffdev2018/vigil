package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Audit log (K08): the actions that matter, written explicitly where they
// happen, never updated, deleted only with the workspace. Exported as JSON
// or CSV in the same order and with the same filters as the paginated view.

const (
	auditPageDefault = 50
	auditPageMax     = 200
	auditExportBatch = 1000
)

// Actions. Keep them stable: exports and filters key on them.
const (
	AuditDecisionAsked        = "decision.asked"
	AuditDecisionAnswered     = "decision.answered"
	AuditDecisionEscalated    = "decision.escalated"
	AuditInterviewAsked       = "interview.asked"
	AuditPlanMaterialized     = "plan.materialized"
	AuditIssueStatus          = "issue.status_changed"
	AuditCriterionProved      = "criterion.proved"
	AuditAgentRolledBack      = "agent.rolled_back"
	AuditOwnershipCreated     = "ownership_rule.created"
	AuditOwnershipDeleted     = "ownership_rule.deleted"
	AuditBriefingSent         = "briefing.sent"
	AuditWorkspaceSettings    = "workspace.settings_updated"
	AuditDecisionRecorded     = "decision_record.created"
	AuditBusinessRuleViolated = "business_rule.violated"
)

type auditOpts struct {
	Model        string
	CostUsdTicks *int64
	ApproverType string
	ApproverID   string
}

// audit appends one entry; a failure is logged, never surfaced, because the
// action it records has already happened.
func (h *Handler) audit(ctx context.Context, wsID pgtype.UUID, actorType, actorID, action, entityType string, entityID pgtype.UUID, details any, opts *auditOpts) {
	if actorType == "" {
		actorType = "system"
	}
	raw, err := json.Marshal(details)
	if err != nil || details == nil {
		raw = []byte("{}")
	}
	params := db.CreateAuditLogEntryParams{
		WorkspaceID: wsID, ActorType: actorType, ActorID: parseUUIDOrZero(actorID), Action: action,
		EntityType: entityType, EntityID: entityID, Details: raw,
	}
	if opts != nil {
		params.Model = pgtype.Text{String: opts.Model, Valid: opts.Model != ""}
		if opts.CostUsdTicks != nil {
			params.CostUsdTicks = pgtype.Int8{Int64: *opts.CostUsdTicks, Valid: true}
		}
		params.ApproverType = pgtype.Text{String: opts.ApproverType, Valid: opts.ApproverType != ""}
		params.ApproverID = parseUUIDOrZero(opts.ApproverID)
	}
	if _, err := h.Queries.CreateAuditLogEntry(ctx, params); err != nil {
		slog.Warn("audit log write failed", "error", err, "action", action, "workspace_id", uuidToString(wsID))
	}
}

type AuditLogEntryResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	OccurredAt   string          `json:"occurred_at"`
	ActorType    string          `json:"actor_type"`
	ActorID      *string         `json:"actor_id"`
	Action       string          `json:"action"`
	EntityType   string          `json:"entity_type"`
	EntityID     *string         `json:"entity_id"`
	Model        *string         `json:"model"`
	CostUsdTicks *int64          `json:"cost_usd_ticks"`
	ApproverType *string         `json:"approver_type"`
	ApproverID   *string         `json:"approver_id"`
	Details      json.RawMessage `json:"details"`
}

func auditEntryToResponse(e db.AuditLogEntry) AuditLogEntryResponse {
	var cost *int64
	if e.CostUsdTicks.Valid {
		v := e.CostUsdTicks.Int64
		cost = &v
	}
	details := json.RawMessage(e.Details)
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	return AuditLogEntryResponse{
		ID: uuidToString(e.ID), WorkspaceID: uuidToString(e.WorkspaceID), OccurredAt: e.OccurredAt.Time.UTC().Format(time.RFC3339Nano),
		ActorType: e.ActorType, ActorID: uuidToPtr(e.ActorID), Action: e.Action, EntityType: e.EntityType, EntityID: uuidToPtr(e.EntityID),
		Model: textToPtr(e.Model), CostUsdTicks: cost, ApproverType: textToPtr(e.ApproverType), ApproverID: uuidToPtr(e.ApproverID), Details: details,
	}
}

type auditFilter struct {
	since, until pgtype.Timestamptz
	actorType    pgtype.Text
	action       pgtype.Text
	entityID     pgtype.UUID
}

func parseAuditFilter(r *http.Request) (auditFilter, error) {
	var f auditFilter
	q := r.URL.Query()
	parseTime := func(key string) (pgtype.Timestamptz, error) {
		v := strings.TrimSpace(q.Get(key))
		if v == "" {
			return pgtype.Timestamptz{}, nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return pgtype.Timestamptz{}, fmt.Errorf("%s must be RFC3339", key)
		}
		return pgtype.Timestamptz{Time: t, Valid: true}, nil
	}
	var err error
	if f.since, err = parseTime("since"); err != nil {
		return f, err
	}
	if f.until, err = parseTime("until"); err != nil {
		return f, err
	}
	if v := strings.TrimSpace(q.Get("actor_type")); v != "" {
		switch v {
		case "member", "agent", "system":
			f.actorType = pgtype.Text{String: v, Valid: true}
		default:
			return f, fmt.Errorf("actor_type must be member, agent or system")
		}
	}
	if v := strings.TrimSpace(q.Get("action")); v != "" {
		f.action = pgtype.Text{String: v, Valid: true}
	}
	if v := strings.TrimSpace(q.Get("entity_id")); v != "" {
		if err := f.entityID.Scan(v); err != nil {
			return f, fmt.Errorf("entity_id must be a UUID")
		}
	}
	return f, nil
}

func (h *Handler) auditPage(ctx context.Context, wsID pgtype.UUID, f auditFilter, cursorAt pgtype.Timestamptz, cursorID pgtype.UUID, size int32) ([]db.AuditLogEntry, error) {
	return h.Queries.ListAuditLogEntries(ctx, db.ListAuditLogEntriesParams{
		WorkspaceID: wsID, Since: f.since, Until: f.until, ActorType: f.actorType, Action: f.action, EntityID: f.entityID,
		CursorAt: cursorAt, CursorID: cursorID, PageSize: size,
	})
}

func auditCursor(e db.AuditLogEntry) string {
	return e.OccurredAt.Time.UTC().Format(time.RFC3339Nano) + "|" + uuidToString(e.ID)
}

func parseAuditCursor(s string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if s == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	at, id, ok := strings.Cut(s, "|")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("invalid cursor")
	}
	u, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, fmt.Errorf("invalid cursor")
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, u, nil
}

// ListAuditLog — GET /api/audit-log?since=&until=&actor_type=&action=&cursor=&limit=.
func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cursorAt, cursorID, err := parseAuditCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	size := auditPageDefault
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		size = min(n, auditPageMax)
	}
	rows, err := h.auditPage(r.Context(), wsUUID, f, cursorAt, cursorID, int32(size+1))
	if err != nil {
		slog.Warn("audit log list failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list the audit log")
		return
	}
	next := ""
	if len(rows) > size {
		rows = rows[:size]
		next = auditCursor(rows[len(rows)-1])
	}
	out := make([]AuditLogEntryResponse, 0, len(rows))
	for _, e := range rows {
		out = append(out, auditEntryToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "next_cursor": next})
}

var auditCSVHeader = []string{"id", "occurred_at", "actor_type", "actor_id", "action", "entity_type", "entity_id", "model", "cost_usd_ticks", "approver_type", "approver_id", "details"}

func auditCSVRow(e AuditLogEntryResponse) []string {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	cost := ""
	if e.CostUsdTicks != nil {
		cost = strconv.FormatInt(*e.CostUsdTicks, 10)
	}
	return []string{e.ID, e.OccurredAt, e.ActorType, str(e.ActorID), e.Action, e.EntityType, str(e.EntityID), str(e.Model), cost, str(e.ApproverType), str(e.ApproverID), string(e.Details)}
}

// ExportAuditLog — GET /api/audit-log/export?format=csv|json (owner/admin).
// Streams batch by batch so a large log never holds the request: the client
// starts receiving after the first thousand rows.
func (h *Handler) ExportAuditLog(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	format := r.URL.Query().Get("format")
	if format != "csv" && format != "json" {
		writeError(w, http.StatusBadRequest, "format must be csv or json")
		return
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-log-%s.%s\"", stamp, format))
	flusher, _ := w.(http.Flusher)
	var cw *csv.Writer
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		cw = csv.NewWriter(w)
		_ = cw.Write(auditCSVHeader)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("["))
	}
	w.WriteHeader(http.StatusOK)
	var cursorAt pgtype.Timestamptz
	var cursorID pgtype.UUID
	first := true
	for {
		rows, err := h.auditPage(r.Context(), wsUUID, f, cursorAt, cursorID, auditExportBatch)
		if err != nil {
			slog.Warn("audit log export failed", append(logger.RequestAttrs(r), "error", err)...)
			break
		}
		for _, e := range rows {
			resp := auditEntryToResponse(e)
			if cw != nil {
				_ = cw.Write(auditCSVRow(resp))
			} else {
				if !first {
					_, _ = w.Write([]byte(","))
				}
				b, _ := json.Marshal(resp)
				_, _ = w.Write(b)
				first = false
			}
		}
		if cw != nil {
			cw.Flush()
		}
		if flusher != nil {
			flusher.Flush()
		}
		if len(rows) < auditExportBatch {
			break
		}
		last := rows[len(rows)-1]
		cursorAt, cursorID = last.OccurredAt, last.ID
	}
	if cw == nil {
		_, _ = w.Write([]byte("]"))
	}
}
