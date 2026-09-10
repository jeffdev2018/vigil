/**
 * Mobile-local zod schemas + fallbacks for endpoints whose responses aren't
 * yet schematised in @multica/core/api/schemas. Lenient by design — see the
 * leniency rationale at the top of the core file (string enums tolerated,
 * loose() so unknown server fields pass through, defaults so a missing
 * array doesn't take the page down).
 *
 * If web/desktop later need these same schemas, promote them to core; until
 * then they live here so mobile satisfies its "Parse, don't cast" rule
 * (root CLAUDE.md "API Response Compatibility") for these endpoints.
 */
import { z } from "zod";
import type {
  Agent,
  AgentInvocationTarget,
  AgentTask,
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  Comment,
  InboxItem,
  IssueLabelsResponse,
  Label,
  ListGoalsResponse,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListProjectsResponse,
  MemberWithUser,
  OrgStructure,
  PinnedItem,
  Project,
  ProjectResource,
  RuntimeDevice,
  SearchIssuesResponse,
  SearchProjectsResponse,
  SendChatMessageResponse,
  Squad,
  User,
  Workspace,
} from "@multica/core/types";
import {
  AgentEffectListSchema,
  AgentEffectSchema,
  IssueSchema,
  UndoReportSchema,
} from "@multica/core/api/schemas";

/**
 * Undo for agent actions (K69). Types derive from the core zod schemas
 * (on the mobile sharing whitelist) instead of `packages/core/issues/
 * agent-effects.ts`, whose interfaces sit next to TanStack hooks that pull
 * in web's api client. Fallbacks mirror `packages/core/api/client.ts`
 * (`listIssueAgentEffects` / `undoTask` / `undoAgentEffect`).
 */
export type AgentEffect = z.infer<typeof AgentEffectSchema>;
export type AgentEffectList = z.infer<typeof AgentEffectListSchema>;
export type UndoReport = z.infer<typeof UndoReportSchema>;
export const EMPTY_AGENT_EFFECT_LIST: AgentEffectList = {
  effects: [],
  window_hours: 24,
};
export const EMPTY_UNDO_REPORT: UndoReport = {
  reversed: 0,
  skipped: [],
  breaker: { tripped: false, trust_mode: "" },
  effects: [],
};

/** Upload response. Only fields mobile actually consumes — `url` to put
 *  into the markdown link, `filename` for the `[📎 name](url)` form, `id`
 *  for future linking. `.loose()` so the server can add fields without
 *  breaking mobile. Web's AttachmentSchema (packages/core/api/schemas.ts:41)
 *  is even looser (only `id`); mobile validates more because the upload
 *  flow inserts `url` directly into editable text and an empty `url` would
 *  produce a broken link the user only notices after submit. */
export const AttachmentSchema: z.ZodType<Attachment> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  issue_id: z.string().nullable().default(null),
  comment_id: z.string().nullable().default(null),
  chat_session_id: z.string().nullable().default(null),
  chat_message_id: z.string().nullable().default(null),
  uploader_type: z.string().default(""),
  uploader_id: z.string().default(""),
  filename: z.string(),
  url: z.string(),
  download_url: z.string().default(""),
  markdown_url: z.string().default(""),
  content_type: z.string().default(""),
  size_bytes: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

/** GET /api/issues/:id/attachments — array of attachments for the issue.
 *  Empty array fallback so a 5xx or shape mismatch doesn't crash markdown
 *  rendering — image URIs simply fail to resolve and fall back to fetch. */
export const AttachmentListSchema = z.array(AttachmentSchema).default([]);
export const EMPTY_ATTACHMENT_LIST: Attachment[] = [];

/** Comment write endpoints all return a full Comment. Used by createComment /
 *  updateComment / resolveComment / unresolveComment via fetchValidatedWith.
 *  Empty fallback yields `id: ""` so downstream code (the mutations'
 *  onSuccess writers) can detect drift and fall back to invalidate. */
export const CommentSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  author_type: z.string().default("member"),
  author_id: z.string().default(""),
  content: z.string().default(""),
  type: z.string().default("comment"),
  parent_id: z.string().nullable().default(null),
  reactions: z.array(z.unknown()).default([]),
  attachments: z.array(z.unknown()).default([]),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  resolved_at: z.string().nullable().default(null),
  resolved_by_type: z.string().nullable().default(null),
  resolved_by_id: z.string().nullable().default(null),
  source_task_id: z.string().nullable().optional(),
}).loose() as unknown as z.ZodType<Comment>;

export const EMPTY_COMMENT: Comment = {
  id: "",
  issue_id: "",
  author_type: "member",
  author_id: "",
  content: "",
  type: "comment",
  parent_id: null,
  reactions: [],
  attachments: [],
  created_at: "",
  updated_at: "",
  resolved_at: null,
  resolved_by_type: null,
  resolved_by_id: null,
};

/** GET/PUT /api/notification-preferences. Preferences are partial — absent
 *  keys mean "default (= all)", an explicit "muted" turns the group off.
 *  Loose() so future group additions on the backend don't break parsing.
 *  Value type is z.string() (not z.enum) so a future server-side value like
 *  "snoozed" downgrades gracefully (read sites treat unknown as enabled)
 *  instead of failing schema parse and dropping the entire preferences map.
 *  Per CLAUDE.md "Enum drift downgrades, not crashes". */
export const NotificationPreferenceResponseSchema = z.object({
  workspace_id: z.string().default(""),
  preferences: z.record(z.string(), z.string()).default({}),
}).loose();
export const EMPTY_NOTIFICATION_PREFERENCES = {
  workspace_id: "",
  preferences: {},
} as const;

const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  color: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

export const IssueLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
}).loose();

export const EMPTY_ISSUE_LABELS_RESPONSE: IssueLabelsResponse = {
  labels: [],
};

export const ProjectSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  icon: z.string().nullable(),
  status: z.string(),
  priority: z.string(),
  lead_type: z.string().nullable(),
  lead_id: z.string().nullable(),
  // .default(null) so a project from an older backend that omits these keys
  // parses to null instead of degrading the batch to the empty fallback.
  start_date: z.string().nullable().default(null),
  due_date: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  issue_count: z.number().default(0),
  done_count: z.number().default(0),
  resource_count: z.number().default(0),
}).loose();

export const ListProjectsResponseSchema = z.object({
  projects: z.array(ProjectSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECTS_RESPONSE: ListProjectsResponse = {
  projects: [],
  total: 0,
};

// Goals with ancestry (K74). Mirror of `GoalSchema` / `ListGoalsResponseSchema`
// in packages/core/api/schemas.ts — same fields, same enum, same fallbacks.
const GoalStatusSchema = z
  .enum(["draft", "active", "done", "dropped"])
  .catch("draft");

export const GoalSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  parent_goal_id: z.string().nullable().catch(null).default(null),
  title: z.string().catch(""),
  description: z.string().catch("").default(""),
  success_measure: z.string().catch("").default(""),
  due_date: z.string().nullable().catch(null).default(null),
  owner_id: z.string().nullable().catch(null).default(null),
  status: GoalStatusSchema.default("draft"),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  // Rolled up over sub-goals by the server; never re-summed client-side.
  issue_count: z.number().catch(0).default(0),
  done_count: z.number().catch(0).default(0),
  project_ids: z.array(z.string()).catch([]).default([]),
}).loose();

export const ListGoalsResponseSchema = z.object({
  goals: z.array(GoalSchema).catch([]).default([]),
  total: z.number().catch(0).default(0),
}).loose();

export const EMPTY_LIST_GOALS_RESPONSE: ListGoalsResponse = {
  goals: [],
  total: 0,
};

// Executable org chart (K75). Mirror of `OrgStructureSchema` /
// `OrgStructureListSchema` in packages/core/api/schemas.ts — same fields,
// same enums, same fallbacks. Mobile renders units only, so edges / rules /
// committees / market stay loose passthrough.
const OrgMemberSchema = z.object({
  type: z.enum(["member", "agent"]).catch("member"),
  id: z.string().catch(""),
  role: z.string().optional(),
  role_id: z.string().optional(),
}).loose();

export const OrgUnitSchema = z.object({
  id: z.string().catch(""),
  name: z.string().catch(""),
  kind: z.string().optional(),
  owner_id: z.string().optional(),
  excludes: z
    .array(z.enum(["untrusted_input", "sensitive_data", "external_effects"]))
    .catch([])
    .default([]),
  autonomy: z
    .enum(["read_only", "draft", "approve_payload", "auto"])
    .catch("draft"),
  allow: z.array(z.string()).catch([]).default([]),
  deny: z.array(z.string()).catch([]).default([]),
  escalation_quota_per_day: z.number().catch(5).default(5),
  members: z.array(OrgMemberSchema).catch([]).default([]),
  roles: z
    .array(
      z.object({
        id: z.string().catch(""),
        name: z.string().catch(""),
        responsibilities: z.string().optional(),
        keywords: z.array(z.string()).optional(),
      }).loose(),
    )
    .catch([])
    .default([]),
}).loose();

const EMPTY_ORG_MARKET = {
  price_cap_usd_ticks: 0,
  offers_per_agent_per_day: 5,
  min_offers: 2,
};

export const OrgDefinitionSchema = z.object({
  units: z.array(OrgUnitSchema).catch([]).default([]),
  edges: z.array(z.looseObject({})).catch([]).default([]),
  rules: z.array(z.looseObject({})).catch([]).default([]),
  committees: z.array(z.looseObject({})).catch([]).default([]),
  market: z
    .object({
      price_cap_usd_ticks: z.number().catch(0).default(0),
      offers_per_agent_per_day: z.number().catch(5).default(5),
      min_offers: z.number().catch(2).default(2),
    })
    .loose()
    .catch(EMPTY_ORG_MARKET),
}).loose();

const EMPTY_ORG_DEFINITION = {
  units: [],
  edges: [],
  rules: [],
  committees: [],
  market: EMPTY_ORG_MARKET,
};

export const OrgStructureSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().nullable().catch(null).default(null),
  model: z
    .enum(["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"])
    .catch("owner_network"),
  name: z.string().catch(""),
  status: z.enum(["draft", "active", "paused", "dissolved"]).catch("draft"),
  revision: z.number().catch(1).default(1),
  revision_id: z.string().nullable().catch(null).default(null),
  definition: OrgDefinitionSchema.catch(EMPTY_ORG_DEFINITION),
  owner_id: z.string().nullable().catch(null).default(null),
  dissolve_at: z.string().nullable().catch(null).default(null),
  end_condition: z.string().catch("").default(""),
  budget_usd_ticks: z.number().catch(0).default(0),
  eval_attestation: z.string().catch("").default(""),
  paused_reason: z.string().catch("").default(""),
  dissolved_at: z.string().nullable().catch(null).default(null),
  paused_units: z.array(z.string()).catch([]).default([]),
  created_by: z.string().nullable().catch(null).default(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const OrgStructureListSchema = z.object({
  structures: z.array(OrgStructureSchema).catch([]).default([]),
}).loose();

export interface OrgStructureList {
  structures: OrgStructure[];
}

export const EMPTY_ORG_STRUCTURE_LIST: OrgStructureList = { structures: [] };

// Fallback for `GET /api/projects/{id}` when the response shape drifts.
// `id` defaults to empty — caller can detect "not found / drift" by checking
// `data.id === ""` and rendering an error state instead of pretending the
// data is valid. Status / priority cast to the enum literals so TS callers
// downstream still flow correctly; runtime values came from the schema
// (`z.string()`), which would have already passed.
export const EMPTY_PROJECT: Project = {
  id: "",
  workspace_id: "",
  title: "",
  description: null,
  icon: null,
  status: "planned",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};

// Project resources are typed pointers to external resources (today: GitHub
// repos). resource_ref shape varies per resource_type; lenient on both
// `resource_type` (so a future type doesn't crash the list) and
// `resource_ref` (passes through unchanged for the renderer to dispatch on).
const ProjectResourceSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string(),
  resource_ref: z.unknown(),
  label: z.string().nullable(),
  position: z.number().default(0),
  created_at: z.string(),
  created_by: z.string().nullable(),
}).loose();

export const ListProjectResourcesResponseSchema = z.object({
  resources: z.array(ProjectResourceSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECT_RESOURCES_RESPONSE: ListProjectResourcesResponse = {
  resources: [],
  total: 0,
};

// =====================================================
// Chat (sessions / messages / pending task)
// =====================================================
// Lenient on every field that's purely informational (status enum, timestamps,
// agent/creator ids). `.loose()` so server-added fields pass through. The two
// fields mobile keys behaviour on — `id` and `chat_session_id` — are required.

export const ChatSessionSchema: z.ZodType<ChatSession> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  creator_id: z.string().default(""),
  title: z.string().default(""),
  // Enum drift defense (root CLAUDE.md "Enum drift downgrades, not crashes"):
  // unknown server values fall back to "active" so the row still renders.
  status: z.enum(["active", "archived"]).catch("active"),
  has_unread: z.boolean().default(false),
  // Unread assistant messages after the read cursor. Optional (not defaulted)
  // so the badge math can tell "older server didn't send it" from a real 0 —
  // the tab badge sums `unread_count ?? 0`, same rule as web's sidebar.
  unread_count: z.number().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const ChatSessionListSchema = z.array(ChatSessionSchema).default([]);

export const EMPTY_CHAT_SESSION_LIST: ChatSession[] = [];

// `attachments` carried for parity rendering only — v1 doesn't author them on
// mobile. AttachmentSchema is reused as-is.
export const ChatMessageSchema: z.ZodType<ChatMessage> = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  // If the server ever introduces a third role, fall back to "assistant" so
  // the message renders (as a left-aligned bubble) instead of crashing the
  // list. Matches Enum drift defense.
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
  message_kind: z.enum(["message", "no_response"]).catch("message").optional(),
  // One malformed optional suggestion must not erase an otherwise valid
  // conversation. The server validates these too; this is mixed-version and
  // corrupted-cache defense at the mobile boundary.
  quick_actions: z.array(z.object({
    label: z.string(),
    prompt: z.string(),
    primary: z.boolean().optional(),
  }).loose()).catch([]).optional().default([]),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);

export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

const ChatQueuedTaskSchema = z.object({
  task_id: z.string(),
  status: z.string().default("queued"),
  created_at: z.string().default(""),
  message_id: z.string().optional(),
  content: z.string().optional(),
}).loose();

const ChatQueuedTasksSchema = z.array(z.unknown()).transform((tasks) =>
  tasks.flatMap((task) => {
    const parsed = ChatQueuedTaskSchema.safeParse(task);
    return parsed.success ? [parsed.data] : [];
  }),
);

// All root fields are optional — server returns an empty object when no
// task is in flight. Ignore malformed queue rows without discarding a valid
// head, matching packages/core/api/schemas.ts.
export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
  supports_queue: z.boolean().optional(),
  queued_tasks: ChatQueuedTasksSchema.optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string(),
  task_id: z.string(),
  supports_queue: z.boolean().optional(),
  queued: z.boolean().optional().catch(undefined),
  created_at: z.string().default(""),
}).loose();

// The live task timeline moved to the shared TaskActivityResponseSchema in
// packages/core/api/schemas.ts, which accepts both the wrapped
// `{ messages, actions }` response and the bare array an older server returns.
// The mobile-only copy is gone rather than kept alongside it: two schemas for
// one endpoint is exactly how the enum drift they both guard against gets
// handled two different ways.

// =====================================================
// Search (issues + projects)
// =====================================================
// Mirrors SearchIssueResult / SearchProjectResult in packages/core/types/api.ts.
// Web does not currently route search responses through parseWithFallback, so
// the schemas live mobile-side. Promote to core when web adopts the same
// defense.
//
// match_source is the server's hint of which field matched. Enum-drift defense
// (root CLAUDE.md "Enum drift downgrades, not crashes"): unknown values fall
// back to "title" so the row still renders without a snippet line.

const SearchIssueResultSchema = IssueSchema.safeExtend({
  match_source: z.enum(["title", "description", "comment"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
};

const SearchProjectResultSchema = ProjectSchema.safeExtend({
  match_source: z.enum(["title", "description"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
};

// =====================================================
// Agent tasks (per-issue runs, active + history)
// =====================================================
// Mirrors AgentTask in packages/core/types/agent.ts. Backend handlers:
//   GET  /api/issues/{id}/active-task → { tasks: AgentTask[] } (may be empty)
//   GET  /api/issues/{id}/task-runs   → AgentTask[]
// Lenient on every field — status / kind use `.catch()` so a future
// server-side enum value renders a generic fallback rather than crashing the
// row (root CLAUDE.md "Enum drift downgrades, not crashes"). failure_reason is
// an open string instead: its taxonomy grows on the backend's cadence, so a
// value this build has never seen must survive parsing and degrade at render.

export const AgentTaskSchema: z.ZodType<AgentTask> = z.object({
  id: z.string(),
  agent_id: z.string().default(""),
  runtime_id: z.string().default(""),
  issue_id: z.string().default(""),
  // Full TaskStatus union (packages/core/types/agent.ts) — was missing
  // waiting_local_directory / deferred / paused, so a task genuinely in one
  // of those states silently rendered as "Queued" (run-row.tsx's
  // STATUS_LABEL/STATUS_CLASS maps were already keyed for all nine and have
  // been unreachable for these three since this schema was written). The
  // Runs fleet page below surfaces exactly these blocked states, so the gap
  // stops being cosmetic once that page exists.
  status: z
    .enum([
      "queued",
      "deferred",
      "dispatched",
      "waiting_local_directory",
      "running",
      "completed",
      "failed",
      "cancelled",
      "paused",
    ])
    .catch("queued"),
  priority: z.number().default(0),
  dispatched_at: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  result: z.unknown().default(null),
  error: z.string().nullable().default(null),
  // Open string, not an enum — same contract as `failure_reason` in
  // packages/core/types/agent.ts and as the chat message schema above. The
  // backend taxonomy passed the six coarse values at MUL-1949 and keeps
  // growing (26 canonical reasons today), so an installed build meets reasons
  // it predates.
  //
  // This field WAS a closed six-value enum, which made the whole thing moot:
  // `.catch("")` erased every refined reason to `undefined`, so run-row's
  // badge map has been unreachable for anything but the coarse values since
  // MUL-5370 widened it, and every agent_error.* / skill_bundle_unavailable /
  // environment_prepare_failed run rendered a bare "Failed" (#7913). Unknown
  // reasons are the badge map's problem to degrade, not the parser's to drop.
  //
  // Backend uses empty string ("") as the "not failed" sentinel (Go
  // `omitempty` on a custom string-typed enum). Normalize that to `undefined`
  // so downstream truthy checks (`if (task.failure_reason)`) don't have to
  // special-case both null/undefined AND "".
  failure_reason: z
    .string()
    .optional()
    .catch(undefined)
    .transform((v) => (v === "" ? undefined : v)),
  created_at: z.string().default(""),
  chat_session_id: z.string().optional(),
  autopilot_run_id: z.string().optional(),
  parent_task_id: z.string().optional(),
  attempt: z.number().optional(),
  trigger_comment_id: z.string().optional(),
  trigger_summary: z.string().optional(),
  kind: z.enum(["comment", "autopilot", "chat", "quick_create", "direct"]).optional().catch("direct"),
  work_dir: z.string().optional(),
}).loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema).default([]);

export const ActiveTasksResponseSchema = z.object({
  tasks: z.array(AgentTaskSchema).default([]),
}).loose();

export interface ActiveTasksResponse {
  tasks: AgentTask[];
}

export const EMPTY_AGENT_TASK_LIST: AgentTask[] = [];
export const EMPTY_ACTIVE_TASKS_RESPONSE: ActiveTasksResponse = { tasks: [] };

// =====================================================
// User / Workspace / Inbox / Member / Agent
// =====================================================
// Mobile reads these on every cold start (auth → workspaces → inbox → members
// → agents form the boot sequence). A schema drift in any of them used to
// cascade — getMe failure flushed the user, listWorkspaces failure landed the
// app on the workspace picker with no entries. With parseWithFallback every
// drift downgrades to "stale defaults render", and the user can keep working.
//
// All five are `.loose()` so additive backend fields (`onboarded_at` style
// flags) pass through without breaking parsing. Required identity fields
// (id, slug, etc.) stay required — a response that genuinely lacks them is
// unusable and parseWithFallback should fall back to the empty sentinel.

export const UserSchema: z.ZodType<User> = z.object({
  id: z.string(),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  onboarded_at: z.string().nullable().default(null),
  onboarding_questionnaire: z.record(z.string(), z.unknown()).default({}),
  starter_content_state: z.string().nullable().default(null),
  language: z.string().nullable().default(null),
  profile_description: z.string().default(""),
  timezone: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

// `id: ""` is the sentinel for "drifted / unauthenticated"; downstream code
// that switches on `user.id` will treat empty-string as a logged-out state
// (the auth hook also clears the cache on 401, so this is rarely seen).
export const EMPTY_USER: User = {
  id: "",
  name: "",
  email: "",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "",
  updated_at: "",
};

export const WorkspaceSchema: z.ZodType<Workspace> = z.object({
  id: z.string(),
  name: z.string().default(""),
  slug: z.string().default(""),
  description: z.string().nullable().default(null),
  context: z.string().nullable().default(null),
  settings: z.record(z.string(), z.unknown()).default({}),
  repos: z.array(z.object({ url: z.string() }).loose()).default([]),
  issue_prefix: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const WorkspaceListSchema = z.array(WorkspaceSchema).default([]);
export const EMPTY_WORKSPACE_LIST: Workspace[] = [];

/** Pin metadata only — display fields (title / status / icon) are NOT here,
 *  consumers derive them from `issueDetailOptions` / `projectDetailOptions`.
 *  Matches the design in packages/core/types/pin.ts. */
export const PinnedItemSchema: z.ZodType<PinnedItem> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  user_id: z.string().default(""),
  item_type: z.enum(["issue", "project"]).catch("issue"),
  item_id: z.string(),
  position: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

export const PinListSchema = z.array(PinnedItemSchema).default([]);
export const EMPTY_PIN_LIST: PinnedItem[] = [];

const InboxItemSchema: z.ZodType<InboxItem> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  // Recipient is always a real actor in the dataset, but defend against
  // either field going missing — mobile's actor lookup tolerates null.
  recipient_type: z.enum(["member", "agent"]).catch("member"),
  recipient_id: z.string().default(""),
  // `actor_type` includes "system" for platform-triggered notifications
  // (packages/core/types/inbox.ts:28). ActorAvatar handles all three plus
  // null. Enum drift falls back to null so the row still renders without an
  // avatar instead of crashing the list.
  actor_type: z
    .enum(["member", "agent", "system"])
    .nullable()
    .catch(null),
  actor_id: z.string().nullable().default(null),
  // `type` discriminates the rendered detail-label. Unknown values pass
  // through as raw strings — `InboxDetailLabel` has a default branch that
  // shows the raw type as fallback (components/inbox/detail-label.tsx).
  type: z.string() as unknown as z.ZodType<InboxItem["type"]>,
  severity: z
    .enum(["action_required", "attention", "info"])
    .catch("info"),
  issue_id: z.string().nullable().default(null),
  title: z.string().default(""),
  body: z.string().nullable().default(null),
  issue_status: z.string().nullable().default(null) as unknown as z.ZodType<
    InboxItem["issue_status"]
  >,
  read: z.boolean().default(false),
  archived: z.boolean().default(false),
  created_at: z.string().default(""),
  details: z.record(z.string(), z.string()).nullable().default(null),
}).loose();

export const InboxListSchema = z.array(InboxItemSchema).default([]);
export const EMPTY_INBOX_LIST: InboxItem[] = [];

export const MemberWithUserSchema: z.ZodType<MemberWithUser> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  user_id: z.string().default(""),
  role: z.enum(["owner", "admin", "member"]).catch("member"),
  created_at: z.string().default(""),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
}).loose();

export const MemberListSchema = z.array(MemberWithUserSchema).default([]);
export const EMPTY_MEMBER_LIST: MemberWithUser[] = [];

const AgentInvocationTargetSchema: z.ZodType<AgentInvocationTarget> = z
  .object({
    target_type: z.enum(["workspace", "member", "team"]).catch("team"),
    target_id: z
      .string()
      .nullable()
      .optional()
      .catch(null)
      .transform((v) => v ?? null),
  })
  .loose();

// Agent schema is loose on every enum / structural field — the agent table is
// where new modes/visibilities/statuses get added most often. We need only id,
// name, avatar_url, and a couple of flags for the assignee picker + chat
// header; everything else is informational and safe to default.
export const AgentSchema: z.ZodType<Agent> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  runtime_id: z.string().default(""),
  runtime_bound: z.boolean().optional(),
  // Smart runtime routing (JEF-237). "fixed" pins the agent to runtime_id;
  // "auto" lets the router pick per task, keeping runtime_id as the preferred
  // fallback. Older backends omit it — consumers must read undefined as
  // "fixed", which is why this stays optional rather than defaulting.
  runtime_routing: z
    .enum(["fixed", "auto"])
    .optional() as unknown as z.ZodType<Agent["runtime_routing"]>,
  name: z.string().default(""),
  description: z.string().default(""),
  instructions: z.string().default(""),
  conversation_starters: z
    .array(
      z
        .object({
          label: z.string().default(""),
          prompt: z.string().default(""),
        })
        .loose(),
    )
    .catch([])
    .default([]),
  avatar_url: z.string().nullable().default(null),
  runtime_mode: z.string().catch("daemon") as unknown as z.ZodType<
    Agent["runtime_mode"]
  >,
  runtime_config: z.record(z.string(), z.unknown()).default({}),
  custom_args: z.array(z.string()).default([]),
  // MUL-2600: agent resource shape no longer carries custom_env or
  // custom_env_redacted. Mobile keeps only the coarse metadata that
  // mirrors web's expectations. Real env values are reachable via the
  // dedicated /env endpoint and we don't expose env editing on mobile.
  has_custom_env: z.boolean().optional(),
  custom_env_key_count: z.number().optional(),
  visibility: z.string().catch("workspace") as unknown as z.ZodType<
    Agent["visibility"]
  >,
  permission_mode: z.enum(["private", "public_to"]).catch("private"),
  invocation_targets: z.array(AgentInvocationTargetSchema).default([]),
  status: z.string().catch("active") as unknown as z.ZodType<Agent["status"]>,
  max_concurrent_tasks: z.number().default(1),
  model: z.string().default(""),
  owner_id: z.string().nullable().default(null),
  skills: z.array(z.unknown()).default([]) as unknown as z.ZodType<
    Agent["skills"]
  >,
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  archived_at: z.string().nullable().default(null),
  archived_by: z.string().nullable().default(null),
}).loose();

export const AgentListSchema = z.array(AgentSchema).default([]);
export const EMPTY_AGENT_LIST: Agent[] = [];

// Runtime device — the daemon (local or cloud) an agent binds to. Mobile reads
// it for the presence dot: `status` + `last_seen_at` drive the three-state
// availability derivation in @multica/core/agents/derive-presence. All other
// fields default safely so a backend that adds optional new metadata
// (timezone, visibility flags, etc.) doesn't break the parse.
export const RuntimeSchema: z.ZodType<RuntimeDevice> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  daemon_id: z.string().nullable().default(null),
  name: z.string().default(""),
  // User-set alias (MUL-4217). Absent on an older backend — `runtimeDisplayName`
  // treats null/blank as "use name".
  custom_name: z.string().nullable().default(null),
  runtime_mode: z.string().catch("local") as unknown as z.ZodType<
    RuntimeDevice["runtime_mode"]
  >,
  provider: z.string().default(""),
  launch_header: z.string().default(""),
  // The two fields presence derivation actually reads. Status defaults to
  // "offline" — a runtime row with an unparseable status is treated as
  // unreachable, which is the safe degrade for the dot.
  status: z.enum(["online", "offline"]).catch("offline"),
  last_seen_at: z.string().nullable().default(null),
  device_info: z.string().default(""),
  metadata: z.record(z.string(), z.unknown()).default({}),
  owner_id: z.string().nullable().default(null),
  visibility: z.string().catch("private") as unknown as z.ZodType<
    RuntimeDevice["visibility"]
  >,
  timezone: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const RuntimeListSchema = z.array(RuntimeSchema).default([]);
export const EMPTY_RUNTIME_LIST: RuntimeDevice[] = [];

// Squad schema — fields mobile actually consumes for the @mention suggestion
// bar (id, name, archived_at filter) plus identity/timestamp fields that are
// safe to default. `.loose()` so the server can add squad fields without
// breaking the parser.
export const SquadSchema: z.ZodType<Squad> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  name: z.string().default(""),
  description: z.string().default(""),
  instructions: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  leader_id: z.string().default(""),
  creator_id: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  archived_at: z.string().nullable().default(null),
  archived_by: z.string().nullable().default(null),
}).loose();

export const SquadListSchema = z.array(SquadSchema).default([]);
export const EMPTY_SQUAD_LIST: Squad[] = [];

// Single-issue fallback used by getIssue. Mobile reuses IssueSchema from core
// for parsing; this sentinel lets parseWithFallback yield a structurally-
// valid Issue when the response drifts. `id: ""` flags drift downstream — the
// detail screen treats it as "issue not found" and shows the empty state.
export const EMPTY_ISSUE_FALLBACK: import("@multica/core/types").Issue = {
  id: "",
  workspace_id: "",
  number: 0,
  identifier: "",
  title: "",
  description: null,
  status: "backlog",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "",
  updated_at: "",
};

// Sub-issue-from-comment preview fallback (mirrors EMPTY_ISSUE_FALLBACK's
// sentinel pattern above). Mobile reuses SourceContextPreviewSchema from
// core for parsing. `capture_token: ""` never validates on the server
// (ParseSourceContextToken rejects an empty token), so
// api.getCommentSubIssuePreview treats this sentinel as a failure and
// throws rather than silently proceeding with an unusable token.
export const EMPTY_SOURCE_CONTEXT_PREVIEW: import("@multica/core/types").SourceContextPreview = {
  source_issue: {
    id: "",
    identifier: "",
    number: 0,
    title: "",
    description: null,
    created_at: "",
    updated_at: "",
    revision: 0,
    attachments: [],
  },
  comment_thread: [],
  anchor_comment_id: "",
  capture_token: "",
  limits: {
    comment_count: 0,
    text_bytes: 0,
    attachment_count: 0,
    attachment_bytes: 0,
  },
};

// Helpers re-exported for ergonomic single-import at the call site.
export type { Label, Project, ProjectResource };

// Inbox zero (K63): my pending Decision Cards, options included, ordered and
// capped on the server. Mirrors packages/core/api/schemas.ts InboxDecisionsSchema.
export const InboxDecisionSchema = z.object({
  inbox_item_id: z.string().default(""),
  issue_id: z.string().default(""),
  issue_identifier: z.string().catch("").default(""),
  issue_title: z.string().catch("").default(""),
  risk_score: z.number().catch(0).default(0),
  decision: z.object({
    id: z.string(),
    issue_id: z.string().default(""),
    question: z.string().default(""),
    options: z.array(z.object({ id: z.string(), label: z.string().default(""), impact: z.string().optional() }).loose()).catch([]).default([]),
    recommended_option_id: z.string().optional(),
    urgency: z.string().default("normal"),
    sla_deadline_at: z.string().nullable().optional().catch(null),
    created_at: z.string().default(""),
  }).loose(),
}).loose();

export const InboxDecisionsSchema = z.object({
  decisions: z.array(InboxDecisionSchema).catch([]).default([]),
  total: z.number().int().catch(0).default(0),
}).loose();

export type InboxDecision = z.infer<typeof InboxDecisionSchema>;
export type InboxDecisions = z.infer<typeof InboxDecisionsSchema>;

// ---------------------------------------------------------------------------
// Run replay (GET /api/tasks/{taskId}/replay) — mobile-only until web's
// scrubber promotes a schema to core. Mirrors `RunReplayResponse` in
// server/internal/handler/task_replay.go. Event `kind` stays a free string so
// a kind newer than this build still renders (Behavioral parity: never drop a
// category).
// ---------------------------------------------------------------------------

export const RunReplayActorSchema = z.looseObject({
  type: z.string().default(""),
  id: z.string().default(""),
  name: z.string().default(""),
});

export const RunReplayEventSchema = z.looseObject({
  seq: z.number(),
  at: z.string(),
  kind: z.string(),
  actor: RunReplayActorSchema.default({ type: "", id: "", name: "" }),
  title: z.string().default(""),
  text: z.string().default(""),
  data: z.record(z.string(), z.unknown()).nullable().default(null),
  source: z.string().default(""),
  source_id: z.string().default(""),
  prev_hash: z.string().default(""),
  hash: z.string().default(""),
});

export const RunReplayLinkSchema = z.looseObject({
  relation: z.string(),
  task_id: z.string(),
  agent_id: z.string().default(""),
  agent_name: z.string().default(""),
});

export const RunReplaySchema = z.looseObject({
  run: z.looseObject({
    id: z.string(),
    issue_id: z.string().default(""),
    agent_id: z.string().default(""),
    agent_name: z.string().default(""),
    status: z.string().default(""),
    trust_mode: z.string().default(""),
    effect_mode: z.string().default(""),
    model: z.string().default(""),
    runtime_id: z.string().default(""),
    created_at: z.string().nullable().default(null),
    started_at: z.string().nullable().default(null),
    completed_at: z.string().nullable().default(null),
    links: z.array(RunReplayLinkSchema).nullable().default([]),
  }),
  events: z.array(RunReplayEventSchema).nullable().default([]),
  total: z.number().default(0),
  next_cursor: z.number().nullable().default(null),
  head_hash: z.string().default(""),
  cost: z
    .looseObject({
      input_tokens: z.number().default(0),
      output_tokens: z.number().default(0),
      cost_usd_ticks: z.number().nullable().default(null),
    })
    .default({ input_tokens: 0, output_tokens: 0, cost_usd_ticks: null }),
  sealed: z
    .looseObject({
      events: z.number().default(0),
      head_hash: z.string().default(""),
      sealed_at: z.string().default(""),
      verified: z.boolean().default(false),
    })
    .nullable()
    .default(null),
});

export type RunReplayEvent = z.infer<typeof RunReplayEventSchema>;
export type RunReplayLink = z.infer<typeof RunReplayLinkSchema>;
export type RunReplay = z.infer<typeof RunReplaySchema>;

// Voice-dictated issue draft (K36): POST /api/issues/from-voice-transcript
// answers with an editable draft, never an issue. Server shape:
// server/internal/handler/issue_from_voice.go `VoiceIssueDraft`. Every field
// tolerates drift because the draft screen renders it straight into inputs —
// a partial value there would be an uneditable form, not a caught error.
export const VoiceIssueDraftSchema = z.object({
  title: z.string().catch("").default(""),
  description: z.string().catch("").default(""),
  suggested_labels: z.array(z.string()).catch([]).default([]),
}).loose();

export type VoiceIssueDraft = z.infer<typeof VoiceIssueDraftSchema>;

export const EMPTY_VOICE_ISSUE_DRAFT: VoiceIssueDraft = {
  title: "",
  description: "",
  suggested_labels: [],
};

// ---------------------------------------------------------------------------
// Issue goal loop — GET/PUT /api/issues/{id}/goal, POST .../pause|resume|
// answer. Mirrors the wire shape of `goalstate.State` in
// server/pkg/goalstate/goalstate.go. Mobile-only until web's goal-loop panel
// promotes a schema to core (packages/core/types/issue-goal.ts already
// defines the strict `IssueGoal` interface with a closed `status` union, but
// no zod schema exists there yet — this file's `status` and `question.kind`
// stay free strings on purpose so a status/kind the server adds before this
// build ships still renders instead of vanishing (root CLAUDE.md "API
// Response Compatibility"); `apps/mobile/lib/issue-goal-display.ts` supplies
// the label fallback for an unrecognised value).
export const IssueGoalQuestionSchema = z.object({
  kind: z.string().catch("text").default("text"),
  prompt: z.string().catch("").default(""),
  options: z.array(z.string()).catch([]).default([]),
  run_id: z.string().catch("").default(""),
  asked_at: z.string().catch("").default(""),
  answer: z.string().optional(),
  answered_by: z.string().optional(),
  answered_by_name: z.string().optional(),
  answered_at: z.string().optional(),
}).loose();

export const IssueGoalSchema = z.object({
  id: z.string(),
  issue_id: z.string(),
  goal: z.string().catch("").default(""),
  status: z.string().catch("active").default("active"),
  continuation: z.number().catch(0).default(0),
  max_continuations: z.number().catch(0).default(0),
  no_progress: z.number().catch(0).default(0),
  last_outcome: z.string().catch("").default(""),
  last_blocker: z.string().optional(),
  last_reason: z.string().optional(),
  next_step: z.string().optional(),
  evidence: z.array(z.string()).catch([]).default([]),
  question: IssueGoalQuestionSchema.nullable().optional(),
  last_run_id: z.string().optional(),
  chain_root_task_id: z.string().optional(),
  done_request_id: z.string().optional(),
  set_by_type: z.string().catch("member").default("member"),
  updated_at: z.string().catch("").default(""),
}).loose();

export const IssueGoalResponseSchema = z.object({
  goal: IssueGoalSchema.nullable(),
}).loose();

export type IssueGoalQuestion = z.infer<typeof IssueGoalQuestionSchema>;
export type IssueGoal = z.infer<typeof IssueGoalSchema>;
export type IssueGoalResponse = z.infer<typeof IssueGoalResponseSchema>;

export const EMPTY_ISSUE_GOAL_RESPONSE: IssueGoalResponse = { goal: null };

// ---------------------------------------------------------------------------
// Inline approvals — GET /api/approvals[?issue_id=]. The unified feed of
// every pending human ask: Decision Cards, held status transitions
// (transition gate, F28) and goal-loop questions. Field shape mirrors
// `packages/core/approvals/schemas.ts` (ApprovalItemSchema /
// ApprovalsResponseSchema) exactly; not imported from there because
// `@multica/core/approvals` only exports its aggregate `./index.ts`, which
// also re-exports `./queries.ts` — a module that imports web's live `api`
// singleton (`../api`). That is not "types and pure functions from
// @multica/core" (apps/mobile/CLAUDE.md import whitelist), so this file
// keeps its own copy; mirror both by hand if either changes.
export type ApprovalSource = "decision" | "transition" | "goal_question";
export type ApprovalKind =
  | "decision"
  | "gate"
  | "plan"
  | "interview"
  | "preview"
  | "watchdog"
  | "pipeline"
  | "goal_attach"
  | "org_assign"
  | "transition"
  | "goal_question";

export const ApprovalOptionSchema = z.object({
  id: z.string().catch(""),
  label: z.string().catch(""),
  impact: z.string().catch(""),
}).loose();

export const ApprovalGateSchema = z.object({
  id: z.string().catch(""),
  task_id: z.string().catch(""),
  gate_type: z.string().catch(""),
  summary: z.string().catch(""),
  details: z.record(z.string(), z.unknown()).catch({}),
  status: z.string().catch("pending"),
  created_at: z.string().catch(""),
  expires_at: z.string().nullable().catch(null),
  resolved_at: z.string().nullable().catch(null),
}).loose();

export const ApprovalTransitionSchema = z.object({
  request_id: z.string().catch(""),
  from_status: z.string().catch(""),
  to_status: z.string().catch(""),
  rule_id: z.string().nullable().catch(null),
  approver_roles: z.array(z.string()).catch([]),
}).loose();

export const ApprovalGoalQuestionSchema = z.object({
  kind: z.string().catch("text"),
  prompt: z.string().catch(""),
  options: z.array(z.string()).catch([]),
  run_id: z.string().catch(""),
  asked_at: z.string().catch(""),
}).loose();

export const ApprovalItemSchema = z.object({
  id: z.string().catch(""),
  source: z.string().catch("decision"),
  kind: z.string().catch("decision"),
  issue: z.object({
    id: z.string().catch(""),
    identifier: z.string().catch(""),
    title: z.string().catch(""),
    status: z.string().catch(""),
  }).loose().catch({ id: "", identifier: "", title: "", status: "" }),
  task_id: z.string().catch(""),
  asked_by: z.object({
    type: z.string().catch(""),
    id: z.string().catch(""),
    name: z.string().catch(""),
  }).loose().catch({ type: "", id: "", name: "" }),
  question: z.string().catch(""),
  options: z.array(ApprovalOptionSchema).catch([]),
  recommended_option_id: z.string().catch(""),
  urgency: z.string().catch("normal"),
  created_at: z.string().catch(""),
  expires_at: z.string().nullable().catch(null),
  sla_deadline_at: z.string().nullable().catch(null),
  can_decide: z.boolean().catch(false),
  cannot_decide_reason: z.string().catch(""),
  gate: ApprovalGateSchema.nullable().catch(null),
  transition: ApprovalTransitionSchema.nullable().catch(null),
  goal_question: ApprovalGoalQuestionSchema.nullable().catch(null),
}).loose();

export const RunHaltSchema = z.object({
  halted: z.boolean().catch(false),
  reason: z.string().catch(""),
  halted_by: z.string().catch(""),
  halted_at: z.string().nullable().catch(null),
}).loose();

export type ApprovalOption = z.infer<typeof ApprovalOptionSchema>;
export type ApprovalGate = z.infer<typeof ApprovalGateSchema>;
export type ApprovalTransition = z.infer<typeof ApprovalTransitionSchema>;
export type ApprovalGoalQuestion = z.infer<typeof ApprovalGoalQuestionSchema>;
export type ApprovalItem = z.infer<typeof ApprovalItemSchema>;
export type RunHalt = z.infer<typeof RunHaltSchema>;

export const EMPTY_RUN_HALT: RunHalt = { halted: false, reason: "", halted_by: "", halted_at: null };

export const ApprovalsResponseSchema = z.object({
  approvals: z.array(ApprovalItemSchema).catch([]),
  total: z.number().catch(0),
  run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
}).loose();

export type ApprovalsResponse = z.infer<typeof ApprovalsResponseSchema>;

export const EMPTY_APPROVALS: ApprovalsResponse = { approvals: [], total: 0, run_halt: EMPTY_RUN_HALT };

// ---------------------------------------------------------------------------
// Runs fleet (OS plan, chantier 4) — GET /api/runs, POST /api/runs/cancel,
// POST /api/runs/kill-switch. Field shape mirrors
// `server/internal/handler/runs.go` (RunResponse / RunsSummary /
// RunsResponse) and `apps/docs/content/docs/runs.mdx`. Not imported from
// `@multica/core` for the same reason as the approvals section above: no
// package there exports this shape as a pure value yet, so this is the
// mobile-local copy until one does; mirror both by hand if either changes.
//
// A run is an AgentTask plus the names, cost and blocker a fleet view needs
// — the server literally embeds AgentTaskResponse, so RunSchema extends the
// AgentTaskSchema above rather than repeating its fields.
export const RunBlockerSchema = z.object({
  kind: z.string().catch(""),
  id: z.string().optional(),
  decision_id: z.string().optional(),
  summary: z.string().catch(""),
  since: z.string().nullable().catch(null),
}).loose();
export type RunBlocker = z.infer<typeof RunBlockerSchema>;

export const RunIssueRefSchema = z.object({
  id: z.string().catch(""),
  identifier: z.string().catch(""),
  title: z.string().catch(""),
  status: z.string().catch(""),
}).loose();
export type RunIssueRef = z.infer<typeof RunIssueRefSchema>;

// `.and()` rather than `.extend()`: AgentTaskSchema above is annotated
// `z.ZodType<AgentTask>`, which erases the concrete ZodObject type and
// its `.extend()` method. `.and()` (ZodIntersection) is declared on the
// base ZodType interface itself, so it's available regardless of that
// annotation, and its inferred output is the plain intersection type —
// exactly `AgentTask & { the fields below }`, same shape `.extend()`
// would have produced.
export const RunSchema = AgentTaskSchema.and(
  z.object({
    agent_name: z.string().catch(""),
    issue: RunIssueRefSchema.nullable().catch(null),
    cost_usd_ticks: z.number().catch(0),
    duration_ms: z.number().catch(0),
    silence_ms: z.number().catch(0),
    blocked_on: RunBlockerSchema.nullable().catch(null),
  }),
);
export type Run = z.infer<typeof RunSchema>;

export const RunsSummarySchema = z.object({
  active: z.number().catch(0),
  queued: z.number().catch(0),
  running: z.number().catch(0),
  blocked: z.number().catch(0),
  completed_since: z.number().catch(0),
  failed_since: z.number().catch(0),
  cancelled_since: z.number().catch(0),
  cost_since_usd_ticks: z.number().catch(0),
  since: z.string().catch(""),
  run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
}).loose();
export type RunsSummary = z.infer<typeof RunsSummarySchema>;

export const EMPTY_RUNS_SUMMARY: RunsSummary = {
  active: 0,
  queued: 0,
  running: 0,
  blocked: 0,
  completed_since: 0,
  failed_since: 0,
  cancelled_since: 0,
  cost_since_usd_ticks: 0,
  since: "",
  run_halt: EMPTY_RUN_HALT,
};

export const RunsResponseSchema = z.object({
  runs: z.array(RunSchema).catch([]),
  next_cursor: z.string().optional(),
  summary: RunsSummarySchema.catch(EMPTY_RUNS_SUMMARY),
}).loose();
export type RunsResponse = z.infer<typeof RunsResponseSchema>;

export const EMPTY_RUNS_RESPONSE: RunsResponse = {
  runs: [],
  summary: EMPTY_RUNS_SUMMARY,
};

export const RunCancelOutcomeSchema = z.object({
  task_id: z.string().catch(""),
  outcome: z.string().catch("error"),
  error: z.string().optional(),
}).loose();
export type RunCancelOutcome = z.infer<typeof RunCancelOutcomeSchema>;

export const CancelRunsResponseSchema = z.object({
  results: z.array(RunCancelOutcomeSchema).catch([]),
  cancelled: z.number().catch(0),
}).loose();
export type CancelRunsResponse = z.infer<typeof CancelRunsResponseSchema>;
export const EMPTY_CANCEL_RUNS_RESPONSE: CancelRunsResponse = { results: [], cancelled: 0 };

export const KillSwitchResponseSchema = z.object({
  run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
  cancelled: z.number().catch(0),
  results: z.array(RunCancelOutcomeSchema).catch([]),
}).loose();
export type KillSwitchResponse = z.infer<typeof KillSwitchResponseSchema>;
export const EMPTY_KILL_SWITCH_RESPONSE: KillSwitchResponse = {
  run_halt: EMPTY_RUN_HALT,
  cancelled: 0,
  results: [],
};

// ---------------------------------------------------------------------------
// Workspace doctrine (OS plan, chantier 22) — GET /api/workspace/doctrine,
// /versions, /versions/{id}/diff, /reports. Field shape mirrors
// `server/internal/handler/workspace_doctrine.go` (DoctrineResponse /
// DoctrineVersionResponse / DoctrineReportResponse / DoctrineDiffLine).
//
// Mobile-local rather than @multica/core/api/schemas for the same reason as
// the approvals and runs sections above: no package there exports this shape
// yet (the doctrine landed server-first; there is no packages/views
// implementation to mirror either, so the parity target is the handler).
// Mirror both by hand if either side changes.
export const DoctrineVersionSchema = z.object({
  id: z.string().catch(""),
  revision: z.number().nullable().catch(null),
  content: z.string().catch(""),
  status: z.string().catch("superseded"),
  note: z.string().catch(""),
  author_id: z.string().nullable().catch(null),
  reviewed_by: z.string().nullable().catch(null),
  reviewed_at: z.string().nullable().catch(null),
  review_note: z.string().catch(""),
  restored_from_revision: z.number().nullable().catch(null),
  created_at: z.string().catch(""),
  bytes: z.number().catch(0),
}).loose();
export type DoctrineVersion = z.infer<typeof DoctrineVersionSchema>;

export const DoctrineSchema = z.object({
  content: z.string().catch(""),
  revision: z.number().catch(0),
  updated_at: z.string().nullable().catch(null),
  updated_by: z.string().nullable().catch(null),
  byte_limit: z.number().catch(0),
  require_review: z.boolean().catch(false),
  can_publish: z.boolean().catch(false),
  active_version_id: z.string().nullable().catch(null),
  pending: DoctrineVersionSchema.nullable().catch(null),
  open_reports: z.number().catch(0),
}).loose();
export type Doctrine = z.infer<typeof DoctrineSchema>;

export const EMPTY_DOCTRINE: Doctrine = {
  content: "",
  revision: 0,
  updated_at: null,
  updated_by: null,
  byte_limit: 0,
  require_review: false,
  can_publish: false,
  active_version_id: null,
  pending: null,
  open_reports: 0,
};

export const DoctrineVersionsResponseSchema = z.object({
  versions: z.array(DoctrineVersionSchema).catch([]),
  next_cursor: z.string().nullable().catch(null),
}).loose();
export type DoctrineVersionsResponse = z.infer<
  typeof DoctrineVersionsResponseSchema
>;
export const EMPTY_DOCTRINE_VERSIONS: DoctrineVersionsResponse = {
  versions: [],
  next_cursor: null,
};

export const DoctrineDiffLineSchema = z.object({
  // same | add | del. Kept a plain string (not an enum) so an unknown kind
  // renders untinted instead of taking the diff screen down.
  kind: z.string().catch("same"),
  text: z.string().catch(""),
}).loose();
export type DoctrineDiffLine = z.infer<typeof DoctrineDiffLineSchema>;

export const DoctrineDiffSchema = z.object({
  // The diff endpoint blanks `content` on both sides — only the lines carry
  // the text.
  from: DoctrineVersionSchema.nullable().catch(null),
  to: DoctrineVersionSchema.nullable().catch(null),
  lines: z.array(DoctrineDiffLineSchema).catch([]),
  added: z.number().catch(0),
  removed: z.number().catch(0),
}).loose();
export type DoctrineDiff = z.infer<typeof DoctrineDiffSchema>;
export const EMPTY_DOCTRINE_DIFF: DoctrineDiff = {
  from: null,
  to: null,
  lines: [],
  added: 0,
  removed: 0,
};

export const DoctrineReportSchema = z.object({
  id: z.string().catch(""),
  doctrine_revision: z.number().catch(0),
  // conflict | refusal | ambiguity — string, not enum, so a kind added
  // server-side still renders (root CLAUDE.md API compatibility).
  kind: z.string().catch(""),
  summary: z.string().catch(""),
  passage: z.string().catch(""),
  reporter_type: z.string().catch("member"),
  reporter_id: z.string().catch(""),
  task_id: z.string().nullable().catch(null),
  issue_id: z.string().nullable().catch(null),
  // open | acknowledged | dismissed
  status: z.string().catch("open"),
  resolved_by: z.string().nullable().catch(null),
  resolved_at: z.string().nullable().catch(null),
  resolution_note: z.string().catch(""),
  created_at: z.string().catch(""),
}).loose();
export type DoctrineReport = z.infer<typeof DoctrineReportSchema>;

export const DoctrineReportsResponseSchema = z.object({
  reports: z.array(DoctrineReportSchema).catch([]),
}).loose();
export type DoctrineReportsResponse = z.infer<
  typeof DoctrineReportsResponseSchema
>;
export const EMPTY_DOCTRINE_REPORTS: DoctrineReportsResponse = { reports: [] };

// ---------------------------------------------------------------------------
// Packs (OS plan, vague B) — GET /api/packs, /api/packs/{id},
// /api/packs/installed, /api/packs/installed/{id}, and the preview / install
// / uninstall responses. Field-for-field mirror of
// `packages/core/packs/schemas.ts` (itself mirroring `PackSummary`,
// `PackPrerequisite`, `PackContents`, `PackInstallResponse`, `packPreview`
// and `packUninstallReport` in `server/internal/handler/packs.go`).
//
// Copied rather than imported: `@multica/core` exports `./packs` only as the
// barrel, which pulls the api client, the react-query hooks and every other
// feature's key factory with it — not on mobile's import whitelist. Same
// mirror-don't-import rule as `data/realtime/issue-ws-updaters.ts`. Keep the
// two in step by hand.
//
// Leniency is deliberate and matches core: server enums stay `z.string()` so
// a newer server shipping a new domain, prerequisite kind, source or
// strategy still renders (root CLAUDE.md "API Compatibility"; every switch
// on these values has a default branch — see lib/packs-display.ts).

/** Mirrors `transferStrategies`. `skip` is the server default on a first install. */
export const PACK_STRATEGIES = ["skip", "merge", "rename"] as const;
export type PackStrategy = (typeof PACK_STRATEGIES)[number];

export const PackMetricSchema = z
  .object({
    label: z.string().catch(""),
    description: z.string().catch(""),
    hint: z.string().catch(""),
  })
  .loose()
  .catch({ label: "", description: "", hint: "" });

export const PackPrerequisiteSchema = z.object({
  kind: z.string().catch(""),
  name: z.string().catch(""),
  optional: z.boolean().catch(false),
  note: z.string().catch(""),
  // met | missing | unknown — `unknown` means the server cannot tell.
  status: z.string().catch("unknown"),
}).loose();
export type PackPrerequisite = z.infer<typeof PackPrerequisiteSchema>;

export const PackChangelogRowSchema = z.object({
  version: z.string().catch(""),
  note: z.string().catch(""),
}).loose();

export const PackManifestSchema = z.object({
  id: z.string().catch(""),
  version: z.string().catch(""),
  title: z.string().catch(""),
  summary: z.string().catch(""),
  /** Markdown. */
  description: z.string().catch(""),
  domain: z.string().catch("other"),
  wave: z.number().catch(0),
  author: z.string().catch(""),
  license: z.string().catch(""),
  tags: z.array(z.string()).catch([]),
  works_without_agents: z.boolean().catch(false),
  metric: PackMetricSchema,
  prerequisites: z.array(PackPrerequisiteSchema).catch([]),
  changelog: z.array(PackChangelogRowSchema).catch([]),
}).loose();
export type PackManifest = z.infer<typeof PackManifestSchema>;

export const EMPTY_PACK_MANIFEST: PackManifest = {
  id: "",
  version: "",
  title: "",
  summary: "",
  description: "",
  domain: "other",
  wave: 0,
  author: "",
  license: "",
  tags: [],
  works_without_agents: false,
  metric: { label: "", description: "", hint: "" },
  prerequisites: [],
  changelog: [],
};

const PackCountsSchema = z.record(z.string(), z.number()).catch({});

export const PackSummarySchema = z.object({
  manifest: PackManifestSchema,
  counts: PackCountsSchema,
  builtin: z.boolean().catch(false),
  installed_version: z.string().nullable().catch(null),
  install_id: z.string().nullable().catch(null),
  upgrade_available: z.boolean().catch(false),
  prerequisites: z.array(PackPrerequisiteSchema).catch([]),
}).loose();
export type PackSummary = z.infer<typeof PackSummarySchema>;

export const EMPTY_PACK_SUMMARY: PackSummary = {
  manifest: EMPTY_PACK_MANIFEST,
  counts: {},
  builtin: false,
  installed_version: null,
  install_id: null,
  upgrade_available: false,
  prerequisites: [],
};

/** `Record<kind, names[]>` — the names a pack would create, per kind. */
export const PackContentsSchema = z
  .record(z.string(), z.array(z.string()).catch([]))
  .catch({});
export type PackContents = z.infer<typeof PackContentsSchema>;

export const PackCollisionSchema = z.object({
  kind: z.string().catch(""),
  name: z.string().catch(""),
  existing_id: z.string().catch(""),
}).loose();
export type PackCollision = z.infer<typeof PackCollisionSchema>;

export const PackItemSchema = z.object({
  kind: z.string().catch(""),
  name: z.string().catch(""),
  id: z.string().catch(""),
  action: z.string().catch(""),
}).loose();
export type PackItem = z.infer<typeof PackItemSchema>;

export const PackReportSchema = z.object({
  created: PackCountsSchema,
  merged: PackCountsSchema,
  skipped: z.array(PackCollisionSchema).catch([]),
  warnings: z.array(z.string()).catch([]),
  items: z.array(PackItemSchema).catch([]),
}).loose();
export type PackReport = z.infer<typeof PackReportSchema>;

export const EMPTY_PACK_REPORT: PackReport = {
  created: {},
  merged: {},
  skipped: [],
  warnings: [],
  items: [],
};

/**
 * One row of the install ledger. `report` and `manifest` stay opaque records
 * (the ledger keeps the transfer report on an install and the uninstall
 * report on a removed one); the typed report comes back from the mutations.
 */
export const PackInstallSchema = z.object({
  id: z.string().catch(""),
  pack_id: z.string().catch(""),
  pack_version: z.string().catch(""),
  title: z.string().catch(""),
  // builtin | upload | workspace
  source: z.string().catch("builtin"),
  strategy: z.string().catch("skip"),
  // installed | failed | removed
  status: z.string().catch("installed"),
  run_id: z.string().nullable().catch(null),
  report: z.record(z.string(), z.unknown()).catch({}),
  manifest: z.record(z.string(), z.unknown()).catch({}),
  installed_by: z.string().nullable().catch(null),
  installed_at: z.string().catch(""),
  removed_at: z.string().nullable().catch(null),
  item_count: z.number().catch(0),
  metric: PackMetricSchema,
  domain: z.string().catch("other"),
  /** The catalogue version this install can move to, when newer. */
  upgrade_to: z.string().nullable().catch(null),
  bundle_sha256: z.string().catch(""),
}).loose();
export type PackInstall = z.infer<typeof PackInstallSchema>;

export const PackCatalogueSchema = z.object({
  packs: z.array(PackSummarySchema).catch([]),
  domains: z.array(z.string()).catch([]),
}).loose();
export type PackCatalogue = z.infer<typeof PackCatalogueSchema>;
export const EMPTY_PACK_CATALOGUE: PackCatalogue = { packs: [], domains: [] };

export const PackDetailSchema = z.object({
  pack: PackSummarySchema,
  contents: PackContentsSchema,
  /** The pack.yaml itself — not rendered on the phone. */
  source: z.string().catch(""),
}).loose();
export type PackDetail = z.infer<typeof PackDetailSchema>;
export const EMPTY_PACK_DETAIL: PackDetail = {
  pack: EMPTY_PACK_SUMMARY,
  contents: {},
  source: "",
};

export const PackPreviewSchema = z.object({
  pack: PackSummarySchema,
  contents: PackContentsSchema,
  collisions: z.array(PackCollisionSchema).catch([]),
  problems: z.array(z.string()).catch([]),
  strategies: z.array(z.string()).catch([...PACK_STRATEGIES]),
  /** The strategy the server picked: skip on a first install, merge on an upgrade. */
  strategy: z.string().catch("skip"),
  installed: PackInstallSchema.nullable().catch(null),
  /** Empty when installable; otherwise why not (same version, downgrade). */
  blocked: z.string().catch(""),
}).loose();
export type PackPreview = z.infer<typeof PackPreviewSchema>;

export const EMPTY_PACK_PREVIEW: PackPreview = {
  pack: EMPTY_PACK_SUMMARY,
  contents: {},
  collisions: [],
  problems: [],
  strategies: [...PACK_STRATEGIES],
  strategy: "skip",
  installed: null,
  blocked: "",
};

export const PackInstallListSchema = z.object({
  installs: z.array(PackInstallSchema).catch([]),
}).loose();
export type PackInstallList = z.infer<typeof PackInstallListSchema>;
export const EMPTY_PACK_INSTALL_LIST: PackInstallList = { installs: [] };

export const PackInstallDetailSchema = z.object({
  install: PackInstallSchema,
  items: z.array(PackItemSchema).catch([]),
}).loose();
export type PackInstallDetail = z.infer<typeof PackInstallDetailSchema>;

export const EMPTY_PACK_INSTALL: PackInstall = {
  id: "",
  pack_id: "",
  pack_version: "",
  title: "",
  source: "builtin",
  strategy: "skip",
  status: "installed",
  run_id: null,
  report: {},
  manifest: {},
  installed_by: null,
  installed_at: "",
  removed_at: null,
  item_count: 0,
  metric: { label: "", description: "", hint: "" },
  domain: "other",
  upgrade_to: null,
  bundle_sha256: "",
};

export const EMPTY_PACK_INSTALL_DETAIL: PackInstallDetail = {
  install: EMPTY_PACK_INSTALL,
  items: [],
};

export const PackInstallResultSchema = z.object({
  install: PackInstallSchema,
  report: PackReportSchema,
}).loose();
export type PackInstallResult = z.infer<typeof PackInstallResultSchema>;
export const EMPTY_PACK_INSTALL_RESULT: PackInstallResult = {
  install: EMPTY_PACK_INSTALL,
  report: EMPTY_PACK_REPORT,
};

export const PackUninstallReportSchema = z.object({
  removed: PackCountsSchema,
  kept: z.array(PackItemSchema).catch([]),
  reasons: z.array(z.string()).catch([]),
}).loose();
export type PackUninstallReport = z.infer<typeof PackUninstallReportSchema>;

export const PackUninstallResultSchema = z.object({
  install: PackInstallSchema,
  report: PackUninstallReportSchema,
}).loose();
export type PackUninstallResult = z.infer<typeof PackUninstallResultSchema>;
export const EMPTY_PACK_UNINSTALL_RESULT: PackUninstallResult = {
  install: EMPTY_PACK_INSTALL,
  report: { removed: {}, kept: [], reasons: [] },
};
