package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Catalog entries use the same portable bundle and import preview as user exports.
// Nothing is activated by downloading or importing a team.
type orgTeamTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
	Procedure   string   `json:"procedure"`
}

func orgTeamCatalog() []orgTeamTemplate {
	return []orgTeamTemplate{
		{"support", "Customer support", "Triage requests, draft helpful replies and review sensitive cases.", []string{"Support coordinator", "Support specialist", "Quality reviewer"}, "Classify urgency and customer need. Check available facts. Draft a concise reply. Escalate refunds, account access and commitments to the human owner. Never send an external reply without approval."},
		{"product", "Product & engineering", "Turn a product brief into implementation and reviewed delivery.", []string{"Product lead", "Engineer", "QA reviewer"}, "Clarify acceptance criteria. Break work into bounded tasks. Implement the smallest correct change. Run relevant checks. Review evidence before accepting delivery. Escalate ambiguous requirements and deployment decisions."},
		{"agency", "Content agency", "Plan, draft and review content with a consistent editorial voice.", []string{"Editorial lead", "Content writer", "Fact checker"}, "Confirm audience, channel and purpose. Draft with concrete examples. Verify factual claims against primary sources. Review tone and attribution. Do not publish or contact external parties without approval."},
		{"finance", "Operations & finance", "Prepare decision briefs and check supporting evidence.", []string{"Operations lead", "Analyst", "Control reviewer"}, "State the decision and period covered. Reconcile source figures. Document assumptions and missing evidence. Present alternatives with uncertainties. Do not move money, change accounts or make commitments; request human approval."},
		{"research", "Research desk", "Investigate a question, cross-check evidence and synthesize findings.", []string{"Research lead", "Researcher", "Evidence reviewer"}, "Define a bounded research question. Prefer primary sources. Record dates and provenance. Separate observations from inferences. Cross-check contradictory evidence. Deliver a concise synthesis with unresolved questions."},
	}
}

func orgTeamBundle(team orgTeamTemplate) *transferBundle {
	b := &transferBundle{Manifest: transferManifest{FormatVersion: transferFormatVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339), Name: team.Name, Template: true, Counts: map[string]int{"agents": len(team.Roles), "skills": 1, "projects": 1, "org_structures": 1, "autopilots": 1, "issues": 1}, Secrets: []transferSecret{}}}
	b.Manifest.Source.Name = "Vigil team catalog"
	skillName := "team-" + team.ID + "-procedure"
	b.Skills = []transferSkill{{Name: skillName, Description: team.Description, Content: team.Procedure, Status: "published", Config: json.RawMessage(`{}`), Files: []transferFile{}}}
	b.Projects = []transferProject{{Title: team.Name, Description: team.Description, Status: "planned", Priority: "medium"}}
	def := OrgDefinition{Units: []OrgUnit{}, Edges: []OrgEdge{}, Rules: []OrgRule{}, Committees: []OrgCommittee{}}
	unit := OrgUnit{ID: "team", Name: team.Name, Kind: "unit", Mission: team.Description, Autonomy: "draft", Excludes: []string{"external_effects"}, Allow: []string{"read", "comment", "propose_plan"}, Deny: []string{}, EscalationQuotaPerDay: 5, Members: []OrgMember{}, Roles: []OrgRole{}}
	for i, role := range team.Roles {
		name := team.Name + " · " + role
		b.Agents = append(b.Agents, transferAgent{Name: name, Description: role, Instructions: "Your role: " + role + ". " + team.Procedure, TrustMode: "propose", EffectMode: "preview", RuntimeMode: "local", Visibility: "workspace", MaxConcurrentTasks: 1, ConversationStarters: json.RawMessage(`[]`), CustomArgs: json.RawMessage(`[]`), Skills: []string{skillName}})
		id := []string{"lead", "delivery", "review"}[i]
		member := OrgMember{Type: "agent", ID: "agent:" + name, RoleID: id}
		if i == 0 {
			member.Role = "lead"
		}
		unit.Members = append(unit.Members, member)
		unit.Roles = append(unit.Roles, OrgRole{ID: id, Name: role, Responsibilities: team.Description})
	}
	def.Units = []OrgUnit{unit}
	def.Rules = []OrgRule{{ID: "default", TargetUnit: "team", Paths: []string{"*"}}}
	raw, _ := json.Marshal(def)
	b.Org = []transferOrg{{Project: team.Name, Model: OrgModelHierarchy, Name: team.Name, Definition: raw}}
	b.Autopilots = []transferAutopilot{{Title: team.Name + " · Weekly review", Description: "Review progress, blockers and quality. " + team.Procedure, AssigneeType: "agent", AssigneeAgent: b.Agents[0].Name, ExecutionMode: "create_issue", IssueTitleTemplate: team.Name + " · Weekly review", Project: team.Name, Triggers: []transferTrigger{{Kind: "schedule", Cron: "0 9 * * 1", Timezone: "UTC"}}}}
	b.Issues = []transferIssue{{Title: team.Name + " · Define the first outcome", Description: "Define a concrete first outcome, its acceptance criteria and the available source material. Assign each role a bounded responsibility before activating the organization.", Project: team.Name, Status: "backlog", Priority: "medium"}}
	return b
}

func (h *Handler) ListOrgTeamTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireWorkspaceMember(w, r, h.resolveWorkspaceID(r), "workspace not found"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": orgTeamCatalog()})
}

func (h *Handler) DownloadOrgTeamTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireWorkspaceRole(w, r, h.resolveWorkspaceID(r), "workspace not found", "owner", "admin"); !ok {
		return
	}
	for _, team := range orgTeamCatalog() {
		if team.ID != chi.URLParam(r, "templateID") {
			continue
		}
		data, err := zipTransferBundle(orgTeamBundle(team))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not prepare team")
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+team.ID+`.zip"`)
		if _, err := w.Write(data); err != nil {
			slog.Warn("download org team template: write response failed", "template_id", team.ID, "error", err)
		}
		return
	}
	writeError(w, http.StatusNotFound, "team template not found")
}
