package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Workspace export / import (K76): a versioned zip bundle (manifest.json +
// bundle.json) carries agents with their versions, skills with files,
// permission profiles, projects with resources, goals, autopilots with
// triggers, triage sources, org structures and, on request, notes and
// issues. Secret VALUES never travel: env values become declarations (agent,
// key), tokens and signing secrets are dropped, every JSON config is scrubbed
// by key and by value pattern, and a test scans the bundle bytes. Import is
// admin-only, previews collisions (same name / title) with three strategies
// — rename, merge, skip — asks the declared secrets back, and never touches
// members or tokens. Every transfer leaves a workspace_transfer_run with its
// report; a template export keeps its bundle so a new workspace can start
// from it.

const (
	transferFormatVersion = 1
	transferMaxUpload     = 32 << 20
	transferRenameSuffix  = " (imported)"

	AuditWorkspaceExported = "workspace.exported"
	AuditWorkspaceImported = "workspace.imported"

	transferStrategyRename = "rename"
	transferStrategyMerge  = "merge"
	transferStrategySkip   = "skip"
)

var transferStrategies = []string{transferStrategyRename, transferStrategyMerge, transferStrategySkip}

// secretKeyRe names the JSON keys whose string values are replaced by the
// gateway mask; secretValueRe catches well-known token shapes anywhere.
var (
	secretKeyRe   = regexp.MustCompile(`(?i)(token|secret|passw|api[_-]?key|apikey|authorization|bearer|credential|private[_-]?key|signing|cookie|session)`)
	secretValueRe = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{10,}|xox[abpr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|Bearer\s+[A-Za-z0-9._-]{16,}|eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{10,})`)
)

type transferSecret struct {
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Scoped bool   `json:"scoped,omitempty"`
}

type transferManifest struct {
	FormatVersion int                         `json:"format_version"`
	ExportedAt    string                      `json:"exported_at"`
	Name          string                      `json:"name"`
	Template      bool                        `json:"template"`
	Source        struct{ Name, Slug string } `json:"source"`
	Counts        map[string]int              `json:"counts"`
	Secrets       []transferSecret            `json:"secrets"`
}

type transferFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type transferAgentVersion struct {
	Number       int32           `json:"number"`
	Instructions string          `json:"instructions"`
	Model        string          `json:"model"`
	Note         string          `json:"note"`
	ToolConfig   json.RawMessage `json:"tool_config"`
	Skills       []string        `json:"skills"`
}

type transferAgent struct {
	Name                 string                 `json:"name"`
	Description          string                 `json:"description"`
	Instructions         string                 `json:"instructions"`
	Model                string                 `json:"model"`
	ThinkingLevel        string                 `json:"thinking_level"`
	ServiceTier          string                 `json:"service_tier"`
	RuntimeMode          string                 `json:"runtime_mode"`
	Visibility           string                 `json:"visibility"`
	MaxConcurrentTasks   int32                  `json:"max_concurrent_tasks"`
	RuntimeConfig        json.RawMessage        `json:"runtime_config"`
	McpConfig            json.RawMessage        `json:"mcp_config"`
	CustomArgs           json.RawMessage        `json:"custom_args"`
	ConversationStarters json.RawMessage        `json:"conversation_starters"`
	EnvKeys              []string               `json:"env_keys"`
	ScopedEnvKeys        []string               `json:"scoped_env_keys"`
	TrustMode            string                 `json:"trust_mode"`
	EffectMode           string                 `json:"effect_mode"`
	PermissionProfile    string                 `json:"permission_profile"`
	Skills               []string               `json:"skills"`
	Versions             []transferAgentVersion `json:"versions"`
}

type transferSkill struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Status      string          `json:"status"`
	Config      json.RawMessage `json:"config"`
	Files       []transferFile  `json:"files"`
}

type transferProfile struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	ReadOnly        bool     `json:"read_only"`
	DeniedPaths     []string `json:"denied_paths"`
	AllowedCommands []string `json:"allowed_commands"`
	HiddenSecrets   []string `json:"hidden_secrets"`
}

type transferResource struct {
	Type     string          `json:"type"`
	Label    string          `json:"label"`
	Ref      json.RawMessage `json:"ref"`
	Position int32           `json:"position"`
}

type transferProject struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Icon        string             `json:"icon"`
	Status      string             `json:"status"`
	Priority    string             `json:"priority"`
	StartDate   string             `json:"start_date"`
	DueDate     string             `json:"due_date"`
	Resources   []transferResource `json:"resources"`
	Goals       []string           `json:"goals"`
}

type transferGoal struct {
	Key            string `json:"key"`
	ParentKey      string `json:"parent_key"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	SuccessMeasure string `json:"success_measure"`
	DueDate        string `json:"due_date"`
	Status         string `json:"status"`
}

type transferTrigger struct {
	Kind               string          `json:"kind"`
	Enabled            bool            `json:"enabled"`
	Cron               string          `json:"cron"`
	Timezone           string          `json:"timezone"`
	Label              string          `json:"label"`
	Provider           string          `json:"provider"`
	EventFilters       json.RawMessage `json:"event_filters"`
	EventMatchCriteria string          `json:"event_match_criteria"`
	WindowMinutes      int32           `json:"window_minutes"`
	HadSecret          bool            `json:"had_secret"`
}

type transferAutopilot struct {
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	AssigneeType       string            `json:"assignee_type"`
	AssigneeAgent      string            `json:"assignee_agent"`
	ExecutionMode      string            `json:"execution_mode"`
	IssueTitleTemplate string            `json:"issue_title_template"`
	Project            string            `json:"project"`
	Triggers           []transferTrigger `json:"triggers"`
}

type transferTriageSource struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Icon       string          `json:"icon"`
	Mode       string          `json:"mode"`
	AutoAccept json.RawMessage `json:"auto_accept"`
	CapPerHour int32           `json:"cap_per_hour"`
	ExpiryDays int32           `json:"expiry_days"`
}

type transferOrg struct {
	Project        string          `json:"project"`
	Model          string          `json:"model"`
	Name           string          `json:"name"`
	EndCondition   string          `json:"end_condition"`
	BudgetUsdTicks int64           `json:"budget_usd_ticks"`
	Definition     json.RawMessage `json:"definition"`
}

type transferNote struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Pinned  bool     `json:"pinned"`
}

type transferIssue struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Project     string   `json:"project"`
	GoalKey     string   `json:"goal_key"`
	Labels      []string `json:"labels"`
}

type transferBundle struct {
	Manifest      transferManifest       `json:"manifest"`
	Profiles      []transferProfile      `json:"permission_profiles"`
	Skills        []transferSkill        `json:"skills"`
	Agents        []transferAgent        `json:"agents"`
	Projects      []transferProject      `json:"projects"`
	Goals         []transferGoal         `json:"goals"`
	Autopilots    []transferAutopilot    `json:"autopilots"`
	TriageSources []transferTriageSource `json:"triage_sources"`
	Org           []transferOrg          `json:"org_structures"`
	Notes         []transferNote         `json:"notes"`
	Issues        []transferIssue        `json:"issues"`
}

// --- scrubbing ---------------------------------------------------------------------

// scrubValue walks decoded JSON: a string under a secret-looking key, or a
// string that looks like a token, becomes the mask.
func scrubValue(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = scrubValue(val, k)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = scrubValue(val, key)
		}
		return out
	case string:
		if t == "" {
			return t
		}
		if secretKeyRe.MatchString(key) || secretValueRe.MatchString(t) {
			return runtimeConfigGatewayTokenMask
		}
		return t
	}
	return v
}

// scrubJSON returns the scrubbed document, or an empty object when the input
// is not JSON (nothing unparseable leaves either).
func scrubJSON(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage("{}")
	}
	out, err := json.Marshal(scrubValue(v, ""))
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}

// scrubText masks token shapes inside free text (instructions, notes).
func scrubText(s string) string {
	return secretValueRe.ReplaceAllString(s, runtimeConfigGatewayTokenMask)
}

func envKeys(raw []byte) []string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dateString(d pgtype.Date) string {
	if p := dateToPtr(d); p != nil {
		return *p
	}
	return ""
}

func parseDateOrEmpty(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	d, err := util.ParseCalendarDate(s)
	if err != nil {
		return pgtype.Date{}
	}
	return d
}

// --- export ------------------------------------------------------------------------

type transferExportOptions struct {
	IncludeIssues bool
	IncludeNotes  bool
	Template      bool
	Name          string
}

func (h *Handler) buildTransferBundle(ctx context.Context, ws db.Workspace, opts transferExportOptions) (*transferBundle, error) {
	b := &transferBundle{Profiles: []transferProfile{}, Skills: []transferSkill{}, Agents: []transferAgent{}, Projects: []transferProject{}, Goals: []transferGoal{}, Autopilots: []transferAutopilot{}, TriageSources: []transferTriageSource{}, Org: []transferOrg{}, Notes: []transferNote{}, Issues: []transferIssue{}}
	b.Manifest = transferManifest{FormatVersion: transferFormatVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339), Name: opts.Name, Template: opts.Template, Counts: map[string]int{}, Secrets: []transferSecret{}}
	b.Manifest.Source.Name, b.Manifest.Source.Slug = ws.Name, ws.Slug

	profileNames := map[string]string{}
	profiles, err := h.Queries.ListPermissionProfiles(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("permission profiles: %w", err)
	}
	for _, p := range profiles {
		profileNames[uuidToString(p.ID)] = p.Name
		if p.Builtin {
			continue
		}
		b.Profiles = append(b.Profiles, transferProfile{Name: p.Name, Description: p.Description, ReadOnly: p.ReadOnly, DeniedPaths: jsonStrings(p.DeniedPaths), AllowedCommands: jsonStrings(p.AllowedCommands), HiddenSecrets: jsonStrings(p.HiddenSecrets)})
	}
	skillNames := map[string]string{}
	skills, err := h.Queries.ListSkillsByWorkspace(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("skills: %w", err)
	}
	for _, s := range skills {
		skillNames[uuidToString(s.ID)] = s.Name
		ts := transferSkill{Name: s.Name, Description: s.Description, Content: scrubText(s.Content), Status: s.Status, Config: scrubJSON(s.Config), Files: []transferFile{}}
		if files, err := h.Queries.ListSkillFiles(ctx, s.ID); err == nil {
			for _, f := range files {
				ts.Files = append(ts.Files, transferFile{Path: f.Path, Content: scrubText(f.Content)})
			}
		}
		b.Skills = append(b.Skills, ts)
	}
	agents, err := h.Queries.ListAgents(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("agents: %w", err)
	}
	for _, a := range agents {
		keys := envKeys(a.CustomEnv)
		scoped := jsonStrings(a.ScopedEnvKeys)
		ta := transferAgent{
			Name: a.Name, Description: a.Description, Instructions: scrubText(a.Instructions), Model: a.Model.String, ThinkingLevel: a.ThinkingLevel.String, ServiceTier: a.ServiceTier.String,
			RuntimeMode: a.RuntimeMode, Visibility: a.Visibility, MaxConcurrentTasks: a.MaxConcurrentTasks,
			RuntimeConfig: scrubJSON(a.RuntimeConfig), McpConfig: scrubJSON(a.McpConfig), CustomArgs: scrubJSON(a.CustomArgs), ConversationStarters: scrubJSON(a.ConversationStarters),
			EnvKeys: keys, ScopedEnvKeys: scoped, TrustMode: a.TrustMode, EffectMode: a.EffectMode, PermissionProfile: profileNames[uuidToString(a.PermissionProfileID)], Skills: []string{}, Versions: []transferAgentVersion{},
		}
		for _, k := range keys {
			b.Manifest.Secrets = append(b.Manifest.Secrets, transferSecret{Scope: "agent", Name: a.Name, Key: k, Scoped: containsStr(scoped, k)})
		}
		if attached, err := h.Queries.ListAgentSkills(ctx, a.ID); err == nil {
			for _, s := range attached {
				ta.Skills = append(ta.Skills, s.Name)
			}
		}
		if versions, err := h.Queries.ListAgentVersions(ctx, a.ID); err == nil {
			for i := len(versions) - 1; i >= 0; i-- {
				v := versions[i]
				var names []string
				for _, id := range jsonStrings(v.SkillIds) {
					if n := skillNames[id]; n != "" {
						names = append(names, n)
					}
				}
				ta.Versions = append(ta.Versions, transferAgentVersion{Number: v.VersionNumber, Instructions: scrubText(v.Instructions), Model: v.Model, Note: v.Note, ToolConfig: scrubJSON(v.ToolConfig), Skills: names})
			}
		}
		b.Agents = append(b.Agents, ta)
	}
	goalKeys := map[string]string{}
	goals, err := h.Queries.ListGoals(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("goals: %w", err)
	}
	for i, g := range goals {
		goalKeys[uuidToString(g.ID)] = fmt.Sprintf("g%d", i+1)
	}
	for _, g := range goals {
		b.Goals = append(b.Goals, transferGoal{Key: goalKeys[uuidToString(g.ID)], ParentKey: goalKeys[uuidToString(g.ParentGoalID)], Title: g.Title, Description: g.Description, SuccessMeasure: g.SuccessMeasure, DueDate: dateString(g.DueDate), Status: g.Status})
	}
	projectTitles := map[string]string{}
	projectGoals := map[string][]string{}
	if links, err := h.Queries.ListProjectGoals(ctx, ws.ID); err == nil {
		for _, l := range links {
			projectGoals[uuidToString(l.ProjectID)] = append(projectGoals[uuidToString(l.ProjectID)], goalKeys[uuidToString(l.GoalID)])
		}
	}
	projects, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: ws.ID})
	if err != nil {
		return nil, fmt.Errorf("projects: %w", err)
	}
	for _, p := range projects {
		projectTitles[uuidToString(p.ID)] = p.Title
		tp := transferProject{Title: p.Title, Description: p.Description.String, Icon: p.Icon.String, Status: p.Status, Priority: p.Priority, StartDate: dateString(p.StartDate), DueDate: dateString(p.DueDate), Resources: []transferResource{}, Goals: projectGoals[uuidToString(p.ID)]}
		if tp.Goals == nil {
			tp.Goals = []string{}
		}
		if res, err := h.Queries.ListProjectResources(ctx, p.ID); err == nil {
			for _, r := range res {
				tp.Resources = append(tp.Resources, transferResource{Type: r.ResourceType, Label: r.Label.String, Ref: scrubJSON(r.ResourceRef), Position: r.Position})
			}
		}
		b.Projects = append(b.Projects, tp)
	}
	agentNames := map[string]string{}
	for _, a := range agents {
		agentNames[uuidToString(a.ID)] = a.Name
	}
	autopilots, err := h.Queries.ListAutopilotsForExport(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("autopilots: %w", err)
	}
	for _, a := range autopilots {
		ta := transferAutopilot{Title: a.Title, Description: a.Description.String, AssigneeType: a.AssigneeType, AssigneeAgent: agentNames[uuidToString(a.AssigneeID)], ExecutionMode: a.ExecutionMode, IssueTitleTemplate: a.IssueTitleTemplate.String, Project: projectTitles[uuidToString(a.ProjectID)], Triggers: []transferTrigger{}}
		if triggers, err := h.Queries.ListAutopilotTriggers(ctx, a.ID); err == nil {
			for _, t := range triggers {
				had := t.WebhookToken.Valid && t.WebhookToken.String != "" || t.SigningSecret.Valid && t.SigningSecret.String != ""
				ta.Triggers = append(ta.Triggers, transferTrigger{Kind: t.Kind, Enabled: t.Enabled, Cron: t.CronExpression.String, Timezone: t.Timezone.String, Label: t.Label.String, Provider: t.Provider, EventFilters: scrubJSON(t.EventFilters), EventMatchCriteria: t.EventMatchCriteria, WindowMinutes: t.WindowMinutes, HadSecret: had})
				if had {
					b.Manifest.Secrets = append(b.Manifest.Secrets, transferSecret{Scope: "autopilot_trigger", Name: a.Title, Key: t.Kind + " token"})
				}
			}
		}
		b.Autopilots = append(b.Autopilots, ta)
	}
	sources, err := h.Queries.ListTriageSources(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("triage sources: %w", err)
	}
	for _, s := range sources {
		b.TriageSources = append(b.TriageSources, transferTriageSource{Kind: s.Kind, Name: s.Name, Icon: s.Icon, Mode: s.Mode, AutoAccept: scrubJSON(s.AutoAccept), CapPerHour: s.CapPerHour, ExpiryDays: s.ExpiryDays})
		if s.TokenHash != "" {
			b.Manifest.Secrets = append(b.Manifest.Secrets, transferSecret{Scope: "triage_source", Name: s.Name, Key: "inbound token"})
		}
	}
	structures, err := h.Queries.ListOrgStructures(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("org structures: %w", err)
	}
	for _, s := range structures {
		if s.Status == orgStatusDissolved {
			continue
		}
		def := decodeOrgDefinition(s.Definition)
		for i := range def.Units {
			u := &def.Units[i]
			u.OwnerID, u.Deciders = "", nil
			members := make([]OrgMember, 0, len(u.Members))
			for _, m := range u.Members {
				if m.Type == "agent" {
					if n := agentNames[m.ID]; n != "" {
						members = append(members, OrgMember{Type: "agent", ID: "agent:" + n, Role: m.Role, RoleID: m.RoleID})
					}
				}
			}
			u.Members = members
			u.SquadID = ""
		}
		raw, _ := json.Marshal(def)
		b.Org = append(b.Org, transferOrg{Project: projectTitles[uuidToString(s.ProjectID)], Model: s.Model, Name: s.Name, EndCondition: s.EndCondition, BudgetUsdTicks: s.BudgetUsdTicks, Definition: raw})
	}
	if opts.IncludeNotes {
		notes, err := h.Queries.ListWorkspaceNotesForExport(ctx, ws.ID)
		if err != nil {
			return nil, fmt.Errorf("notes: %w", err)
		}
		for _, n := range notes {
			b.Notes = append(b.Notes, transferNote{Title: n.Title, Content: scrubText(n.Content), Tags: n.Tags, Pinned: n.Pinned})
		}
	}
	if opts.IncludeIssues {
		issues, err := h.Queries.ListIssuesForExport(ctx, ws.ID)
		if err != nil {
			return nil, fmt.Errorf("issues: %w", err)
		}
		labels := map[string][]string{}
		if rows, err := h.Queries.ListLabelsForExport(ctx, ws.ID); err == nil {
			for _, r := range rows {
				labels[uuidToString(r.IssueID)] = append(labels[uuidToString(r.IssueID)], r.Name)
			}
		}
		for _, i := range issues {
			ti := transferIssue{Title: i.Title, Description: scrubText(i.Description.String), Status: i.Status, Priority: i.Priority, Project: projectTitles[uuidToString(i.ProjectID)], GoalKey: goalKeys[uuidToString(i.GoalID)], Labels: labels[uuidToString(i.ID)]}
			if ti.Labels == nil {
				ti.Labels = []string{}
			}
			b.Issues = append(b.Issues, ti)
		}
	}
	b.Manifest.Counts = map[string]int{"agents": len(b.Agents), "skills": len(b.Skills), "permission_profiles": len(b.Profiles), "projects": len(b.Projects), "goals": len(b.Goals), "autopilots": len(b.Autopilots), "triage_sources": len(b.TriageSources), "org_structures": len(b.Org), "notes": len(b.Notes), "issues": len(b.Issues)}
	return b, nil
}

func zipTransferBundle(b *transferBundle) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest, _ := json.MarshalIndent(b.Manifest, "", "  ")
	body, _ := json.MarshalIndent(b, "", "  ")
	for name, content := range map[string][]byte{"manifest.json": manifest, "bundle.json": body} {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseTransferBundle(data []byte) (*transferBundle, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("the file is not a zip bundle")
	}
	for _, f := range zr.File {
		if f.Name != "bundle.json" {
			continue
		}
		if f.UncompressedSize64 > transferMaxUpload {
			return nil, errors.New("bundle.json exceeds the size limit")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		raw, err := io.ReadAll(io.LimitReader(rc, transferMaxUpload))
		if err != nil {
			return nil, err
		}
		var b transferBundle
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, errors.New("bundle.json is not valid")
		}
		if b.Manifest.FormatVersion != transferFormatVersion {
			return nil, fmt.Errorf("bundle format %d is not supported (this server reads %d)", b.Manifest.FormatVersion, transferFormatVersion)
		}
		return &b, nil
	}
	return nil, errors.New("the bundle carries no bundle.json")
}

// POST /api/workspace-transfer/export {include_issues, include_notes, template, name}
func (h *Handler) ExportWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, wsRaw, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var req struct {
		IncludeIssues bool   `json:"include_issues"`
		IncludeNotes  bool   `json:"include_notes"`
		Template      bool   `json:"template"`
		Name          string `json:"name"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = ws.Name
	}
	b, err := h.buildTransferBundle(r.Context(), ws, transferExportOptions{IncludeIssues: req.IncludeIssues, IncludeNotes: req.IncludeNotes, Template: req.Template, Name: name})
	if err != nil {
		slog.Warn("workspace export failed", "error", err, "workspace_id", wsRaw)
		writeError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}
	data, err := zipTransferBundle(b)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write the bundle")
		return
	}
	sum := sha256.Sum256(data)
	report, _ := json.Marshal(map[string]any{"counts": b.Manifest.Counts, "secrets": b.Manifest.Secrets, "bytes": len(data)})
	var stored []byte
	if req.Template {
		stored = data
	}
	run, err := h.Queries.CreateWorkspaceTransferRun(r.Context(), db.CreateWorkspaceTransferRunParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Direction: "export", Status: "completed", Name: name, Template: req.Template, SourceName: ws.Name, BundleSha256: hex.EncodeToString(sum[:]), Bundle: stored, Report: report, CreatedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("workspace export: run record failed", "error", err)
	} else {
		_, _ = h.Queries.FinishWorkspaceTransferRun(r.Context(), db.FinishWorkspaceTransferRunParams{ID: run.ID, Status: "completed", Report: report})
	}
	h.audit(r.Context(), wsUUID, "member", userID, AuditWorkspaceExported, "workspace", wsUUID, map[string]any{"run_id": uuidToString(run.ID), "counts": b.Manifest.Counts, "template": req.Template, "sha256": hex.EncodeToString(sum[:])}, nil)
	stamp := time.Now().UTC().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s.multica.zip\"", ws.Slug, stamp))
	w.Header().Set("X-Transfer-Run-ID", uuidToString(run.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// --- import --------------------------------------------------------------------------

type transferCollision struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ExistingID string `json:"existing_id"`
}

type transferPreview struct {
	Manifest   transferManifest    `json:"manifest"`
	Collisions []transferCollision `json:"collisions"`
	Secrets    []transferSecret    `json:"secrets"`
	Strategies []string            `json:"strategies"`
}

func (h *Handler) transferCollisions(ctx context.Context, wsUUID pgtype.UUID, b *transferBundle) []transferCollision {
	out := []transferCollision{}
	for _, p := range b.Profiles {
		if row, err := h.Queries.GetPermissionProfileByNameForImport(ctx, db.GetPermissionProfileByNameForImportParams{WorkspaceID: wsUUID, Name: p.Name}); err == nil {
			out = append(out, transferCollision{Kind: "permission_profile", Name: p.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, s := range b.Skills {
		if row, err := h.Queries.GetSkillByNameForImport(ctx, db.GetSkillByNameForImportParams{WorkspaceID: wsUUID, Name: s.Name}); err == nil {
			out = append(out, transferCollision{Kind: "skill", Name: s.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, a := range b.Agents {
		if row, err := h.Queries.GetAgentByNameForImport(ctx, db.GetAgentByNameForImportParams{WorkspaceID: wsUUID, Name: a.Name}); err == nil {
			out = append(out, transferCollision{Kind: "agent", Name: a.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, p := range b.Projects {
		if row, err := h.Queries.GetProjectByTitleForImport(ctx, db.GetProjectByTitleForImportParams{WorkspaceID: wsUUID, Title: p.Title}); err == nil {
			out = append(out, transferCollision{Kind: "project", Name: p.Title, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, g := range b.Goals {
		if row, err := h.Queries.GetGoalByTitleForImport(ctx, db.GetGoalByTitleForImportParams{WorkspaceID: wsUUID, Title: g.Title}); err == nil {
			out = append(out, transferCollision{Kind: "goal", Name: g.Title, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, a := range b.Autopilots {
		if row, err := h.Queries.GetAutopilotByTitleForImport(ctx, db.GetAutopilotByTitleForImportParams{WorkspaceID: wsUUID, Title: a.Title}); err == nil {
			out = append(out, transferCollision{Kind: "autopilot", Name: a.Title, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, s := range b.TriageSources {
		if row, err := h.Queries.GetTriageSourceByNameForImport(ctx, db.GetTriageSourceByNameForImportParams{WorkspaceID: wsUUID, Kind: s.Kind, Name: s.Name}); err == nil {
			out = append(out, transferCollision{Kind: "triage_source", Name: s.Name, ExistingID: uuidToString(row.ID)})
		}
	}
	for _, o := range b.Org {
		var existing db.OrgStructure
		var err error
		if o.Project == "" {
			existing, err = h.Queries.GetOrgStructureDefault(ctx, wsUUID)
		} else if p, perr := h.Queries.GetProjectByTitleForImport(ctx, db.GetProjectByTitleForImportParams{WorkspaceID: wsUUID, Title: o.Project}); perr == nil {
			existing, err = h.Queries.GetOrgStructureForProject(ctx, db.GetOrgStructureForProjectParams{WorkspaceID: wsUUID, ProjectID: p.ID})
		} else {
			err = perr
		}
		if err == nil {
			out = append(out, transferCollision{Kind: "org_structure", Name: nonEmpty(o.Project, "workspace default"), ExistingID: uuidToString(existing.ID)})
		}
	}
	return out
}

func (h *Handler) readTransferUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, transferMaxUpload)
	if err := r.ParseMultipartForm(transferMaxUpload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload or file exceeds the size limit")
		return nil, false
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `a bundle file is required (form field "file")`)
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read uploaded file")
		return nil, false
	}
	return data, true
}

// POST /api/workspace-transfer/preview (multipart: file)
func (h *Handler) PreviewWorkspaceImport(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, wsRaw, "workspace not found", "owner", "admin"); !ok {
		return
	}
	data, ok := h.readTransferUpload(w, r)
	if !ok {
		return
	}
	b, err := parseTransferBundle(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, transferPreview{Manifest: b.Manifest, Collisions: h.transferCollisions(r.Context(), wsUUID, b), Secrets: b.Manifest.Secrets, Strategies: transferStrategies})
}

type transferReport struct {
	Created        map[string]int      `json:"created"`
	Merged         map[string]int      `json:"merged"`
	Skipped        []transferCollision `json:"skipped"`
	SecretsPending []transferSecret    `json:"secrets_pending"`
	Warnings       []string            `json:"warnings"`
}

// POST /api/workspace-transfer/import (multipart: file, strategy, secrets)
func (h *Handler) ImportWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, wsRaw, "workspace not found", "owner", "admin"); !ok {
		return
	}
	data, ok := h.readTransferUpload(w, r)
	if !ok {
		return
	}
	strategy := r.FormValue("strategy")
	if strategy == "" {
		strategy = transferStrategyRename
	}
	if !containsStr(transferStrategies, strategy) {
		writeError(w, http.StatusBadRequest, "strategy must be one of: rename, merge, skip")
		return
	}
	secrets := map[string]map[string]string{}
	if raw := r.FormValue("secrets"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &secrets); err != nil {
			writeError(w, http.StatusBadRequest, "secrets must be a JSON object {agent: {KEY: value}}")
			return
		}
	}
	b, err := parseTransferBundle(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	report, runID, err := h.importTransferBundle(r.Context(), wsUUID, b, strategy, secrets, parseUUID(userID), data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "import failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run_id": runID, "report": report})
}

// importTransferBundle applies the bundle in one transaction and records the
// run. Members and tokens are never written: agents belong to the importer,
// autopilots start paused under the importer's authority, triggers come back
// disabled without their secrets, triage sources without their token.
func (h *Handler) importTransferBundle(ctx context.Context, wsUUID pgtype.UUID, b *transferBundle, strategy string, secrets map[string]map[string]string, importer pgtype.UUID, data []byte) (transferReport, string, error) {
	report := transferReport{Created: map[string]int{}, Merged: map[string]int{}, Skipped: []transferCollision{}, SecretsPending: []transferSecret{}, Warnings: []string{}}
	sum := sha256.Sum256(data)
	run, err := h.Queries.CreateWorkspaceTransferRun(ctx, db.CreateWorkspaceTransferRunParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Direction: "import", Status: "running", Name: b.Manifest.Name, Strategy: strategy, SourceName: b.Manifest.Source.Name, BundleSha256: hex.EncodeToString(sum[:]), Report: json.RawMessage("{}"), CreatedBy: importer,
	})
	if err != nil {
		return report, "", fmt.Errorf("record run: %w", err)
	}
	membersBefore, _ := h.Queries.CountMembersForTransfer(ctx, wsUUID)
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return report, uuidToString(run.ID), err
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)
	maps := &transferMaps{}
	if err := h.applyTransferBundle(ctx, q, wsUUID, b, strategy, secrets, importer, &report, maps); err != nil {
		raw, _ := json.Marshal(map[string]any{"error": err.Error()})
		_, _ = h.Queries.FinishWorkspaceTransferRun(ctx, db.FinishWorkspaceTransferRunParams{ID: run.ID, Status: "failed", Report: raw})
		return report, uuidToString(run.ID), err
	}
	if err := tx.Commit(ctx); err != nil {
		return report, uuidToString(run.ID), err
	}
	// Issues go through the issue service (numbering, labels, events) after
	// the configuration committed; a failed issue is a warning, not a rollback.
	h.importTransferIssues(ctx, wsUUID, b, strategy, importer, maps, &report)
	if after, _ := h.Queries.CountMembersForTransfer(ctx, wsUUID); after != membersBefore {
		report.Warnings = append(report.Warnings, "member count changed during import")
	}
	raw, _ := json.Marshal(report)
	_, _ = h.Queries.FinishWorkspaceTransferRun(ctx, db.FinishWorkspaceTransferRunParams{ID: run.ID, Status: "completed", Report: raw})
	h.audit(ctx, wsUUID, "member", uuidToString(importer), AuditWorkspaceImported, "workspace", wsUUID, map[string]any{"run_id": uuidToString(run.ID), "source": b.Manifest.Source.Name, "strategy": strategy, "created": report.Created, "merged": report.Merged, "skipped": len(report.Skipped), "secrets_pending": len(report.SecretsPending), "sha256": hex.EncodeToString(sum[:])}, nil)
	return report, uuidToString(run.ID), nil
}

// transferMaps carries the ids the post-commit issue import needs.
type transferMaps struct {
	projects map[string]pgtype.UUID
	goals    map[string]pgtype.UUID
}

func (h *Handler) applyTransferBundle(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID, b *transferBundle, strategy string, secrets map[string]map[string]string, importer pgtype.UUID, report *transferReport, maps *transferMaps) error {
	skip := func(kind, name, existing string) {
		report.Skipped = append(report.Skipped, transferCollision{Kind: kind, Name: name, ExistingID: existing})
	}
	renamed := func(kind, name string, exists func(string) bool) (string, bool) {
		if !exists(name) {
			return name, true
		}
		switch strategy {
		case transferStrategyRename:
			if candidate := name + transferRenameSuffix; !exists(candidate) {
				return candidate, true
			}
			skip(kind, name, "")
			return "", false
		default:
			return name, false
		}
	}

	// Permission profiles: by name; builtins match the workspace's own.
	profileIDs := map[string]pgtype.UUID{}
	if rows, err := q.ListPermissionProfiles(ctx, wsUUID); err == nil {
		for _, p := range rows {
			profileIDs[p.Name] = p.ID
		}
	}
	for _, p := range b.Profiles {
		if existing, ok := profileIDs[p.Name]; ok {
			switch strategy {
			case transferStrategyMerge:
				if _, err := q.UpdatePermissionProfileRules(ctx, db.UpdatePermissionProfileRulesParams{ID: existing, Description: p.Description, ReadOnly: p.ReadOnly, DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets)}); err != nil {
					return fmt.Errorf("merge profile %q: %w", p.Name, err)
				}
				report.Merged["permission_profiles"]++
			case transferStrategySkip:
				skip("permission_profile", p.Name, uuidToString(existing))
			default:
				name := p.Name + transferRenameSuffix
				row, err := q.CreatePermissionProfile(ctx, db.CreatePermissionProfileParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: name, Description: p.Description, ReadOnly: p.ReadOnly, DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets)})
				if err != nil {
					return fmt.Errorf("create profile %q: %w", name, err)
				}
				profileIDs[p.Name] = row.ID
				report.Created["permission_profiles"]++
			}
			continue
		}
		row, err := q.CreatePermissionProfile(ctx, db.CreatePermissionProfileParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: p.Name, Description: p.Description, ReadOnly: p.ReadOnly, DeniedPaths: profileJSON(p.DeniedPaths), AllowedCommands: profileJSON(p.AllowedCommands), HiddenSecrets: profileJSON(p.HiddenSecrets)})
		if err != nil {
			return fmt.Errorf("create profile %q: %w", p.Name, err)
		}
		profileIDs[p.Name] = row.ID
		report.Created["permission_profiles"]++
	}

	// Skills with files.
	skillIDs := map[string]pgtype.UUID{}
	skillExists := func(name string) bool {
		_, err := q.GetSkillByNameForImport(ctx, db.GetSkillByNameForImportParams{WorkspaceID: wsUUID, Name: name})
		return err == nil
	}
	for _, s := range b.Skills {
		if existing, err := q.GetSkillByNameForImport(ctx, db.GetSkillByNameForImportParams{WorkspaceID: wsUUID, Name: s.Name}); err == nil {
			skillIDs[s.Name] = existing.ID
			switch strategy {
			case transferStrategyMerge:
				if _, err := q.UpdateSkill(ctx, db.UpdateSkillParams{ID: existing.ID, Description: pgtype.Text{String: s.Description, Valid: true}, Content: pgtype.Text{String: s.Content, Valid: true}, Config: s.Config}); err != nil {
					return fmt.Errorf("merge skill %q: %w", s.Name, err)
				}
				for _, f := range s.Files {
					if _, err := q.UpsertSkillFile(ctx, db.UpsertSkillFileParams{SkillID: existing.ID, Path: f.Path, Content: f.Content}); err != nil {
						return fmt.Errorf("merge skill file %q: %w", f.Path, err)
					}
				}
				report.Merged["skills"]++
				continue
			case transferStrategySkip:
				skip("skill", s.Name, uuidToString(existing.ID))
				continue
			}
		}
		name, ok := renamed("skill", s.Name, skillExists)
		if !ok {
			continue
		}
		config := s.Config
		if len(config) == 0 {
			config = json.RawMessage("{}")
		}
		row, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: wsUUID, Name: name, Description: s.Description, Content: s.Content, Config: config, CreatedBy: importer})
		if err != nil {
			return fmt.Errorf("create skill %q: %w", name, err)
		}
		if s.Status == "draft" {
			_, _ = q.UpdateSkill(ctx, db.UpdateSkillParams{ID: row.ID, Status: pgtype.Text{String: "draft", Valid: true}})
		}
		for _, f := range s.Files {
			if _, err := q.UpsertSkillFile(ctx, db.UpsertSkillFileParams{SkillID: row.ID, Path: f.Path, Content: f.Content}); err != nil {
				return fmt.Errorf("skill file %q: %w", f.Path, err)
			}
		}
		skillIDs[s.Name] = row.ID
		report.Created["skills"]++
	}

	// Agents: owned by the importer, no runtime bound, env declared not valued.
	agentIDs := map[string]pgtype.UUID{}
	agentExists := func(name string) bool {
		_, err := q.GetAgentByNameForImport(ctx, db.GetAgentByNameForImportParams{WorkspaceID: wsUUID, Name: name})
		return err == nil
	}
	for _, a := range b.Agents {
		scoped, _ := json.Marshal(a.ScopedEnvKeys)
		env := map[string]string{}
		for _, k := range a.EnvKeys {
			if v, ok := secrets[a.Name][k]; ok && v != "" {
				env[k] = v
			} else {
				report.SecretsPending = append(report.SecretsPending, transferSecret{Scope: "agent", Name: a.Name, Key: k, Scoped: containsStr(a.ScopedEnvKeys, k)})
			}
		}
		envRaw, _ := json.Marshal(env)
		profileID := profileIDs[a.PermissionProfile]
		if existing, err := q.GetAgentByNameForImport(ctx, db.GetAgentByNameForImportParams{WorkspaceID: wsUUID, Name: a.Name}); err == nil {
			agentIDs[a.Name] = existing.ID
			switch strategy {
			case transferStrategyMerge:
				if err := q.MergeImportedAgent(ctx, db.MergeImportedAgentParams{ID: existing.ID, WorkspaceID: wsUUID, Description: a.Description, Instructions: a.Instructions, Model: pgtype.Text{String: a.Model, Valid: a.Model != ""}, ThinkingLevel: pgtype.Text{String: a.ThinkingLevel, Valid: a.ThinkingLevel != ""}, McpConfig: a.McpConfig, RuntimeConfig: a.RuntimeConfig, CustomArgs: a.CustomArgs, ConversationStarters: a.ConversationStarters, TrustMode: nonEmpty(a.TrustMode, existing.TrustMode), EffectMode: nonEmpty(a.EffectMode, existing.EffectMode), ScopedEnvKeys: scoped}); err != nil {
					return fmt.Errorf("merge agent %q: %w", a.Name, err)
				}
				for _, name := range a.Skills {
					if id, ok := skillIDs[name]; ok {
						_ = q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: existing.ID, SkillID: id})
					}
				}
				report.Merged["agents"]++
				continue
			case transferStrategySkip:
				skip("agent", a.Name, uuidToString(existing.ID))
				continue
			}
		}
		name, ok := renamed("agent", a.Name, agentExists)
		if !ok {
			continue
		}
		row, err := q.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID: wsUUID, Name: name, Description: a.Description, RuntimeMode: nonEmpty(a.RuntimeMode, "local"), RuntimeConfig: nonEmptyJSON(a.RuntimeConfig), Visibility: nonEmpty(a.Visibility, "public"), MaxConcurrentTasks: maxInt32(a.MaxConcurrentTasks, 1), OwnerID: importer,
			Instructions: a.Instructions, CustomEnv: envRaw, CustomArgs: nonEmptyJSON(a.CustomArgs), McpConfig: nonEmptyJSON(a.McpConfig), Model: pgtype.Text{String: a.Model, Valid: a.Model != ""}, ThinkingLevel: pgtype.Text{String: a.ThinkingLevel, Valid: a.ThinkingLevel != ""},
			ServiceTier: pgtype.Text{String: a.ServiceTier, Valid: a.ServiceTier != ""}, ConversationStarters: nonEmptyJSON(a.ConversationStarters), ComposioToolkitAllowlist: []string{}, PermissionMode: "private", RuntimeRouting: "fixed",
		})
		if err != nil {
			return fmt.Errorf("create agent %q: %w", name, err)
		}
		if err := q.SetImportedAgentModes(ctx, db.SetImportedAgentModesParams{ID: row.ID, WorkspaceID: wsUUID, TrustMode: nonEmpty(a.TrustMode, "propose"), EffectMode: nonEmpty(a.EffectMode, row.EffectMode), ScopedEnvKeys: scoped, PermissionProfileID: profileID, CustomEnv: envRaw}); err != nil {
			return fmt.Errorf("agent modes %q: %w", name, err)
		}
		for _, sname := range a.Skills {
			if id, ok := skillIDs[sname]; ok {
				_ = q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: row.ID, SkillID: id})
			}
		}
		for _, v := range a.Versions {
			var ids []string
			for _, sname := range v.Skills {
				if id, ok := skillIDs[sname]; ok {
					ids = append(ids, uuidToString(id))
				}
			}
			idsRaw, _ := json.Marshal(ids)
			if _, err := q.CreateAgentVersion(ctx, db.CreateAgentVersionParams{WorkspaceID: wsUUID, AgentID: row.ID, Instructions: v.Instructions, Model: v.Model, SkillIds: idsRaw, ToolConfig: nonEmptyJSON(v.ToolConfig), Note: nonEmpty(v.Note, "imported"), CreatedByType: "member", CreatedByID: importer}); err != nil {
				return fmt.Errorf("agent version %q: %w", name, err)
			}
		}
		agentIDs[a.Name] = row.ID
		report.Created["agents"]++
	}

	// Goals, parents first (the bundle lists them in creation order).
	goalIDs := map[string]pgtype.UUID{}
	for _, g := range b.Goals {
		if existing, err := q.GetGoalByTitleForImport(ctx, db.GetGoalByTitleForImportParams{WorkspaceID: wsUUID, Title: g.Title}); err == nil {
			goalIDs[g.Key] = existing.ID
			switch strategy {
			case transferStrategyMerge:
				if _, err := q.UpdateGoal(ctx, db.UpdateGoalParams{ID: existing.ID, WorkspaceID: wsUUID, ParentGoalID: existing.ParentGoalID, Title: existing.Title, Description: g.Description, SuccessMeasure: g.SuccessMeasure, DueDate: parseDateOrEmpty(g.DueDate), OwnerID: existing.OwnerID, Status: existing.Status}); err != nil {
					return fmt.Errorf("merge goal %q: %w", g.Title, err)
				}
				report.Merged["goals"]++
				continue
			case transferStrategySkip:
				skip("goal", g.Title, uuidToString(existing.ID))
				continue
			}
		}
		title := g.Title
		if _, err := q.GetGoalByTitleForImport(ctx, db.GetGoalByTitleForImportParams{WorkspaceID: wsUUID, Title: title}); err == nil {
			title += transferRenameSuffix
		}
		status := g.Status
		if status == goalStatusActive {
			status = goalStatusDraft // an active goal needs an owner of this workspace
		}
		row, err := q.CreateGoal(ctx, db.CreateGoalParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, ParentGoalID: goalIDs[g.ParentKey], Title: title, Description: g.Description, SuccessMeasure: g.SuccessMeasure, DueDate: parseDateOrEmpty(g.DueDate), Status: nonEmpty(status, goalStatusDraft)})
		if err != nil {
			return fmt.Errorf("create goal %q: %w", title, err)
		}
		goalIDs[g.Key] = row.ID
		report.Created["goals"]++
	}

	// Projects with resources and goal links.
	projectIDs := map[string]pgtype.UUID{}
	projectExists := func(title string) bool {
		_, err := q.GetProjectByTitleForImport(ctx, db.GetProjectByTitleForImportParams{WorkspaceID: wsUUID, Title: title})
		return err == nil
	}
	for _, p := range b.Projects {
		if existing, err := q.GetProjectByTitleForImport(ctx, db.GetProjectByTitleForImportParams{WorkspaceID: wsUUID, Title: p.Title}); err == nil {
			projectIDs[p.Title] = existing.ID
			switch strategy {
			case transferStrategyMerge:
				if err := q.MergeImportedProject(ctx, db.MergeImportedProjectParams{ID: existing.ID, WorkspaceID: wsUUID, Description: pgtype.Text{String: p.Description, Valid: p.Description != ""}, Icon: pgtype.Text{String: p.Icon, Valid: p.Icon != ""}, Status: nonEmpty(p.Status, existing.Status), Priority: nonEmpty(p.Priority, existing.Priority)}); err != nil {
					return fmt.Errorf("merge project %q: %w", p.Title, err)
				}
				for _, key := range p.Goals {
					if gid, ok := goalIDs[key]; ok {
						_ = q.AddProjectGoal(ctx, db.AddProjectGoalParams{WorkspaceID: wsUUID, ProjectID: existing.ID, GoalID: gid})
					}
				}
				report.Merged["projects"]++
				continue
			case transferStrategySkip:
				skip("project", p.Title, uuidToString(existing.ID))
				continue
			}
		}
		title, ok := renamed("project", p.Title, projectExists)
		if !ok {
			continue
		}
		row, err := q.CreateProject(ctx, db.CreateProjectParams{WorkspaceID: wsUUID, Title: title, Description: pgtype.Text{String: p.Description, Valid: p.Description != ""}, Icon: pgtype.Text{String: p.Icon, Valid: p.Icon != ""}, Status: nonEmpty(p.Status, "planned"), Priority: nonEmpty(p.Priority, "none"), StartDate: parseDateOrEmpty(p.StartDate), DueDate: parseDateOrEmpty(p.DueDate)})
		if err != nil {
			return fmt.Errorf("create project %q: %w", title, err)
		}
		for _, r := range p.Resources {
			if _, err := q.CreateProjectResource(ctx, db.CreateProjectResourceParams{ProjectID: row.ID, WorkspaceID: wsUUID, ResourceType: r.Type, ResourceRef: nonEmptyJSON(r.Ref), Label: pgtype.Text{String: r.Label, Valid: r.Label != ""}, Position: r.Position, CreatedBy: importer}); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("project %q: resource %q not imported: %v", title, r.Label, err))
			}
		}
		for _, key := range p.Goals {
			if gid, ok := goalIDs[key]; ok {
				_ = q.AddProjectGoal(ctx, db.AddProjectGoalParams{WorkspaceID: wsUUID, ProjectID: row.ID, GoalID: gid})
			}
		}
		projectIDs[p.Title] = row.ID
		report.Created["projects"]++
	}

	// Autopilots: paused under the importer's authority; triggers disabled, secrets to regenerate.
	for _, a := range b.Autopilots {
		if existing, err := q.GetAutopilotByTitleForImport(ctx, db.GetAutopilotByTitleForImportParams{WorkspaceID: wsUUID, Title: a.Title}); err == nil {
			if strategy != transferStrategyRename {
				skip("autopilot", a.Title, uuidToString(existing.ID))
				continue
			}
		}
		title := a.Title
		if _, err := q.GetAutopilotByTitleForImport(ctx, db.GetAutopilotByTitleForImportParams{WorkspaceID: wsUUID, Title: title}); err == nil {
			title += transferRenameSuffix
		}
		assigneeType, assigneeID := a.AssigneeType, agentIDs[a.AssigneeAgent]
		if assigneeType == "agent" && !assigneeID.Valid {
			report.Warnings = append(report.Warnings, fmt.Sprintf("autopilot %q: assignee agent %q not found; assigned to you", a.Title, a.AssigneeAgent))
			assigneeType, assigneeID = "member", importer
		}
		if assigneeType == "" || assigneeType == "member" {
			assigneeType, assigneeID = "member", importer
		}
		row, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{WorkspaceID: wsUUID, Title: title, AssigneeType: assigneeType, AssigneeID: assigneeID, Status: "paused", ExecutionMode: nonEmpty(a.ExecutionMode, "create_issue"), CreatedByType: "member", CreatedByID: importer, Description: pgtype.Text{String: a.Description, Valid: a.Description != ""}, IssueTitleTemplate: pgtype.Text{String: a.IssueTitleTemplate, Valid: a.IssueTitleTemplate != ""}, ProjectID: projectIDs[a.Project]})
		if err != nil {
			return fmt.Errorf("create autopilot %q: %w", title, err)
		}
		for _, t := range a.Triggers {
			if _, err := q.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{AutopilotID: row.ID, Kind: t.Kind, Enabled: false, CronExpression: pgtype.Text{String: t.Cron, Valid: t.Cron != ""}, Timezone: pgtype.Text{String: t.Timezone, Valid: t.Timezone != ""}, Label: pgtype.Text{String: t.Label, Valid: t.Label != ""}, Provider: pgtype.Text{String: t.Provider, Valid: t.Provider != ""}, EventFilters: nonEmptyJSON(t.EventFilters), EventMatchCriteria: pgtype.Text{String: t.EventMatchCriteria, Valid: t.EventMatchCriteria != ""}, WindowMinutes: pgtype.Int4{Int32: t.WindowMinutes, Valid: true}, CreatedByType: pgtype.Text{String: "member", Valid: true}, CreatedByID: importer}); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("autopilot %q: trigger %s not imported: %v", title, t.Kind, err))
				continue
			}
			if t.HadSecret {
				report.SecretsPending = append(report.SecretsPending, transferSecret{Scope: "autopilot_trigger", Name: title, Key: t.Kind + " token"})
			}
		}
		report.Created["autopilots"]++
	}

	// Triage sources without their inbound token.
	for _, s := range b.TriageSources {
		if existing, err := q.GetTriageSourceByNameForImport(ctx, db.GetTriageSourceByNameForImportParams{WorkspaceID: wsUUID, Kind: s.Kind, Name: s.Name}); err == nil {
			switch strategy {
			case transferStrategyMerge:
				if err := q.MergeImportedTriageSource(ctx, db.MergeImportedTriageSourceParams{ID: existing.ID, WorkspaceID: wsUUID, Mode: nonEmpty(s.Mode, existing.Mode), AutoAccept: nonEmptyJSON(s.AutoAccept), CapPerHour: s.CapPerHour, ExpiryDays: s.ExpiryDays}); err != nil {
					return fmt.Errorf("merge triage source %q: %w", s.Name, err)
				}
				report.Merged["triage_sources"]++
			default:
				skip("triage_source", s.Name, uuidToString(existing.ID))
			}
			continue
		}
		if _, err := q.CreateTriageSourceForImport(ctx, db.CreateTriageSourceForImportParams{WorkspaceID: wsUUID, Kind: s.Kind, RefID: dbid.NewV7(), Name: s.Name, Icon: s.Icon, Mode: nonEmpty(s.Mode, "gate"), AutoAccept: nonEmptyJSON(s.AutoAccept), CapPerHour: s.CapPerHour, ExpiryDays: s.ExpiryDays, CreatedByID: importer}); err != nil {
			return fmt.Errorf("create triage source %q: %w", s.Name, err)
		}
		report.Created["triage_sources"]++
	}

	// Org structures: drafts owned by the importer, agents remapped by name.
	for _, o := range b.Org {
		var projectID pgtype.UUID
		if o.Project != "" {
			pid, ok := projectIDs[o.Project]
			if !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("org structure for project %q skipped: project not imported", o.Project))
				continue
			}
			projectID = pid
		}
		var existing db.OrgStructure
		var err error
		if projectID.Valid {
			existing, err = q.GetOrgStructureForProject(ctx, db.GetOrgStructureForProjectParams{WorkspaceID: wsUUID, ProjectID: projectID})
		} else {
			existing, err = q.GetOrgStructureDefault(ctx, wsUUID)
		}
		if err == nil {
			skip("org_structure", nonEmpty(o.Project, "workspace default"), uuidToString(existing.ID))
			continue
		}
		def := decodeOrgDefinition(o.Definition)
		for i := range def.Units {
			u := &def.Units[i]
			u.OwnerID = uuidToString(importer)
			members := make([]OrgMember, 0, len(u.Members)+1)
			members = append(members, OrgMember{Type: "member", ID: uuidToString(importer), Role: "owner"})
			for _, m := range u.Members {
				if m.Type == "agent" && strings.HasPrefix(m.ID, "agent:") {
					if id, ok := agentIDs[strings.TrimPrefix(m.ID, "agent:")]; ok {
						members = append(members, OrgMember{Type: "agent", ID: uuidToString(id), Role: m.Role, RoleID: m.RoleID})
					}
				}
			}
			u.Members = members
			if u.properties()["external_effects"] {
				u.Deciders = map[string]string{"money": uuidToString(importer), "outbound_data": uuidToString(importer), "external_message": uuidToString(importer)}
			}
		}
		raw, _ := json.Marshal(def)
		id, revID := dbid.NewV7(), dbid.NewV7()
		row, err := q.CreateOrgStructure(ctx, db.CreateOrgStructureParams{ID: id, WorkspaceID: wsUUID, ProjectID: projectID, Model: o.Model, Name: o.Name, Status: orgStatusDraft, RevisionID: revID, Definition: raw, OwnerID: importer, EndCondition: o.EndCondition, BudgetUsdTicks: o.BudgetUsdTicks, CreatedBy: importer})
		if err != nil {
			return fmt.Errorf("create org structure %q: %w", o.Name, err)
		}
		if _, err := q.CreateOrgRevision(ctx, db.CreateOrgRevisionParams{ID: revID, WorkspaceID: wsUUID, StructureID: row.ID, Revision: 1, Model: o.Model, Status: orgStatusDraft, Definition: raw, ChangedBy: importer, Note: "imported from " + b.Manifest.Source.Name}); err != nil {
			return fmt.Errorf("org revision %q: %w", o.Name, err)
		}
		report.Created["org_structures"]++
	}

	// Notes and issues carry no collision rule: they are content.
	// A note or issue whose title already exists is the same content: skip and
	// merge leave it alone, rename brings a suffixed copy.
	for _, n := range b.Notes {
		tags := n.Tags
		if tags == nil {
			tags = []string{}
		}
		title := n.Title
		if count, err := q.CountWorkspaceNotesByTitleForImport(ctx, db.CountWorkspaceNotesByTitleForImportParams{WorkspaceID: wsUUID, Title: title}); err == nil && count > 0 {
			if strategy != transferStrategyRename {
				report.Skipped = append(report.Skipped, transferCollision{Kind: "note", Name: title})
				continue
			}
			title += transferRenameSuffix
		}
		if _, err := q.CreateWorkspaceNote(ctx, db.CreateWorkspaceNoteParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: title, Content: n.Content, Tags: tags, Source: "manual", Pinned: n.Pinned, CreatedByType: "member", CreatedByID: importer}); err != nil {
			return fmt.Errorf("create note %q: %w", n.Title, err)
		}
		report.Created["notes"]++
	}
	maps.projects, maps.goals = projectIDs, goalIDs
	return nil
}

func (h *Handler) importTransferIssues(ctx context.Context, wsUUID pgtype.UUID, b *transferBundle, strategy string, importer pgtype.UUID, maps *transferMaps, report *transferReport) {
	if len(b.Issues) == 0 || h.IssueService == nil {
		return
	}
	labelIDs := map[string]pgtype.UUID{}
	for _, i := range b.Issues {
		title := i.Title
		if count, err := h.Queries.CountIssuesByTitleForImport(ctx, db.CountIssuesByTitleForImportParams{WorkspaceID: wsUUID, Title: title}); err == nil && count > 0 {
			if strategy != transferStrategyRename {
				report.Skipped = append(report.Skipped, transferCollision{Kind: "issue", Name: title})
				continue
			}
			title += transferRenameSuffix
		}
		var ids []pgtype.UUID
		for _, name := range i.Labels {
			id, ok := labelIDs[name]
			if !ok {
				if row, err := h.Queries.GetLabelByNameForImport(ctx, db.GetLabelByNameForImportParams{WorkspaceID: wsUUID, Name: name}); err == nil {
					id = row.ID
				} else if row, err := h.Queries.CreateLabel(ctx, db.CreateLabelParams{WorkspaceID: wsUUID, ResourceType: "issue", Name: name, Description: "", Color: "#6b7280"}); err == nil {
					id = row.ID
				} else {
					continue
				}
				labelIDs[name] = id
			}
			ids = append(ids, id)
		}
		res, err := h.IssueService.Create(ctx, service.IssueCreateParams{WorkspaceID: wsUUID, Title: title, Description: pgtype.Text{String: i.Description, Valid: i.Description != ""}, Status: nonEmpty(i.Status, "todo"), Priority: nonEmpty(i.Priority, "medium"), CreatorType: "member", CreatorID: importer, ProjectID: maps.projects[i.Project], LabelIDs: ids, AllowDuplicate: true}, service.IssueCreateOpts{})
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("issue %q not imported: %v", i.Title, err))
			continue
		}
		if gid, ok := maps.goals[i.GoalKey]; ok {
			_ = h.Queries.SetIssueGoalForImport(ctx, db.SetIssueGoalForImportParams{ID: res.Issue.ID, WorkspaceID: wsUUID, GoalID: gid})
		}
		report.Created["issues"]++
	}
}

func nonEmptyJSON(raw json.RawMessage) []byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}")
	}
	return raw
}

func maxInt32(v, floor int32) int32 {
	if v < floor {
		return floor
	}
	return v
}

// --- runs and templates --------------------------------------------------------------

// GET /api/workspace-transfer/runs
func (h *Handler) ListWorkspaceTransferRuns(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, wsRaw, "workspace not found", "owner", "admin"); !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceTransferRuns(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, map[string]any{"id": uuidToString(x.ID), "direction": x.Direction, "status": x.Status, "name": x.Name, "template": x.Template, "strategy": x.Strategy, "source_name": x.SourceName, "bundle_sha256": x.BundleSha256, "report": json.RawMessage(x.Report), "created_by": uuidToPtr(x.CreatedBy), "created_at": timestampToString(x.CreatedAt), "completed_at": tsStringPtr(x.CompletedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// GET /api/workspace-templates: template exports of the workspaces the user belongs to.
func (h *Handler) ListWorkspaceTemplates(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceTemplatesForUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, map[string]any{"id": uuidToString(x.ID), "name": x.Name, "source_name": x.SourceName, "workspace_name": x.WorkspaceName, "report": json.RawMessage(x.Report), "created_at": timestampToString(x.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// applyWorkspaceTemplate seeds a freshly created workspace from a template
// run the creator has access to. Failure never undoes the workspace.
func (h *Handler) applyWorkspaceTemplate(ctx context.Context, wsUUID pgtype.UUID, runID string, userID string) (map[string]any, error) {
	id, err := util.ParseUUID(runID)
	if err != nil {
		return nil, errors.New("invalid template_run_id")
	}
	run, err := h.Queries.GetWorkspaceTransferRun(ctx, id)
	if err != nil || !run.Template || len(run.Bundle) == 0 {
		return nil, errors.New("template not found")
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(userID), WorkspaceID: run.WorkspaceID}); err != nil {
		return nil, errors.New("template not found")
	}
	b, err := parseTransferBundle(run.Bundle)
	if err != nil {
		return nil, err
	}
	report, importRunID, err := h.importTransferBundle(ctx, wsUUID, b, transferStrategyRename, nil, parseUUID(userID), run.Bundle)
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": importRunID, "report": report}, nil
}
