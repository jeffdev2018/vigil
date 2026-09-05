package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Executable org chart (K75): a structure is data that routes, escalates,
// gates and budgets. Seven models share one definition shape (units with
// members that are humans or agents, roles, edges, routing rules, committees,
// a market). One structure per project, or the workspace default. Guard
// rails hold at save time: Rule of Two per unit and per path, a named human
// owner per unit and per structure, the Trust Dial as the ceiling of any
// unit's autonomy, termination declared for task forces and committees,
// non-negotiable denials. The living org measures real flows and proposes
// restructurings that a human applies.

const (
	OrgModelHierarchy    = "hierarchy"
	OrgModelSquads       = "squads"
	OrgModelMatrix       = "matrix"
	OrgModelCircles      = "circles"
	OrgModelOwnerNetwork = "owner_network"
	OrgModelTaskforce    = "taskforce"
	OrgModelMarket       = "market"

	orgStatusDraft     = "draft"
	orgStatusActive    = "active"
	orgStatusPaused    = "paused"
	orgStatusDissolved = "dissolved"

	orgFlowRouting     = "routing"
	orgFlowEscalation  = "escalation"
	orgFlowUnrouted    = "unrouted"
	orgFlowMarketShort = "market_short"
	orgFlowBreaker     = "breaker"
	orgFlowReassigned  = "reassigned_outside"
	orgFlowProposal    = "proposal"
	orgFlowDissolved   = "dissolved"

	InboxTypeOrgAlert = "org_alert"

	AuditOrgSaved     = "org.saved"
	AuditOrgRouted    = "org.routed"
	AuditOrgEscalated = "org.escalated"
	AuditOrgDissolved = "org.dissolved"

	orgAssignOptionPrefix = "org_assign:"
	orgHoldOption         = "org_hold"

	// ponytail: constants; settings when someone asks.
	orgDefaultEscalationQuota  = 5
	orgDefaultOffersPerDay     = 5
	orgDefaultMinOffers        = 2
	orgSaturatedOpenTasks      = 5
	orgBreakerMinRuns          = 4
	orgBreakerFailureRate      = 0.5
	orgDriftReassignedRate     = 0.3
	orgVacantRolesRate         = 0.25
	orgUnroutedRate            = 0.1
	orgHealthWindow            = 7 * 24 * time.Hour
	orgProposalCooldown        = 24 * time.Hour
	orgLLMReviewSecondsPerItem = 90
)

var orgModels = []string{OrgModelHierarchy, OrgModelSquads, OrgModelMatrix, OrgModelCircles, OrgModelOwnerNetwork, OrgModelTaskforce, OrgModelMarket}

// Rule of Two properties: a unit that holds all three needs a human in the loop.
var orgProperties = []string{"untrusted_input", "sensitive_data", "external_effects"}

// orgNonNegotiableDeny is merged into every unit's deny list at save.
var orgNonNegotiableDeny = []string{"delete", "bill", "send_external_without_approval", "touch_secrets", "commit_money"}

// orgAutonomyRank maps a unit's autonomy tier onto the Trust Dial ranks.
var orgAutonomyRank = map[string]int{"read_only": 0, "draft": 1, "approve_payload": 2, "auto": 3}

var orgEdgeKinds = map[string]bool{"reports_to": true, "backs_up": true, "escalates_to": true, "consults": true}

type OrgMember struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Role   string `json:"role,omitempty"`
	RoleID string `json:"role_id,omitempty"`
}

type OrgRole struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Responsibilities string   `json:"responsibilities,omitempty"`
	Keywords         []string `json:"keywords,omitempty"`
}

type OrgUnit struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Kind                  string            `json:"kind,omitempty"`
	OwnerID               string            `json:"owner_id,omitempty"`
	SquadID               string            `json:"squad_id,omitempty"`
	MissionGoalID         string            `json:"mission_goal_id,omitempty"`
	BudgetUsdTicks        int64             `json:"budget_usd_ticks,omitempty"`
	Excludes              []string          `json:"excludes"`
	Autonomy              string            `json:"autonomy"`
	Allow                 []string          `json:"allow"`
	Deny                  []string          `json:"deny"`
	EscalationQuotaPerDay int               `json:"escalation_quota_per_day"`
	HumanApproval         bool              `json:"human_approval,omitempty"`
	ApprovalRisk          string            `json:"approval_risk,omitempty"`
	Deciders              map[string]string `json:"deciders,omitempty"`
	Members               []OrgMember       `json:"members"`
	Roles                 []OrgRole         `json:"roles"`
}

type OrgEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Kind          string `json:"kind"`
	HumanApproval bool   `json:"human_approval,omitempty"`
}

type OrgRule struct {
	ID         string   `json:"id"`
	Labels     []string `json:"labels,omitempty"`
	Paths      []string `json:"paths,omitempty"`
	Keywords   []string `json:"keywords,omitempty"`
	TargetUnit string   `json:"target_unit"`
	Priority   int      `json:"priority"`
}

type OrgCommittee struct {
	DecisionType string   `json:"decision_type"`
	UnitIDs      []string `json:"unit_ids"`
	Quorum       int      `json:"quorum"`
	MaxRounds    int      `json:"max_rounds"`
}

type OrgMarket struct {
	PriceCapUsdTicks     int64 `json:"price_cap_usd_ticks"`
	OffersPerAgentPerDay int   `json:"offers_per_agent_per_day"`
	MinOffers            int   `json:"min_offers"`
}

type OrgDefinition struct {
	Units      []OrgUnit      `json:"units"`
	Edges      []OrgEdge      `json:"edges"`
	Rules      []OrgRule      `json:"rules"`
	Committees []OrgCommittee `json:"committees"`
	Market     OrgMarket      `json:"market"`
}

func (d *OrgDefinition) unit(id string) *OrgUnit {
	for i := range d.Units {
		if d.Units[i].ID == id {
			return &d.Units[i]
		}
	}
	return nil
}

func (u OrgUnit) properties() map[string]bool {
	p := map[string]bool{}
	for _, prop := range orgProperties {
		p[prop] = true
	}
	for _, e := range u.Excludes {
		delete(p, e)
	}
	return p
}

func (u OrgUnit) memberIDs(kind string) []string {
	var out []string
	for _, m := range u.Members {
		if m.Type == kind {
			out = append(out, m.ID)
		}
	}
	return out
}

func (u OrgUnit) hasMember(kind, id string) bool {
	for _, m := range u.Members {
		if m.Type == kind && m.ID == id {
			return true
		}
	}
	return false
}

type OrgStructureResponse struct {
	ID              string        `json:"id"`
	WorkspaceID     string        `json:"workspace_id"`
	ProjectID       *string       `json:"project_id"`
	Model           string        `json:"model"`
	Name            string        `json:"name"`
	Status          string        `json:"status"`
	Revision        int32         `json:"revision"`
	RevisionID      *string       `json:"revision_id"`
	Definition      OrgDefinition `json:"definition"`
	OwnerID         *string       `json:"owner_id"`
	DissolveAt      *string       `json:"dissolve_at"`
	EndCondition    string        `json:"end_condition"`
	BudgetUsdTicks  int64         `json:"budget_usd_ticks"`
	EvalAttestation string        `json:"eval_attestation"`
	PausedReason    string        `json:"paused_reason"`
	DissolvedAt     *string       `json:"dissolved_at"`
	PausedUnits     []string      `json:"paused_units"`
	CreatedBy       *string       `json:"created_by"`
	CreatedAt       string        `json:"created_at"`
	UpdatedAt       string        `json:"updated_at"`
}

func tsStringPtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	s := timestampToString(ts)
	return &s
}

func decodeOrgDefinition(raw []byte) OrgDefinition {
	var d OrgDefinition
	_ = json.Unmarshal(raw, &d)
	if d.Units == nil {
		d.Units = []OrgUnit{}
	}
	if d.Edges == nil {
		d.Edges = []OrgEdge{}
	}
	if d.Rules == nil {
		d.Rules = []OrgRule{}
	}
	if d.Committees == nil {
		d.Committees = []OrgCommittee{}
	}
	return d
}

func (h *Handler) orgToResponse(ctx context.Context, s db.OrgStructure) OrgStructureResponse {
	out := OrgStructureResponse{
		ID: uuidToString(s.ID), WorkspaceID: uuidToString(s.WorkspaceID), ProjectID: uuidToPtr(s.ProjectID), Model: s.Model, Name: s.Name, Status: s.Status,
		Revision: s.Revision, RevisionID: uuidToPtr(s.RevisionID), Definition: decodeOrgDefinition(s.Definition), OwnerID: uuidToPtr(s.OwnerID),
		DissolveAt: tsStringPtr(s.DissolveAt), EndCondition: s.EndCondition, BudgetUsdTicks: s.BudgetUsdTicks, EvalAttestation: s.EvalAttestation, PausedReason: s.PausedReason,
		DissolvedAt: tsStringPtr(s.DissolvedAt), PausedUnits: []string{}, CreatedBy: uuidToPtr(s.CreatedBy), CreatedAt: timestampToString(s.CreatedAt), UpdatedAt: timestampToString(s.UpdatedAt),
	}
	for _, u := range out.Definition.Units {
		if h.orgUnitPaused(ctx, s, u.ID) {
			out.PausedUnits = append(out.PausedUnits, u.ID)
		}
	}
	return out
}

// --- templates ---------------------------------------------------------------

type OrgTemplate struct {
	Model                    string        `json:"model"`
	Name                     string        `json:"name"`
	Pattern                  string        `json:"pattern"`
	Description              string        `json:"description"`
	CoordinationRunsPerIssue float64       `json:"coordination_runs_per_issue"`
	Definition               OrgDefinition `json:"definition"`
}

func orgTemplate(model, ownerID string, agentIDs []string) OrgTemplate {
	unit := func(id, name, kind string) OrgUnit {
		u := OrgUnit{ID: id, Name: name, Kind: kind, OwnerID: ownerID, Excludes: []string{"external_effects"}, Autonomy: "draft", Allow: []string{"read", "comment", "propose_plan"}, Deny: []string{}, EscalationQuotaPerDay: orgDefaultEscalationQuota, Members: []OrgMember{{Type: "member", ID: ownerID, Role: "owner"}}, Roles: []OrgRole{}}
		for _, a := range agentIDs {
			u.Members = append(u.Members, OrgMember{Type: "agent", ID: a, Role: "member"})
		}
		return u
	}
	t := OrgTemplate{Model: model, Definition: OrgDefinition{Edges: []OrgEdge{}, Rules: []OrgRule{}, Committees: []OrgCommittee{}}}
	switch model {
	case OrgModelHierarchy:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Hierarchy", "supervisor", 1
		t.Description = "A superior per node: delegation down, escalation up, approval by the superior above a risk threshold."
		lead := unit("lead", "Lead", "unit")
		lead.Members = lead.Members[:1]
		team := unit("team", "Team", "unit")
		team.ApprovalRisk = "high"
		t.Definition.Units = []OrgUnit{lead, team}
		t.Definition.Edges = []OrgEdge{{From: "team", To: "lead", Kind: "reports_to"}, {From: "team", To: "lead", Kind: "escalates_to"}}
	case OrgModelSquads:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Autonomous squads", "parallel with a lead", 1
		t.Description = "A human owner, agents, a mission and a budget per squad; everything that touches X goes to squad Y."
		t.Definition.Units = []OrgUnit{unit("squad-1", "Squad 1", "unit")}
		t.Definition.Rules = []OrgRule{{ID: "r1", Keywords: []string{}, TargetUnit: "squad-1", Priority: 1}}
	case OrgModelMatrix:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Competence × project matrix", "supervisor by characteristic", 0
		t.Description = "Competence pools lend agents to projects; routing by measured competence, runtime availability and cost."
		t.Definition.Units = []OrgUnit{unit("pool", "Competence pool", "pool")}
	case OrgModelCircles:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Circles and roles", "handoff by role", 1
		t.Description = "Roles carry responsibilities, humans and agents fill them, circles nest, decisions by consent."
		c := unit("circle", "Circle", "circle")
		c.Roles = []OrgRole{{ID: "facilitator", Name: "Facilitator", Responsibilities: "keeps the circle moving", Keywords: []string{}}}
		c.Members[0].RoleID = "facilitator"
		t.Definition.Units = []OrgUnit{c}
		t.Definition.Committees = []OrgCommittee{{DecisionType: "consent", UnitIDs: []string{"circle"}, Quorum: 1, MaxRounds: 1}}
	case OrgModelOwnerNetwork:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Owner network", "handoff with criteria", 0
		t.Description = "Flat: an owner and a backup per module, folder, customer or channel. The default of a new workspace."
		o := unit("owners", "Owners", "unit")
		t.Definition.Units = []OrgUnit{o}
		t.Definition.Rules = []OrgRule{{ID: "r1", Paths: []string{"*"}, TargetUnit: "owners", Priority: 0}}
	case OrgModelTaskforce:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Temporary task force", "plan orchestration with termination", 1
		t.Description = "Borrowed members, a budget, a dissolution date and an end condition; dissolution drafts a postmortem and mines skills."
		t.Definition.Units = []OrgUnit{unit("taskforce", "Task force", "taskforce")}
	case OrgModelMarket:
		t.Name, t.Pattern, t.CoordinationRunsPerIssue = "Internal market", "supervisor by scoring", 0
		t.Description = "Agents bid on issues with an offer (confidence, estimated cost, delay); the best offer under the human price cap wins."
		t.Definition.Units = []OrgUnit{unit("market", "Market", "market")}
		t.Definition.Market = OrgMarket{PriceCapUsdTicks: 5_000_000, OffersPerAgentPerDay: orgDefaultOffersPerDay, MinOffers: orgDefaultMinOffers}
	}
	return t
}

// GET /api/org/templates
func (h *Handler) ListOrgTemplates(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var agentIDs []string
	if agents, err := h.Queries.ListAgents(r.Context(), wsUUID); err == nil {
		for _, a := range agents {
			agentIDs = append(agentIDs, uuidToString(a.ID))
		}
	}
	out := make([]OrgTemplate, 0, len(orgModels))
	for _, m := range orgModels {
		out = append(out, orgTemplate(m, userID, agentIDs))
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// --- validation ----------------------------------------------------------------

type orgValidationError struct{ msg string }

func (e orgValidationError) Error() string { return e.msg }

func orgErrorf(format string, args ...any) error {
	return orgValidationError{msg: fmt.Sprintf(format, args...)}
}

// validateOrg normalizes and checks a definition against the model and the
// workspace: ids and references, Rule of Two per unit and per path, the
// Trust Dial ceiling per agent member, termination for committees, market
// parameters, model-specific shape. Non-negotiable denials are merged in.
func (h *Handler) validateOrg(ctx context.Context, wsUUID pgtype.UUID, model string, d *OrgDefinition) error {
	if !containsStr(orgModels, model) {
		return orgErrorf("model must be one of: %s", strings.Join(orgModels, ", "))
	}
	if len(d.Units) == 0 {
		return orgErrorf("a structure needs at least one unit")
	}
	agents := map[string]db.Agent{}
	if rows, err := h.Queries.ListAgents(ctx, wsUUID); err == nil {
		for _, a := range rows {
			agents[uuidToString(a.ID)] = a
		}
	}
	memberKnown := func(id string) bool {
		uid, err := util.ParseUUID(id)
		if err != nil {
			return false
		}
		_, err = h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: uid, WorkspaceID: wsUUID})
		return err == nil
	}
	seen := map[string]bool{}
	for i := range d.Units {
		u := &d.Units[i]
		u.ID = strings.TrimSpace(u.ID)
		u.Name = strings.TrimSpace(u.Name)
		if u.ID == "" || u.Name == "" {
			return orgErrorf("unit #%d needs an id and a name", i+1)
		}
		if seen[u.ID] {
			return orgErrorf("unit id %q is used twice", u.ID)
		}
		seen[u.ID] = true
		if u.Autonomy == "" {
			u.Autonomy = "draft"
		}
		if _, ok := orgAutonomyRank[u.Autonomy]; !ok {
			return orgErrorf("unit %q: autonomy must be read_only, draft, approve_payload or auto", u.Name)
		}
		for _, e := range u.Excludes {
			if !containsStr(orgProperties, e) {
				return orgErrorf("unit %q: unknown property %q (untrusted_input, sensitive_data, external_effects)", u.Name, e)
			}
		}
		if u.Excludes == nil {
			u.Excludes = []string{}
		}
		if u.Allow == nil {
			u.Allow = []string{}
		}
		for _, deny := range orgNonNegotiableDeny {
			if !containsStr(u.Deny, deny) {
				u.Deny = append(u.Deny, deny)
			}
		}
		if u.EscalationQuotaPerDay <= 0 {
			u.EscalationQuotaPerDay = orgDefaultEscalationQuota
		}
		if u.OwnerID != "" && !memberKnown(u.OwnerID) {
			return orgErrorf("unit %q: owner must be a member of this workspace", u.Name)
		}
		if u.SquadID != "" {
			sid, err := util.ParseUUID(u.SquadID)
			if err != nil {
				return orgErrorf("unit %q: invalid squad id", u.Name)
			}
			if squad, err := h.Queries.GetSquad(ctx, sid); err != nil || squad.WorkspaceID != wsUUID {
				return orgErrorf("unit %q: squad not found in this workspace", u.Name)
			}
		}
		if u.Members == nil {
			u.Members = []OrgMember{}
		}
		if u.Roles == nil {
			u.Roles = []OrgRole{}
		}
		roleIDs := map[string]bool{}
		for _, r := range u.Roles {
			if r.ID == "" {
				return orgErrorf("unit %q: every role needs an id", u.Name)
			}
			roleIDs[r.ID] = true
		}
		for _, m := range u.Members {
			switch m.Type {
			case "agent":
				a, ok := agents[m.ID]
				if !ok {
					return orgErrorf("unit %q: agent %s not found in this workspace", u.Name, m.ID)
				}
				if orgAutonomyRank[u.Autonomy] > trustRank[a.TrustMode] {
					return orgErrorf("unit %q grants %s but agent %q's trust dial is %s; the structure cannot grant more than the dial", u.Name, u.Autonomy, a.Name, a.TrustMode)
				}
			case "member":
				if !memberKnown(m.ID) {
					return orgErrorf("unit %q: member %s not found in this workspace", u.Name, m.ID)
				}
			default:
				return orgErrorf("unit %q: member type must be member or agent", u.Name)
			}
			if m.RoleID != "" && !roleIDs[m.RoleID] {
				return orgErrorf("unit %q: role %q does not exist", u.Name, m.RoleID)
			}
		}
		// Rule of Two per unit.
		if p := u.properties(); len(p) == len(orgProperties) && !u.HumanApproval {
			return orgErrorf("Rule of Two: unit %q holds untrusted input, sensitive data and external effects at once; exclude one property or set human_approval", u.Name)
		}
		// A unit exposed to external effects names who decides on money, outbound data and external messages.
		if u.properties()["external_effects"] {
			for _, class := range []string{"money", "outbound_data", "external_message"} {
				if u.Deciders[class] == "" || !memberKnown(u.Deciders[class]) {
					return orgErrorf("unit %q has external effects: name a member as decider for %s", u.Name, class)
				}
			}
		}
	}
	for i, e := range d.Edges {
		if !orgEdgeKinds[e.Kind] {
			return orgErrorf("edge #%d: kind must be reports_to, backs_up, escalates_to or consults", i+1)
		}
		from, to := d.unit(e.From), d.unit(e.To)
		if from == nil || to == nil || e.From == e.To {
			return orgErrorf("edge #%d: from and to must name two different units", i+1)
		}
		// Rule of Two per path: two units that together cumulate the three properties.
		union := map[string]bool{}
		for p := range from.properties() {
			union[p] = true
		}
		for p := range to.properties() {
			union[p] = true
		}
		if len(union) == len(orgProperties) && !e.HumanApproval {
			return orgErrorf("Rule of Two: path %s → %s cumulates untrusted input, sensitive data and external effects; require human approval on that edge", from.Name, to.Name)
		}
	}
	for i := range d.Rules {
		r := &d.Rules[i]
		if r.ID == "" {
			r.ID = fmt.Sprintf("r%d", i+1)
		}
		if d.unit(r.TargetUnit) == nil {
			return orgErrorf("rule %q targets unknown unit %q", r.ID, r.TargetUnit)
		}
	}
	for i, c := range d.Committees {
		if c.DecisionType == "" || len(c.UnitIDs) == 0 {
			return orgErrorf("committee #%d needs a decision type and units", i+1)
		}
		for _, id := range c.UnitIDs {
			if d.unit(id) == nil {
				return orgErrorf("committee %q: unknown unit %q", c.DecisionType, id)
			}
		}
		if c.Quorum < 1 || c.Quorum > len(c.UnitIDs) || c.MaxRounds < 1 {
			return orgErrorf("committee %q: termination is mandatory (quorum between 1 and %d, max_rounds ≥ 1)", c.DecisionType, len(c.UnitIDs))
		}
	}
	switch model {
	case OrgModelHierarchy:
		roots := 0
		for _, u := range d.Units {
			has := false
			for _, e := range d.Edges {
				if e.From == u.ID && e.Kind == "reports_to" {
					has = true
				}
			}
			if !has {
				roots++
			}
		}
		if roots != 1 {
			return orgErrorf("hierarchy: exactly one unit reports to nobody (found %d)", roots)
		}
	case OrgModelSquads:
		for _, u := range d.Units {
			if u.SquadID == "" && len(u.memberIDs("agent")) == 0 {
				return orgErrorf("squads: unit %q needs a squad or agent members", u.Name)
			}
		}
	case OrgModelCircles:
		for _, u := range d.Units {
			if len(u.Roles) == 0 {
				return orgErrorf("circles: unit %q needs at least one role", u.Name)
			}
		}
	case OrgModelMarket:
		if d.Market.PriceCapUsdTicks <= 0 {
			return orgErrorf("market: the human price cap (price_cap_usd_ticks) is required")
		}
		if d.Market.OffersPerAgentPerDay <= 0 {
			d.Market.OffersPerAgentPerDay = orgDefaultOffersPerDay
		}
		if d.Market.MinOffers < 2 {
			d.Market.MinOffers = orgDefaultMinOffers
		}
	}
	return nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// orgActivationCheck: what a structure needs before it acts.
func (h *Handler) orgActivationCheck(ctx context.Context, wsUUID pgtype.UUID, model string, ownerID pgtype.UUID, dissolveAt pgtype.Timestamptz, endCondition, evalAttestation string, d OrgDefinition) error {
	if !ownerID.Valid {
		return orgErrorf("an active structure needs a human owner")
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: ownerID, WorkspaceID: wsUUID}); err != nil {
		return orgErrorf("the owner must be a member of this workspace")
	}
	if strings.TrimSpace(evalAttestation) == "" {
		return orgErrorf("activation needs the eval attestation: the 30-case set (normal, edge, adversarial) ran on this revision; name the eval run")
	}
	if model == OrgModelTaskforce && !dissolveAt.Valid && strings.TrimSpace(endCondition) == "" {
		return orgErrorf("a task force declares its termination: a dissolution date or an end condition (all_issues_done, budget_spent)")
	}
	for _, u := range d.Units {
		if u.OwnerID == "" {
			slog.Info("org: unit without owner stays inactive", "unit", u.Name)
		}
	}
	return nil
}

// --- CRUD --------------------------------------------------------------------------

type orgWriteRequest struct {
	ProjectID       *string         `json:"project_id"`
	Model           string          `json:"model"`
	Name            string          `json:"name"`
	Definition      json.RawMessage `json:"definition"`
	OwnerID         *string         `json:"owner_id"`
	DissolveAt      *string         `json:"dissolve_at"`
	EndCondition    string          `json:"end_condition"`
	BudgetUsdTicks  int64           `json:"budget_usd_ticks"`
	EvalAttestation string          `json:"eval_attestation"`
	Note            string          `json:"note"`
}

func (h *Handler) decodeOrgRequest(w http.ResponseWriter, r *http.Request) (orgWriteRequest, OrgDefinition, pgtype.UUID, pgtype.Timestamptz, bool) {
	var req orgWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, OrgDefinition{}, pgtype.UUID{}, pgtype.Timestamptz{}, false
	}
	def := decodeOrgDefinition(req.Definition)
	var owner pgtype.UUID
	if req.OwnerID != nil && *req.OwnerID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.OwnerID, "owner_id")
		if !ok {
			return req, def, owner, pgtype.Timestamptz{}, false
		}
		owner = id
	}
	var dissolve pgtype.Timestamptz
	if req.DissolveAt != nil && *req.DissolveAt != "" {
		t, err := time.Parse(time.RFC3339, *req.DissolveAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dissolve_at must be RFC3339")
			return req, def, owner, dissolve, false
		}
		dissolve = pgtype.Timestamptz{Time: t, Valid: true}
	}
	return req, def, owner, dissolve, true
}

func (h *Handler) writeOrgError(w http.ResponseWriter, err error) {
	var ve orgValidationError
	if errors.As(err, &ve) {
		writeError(w, http.StatusUnprocessableEntity, ve.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// GET /api/org
func (h *Handler) ListOrgStructures(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListOrgStructures(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list structures")
		return
	}
	out := make([]OrgStructureResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, h.orgToResponse(r.Context(), s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"structures": out})
}

// GET /api/org/resolve?project_id= : the structure in force for a project.
func (h *Handler) ResolveOrgStructure(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var projectID pgtype.UUID
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		if projectID, ok = parseUUIDOrBadRequest(w, raw, "project_id"); !ok {
			return
		}
	}
	s, found := h.orgStructureFor(r.Context(), wsUUID, projectID)
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"structure": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"structure": h.orgToResponse(r.Context(), s)})
}

// orgStructureFor is the project's live structure, else the workspace default.
func (h *Handler) orgStructureFor(ctx context.Context, wsUUID, projectID pgtype.UUID) (db.OrgStructure, bool) {
	if projectID.Valid {
		if s, err := h.Queries.GetOrgStructureForProject(ctx, db.GetOrgStructureForProjectParams{WorkspaceID: wsUUID, ProjectID: projectID}); err == nil {
			return s, true
		}
	}
	s, err := h.Queries.GetOrgStructureDefault(ctx, wsUUID)
	return s, err == nil
}

// GET /api/org/{id}
func (h *Handler) GetOrgStructure(w http.ResponseWriter, r *http.Request) {
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
	revisions, _ := h.Queries.ListOrgRevisions(r.Context(), db.ListOrgRevisionsParams{StructureID: s.ID, WorkspaceID: wsUUID})
	revs := make([]map[string]any, 0, len(revisions))
	for _, rv := range revisions {
		revs = append(revs, map[string]any{"id": uuidToString(rv.ID), "revision": rv.Revision, "model": rv.Model, "status": rv.Status, "note": rv.Note, "changed_by": uuidToPtr(rv.ChangedBy), "created_at": timestampToString(rv.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"structure": h.orgToResponse(r.Context(), s), "revisions": revs})
}

// POST /api/org
func (h *Handler) CreateOrgStructure(w http.ResponseWriter, r *http.Request) {
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
	req, def, owner, dissolve, ok := h.decodeOrgRequest(w, r)
	if !ok {
		return
	}
	var projectID pgtype.UUID
	if req.ProjectID != nil && *req.ProjectID != "" {
		pid, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: pid, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
		projectID = pid
	}
	if len(def.Units) == 0 {
		var agentIDs []string
		def = orgTemplate(req.Model, userID, agentIDs).Definition
	}
	if err := h.validateOrg(r.Context(), wsUUID, req.Model, &def); err != nil {
		h.writeOrgError(w, err)
		return
	}
	if !owner.Valid {
		owner = parseUUID(userID)
	}
	s, err := h.createOrgStructure(r.Context(), wsUUID, projectID, req.Model, req.Name, orgStatusDraft, def, owner, dissolve, req.EndCondition, req.BudgetUsdTicks, req.EvalAttestation, parseUUID(userID), req.Note)
	if err != nil {
		if strings.Contains(err.Error(), "uq_org_structure") {
			writeError(w, http.StatusConflict, "this project (or the workspace default) already has a live structure")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create structure")
		return
	}
	writeJSON(w, http.StatusCreated, h.orgToResponse(r.Context(), s))
}

func (h *Handler) createOrgStructure(ctx context.Context, wsUUID, projectID pgtype.UUID, model, name, status string, def OrgDefinition, owner pgtype.UUID, dissolve pgtype.Timestamptz, endCondition string, budget int64, eval string, createdBy pgtype.UUID, note string) (db.OrgStructure, error) {
	if strings.TrimSpace(name) == "" {
		name = orgTemplate(model, "", nil).Name
	}
	raw, _ := json.Marshal(def)
	id, revID := dbid.NewV7(), dbid.NewV7()
	s, err := h.Queries.CreateOrgStructure(ctx, db.CreateOrgStructureParams{
		ID: id, WorkspaceID: wsUUID, ProjectID: projectID, Model: model, Name: name, Status: status, RevisionID: revID, Definition: raw, OwnerID: owner,
		DissolveAt: dissolve, EndCondition: endCondition, BudgetUsdTicks: budget, EvalAttestation: eval, CreatedBy: createdBy,
	})
	if err != nil {
		return db.OrgStructure{}, err
	}
	if _, err := h.Queries.CreateOrgRevision(ctx, db.CreateOrgRevisionParams{ID: revID, WorkspaceID: wsUUID, StructureID: s.ID, Revision: 1, Model: model, Status: status, Definition: raw, ChangedBy: createdBy, Note: note}); err != nil {
		return db.OrgStructure{}, err
	}
	h.audit(ctx, wsUUID, "member", uuidToString(createdBy), AuditOrgSaved, "org_structure", s.ID, map[string]any{"model": model, "revision": 1, "status": status, "units": len(def.Units)}, nil)
	return s, nil
}

// PUT /api/org/{id}: a new revision. Status changes go through activate / pause / dissolve.
func (h *Handler) UpdateOrgStructure(w http.ResponseWriter, r *http.Request) {
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
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "structure id")
	if !ok {
		return
	}
	prev, err := h.Queries.GetOrgStructure(r.Context(), db.GetOrgStructureParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "structure not found")
		return
	}
	if prev.Status == orgStatusDissolved {
		writeError(w, http.StatusConflict, "a dissolved structure is history; create a new one")
		return
	}
	req, def, owner, dissolve, ok := h.decodeOrgRequest(w, r)
	if !ok {
		return
	}
	model := req.Model
	if model == "" {
		model = prev.Model
	}
	if len(req.Definition) == 0 {
		def = decodeOrgDefinition(prev.Definition)
	}
	if err := h.validateOrg(r.Context(), wsUUID, model, &def); err != nil {
		h.writeOrgError(w, err)
		return
	}
	if req.OwnerID == nil {
		owner = prev.OwnerID
	}
	if req.DissolveAt == nil {
		dissolve = prev.DissolveAt
	}
	eval := req.EvalAttestation
	if eval == "" {
		eval = prev.EvalAttestation
	}
	name := req.Name
	if name == "" {
		name = prev.Name
	}
	endCondition := req.EndCondition
	if endCondition == "" {
		endCondition = prev.EndCondition
	}
	budget := req.BudgetUsdTicks
	if budget == 0 {
		budget = prev.BudgetUsdTicks
	}
	if prev.Status == orgStatusActive {
		if err := h.orgActivationCheck(r.Context(), wsUUID, model, owner, dissolve, endCondition, eval, def); err != nil {
			h.writeOrgError(w, err)
			return
		}
	}
	s, err := h.saveOrgRevision(r.Context(), prev, model, name, prev.Status, def, owner, dissolve, endCondition, budget, eval, parseUUID(userID), req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save structure")
		return
	}
	writeJSON(w, http.StatusOK, h.orgToResponse(r.Context(), s))
}

func (h *Handler) saveOrgRevision(ctx context.Context, prev db.OrgStructure, model, name, status string, def OrgDefinition, owner pgtype.UUID, dissolve pgtype.Timestamptz, endCondition string, budget int64, eval string, by pgtype.UUID, note string) (db.OrgStructure, error) {
	raw, _ := json.Marshal(def)
	revID := dbid.NewV7()
	s, err := h.Queries.UpdateOrgStructure(ctx, db.UpdateOrgStructureParams{
		ID: prev.ID, WorkspaceID: prev.WorkspaceID, Model: model, Name: name, Status: status, RevisionID: revID, Definition: raw, OwnerID: owner,
		DissolveAt: dissolve, EndCondition: endCondition, BudgetUsdTicks: budget, EvalAttestation: eval,
	})
	if err != nil {
		return db.OrgStructure{}, err
	}
	if _, err := h.Queries.CreateOrgRevision(ctx, db.CreateOrgRevisionParams{ID: revID, WorkspaceID: prev.WorkspaceID, StructureID: s.ID, Revision: s.Revision, Model: model, Status: status, Definition: raw, ChangedBy: by, Note: note}); err != nil {
		return db.OrgStructure{}, err
	}
	h.audit(ctx, prev.WorkspaceID, "member", uuidToString(by), AuditOrgSaved, "org_structure", s.ID, map[string]any{"model": model, "revision": s.Revision, "status": status, "units": len(def.Units), "note": note}, nil)
	h.publish("org:updated", uuidToString(prev.WorkspaceID), "member", uuidToString(by), map[string]any{"structure_id": uuidToString(s.ID), "revision": s.Revision, "status": status})
	return s, nil
}

// POST /api/org/{id}/activate {eval_attestation} | /pause {reason} | /resume | /dissolve
func (h *Handler) SetOrgStructureStatus(w http.ResponseWriter, r *http.Request) {
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
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "structure id")
	if !ok {
		return
	}
	s, err := h.Queries.GetOrgStructure(r.Context(), db.GetOrgStructureParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "structure not found")
		return
	}
	var req struct {
		EvalAttestation string `json:"eval_attestation"`
		Reason          string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
	action := chi.URLParam(r, "action")
	var next, reason string
	switch action {
	case "activate":
		eval := req.EvalAttestation
		if eval == "" {
			eval = s.EvalAttestation
		}
		def := decodeOrgDefinition(s.Definition)
		if err := h.orgActivationCheck(r.Context(), wsUUID, s.Model, s.OwnerID, s.DissolveAt, s.EndCondition, eval, def); err != nil {
			h.writeOrgError(w, err)
			return
		}
		if eval != s.EvalAttestation {
			if s, err = h.saveOrgRevision(r.Context(), s, s.Model, s.Name, s.Status, def, s.OwnerID, s.DissolveAt, s.EndCondition, s.BudgetUsdTicks, eval, parseUUID(userID), "eval attestation"); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record the attestation")
				return
			}
		}
		next = orgStatusActive
	case "pause":
		next, reason = orgStatusPaused, truncateUTF8(strings.TrimSpace(req.Reason), 500)
	case "resume":
		next = orgStatusActive
	case "dissolve":
		next, reason = orgStatusDissolved, truncateUTF8(strings.TrimSpace(req.Reason), 500)
	default:
		writeError(w, http.StatusNotFound, "unknown action")
		return
	}
	if s.Status == orgStatusDissolved {
		writeError(w, http.StatusConflict, "a dissolved structure stays dissolved")
		return
	}
	updated, err := h.Queries.SetOrgStructureStatus(r.Context(), db.SetOrgStructureStatusParams{ID: s.ID, WorkspaceID: wsUUID, Status: next, PausedReason: reason})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change status")
		return
	}
	if next == orgStatusDissolved {
		h.dissolveOrgStructure(r.Context(), updated, "dissolved by "+userID+": "+reason)
	}
	h.audit(r.Context(), wsUUID, "member", userID, "org."+action, "org_structure", s.ID, map[string]any{"reason": reason, "status": next}, nil)
	h.publish("org:updated", uuidToString(wsUUID), "member", userID, map[string]any{"structure_id": uuidToString(s.ID), "status": next})
	writeJSON(w, http.StatusOK, h.orgToResponse(r.Context(), updated))
}

// DELETE /api/org/{id} (owner/admin): drafts and dissolved structures only.
func (h *Handler) DeleteOrgStructure(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	requester, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "workspace not found", "owner", "admin")
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
	if s.Status == orgStatusActive || s.Status == orgStatusPaused {
		writeError(w, http.StatusBadRequest, "dissolve the structure before deleting it")
		return
	}
	if err := h.Queries.DeleteOrgStructure(r.Context(), db.DeleteOrgStructureParams{ID: s.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete structure")
		return
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(requester.UserID), "org.deleted", "org_structure", s.ID, map[string]any{"model": s.Model}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- preflight: coordination cost --------------------------------------------------------

// GET /api/org/{id}/preflight: what activating this structure costs in
// coordination runs and in human review load, before anyone switches it on.
func (h *Handler) PreflightOrgStructure(w http.ResponseWriter, r *http.Request) {
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
	def := decodeOrgDefinition(s.Definition)
	tpl := orgTemplate(s.Model, "", nil)
	var avgCost int64
	agents := 0
	for _, u := range def.Units {
		for _, a := range u.memberIDs("agent") {
			if c, err := h.Queries.AvgAgentRecentTaskCostTicks(r.Context(), parseUUID(a)); err == nil {
				avgCost += c
				agents++
			}
		}
	}
	if agents > 0 {
		avgCost /= int64(agents)
	}
	// Human review load: the items a person reads per issue — escalations and
	// approvals for the models that produce them.
	reviewPerIssue := 0.0
	switch s.Model {
	case OrgModelHierarchy, OrgModelCircles:
		reviewPerIssue = 0.5
	case OrgModelMarket, OrgModelTaskforce:
		reviewPerIssue = 0.3
	default:
		reviewPerIssue = 0.1
	}
	unowned := 0
	for _, u := range def.Units {
		if u.OwnerID == "" {
			unowned++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model": s.Model, "pattern": tpl.Pattern,
		"coordination_runs_per_issue":           tpl.CoordinationRunsPerIssue,
		"coordination_cost_usd_ticks_per_issue": int64(tpl.CoordinationRunsPerIssue * float64(avgCost)),
		"human_review_items_per_issue":          reviewPerIssue,
		"human_review_seconds_per_issue":        int(reviewPerIssue * orgLLMReviewSecondsPerItem),
		"units":                                 len(def.Units), "units_without_owner": unowned, "agents": agents,
		"activation_requirements": []string{"human owner", "eval attestation (30 cases)", "termination for task force, committee and market"},
	})
}

// --- routing on new issues ----------------------------------------------------------

func (h *Handler) orgUnitPaused(ctx context.Context, s db.OrgStructure, unitID string) bool {
	n, err := h.Queries.CountOrgFlowsSince(ctx, db.CountOrgFlowsSinceParams{StructureID: s.ID, Kind: orgFlowBreaker, Since: pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}, UnitID: pgtype.Text{String: unitID, Valid: true}})
	return err == nil && n > 0
}

func (h *Handler) orgFlow(ctx context.Context, s db.OrgStructure, unitID, kind string, issueID pgtype.UUID, actorType string, actorID pgtype.UUID, details map[string]any) {
	raw, _ := json.Marshal(details)
	if err := h.Queries.CreateOrgFlow(ctx, db.CreateOrgFlowParams{ID: dbid.NewV7(), WorkspaceID: s.WorkspaceID, StructureID: s.ID, UnitID: unitID, Kind: kind, IssueID: issueID, ActorType: actorType, ActorID: actorID, Details: raw}); err != nil {
		slog.Warn("org: flow record failed", "error", err, "kind", kind)
	}
}

// orgMatchUnit picks the unit a rule routes the issue to, else the model's
// fallback. A paused unit or one without owner never receives work.
func (h *Handler) orgMatchUnit(ctx context.Context, s db.OrgStructure, def OrgDefinition, issue db.Issue) *OrgUnit {
	labels := map[string]bool{}
	if rows, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err == nil {
		for _, l := range rows {
			labels[strings.ToLower(l.Name)] = true
		}
	}
	paths := service.IssuePaths(issue.Title, issue.Description.String)
	text := strings.ToLower(issue.Title + "\n" + issue.Description.String)
	active := func(u *OrgUnit) bool {
		return u != nil && u.OwnerID != "" && !h.orgUnitPaused(ctx, s, u.ID)
	}
	var best *OrgRule
	bestSpec := -1
	for i := range def.Rules {
		r := &def.Rules[i]
		matched, spec := false, 0
		for _, l := range r.Labels {
			if labels[strings.ToLower(l)] {
				matched, spec = true, spec+3
			}
		}
		for _, k := range r.Keywords {
			if k != "" && strings.Contains(text, strings.ToLower(k)) {
				matched, spec = true, spec+2
			}
		}
		for _, p := range r.Paths {
			if g, err := compileGlob(p); err == nil && g != nil {
				for _, ip := range paths {
					if g.MatchString(ip) {
						matched, spec = true, spec+1+len(strings.ReplaceAll(p, "*", ""))/8
					}
				}
			}
			if p == "*" {
				matched = true
			}
		}
		if !matched || !active(def.unit(r.TargetUnit)) {
			continue
		}
		if best == nil || r.Priority > best.Priority || (r.Priority == best.Priority && spec > bestSpec) {
			best, bestSpec = r, spec
		}
	}
	if best != nil {
		return def.unit(best.TargetUnit)
	}
	switch s.Model {
	case OrgModelHierarchy:
		for i := range def.Units {
			isRoot := true
			for _, e := range def.Edges {
				if e.From == def.Units[i].ID && e.Kind == "reports_to" {
					isRoot = false
				}
			}
			if isRoot && active(&def.Units[i]) {
				return &def.Units[i]
			}
		}
	case OrgModelCircles:
		for i := range def.Units {
			for _, role := range def.Units[i].Roles {
				for _, k := range role.Keywords {
					if k != "" && strings.Contains(text, strings.ToLower(k)) && active(&def.Units[i]) {
						return &def.Units[i]
					}
				}
			}
		}
	case OrgModelMatrix, OrgModelTaskforce, OrgModelMarket:
		if len(def.Units) > 0 && active(&def.Units[0]) {
			return &def.Units[0]
		}
	}
	return nil
}

// orgTargetForUnit is who takes an issue routed to a unit: the unit's squad,
// its lead agent, any agent member, else its owner (a human). For the matrix
// model the most competent member for the issue's domain wins.
func (h *Handler) orgTargetForUnit(ctx context.Context, s db.OrgStructure, u *OrgUnit, issue db.Issue) (string, pgtype.UUID) {
	if u.SquadID != "" {
		return "squad", parseUUID(u.SquadID)
	}
	agents := u.memberIDs("agent")
	if s.Model == OrgModelMatrix && len(agents) > 1 {
		domain := h.issueDomainKey(ctx, issue)
		rows, _ := h.Queries.ListDomainCompetency(ctx, db.ListDomainCompetencyParams{WorkspaceID: issue.WorkspaceID, DomainKey: domain})
		bestScore, best := -1.0, ""
		for _, c := range rows {
			if containsStr(agents, uuidToString(c.AgentID)) {
				if sc := competencyScore(c.SuccessCount, c.TotalCount, c.DuelWins, c.DuelLosses); sc > bestScore {
					bestScore, best = sc, uuidToString(c.AgentID)
				}
			}
		}
		if best != "" {
			return "agent", parseUUID(best)
		}
	}
	for _, m := range u.Members {
		if m.Type == "agent" && m.Role == "lead" {
			return "agent", parseUUID(m.ID)
		}
	}
	if len(agents) > 0 {
		return "agent", parseUUID(agents[0])
	}
	if u.OwnerID != "" {
		return "member", parseUUID(u.OwnerID)
	}
	return "", pgtype.UUID{}
}

// orgAssign sets the assignee and starts the run the assignment implies.
func (h *Handler) orgAssign(ctx context.Context, issue db.Issue, assigneeType string, assigneeID pgtype.UUID, note string, by pgtype.UUID) (db.Issue, error) {
	updated, err := h.Queries.SetIssueAssigneeForPipeline(ctx, db.SetIssueAssigneeForPipelineParams{ID: issue.ID, AssigneeType: pgtype.Text{String: assigneeType, Valid: true}, AssigneeID: assigneeID})
	if err != nil {
		return issue, err
	}
	switch assigneeType {
	case "agent":
		if _, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, updated, note, by); err != nil {
			slog.Warn("org: enqueue after routing failed", "error", err, "issue_id", uuidToString(issue.ID))
		}
	case "squad":
		if squad, err := h.Queries.GetSquad(ctx, assigneeID); err == nil {
			if _, err := h.TaskService.EnqueueTaskForSquadLeaderWithHandoff(ctx, updated, squad.LeaderID, squad.ID, note, by); err != nil {
				slog.Warn("org: enqueue squad leader after routing failed", "error", err, "issue_id", uuidToString(issue.ID))
			}
		}
	}
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(updated, prefix)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue": resp, "assignee_changed": true})
	return updated, nil
}

// orgRouteIssue runs when an issue is created: the structure in force picks
// a unit and the unit picks who takes it. Never fails the create.
func (h *Handler) orgRouteIssue(ctx context.Context, issue db.Issue, actorType, actorID string) db.Issue {
	if issue.AssigneeID.Valid {
		return issue
	}
	s, found := h.orgStructureFor(ctx, issue.WorkspaceID, issue.ProjectID)
	if !found || s.Status != orgStatusActive {
		return issue
	}
	def := decodeOrgDefinition(s.Definition)
	unit := h.orgMatchUnit(ctx, s, def, issue)
	actor := pgtype.UUID{}
	if actorType == "member" {
		actor = parseUUID(actorID)
	}
	if unit == nil {
		h.orgFlow(ctx, s, "", orgFlowUnrouted, issue.ID, actorType, actor, map[string]any{"reason": "no rule matched and the model has no fallback unit"})
		return issue
	}
	if s.Model == OrgModelMarket {
		return h.orgMarketRound(ctx, s, def, unit, issue, actor)
	}
	targetType, targetID := h.orgTargetForUnit(ctx, s, unit, issue)
	if targetType == "" {
		h.orgFlow(ctx, s, unit.ID, orgFlowUnrouted, issue.ID, actorType, actor, map[string]any{"reason": "unit has nobody to take the issue"})
		return issue
	}
	// Hierarchy: above the risk threshold, the superior approves before the unit takes it.
	if s.Model == OrgModelHierarchy && unit.ApprovalRisk != "" && issue.ContractRisk == unit.ApprovalRisk {
		if superior := orgSuperior(def, unit.ID); superior != nil && superior.OwnerID != "" {
			h.orgAskApproval(ctx, s, unit, superior, issue, targetType, targetID)
			return issue
		}
	}
	note := fmt.Sprintf("Routed by the %s structure %q to unit %q.", s.Model, s.Name, unit.Name)
	updated, err := h.orgAssign(ctx, issue, targetType, targetID, note, actor)
	if err != nil {
		slog.Warn("org: assign failed", "error", err, "issue_id", uuidToString(issue.ID))
		return issue
	}
	h.orgFlow(ctx, s, unit.ID, orgFlowRouting, issue.ID, actorType, actor, map[string]any{"assignee_type": targetType, "assignee_id": uuidToString(targetID)})
	h.audit(ctx, issue.WorkspaceID, "system", "", AuditOrgRouted, "issue", issue.ID, map[string]any{"structure_id": uuidToString(s.ID), "revision_id": uuidToString(s.RevisionID), "unit": unit.ID, "assignee_type": targetType, "assignee_id": uuidToString(targetID)}, nil)
	return updated
}

func orgSuperior(def OrgDefinition, unitID string) *OrgUnit {
	for _, e := range def.Edges {
		if e.From == unitID && e.Kind == "reports_to" {
			return def.unit(e.To)
		}
	}
	return nil
}

// orgAskApproval files the superior's decision; the answer assigns or holds.
func (h *Handler) orgAskApproval(ctx context.Context, s db.OrgStructure, unit, superior *OrgUnit, issue db.Issue, targetType string, targetID pgtype.UUID) {
	question := fmt.Sprintf("Hierarchy · %q is about to take %q (risk %s). Approve the assignment?", unit.Name, truncate(issue.Title, 120), issue.ContractRisk)
	options, _ := json.Marshal([]DecisionOption{
		{ID: orgAssignOptionPrefix + targetType + ":" + uuidToString(targetID) + ":" + unit.ID, Label: "Approve", Impact: "the unit takes the issue and its run starts"},
		{ID: orgHoldOption, Label: "Hold", Impact: "the issue stays unassigned for a human to route"},
	})
	decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AskedByType: "member", AskedByID: parseUUID(superior.OwnerID),
		Question: question, Options: options, Urgency: "high", SlaDeadlineAt: h.decisionDeadline(ctx, issue.WorkspaceID),
	})
	if err != nil {
		slog.Warn("org: approval decision failed", "error", err)
		return
	}
	h.orgFlow(ctx, s, unit.ID, "approval_asked", issue.ID, "system", pgtype.UUID{}, map[string]any{"decision_id": uuidToString(decision.ID), "superior": superior.ID})
	h.notifyDecisionRequested(ctx, issue, decision, "member", superior.OwnerID)
}

// applyOrgForDecision settles a hierarchy approval. False when the decision is not one.
func (h *Handler) applyOrgForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	var options []DecisionOption
	if err := json.Unmarshal(decision.Options, &options); err != nil {
		return false
	}
	isOrg := false
	for _, o := range options {
		if strings.HasPrefix(o.ID, orgAssignOptionPrefix) {
			isOrg = true
		}
	}
	if !isOrg {
		return false
	}
	if !strings.HasPrefix(optionID, orgAssignOptionPrefix) {
		return true
	}
	parts := strings.SplitN(strings.TrimPrefix(optionID, orgAssignOptionPrefix), ":", 3)
	if len(parts) != 3 {
		return true
	}
	targetID, err := util.ParseUUID(parts[1])
	if err != nil {
		return true
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: decision.IssueID, WorkspaceID: decision.WorkspaceID})
	if err != nil {
		return true
	}
	by := pgtype.UUID{}
	if actorType == "member" {
		by = parseUUID(actorID)
	}
	if _, err := h.orgAssign(ctx, issue, parts[0], targetID, "Assignment approved by the superior.", by); err != nil {
		slog.Warn("org: approved assignment failed", "error", err)
		return true
	}
	if s, found := h.orgStructureFor(ctx, issue.WorkspaceID, issue.ProjectID); found {
		h.orgFlow(ctx, s, parts[2], orgFlowRouting, issue.ID, actorType, by, map[string]any{"assignee_type": parts[0], "assignee_id": parts[1], "approved_by": actorID})
	}
	return true
}

// orgMarketRound: every eligible agent of the unit makes an offer from its
// measured signals (competence, recent cost, queue); the best offer under
// the human price cap wins; fewer than min_offers leaves the issue to the owner.
func (h *Handler) orgMarketRound(ctx context.Context, s db.OrgStructure, def OrgDefinition, unit *OrgUnit, issue db.Issue, actor pgtype.UUID) db.Issue {
	agents := unit.memberIDs("agent")
	if len(agents) == 0 {
		if rows, err := h.Queries.ListAgents(ctx, issue.WorkspaceID); err == nil {
			for _, a := range rows {
				agents = append(agents, uuidToString(a.ID))
			}
		}
	}
	domain := h.issueDomainKey(ctx, issue)
	competence := map[string]float64{}
	if rows, err := h.Queries.ListDomainCompetency(ctx, db.ListDomainCompetencyParams{WorkspaceID: issue.WorkspaceID, DomainKey: domain}); err == nil {
		for _, c := range rows {
			competence[uuidToString(c.AgentID)] = competencyScore(c.SuccessCount, c.TotalCount, c.DuelWins, c.DuelLosses)
		}
	}
	since := pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
	var offers []db.OrgOffer
	for _, a := range agents {
		agentID := parseUUID(a)
		// An archived agent bids on nothing; a quota-spent one waits for tomorrow.
		if agent, err := h.Queries.GetAgent(ctx, agentID); err != nil || agent.ArchivedAt.Valid {
			continue
		}
		if n, err := h.Queries.CountAgentOffersSince(ctx, db.CountAgentOffersSinceParams{AgentID: agentID, Since: since}); err == nil && n >= int64(def.Market.OffersPerAgentPerDay) {
			continue
		}
		cost, _ := h.Queries.AvgAgentRecentTaskCostTicks(ctx, agentID)
		open, _ := h.Queries.CountAgentOpenTasks(ctx, agentID)
		conf := competence[a]
		if conf == 0 {
			conf = 0.5
		}
		status := "pending"
		if cost > def.Market.PriceCapUsdTicks {
			status = "over_cap"
		}
		row, err := h.Queries.CreateOrgOffer(ctx, db.CreateOrgOfferParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, StructureID: s.ID, IssueID: issue.ID, AgentID: agentID, Confidence: conf, CostUsdTicks: cost, EtaHours: float64(open+1) * 2, Status: status})
		if err == nil {
			offers = append(offers, row)
		}
	}
	var eligible []db.OrgOffer
	for _, o := range offers {
		if o.Status == "pending" {
			eligible = append(eligible, o)
		}
	}
	if len(eligible) < def.Market.MinOffers {
		h.orgFlow(ctx, s, unit.ID, orgFlowMarketShort, issue.ID, "system", pgtype.UUID{}, map[string]any{"offers": len(eligible), "min_offers": def.Market.MinOffers})
		h.orgAlert(ctx, s, unit.OwnerID, "Market: not enough offers on "+truncate(issue.Title, 80), fmt.Sprintf("%d offer(s) under the cap, %d needed. Route it yourself or raise the cap.", len(eligible), def.Market.MinOffers), issue.ID, map[string]any{"unit": unit.ID})
		return issue
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.CostUsdTicks != b.CostUsdTicks {
			return a.CostUsdTicks < b.CostUsdTicks
		}
		return a.EtaHours < b.EtaHours
	})
	winner := eligible[0]
	for _, o := range eligible {
		status := "lost"
		if o.ID == winner.ID {
			status = "won"
		}
		if err := h.Queries.SetOrgOfferStatus(ctx, db.SetOrgOfferStatusParams{ID: o.ID, Status: status}); err != nil {
			slog.Warn("org: settle offer failed", "error", err)
		}
	}
	note := fmt.Sprintf("Won on the internal market of %q: confidence %.2f, estimated cost %d ticks, %d other offer(s).", s.Name, winner.Confidence, winner.CostUsdTicks, len(eligible)-1)
	updated, err := h.orgAssign(ctx, issue, "agent", winner.AgentID, note, actor)
	if err != nil {
		slog.Warn("org: market assign failed", "error", err)
		return issue
	}
	h.orgFlow(ctx, s, unit.ID, orgFlowRouting, issue.ID, "system", pgtype.UUID{}, map[string]any{"assignee_type": "agent", "assignee_id": uuidToString(winner.AgentID), "offers": len(eligible), "market": true})
	h.audit(ctx, issue.WorkspaceID, "system", "", AuditOrgRouted, "issue", issue.ID, map[string]any{"structure_id": uuidToString(s.ID), "revision_id": uuidToString(s.RevisionID), "unit": unit.ID, "market_winner": uuidToString(winner.AgentID), "offers": len(eligible)}, nil)
	return updated
}

func (h *Handler) orgAlert(ctx context.Context, s db.OrgStructure, ownerID, title, body string, issueID pgtype.UUID, details map[string]any) {
	recipient := s.OwnerID
	if ownerID != "" {
		recipient = parseUUID(ownerID)
	}
	if !recipient.Valid {
		return
	}
	details["structure_id"] = uuidToString(s.ID)
	raw, _ := json.Marshal(details)
	item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID: dbid.NewV7(), WorkspaceID: s.WorkspaceID, RecipientType: "member", RecipientID: recipient, Type: InboxTypeOrgAlert, Severity: "action_required",
		IssueID: issueID, Title: truncate(title, 120), Body: pgtype.Text{String: truncate(body, 1000), Valid: true}, Details: raw,
	})
	if err != nil {
		slog.Warn("org: inbox failed", "error", err)
		return
	}
	h.publish(protocol.EventInboxNew, uuidToString(s.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
}
