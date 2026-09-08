package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// Simulating a request against an organisation (K75).
//
// A structure is data that routes, and until now the only way to learn where
// a request would land was to file it and watch. That makes every edit to a
// draft a bet: you save, you activate, and the next real issue tells you
// whether the rule you wrote does what you meant. POST /api/org/simulate
// answers the same question with no consequence — the same matching, the
// same target, the same escalation ladder the live path uses, run against a
// draft definition or against a revision already in force, and writing
// nothing: no assignment, no org_flow row, no queued run.

const orgSimulationWindow = 30 * 24 * time.Hour

type orgSimulateRequest struct {
	Model       string          `json:"model"`
	Definition  json.RawMessage `json:"definition"`
	StructureID string          `json:"structure_id"`
	Request     struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Keywords    []string `json:"keywords"`
		Labels      []string `json:"labels"`
	} `json:"request"`
}

type OrgSimulationUnit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Model    string `json:"model"`
	Autonomy string `json:"autonomy"`
}

type OrgSimulationRef struct {
	UnitID   string `json:"unit_id"`
	UnitName string `json:"unit_name"`
}

// OrgSimulationActor is who prepares or who decides. Kind is "agent",
// "member", "squad" (a unit that lends its work to a squad) or "none".
type OrgSimulationActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type OrgSimulation struct {
	Basis                string             `json:"basis"`
	StructureID          string             `json:"structure_id"`
	Revision             int32              `json:"revision"`
	Unit                 *OrgSimulationUnit `json:"unit"`
	Receives             *OrgSimulationRef  `json:"receives"`
	Prepares             OrgSimulationActor `json:"prepares"`
	Decides              OrgSimulationActor `json:"decides"`
	EscalationPath       []OrgSimulationRef `json:"escalation_path"`
	BlockingDenies       []string           `json:"blocking_denies"`
	CostEstimateUsdTicks int64              `json:"cost_estimate_usd_ticks"`
	Notes                []string           `json:"notes"`
}

// POST /api/org/simulate
func (h *Handler) SimulateOrgRequest(w http.ResponseWriter, r *http.Request) {
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
	var req orgSimulateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The structure, when one is named: the tenant guard is the workspace
	// filter of the query itself, so another workspace's id reads as absent.
	var structure db.OrgStructure
	haveStructure := false
	if req.StructureID != "" {
		id, ok := parseUUIDOrBadRequest(w, req.StructureID, "structure_id")
		if !ok {
			return
		}
		s, err := h.Queries.GetOrgStructure(r.Context(), db.GetOrgStructureParams{ID: id, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "structure not found")
			return
		}
		structure, haveStructure = s, true
	}

	out := OrgSimulation{Basis: "revision", EscalationPath: []OrgSimulationRef{}, BlockingDenies: []string{}, Notes: []string{}}
	var def OrgDefinition
	model := req.Model
	switch {
	case len(req.Definition) > 0 && !isJSONNull(req.Definition):
		// A draft wins over the revision in force: this endpoint exists to
		// answer "what would this edit change?".
		def, out.Basis = decodeOrgDefinition(req.Definition), "draft"
		if model == "" && haveStructure {
			model = structure.Model
		}
	case haveStructure:
		def, model = decodeOrgDefinition(structure.Definition), structure.Model
	default:
		writeError(w, http.StatusBadRequest, "give a definition to simulate, a structure_id, or both")
		return
	}
	if haveStructure {
		out.StructureID, out.Revision = uuidToString(structure.ID), structure.Revision
	}

	// The same validation the save runs, so a simulation never reports on a
	// definition the product would refuse — including the non-negotiable
	// denials it merges into every unit.
	if err := h.validateOrg(r.Context(), wsUUID, model, &def); err != nil {
		h.writeOrgError(w, err)
		return
	}

	labels := req.Request.Labels
	issue := db.Issue{
		ID:          dbid.NewV7(), // no row: label and task lookups come back empty, as they should
		WorkspaceID: wsUUID,
		ProjectID:   structure.ProjectID,
		Title:       strings.TrimSpace(req.Request.Title),
	}
	// Keywords ride in the description because that is where the live
	// matcher reads free text from; nothing else distinguishes them.
	body := strings.TrimSpace(strings.Join(append([]string{req.Request.Description}, req.Request.Keywords...), "\n"))
	issue.Description = pgtype.Text{String: body, Valid: body != ""}

	// A structure value the matcher can read: the real one when there is one,
	// so a paused unit still refuses work, with the simulated model on top.
	sim := structure
	sim.WorkspaceID, sim.Model = wsUUID, model

	unit := h.orgMatchUnitWith(r.Context(), sim, def, issue, labels)
	if unit == nil {
		out.Prepares, out.Decides = OrgSimulationActor{Kind: "none"}, OrgSimulationActor{Kind: "none"}
		out.Notes = append(out.Notes, "no rule matched and the model has no fallback unit")
		for _, u := range def.Units {
			if u.OwnerID == "" {
				out.Notes = append(out.Notes, "unit "+u.Name+" has no human owner, so it never receives work")
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	unitModel := orgEffectiveModel(&def, unit.ID, model)
	out.Unit = &OrgSimulationUnit{ID: unit.ID, Name: unit.Name, Model: unitModel, Autonomy: unit.Autonomy}
	out.Receives = &OrgSimulationRef{UnitID: unit.ID, UnitName: unit.Name}

	targetType, targetID := h.orgTargetForUnitWith(r.Context(), unitModel, unit, issue, labels)
	out.Prepares = h.orgSimulationActor(r.Context(), targetType, targetID)
	if unitModel == OrgModelMarket {
		out.Notes = append(out.Notes, "this unit runs an internal market: the run goes to the best offer under the price cap, which a simulation does not stage")
	}
	if unit.ApprovalRisk != "" {
		out.Notes = append(out.Notes, "the unit's superior approves first when the request's contract risk is "+unit.ApprovalRisk)
	}

	for _, up := range orgEscalationChain(&def, unit) {
		out.EscalationPath = append(out.EscalationPath, OrgSimulationRef{UnitID: up.ID, UnitName: up.Name})
	}

	deciderKind, deciderID := orgDeciderFor(&def, unit)
	if deciderKind == "" && haveStructure && structure.OwnerID.Valid {
		deciderKind, deciderID = "member", uuidToString(structure.OwnerID)
	}
	if deciderKind == "" && unit.OwnerID != "" {
		deciderKind, deciderID = "member", unit.OwnerID
	}
	out.Decides = h.orgSimulationActor(r.Context(), deciderKind, orgSimUUID(deciderID))

	out.BlockingDenies = orgBlockingDenies(unit.Deny, issue.Title+"\n"+body)
	if haveStructure {
		out.CostEstimateUsdTicks = h.orgUnitCostPerRun(r.Context(), structure, unit.ID)
	}
	if out.CostEstimateUsdTicks == 0 {
		out.Notes = append(out.Notes, "no spend observed for this unit over the last 30 days")
	}
	writeJSON(w, http.StatusOK, out)
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// orgSimUUID reads an id out of a definition. validateOrg only checks the
// deciders of a unit with external effects, so an id from anywhere else is
// request input: it must not reach parseUUID, which panics.
func orgSimUUID(id string) pgtype.UUID {
	parsed, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

// orgDeciderFor is who answers for a unit: its own decider, else the nearest
// superior's along reports_to. Empty when the ladder names nobody.
func orgDeciderFor(d *OrgDefinition, unit *OrgUnit) (string, string) {
	seen := map[string]bool{}
	for cur := unit; cur != nil && !seen[cur.ID]; cur = d.parent(cur.ID) {
		seen[cur.ID] = true
		for _, class := range orgDecisionClasses {
			if id := cur.Deciders[class]; id != "" {
				return "member", id
			}
		}
	}
	return "", ""
}

// orgBlockingDenies names the unit's refusals the request runs into: the
// non-negotiable verbs matched the way mcpgov matches them against a tool,
// plus any verb the unit added, matched literally.
func orgBlockingDenies(deny []string, text string) []string {
	lower := strings.ToLower(text)
	words := orgSimWords(lower)
	out := []string{}
	for _, verb := range deny {
		v := strings.ToLower(strings.TrimSpace(verb))
		if v == "" {
			continue
		}
		if mcpgov.OrgDenyClass(verb, lower, "") != "" || strings.Contains(lower, v) || orgDenyTouches(v, words) {
			out = append(out, verb)
		}
	}
	return out
}

// orgDenyTouches matches a refusal against the request by word stem: a unit
// that refuses "rembourser" must be flagged on "demande le remboursement", and
// "envoyer e-mail externe" on "envoyer un email à un client externe". Every
// significant word of the refusal (four letters or more) must share a stem
// with some word of the request. A stem is the first five letters, or the
// whole word when shorter — crude, but it errs towards flagging, which is the
// right side for a preview whose job is to warn.
func orgDenyTouches(deny string, words []string) bool {
	significant := 0
	for _, dw := range orgSimWords(deny) {
		if len([]rune(dw)) < 4 {
			continue
		}
		significant++
		found := false
		ds := orgSimStem(dw)
		for _, w := range words {
			if orgSimStem(w) == ds {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return significant > 0
}

func orgSimWords(text string) []string {
	text = strings.ReplaceAll(strings.ToLower(text), "-", "")
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x00C0 && r <= 0x024F)
	})
}

func orgSimStem(word string) string {
	r := []rune(word)
	if len(r) > 5 {
		r = r[:5]
	}
	return string(r)
}

// orgUnitCostPerRun is what one routed issue cost this unit on average over
// the window, zero when it routed nothing.
func (h *Handler) orgUnitCostPerRun(ctx context.Context, s db.OrgStructure, unitID string) int64 {
	since := pgtype.Timestamptz{Time: time.Now().Add(-orgSimulationWindow), Valid: true}
	routed, err := h.Queries.CountOrgFlowsSince(ctx, db.CountOrgFlowsSinceParams{StructureID: s.ID, Kind: orgFlowRouting, Since: since, UnitID: pgtype.Text{String: unitID, Valid: true}})
	if err != nil || routed <= 0 {
		return 0
	}
	spend, err := h.Queries.SumOrgUnitSpendSince(ctx, db.SumOrgUnitSpendSinceParams{StructureID: s.ID, UnitID: unitID, Since: since})
	if err != nil {
		return 0
	}
	return spend / routed
}

// orgSimulationActor names an agent, a workspace member or a squad.
func (h *Handler) orgSimulationActor(ctx context.Context, kind string, id pgtype.UUID) OrgSimulationActor {
	if kind == "" || !id.Valid {
		return OrgSimulationActor{Kind: "none"}
	}
	out := OrgSimulationActor{Kind: kind, ID: uuidToString(id)}
	switch kind {
	case "agent":
		if a, err := h.Queries.GetAgent(ctx, id); err == nil {
			out.Name = a.Name
		}
	case "member":
		if u, err := h.Queries.GetUser(ctx, id); err == nil {
			out.Name = u.Name
		}
	case "squad":
		if sq, err := h.Queries.GetSquad(ctx, id); err == nil {
			out.Name = sq.Name
		}
	}
	return out
}
