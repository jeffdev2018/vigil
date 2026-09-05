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
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Org chart operations (K75): escalation, offers, the living organisation
// (measured flows, proposals, drift), the tick (task-force termination,
// breaker, budget), the context a run receives and the revision a replay names.

// --- escalation ------------------------------------------------------------------

// orgUnitHolding is the unit the issue currently sits in: the latest routing
// flow, else the unit whose members include the assignee.
func (h *Handler) orgUnitHolding(ctx context.Context, s db.OrgStructure, def OrgDefinition, issue db.Issue) *OrgUnit {
	if f, err := h.Queries.GetLatestOrgRoutingForIssue(ctx, issue.ID); err == nil && f.StructureID == s.ID {
		if u := def.unit(f.UnitID); u != nil {
			return u
		}
	}
	if issue.AssigneeID.Valid {
		for i := range def.Units {
			if def.Units[i].hasMember(issue.AssigneeType.String, uuidToString(issue.AssigneeID)) || def.Units[i].SquadID == uuidToString(issue.AssigneeID) {
				return &def.Units[i]
			}
		}
	}
	return nil
}

// POST /api/issues/{id}/escalate {reason}: the issue moves up the escalates_to
// (else reports_to) edge of its unit; the unit's daily quota caps it.
func (h *Handler) EscalateIssue(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
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
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
	reason := truncateUTF8(strings.TrimSpace(req.Reason), 600)
	actorType, actorID := h.resolveActor(r, userID, wsRaw)
	s, found := h.orgStructureFor(r.Context(), wsUUID, issue.ProjectID)
	if !found || s.Status != orgStatusActive {
		writeError(w, http.StatusConflict, "no active structure routes this issue")
		return
	}
	def := decodeOrgDefinition(s.Definition)
	unit := h.orgUnitHolding(r.Context(), s, def, issue)
	if unit == nil {
		writeError(w, http.StatusConflict, "the issue sits in no unit of the structure")
		return
	}
	var target *OrgUnit
	for _, kind := range []string{"escalates_to", "reports_to"} {
		for _, e := range def.Edges {
			if e.From == unit.ID && e.Kind == kind {
				target = def.unit(e.To)
			}
		}
		if target != nil {
			break
		}
	}
	actor := pgtype.UUID{}
	if actorType == "member" {
		actor = parseUUID(actorID)
	}
	if target == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("unit %q has nobody to escalate to", unit.Name))
		return
	}
	since := pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
	if n, err := h.Queries.CountOrgFlowsSince(r.Context(), db.CountOrgFlowsSinceParams{StructureID: s.ID, Kind: orgFlowEscalation, Since: since, UnitID: pgtype.Text{String: unit.ID, Valid: true}}); err == nil && n >= int64(unit.EscalationQuotaPerDay) {
		h.orgAlert(r.Context(), s, unit.OwnerID, "Escalation quota reached for "+unit.Name, fmt.Sprintf("%d escalations today; %q asked for one more: %s", n, truncate(issue.Title, 80), reason), issue.ID, map[string]any{"unit": unit.ID, "quota": unit.EscalationQuotaPerDay})
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("unit %q reached its daily escalation quota (%d); its owner was told", unit.Name, unit.EscalationQuotaPerDay))
		return
	}
	targetType, targetID := h.orgTargetForUnit(r.Context(), s, target, issue)
	if targetType == "" {
		writeError(w, http.StatusConflict, fmt.Sprintf("unit %q has nobody to take the issue", target.Name))
		return
	}
	note := fmt.Sprintf("Escalated from %q to %q: %s", unit.Name, target.Name, reason)
	updated, err := h.orgAssign(r.Context(), issue, targetType, targetID, note, actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to escalate")
		return
	}
	h.orgFlow(r.Context(), s, unit.ID, orgFlowEscalation, issue.ID, actorType, actor, map[string]any{"to_unit": target.ID, "reason": reason, "assignee_type": targetType, "assignee_id": uuidToString(targetID)})
	h.orgFlow(r.Context(), s, target.ID, orgFlowRouting, issue.ID, actorType, actor, map[string]any{"assignee_type": targetType, "assignee_id": uuidToString(targetID), "escalated_from": unit.ID})
	h.orgAlert(r.Context(), s, target.OwnerID, "Escalation: "+truncate(issue.Title, 80), note, issue.ID, map[string]any{"from_unit": unit.ID, "to_unit": target.ID})
	h.audit(r.Context(), wsUUID, actorType, actorID, AuditOrgEscalated, "issue", issue.ID, map[string]any{"structure_id": uuidToString(s.ID), "from_unit": unit.ID, "to_unit": target.ID, "reason": reason}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"issue": issueToResponse(updated, h.getIssuePrefix(r.Context(), wsUUID)), "from_unit": unit.ID, "to_unit": target.ID})
}

// orgObserveReassignment records an issue leaving the unit that was routed
// it (drift signal), when a human reassigns it elsewhere.
func (h *Handler) orgObserveReassignment(ctx context.Context, issue db.Issue, actorType, actorID string) {
	f, err := h.Queries.GetLatestOrgRoutingForIssue(ctx, issue.ID)
	if err != nil || !issue.AssigneeID.Valid {
		return
	}
	s, err := h.Queries.GetOrgStructure(ctx, db.GetOrgStructureParams{ID: f.StructureID, WorkspaceID: issue.WorkspaceID})
	if err != nil || s.Status == orgStatusDissolved {
		return
	}
	def := decodeOrgDefinition(s.Definition)
	u := def.unit(f.UnitID)
	if u == nil {
		return
	}
	if u.hasMember(issue.AssigneeType.String, uuidToString(issue.AssigneeID)) || u.SquadID == uuidToString(issue.AssigneeID) || u.OwnerID == uuidToString(issue.AssigneeID) {
		return
	}
	actor := pgtype.UUID{}
	if actorType == "member" {
		actor = parseUUID(actorID)
	}
	h.orgFlow(ctx, s, u.ID, orgFlowReassigned, issue.ID, actorType, actor, map[string]any{"assignee_type": issue.AssigneeType.String, "assignee_id": uuidToString(issue.AssigneeID)})
}

// GET /api/issues/{id}/org-offers
func (h *Handler) ListIssueOrgOffers(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListOrgOffersForIssue(r.Context(), db.ListOrgOffersForIssueParams{IssueID: issueID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list offers")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, o := range rows {
		out = append(out, map[string]any{"id": uuidToString(o.ID), "agent_id": uuidToString(o.AgentID), "agent_name": o.AgentName, "confidence": o.Confidence, "cost_usd_ticks": o.CostUsdTicks, "eta_hours": o.EtaHours, "status": o.Status, "created_at": timestampToString(o.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": out})
}

// POST /api/issues/{id}/org-route: a member asks the structure to route an issue now.
func (h *Handler) RouteIssueNow(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if issue.AssigneeID.Valid {
		writeError(w, http.StatusConflict, "the issue already has an assignee")
		return
	}
	routed := h.orgRouteIssue(r.Context(), issue, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"issue": issueToResponse(routed, h.getIssuePrefix(r.Context(), issue.WorkspaceID))})
}

// --- living organisation -------------------------------------------------------------

type OrgUnitHealth struct {
	UnitID           string   `json:"unit_id"`
	Name             string   `json:"name"`
	Routed           int      `json:"routed"`
	Escalations      int      `json:"escalations"`
	ReassignedOut    int      `json:"reassigned_outside"`
	VacantRoles      []string `json:"vacant_roles"`
	SaturatedAgents  []string `json:"saturated_agents"`
	Paused           bool     `json:"paused"`
	SpendUsdTicks    int64    `json:"spend_usd_ticks"`
	BudgetUsdTicks   int64    `json:"budget_usd_ticks"`
	HumanReviewItems int      `json:"human_review_items"`
}

type OrgProposal struct {
	Key     string `json:"key"`
	UnitID  string `json:"unit_id,omitempty"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Measure string `json:"measure"`
}

type OrgHealth struct {
	StructureID        string          `json:"structure_id"`
	WindowDays         int             `json:"window_days"`
	Routed             int             `json:"routed"`
	Unrouted           int             `json:"unrouted"`
	Escalations        int             `json:"escalations"`
	StackedEscalations int64           `json:"stacked_escalations"`
	ReassignedOutside  int             `json:"reassigned_outside"`
	MarketShort        int             `json:"market_short"`
	Breakers           int             `json:"breakers"`
	HumanReviewItems   int             `json:"human_review_items"`
	DriftRate          float64         `json:"drift_rate"`
	Units              []OrgUnitHealth `json:"units"`
	Proposals          []OrgProposal   `json:"proposals"`
}

func (h *Handler) orgHealth(ctx context.Context, s db.OrgStructure) OrgHealth {
	def := decodeOrgDefinition(s.Definition)
	since := pgtype.Timestamptz{Time: time.Now().Add(-orgHealthWindow), Valid: true}
	out := OrgHealth{StructureID: uuidToString(s.ID), WindowDays: int(orgHealthWindow.Hours() / 24), Units: []OrgUnitHealth{}, Proposals: []OrgProposal{}}
	flows, _ := h.Queries.ListOrgFlowsSince(ctx, db.ListOrgFlowsSinceParams{StructureID: s.ID, Since: since})
	byUnit := map[string]*OrgUnitHealth{}
	for i := range def.Units {
		u := def.Units[i]
		uh := &OrgUnitHealth{UnitID: u.ID, Name: u.Name, VacantRoles: []string{}, SaturatedAgents: []string{}, BudgetUsdTicks: u.BudgetUsdTicks, Paused: h.orgUnitPaused(ctx, s, u.ID)}
		for _, role := range u.Roles {
			filled := false
			for _, m := range u.Members {
				if m.RoleID == role.ID {
					filled = true
				}
			}
			if !filled {
				uh.VacantRoles = append(uh.VacantRoles, role.Name)
			}
		}
		for _, a := range u.memberIDs("agent") {
			if n, err := h.Queries.CountAgentOpenTasks(ctx, parseUUID(a)); err == nil && n >= orgSaturatedOpenTasks {
				uh.SaturatedAgents = append(uh.SaturatedAgents, a)
			}
		}
		if spend, err := h.Queries.SumOrgUnitSpendSince(ctx, db.SumOrgUnitSpendSinceParams{StructureID: s.ID, UnitID: u.ID, Since: pgtype.Timestamptz{Time: time.Now().AddDate(0, -1, 0), Valid: true}}); err == nil {
			uh.SpendUsdTicks = spend
		}
		byUnit[u.ID] = uh
	}
	for _, f := range flows {
		uh := byUnit[f.UnitID]
		switch f.Kind {
		case orgFlowRouting:
			out.Routed++
			if uh != nil {
				uh.Routed++
			}
		case orgFlowUnrouted:
			out.Unrouted++
		case orgFlowEscalation:
			out.Escalations++
			out.HumanReviewItems++
			if uh != nil {
				uh.Escalations++
				uh.HumanReviewItems++
			}
		case orgFlowReassigned:
			out.ReassignedOutside++
			if uh != nil {
				uh.ReassignedOut++
			}
		case orgFlowMarketShort:
			out.MarketShort++
			out.HumanReviewItems++
		case orgFlowBreaker:
			out.Breakers++
			out.HumanReviewItems++
		case "approval_asked":
			out.HumanReviewItems++
			if uh != nil {
				uh.HumanReviewItems++
			}
		}
	}
	out.StackedEscalations, _ = h.Queries.CountIssuesEscalatedTwiceSince(ctx, db.CountIssuesEscalatedTwiceSinceParams{StructureID: s.ID, Since: since})
	if out.Routed > 0 {
		out.DriftRate = float64(out.ReassignedOutside) / float64(out.Routed)
	}
	for _, u := range def.Units {
		uh := byUnit[u.ID]
		out.Units = append(out.Units, *uh)
		if uh.Escalations >= 2*u.EscalationQuotaPerDay {
			out.Proposals = append(out.Proposals, OrgProposal{Key: "escalations:" + u.ID, UnitID: u.ID, Title: "Unit " + u.Name + " escalates too much", Body: "Add a backup member, split the unit, or widen its allow list so fewer issues go up.", Measure: fmt.Sprintf("%d escalations in %d days (quota %d/day)", uh.Escalations, out.WindowDays, u.EscalationQuotaPerDay)})
		}
		if len(u.Roles) > 0 && float64(len(uh.VacantRoles)) > orgVacantRolesRate*float64(len(u.Roles)) {
			out.Proposals = append(out.Proposals, OrgProposal{Key: "vacant:" + u.ID, UnitID: u.ID, Title: "Roles vacant in " + u.Name, Body: "Fill the roles or remove them: " + strings.Join(uh.VacantRoles, ", "), Measure: fmt.Sprintf("%d of %d roles vacant", len(uh.VacantRoles), len(u.Roles))})
		}
		if len(uh.SaturatedAgents) > 0 {
			out.Proposals = append(out.Proposals, OrgProposal{Key: "saturated:" + u.ID, UnitID: u.ID, Title: "Agents saturated in " + u.Name, Body: "Add capacity to the unit or route less to it.", Measure: fmt.Sprintf("%d agent(s) with %d+ open runs", len(uh.SaturatedAgents), orgSaturatedOpenTasks)})
		}
		if u.BudgetUsdTicks > 0 && uh.SpendUsdTicks >= u.BudgetUsdTicks {
			out.Proposals = append(out.Proposals, OrgProposal{Key: "budget:" + u.ID, UnitID: u.ID, Title: "Budget spent by " + u.Name, Body: "Raise the budget or pause the unit until next month.", Measure: fmt.Sprintf("%d of %d ticks this month", uh.SpendUsdTicks, u.BudgetUsdTicks)})
		}
	}
	if out.Routed+out.Unrouted > 0 && float64(out.Unrouted) > orgUnroutedRate*float64(out.Routed+out.Unrouted) {
		out.Proposals = append(out.Proposals, OrgProposal{Key: "unrouted", Title: "Issues without a resolvable owner", Body: "Add routing rules (labels, paths, keywords) or a fallback unit.", Measure: fmt.Sprintf("%d of %d issues unrouted", out.Unrouted, out.Routed+out.Unrouted)})
	}
	if out.Routed >= 5 && out.DriftRate > orgDriftReassignedRate {
		out.Proposals = append(out.Proposals, OrgProposal{Key: "drift", Title: "Measured flows drift from the declared structure", Body: "Re-review the structure: humans reassign outside the units it routes to.", Measure: fmt.Sprintf("%.0f%% of routed issues reassigned outside", out.DriftRate*100)})
	}
	return out
}

// GET /api/org/{id}/health
func (h *Handler) GetOrgHealth(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "structure id")
	if !ok {
		return
	}
	s, err := h.Queries.GetOrgStructure(r.Context(), db.GetOrgStructureParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "structure not found")
		return
	}
	writeJSON(w, http.StatusOK, h.orgHealth(r.Context(), s))
}

// orgProposeRestructurings files each proposal once per cooldown to the
// structure owner. Proposals are never applied by the system.
func (h *Handler) orgProposeRestructurings(ctx context.Context, s db.OrgStructure, health OrgHealth) int {
	filed := 0
	since := pgtype.Timestamptz{Time: time.Now().Add(-orgProposalCooldown), Valid: true}
	recent, _ := h.Queries.ListOrgFlowsSince(ctx, db.ListOrgFlowsSinceParams{StructureID: s.ID, Since: since})
	seen := map[string]bool{}
	for _, f := range recent {
		if f.Kind == orgFlowProposal {
			var d struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(f.Details, &d)
			seen[d.Key] = true
		}
	}
	for _, p := range health.Proposals {
		if seen[p.Key] {
			continue
		}
		h.orgAlert(ctx, s, "", "Restructuring proposed: "+p.Title, p.Body+" ("+p.Measure+"). Nothing changes until you save a new revision.", pgtype.UUID{}, map[string]any{"proposal": p})
		h.orgFlow(ctx, s, p.UnitID, orgFlowProposal, pgtype.UUID{}, "system", pgtype.UUID{}, map[string]any{"key": p.Key, "title": p.Title, "measure": p.Measure})
		filed++
	}
	return filed
}

// --- the tick ------------------------------------------------------------------------

// TickOrgStructures runs every few minutes: dissolves task forces at their
// end, trips the breaker on failing units, pauses units over budget, files
// restructuring proposals. Returns how many structures it acted on.
func (h *Handler) TickOrgStructures(ctx context.Context, now time.Time) (int, error) {
	structures, err := h.Queries.ListLiveOrgStructures(ctx)
	if err != nil {
		return 0, err
	}
	acted := 0
	for _, s := range structures {
		if s.Status != orgStatusActive {
			continue
		}
		if s.Model == OrgModelTaskforce && h.orgTaskforceEnded(ctx, s, now) {
			if updated, err := h.Queries.SetOrgStructureStatus(ctx, db.SetOrgStructureStatusParams{ID: s.ID, WorkspaceID: s.WorkspaceID, Status: orgStatusDissolved, PausedReason: "end condition met"}); err == nil {
				h.dissolveOrgStructure(ctx, updated, "task force reached its end: "+nonEmpty(s.EndCondition, "dissolution date"))
				acted++
			}
			continue
		}
		if h.orgBreaker(ctx, s, now) {
			acted++
		}
		if n := h.orgProposeRestructurings(ctx, s, h.orgHealth(ctx, s)); n > 0 {
			acted++
		}
	}
	return acted, nil
}

func (h *Handler) orgTaskforceEnded(ctx context.Context, s db.OrgStructure, now time.Time) bool {
	if s.DissolveAt.Valid && !now.Before(s.DissolveAt.Time) {
		return true
	}
	switch strings.TrimSpace(s.EndCondition) {
	case "all_issues_done":
		if !s.ProjectID.Valid {
			return false
		}
		n, err := h.Queries.CountOpenIssuesByProject(ctx, db.CountOpenIssuesByProjectParams{WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID, TerminalStatusKeys: h.projectTerminalIssueStatusKeys(ctx, s.WorkspaceID)})
		// A task force with no issue yet has not started; it ends once its work is done.
		routed, _ := h.Queries.CountOrgFlowsSince(ctx, db.CountOrgFlowsSinceParams{StructureID: s.ID, Kind: orgFlowRouting, Since: pgtype.Timestamptz{Time: s.CreatedAt.Time, Valid: true}})
		return err == nil && n == 0 && routed > 0
	case "budget_spent":
		if s.BudgetUsdTicks <= 0 {
			return false
		}
		var spend int64
		for _, u := range decodeOrgDefinition(s.Definition).Units {
			if n, err := h.Queries.SumOrgUnitSpendSince(ctx, db.SumOrgUnitSpendSinceParams{StructureID: s.ID, UnitID: u.ID, Since: pgtype.Timestamptz{Time: s.CreatedAt.Time, Valid: true}}); err == nil {
				spend += n
			}
		}
		return spend >= s.BudgetUsdTicks
	}
	return false
}

// dissolveOrgStructure: postmortem from the last run of its project, skills
// mined, owner told. Members are returned by the dissolution itself (the
// structure routes nothing anymore).
func (h *Handler) dissolveOrgStructure(ctx context.Context, s db.OrgStructure, reason string) {
	if s.ProjectID.Valid {
		if tasks, err := h.Queries.ListRecentTasksForProject(ctx, db.ListRecentTasksForProjectParams{WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID}); err == nil && len(tasks) > 0 {
			if err := h.TaskService.DraftPostmortemForRun(ctx, tasks[0], "taskforce_dissolved", reason); err != nil {
				slog.Warn("org: postmortem on dissolution failed", "error", err)
			}
		}
	}
	if _, err := h.MineSkills(ctx, time.Now()); err != nil {
		slog.Warn("org: skill mining on dissolution failed", "error", err)
	}
	h.orgFlow(ctx, s, "", orgFlowDissolved, pgtype.UUID{}, "system", pgtype.UUID{}, map[string]any{"reason": reason})
	h.orgAlert(ctx, s, "", "Dissolved: "+s.Name, reason+". A postmortem was drafted from its last run and its members are back in their home units.", pgtype.UUID{}, map[string]any{"reason": reason})
	h.audit(ctx, s.WorkspaceID, "system", "", AuditOrgDissolved, "org_structure", s.ID, map[string]any{"reason": reason, "model": s.Model}, nil)
	h.publish("org:updated", uuidToString(s.WorkspaceID), "system", "", map[string]any{"structure_id": uuidToString(s.ID), "status": orgStatusDissolved})
}

// orgBreaker pauses a unit whose agents fail or get undone too often, or
// that spent its monthly budget; the owner is told. One trip per day.
func (h *Handler) orgBreaker(ctx context.Context, s db.OrgStructure, now time.Time) bool {
	def := decodeOrgDefinition(s.Definition)
	since := pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true}
	tripped := false
	for _, u := range def.Units {
		if h.orgUnitPaused(ctx, s, u.ID) {
			continue
		}
		reason := ""
		for _, a := range u.memberIDs("agent") {
			agentID := parseUUID(a)
			if c, err := h.Queries.CountAgentTasksSince(ctx, db.CountAgentTasksSinceParams{AgentID: agentID, Since: since}); err == nil && c.Total >= orgBreakerMinRuns && float64(c.Failed)/float64(c.Total) >= orgBreakerFailureRate {
				reason = fmt.Sprintf("agent %s failed %d of %d runs in 24h", a, c.Failed, c.Total)
			}
			if n, err := h.Queries.CountAgentRunsReversedSince(ctx, db.CountAgentRunsReversedSinceParams{WorkspaceID: s.WorkspaceID, AgentID: agentID, Since: since}); err == nil && n >= orgBreakerMinRuns {
				reason = fmt.Sprintf("agent %s had %d runs undone in 24h", a, n)
			}
		}
		if u.BudgetUsdTicks > 0 {
			if spend, err := h.Queries.SumOrgUnitSpendSince(ctx, db.SumOrgUnitSpendSinceParams{StructureID: s.ID, UnitID: u.ID, Since: pgtype.Timestamptz{Time: now.AddDate(0, -1, 0), Valid: true}}); err == nil && spend >= u.BudgetUsdTicks {
				reason = fmt.Sprintf("monthly budget spent (%d of %d ticks)", spend, u.BudgetUsdTicks)
			}
		}
		if reason == "" {
			continue
		}
		h.orgFlow(ctx, s, u.ID, orgFlowBreaker, pgtype.UUID{}, "system", pgtype.UUID{}, map[string]any{"reason": reason})
		h.orgAlert(ctx, s, u.OwnerID, "Unit paused: "+u.Name, reason+". The unit receives nothing for 24 hours; resume it by saving a revision or wait.", pgtype.UUID{}, map[string]any{"unit": u.ID, "reason": reason})
		tripped = true
	}
	return tripped
}

// --- the run's context and the replay's revision ------------------------------------

// OrgContextNode is what a run learns about the structure it works in.
type OrgContext struct {
	StructureID    string   `json:"structure_id"`
	StructureName  string   `json:"structure_name"`
	Model          string   `json:"model"`
	Revision       int32    `json:"revision"`
	RevisionID     string   `json:"revision_id"`
	UnitID         string   `json:"unit_id,omitempty"`
	UnitName       string   `json:"unit_name,omitempty"`
	Autonomy       string   `json:"autonomy,omitempty"`
	Allow          []string `json:"allow,omitempty"`
	Deny           []string `json:"deny,omitempty"`
	EscalationPath []string `json:"escalation_path,omitempty"`
}

// resolveClaimOrgContext names the structure and unit a claimed run acts in.
func (h *Handler) resolveClaimOrgContext(ctx context.Context, issue db.Issue, agentID pgtype.UUID) *OrgContext {
	s, found := h.orgStructureFor(ctx, issue.WorkspaceID, issue.ProjectID)
	if !found || s.Status == orgStatusDissolved {
		return nil
	}
	def := decodeOrgDefinition(s.Definition)
	out := &OrgContext{StructureID: uuidToString(s.ID), StructureName: s.Name, Model: s.Model, Revision: s.Revision, RevisionID: uuidToString(s.RevisionID)}
	unit := h.orgUnitHolding(ctx, s, def, issue)
	if unit == nil {
		for i := range def.Units {
			if def.Units[i].hasMember("agent", uuidToString(agentID)) {
				unit = &def.Units[i]
			}
		}
	}
	if unit != nil {
		out.UnitID, out.UnitName, out.Autonomy, out.Allow, out.Deny = unit.ID, unit.Name, unit.Autonomy, unit.Allow, unit.Deny
		seen := map[string]bool{unit.ID: true}
		for cur := unit; cur != nil; {
			var next *OrgUnit
			for _, kind := range []string{"escalates_to", "reports_to"} {
				for _, e := range def.Edges {
					if e.From == cur.ID && e.Kind == kind && !seen[e.To] {
						next = def.unit(e.To)
					}
				}
				if next != nil {
					break
				}
			}
			if next == nil {
				break
			}
			seen[next.ID] = true
			out.EscalationPath = append(out.EscalationPath, next.Name)
			cur = next
		}
	}
	return out
}

// orgRevisionForIssue is the revision a replay snapshot records.
func (h *Handler) orgRevisionForIssue(ctx context.Context, wsID pgtype.UUID, issueID pgtype.UUID) (string, int32) {
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsID})
	if err != nil {
		return "", 0
	}
	s, found := h.orgStructureFor(ctx, wsID, issue.ProjectID)
	if !found {
		return "", 0
	}
	return uuidToString(s.RevisionID), s.Revision
}

// --- seeds ---------------------------------------------------------------------------

// seedDefaultOrg gives a new workspace the owner network with its creator as
// owner, active at once: a flat structure every project inherits until it
// picks another model.
func (h *Handler) seedDefaultOrg(ctx context.Context, q *db.Queries, wsID, userID pgtype.UUID) error {
	tpl := orgTemplate(OrgModelOwnerNetwork, uuidToString(userID), nil)
	def := tpl.Definition
	for i := range def.Units {
		for _, deny := range orgNonNegotiableDeny {
			if !containsStr(def.Units[i].Deny, deny) {
				def.Units[i].Deny = append(def.Units[i].Deny, deny)
			}
		}
	}
	raw, _ := json.Marshal(def)
	id, revID := dbid.NewV7(), dbid.NewV7()
	s, err := q.CreateOrgStructure(ctx, db.CreateOrgStructureParams{ID: id, WorkspaceID: wsID, Model: OrgModelOwnerNetwork, Name: tpl.Name, Status: orgStatusActive, RevisionID: revID, Definition: raw, OwnerID: userID, EvalAttestation: "seed: default owner network, no agent member yet", CreatedBy: userID})
	if err != nil {
		return err
	}
	_, err = q.CreateOrgRevision(ctx, db.CreateOrgRevisionParams{ID: revID, WorkspaceID: wsID, StructureID: s.ID, Revision: 1, Model: OrgModelOwnerNetwork, Status: orgStatusActive, Definition: raw, ChangedBy: userID, Note: "workspace default"})
	return err
}

// seedProjectOrg creates a draft structure for a new project from a template.
func (h *Handler) seedProjectOrg(ctx context.Context, wsID, projectID pgtype.UUID, model, userID string) {
	if !containsStr(orgModels, model) {
		return
	}
	var agentIDs []string
	if agents, err := h.Queries.ListAgents(ctx, wsID); err == nil {
		for _, a := range agents {
			agentIDs = append(agentIDs, uuidToString(a.ID))
		}
	}
	tpl := orgTemplate(model, userID, agentIDs)
	def := tpl.Definition
	if err := h.validateOrg(ctx, wsID, model, &def); err != nil {
		// A template that the workspace cannot honour (a dial too low) still seeds without agents.
		def = orgTemplate(model, userID, nil).Definition
		if err := h.validateOrg(ctx, wsID, model, &def); err != nil {
			slog.Warn("org: project template invalid", "error", err, "model", model)
			return
		}
	}
	if _, err := h.createOrgStructure(ctx, wsID, projectID, model, tpl.Name, orgStatusDraft, def, parseUUID(userID), pgtype.Timestamptz{}, "", 0, "", parseUUID(userID), "project template"); err != nil {
		slog.Warn("org: project template seed failed", "error", err)
	}
}

var _ = util.ParseUUID
