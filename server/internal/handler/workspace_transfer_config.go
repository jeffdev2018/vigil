package handler

// Transfer format 2 (packs, OS plan vague B): the workspace configuration
// kinds the original bundle did not carry — statuses, work item types,
// labels, custom properties, saved views, transition rules, business rules,
// ownership rules and the doctrine. Same pipeline as the rest: collisions in
// the preview, one strategy, one transaction, every row in the report ledger.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// transferRefs carries the ids the later legs resolve by name.
type transferRefs struct {
	labels     map[string]pgtype.UUID // issue labels by lower-cased name
	statuses   map[string]bool        // status keys present after the config leg
	types      map[string]bool        // type keys present after the config leg
	properties map[string]transferPropertyRef
	agents     map[string]pgtype.UUID
	projects   map[string]pgtype.UUID
}

// transferPropertyRef lets a pack's saved view name a property and its
// options instead of the ids a workspace only has once they exist.
type transferPropertyRef struct {
	id      pgtype.UUID
	options map[string]string // lower-cased option name → option id
}

func propertyRefOf(id pgtype.UUID, config []byte) transferPropertyRef {
	ref := transferPropertyRef{id: id, options: map[string]string{}}
	var cfg PropertyConfig
	if json.Unmarshal(config, &cfg) == nil {
		for _, o := range cfg.Options {
			ref.options[strings.ToLower(o.Name)] = o.ID
		}
	}
	return ref
}

// rewriteViewQuery replaces property names and option names in a view's
// propertyFilters with the ids the workspace now has. Keys and values that
// already are ids, or that name nothing, are left alone.
func rewriteViewQuery(raw json.RawMessage, refs *transferRefs) json.RawMessage {
	if len(raw) == 0 || refs == nil || len(refs.properties) == 0 {
		return raw
	}
	var query map[string]any
	if json.Unmarshal(raw, &query) != nil {
		return raw
	}
	filters, ok := query["propertyFilters"].(map[string]any)
	if !ok {
		return raw
	}
	rewritten := map[string]any{}
	for key, value := range filters {
		ref, named := refs.properties[strings.ToLower(key)]
		if !named {
			rewritten[key] = value
			continue
		}
		values, isList := value.([]any)
		if isList {
			mapped := make([]any, 0, len(values))
			for _, v := range values {
				if name, isString := v.(string); isString {
					if id, found := ref.options[strings.ToLower(name)]; found {
						mapped = append(mapped, id)
						continue
					}
				}
				mapped = append(mapped, v)
			}
			value = mapped
		}
		rewritten[uuidToString(ref.id)] = value
	}
	query["propertyFilters"] = rewritten
	out, err := json.Marshal(query)
	if err != nil {
		return raw
	}
	return out
}

func newTransferBundle() *transferBundle {
	return &transferBundle{
		Profiles: []transferProfile{}, Skills: []transferSkill{}, Agents: []transferAgent{}, Projects: []transferProject{}, Goals: []transferGoal{}, Autopilots: []transferAutopilot{}, TriageSources: []transferTriageSource{}, Org: []transferOrg{}, Notes: []transferNote{}, Issues: []transferIssue{},
		IssueStatuses: []transferIssueStatus{}, IssueTypes: []transferIssueType{}, Labels: []transferLabel{}, Properties: []transferProperty{}, Views: []transferView{}, TransitionRules: []transferTransition{}, BusinessRules: []transferBusinessRule{}, OwnershipRules: []transferOwnershipRule{},
	}
}

func transferCounts(b *transferBundle) map[string]int {
	counts := map[string]int{"agents": len(b.Agents), "skills": len(b.Skills), "permission_profiles": len(b.Profiles), "projects": len(b.Projects), "goals": len(b.Goals), "autopilots": len(b.Autopilots), "triage_sources": len(b.TriageSources), "org_structures": len(b.Org), "notes": len(b.Notes), "issues": len(b.Issues)}
	for k, n := range map[string]int{"issue_statuses": len(b.IssueStatuses), "issue_types": len(b.IssueTypes), "labels": len(b.Labels), "properties": len(b.Properties), "views": len(b.Views), "transition_rules": len(b.TransitionRules), "business_rules": len(b.BusinessRules), "ownership_rules": len(b.OwnershipRules)} {
		if n > 0 {
			counts[k] = n
		}
	}
	if strings.TrimSpace(b.Doctrine) != "" {
		counts["doctrine"] = 1
	}
	return counts
}

func statusKeyOf(s transferIssueStatus) string {
	if s.Key != "" {
		return strings.ToLower(strings.TrimSpace(s.Key))
	}
	return ""
}

func typeKeyOf(t transferIssueType) string {
	if t.Key != "" {
		return strings.ToLower(strings.TrimSpace(t.Key))
	}
	return slugifyIssueTypeName(t.Name)
}

func transitionRuleName(t transferTransition) string {
	from := t.FromCategory
	if from == "" {
		from = "any"
	}
	name := from + " → " + t.ToCategory
	if t.Project != "" {
		name = t.Project + ": " + name
	}
	return name
}

func ownershipRuleName(o transferOwnershipRule) string {
	if o.Label != "" {
		return "label " + o.Label
	}
	return "path " + o.PathPattern
}

// --- validation --------------------------------------------------------------------

var (
	validPropertyTypesForImport = []string{"text", "url", "number", "checkbox", "date", "select", "multi_select", "actor", "multi_actor"}
	validAttachPointsForImport  = []string{"project_create", "issue_submit_review", "agent_run_dispatch", "webhook_received"}
	validTrustModesForImport    = []string{"", "observer", "propose", "approval", "autonomous"}
	validActorTypesForImport    = []string{"member", "agent"}
	validViewScopesForImport    = []string{"workspace", "project"}
)

// validateTransferBundle reports every enum and cross-reference problem a
// bundle carries, so a pack author (or an upload) hears all of them at once.
func validateTransferBundle(b *transferBundle) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	statusKeys := map[string]bool{}
	for _, k := range issuestatus.Canonical() {
		statusKeys[k] = true
	}
	for i, s := range b.IssueStatuses {
		if strings.TrimSpace(s.Name) == "" {
			add("issue_statuses[%d]: name is required", i)
		}
		if !issuestatus.IsCategory(s.Category) {
			add("issue_statuses[%d] %q: category must be one of %s", i, s.Name, strings.Join(issuestatus.Canonical(), ", "))
		}
		if _, err := normalizeColor(s.Color); err != nil {
			add("issue_statuses[%d] %q: %v", i, s.Name, err)
		}
		if key := statusKeyOf(s); key != "" {
			if issuestatus.IsBuiltIn(key) {
				add("issue_statuses[%d] %q: key %q is built in", i, s.Name, key)
			}
			statusKeys[key] = true
		}
	}
	typeKeys := map[string]bool{}
	for i, t := range b.IssueTypes {
		if strings.TrimSpace(t.Name) == "" {
			add("issue_types[%d]: name is required", i)
		}
		key := typeKeyOf(t)
		if key == "" {
			add("issue_types[%d] %q: a key is required (the name has no key characters)", i, t.Name)
		} else if _, err := validateIssueTypeKey(key); err != nil {
			add("issue_types[%d] %q: %v", i, t.Name, err)
		}
		if _, err := normalizeColor(t.Color); err != nil {
			add("issue_types[%d] %q: %v", i, t.Name, err)
		}
		typeKeys[key] = true
	}
	labelNames := map[string]bool{}
	for i, l := range b.Labels {
		if _, err := validateLabelName(l.Name); err != nil {
			add("labels[%d]: %v", i, err)
		}
		if _, err := normalizeColor(l.Color); err != nil {
			add("labels[%d] %q: %v", i, l.Name, err)
		}
		if l.ResourceType != "" && l.ResourceType != "issue" && l.ResourceType != "project" {
			add("labels[%d] %q: resource_type must be issue or project", i, l.Name)
		}
		labelNames[strings.ToLower(l.Name)] = true
	}
	for i, p := range b.Properties {
		if _, err := validatePropertyName(p.Name); err != nil {
			add("properties[%d]: %v", i, err)
		}
		if _, err := validatePropertyIcon(p.Icon); err != nil {
			add("properties[%d] %q: %v (supported: %s)", i, p.Name, err, strings.Join(propertyIconKeys(), ", "))
		}
		if !containsStr(validPropertyTypesForImport, p.Type) {
			add("properties[%d] %q: type must be one of %s", i, p.Name, strings.Join(validPropertyTypesForImport, ", "))
		} else {
			var cfg *PropertyConfig
			if len(p.Config) > 0 {
				cfg = &PropertyConfig{}
				if err := json.Unmarshal(p.Config, cfg); err != nil {
					add("properties[%d] %q: config is not valid", i, p.Name)
				}
			}
			if _, err := validatePropertyConfig(p.Type, cfg); err != nil {
				add("properties[%d] %q: %v", i, p.Name, err)
			}
		}
		for _, k := range p.IssueTypes {
			if !typeKeys[k] && !isSeededIssueTypeKey(k) {
				add("properties[%d] %q: issue type %q is neither built in nor in the bundle", i, p.Name, k)
			}
		}
	}
	projectTitles := map[string]bool{}
	for _, p := range b.Projects {
		projectTitles[p.Title] = true
	}
	for i, v := range b.Views {
		if strings.TrimSpace(v.Name) == "" {
			add("views[%d]: name is required", i)
		}
		scope := v.ScopeType
		if scope == "" {
			scope = "workspace"
		}
		if !containsStr(validViewScopesForImport, scope) {
			add("views[%d] %q: scope_type must be workspace or project", i, v.Name)
		}
		if scope == "project" && !projectTitles[v.Project] {
			add("views[%d] %q: project %q is not in the bundle", i, v.Name, v.Project)
		}
		if len(v.Query) > 0 && !json.Valid(v.Query) {
			add("views[%d] %q: query is not valid JSON", i, v.Name)
		}
		if len(v.Display) > 0 && !json.Valid(v.Display) {
			add("views[%d] %q: display is not valid JSON", i, v.Name)
		}
	}
	for i, t := range b.TransitionRules {
		if !issuestatus.IsCategory(t.ToCategory) {
			add("transition_rules[%d]: to_category must be one of %s", i, strings.Join(issuestatus.Canonical(), ", "))
		}
		if t.FromCategory != "" && !issuestatus.IsCategory(t.FromCategory) {
			add("transition_rules[%d]: from_category must be a category", i)
		}
		for _, role := range append(append([]string{}, t.AllowedRoles...), t.ApproverRoles...) {
			if !containsStr(validTransitionRoles, role) {
				add("transition_rules[%d]: role %q must be owner, admin or member", i, role)
			}
		}
		for _, a := range t.AllowActorTypes {
			if !containsStr(validActorTypesForImport, a) {
				add("transition_rules[%d]: actor type %q must be member or agent", i, a)
			}
		}
		if t.RejectStatusKey != "" && !statusKeys[t.RejectStatusKey] {
			add("transition_rules[%d]: reject_status_key %q is not a status of the bundle or a built-in", i, t.RejectStatusKey)
		}
		if t.Project != "" && !projectTitles[t.Project] {
			add("transition_rules[%d]: project %q is not in the bundle", i, t.Project)
		}
	}
	for i, r := range b.BusinessRules {
		if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.NaturalLanguage) == "" {
			add("business_rules[%d]: title and natural_language are required", i)
		}
		if !containsStr(validAttachPointsForImport, r.AttachPoint) {
			add("business_rules[%d] %q: attach_point must be one of %s", i, r.Title, strings.Join(validAttachPointsForImport, ", "))
		}
	}
	agentNames := map[string]bool{}
	skillNames := map[string]bool{}
	for _, s := range b.Skills {
		skillNames[s.Name] = true
	}
	profileNames := map[string]bool{}
	for _, p := range b.Profiles {
		profileNames[p.Name] = true
	}
	for i, a := range b.Agents {
		if strings.TrimSpace(a.Name) == "" {
			add("agents[%d]: name is required", i)
		}
		agentNames[a.Name] = true
		if !containsStr(validTrustModesForImport, a.TrustMode) {
			add("agents[%d] %q: trust_mode must be observer, propose, approval or autonomous", i, a.Name)
		}
		for _, s := range a.Skills {
			if !skillNames[s] {
				add("agents[%d] %q: skill %q is not in the bundle", i, a.Name, s)
			}
		}
		if a.PermissionProfile != "" && !profileNames[a.PermissionProfile] {
			add("agents[%d] %q: permission profile %q is not in the bundle", i, a.Name, a.PermissionProfile)
		}
	}
	for i, o := range b.OwnershipRules {
		if o.Label == "" && o.PathPattern == "" {
			add("ownership_rules[%d]: label or path_pattern is required", i)
		}
		if o.Label != "" && !labelNames[strings.ToLower(o.Label)] {
			add("ownership_rules[%d]: label %q is not in the bundle", i, o.Label)
		}
		if o.ReferentAgent != "" && !agentNames[o.ReferentAgent] {
			add("ownership_rules[%d]: agent %q is not in the bundle", i, o.ReferentAgent)
		}
	}
	goalKeys := map[string]bool{}
	for _, g := range b.Goals {
		goalKeys[g.Key] = true
	}
	for i, a := range b.Autopilots {
		if a.AssigneeType == "agent" && !agentNames[a.AssigneeAgent] {
			add("autopilots[%d] %q: assignee agent %q is not in the bundle", i, a.Title, a.AssigneeAgent)
		}
		if a.Project != "" && !projectTitles[a.Project] {
			add("autopilots[%d] %q: project %q is not in the bundle", i, a.Title, a.Project)
		}
	}
	for i, p := range b.Projects {
		for _, k := range p.Goals {
			if !goalKeys[k] {
				add("projects[%d] %q: goal %q is not in the bundle", i, p.Title, k)
			}
		}
	}
	for i, is := range b.Issues {
		if is.Status != "" && !statusKeys[is.Status] {
			add("issues[%d] %q: status %q is not a status of the bundle or a built-in", i, is.Title, is.Status)
		}
		if is.Project != "" && !projectTitles[is.Project] {
			add("issues[%d] %q: project %q is not in the bundle", i, is.Title, is.Project)
		}
		if is.GoalKey != "" && !goalKeys[is.GoalKey] {
			add("issues[%d] %q: goal %q is not in the bundle", i, is.Title, is.GoalKey)
		}
		for _, l := range is.Labels {
			if !labelNames[strings.ToLower(l)] {
				add("issues[%d] %q: label %q is not in the bundle (add it under labels)", i, is.Title, l)
			}
		}
	}
	if len(b.Doctrine) > doctrineMaxBytes {
		add("doctrine: limited to %d bytes", doctrineMaxBytes)
	}
	return problems
}

// --- collisions --------------------------------------------------------------------

func (h *Handler) transferConfigCollisions(ctx context.Context, wsUUID pgtype.UUID, b *transferBundle) []transferCollision {
	out := []transferCollision{}
	for _, s := range b.IssueStatuses {
		if key := statusKeyOf(s); key != "" {
			if row, err := h.Queries.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: wsUUID, Key: key}); err == nil {
				out = append(out, transferCollision{Kind: "issue_status", Name: s.Name, ExistingID: uuidToString(row.ID)})
			}
		}
	}
	for _, t := range b.IssueTypes {
		if row, err := h.Queries.GetIssueTypeEntryByKey(ctx, db.GetIssueTypeEntryByKeyParams{WorkspaceID: wsUUID, Key: typeKeyOf(t)}); err == nil {
			out = append(out, transferCollision{Kind: "issue_type", Name: t.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, l := range b.Labels {
		if row, err := h.Queries.GetLabelByNameForImport(ctx, db.GetLabelByNameForImportParams{WorkspaceID: wsUUID, Name: l.Name}); err == nil {
			out = append(out, transferCollision{Kind: "label", Name: l.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, p := range b.Properties {
		if row, err := h.Queries.GetIssuePropertyByNameForImport(ctx, db.GetIssuePropertyByNameForImportParams{WorkspaceID: wsUUID, Lower: p.Name}); err == nil {
			out = append(out, transferCollision{Kind: "property", Name: p.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, v := range b.Views {
		if row, err := h.Queries.GetIssueViewByNameForImport(ctx, db.GetIssueViewByNameForImportParams{WorkspaceID: wsUUID, Name: v.Name}); err == nil {
			out = append(out, transferCollision{Kind: "view", Name: v.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, r := range b.BusinessRules {
		if row, err := h.Queries.GetBusinessRuleByTitleForImport(ctx, db.GetBusinessRuleByTitleForImportParams{WorkspaceID: wsUUID, Title: r.Title}); err == nil {
			out = append(out, transferCollision{Kind: "business_rule", Name: r.Title, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, t := range b.TransitionRules {
		if t.Project != "" {
			continue // project rules collide only once the project resolves, at apply time
		}
		if row, err := h.Queries.GetIssueTransitionRuleForImport(ctx, db.GetIssueTransitionRuleForImportParams{WorkspaceID: wsUUID, FromCategory: pgtype.Text{String: t.FromCategory, Valid: t.FromCategory != ""}, ToCategory: t.ToCategory}); err == nil {
			out = append(out, transferCollision{Kind: "transition_rule", Name: transitionRuleName(t), ExistingID: uuidToString(row.ID)})
		}
	}
	return out
}

// --- apply ---------------------------------------------------------------------------

func (h *Handler) applyTransferConfig(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID, b *transferBundle, strategy string, importer pgtype.UUID, report *transferReport) (*transferRefs, error) {
	refs := &transferRefs{labels: map[string]pgtype.UUID{}, statuses: map[string]bool{}, types: map[string]bool{}, properties: map[string]transferPropertyRef{}}
	skip := func(kind, name, existing string) {
		report.Skipped = append(report.Skipped, transferCollision{Kind: kind, Name: name, ExistingID: existing})
	}

	// Statuses: keyed rows, never renamed (a key is the value issues store).
	// An existing key is merged (name, description, colour) or skipped; the
	// rename strategy behaves like skip here.
	if len(b.IssueStatuses) > 0 {
		if err := q.LockIssueStatusCatalog(ctx, wsUUID); err != nil {
			return nil, fmt.Errorf("lock statuses: %w", err)
		}
		entries, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: wsUUID, IncludeArchived: true})
		if err != nil {
			return nil, fmt.Errorf("statuses: %w", err)
		}
		taken := map[string]bool{}
		for _, e := range entries {
			taken[e.Key] = true
		}
		for _, s := range b.IssueStatuses {
			color, err := normalizeColor(s.Color)
			if err != nil {
				return nil, fmt.Errorf("status %q: %w", s.Name, err)
			}
			key := statusKeyOf(s)
			if key == "" {
				derived, err := issuestatus.DeriveKey(s.Name, s.Category, taken)
				if err != nil {
					return nil, fmt.Errorf("status %q: %w", s.Name, err)
				}
				key = derived
			}
			if existing, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: wsUUID, Key: key}); err == nil {
				refs.statuses[key] = true
				if strategy == transferStrategyMerge {
					if _, err := q.UpdateIssueStatusEntry(ctx, db.UpdateIssueStatusEntryParams{ID: existing.ID, WorkspaceID: wsUUID, Name: pgtype.Text{String: strings.TrimSpace(s.Name), Valid: true}, Description: pgtype.Text{String: s.Description, Valid: true}, Color: pgtype.Text{String: color, Valid: true}}); err != nil {
						return nil, fmt.Errorf("merge status %q: %w", s.Name, err)
					}
					report.merged("issue_statuses", s.Name, existing.ID)
				} else {
					skip("issue_status", s.Name, uuidToString(existing.ID))
				}
				continue
			}
			row, err := q.CreateIssueStatusEntry(ctx, db.CreateIssueStatusEntryParams{WorkspaceID: wsUUID, Key: key, Name: strings.TrimSpace(s.Name), Description: s.Description, Category: s.Category, Color: color})
			if err != nil {
				return nil, fmt.Errorf("create status %q: %w", s.Name, err)
			}
			taken[key] = true
			refs.statuses[key] = true
			report.created("issue_statuses", s.Name, row.ID)
		}
	}

	// Work item types: same rule as statuses.
	if len(b.IssueTypes) > 0 {
		if err := q.LockIssueTypeCatalog(ctx, wsUUID); err != nil {
			return nil, fmt.Errorf("lock types: %w", err)
		}
		if err := q.SeedIssueTypeEntries(ctx, wsUUID); err != nil {
			return nil, fmt.Errorf("seed types: %w", err)
		}
		for _, t := range b.IssueTypes {
			key, err := validateIssueTypeKey(typeKeyOf(t))
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", t.Name, err)
			}
			color, err := normalizeColor(t.Color)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", t.Name, err)
			}
			if existing, err := q.GetIssueTypeEntryByKey(ctx, db.GetIssueTypeEntryByKeyParams{WorkspaceID: wsUUID, Key: key}); err == nil {
				refs.types[key] = true
				if strategy == transferStrategyMerge {
					if _, err := q.UpdateIssueTypeEntry(ctx, db.UpdateIssueTypeEntryParams{ID: existing.ID, WorkspaceID: wsUUID, Name: pgtype.Text{String: strings.TrimSpace(t.Name), Valid: true}, Description: pgtype.Text{String: t.Description, Valid: true}, Color: pgtype.Text{String: color, Valid: true}, Icon: pgtype.Text{String: t.Icon, Valid: true}}); err != nil {
						return nil, fmt.Errorf("merge type %q: %w", t.Name, err)
					}
					report.merged("issue_types", t.Name, existing.ID)
				} else {
					skip("issue_type", t.Name, uuidToString(existing.ID))
				}
				continue
			}
			row, err := q.CreateIssueTypeEntry(ctx, db.CreateIssueTypeEntryParams{WorkspaceID: wsUUID, Key: key, Name: strings.TrimSpace(t.Name), Description: t.Description, Color: color, Icon: t.Icon})
			if err != nil {
				return nil, fmt.Errorf("create type %q: %w", t.Name, err)
			}
			refs.types[key] = true
			report.created("issue_types", t.Name, row.ID)
		}
	}

	// Labels: unique per resource type, case-insensitive; merged or skipped,
	// never renamed (a renamed label would not be the one issues reference).
	for _, l := range b.Labels {
		name, err := validateLabelName(l.Name)
		if err != nil {
			return nil, fmt.Errorf("label %q: %w", l.Name, err)
		}
		color, err := normalizeColor(l.Color)
		if err != nil {
			return nil, fmt.Errorf("label %q: %w", l.Name, err)
		}
		resource := nonEmpty(l.ResourceType, "issue")
		if existing, err := q.GetLabelByNameForImport(ctx, db.GetLabelByNameForImportParams{WorkspaceID: wsUUID, Name: name}); err == nil && existing.ResourceType == resource {
			refs.labels[strings.ToLower(name)] = existing.ID
			if strategy == transferStrategyMerge {
				if _, err := q.UpdateLabel(ctx, db.UpdateLabelParams{ID: existing.ID, WorkspaceID: wsUUID, Description: pgtype.Text{String: l.Description, Valid: true}, Color: pgtype.Text{String: color, Valid: true}}); err != nil {
					return nil, fmt.Errorf("merge label %q: %w", name, err)
				}
				report.merged("labels", name, existing.ID)
			} else {
				skip("label", name, uuidToString(existing.ID))
			}
			continue
		}
		row, err := q.CreateLabel(ctx, db.CreateLabelParams{WorkspaceID: wsUUID, ResourceType: resource, Name: name, Description: l.Description, Color: color})
		if err != nil {
			return nil, fmt.Errorf("create label %q: %w", name, err)
		}
		refs.labels[strings.ToLower(name)] = row.ID
		report.created("labels", name, row.ID)
	}

	// Custom properties with their type scope.
	for _, p := range b.Properties {
		name, err := validatePropertyName(p.Name)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", p.Name, err)
		}
		var cfg *PropertyConfig
		if len(p.Config) > 0 {
			cfg = &PropertyConfig{}
			if err := json.Unmarshal(p.Config, cfg); err != nil {
				return nil, fmt.Errorf("property %q: config: %w", name, err)
			}
		}
		config, err := validatePropertyConfig(p.Type, cfg)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		icon, err := validatePropertyIcon(p.Icon)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		scope := []string{}
		for _, k := range p.IssueTypes {
			scope = append(scope, k)
		}
		if existing, err := q.GetIssuePropertyByNameForImport(ctx, db.GetIssuePropertyByNameForImportParams{WorkspaceID: wsUUID, Lower: name}); err == nil {
			refs.properties[strings.ToLower(name)] = propertyRefOf(existing.ID, existing.Config)
			if strategy == transferStrategyMerge && existing.Type == p.Type {
				if _, err := q.UpdateIssueProperty(ctx, db.UpdateIssuePropertyParams{ID: existing.ID, WorkspaceID: wsUUID, Description: pgtype.Text{String: p.Description, Valid: true}, Icon: pgtype.Text{String: icon, Valid: true}, Config: config}); err != nil {
					return nil, fmt.Errorf("merge property %q: %w", name, err)
				}
				if err := q.DeleteIssuePropertyTypes(ctx, existing.ID); err != nil {
					return nil, fmt.Errorf("property scope %q: %w", name, err)
				}
				if len(scope) > 0 {
					if err := q.InsertIssuePropertyTypes(ctx, db.InsertIssuePropertyTypesParams{WorkspaceID: wsUUID, PropertyID: existing.ID, TypeKeys: scope}); err != nil {
						return nil, fmt.Errorf("property scope %q: %w", name, err)
					}
				}
				refs.properties[strings.ToLower(name)] = propertyRefOf(existing.ID, config)
				report.merged("properties", name, existing.ID)
			} else {
				skip("property", name, uuidToString(existing.ID))
			}
			continue
		}
		row, err := q.CreateIssueProperty(ctx, db.CreateIssuePropertyParams{WorkspaceID: wsUUID, Name: name, Type: p.Type, Description: p.Description, Icon: icon, Config: config})
		if err != nil {
			return nil, fmt.Errorf("create property %q: %w", name, err)
		}
		if len(scope) > 0 {
			if err := q.InsertIssuePropertyTypes(ctx, db.InsertIssuePropertyTypesParams{WorkspaceID: wsUUID, PropertyID: row.ID, TypeKeys: scope}); err != nil {
				return nil, fmt.Errorf("property scope %q: %w", name, err)
			}
		}
		refs.properties[strings.ToLower(name)] = propertyRefOf(row.ID, row.Config)
		report.created("properties", name, row.ID)
	}

	// Business rules land as drafts: a person previews and activates them.
	// Without a precompiled predicate the rule is compiled when an LLM is
	// configured, otherwise it stays a draft to compile from the settings.
	for _, r := range b.BusinessRules {
		if existing, err := q.GetBusinessRuleByTitleForImport(ctx, db.GetBusinessRuleByTitleForImportParams{WorkspaceID: wsUUID, Title: r.Title}); err == nil {
			skip("business_rule", r.Title, uuidToString(existing.ID))
			continue
		}
		predicate, action := nonEmptyJSON(r.Predicate), nonEmptyJSON(r.Action)
		if len(strings.TrimSpace(string(r.Predicate))) == 0 {
			if h.LLM != nil && h.LLM.Enabled() {
				if _, compiled, compiledAction, err := h.compileBusinessRule(ctx, r.AttachPoint, r.NaturalLanguage); err == nil {
					predicate, action = compiled, nonEmptyJSON(compiledAction)
				} else {
					report.Warnings = append(report.Warnings, fmt.Sprintf("business rule %q: not compiled (%v); open it in Settings to compile", r.Title, err))
				}
			} else {
				report.Warnings = append(report.Warnings, fmt.Sprintf("business rule %q: no model configured; open it in Settings to compile", r.Title))
			}
		}
		row, err := q.CreateBusinessRule(ctx, db.CreateBusinessRuleParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: r.Title, NaturalLanguage: r.NaturalLanguage, CompiledPredicate: predicate, AttachPoint: r.AttachPoint, CreatedBy: importer, ActionSpec: action})
		if err != nil {
			return nil, fmt.Errorf("create business rule %q: %w", r.Title, err)
		}
		report.created("business_rules", r.Title, row.ID)
	}
	return refs, nil
}

// applyTransferViewsAndOwnership runs once agents and projects exist.
func (h *Handler) applyTransferViewsAndOwnership(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID, b *transferBundle, strategy string, importer pgtype.UUID, report *transferReport, refs *transferRefs) error {
	skip := func(kind, name, existing string) {
		report.Skipped = append(report.Skipped, transferCollision{Kind: kind, Name: name, ExistingID: existing})
	}
	for _, t := range b.TransitionRules {
		projectID := pgtype.UUID{}
		if t.Project != "" {
			pid, ok := refs.projects[t.Project]
			if !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("transition rule %q skipped: project not imported", transitionRuleName(t)))
				continue
			}
			projectID = pid
		}
		if existing, err := q.GetIssueTransitionRuleForImport(ctx, db.GetIssueTransitionRuleForImportParams{WorkspaceID: wsUUID, ProjectID: projectID, FromCategory: pgtype.Text{String: t.FromCategory, Valid: t.FromCategory != ""}, ToCategory: t.ToCategory}); err == nil {
			skip("transition_rule", transitionRuleName(t), uuidToString(existing.ID))
			continue
		}
		reject := pgtype.Text{}
		if t.RejectStatusKey != "" {
			reject = pgtype.Text{String: t.RejectStatusKey, Valid: true}
		}
		row, err := q.CreateIssueTransitionRule(ctx, db.CreateIssueTransitionRuleParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, ProjectID: projectID, FromCategory: pgtype.Text{String: t.FromCategory, Valid: t.FromCategory != ""}, ToCategory: t.ToCategory, AllowedRoles: nonNilStrings(t.AllowedRoles), AllowActorTypes: nonNilStrings(t.AllowActorTypes), RequiresApproval: t.RequiresApproval, ApproverRoles: nonNilStrings(t.ApproverRoles), RejectStatusKey: reject, Enabled: t.Enabled, CreatedBy: importer})
		if err != nil {
			return fmt.Errorf("create transition rule %q: %w", transitionRuleName(t), err)
		}
		report.created("transition_rules", transitionRuleName(t), row.ID)
	}

	viewExists := func(name string) bool {
		_, err := q.GetIssueViewByNameForImport(ctx, db.GetIssueViewByNameForImportParams{WorkspaceID: wsUUID, Name: name})
		return err == nil
	}
	for _, v := range b.Views {
		scope := nonEmpty(v.ScopeType, "workspace")
		scopeID := pgtype.UUID{}
		if scope == "project" {
			pid, ok := refs.projects[v.Project]
			if !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("view %q skipped: project %q not imported", v.Name, v.Project))
				continue
			}
			scopeID = pid
		}
		name := v.Name
		query := rewriteViewQuery(nonEmptyJSON(v.Query), refs)
		if existing, err := q.GetIssueViewByNameForImport(ctx, db.GetIssueViewByNameForImportParams{WorkspaceID: wsUUID, Name: name}); err == nil {
			switch strategy {
			case transferStrategyMerge:
				if _, err := q.UpdateIssueView(ctx, db.UpdateIssueViewParams{ID: existing.ID, WorkspaceID: wsUUID, Name: name, Visibility: "workspace", Query: query, Display: nonEmptyJSON(v.Display), Revision: existing.Revision}); err != nil {
					return fmt.Errorf("merge view %q: %w", name, err)
				}
				report.merged("views", name, existing.ID)
				continue
			case transferStrategyRename:
				if candidate := name + transferRenameSuffix; !viewExists(candidate) {
					name = candidate
				} else {
					skip("view", v.Name, uuidToString(existing.ID))
					continue
				}
			default:
				skip("view", name, uuidToString(existing.ID))
				continue
			}
		}
		row, err := q.CreateIssueView(ctx, db.CreateIssueViewParams{WorkspaceID: wsUUID, OwnerID: importer, Name: name, ScopeType: scope, ScopeID: scopeID, Visibility: "workspace", DefinitionVersion: maxInt32(v.DefinitionVersion, 1), Query: query, Display: nonEmptyJSON(v.Display)})
		if err != nil {
			return fmt.Errorf("create view %q: %w", name, err)
		}
		report.created("views", name, row.ID)
	}

	for _, o := range b.OwnershipRules {
		// The importer is the rule's human owner: a pack names a referent
		// agent, never a person, and the table needs an accountable member.
		params := db.CreateModuleOwnershipParams{WorkspaceID: wsUUID, Priority: o.Priority, OwnerUserID: importer}
		if o.PathPattern != "" {
			params.PathPattern = pgtype.Text{String: o.PathPattern, Valid: true}
		}
		if o.Label != "" {
			id, ok := refs.labels[strings.ToLower(o.Label)]
			if !ok {
				if row, err := q.GetLabelByNameForImport(ctx, db.GetLabelByNameForImportParams{WorkspaceID: wsUUID, Name: o.Label}); err == nil {
					id, ok = row.ID, true
				}
			}
			if !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("ownership rule for label %q skipped: label not found", o.Label))
				continue
			}
			params.LabelID = id
		}
		if o.ReferentAgent != "" {
			if id, ok := refs.agents[o.ReferentAgent]; ok {
				params.ReferentAgentID = id
			} else {
				report.Warnings = append(report.Warnings, fmt.Sprintf("ownership rule %q: referent agent %q not found", ownershipRuleName(o), o.ReferentAgent))
			}
		}
		if existing, err := q.GetModuleOwnershipForImport(ctx, db.GetModuleOwnershipForImportParams{WorkspaceID: wsUUID, PathPattern: params.PathPattern, LabelID: params.LabelID}); err == nil {
			skip("ownership_rule", ownershipRuleName(o), uuidToString(existing.ID))
			continue
		}
		row, err := q.CreateModuleOwnership(ctx, params)
		if err != nil {
			return fmt.Errorf("create ownership rule %q: %w", ownershipRuleName(o), err)
		}
		report.created("ownership_rules", ownershipRuleName(o), row.ID)
	}
	return nil
}

// importTransferDoctrine runs after the configuration committed: the pack's
// rules become the doctrine when the workspace has none, otherwise a section
// named after the bundle is appended under merge and rename; skip leaves the
// doctrine alone. It goes through the doctrine ledger like any publication,
// so a workspace that requires a second reviewer sees it as a proposal.
func (h *Handler) importTransferDoctrine(ctx context.Context, wsUUID pgtype.UUID, b *transferBundle, strategy string, importer pgtype.UUID, report *transferReport) {
	text := strings.TrimSpace(b.Doctrine)
	if text == "" {
		return
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: importer, WorkspaceID: wsUUID})
	if err != nil {
		report.Warnings = append(report.Warnings, "doctrine not applied: importer is not a member")
		return
	}
	current, err := h.Queries.GetWorkspaceDoctrine(ctx, wsUUID)
	if err != nil {
		report.Warnings = append(report.Warnings, "doctrine not applied: "+err.Error())
		return
	}
	live := strings.TrimSpace(current.Context.String)
	section := "## " + nonEmpty(b.Manifest.Name, "Imported rules") + "\n\n" + text
	content := section
	if live != "" {
		// A section is not a collision: every strategy appends it, and only
		// the very same section already present is skipped.
		if strings.Contains(live, section) {
			report.Skipped = append(report.Skipped, transferCollision{Kind: "doctrine", Name: b.Manifest.Name})
			return
		}
		content = live + "\n\n" + section
	}
	version, status, err := h.publishDoctrine(ctx, wsUUID, member, doctrinePublication{Content: content, Note: "imported from " + nonEmpty(b.Manifest.Source.Name, b.Manifest.Name)})
	if err != nil {
		var de *doctrineError
		if errors.As(err, &de) {
			report.Warnings = append(report.Warnings, "doctrine not applied: "+de.msg)
		} else {
			report.Warnings = append(report.Warnings, "doctrine not applied: "+err.Error())
		}
		return
	}
	if status == 202 {
		report.Warnings = append(report.Warnings, "doctrine filed as a proposal: another owner or admin has to approve it")
	}
	report.created("doctrine", b.Manifest.Name, version.ID)
}

// --- export --------------------------------------------------------------------------

func (h *Handler) exportTransferConfig(ctx context.Context, ws db.Workspace, b *transferBundle) error {
	for _, cat := range issuestatus.Canonical() {
		rows, err := h.Queries.ListActiveCustomIssueStatusEntries(ctx, db.ListActiveCustomIssueStatusEntriesParams{WorkspaceID: ws.ID, Category: cat})
		if err != nil {
			return fmt.Errorf("statuses: %w", err)
		}
		for _, s := range rows {
			b.IssueStatuses = append(b.IssueStatuses, transferIssueStatus{Key: s.Key, Name: s.Name, Description: s.Description, Category: s.Category, Color: s.Color})
		}
	}
	types, err := h.Queries.ListActiveCustomIssueTypeEntries(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("issue types: %w", err)
	}
	for _, t := range types {
		if t.IsSystem {
			continue
		}
		b.IssueTypes = append(b.IssueTypes, transferIssueType{Key: t.Key, Name: t.Name, Description: t.Description, Color: t.Color, Icon: t.Icon})
	}
	labelNames := map[string]string{}
	for _, resource := range []string{"issue", "project"} {
		rows, err := h.Queries.ListLabels(ctx, db.ListLabelsParams{WorkspaceID: ws.ID, ResourceType: resource})
		if err != nil {
			return fmt.Errorf("labels: %w", err)
		}
		for _, l := range rows {
			labelNames[uuidToString(l.ID)] = l.Name
			b.Labels = append(b.Labels, transferLabel{Name: l.Name, Description: l.Description, Color: l.Color, ResourceType: l.ResourceType})
		}
	}
	scopes := map[string][]string{}
	if rows, err := h.Queries.ListIssuePropertyTypesForWorkspace(ctx, ws.ID); err == nil {
		for _, r := range rows {
			scopes[uuidToString(r.PropertyID)] = append(scopes[uuidToString(r.PropertyID)], r.TypeKey)
		}
	}
	props, err := h.Queries.ListIssueProperties(ctx, db.ListIssuePropertiesParams{WorkspaceID: ws.ID})
	if err != nil {
		return fmt.Errorf("properties: %w", err)
	}
	for _, p := range props {
		scope := scopes[uuidToString(p.ID)]
		if scope == nil {
			scope = []string{}
		}
		b.Properties = append(b.Properties, transferProperty{Name: p.Name, Type: p.Type, Description: p.Description, Icon: p.Icon, Config: nonEmptyJSON(p.Config), IssueTypes: scope})
	}
	projectTitles := map[string]string{}
	for _, p := range b.Projects {
		projectTitles[p.Title] = p.Title
	}
	projectByID := map[string]string{}
	if rows, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: ws.ID}); err == nil {
		for _, p := range rows {
			projectByID[uuidToString(p.ID)] = p.Title
		}
	}
	views, err := h.Queries.ListWorkspaceIssueViewsForExport(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("views: %w", err)
	}
	for _, v := range views {
		project := ""
		if v.ScopeType == "project" {
			project = projectByID[uuidToString(v.ScopeID)]
			if project == "" {
				continue
			}
		}
		b.Views = append(b.Views, transferView{Name: v.Name, ScopeType: v.ScopeType, Project: project, Visibility: v.Visibility, DefinitionVersion: v.DefinitionVersion, Query: scrubJSON(v.Query), Display: scrubJSON(v.Display)})
	}
	rules, err := h.Queries.ListIssueTransitionRules(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("transition rules: %w", err)
	}
	for _, r := range rules {
		project := ""
		if r.ProjectID.Valid {
			project = projectByID[uuidToString(r.ProjectID)]
			if project == "" {
				continue
			}
		}
		b.TransitionRules = append(b.TransitionRules, transferTransition{Project: project, FromCategory: r.FromCategory.String, ToCategory: r.ToCategory, AllowedRoles: nonNilStrings(r.AllowedRoles), AllowActorTypes: nonNilStrings(r.AllowActorTypes), RequiresApproval: r.RequiresApproval, ApproverRoles: nonNilStrings(r.ApproverRoles), RejectStatusKey: r.RejectStatusKey.String, Enabled: r.Enabled})
	}
	brules, err := h.Queries.ListBusinessRules(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("business rules: %w", err)
	}
	for _, r := range brules {
		b.BusinessRules = append(b.BusinessRules, transferBusinessRule{Title: r.Title, AttachPoint: r.AttachPoint, NaturalLanguage: r.NaturalLanguage, Predicate: scrubJSON(nonEmptyJSON(r.CompiledPredicate)), Action: scrubJSON(nonEmptyJSON(r.ActionSpec))})
	}
	agentNames := map[string]string{}
	if rows, err := h.Queries.ListAgents(ctx, ws.ID); err == nil {
		for _, a := range rows {
			agentNames[uuidToString(a.ID)] = a.Name
		}
	}
	owns, err := h.Queries.ListModuleOwnership(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("ownership rules: %w", err)
	}
	for _, o := range owns {
		if o.OwnerUserID.Valid && !o.LabelID.Valid && !o.PathPattern.Valid {
			continue
		}
		b.OwnershipRules = append(b.OwnershipRules, transferOwnershipRule{PathPattern: o.PathPattern.String, Label: labelNames[uuidToString(o.LabelID)], ReferentAgent: agentNames[uuidToString(o.ReferentAgentID)], Priority: o.Priority})
	}
	b.Doctrine = strings.TrimSpace(ws.Context.String)
	return nil
}

func propertyIconKeys() []string {
	keys := make([]string, 0, len(validPropertyIcons))
	for k := range validPropertyIcons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
