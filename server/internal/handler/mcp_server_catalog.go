package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// The catalogue of Vigil's MCP server. Every entry is a leaf: one HTTP
// operation of the API, with an explicit risk class (never inferred from
// words), its parameters and where each goes (path, query, body). The
// granular surface exposes the leaves as they are; the compound surface
// folds them by group into one tool with an `action` argument. Both run
// the same decisions and the same dispatch.
//
// Deliberately absent (the "never exposed" list, after OpenMausBot's
// bounded control API): deletes of any kind, member and role management,
// secrets and agent environments, approval and gate resolution, billing
// and spend redemption, workspace settings. A client that needs them has a
// person open the app.

type mcpParam struct {
	Name     string
	Type     string // string | integer | boolean | array | object
	Desc     string
	Required bool
	In       string // path | query | body
	Enum     []string
	Items    string // element type for arrays
}

type mcpLeaf struct {
	Name        string
	Group       string // compound tool name
	Action      string // action within the group
	Description string
	Risk        string // mcpgov risk class
	Method      string
	Path        string // with {name} placeholders; {workspace_id} is filled by the server
	Params      []mcpParam
	AgentOnly   bool
}

var (
	pID       = mcpParam{Name: "id", Type: "string", Desc: "Issue id or identifier (ONE-42).", Required: true, In: "path"}
	pLimit    = mcpParam{Name: "limit", Type: "integer", Desc: "Maximum results.", In: "query"}
	pOffset   = mcpParam{Name: "offset", Type: "integer", Desc: "Results to skip.", In: "query"}
	pIssueRef = mcpParam{Name: "issue_id", Type: "string", Desc: "Issue id or identifier (ONE-42).", Required: true, In: "path"}
)

var mcpLeaves = []mcpLeaf{
	// ---- Issues -------------------------------------------------------------
	{Name: "issue_list", Group: "vigil_issue", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues",
		Description: "List issues of the workspace, newest first, with filters.",
		Params: []mcpParam{
			{Name: "q", Type: "string", Desc: "Words to look for in title and description.", In: "query"},
			{Name: "status", Type: "string", Desc: "One status (backlog, todo, in_progress, in_review, done, blocked, cancelled) or a workspace status.", In: "query"},
			{Name: "priority", Type: "string", Desc: "urgent, high, medium, low or none.", In: "query"},
			{Name: "assignee_id", Type: "string", Desc: "Member or agent id.", In: "query"},
			{Name: "project_id", Type: "string", Desc: "Project id.", In: "query"},
			{Name: "open_only", Type: "boolean", Desc: "Only issues not done or cancelled.", In: "query"},
			pLimit, pOffset,
		}},
	{Name: "issue_search", Group: "vigil_issue", Action: "search", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/search",
		Description: "Full-text search over issues.",
		Params:      []mcpParam{{Name: "q", Type: "string", Desc: "Search words.", Required: true, In: "query"}, pLimit}},
	{Name: "issue_get", Group: "vigil_issue", Action: "get", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{id}",
		Description: "Read one issue: fields, assignee, project, dates.", Params: []mcpParam{pID}},
	{Name: "issue_create", Group: "vigil_issue", Action: "create", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues",
		Description: "Create an issue. Assign an agent (assignee_type agent) to have it run.",
		Params: []mcpParam{
			{Name: "title", Type: "string", Desc: "Title.", Required: true, In: "body"},
			{Name: "description", Type: "string", Desc: "Markdown description.", In: "body"},
			{Name: "status", Type: "string", Desc: "Initial status (default: the workspace default).", In: "body"},
			{Name: "priority", Type: "string", Desc: "urgent, high, medium, low or none.", In: "body"},
			{Name: "assignee_type", Type: "string", Desc: "member or agent.", In: "body", Enum: []string{"member", "agent"}},
			{Name: "assignee_id", Type: "string", Desc: "Member or agent id.", In: "body"},
			{Name: "project_id", Type: "string", Desc: "Project id.", In: "body"},
			{Name: "parent_issue_id", Type: "string", Desc: "Parent issue id, for a sub-issue.", In: "body"},
			{Name: "due_date", Type: "string", Desc: "Due date, YYYY-MM-DD.", In: "body"},
		}},
	{Name: "issue_update", Group: "vigil_issue", Action: "update", Risk: mcpgov.RiskInternalWrite, Method: "PUT", Path: "/api/issues/{id}",
		Description: "Update an issue's fields. A status change goes through the workspace's transition rules and may be held for approval.",
		Params: []mcpParam{pID,
			{Name: "title", Type: "string", Desc: "Title.", In: "body"},
			{Name: "description", Type: "string", Desc: "Markdown description.", In: "body"},
			{Name: "status", Type: "string", Desc: "New status.", In: "body"},
			{Name: "priority", Type: "string", Desc: "urgent, high, medium, low or none.", In: "body"},
			{Name: "assignee_type", Type: "string", Desc: "member or agent.", In: "body", Enum: []string{"member", "agent"}},
			{Name: "assignee_id", Type: "string", Desc: "Member or agent id.", In: "body"},
			{Name: "project_id", Type: "string", Desc: "Project id.", In: "body"},
			{Name: "due_date", Type: "string", Desc: "Due date, YYYY-MM-DD.", In: "body"},
		}},
	{Name: "issue_comments", Group: "vigil_issue", Action: "comments", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{id}/comments",
		Description: "Read an issue's comments, oldest first.", Params: []mcpParam{pID}},
	{Name: "issue_comment", Group: "vigil_issue", Action: "comment", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{id}/comments",
		Description: "Post a comment on an issue. A comment on an agent's issue queues a run for it.",
		Params:      []mcpParam{pID, {Name: "content", Type: "string", Desc: "Markdown comment.", Required: true, In: "body"}}},
	{Name: "issue_timeline", Group: "vigil_issue", Action: "timeline", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{id}/timeline",
		Description: "The issue's timeline: comments, status changes, runs.", Params: []mcpParam{pID}},
	{Name: "issue_labels", Group: "vigil_issue", Action: "labels", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{id}/labels",
		Description: "Labels on an issue.", Params: []mcpParam{pID}},

	// ---- Goal loop ------------------------------------------------------------
	{Name: "goal_get", Group: "vigil_goal", Action: "get", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{issue_id}/goal",
		Description: "The issue's goal and run-chain state: status, continuation, blocker, evidence, pending question.", Params: []mcpParam{pIssueRef}},
	{Name: "goal_set", Group: "vigil_goal", Action: "set", Risk: mcpgov.RiskInternalWrite, Method: "PUT", Path: "/api/issues/{issue_id}/goal",
		Description: "Write the issue's definition of done and the ceiling of continuations (1-20).",
		Params: []mcpParam{pIssueRef,
			{Name: "goal", Type: "string", Desc: "Definition of done.", Required: true, In: "body"},
			{Name: "max_continuations", Type: "integer", Desc: "1 to 20.", In: "body"},
		}},
	{Name: "goal_pause", Group: "vigil_goal", Action: "pause", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{issue_id}/goal/pause",
		Description: "Pause the goal loop on an issue: no further run is queued.", Params: []mcpParam{pIssueRef}},
	{Name: "goal_resume", Group: "vigil_goal", Action: "resume", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{issue_id}/goal/resume",
		Description: "Resume the goal loop with a fresh allowance; queues a run when the issue is an agent's.", Params: []mcpParam{pIssueRef}},
	{Name: "goal_answer", Group: "vigil_goal", Action: "answer", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{issue_id}/goal/answer",
		Description: "Answer the agent's pending question; the follow-up run receives it.",
		Params:      []mcpParam{pIssueRef, {Name: "answer", Type: "string", Desc: "The answer (an option number or text).", Required: true, In: "body"}}},
	{Name: "goal_question", Group: "vigil_goal", Action: "ask", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{issue_id}/goal/question", AgentOnly: true,
		Description: "For a run: ask the team a typed question and stop; the chain waits for the answer.",
		Params: []mcpParam{pIssueRef,
			{Name: "question", Type: "string", Desc: "The question.", Required: true, In: "body"},
			{Name: "kind", Type: "string", Desc: "text or choice.", In: "body", Enum: []string{"text", "choice"}},
			{Name: "options", Type: "array", Items: "string", Desc: "Options for a choice question.", In: "body"},
		}},

	// ---- Brain -------------------------------------------------------------------
	{Name: "note_list", Group: "vigil_brain", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspace/notes",
		Description: "List or search the workspace Brain: shared notes every run reads.",
		Params: []mcpParam{
			{Name: "search", Type: "string", Desc: "Words to look for.", In: "query"},
			{Name: "tag", Type: "string", Desc: "Only notes with this tag.", In: "query"},
			{Name: "archived", Type: "boolean", Desc: "Include archived notes.", In: "query"},
			pLimit,
		}},
	{Name: "note_get", Group: "vigil_brain", Action: "get", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspace/notes/{id}",
		Description: "Read one Brain note.", Params: []mcpParam{{Name: "id", Type: "string", Desc: "Note id.", Required: true, In: "path"}}},
	{Name: "note_create", Group: "vigil_brain", Action: "save", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/workspace/notes",
		Description: "Save a new Brain note: a decision, a convention, a fact worth every run knowing.",
		Params: []mcpParam{
			{Name: "title", Type: "string", Desc: "Title.", Required: true, In: "body"},
			{Name: "content", Type: "string", Desc: "Markdown body.", Required: true, In: "body"},
			{Name: "tags", Type: "array", Items: "string", Desc: "Tags.", In: "body"},
			{Name: "pinned", Type: "boolean", Desc: "Pin the note.", In: "body"},
		}},
	{Name: "note_update", Group: "vigil_brain", Action: "update", Risk: mcpgov.RiskInternalWrite, Method: "PATCH", Path: "/api/workspace/notes/{id}",
		Description: "Update a Brain note.",
		Params: []mcpParam{{Name: "id", Type: "string", Desc: "Note id.", Required: true, In: "path"},
			{Name: "title", Type: "string", Desc: "Title.", In: "body"},
			{Name: "content", Type: "string", Desc: "Markdown body.", In: "body"},
			{Name: "tags", Type: "array", Items: "string", Desc: "Tags.", In: "body"},
			{Name: "pinned", Type: "boolean", Desc: "Pin the note.", In: "body"},
		}},
	{Name: "note_archive", Group: "vigil_brain", Action: "archive", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/workspace/notes/{id}/archive",
		Description: "Archive a Brain note (reversible in the app).", Params: []mcpParam{{Name: "id", Type: "string", Desc: "Note id.", Required: true, In: "path"}}},

	// ---- Projects ------------------------------------------------------------------
	{Name: "project_list", Group: "vigil_project", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/projects",
		Description: "List projects.", Params: nil},
	{Name: "project_search", Group: "vigil_project", Action: "search", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/projects/search",
		Description: "Search projects by name.", Params: []mcpParam{{Name: "q", Type: "string", Desc: "Search words.", Required: true, In: "query"}}},
	{Name: "project_get", Group: "vigil_project", Action: "get", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/projects/{id}",
		Description: "Read one project.", Params: []mcpParam{{Name: "id", Type: "string", Desc: "Project id.", Required: true, In: "path"}}},
	{Name: "project_create", Group: "vigil_project", Action: "create", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/projects",
		Description: "Create a project.",
		Params: []mcpParam{
			{Name: "title", Type: "string", Desc: "Title.", Required: true, In: "body"},
			{Name: "description", Type: "string", Desc: "Description.", In: "body"},
			{Name: "status", Type: "string", Desc: "Project status.", In: "body"},
			{Name: "priority", Type: "string", Desc: "Priority.", In: "body"},
		}},
	{Name: "project_update", Group: "vigil_project", Action: "update", Risk: mcpgov.RiskInternalWrite, Method: "PUT", Path: "/api/projects/{id}",
		Description: "Update a project's fields.",
		Params: []mcpParam{{Name: "id", Type: "string", Desc: "Project id.", Required: true, In: "path"},
			{Name: "title", Type: "string", Desc: "Title.", In: "body"},
			{Name: "description", Type: "string", Desc: "Description.", In: "body"},
			{Name: "status", Type: "string", Desc: "Project status.", In: "body"},
			{Name: "priority", Type: "string", Desc: "Priority.", In: "body"},
		}},

	// ---- Team ------------------------------------------------------------------------
	{Name: "agent_list", Group: "vigil_team", Action: "agents", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/agents",
		Description: "List the workspace's agents with their runtime and trust dial.", Params: nil},
	{Name: "agent_get", Group: "vigil_team", Action: "agent", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/agents/{id}",
		Description: "Read one agent.", Params: []mcpParam{{Name: "id", Type: "string", Desc: "Agent id.", Required: true, In: "path"}}},
	{Name: "agent_runs", Group: "vigil_team", Action: "agent_runs", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/agents/{id}/tasks",
		Description: "An agent's runs, newest first; filter by issue.",
		Params:      []mcpParam{{Name: "id", Type: "string", Desc: "Agent id.", Required: true, In: "path"}, {Name: "issue_id", Type: "string", Desc: "Only runs on this issue.", In: "query"}}},
	{Name: "member_list", Group: "vigil_team", Action: "members", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspaces/{workspace_id}/members",
		Description: "The workspace's members and roles.", Params: nil},
	{Name: "label_list", Group: "vigil_team", Action: "labels", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/labels",
		Description: "Workspace labels.", Params: nil},
	{Name: "cycle_list", Group: "vigil_team", Action: "cycles", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/cycles",
		Description: "Workspace cycles.", Params: nil},
	{Name: "workspace_get", Group: "vigil_team", Action: "workspace", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspaces/{workspace_id}",
		Description: "The workspace itself: name, slug, settings that matter to a client.", Params: nil},

	// ---- Triage, inbox, runs, handoff -------------------------------------------------
	{Name: "triage_list", Group: "vigil_triage", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/triage/items",
		Description: "Inbound work waiting for a human's verdict.", Params: nil},
	{Name: "triage_stats", Group: "vigil_triage", Action: "stats", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/triage/stats",
		Description: "Counts of the triage queue.", Params: nil},
	{Name: "triage_verdict", Group: "vigil_triage", Action: "verdict", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/triage/items/{id}/verdict",
		Description: "Suggest a verdict on a triage item; a human decides.",
		Params: []mcpParam{{Name: "id", Type: "string", Desc: "Triage item id.", Required: true, In: "path"},
			{Name: "verdict", Type: "string", Desc: "The suggested verdict.", Required: true, In: "body"},
			{Name: "reason", Type: "string", Desc: "Why.", In: "body"},
		}},
	{Name: "inbox_list", Group: "vigil_inbox", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/inbox",
		Description: "The caller's inbox: what needs them.", Params: nil},
	{Name: "run_transcript", Group: "vigil_run", Action: "transcript", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/tasks/{id}/messages",
		Description: "A run's transcript: tool calls, results, closing status, goal check.",
		Params:      []mcpParam{{Name: "id", Type: "string", Desc: "Run (task) id.", Required: true, In: "path"}}},
	{Name: "run_legs", Group: "vigil_run", Action: "legs", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/tasks/{id}/legs",
		Description: "Every run of a workflow (continuations, sub-agents, reviews) with what they cost.",
		Params:      []mcpParam{{Name: "id", Type: "string", Desc: "Any run id of the workflow.", Required: true, In: "path"}}},
	{Name: "handoff_latest", Group: "vigil_handoff", Action: "latest", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{issue_id}/handoff-packet/latest",
		Description: "The latest handoff packet on an issue: objective, decisions, evidence, failed attempts, next action.", Params: []mcpParam{pIssueRef}},
	{Name: "handoff_list", Group: "vigil_handoff", Action: "list", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{issue_id}/handoff-packets",
		Description: "Every handoff packet on an issue, oldest first.", Params: []mcpParam{pIssueRef}},
	{Name: "handoff_create", Group: "vigil_handoff", Action: "create", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{issue_id}/handoff-packet",
		Description: "Leave a handoff packet on an issue for the next hand.",
		Params: []mcpParam{pIssueRef,
			{Name: "run_id", Type: "string", Desc: "The run this packet belongs to.", Required: true, In: "body"},
			{Name: "objective", Type: "string", Desc: "What the work is for.", Required: true, In: "body"},
			{Name: "decisions", Type: "array", Items: "string", Desc: "Decisions taken.", In: "body"},
			{Name: "evidence", Type: "array", Items: "string", Desc: "Evidence: links, ids, results.", In: "body"},
			{Name: "failed_attempts", Type: "array", Items: "string", Desc: "What was tried and failed.", In: "body"},
			{Name: "next_action", Type: "string", Desc: "What the next hand should do.", In: "body"},
		}},
}

var mcpLeafByName = func() map[string]mcpLeaf {
	out := make(map[string]mcpLeaf, len(mcpLeaves))
	for _, l := range mcpLeaves {
		out[l.Name] = l
	}
	return out
}()

// mcpGroupDescriptions introduce each compound tool.
var mcpGroupDescriptions = map[string]string{
	"vigil_issue":   "Issues: list, search, get, create, update, comments, comment, timeline, labels. Pick the action; pass that action's arguments.",
	"vigil_goal":    "The goal loop of an issue: get the state, set the definition of done, pause, resume, answer the agent's question (ask: a run asks the team).",
	"vigil_brain":   "The workspace Brain, shared notes every run reads: list/search, get, save, update, archive.",
	"vigil_project": "Projects: list, search, get, create, update.",
	"vigil_team":    "Who is here: agents, agent, agent_runs, members, labels, cycles, workspace.",
	"vigil_triage":  "The triage queue: list, stats, verdict (a suggestion; a human decides).",
	"vigil_inbox":   "The caller's inbox: list.",
	"vigil_run":     "Runs: transcript of one run, legs (every run of a workflow with its cost).",
	"vigil_handoff": "Handoff packets on an issue: latest, list, create.",
}

// mcpCatalog is tools/list for a surface. The gate-wait tool is only
// offered to agents; members get confirmation tokens instead.
func mcpServerCatalog(surface string, caller mcpCaller) []map[string]any {
	var tools []map[string]any
	if surface == service.MCPSurfaceGranular {
		for _, leaf := range mcpLeaves {
			if leaf.AgentOnly && caller.kind != "agent" {
				continue
			}
			tools = append(tools, map[string]any{"name": leaf.Name, "description": leaf.Description + " Risk: " + leaf.Risk + ".", "inputSchema": mcpSchema(leaf.Params, caller, "")})
		}
	} else {
		groups := map[string][]mcpLeaf{}
		var order []string
		for _, leaf := range mcpLeaves {
			if leaf.AgentOnly && caller.kind != "agent" {
				continue
			}
			if _, seen := groups[leaf.Group]; !seen {
				order = append(order, leaf.Group)
			}
			groups[leaf.Group] = append(groups[leaf.Group], leaf)
		}
		for _, group := range order {
			tools = append(tools, map[string]any{"name": group, "description": mcpGroupDescriptions[group] + " " + mcpActionsHelp(groups[group]), "inputSchema": mcpCompoundSchema(groups[group], caller)})
		}
	}
	if caller.kind == "agent" {
		tools = append(tools, map[string]any{
			"name":        "vigil_gate_wait",
			"description": "Wait for the approval gate a held call named. Returns approved, denied, expired or pending; once approved, re-send the original call with gate_id.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"gate_id": map[string]any{"type": "string", "description": "The gate id from the held result."},
				"wait":    map[string]any{"type": "integer", "description": "Seconds to wait (default 20, max 55)."},
			}, "required": []string{"gate_id"}},
		})
	}
	return tools
}

func mcpActionsHelp(leaves []mcpLeaf) string {
	var parts []string
	for _, l := range leaves {
		var req []string
		for _, p := range l.Params {
			if p.Required {
				req = append(req, p.Name)
			}
		}
		line := l.Action + " (" + l.Risk
		if len(req) > 0 {
			line += "; needs " + strings.Join(req, ", ")
		}
		parts = append(parts, line+")")
	}
	return "Actions: " + strings.Join(parts, "; ") + "."
}

func mcpSchema(params []mcpParam, caller mcpCaller, action string) map[string]any {
	props := map[string]any{}
	var required []string
	for _, p := range params {
		props[p.Name] = mcpParamSchema(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	mcpAddControlParams(props, caller)
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}

func mcpCompoundSchema(leaves []mcpLeaf, caller mcpCaller) map[string]any {
	props := map[string]any{}
	var actions []string
	for _, l := range leaves {
		actions = append(actions, l.Action)
		for _, p := range l.Params {
			if _, seen := props[p.Name]; !seen {
				props[p.Name] = mcpParamSchema(p)
			}
		}
	}
	props["action"] = map[string]any{"type": "string", "enum": actions, "description": "Which operation to run."}
	mcpAddControlParams(props, caller)
	return map[string]any{"type": "object", "properties": props, "required": []string{"action"}}
}

func mcpAddControlParams(props map[string]any, caller mcpCaller) {
	if caller.kind == "agent" {
		props["gate_id"] = map[string]any{"type": "string", "description": "Re-send a held call with the approved gate id."}
		props["wait"] = map[string]any{"type": "integer", "description": "Seconds to wait for an approval when the call is held (default 20, max 55)."}
	} else {
		props["confirm_token"] = map[string]any{"type": "string", "description": "Re-send a held call with the confirmation token it returned."}
	}
}

func mcpParamSchema(p mcpParam) map[string]any {
	s := map[string]any{"type": p.Type, "description": p.Desc}
	if len(p.Enum) > 0 {
		s["enum"] = p.Enum
	}
	if p.Type == "array" {
		s["items"] = map[string]any{"type": p.Items}
	}
	return s
}

// mcpResolveLeaf maps a called name to a leaf: the leaf itself on the
// granular surface, or group + action on the compound one. Either name is
// accepted on either surface, so a client that cached the other catalogue
// still works.
func mcpResolveLeaf(surface, name string, args map[string]any) (mcpLeaf, bool) {
	if leaf, ok := mcpLeafByName[name]; ok {
		return leaf, true
	}
	action, _ := args["action"].(string)
	for _, leaf := range mcpLeaves {
		if leaf.Group == name && leaf.Action == action {
			return leaf, true
		}
	}
	return mcpLeaf{}, false
}

// build turns arguments into the request: path placeholders, query, body.
func (l mcpLeaf) build(args map[string]any, caller mcpCaller) (string, url.Values, map[string]any, error) {
	path := strings.ReplaceAll(l.Path, "{workspace_id}", uuidToString(caller.workspaceID))
	query := url.Values{}
	var body map[string]any
	for _, p := range l.Params {
		raw, present := args[p.Name]
		if !present || raw == nil {
			if p.Required {
				return "", nil, nil, fmt.Errorf("%s is required for %s", p.Name, l.Name)
			}
			continue
		}
		switch p.In {
		case "path":
			s, ok := raw.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return "", nil, nil, fmt.Errorf("%s must be a non-empty string", p.Name)
			}
			path = strings.ReplaceAll(path, "{"+p.Name+"}", escapeURLSegment(s))
		case "query":
			query.Set(p.Name, mcpScalar(raw))
		default:
			if body == nil {
				body = map[string]any{}
			}
			body[p.Name] = raw
		}
	}
	if strings.Contains(path, "{") {
		return "", nil, nil, errors.New("a path parameter is missing")
	}
	if l.Method != "GET" && body == nil {
		body = map[string]any{}
	}
	return path, query, body, nil
}

func mcpScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}
