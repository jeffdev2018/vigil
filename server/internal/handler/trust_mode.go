package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Trust Dial (K26): four modes per agent that decide where it must stop and
// ask. The mode modulates the decision points that already exist rather
// than adding new ones: observer never moves an issue or proves a criterion,
// propose needs an approved plan before work starts, approval is today's
// behaviour, autonomous skips the Plan Gate card (the Outcome Contract, a
// hard control, stays). A mode only changes by a human's explicit call.

const (
	TrustObserver   = "observer"
	TrustPropose    = "propose"
	TrustApproval   = "approval"
	TrustAutonomous = "autonomous"

	AuditTrustModeChanged        = "agent.trust_mode_changed"
	AuditTrustPromotionSuggested = "agent.trust_promotion_suggested"
	AuditPlanAutoApproved        = "plan.auto_approved"

	ErrCodeTrustModeBlocked      = "trust_mode_blocked"
	ErrCodeTrustModePlanRequired = "trust_mode_plan_required"
	trustSuggestionCooldown      = 7 * 24 * time.Hour
)

var trustRank = map[string]int{TrustObserver: 0, TrustPropose: 1, TrustApproval: 2, TrustAutonomous: 3}
var trustOrder = []string{TrustObserver, TrustPropose, TrustApproval, TrustAutonomous}

func nextTrustMode(mode string) string {
	r, ok := trustRank[mode]
	if !ok || r >= len(trustOrder)-1 {
		return ""
	}
	return trustOrder[r+1]
}

// requestAgent is the agent behind a task-token request, if any.
func (h *Handler) requestAgent(r *http.Request) (db.Agent, bool) {
	if !isMachineCredentialActor(r) {
		return db.Agent{}, false
	}
	id := strings.TrimSpace(r.Header.Get("X-Agent-ID"))
	if id == "" {
		return db.Agent{}, false
	}
	agentID := pgtype.UUID{}
	if err := agentID.Scan(id); err != nil {
		return db.Agent{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		return db.Agent{}, false
	}
	return agent, true
}

func writeTrustBlocked(w http.ResponseWriter, agent db.Agent, code, msg string) {
	writeJSON(w, http.StatusForbidden, map[string]any{"code": code, "error": msg, "agent_id": uuidToString(agent.ID), "trust_mode": agent.TrustMode})
}

// trustModeAllowsStatus applies the mode to an agent-made status change.
func (h *Handler) trustModeAllowsStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	if statusKey == "" || statusKey == issue.Status {
		return true
	}
	agent, ok := h.requestAgent(r)
	if !ok {
		return true
	}
	switch agent.TrustMode {
	case TrustObserver:
		writeTrustBlocked(w, agent, ErrCodeTrustModeBlocked, fmt.Sprintf("%s is in observer mode: it may comment and suggest, not change the issue", agent.Name))
		return false
	case TrustPropose:
		switch issuestatus.Effective(r.Context(), h.Queries, issue.WorkspaceID, statusKey) {
		case issuestatus.InProgress, issuestatus.InReview, issuestatus.Done:
			has, err := h.Queries.HasMaterializedIssuePlan(r.Context(), issue.ID)
			if err != nil {
				slog.Warn("trust dial: plan lookup failed", append(logger.RequestAttrs(r), "error", err)...)
				return true
			}
			if !has {
				writeTrustBlocked(w, agent, ErrCodeTrustModePlanRequired, fmt.Sprintf("%s is in propose mode: publish a plan and wait for its approval before starting", agent.Name))
				return false
			}
		}
	}
	return true
}

// trustModeAllowsProof: an observer proves nothing.
func (h *Handler) trustModeAllowsProof(w http.ResponseWriter, r *http.Request) bool {
	agent, ok := h.requestAgent(r)
	if ok && agent.TrustMode == TrustObserver {
		writeTrustBlocked(w, agent, ErrCodeTrustModeBlocked, fmt.Sprintf("%s is in observer mode: it cannot prove a criterion", agent.Name))
		return false
	}
	return true
}

// trustModeAutoApprovesPlan: an autonomous agent's plan materializes without
// a card. Returns true when it did.
func (h *Handler) trustModeAutoApprovesPlan(ctx context.Context, r *http.Request, issue db.Issue, plan db.IssuePlan, actorType, actorID string) bool {
	agent, ok := h.requestAgent(r)
	if !ok || agent.TrustMode != TrustAutonomous {
		return false
	}
	children, _, err := h.materializePlan(ctx, r, issue, plan, actorType, actorID)
	if err != nil {
		slog.Warn("trust dial: auto-approve failed, asking instead", append(logger.RequestAttrs(r), "error", err)...)
		return false
	}
	h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPlanAutoApproved, "issue", issue.ID, map[string]any{"plan_version": plan.Version, "sub_issues": len(children), "trust_mode": agent.TrustMode}, nil)
	return true
}

// ---- suggestions ----

type TrustMetrics struct {
	Days               int     `json:"days"`
	RunsTotal          int     `json:"runs_total"`
	AcceptedRate       float64 `json:"accepted_rate"`
	NoInterventionRate float64 `json:"no_intervention_rate"`
	ReopenRate         float64 `json:"reopen_rate"`
}

type TrustSuggestion struct {
	Eligible      bool              `json:"eligible"`
	CurrentMode   string            `json:"current_mode"`
	SuggestedMode string            `json:"suggested_mode,omitempty"`
	Metrics       TrustMetrics      `json:"metrics"`
	Thresholds    service.TrustDial `json:"thresholds"`
	Reasons       []string          `json:"reasons"`
}

func (h *Handler) trustSuggestionFor(ctx context.Context, agent db.Agent, now time.Time) (TrustSuggestion, error) {
	ws, err := h.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		return TrustSuggestion{}, err
	}
	dial := service.TrustDialSettings(ws.Settings)
	from := now.AddDate(0, 0, -dial.Days)
	rows, err := h.Queries.ListAgentScorecardDays(ctx, db.ListAgentScorecardDaysParams{WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, Day: dateOf(from), Day_2: dateOf(now.AddDate(0, 0, 1))})
	if err != nil {
		return TrustSuggestion{}, err
	}
	var t ScorecardTotals
	for _, row := range rows {
		t.add(int(row.RunsTotal), int(row.RunsFailed), int(row.RunsCancelled), int(row.RunsAccepted), int(row.RunsReopened), int(row.RunsNoIntervention), row.CostUsdTicksTotal)
	}
	s := TrustSuggestion{CurrentMode: agent.TrustMode, Thresholds: dial, Reasons: []string{}, Metrics: TrustMetrics{Days: dial.Days, RunsTotal: t.RunsTotal}}
	if t.RunsTotal > 0 {
		s.Metrics.AcceptedRate = float64(t.RunsAccepted) / float64(t.RunsTotal)
		s.Metrics.NoInterventionRate = float64(t.RunsNoIntervention) / float64(t.RunsTotal)
		s.Metrics.ReopenRate = float64(t.RunsReopened) / float64(t.RunsTotal)
	}
	next := nextTrustMode(agent.TrustMode)
	if next == "" {
		s.Reasons = append(s.Reasons, "already autonomous")
		return s, nil
	}
	if t.RunsTotal < dial.MinRuns {
		s.Reasons = append(s.Reasons, fmt.Sprintf("%d runs in %d days, %d needed", t.RunsTotal, dial.Days, dial.MinRuns))
	}
	if t.RunsTotal > 0 && s.Metrics.AcceptedRate < dial.MinAcceptedRate {
		s.Reasons = append(s.Reasons, fmt.Sprintf("accepted rate %.0f%% under %.0f%%", s.Metrics.AcceptedRate*100, dial.MinAcceptedRate*100))
	}
	if t.RunsTotal > 0 && s.Metrics.NoInterventionRate < dial.MinNoInterventionRate {
		s.Reasons = append(s.Reasons, fmt.Sprintf("no-intervention rate %.0f%% under %.0f%%", s.Metrics.NoInterventionRate*100, dial.MinNoInterventionRate*100))
	}
	if t.RunsTotal > 0 && s.Metrics.ReopenRate > dial.MaxReopenRate {
		s.Reasons = append(s.Reasons, fmt.Sprintf("reopen rate %.0f%% over %.0f%%", s.Metrics.ReopenRate*100, dial.MaxReopenRate*100))
	}
	if len(s.Reasons) == 0 {
		s.Eligible = true
		s.SuggestedMode = next
	}
	return s, nil
}

// NotifyTrustPromotions files one suggestion per eligible agent and week to
// the workspace leads. Called after the scorecard rollup. Never changes a mode.
func (h *Handler) NotifyTrustPromotions(ctx context.Context, now time.Time) (int, error) {
	agents, err := h.Queries.ListAgentsForTrustSuggestions(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	sent := 0
	for _, agent := range agents {
		s, err := h.trustSuggestionFor(ctx, agent, now)
		if err != nil || !s.Eligible {
			continue
		}
		n, err := h.Queries.CountTrustSuggestionNoticesSince(ctx, db.CountTrustSuggestionNoticesSinceParams{
			WorkspaceID: agent.WorkspaceID, AgentID: uuidToString(agent.ID), Since: pgtype.Timestamptz{Time: now.Add(-trustSuggestionCooldown), Valid: true},
		})
		if err != nil || n > 0 {
			continue
		}
		leads, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, agent.WorkspaceID)
		if err != nil {
			continue
		}
		details, _ := json.Marshal(map[string]any{"agent_id": uuidToString(agent.ID), "current_mode": agent.TrustMode, "suggested_mode": s.SuggestedMode, "metrics": s.Metrics})
		title := fmt.Sprintf("%s is ready for %s mode", agent.Name, s.SuggestedMode)
		body := fmt.Sprintf("%d runs in %d days · %.0f%% accepted · %.0f%% without intervention · %.0f%% reopened", s.Metrics.RunsTotal, s.Metrics.Days, s.Metrics.AcceptedRate*100, s.Metrics.NoInterventionRate*100, s.Metrics.ReopenRate*100)
		for _, l := range leads {
			item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
				ID: dbid.NewV7(), WorkspaceID: agent.WorkspaceID, RecipientType: l.Type, RecipientID: l.ID, Type: "trust_promotion_suggested", Severity: "info",
				Title: title, Body: pgtype.Text{String: body, Valid: true}, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
			})
			if err != nil {
				continue
			}
			h.publish(protocol.EventInboxNew, uuidToString(agent.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
		}
		h.audit(ctx, agent.WorkspaceID, "system", "", AuditTrustPromotionSuggested, "agent", agent.ID, map[string]any{"suggested_mode": s.SuggestedMode, "metrics": s.Metrics}, nil)
		sent++
	}
	return sent, nil
}

// ---- endpoints ----

type TrustModeChangeResponse struct {
	ID              string  `json:"id"`
	FromMode        string  `json:"from_mode"`
	ToMode          string  `json:"to_mode"`
	Reason          *string `json:"reason"`
	TriggeredByType string  `json:"triggered_by_type"`
	TriggeredByID   *string `json:"triggered_by_id"`
	CreatedAt       string  `json:"created_at"`
	Demotion        bool    `json:"demotion"`
}

func trustChangeToResponse(c db.TrustModeChange) TrustModeChangeResponse {
	return TrustModeChangeResponse{
		ID: uuidToString(c.ID), FromMode: c.FromMode, ToMode: c.ToMode, Reason: textToPtr(c.Reason), TriggeredByType: c.TriggeredByType,
		TriggeredByID: uuidToPtr(c.TriggeredByID), CreatedAt: timestampToString(c.CreatedAt), Demotion: trustRank[c.ToMode] < trustRank[c.FromMode],
	}
}

// GET /api/agents/{id}/trust-mode
func (h *Handler) GetAgentTrustMode(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "mode": agent.TrustMode, "modes": trustOrder})
}

// PUT /api/agents/{id}/trust-mode {mode, reason} — the only way a mode changes.
func (h *Handler) SetAgentTrustMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	var req struct {
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, valid := trustRank[req.Mode]; !valid {
		writeError(w, http.StatusBadRequest, "mode must be one of "+strings.Join(trustOrder, ", "))
		return
	}
	if req.Mode == agent.TrustMode {
		writeErrorCode(w, http.StatusUnprocessableEntity, "trust_mode_unchanged", "the agent is already in "+req.Mode+" mode")
		return
	}
	updated, err := h.Queries.SetAgentTrustMode(r.Context(), db.SetAgentTrustModeParams{ID: agent.ID, TrustMode: req.Mode})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the trust mode")
		return
	}
	change, err := h.Queries.CreateTrustModeChange(r.Context(), db.CreateTrustModeChangeParams{
		ID: dbid.NewV7(), WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, FromMode: agent.TrustMode, ToMode: req.Mode,
		Reason: pgtype.Text{String: strings.TrimSpace(req.Reason), Valid: strings.TrimSpace(req.Reason) != ""}, TriggeredByType: "member", TriggeredByID: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("trust dial: record change failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", userID, AuditTrustModeChanged, "agent", agent.ID, map[string]any{"from": agent.TrustMode, "to": req.Mode, "reason": strings.TrimSpace(req.Reason)}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "mode": updated.TrustMode, "change": trustChangeToResponse(change)})
}

// GET /api/agents/{id}/trust-mode/suggestions
func (h *Handler) GetAgentTrustSuggestion(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, err := h.trustSuggestionFor(r.Context(), agent, time.Now())
	if err != nil {
		slog.Warn("trust dial: suggestion failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate the suggestion")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GET /api/agents/{id}/trust-mode/history
func (h *Handler) ListAgentTrustHistory(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListTrustModeChanges(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list trust changes")
		return
	}
	out := make([]TrustModeChangeResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, trustChangeToResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": out})
}
