package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/blastradius"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Approval gates (K05): a run that is about to push, call a sensitive MCP
// tool or spend asks the server first. The ask is a Decision Card (K01) on
// the run's issue; the run blocks on a long-poll until a human approves,
// denies, or the gate expires. The decision never restarts the run: the
// run is still alive, waiting.

const (
	GateGitPush     = "git_push"
	GateMCPToolCall = "mcp_tool_call"
	GateSpend       = "spend"

	gateApproveOptionID = "approve"
	gateDenyOptionID    = "deny"
	gatePollMaxWait     = 30 * time.Second
	spendTokenTTL       = 5 * time.Minute

	AuditGateAsked    = "gate.asked"
	AuditGateResolved = "gate.resolved"
)

type ApprovalGateResponse struct {
	ID         string          `json:"id"`
	TaskID     string          `json:"task_id"`
	IssueID    *string         `json:"issue_id"`
	GateType   string          `json:"gate_type"`
	DecisionID *string         `json:"decision_id"`
	Summary    string          `json:"summary"`
	Details    json.RawMessage `json:"details"`
	Status     string          `json:"status"` // pending | approved | denied | expired
	CreatedAt  string          `json:"created_at"`
	ExpiresAt  *string         `json:"expires_at"`
	ResolvedAt *string         `json:"resolved_at"`
}

func gateStatus(g db.ApprovalGateEvent) string {
	if g.ResolvedAction.Valid {
		if g.ResolvedAction.String == "approved" {
			return "approved"
		}
		var d struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(g.Details, &d) == nil && d.Reason == "timeout" {
			return "expired"
		}
		return "denied"
	}
	return "pending"
}

func gateToResponse(g db.ApprovalGateEvent) ApprovalGateResponse {
	return ApprovalGateResponse{
		ID: uuidToString(g.ID), TaskID: uuidToString(g.TaskID), IssueID: uuidToPtr(g.IssueID), GateType: g.GateType,
		DecisionID: uuidToPtr(g.DecisionRequestID), Summary: g.Summary, Details: json.RawMessage(g.Details), Status: gateStatus(g),
		CreatedAt: timestampToString(g.CreatedAt), ExpiresAt: timestampToPtr(g.ExpiresAt), ResolvedAt: timestampToPtr(g.ResolvedAt),
	}
}

// gateTask loads the task named by the path and checks the caller: the
// task's own token, or a member of its workspace.
func (h *Handler) gateTask(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, bool) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, false
	}
	if isMachineCredentialActor(r) {
		if strings.TrimSpace(r.Header.Get("X-Task-ID")) != uuidToString(task.ID) {
			writeError(w, http.StatusForbidden, "this token belongs to another run")
			return db.AgentTaskQueue{}, false
		}
		return task, true
	}
	if task.IssueID.Valid {
		if _, ok := h.loadIssueForUser(w, r, uuidToString(task.IssueID)); !ok {
			return db.AgentTaskQueue{}, false
		}
		return task, true
	}
	writeError(w, http.StatusForbidden, "no access to this run")
	return db.AgentTaskQueue{}, false
}

// openGate files the Decision Card and the gate row.
func (h *Handler) openGate(ctx context.Context, task db.AgentTaskQueue, gateType, summary string, details map[string]any) (db.ApprovalGateEvent, error) {
	if !task.IssueID.Valid {
		return db.ApprovalGateEvent{}, errors.New("the run has no issue to ask on")
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return db.ApprovalGateEvent{}, fmt.Errorf("load issue: %w", err)
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return db.ApprovalGateEvent{}, fmt.Errorf("load workspace: %w", err)
	}
	cfg := service.ApprovalGatesSettings(ws.Settings)
	label := map[string]string{GateGitPush: "git push", GateMCPToolCall: "tool call", GateSpend: "spend"}[gateType]
	options, _ := json.Marshal([]DecisionOption{
		{ID: gateApproveOptionID, Label: "Approve", Impact: "the run continues with the " + label},
		{ID: gateDenyOptionID, Label: "Deny", Impact: "the run gets an explicit refusal"},
	})
	decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, TaskID: task.ID, AskedByType: "agent", AskedByID: task.AgentID,
		Question: "Blocked action · " + summary, Options: options, RecommendedOptionID: pgtype.Text{},
		Urgency: "high", SlaDeadlineAt: h.decisionDeadline(ctx, issue.WorkspaceID),
	})
	if err != nil {
		return db.ApprovalGateEvent{}, fmt.Errorf("file decision: %w", err)
	}
	if details == nil {
		details = map[string]any{}
	}
	raw, _ := json.Marshal(details)
	gate, err := h.Queries.CreateApprovalGateEvent(ctx, db.CreateApprovalGateEventParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, TaskID: task.ID, IssueID: issue.ID, GateType: gateType, DecisionRequestID: decision.ID,
		Summary: summary, Details: raw, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Duration(cfg.TimeoutMinutes) * time.Minute), Valid: true},
	})
	if err != nil {
		return db.ApprovalGateEvent{}, fmt.Errorf("file gate: %w", err)
	}
	h.notifyDecisionRequested(ctx, issue, decision, "agent", uuidToString(task.AgentID))
	h.audit(ctx, issue.WorkspaceID, "agent", uuidToString(task.AgentID), AuditGateAsked, "task", task.ID, map[string]any{"gate_id": uuidToString(gate.ID), "gate_type": gateType, "summary": summary}, nil)
	return gate, nil
}

// gatePaths reads the paths an action touches from its details.
func gatePaths(details map[string]any) []string {
	var out []string
	if list, ok := details["paths"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// openGateWithBlastRadius (K07) lets the project's rules decide before any
// card: read_only refuses at once, autonomous approves at once, dual
// approval asks two different humans, no rule asks one.
func (h *Handler) openGateWithBlastRadius(ctx context.Context, task db.AgentTaskQueue, gateType, summary string, details map[string]any) (db.ApprovalGateEvent, error) {
	if details == nil {
		details = map[string]any{}
	}
	paths := gatePaths(details)
	if !task.IssueID.Valid || len(paths) == 0 {
		return h.openGate(ctx, task, gateType, summary, details)
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return db.ApprovalGateEvent{}, fmt.Errorf("load issue: %w", err)
	}
	rules := h.projectBlastRules(ctx, issue.WorkspaceID, issue.ProjectID)
	level, ok := blastradius.Worst(rules, paths)
	if !ok {
		return h.openGate(ctx, task, gateType, summary, details)
	}
	details["blast_radius"] = level
	settle := func(action, reason string) (db.ApprovalGateEvent, error) {
		details["reason"] = reason
		raw, _ := json.Marshal(details)
		gate, err := h.Queries.CreateApprovalGateEvent(ctx, db.CreateApprovalGateEventParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, TaskID: task.ID, IssueID: issue.ID, GateType: gateType,
			Summary: summary, Details: raw, ResolvedAction: pgtype.Text{String: action, Valid: true}, ResolvedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			return db.ApprovalGateEvent{}, fmt.Errorf("file gate: %w", err)
		}
		h.audit(ctx, issue.WorkspaceID, "system", "", AuditGateResolved, "task", task.ID, map[string]any{"gate_id": uuidToString(gate.ID), "gate_type": gateType, "action": action, "reason": reason, "paths": paths}, nil)
		return gate, nil
	}
	switch level {
	case blastradius.LevelReadOnly:
		return settle("denied", "read_only")
	case blastradius.LevelAutonomous:
		return settle("approved", "autonomous")
	case blastradius.LevelDualApproval:
		details["required_approvals"] = 2
		details["approvers"] = []string{}
	}
	return h.openGate(ctx, task, gateType, summary, details)
}

// gateDualApproval (K07) counts a second, different approver before the
// gate opens; a first approval files another card for someone else.
func (h *Handler) gateDualApproval(ctx context.Context, gate db.ApprovalGateEvent, decision db.IssueDecision, actorID string) (settled bool) {
	var d struct {
		Required  int      `json:"required_approvals"`
		Approvers []string `json:"approvers"`
	}
	if json.Unmarshal(gate.Details, &d) != nil || d.Required < 2 {
		return true
	}
	seen := false
	for _, a := range d.Approvers {
		seen = seen || a == actorID
	}
	if !seen {
		d.Approvers = append(d.Approvers, actorID)
	}
	if len(d.Approvers) >= d.Required {
		return true
	}
	// Ask someone else: a new card on the same issue, tracked on the gate.
	options, _ := json.Marshal([]DecisionOption{
		{ID: gateApproveOptionID, Label: "Approve (second approver)", Impact: "a second, different approver is required for this path"},
		{ID: gateDenyOptionID, Label: "Deny", Impact: "the run gets an explicit refusal"},
	})
	next, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID: gate.WorkspaceID, IssueID: gate.IssueID, TaskID: gate.TaskID, AskedByType: decision.AskedByType, AskedByID: decision.AskedByID,
		Question: strings.Replace(decision.Question, "Blocked action ·", "Second approval ·", 1), Options: options, Urgency: "high", SlaDeadlineAt: h.decisionDeadline(ctx, gate.WorkspaceID),
	})
	if err != nil {
		slog.Warn("approval gate: second card failed", "error", err, "gate_id", uuidToString(gate.ID))
		return false
	}
	extra, _ := json.Marshal(map[string]any{"approvers": d.Approvers, "pending_decision_id": uuidToString(next.ID)})
	if err := h.Queries.AttachSpendToken(ctx, db.AttachSpendTokenParams{ID: gate.ID, Extra: extra}); err != nil {
		slog.Warn("approval gate: record approver failed", "error", err, "gate_id", uuidToString(gate.ID))
	}
	if issue, err := h.Queries.GetIssue(ctx, gate.IssueID); err == nil {
		h.notifyDecisionRequested(ctx, issue, next, decision.AskedByType, uuidToString(decision.AskedByID))
	}
	return false
}

// resolveGateForDecision is called when a Decision Card is answered: a gate
// behind it settles and the waiting run reads the outcome. Returns true when
// the decision was a gate, so the caller does not enqueue a resume run.
func (h *Handler) resolveGateForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	gate, err := h.Queries.GetApprovalGateByDecision(ctx, decision.ID)
	if err != nil {
		return false
	}
	action := "denied"
	if optionID == gateApproveOptionID {
		action = "approved"
		if !h.gateDualApproval(ctx, gate, decision, actorID) {
			h.audit(ctx, gate.WorkspaceID, actorType, actorID, AuditGateResolved, "task", gate.TaskID, map[string]any{"gate_id": uuidToString(gate.ID), "gate_type": gate.GateType, "action": "first_approval"}, &auditOpts{ApproverType: actorType, ApproverID: actorID})
			return true
		}
	}
	extra, _ := json.Marshal(map[string]any{"decided_by_type": actorType, "decided_by_id": actorID})
	if _, err := h.Queries.ResolveApprovalGateEvent(ctx, db.ResolveApprovalGateEventParams{ID: gate.ID, ResolvedAction: pgtype.Text{String: action, Valid: true}, Extra: extra}); err != nil {
		slog.Warn("approval gate: resolve failed", "error", err, "gate_id", uuidToString(gate.ID))
	}
	h.audit(ctx, gate.WorkspaceID, actorType, actorID, AuditGateResolved, "task", gate.TaskID, map[string]any{"gate_id": uuidToString(gate.ID), "gate_type": gate.GateType, "action": action}, &auditOpts{ApproverType: actorType, ApproverID: actorID})
	return true
}

// expireGate settles a pending gate whose deadline passed.
func (h *Handler) expireGate(ctx context.Context, gate db.ApprovalGateEvent) db.ApprovalGateEvent {
	if gate.ResolvedAction.Valid || !gate.ExpiresAt.Valid || time.Now().Before(gate.ExpiresAt.Time) {
		return gate
	}
	extra, _ := json.Marshal(map[string]any{"reason": "timeout"})
	updated, err := h.Queries.ResolveApprovalGateEvent(ctx, db.ResolveApprovalGateEventParams{ID: gate.ID, ResolvedAction: pgtype.Text{String: "denied", Valid: true}, Extra: extra})
	if err != nil {
		return gate
	}
	h.audit(ctx, gate.WorkspaceID, "system", "", AuditGateResolved, "task", gate.TaskID, map[string]any{"gate_id": uuidToString(gate.ID), "gate_type": gate.GateType, "action": "expired"}, nil)
	return updated
}

// POST /api/tasks/{taskId}/gates {gate_type, summary, details}
func (h *Handler) CreateApprovalGate(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	var req struct {
		GateType string         `json:"gate_type"`
		Summary  string         `json:"summary"`
		Details  map[string]any `json:"details"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.GateType != GateGitPush && req.GateType != GateMCPToolCall && req.GateType != GateSpend {
		writeError(w, http.StatusBadRequest, "gate_type must be git_push, mcp_tool_call or spend")
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if req.Summary == "" || len(req.Summary) > 500 {
		writeError(w, http.StatusBadRequest, "summary is required (at most 500 characters)")
		return
	}
	gate, err := h.openGateWithBlastRadius(r.Context(), task, req.GateType, req.Summary, req.Details)
	if err != nil {
		slog.Warn("approval gate: open failed", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusUnprocessableEntity, "gate_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, gateToResponse(gate))
}

// GET /api/tasks/{taskId}/gates/{gateId}?wait=25 — long-poll until settled.
func (h *Handler) GetApprovalGate(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	gateID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "gateId"), "gate id")
	if !ok {
		return
	}
	wait := 0
	if v := r.URL.Query().Get("wait"); v != "" {
		wait, _ = strconv.Atoi(v)
	}
	deadline := time.Now().Add(time.Duration(min(max(wait, 0), int(gatePollMaxWait/time.Second))) * time.Second)
	for {
		gate, err := h.Queries.GetApprovalGateEvent(r.Context(), db.GetApprovalGateEventParams{ID: gateID, TaskID: task.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "gate not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load the gate")
			return
		}
		gate = h.expireGate(r.Context(), gate)
		if gate.ResolvedAction.Valid || time.Now().After(deadline) {
			writeJSON(w, http.StatusOK, gateToResponse(gate))
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// GET /api/tasks/{taskId}/gates — history for the cockpit and tests.
func (h *Handler) ListApprovalGates(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListApprovalGateEvents(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list gates")
		return
	}
	out := make([]ApprovalGateResponse, 0, len(rows))
	for _, g := range rows {
		out = append(out, gateToResponse(h.expireGate(r.Context(), g)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"gates": out})
}

// ---- spend tokens ----

func newSpendToken() (token, hash string, err error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = "mst_" + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func hashSpendToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// POST /api/tasks/{taskId}/spend-token {amount_usd_ticks, purpose, gate_id?}
// Below the workspace threshold the token is issued at once; above it, a
// spend gate opens and the token is issued on a second call naming the
// approved gate.
func (h *Handler) IssueSpendToken(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	var req struct {
		AmountUsdTicks int64  `json:"amount_usd_ticks"`
		Purpose        string `json:"purpose"`
		GateID         string `json:"gate_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.AmountUsdTicks <= 0 {
		writeError(w, http.StatusBadRequest, "amount_usd_ticks must be positive")
		return
	}
	req.Purpose = strings.TrimSpace(req.Purpose)
	ctx := r.Context()
	if !task.IssueID.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, "gate_unavailable", "the run has no issue to ask on")
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.ApprovalGatesSettings(ws.Settings)
	issueToken := func() {
		token, hash, err := newSpendToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mint the token")
			return
		}
		expires := time.Now().Add(spendTokenTTL)
		raw, _ := json.Marshal(map[string]any{"amount_usd_ticks": req.AmountUsdTicks, "purpose": req.Purpose, "token_hash": hash, "token_expires_at": expires.UTC().Format(time.RFC3339Nano)})
		if _, err := h.Queries.CreateApprovalGateEvent(ctx, db.CreateApprovalGateEventParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, TaskID: task.ID, IssueID: issue.ID, GateType: GateSpend, Summary: fmt.Sprintf("spend %s · %s", usdLabel(req.AmountUsdTicks), req.Purpose),
			Details: raw, ResolvedAction: pgtype.Text{String: "approved", Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, ResolvedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record the token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires.UTC().Format(time.RFC3339Nano), "amount_usd_ticks": req.AmountUsdTicks})
	}
	if req.GateID != "" {
		gateID, ok := parseUUIDOrBadRequest(w, req.GateID, "gate id")
		if !ok {
			return
		}
		gate, err := h.Queries.GetApprovalGateEvent(ctx, db.GetApprovalGateEventParams{ID: gateID, TaskID: task.ID})
		if err != nil || gate.GateType != GateSpend {
			writeError(w, http.StatusNotFound, "spend gate not found")
			return
		}
		gate = h.expireGate(ctx, gate)
		switch gateStatus(gate) {
		case "approved":
			var d struct {
				TokenHash string `json:"token_hash"`
			}
			if json.Unmarshal(gate.Details, &d) == nil && d.TokenHash != "" {
				writeErrorCode(w, http.StatusConflict, "spend_token_issued", "a token was already issued for this gate")
				return
			}
			token, hash, err := newSpendToken()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to mint the token")
				return
			}
			expires := time.Now().Add(spendTokenTTL)
			if err := h.attachSpendToken(ctx, gate.ID, hash, expires); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record the token")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires.UTC().Format(time.RFC3339Nano), "amount_usd_ticks": req.AmountUsdTicks})
		case "pending":
			writeJSON(w, http.StatusAccepted, gateToResponse(gate))
		default:
			writeErrorCode(w, http.StatusForbidden, "spend_denied", "the spend was "+gateStatus(gate))
		}
		return
	}
	if req.AmountUsdTicks > cfg.SpendThresholdUsdTicks {
		gate, err := h.openGate(ctx, task, GateSpend, fmt.Sprintf("spend %s · %s", usdLabel(req.AmountUsdTicks), req.Purpose), map[string]any{"amount_usd_ticks": req.AmountUsdTicks, "purpose": req.Purpose})
		if err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "gate_unavailable", err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, gateToResponse(gate))
		return
	}
	issueToken()
}

func (h *Handler) attachSpendToken(ctx context.Context, gateID pgtype.UUID, hash string, expires time.Time) error {
	extra, _ := json.Marshal(map[string]any{"token_hash": hash, "token_expires_at": expires.UTC().Format(time.RFC3339Nano)})
	return h.Queries.AttachSpendToken(ctx, db.AttachSpendTokenParams{ID: gateID, Extra: extra})
}

func usdLabel(ticks int64) string {
	return fmt.Sprintf("$%.2f", float64(ticks)/1e10)
}

// POST /api/tasks/{taskId}/spend-token/verify {token, amount_usd_ticks}
// Single use: a token is spent by the first successful verification.
func (h *Handler) VerifySpendToken(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	var req struct {
		Token          string `json:"token"`
		AmountUsdTicks int64  `json:"amount_usd_ticks"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	hash := hashSpendToken(req.Token)
	rows, err := h.Queries.ListApprovalGateEvents(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load gates")
		return
	}
	for _, g := range rows {
		if g.GateType != GateSpend || !g.ResolvedAction.Valid || g.ResolvedAction.String != "approved" {
			continue
		}
		var d struct {
			TokenHash string `json:"token_hash"`
			Expires   string `json:"token_expires_at"`
			Amount    int64  `json:"amount_usd_ticks"`
			UsedAt    string `json:"used_at"`
		}
		if json.Unmarshal(g.Details, &d) != nil || d.TokenHash != hash {
			continue
		}
		if d.UsedAt != "" {
			writeErrorCode(w, http.StatusForbidden, "spend_token_used", "this token was already spent")
			return
		}
		if exp, err := time.Parse(time.RFC3339Nano, d.Expires); err != nil || time.Now().After(exp) {
			writeErrorCode(w, http.StatusForbidden, "spend_token_expired", "this token expired")
			return
		}
		if d.Amount > 0 && req.AmountUsdTicks > d.Amount {
			writeErrorCode(w, http.StatusForbidden, "spend_over_cap", fmt.Sprintf("the token allows %s", usdLabel(d.Amount)))
			return
		}
		if _, err := h.Queries.MarkSpendTokenUsed(r.Context(), g.ID); err != nil {
			writeErrorCode(w, http.StatusForbidden, "spend_token_used", "this token was already spent")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "gate_id": uuidToString(g.ID), "amount_usd_ticks": req.AmountUsdTicks})
		return
	}
	writeErrorCode(w, http.StatusForbidden, "spend_token_invalid", "unknown token for this run")
}
