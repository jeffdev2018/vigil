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
// bounded control API): deletes, member and role management, secrets and
// agent environments, approval and gate resolution, billing and spend
// redemption, workspace settings. A client that needs them has a person
// open the app. The single exception is note_capture_delete: a client that
// can capture must be able to take back what it captured, and it is classed
// as an external effect so the trust dial gates it.

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
	// Fixed are body fields the leaf always sends and the caller cannot
	// choose. Provenance uses it: a capture filed through MCP is stamped
	// origin "mcp", and a client that asked for another origin would be
	// claiming to be a surface it is not.
	Fixed     map[string]any
	AgentOnly bool
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
	// Follow-ups (réveil programmé): a deferred wake-up of the issue's agent.
	// The write is internal — it schedules a run inside the workspace, nothing
	// leaves it — and the cancel is the way back out of one, so it is the same
	// class rather than an external effect.
	{Name: "issue_followup", Group: "vigil_issue", Action: "followup", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/issues/{id}/followups",
		Description: "Wake this issue's agent later with a note: a single deferred run fires at the given time. when is RFC 3339 or +minutes from now, between 1 minute and 30 days away. Daily budgets per agent and per workspace apply.",
		Params: []mcpParam{pID,
			{Name: "when", Type: "string", Desc: "RFC 3339 instant (2026-09-11T09:00:00+02:00) or +minutes from now (+90).", Required: true, In: "body"},
			{Name: "note", Type: "string", Desc: "What the follow-up run should do; it becomes the run's trigger (500 characters max).", In: "body"},
			{Name: "agent_id", Type: "string", Desc: "Agent to wake. Required unless the issue is assigned to an agent.", In: "body"},
		}},
	{Name: "issue_followups", Group: "vigil_issue", Action: "followups", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/issues/{id}/followups",
		Description: "The follow-ups waiting to fire on this issue, soonest first, with the workspace's daily budget. Read it before scheduling another.",
		Params:      []mcpParam{pID}},
	{Name: "issue_followup_cancel", Group: "vigil_issue", Action: "followup_cancel", Risk: mcpgov.RiskInternalWrite, Method: "DELETE", Path: "/api/issues/{id}/followups/{followup_id}",
		Description: "Cancel a follow-up that has not fired yet. One that already fired or was cancelled answers 409.",
		Params: []mcpParam{pID,
			{Name: "followup_id", Type: "string", Desc: "Follow-up id, from issue_followups.", Required: true, In: "path"},
		}},

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
	{Name: "note_search", Group: "vigil_brain", Action: "search", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspace/notes/search",
		Description: "Ranked search over the Brain: relevance, not recency. Accepts websearch syntax (\"a quoted phrase\", -negation, OR); each hit carries a score and a snippet. Prefer it over list when you are looking for something rather than browsing.",
		Params: []mcpParam{
			{Name: "q", Type: "string", Desc: "What to look for.", Required: true, In: "query"},
			{Name: "tag", Type: "string", Desc: "Only notes with this tag.", In: "query"},
			{Name: "archived", Type: "boolean", Desc: "Include archived notes.", In: "query"},
			pLimit,
		}},
	// The capture inbox: anything worth keeping lands here in one gesture
	// and a person files it later. A client that is unsure whether something
	// belongs in the Brain captures it instead of writing a note nobody asked
	// for.
	{Name: "note_capture", Group: "vigil_brain", Action: "capture", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/brain/captures",
		Description: "Capture a line of text or a link into the Brain inbox, to be organized later. Use it instead of save when you are not sure the workspace wants this as a note: a person turns it into one, merges it, or discards it.",
		Fixed:       map[string]any{"origin": "mcp"},
		Params: []mcpParam{
			{Name: "content", Type: "string", Desc: "The text to capture (content or url is required).", In: "body"},
			{Name: "url", Type: "string", Desc: "An http(s) link to capture.", In: "body"},
			{Name: "kind", Type: "string", Desc: "text, link or todo. Inferred when omitted.", In: "body", Enum: []string{"text", "link", "todo"}},
			{Name: "title_hint", Type: "string", Desc: "A hint for the note title, used when the capture is organized.", In: "body"},
		}},
	{Name: "note_inbox", Group: "vigil_brain", Action: "inbox", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/brain/captures",
		Description: "The Brain capture inbox: what was captured and not yet filed, with the raw count.",
		Params: []mcpParam{
			{Name: "status", Type: "string", Desc: "raw (default), organized, discarded or all.", In: "query", Enum: []string{"raw", "organized", "discarded", "all"}},
			pLimit,
		}},
	{Name: "note_organize", Group: "vigil_brain", Action: "organize", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/brain/captures/{capture_id}/organize",
		Description: "File a raw capture: turn it into a note, merge it into an existing one, or discard it. Only works while the capture is raw.",
		Params: []mcpParam{
			{Name: "capture_id", Type: "string", Desc: "Capture id.", Required: true, In: "path"},
			{Name: "action", Type: "string", Desc: "note, merge or discard.", Required: true, In: "body", Enum: []string{"note", "merge", "discard"}},
			{Name: "title", Type: "string", Desc: "Note title. Defaults to the capture's title hint, then its first line.", In: "body"},
			{Name: "content", Type: "string", Desc: "Note body. Defaults to the capture rendered as markdown.", In: "body"},
			{Name: "tags", Type: "array", Items: "string", Desc: "Tags.", In: "body"},
			{Name: "pinned", Type: "boolean", Desc: "Pin the created note.", In: "body"},
			{Name: "note_id", Type: "string", Desc: "The note to merge into. Required for merge.", In: "body"},
		}},
	{Name: "note_capture_reopen", Group: "vigil_brain", Action: "reopen", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/brain/captures/{capture_id}/reopen",
		Description: "Put a discarded capture back into the inbox.",
		Params:      []mcpParam{{Name: "capture_id", Type: "string", Desc: "Capture id.", Required: true, In: "path"}}},
	// The one delete on the surface, and it is classed as an external effect
	// so every trust dial below "autonomous" has to ask: a capture is often
	// the only copy of what someone said.
	{Name: "note_capture_delete", Group: "vigil_brain", Action: "delete", Risk: mcpgov.RiskExternal, Method: "DELETE", Path: "/api/brain/captures/{capture_id}",
		Description: "Delete a capture for good, with its file. Irreversible: discard it instead unless someone asked for it gone.",
		Params:      []mcpParam{{Name: "capture_id", Type: "string", Desc: "Capture id.", Required: true, In: "path"}}},

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
	// Native calendar (OS plan, chantier 19).
	{Name: "calendar_events", Group: "vigil_calendar", Action: "events", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/calendar/events",
		Description: "Events overlapping a window (from/to RFC 3339, default the coming month), optionally one participant's.",
		Params: []mcpParam{
			{Name: "from", Type: "string", Desc: "Window start, RFC 3339.", In: "query"},
			{Name: "to", Type: "string", Desc: "Window end, RFC 3339.", In: "query"},
			{Name: "participant_type", Type: "string", Desc: "member or agent.", In: "query", Enum: []string{"member", "agent"}},
			{Name: "participant_id", Type: "string", Desc: "Member user id or agent id.", In: "query"},
		}},
	{Name: "calendar_agenda", Group: "vigil_calendar", Action: "agenda", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/calendar/agenda",
		Description: "Everything dated in the window: events, issues due, cycles, meetings.",
		Params: []mcpParam{
			{Name: "from", Type: "string", Desc: "Window start, RFC 3339.", In: "query"},
			{Name: "to", Type: "string", Desc: "Window end, RFC 3339.", In: "query"},
		}},
	{Name: "calendar_slots", Group: "vigil_calendar", Action: "slots", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/calendar/slots",
		Description: "Free windows every listed participant can make (members inside 09:00–18:00 weekdays in tz, agents any time).",
		Params: []mcpParam{
			{Name: "participants", Type: "string", Desc: "Comma-separated member:<user id> / agent:<id>.", Required: true, In: "query"},
			{Name: "duration", Type: "integer", Desc: "Minutes (default 30).", In: "query"},
			{Name: "from", Type: "string", Desc: "Window start, RFC 3339.", In: "query"},
			{Name: "to", Type: "string", Desc: "Window end, RFC 3339.", In: "query"},
			{Name: "tz", Type: "string", Desc: "IANA zone for working hours (default UTC).", In: "query"},
		}},
	{Name: "calendar_propose", Group: "vigil_calendar", Action: "propose", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/calendar/events",
		Description: "Propose an event on an issue: filed as proposed with a Decision Card; a person accepts or declines. A member calling this schedules it directly.",
		Params: []mcpParam{
			{Name: "title", Type: "string", Desc: "Title.", Required: true, In: "body"},
			{Name: "starts_at", Type: "string", Desc: "Start, RFC 3339.", Required: true, In: "body"},
			{Name: "ends_at", Type: "string", Desc: "End, RFC 3339.", Required: true, In: "body"},
			{Name: "issue_id", Type: "string", Desc: "The issue the event serves (required for a proposal).", In: "body"},
			{Name: "description", Type: "string", Desc: "What the event is for.", In: "body"},
			{Name: "timezone", Type: "string", Desc: "IANA zone the times are shown in (default UTC).", In: "body"},
			{Name: "location", Type: "string", Desc: "Place or link.", In: "body"},
			{Name: "participants", Type: "array", Desc: "[{type: member|agent, id}].", In: "body", Items: "object"},
		}},
	// Autopilots from a sentence (réveil programmé). Draft writes nothing —
	// it asks the model and validates the cron — so it is a read; propose
	// files the autopilot PAUSED behind a Decision Card, which is why the
	// write is internal: nothing runs until a person answers the card.
	{Name: "autopilot_draft", Group: "vigil_autopilot", Action: "draft", Risk: mcpgov.RiskRead, Method: "POST", Path: "/api/autopilots/draft",
		Description: "Turn a sentence (\"every Monday at 9, list the open tickets\") into a schedule: title, 5-field cron, timezone, the instruction each run follows, and the next three firing instants. Writes nothing. 503 when the workspace has no model.",
		Params: []mcpParam{
			{Name: "text", Type: "string", Desc: "The schedule and the task in plain words (2000 characters max).", Required: true, In: "body"},
			{Name: "timezone", Type: "string", Desc: "IANA timezone to read the sentence in (default UTC).", In: "body"},
		}},
	{Name: "autopilot_propose", Group: "vigil_autopilot", Action: "propose", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/autopilots/propose",
		Description: "File a recurring automation, PAUSED, with its schedule disabled. Give text (drafted first) or title + cron_expression + description. With an issue, a Decision Card is filed on it whose answer activates or discards the autopilot; nothing runs before that.",
		Params: []mcpParam{
			{Name: "text", Type: "string", Desc: "The schedule and the task in plain words; used when title or cron_expression is missing.", In: "body"},
			{Name: "title", Type: "string", Desc: "Autopilot title.", In: "body"},
			{Name: "cron_expression", Type: "string", Desc: "5-field cron: minute hour day-of-month month day-of-week.", In: "body"},
			{Name: "timezone", Type: "string", Desc: "IANA timezone (default UTC).", In: "body"},
			{Name: "description", Type: "string", Desc: "The instruction the agent follows at each run.", In: "body"},
			{Name: "execution_mode", Type: "string", Desc: "create_issue opens an issue each run; run_only just runs.", In: "body", Enum: []string{"create_issue", "run_only"}},
			{Name: "issue_title_template", Type: "string", Desc: "Title of the issue created each run; may contain {{date}} (create_issue only).", In: "body"},
			{Name: "assignee_id", Type: "string", Desc: "Agent that will run it. Required for a member; a run proposes as itself.", In: "body"},
			{Name: "project_id", Type: "string", Desc: "Project the autopilot belongs to.", In: "body"},
			{Name: "issue_id", Type: "string", Desc: "Issue to file the activation Decision Card on (a run's own issue by default).", In: "body"},
			{Name: "activate", Type: "boolean", Desc: "Members only: create it active with its schedule enabled and file no card. A run asking for this is refused — a run proposes, a person decides.", In: "body"},
		}},
	// Workspace doctrine (OS plan, chantier 22). Read-only for a client, plus
	// the report a run files when a task collides with a rule. Publishing,
	// approving and restoring stay human affordances in the app or the CLI.
	{Name: "doctrine_get", Group: "vigil_doctrine", Action: "get", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspace/doctrine",
		Description: "The workspace doctrine: the governing text every agent is bound by, its revision, the byte limit, whether a second reviewer is required, and what is waiting on a person.", Params: nil},
	{Name: "doctrine_versions", Group: "vigil_doctrine", Action: "versions", Risk: mcpgov.RiskRead, Method: "GET", Path: "/api/workspace/doctrine/versions",
		Description: "The doctrine's revision ledger, newest first: who wrote each version, who reviewed it, and its status.",
		Params: []mcpParam{
			{Name: "cursor", Type: "string", Desc: "Page cursor from a previous read.", In: "query"},
			pLimit,
		}},
	{Name: "doctrine_report", Group: "vigil_doctrine", Action: "report", Risk: mcpgov.RiskInternalWrite, Method: "POST", Path: "/api/workspace/doctrine/reports",
		Description: "File a doctrine report: a task cannot be done without breaking a rule (refusal), two rules conflict (conflict), or a rule is too vague to apply (ambiguity). Reaches the owners' inbox against the revision you ran under.",
		Params: []mcpParam{
			{Name: "kind", Type: "string", Desc: "conflict, refusal or ambiguity.", Required: true, In: "body", Enum: []string{"conflict", "refusal", "ambiguity"}},
			{Name: "summary", Type: "string", Desc: "What collided, in your own words.", Required: true, In: "body"},
			{Name: "passage", Type: "string", Desc: "The doctrine passage at stake, quoted.", In: "body"},
			{Name: "issue_id", Type: "string", Desc: "The issue the collision happened on (a run's issue rides along on its own).", In: "body"},
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
	"vigil_issue":     "Issues: list, search, get, create, update, comments, comment, timeline, labels, and follow-ups (followup, followups, followup_cancel: wake the issue's agent later with a note). Pick the action; pass that action's arguments.",
	"vigil_goal":      "The goal loop of an issue: get the state, set the definition of done, pause, resume, answer the agent's question (ask: a run asks the team).",
	"vigil_brain":     "The workspace Brain, shared notes every run reads, and its capture inbox: list, search (ranked), get, save, update, archive; capture (park something to be filed later), inbox, organize, reopen, delete.",
	"vigil_project":   "Projects: list, search, get, create, update.",
	"vigil_team":      "Who is here: agents, agent, agent_runs, members, labels, cycles, workspace.",
	"vigil_triage":    "The triage queue: list, stats, verdict (a suggestion; a human decides).",
	"vigil_inbox":     "The caller's inbox: list.",
	"vigil_run":       "Runs: transcript of one run, legs (every run of a workflow with its cost).",
	"vigil_handoff":   "Handoff packets on an issue: latest, list, create.",
	"vigil_calendar":  "The workspace calendar: events in a window, the agenda (events, issue due dates, cycles, meetings), free slots for people and agents, propose an event (a person accepts).",
	"vigil_autopilot": "Recurring automations from plain words: draft (a sentence becomes a title, a cron and a prompt; writes nothing), propose (file it paused behind a Decision Card someone answers to activate it).",
	"vigil_doctrine":  "The workspace doctrine, the standing rules every agent is bound by: get the live text and its revision, read the revision ledger, report a rule you cannot follow or two rules that conflict.",
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
	for name, value := range l.Fixed {
		if body == nil {
			body = map[string]any{}
		}
		body[name] = value
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
