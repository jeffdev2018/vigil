import { z } from "zod";
import type {
  AgentBuilderRuntimeSwitch,
  AnchoredThreads,
  AgentBuilderSession,
  AgentBuilderSessionSummary,
  Attachment,
  AutopilotRun,
  BillingBalance,
  BillingBatchesPage,
  BillingCheckoutSessionStatus,
  BillingPriceTier,
  BillingTopupsPage,
  BillingTransactionsPage,
  CancelTaskResponse,
  ChatMessage,
  ChatParticipantList,
  ChatDraftRestoresResponse,
  ChatPendingTask,
  ChatSession,
  PrioritizeQueuedChatTaskResponse,
  SendChatMessageResponse,
  TaskActivityResponse,
  StartMikaOnboardingResponse,
  Comment,
  CreateBillingCheckoutSessionResponse,
  CreateBillingPortalSessionResponse,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
  IssueLimitUsage,
  WorkspaceSubscriptionPrice,
  WorkspaceSubscriptionPrices,
  CreateWorkspaceSubscriptionCheckoutResponse,
  WorkspaceSubscriptionSeatReconcileResult,
  WorkspaceSeatPurchasePreview,
  PurchaseWorkspaceSeatsResponse,
  CreateWorkspaceSubscriptionPortalResponse,
  CronPreviewResponse,
  DingTalkInstallation,
  ListDingTalkInstallationsResponse,
  ListDingTalkGroupsResponse,
  RedeemDingTalkBindingTokenResponse,
  WecomInstallation,
  ListWecomInstallationsResponse,
  RedeemWecomBindingTokenResponse,
  TelegramInstallation,
  ListTelegramInstallationsResponse,
  RedeemTelegramBindingTokenResponse,
  GroupedIssuesResponse,
  GitHubConnectResponse,
  GitHubPullRequest,
  InboxItem,
  InboxWorkspaceUnread,
  TriageStats,
  TriageSource,
  TriageItemsResponse,
  TriageEmailSource,
  Meeting,
  VoiceTranscription,
  RealtimeVoiceSession,
  MeetingListResponse,
  MeetingSegmentResponse,
  CalendarUpcoming,
  CalendarFeed,
  CalendarEventEntry,
  CalendarEventsResponse,
  CalendarAgenda,
  Followup,
  FollowupBudget,
  IssueFollowupsResponse,
  CalendarSlotsResponse,
  CalendarFeedTokenStatus,
  CalendarGoogleImportResult,
  PostmortemStats,
  PostmortemsResponse,
  WorkspaceNote,
  WorkspaceNotesResponse,
  AutopilotDraft,
  AutopilotProposalResponse,
  BrainCapture,
  BrainCapturesResponse,
  OrganizeBrainCaptureResponse,
  WorkspaceNoteSearchResponse,
  Label,
  AgentMemory,
  ProjectMemory,
  AgentMemoryList,
  MemberWithUser,
  Invitation,
  SkillSummary,
  CreatePersonalAccessTokenResponse,
  ChatPinnedAgent,
  PendingChatTasksResponse,
  HasPendingChatTasksResponse,
  Project,
  ListProjectsResponse,
  ProjectResource,
  ListProjectResourcesResponse,
  IssueProperty,
  ListPropertiesResponse,
  QuickAction,
  ListQuickActionsResponse,
  IssuePropertiesResponse,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
  IssueTableGroupsResponse,
  IssueTableRowsResponse,
  ListIssuesResponse,
  ListGitHubInstallationsResponse,
  ListGitHubRepositoriesResponse,
  ListLabelsResponse,
  ListWebhookDeliveriesResponse,
  IssueStatusEntry,
  ListIssueStatusesResponse,
  IssueTypeEntry,
  ListIssueTypesResponse,
  IssueDependencyEdge,
  NotificationPreferenceResponse,
  PluginInstallation,
  PluginInstallationListResponse,
  PluginPackage,
  PluginPackageListResponse,
  PluginPreview,
  PluginSurfaceLaunch,
  ResourceLabelsResponse,
  RuntimeModelListRequest,
  SearchIssuesResponse,
  SearchProjectsResponse,
  ShareLink,
  ShareLinkInfo,
  Skill,
  SkillImportResult,
  Squad,
  RuntimeRoutingStatsResponse,
  WorkflowStatsResponse,
  TimelineEntry,
  User,
  WebhookDelivery,
  WebhookTriggerDryRunResult,
  ScheduleTriggerDryRunResult,
  WorkspaceMcpServer,
  MergeReadiness,
  PRStack,
  IssuePlanEnvelope,
  RuntimeProfile,
  Agent,
  IssueReaction,
  Reaction,
  // JEF-321 batch B
  Workspace,
  IssueUsageSummary,
  AgentActivityBucket,
  AgentRunCount,
  WorkspaceWorkingAgent,
  RuntimeUpdate,
  RuntimeLocalSkillListRequest,
  RuntimeLocalSkillImportRequest,
  AgentTask,
  // JEF-321 batch D
  PinnedItem,
  SquadMember,
  Autopilot,
  AutopilotCollaboratorsResponse,
  AutopilotTrigger,
  ListAutopilotRunsResponse,
  ListVCSConnectionsResponse,
  ListLarkInstallationsResponse,
  ComposioToolkit,
  ComposioConnection,
  ListSlackInstallationsResponse,
} from "../types";
import type {
  CloudRuntimeNode,
  CloudRuntimeNodeActionResult,
} from "../runtimes/cloud-runtime";
import type { CreateFeedbackResponse } from "../feedback/types";

export const PluginConfigFieldSchema = z.object({
  key: z.string(),
  type: z.string().default("string"),
  label: z.string().default(""),
  description: z.string().optional(),
  required: z.boolean().default(false),
  options: z.array(z.string()).default([]),
  placeholder: z.string().optional(),
  multiline: z.boolean().default(false),
}).loose();

export const PluginSurfaceSchema = z.object({
  key: z.string(),
  type: z.string().default(""),
  name: z.string().default(""),
  entry: z.string().default(""),
  platforms: z.array(z.string()).default([]),
}).loose();

export const PluginHookSchema = z.object({
  key: z.string(),
  name: z.string().default(""),
  description: z.string().default(""),
  triggers: z.array(z.string()).default([]),
  events: z.array(z.string()).default([]),
  schedule: z.object({
    cron: z.string().default(""),
    timezone: z.string().default(""),
    next_run_at: z.string().optional(),
  }).loose().optional(),
  transport: z.string().default(""),
}).loose();

export const PluginResourceSchema = z.object({
  type: z.string().default(""),
  key: z.string(),
  entry: z.string().default(""),
}).loose();

export const PluginInstallationSchema = z.object({
  id: z.string(),
  plugin_key: z.string().default(""),
  name: z.string().default(""),
  description: z.string().optional(),
  version: z.string().default(""),
  package_version_id: z.string().default(""),
  enabled: z.boolean().default(false),
  granted_scopes: z.array(z.string()).default([]),
  config_schema: z.array(PluginConfigFieldSchema).default([]),
  config: z.record(z.string(), z.unknown()).default({}),
  configured_secrets: z.array(z.string()).default([]),
  surfaces: z.array(PluginSurfaceSchema).default([]),
  hooks: z.array(PluginHookSchema).default([]),
  resources: z.array(PluginResourceSchema).default([]),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION: PluginInstallation = {
  id: "",
  plugin_key: "",
  name: "",
  version: "",
  package_version_id: "",
  enabled: false,
  granted_scopes: [],
  config_schema: [],
  config: {},
  configured_secrets: [],
  surfaces: [],
  hooks: [],
  resources: [],
  created_at: "",
  updated_at: "",
};

export const PluginInstallationListResponseSchema = z.object({
  plugins: z.array(PluginInstallationSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION_LIST: PluginInstallationListResponse = {
  plugins: [],
};

/**
 * One completed hook call. `status` is the host's classification, not the
 * endpoint's: "refused" means we declined to make the call at all, which is a
 * different problem for the reader than an endpoint that answered badly.
 */
export const PluginHookResultSchema = z.object({
  status: z.string().default("ok"),
  output: z.unknown().optional(),
  error: z.string().optional(),
  latency_ms: z.number().default(0),
  hook_key: z.string().default(""),
  trigger: z.string().default(""),
  attempts: z.number().default(1),
}).loose();

export const PluginInvocationSchema = z.object({
  id: z.string().default(""),
  hook_key: z.string().default(""),
  trigger: z.string().default(""),
  status: z.string().default(""),
  event_type: z.string().optional(),
  attempt: z.number().default(1),
  latency_ms: z.number().default(0),
  error: z.string().optional(),
  delivery_id: z.string().optional(),
  planned_at: z.string().optional(),
  created_at: z.string().default(""),
}).loose();

export const PluginInvocationListSchema = z.object({
  invocations: z.array(PluginInvocationSchema).default([]),
}).loose();

/**
 * Returned once, by the request that minted it. There is no read endpoint for
 * either value, so a client that discards this cannot recover it.
 */
export const PluginTokenIssueSchema = z.object({
  token: z.string().default(""),
  signing_secret: z.string().default(""),
}).loose();

/**
 * Discovered tools for one `mcp`-transport hook.
 *
 * Defaults matter here in the usual direction: an unparseable response yields
 * an EMPTY list and nothing approved, so a drifted backend cannot make the UI
 * render a tool as already-approved.
 */
export const PluginMCPToolSchema = z.object({
  name: z.string().default(""),
  description: z.string().default(""),
  schema_digest: z.string().default(""),
  approved: z.boolean().default(false),
  drifted: z.boolean().default(false),
}).loose();

export const PluginMCPToolListSchema = z.object({
  tools: z.array(PluginMCPToolSchema).default([]),
}).loose();

export const PluginManifestSummarySchema = z.object({
  key: z.string().default(""),
  name: z.string().default(""),
  description: z.string().optional(),
  version: z.string().default(""),
  author: z.object({
    name: z.string().default(""),
    url: z.string().optional(),
  }).loose().default({ name: "" }),
  contributes: z.object({
    hooks: z.array(z.object({
      key: z.string().default(""),
      name: z.string().default(""),
      triggers: z.array(z.string()).default([]),
      schedule: z.object({
        cron: z.string().default(""),
        timezone: z.string().default(""),
      }).loose().optional(),
    }).loose()).default([]),
  }).loose().optional(),
}).loose();

export const PluginPreviewSchema = z.object({
  manifest: PluginManifestSummarySchema,
  scopes: z.array(z.string()).default([]),
  config_schema: z.array(PluginConfigFieldSchema).default([]),
  version_id: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
  installed: z.boolean().default(false),
  installed_version: z.string().optional(),
  added_scopes: z.array(z.string()).default([]),
}).loose();

export const EMPTY_PLUGIN_PREVIEW: PluginPreview = {
  manifest: { key: "", name: "", version: "", author: { name: "" } },
  scopes: [],
  config_schema: [],
  version_id: "",
  version: "",
  digest: "",
  installed: false,
  added_scopes: [],
};

/**
 * A published version. `installed` is the marker the settings page reads to
 * answer "which one am I on"; it defaults to false so a malformed response can
 * never claim a version is running that is not.
 */
export const PluginPackageVersionSchema = z.object({
  id: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
  size_bytes: z.number().default(0),
  published_at: z.string().default(""),
  installed: z.boolean().default(false),
}).loose();

export const PluginPackageSchema = z.object({
  id: z.string().default(""),
  plugin_key: z.string().default(""),
  name: z.string().default(""),
  versions: z.array(PluginPackageVersionSchema).default([]),
  created_at: z.string().default(""),
}).loose();

export const PluginPackageListResponseSchema = z.object({
  packages: z.array(PluginPackageSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_PACKAGE_LIST: PluginPackageListResponse = {
  packages: [],
};

export const EMPTY_PLUGIN_PACKAGE: PluginPackage = {
  id: "",
  plugin_key: "",
  name: "",
  versions: [],
  created_at: "",
};

/** A malformed launch becomes unavailable, never a partly trusted frame. */
export const PluginSurfaceLaunchSchema = z.object({
  url: z.string().default(""),
  bridge_token: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
}).loose();

export const EMPTY_PLUGIN_SURFACE_LAUNCH: PluginSurfaceLaunch = {
  url: "",
  bridge_token: "",
  version: "",
  digest: "",
};

export const GitHubInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  installation_id: z.number().optional(),
  account_login: z.string(),
  account_type: z.string(),
  account_avatar_url: z.string().nullable(),
  created_at: z.string(),
  connected_by: z.string().optional(),
}).loose();

export const ListGitHubInstallationsResponseSchema = z.object({
  installations: z.array(GitHubInstallationSchema).default([]),
  configured: z.boolean().optional().default(false),
  repository_browse_configured: z.boolean().optional().default(false),
  can_manage: z.boolean().optional().default(false),
}).loose();

export const EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE: ListGitHubInstallationsResponse = {
  installations: [],
  configured: false,
  repository_browse_configured: false,
  can_manage: false,
};

export const GitHubConnectResponseSchema = z.object({
  url: z.string().optional(),
  configured: z.boolean().optional().default(false),
}).loose();

export const EMPTY_GITHUB_CONNECT_RESPONSE: GitHubConnectResponse = {
  configured: false,
};

export const GitHubRepositorySchema = z.object({
  id: z.number(),
  full_name: z.string(),
  html_url: z.string(),
  clone_url: z.string(),
  description: z.string().nullable(),
  private: z.boolean(),
  archived: z.boolean(),
  default_branch: z.string(),
}).loose();

export const ListGitHubRepositoriesResponseSchema = z.object({
  repositories: z.array(GitHubRepositorySchema).default([]),
  total_count: z.number().optional().default(0),
  next_page: z.number().nullable().optional().default(null),
}).loose();

export const EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE: ListGitHubRepositoriesResponse = {
  repositories: [],
  total_count: 0,
  next_page: null,
};

export const GitHubPullRequestSchema = z.object({
  id: z.string(),
  provider: z.string().optional().default("github"),
  workspace_id: z.string(),
  repo_owner: z.string(),
  repo_name: z.string(),
  number: z.number(),
  title: z.string(),
  state: z.string(),
  html_url: z.string(),
  branch: z.string().nullable(),
  author_login: z.string().nullable(),
  author_avatar_url: z.string().nullable(),
  merged_at: z.string().nullable(),
  closed_at: z.string().nullable(),
  pr_created_at: z.string(),
  pr_updated_at: z.string(),
  mergeable: z.string().nullable().optional(),
  merge_state_status: z.string().nullable().optional(),
  snapshot_available: z.boolean().optional(),
  checks_rollup: z.string().nullable().optional(),
  checks_conclusion: z.string().nullable().optional(),
  checks_total: z.number().optional().default(0),
  checks_passed: z.number().optional().default(0),
  checks_failed: z.number().optional().default(0),
  checks_running: z.number().optional().default(0),
  checks_pending: z.number().optional().default(0),
  failed_check_names: z.array(z.string()).optional().default([]),
  snapshot_stale: z.boolean().optional().default(false),
  snapshot_fetched_at: z.string().nullable().optional(),
  mergeable_state: z.string().nullable().optional(),
  additions: z.number().optional().default(0),
  deletions: z.number().optional().default(0),
  changed_files: z.number().optional().default(0),
}).loose();

export const IssuePullRequestsResponseSchema = z.object({
  pull_requests: z.array(GitHubPullRequestSchema).default([]),
}).loose();

export const EMPTY_ISSUE_PULL_REQUESTS_RESPONSE: { pull_requests: GitHubPullRequest[] } = {
  pull_requests: [],
};

// Merge readiness (F10). `ready` never defaults to true: a malformed or
// partial answer reads as "not ready", and an unknown blocker kind stays a
// blocker (see github/merge-readiness.ts).
export const MergeReadinessPRSchema = z.object({
  id: z.string(),
  source: z.string().default(""),
  number: z.number().default(0),
  title: z.string().default(""),
  html_url: z.string().default(""),
  state: z.string().default(""),
  mergeable: z.string().nullable().default(null),
  merge_state: z.string().nullable().default(null),
  checks: z.object({
    total: z.number().default(0),
    passed: z.number().default(0),
    failed: z.number().default(0),
    pending: z.number().default(0),
  }).default({ total: 0, passed: 0, failed: 0, pending: 0 }),
  stale_snapshot: z.boolean().default(false),
  ready: z.boolean().default(false),
}).loose();

export const MergeBlockerSchema = z.object({
  kind: z.string(),
  label: z.string().default(""),
  count: z.number().optional(),
  issue_identifier: z.string().optional(),
  pr_number: z.number().optional(),
}).loose();

export const MergeReadinessSchema = z.object({
  prs: z.array(MergeReadinessPRSchema).default([]),
  blockers: z.array(MergeBlockerSchema).default([]),
  unresolved_threads: z.number().default(0),
  open_todos: z.number().default(0),
  ready: z.boolean().default(false),
}).loose();

export const EMPTY_MERGE_READINESS: MergeReadiness = {
  prs: [],
  blockers: [],
  unresolved_threads: 0,
  open_todos: 0,
  ready: false,
};

export const PRStackSchema = z.object({
  nodes: z.array(z.object({
    issue_id: z.string(),
    identifier: z.string().default(""),
    title: z.string().default(""),
    status: z.string().default(""),
    depth: z.number().default(0),
    prs: z.array(MergeReadinessPRSchema).default([]),
    ready: z.boolean().default(false),
  }).loose()).default([]),
  truncated: z.boolean().default(false),
  cyclic: z.boolean().default(false),
}).loose();

export const EMPTY_PR_STACK: PRStack = { nodes: [], truncated: false, cyclic: false };

// Label responses are consumed by settings tables and resource pickers. Keep
// the resource type lenient so newer server scopes do not break older clients,
// while defaulting fields that predate scoped label catalogs.
export const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string().optional().default("issue"),
  name: z.string(),
  description: z.string().optional().default(""),
  color: z.string(),
  usage_count: z.number().optional().default(0),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_LABEL: Label = {
  id: "",
  workspace_id: "",
  resource_type: "issue",
  name: "",
  description: "",
  color: "#6b7280",
  usage_count: 0,
  created_at: "",
  updated_at: "",
};

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

// Issue status catalog (MUL-6243). `category` is parsed as a plain string
// rather than an enum: a newer server could in principle report a category this
// build does not know, and failing the whole catalog parse would leave the UI
// with no statuses at all. Consumers fall back to rendering by `color`/`name`
// when they do not recognize a category.
export const IssueStatusEntrySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  key: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  category: z.string(),
  color: z.string().optional().default("#6b7280"),
  is_system: z.boolean().optional().default(false),
  position: z.number().optional().default(0),
  archived_at: z.string().nullable().optional().default(null),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_STATUS_ENTRY: IssueStatusEntry = {
  id: "",
  workspace_id: "",
  key: "",
  name: "",
  description: "",
  category: "backlog",
  color: "#6b7280",
  is_system: false,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

export const ListIssueStatusesResponseSchema = z.object({
  statuses: z.array(IssueStatusEntrySchema).default([]),
  categories: z.array(z.string()).default([]),
  total: z.number().default(0),
}).loose();

// The fallback carries the 7 built-ins' keys as categories, so a client talking
// to a server that predates this endpoint still has the canonical list.
export const EMPTY_LIST_ISSUE_STATUSES_RESPONSE: ListIssueStatusesResponse = {
  statuses: [],
  categories: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  total: 0,
};

// Work item type catalogue (F30). Every field a client renders is lenient for
// the same reason the status catalogue's are: a newer server can add a type
// this build has never heard of, and failing the whole catalogue parse would
// leave the UI with no types at all — including the four it does know.
export const IssueTypeEntrySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  key: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  color: z.string().optional().default("#6b7280"),
  icon: z.string().optional().default(""),
  is_system: z.boolean().optional().default(false),
  position: z.number().optional().default(0),
  archived_at: z.string().nullable().optional().default(null),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_TYPE_ENTRY: IssueTypeEntry = {
  id: "",
  workspace_id: "",
  key: "",
  name: "",
  description: "",
  color: "#6b7280",
  icon: "",
  is_system: false,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

export const ListIssueTypesResponseSchema = z.object({
  types: z.array(IssueTypeEntrySchema).default([]),
  total: z.number().default(0),
}).loose();

// Empty rather than the four seeded keys: unlike a status category, a type key
// is not a constant of the product — a workspace can rename all four — so a
// client that cannot reach the endpoint must render "no type", not four
// invented rows.
export const EMPTY_LIST_ISSUE_TYPES_RESPONSE: ListIssueTypesResponse = {
  types: [],
  total: 0,
};

// One dependency edge (F30 Gantt arrows). `type` stays a plain string: the
// server may add a relation kind, and the arrow layer draws only the ones it
// recognizes rather than dropping the whole graph.
export const IssueDependencyEdgeSchema = z.object({
  id: z.string(),
  from: z.string(),
  to: z.string(),
  type: z.string(),
}).loose();

export const ListIssueDependencyEdgesResponseSchema = z.object({
  dependencies: z.array(IssueDependencyEdgeSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_ISSUE_DEPENDENCY_EDGES_RESPONSE: {
  dependencies: IssueDependencyEdge[];
  total: number;
} = { dependencies: [], total: 0 };

export const ResourceLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  issue_revision: z.number().int().positive().optional(),
}).loose();

export const EMPTY_RESOURCE_LABELS_RESPONSE: ResourceLabelsResponse = {
  labels: [],
};

// Saved issue views (MUL-4796). `query`/`display` are opaque definition
// blobs interpreted client-side per `definition_version` — keep them as
// loose records so newer servers can add fields freely. `scope_type` /
// `visibility` stay lenient strings; downstream code uses explicit `===`
// comparisons and default branches per the API-compat rules.
export const IssueViewSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  owner_id: z.string().default(""),
  name: z.string().default(""),
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  scope_variant: z.string().nullish(),
  visibility: z.string().default("private"),
  definition_version: z.number().default(1),
  query: z.record(z.string(), z.unknown()).default({}),
  display: z.record(z.string(), z.unknown()).default({}),
  revision: z.number().default(1),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export type IssueView = z.infer<typeof IssueViewSchema>;

export const IssueViewListSchema = z.array(IssueViewSchema);

export const IssueViewPreferenceSchema = z.object({
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  prefs: z.object({
    hidden: z.array(z.string()).default([]),
    order: z.array(z.string()).default([]),
  }).loose().default({ hidden: [], order: [] }),
  updated_at: z.string().default(""),
}).loose();

export type IssueViewPreference = z.infer<typeof IssueViewPreferenceSchema>;

export const EMPTY_ISSUE_VIEW_PREFERENCE: IssueViewPreference = {
  scope_type: "workspace",
  scope_id: null,
  prefs: { hidden: [], order: [] },
  updated_at: "",
};

export interface CreateIssueViewRequest {
  name: string;
  scope_type: "workspace" | "my" | "project";
  scope_id?: string | null;
  scope_variant?: "assigned" | "created" | "involved" | "any" | "members" | "agents" | null;
  visibility: "private" | "workspace";
  definition_version: number;
  query: Record<string, unknown>;
  display: Record<string, unknown>;
}

// Custom property definitions. `type` stays a lenient string so newer server
// types don't break installed clients; UI narrows with isKnownPropertyType.
export const IssuePropertySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  type: z.string(),
  description: z.string().optional().default(""),
  icon: z.string().optional().default(""),
  config: z.object({
    options: z.array(z.object({
      id: z.string(),
      name: z.string(),
      color: z.string().optional().default("#6b7280"),
    }).loose()).optional(),
  }).loose().default({}),
  position: z.number().optional().default(0),
  archived: z.boolean().optional().default(false),
  archived_at: z.string().nullable().optional(),
  usage_count: z.number().optional().default(0),
  // F30 type scope. Absent on an older backend, which is also its product
  // meaning: nothing is scoped, so every property is global.
  type_keys: z.array(z.string()).optional().default([]),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_PROPERTY: IssueProperty = {
  id: "",
  workspace_id: "",
  name: "",
  type: "text",
  description: "",
  icon: "",
  config: {},
  position: 0,
  archived: false,
  usage_count: 0,
  type_keys: [],
  created_at: "",
  updated_at: "",
};

// Quick actions (MUL-5465). `visibility` and `status` stay z.string() rather
// than z.enum: they are server-driven, and a newer server adding a value must
// degrade to the UI's default branch, not blank the whole list.
export const QuickActionSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  assignee_type: z.string(),
  assignee_id: z.string(),
  prompt: z.string().optional().default(""),
  visibility: z.string().optional().default("public"),
  status: z.string().optional().default("active"),
  last_used_at: z.string().nullable().optional().default(null),
  use_count: z.number().optional().default(0),
  created_by_id: z.string().optional().default(""),
  created_at: z.string(),
  updated_at: z.string(),
  target_name: z.string().optional(),
  // Both default to the pessimistic reading on an older server: "not known to
  // be public" and "not known to be missing" keep the settings row honest
  // rather than asserting a state the server never sent.
  target_public: z.boolean().optional().default(false),
  target_missing: z.boolean().optional().default(false),
}).loose();

export const EMPTY_QUICK_ACTION: QuickAction = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  assignee_type: "agent",
  assignee_id: "",
  prompt: "",
  visibility: "public",
  status: "active",
  last_used_at: null,
  use_count: 0,
  created_by_id: "",
  created_at: "",
  updated_at: "",
  target_public: false,
  target_missing: true,
};

export const ListQuickActionsResponseSchema = z.object({
  quick_actions: z.array(QuickActionSchema).default([]),
}).loose();

export const EMPTY_LIST_QUICK_ACTIONS_RESPONSE: ListQuickActionsResponse = {
  quick_actions: [],
};

export const QuickActionRenderSchema = z.object({
  content: z.string().default(""),
}).loose();

export const ListPropertiesResponseSchema = z.object({
  properties: z.array(IssuePropertySchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROPERTIES_RESPONSE: ListPropertiesResponse = {
  properties: [],
  total: 0,
};

// Value bag: keyed by definition UUID; values are primitives or string
// arrays (multi_select). The preprocess step drops entries with unknown
// shapes BEFORE validation — a newer server shipping an object-shaped value
// (future actor/relation types) must degrade to "that one property missing",
// never fail the whole IssueSchema and blank the list via parseWithFallback.
export const IssuePropertyValuesSchema = z.preprocess(
  (raw) => {
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return {};
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(raw)) {
      const ok =
        typeof value === "string" ||
        typeof value === "number" ||
        typeof value === "boolean" ||
        (Array.isArray(value) && value.every((item) => typeof item === "string"));
      if (ok) out[key] = value;
    }
    return out;
  },
  z.record(z.string(), z.union([z.string(), z.number(), z.boolean(), z.array(z.string())])).default({}),
);

export const IssuePropertiesResponseSchema = z.object({
  properties: IssuePropertyValuesSchema,
  issue_revision: z.number().int().positive().optional(),
}).loose();

export const EMPTY_ISSUE_PROPERTIES_RESPONSE: IssuePropertiesResponse = {
  properties: {},
};

export interface AppConfigResponse {
  cdn_domain: string;
  /** Speech-to-text provider configured (MULTICA_STT_*); absent on older servers. */
  meeting_transcription_available?: boolean;
  meeting_realtime_available?: boolean;
  tts_available?: boolean;
  // True when the CDN domain serves private content via time-bounded signed
  // URLs (CloudFront signing) — raw storage URLs on that domain are NOT
  // publicly fetchable and must not be used as native media sources
  // (MUL-3254). Older servers omit the field; treat that as false.
  cdn_signed?: boolean;
  allow_signup: boolean;
  google_client_id?: string;
  posthog_key?: string;
  posthog_host?: string;
  analytics_environment?: string;
  daemon_server_url?: string;
  daemon_app_url?: string;
  workspace_creation_disabled?: boolean;
  /** Whether this deployment offers the self-hosted Git provider integration
   * (self-host only; off on the managed cloud). Absent/false hides the whole
   * Settings → Integrations "Git providers" section. */
  vcs_integration_available?: boolean;
  feature_flags?: Record<string, boolean>;
  /** Whether this server understands local_directory `execution_mode` and
   * gates worktree mode at save time. Absent on every server that predates this
   * capability signal, which includes the ones that silently DROPPED an unknown
   * `execution_mode` and answered 201 — the resource then ran in place while
   * the user was promised isolation (#7113). Servers between that fix and this
   * signal do validate but cannot say so, and are treated as unable: the client
   * has no way to tell them apart, and only one of the two answers is safe. */
  local_worktree_supported?: boolean;
  /** Whether agent create/update persists `conversation_starters`. Older servers
   * silently ignored the unknown field, so absent must be treated as false. */
  agent_conversation_starters_supported?: boolean;
  /** Whether this deployment has a configured model for the browser-based
   * native runtime (OS plan, chantier 5). Absent/false on servers without
   * MULTICA_LLM_API_KEY set — the onboarding native-runtime card stays
   * disabled and no native RuntimeDevice row exists for new workspaces. */
  native_runtime_available?: boolean;
  server_version?: string;
  /** Run liveness threshold in seconds (F02): an active run whose
   * last_activity_at is older than this is shown as unresponsive. Omitted by
   * older servers; the client keeps its own default. */
  run_unresponsive_after_seconds?: number;
}

// ---------------------------------------------------------------------------
// Schemas for the highest-risk API endpoints — those whose responses drive
// the issue detail page (timeline, comments, subscribers) and the issues
// list. These are the surfaces that white-screened in #2143 / #2147 / #2192.
//
// These schemas are intentionally LENIENT:
//   - String enums are stored as `z.string()` rather than `z.enum([...])`.
//     A new server-side enum value should render as a generic fallback in
//     the UI, never crash a `safeParse`.
//   - Optional fields are unioned with `null` and given fallbacks where
//     existing UI code already coerces them.
//   - Arrays default to `[]` so a missing `reactions` / `attachments` /
//     `entries` field doesn't take the page down.
//   - Every object schema ends with `.loose()` so unknown server-side
//     fields pass through unchanged. zod 4's `.object()` defaults to STRIP,
//     which would silently delete fields the schema didn't explicitly list
//     — fine while the TS type doesn't claim them, but the moment a future
//     PR adds a TS field without updating the schema, the cast `as T` lies
//     and the field shows up as `undefined` at runtime. `.loose()` removes
//     that synchronisation hazard.
//
// These schemas are deliberately not typed as `z.ZodType<TimelineEntry>` /
// `z.ZodType<Issue>` etc. — the strict TS types narrow string fields to
// literal unions, which would defeat the leniency above. `parseWithFallback`
// returns the parsed value cast to the caller-supplied `T`, so the strict
// type still flows out at the call site; the schema only guards shape.
// ---------------------------------------------------------------------------

// Exported (JEF-321 batch A) so a standalone POST /comments/:id/reactions
// response can be validated the same way it is when embedded in a comment.
export const ReactionSchema = z.object({
  id: z.string(),
  comment_id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  emoji: z.string(),
  created_at: z.string(),
  comment_revision: z.number().int().positive().optional(),
});

export const EMPTY_REACTION: Reaction = {
  id: "",
  comment_id: "",
  actor_type: "",
  actor_id: "",
  emoji: "",
  created_at: "",
};

// Nested attachments embedded in timeline/comment responses stay lenient on
// purpose: a single malformed attachment must not knock the whole timeline
// into the fallback `[]`.
const AttachmentSchema = z.object({
  id: z.string(),
}).loose();

const ChatQuickActionSchema = z.object({
  label: z.string(),
  prompt: z.string(),
  primary: z.boolean().optional(),
}).loose();

export const ChatMessageSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
  message_kind: z
    .enum(["message", "no_response", "onboarding_kickoff", "onboarding_opening"])
    .catch("message")
    .optional(),
  // Optional additive data degrades independently: a malformed suggestion
  // must not hide the assistant reply that contains it.
  quick_actions: z.array(ChatQuickActionSchema).catch([]).optional().default([]),
  // Multiplayer attribution (K31): the human who sent a user message. Null on
  // assistant rows and on messages written before the column existed.
  author_user_id: z.string().nullable().optional(),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);
export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

export const ChatMessagesPageSchema = z.object({
  messages: z.array(ChatMessageSchema).default([]),
  limit: z.number().default(50),
  has_more: z.boolean().default(false),
  next_cursor: z.object({
    created_at: z.string(),
    id: z.string(),
  }).loose().nullable().optional(),
}).loose();

// Standalone attachment lookup (`GET /api/attachments/{id}`) is the source of
// truth for click-time download URLs. The two fields the download flow opens
// in a new tab — `download_url` and `url` — must be strings, otherwise we'd
// happily `window.open(undefined)`. `filename` gates the toast/title and is
// also enforced so a missing value falls back to the empty record below.
//
// `markdown_url` is parsed lenient: a server old enough to predate
// MUL-3192 omits the field, in which case the schema defaults it to "".
// Callers that need to persist a URL into markdown should go through the
// `useFileUpload` helper (which falls back to the legacy
// `attachmentDownloadPath` shape when `markdown_url` is empty), so the
// empty-string default does not silently break any persistence path.
export const AttachmentResponseSchema = z.object({
  id: z.string(),
  url: z.string(),
  download_url: z.string(),
  // Forced-attachment ("download button") URL — credential-free and, unlike
  // `download_url`, always Content-Disposition: attachment across every storage
  // mode. Optional: a server older than this field omits it, and callers fall
  // back to `download_url` / the stable endpoint. Never persisted (short-lived).
  attachment_download_url: z.string().optional(),
  markdown_url: z.string().optional().default(""),
  filename: z.string(),
  chat_session_id: z.string().nullable().optional(),
  chat_message_id: z.string().nullable().optional(),
}).loose();

export const EMPTY_ATTACHMENT: Attachment = {
  id: "",
  workspace_id: "",
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "",
  uploader_id: "",
  filename: "",
  url: "",
  download_url: "",
  markdown_url: "",
  content_type: "",
  size_bytes: 0,
  created_at: "",
};

// All object schemas use `.loose()` so unknown server-side fields pass
// through unchanged. zod 4's `.object()` defaults to STRIP, which would
// silently drop new fields and surface as a "field neither showed up in
// the UI" mystery the next time the TS type adopted them but the schema
// wasn't updated in lock-step. `.loose()` removes that synchronisation
// hazard — the schema validates the shape it knows about and leaves the
// rest alone.
/**
 * Diff anchor of a comment thread (F07 / JEF-21).
 *
 * `kind` is deliberately an open string: a newer server may add one. A thread
 * whose kind this build does not recognise renders WITHOUT its anchor rather
 * than disappearing — losing a discussion is far worse than losing a chip.
 *
 * `.catch()` on every field means a partially malformed anchor still yields a
 * usable object; the UI checks `file_path` before drawing anything.
 */
export const CommentAnchorSchema = z.object({
  kind: z.string().catch(""),
  pr_source: z.string().catch(""),
  pr_id: z.string().catch(""),
  head_sha: z.string().catch(""),
  file_path: z.string().catch(""),
  line_start: z.number().catch(0).default(0),
  line_end: z.number().catch(0).default(0),
  side: z.string().catch("new"),
  review_flag_id: z.string().nullish().catch(null),
}).loose();

const TimelineEntrySchema = z.object({
  type: z.string(),
  id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  created_at: z.string(),
  actor_name: z.string().optional(),
  actor_avatar_url: z.string().optional(),
  action: z.string().optional(),
  details: z.record(z.string(), z.unknown()).optional(),
  content: z.string().optional(),
  parent_id: z.string().nullable().optional(),
  updated_at: z.string().optional(),
  revision: z.number().int().positive().optional(),
  comment_type: z.string().optional(),
  // Agent-to-agent message intent (F19). Same free-string contract as
  // CommentSchema.a2a_intent.
  a2a_intent: z.string().nullish(),
  reactions: z.array(ReactionSchema).optional(),
  attachments: z.array(AttachmentSchema).optional(),
  source_task_id: z.string().nullable().optional(),
  coalesced_count: z.number().optional(),
  // Diff anchor of the thread (F07). Absent on activity rows, on unanchored
  // comments, and on a backend that predates the feature.
  anchor: CommentAnchorSchema.nullish().catch(null),
  anchor_stale: z.boolean().catch(false).default(false),
}).loose();

// /timeline returns a flat array of TimelineEntry, oldest first. The
// previously cursor-paginated wrapper was removed (#1929) — at observed data
// sizes (p99 ~30 entries per issue) paged delivery only created bugs.
export const TimelineEntriesSchema = z.array(TimelineEntrySchema);

export const EMPTY_TIMELINE_ENTRIES: TimelineEntry[] = [];

const OptionalStringSchema = z.preprocess(
  (value) => (typeof value === "string" ? value : undefined),
  z.string().optional(),
);

const BooleanWithDefaultSchema = (fallback: boolean) =>
  z.preprocess(
    (value) => (typeof value === "boolean" ? value : undefined),
    z.boolean().default(fallback),
  );

const FeatureFlagsSchema = z.preprocess(
  (value) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? value
      : undefined,
  z.record(z.string(), BooleanWithDefaultSchema(false)).default({}),
);

export const AppConfigSchema = z.object({
  cdn_domain: z.string().default(""),
  cdn_signed: BooleanWithDefaultSchema(false),
  allow_signup: BooleanWithDefaultSchema(true),
  google_client_id: OptionalStringSchema,
  posthog_key: OptionalStringSchema,
  posthog_host: OptionalStringSchema,
  analytics_environment: OptionalStringSchema,
  daemon_server_url: OptionalStringSchema,
  daemon_app_url: OptionalStringSchema,
  workspace_creation_disabled: BooleanWithDefaultSchema(false).optional(),
  vcs_integration_available: BooleanWithDefaultSchema(false).optional(),
  feature_flags: FeatureFlagsSchema,
  local_worktree_supported: BooleanWithDefaultSchema(false),
  agent_conversation_starters_supported: BooleanWithDefaultSchema(false),
  native_runtime_available: BooleanWithDefaultSchema(false),
  meeting_transcription_available: BooleanWithDefaultSchema(false).optional(),
  meeting_realtime_available: BooleanWithDefaultSchema(false).optional(),
  tts_available: BooleanWithDefaultSchema(false).optional(),
  server_version: OptionalStringSchema,
  run_unresponsive_after_seconds: z.number().positive().optional().catch(undefined),
}).loose();

export const EMPTY_APP_CONFIG: AppConfigResponse = {
  cdn_domain: "",
  cdn_signed: false,
  allow_signup: true,
  google_client_id: "",
  daemon_server_url: "",
  daemon_app_url: "",
  workspace_creation_disabled: false,
  vcs_integration_available: false,
  // Fail closed: an unreadable config must not look like a server that
  // validates execution_mode.
  local_worktree_supported: false,
  // Fail closed: old servers returned success while dropping the field.
  agent_conversation_starters_supported: false,
  // Fail closed: no declared model means the native runtime card must stay
  // disabled rather than default to "try it".
  native_runtime_available: false,
  feature_flags: {},
};

// Preference keys may grow over time, so keep both the key and value spaces
// forward-compatible while still rejecting non-string persisted data.
export const NotificationPreferenceResponseSchema = z.object({
  workspace_id: z.string(),
  preferences: z.record(z.string(), z.string()).default({}),
}).loose();

export const EMPTY_NOTIFICATION_PREFERENCE_RESPONSE: NotificationPreferenceResponse = {
  workspace_id: "",
  preferences: {},
};

export const CreateFeedbackResponseSchema = z.object({
  id: z.string(),
  created_at: z.string(),
}).loose();

export const EMPTY_CREATE_FEEDBACK_RESPONSE: CreateFeedbackResponse = {
  id: "",
  created_at: "",
};

export const CommentSchema = z.object({
  id: z.string(),
  issue_id: z.string(),
  author_type: z.string(),
  author_id: z.string(),
  content: z.string(),
  type: z.string(),
  parent_id: z.string().nullable(),
  reactions: z.array(ReactionSchema).default([]),
  attachments: z.array(AttachmentSchema).default([]),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number().int().positive().optional(),
  source_task_id: z.string().nullable().optional(),
  // Set only on comments a quick action produced (MUL-5465). Server-only.
  quick_action_id: z.string().nullable().optional(),
  // Agent-to-agent message intent (F19): question | review | handoff. Server-only
  // — POST /comments has no field for it, which is what makes the chip
  // unforgeable. A FREE STRING with a `default` branch downstream, not an enum:
  // the column has no CHECK, so a value this build cannot label must render as
  // an ordinary comment rather than fail the whole comment's parse.
  a2a_intent: z.string().nullish(),
  // Diff anchor (F07). `nullish` rather than required: a backend that predates
  // the feature omits it entirely, and the thread must still render.
  anchor: CommentAnchorSchema.nullish().catch(null),
  anchor_stale: z.boolean().catch(false).default(false),
}).loose();

export const CommentsListSchema = z.array(CommentSchema);

/**
 * One anchored discussion as the walkthrough reads it: the root, its replies,
 * and the anchor resolved once for the whole thread.
 */
export const AnchoredThreadSchema = z.object({
  root: CommentSchema,
  replies: z.array(CommentSchema).catch([]).default([]),
  anchor: CommentAnchorSchema.nullish().catch(null),
  anchor_stale: z.boolean().catch(false).default(false),
}).loose();

export const AnchoredThreadsSchema = z.object({
  threads: z.array(AnchoredThreadSchema).catch([]).default([]),
}).loose();

/** A response this build cannot read hides the threads, never the diff. */
export const EMPTY_ANCHORED_THREADS: AnchoredThreads = { threads: [] };

// Degraded placeholder for a comment response that failed schema validation.
// The empty id is the caller's signal that nothing usable came back — the run
// UI treats it as "could not read the result" rather than a successful run.
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

const CommentTriggerPreviewAgentSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  avatar_url: z.string().optional(),
  source: z.string().default(""),
  reason: z.string().default(""),
}).loose();

// Per-target outcome of an explicit @agent / @squad mention (MUL-4525 §2).
// target_id is required to correlate with the client's rendered mention; a
// malformed entry (missing id) is dropped rather than failing the whole payload.
export const CommentTriggerOutcomeSchema = z.object({
  target_type: z.string().default(""),
  target_id: z.string(),
  status: z.string().default(""),
  reason_code: z.string().default(""),
}).loose();

export const CommentTriggerPreviewSchema = z.object({
  agents: z.array(CommentTriggerPreviewAgentSchema).default([]),
  // Drop malformed blocked entries INDIVIDUALLY (MUL-4525): a single bad item
  // must not discard the whole set of valid blocked mentions. A non-array
  // degrades to []; each valid entry is kept, each malformed one dropped.
  blocked: z
    .array(z.unknown())
    .catch([])
    .default([])
    .transform((items) =>
      items.flatMap((item) => {
        const parsed = CommentTriggerOutcomeSchema.safeParse(item);
        return parsed.success ? [parsed.data] : [];
      }),
    ),
}).loose();

const IssueTriggerPreviewItemSchema = z.object({
  issue_id: z.string(),
  agent_id: z.string().default(""),
  source: z.string().default(""),
}).loose();

export const IssueTriggerPreviewSchema = z.object({
  triggers: z.array(IssueTriggerPreviewItemSchema).default([]),
  total_count: z.number().default(0),
}).loose();

// Metadata is primitive-only by API/DB contract. Stay lenient on shape:
// unknown keys land as `unknown` to a caller, but the field itself defaults
// to {} so consumers never need to nil-guard `issue.metadata`.
const IssueMetadataSchema = z.record(z.string(), z.union([z.string(), z.number(), z.boolean()])).default({});

const SourceContextAttachmentSchema = z.object({
  id: z.string(),
  source_attachment_id: z.string().optional(),
  owner_type: z.string(),
  owner_id: z.string(),
  filename: z.string(),
  content_type: z.string(),
  size_bytes: z.number(),
  created_at: z.string(),
}).loose();

// Early source-context servers encoded an empty Go slice as JSON null. Keep
// installed clients compatible while normalizing consumers onto the canonical
// array shape emitted by current servers.
const SourceContextAttachmentsSchema = z.array(SourceContextAttachmentSchema)
  .nullish()
  .transform((attachments) => attachments ?? []);

const SourceContextAuthorSchema = z.object({
  type: z.string(),
  id: z.string(),
  name: z.string(),
}).loose();

const SourceContextIssueSnapshotSchema = z.object({
  id: z.string(),
  identifier: z.string(),
  number: z.number(),
  title: z.string(),
  description: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number(),
  attachments: SourceContextAttachmentsSchema,
}).loose();

const SourceContextCommentSnapshotSchema = z.object({
  id: z.string(),
  parent_id: z.string().nullable(),
  type: z.string(),
  content: z.string(),
  author: SourceContextAuthorSchema,
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number(),
  attachments: SourceContextAttachmentsSchema,
}).loose();

export const SourceContextSnapshotSchema = z.object({
  version: z.number().optional(),
  captured_by_user_id: z.string().optional(),
  captured_at: z.string().optional(),
  source_issue: SourceContextIssueSnapshotSchema,
  comment_thread: z.array(SourceContextCommentSnapshotSchema),
  anchor_comment_id: z.string(),
}).loose();

export const SourceContextPreviewSchema = SourceContextSnapshotSchema.extend({
  capture_token: z.string().min(1),
  limits: z.object({
    comment_count: z.number(),
    text_bytes: z.number(),
    attachment_count: z.number(),
    attachment_bytes: z.number(),
  }).loose(),
}).loose();

const IssueSourceContextSchema = z.object({
  id: z.string(),
  version: z.number(),
  usage: z.string(),
  captured_at: z.string(),
  display_state: z.string(),
  source_issue_state: z.string(),
  comment_thread_state: z.string(),
  anchor_comment_state: z.string(),
  can_open_current_source: z.boolean(),
  change_reasons: z.array(z.string()).optional(),
  change_details: z.object({
    changed_comment_ids: z.array(z.string()),
    added_comments: z.array(SourceContextCommentSnapshotSchema).optional(),
    removed_comment_ids: z.array(z.string()).optional(),
    description_attachment_changes: z.array(z.object({
      kind: z.string(),
      attachment_id: z.string(),
      filename: z.string(),
      previous_filename: z.string().optional(),
    }).loose()),
  }).loose().optional(),
  current_source: z.object({
    issue_id: z.string(),
    identifier: z.string(),
    anchor_comment_id: z.string(),
  }).loose().optional(),
  source_author_state: z.array(z.object({
    type: z.string(),
    id: z.string(),
    captured_name: z.string(),
    current_name: z.string().optional(),
    state: z.string(),
  }).loose()).optional(),
  snapshot: SourceContextSnapshotSchema,
}).loose();

export const CommentSubIssueTaskResponseSchema = z.object({
  task_id: z.string().min(1),
}).loose();

export const IssueSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  status: z.string(),
  // The canonical status whose platform behavior `status` carries — equal to
  // `status` for the 7 built-ins, and the inherited category for a custom
  // status. Optional because only endpoints that resolve it emit it, so
  // consumers must fall back to `status` rather than treat "" as a category.
  // (MUL-6243)
  status_category: z.string().optional(),
  // A CUSTOM status's display name; "" for a built-in, which clients localize
  // from the key. Optional so a response from a server that predates the field
  // still validates.
  //
  // .catch(undefined) because this is ADDITIVE display data and the failure it
  // guards against is disproportionate: a parse failure anywhere in IssueSchema
  // takes the whole response through parseWithFallback to EMPTY_LIST_ISSUES_RESPONSE,
  // so one malformed name from a mixed-version deploy would blank an entire
  // issue list. Same treatment as source_context and labels above. Nothing reads
  // this field to make a decision — useStatusLabel resolves the label from the
  // catalog — so dropping it costs a fallback to the key. (MUL-6749)
  status_name: z.string().optional().catch(undefined),
  priority: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  // The delegate — the assignee's partner (F01). Optional + defaulted rather
  // than a bare .nullable() like the assignee pair: a server that predates the
  // field sends nothing, and IssueSchema parse failures take the WHOLE list
  // response to its fallback, so one older backend would blank every issue.
  // Absent therefore parses to null, which is also its product meaning: no
  // delegate.
  delegate_type: z.string().nullable().optional().default(null),
  delegate_id: z.string().nullable().optional().default(null),
  creator_type: z.string(),
  creator_id: z.string(),
  parent_issue_id: z.string().nullable(),
  project_id: z.string().nullable(),
  // Goals (K74) predate older backends; absent parses to null.
  goal_id: z.string().nullable().optional().default(null),
  // Work item type key (F30), or null for an UNTYPED issue. Optional +
  // defaulted rather than a bare .nullable(): a server that predates the field
  // sends nothing, and an IssueSchema parse failure takes the WHOLE list
  // response to its fallback, so one older backend would blank every issue.
  // Absent therefore parses to null, which is also its product meaning.
  issue_type: z.string().nullable().optional().default(null),
  // Detail-only, and absent on an older backend. Absent means "not resolved
  // here", so consumers must not read it as "no origin".
  origin_type: z.string().nullish(),
  origin_id: z.string().nullish(),
  // The recurrence series the issue belongs to (source or occurrence). Sent
  // on list rows too; absent on an older backend, which parses to null.
  recurrence_id: z.string().nullable().optional().default(null),
  position: z.number(),
  // Older backends predate `stage`; default to null so a missing field parses
  // cleanly into the non-optional Issue.stage (number | null).
  stage: z.number().nullable().default(null),
  start_date: z.string().nullable(),
  due_date: z.string().nullable(),
  metadata: IssueMetadataSchema,
  // Older backends predate custom properties; default {} so consumers never
  // nil-guard issue.properties.
  properties: IssuePropertyValuesSchema,
  reactions: z.array(z.unknown()).optional(),
  labels: z.array(z.unknown()).optional(),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number().int().positive().optional(),
  // Optional for compatibility with older self-hosted backends; a current
  // backend emits null until its historical backfill reaches the issue.
  last_activity_at: z.string().nullable().optional(),
  // Detail-only and potentially large. A malformed additive field must not
  // erase an otherwise usable issue returned by a mixed-version server.
  source_context: IssueSourceContextSchema.optional().catch(undefined),
}).loose();

// Plan verification (F17). Findings are LLM output: severity stays a string
// and an unknown value is kept as data; nothing here can make a plan look
// verified by default.
export const IssuePlanSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  version: z.number().int().default(0),
  content: z.string().default(""),
  steps: z.array(z.object({
    id: z.string().default(""),
    title: z.string().default(""),
    after: z.array(z.string()).optional().catch(undefined),
    assignee_type: z.string().optional(),
    assignee_id: z.string().optional(),
    issue_id: z.string().optional(),
  }).loose()).catch([]).default([]),
  author_type: z.string().default(""),
  author_id: z.string().default(""),
  superseded_at: z.string().nullable().default(null),
  materialized_at: z.string().nullable().optional().catch(null),
  created_at: z.string().default(""),
}).loose();

export const IssuePlanEnvelopeSchema = z.object({
  plan: IssuePlanSchema.nullable().catch(null).default(null),
  versions: z.array(IssuePlanSchema).catch([]).default([]),
}).loose();

export const EMPTY_ISSUE_PLAN: IssuePlanEnvelope = { plan: null, versions: [] };

// Plan Gate (K11): what an approval produced.
export const PlanMaterializationSchema = z.object({
  plan: IssuePlanSchema,
  issues: z.array(IssueSchema).catch([]).default([]),
}).loose();

export const PlanFindingSchema = z.object({
  severity: z.string().default(""),
  title: z.string().default(""),
  detail: z.string().optional(),
  files: z.array(z.string()).optional(),
  plan_step_id: z.string().optional(),
}).loose();

export const PlanVerificationSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  plan_id: z.string().default(""),
  plan_version: z.number().int().default(0),
  task_id: z.string().default(""),
  source_task_id: z.string().default(""),
  state: z.string().default("queued"),
  findings: z.array(PlanFindingSchema).catch([]).default([]),
  critical_count: z.number().int().default(0),
  major_count: z.number().int().default(0),
  minor_count: z.number().int().default(0),
  outdated_count: z.number().int().default(0),
  summary: z.string().nullable().default(null),
  reported_at: z.string().nullable().default(null),
  created_at: z.string().default(""),
}).loose();

export const PlanVerificationsResponseSchema = z.object({
  verifications: z.array(PlanVerificationSchema).catch([]).default([]),
}).loose();

// Issue scoping assistant (K14). Every field degrades on its own: a model that
// skipped the files still yields a usable draft.
export const IssueScopingProposalSchema = z.object({
  title: z.string().catch("").default(""),
  description: z.string().catch("").default(""),
  acceptance_criteria: z.array(z.string()).catch([]).default([]),
  probable_files: z.array(z.object({ path: z.string(), reason: z.string().optional() }).loose()).catch([]).default([]),
}).loose();

export const IssueScopingEnvelopeSchema = z.object({
  proposal: IssueScopingProposalSchema,
}).loose();

// Decision Cards (K01). A card is data from an agent: nothing here decides.
export const IssueDecisionSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  task_id: z.string().optional(),
  asked_by_type: z.string().default(""),
  asked_by_id: z.string().default(""),
  question: z.string().default(""),
  options: z.array(z.object({
    id: z.string(),
    label: z.string().default(""),
    impact: z.string().optional(),
  }).loose()).catch([]).default([]),
  recommended_option_id: z.string().optional(),
  urgency: z.string().default("normal"),
  response: z.object({
    option_id: z.string().optional(),
    modified_text: z.string().optional(),
  }).loose().nullable().catch(null).default(null),
  responded_by_type: z.string().optional(),
  responded_by_id: z.string().optional(),
  responded_at: z.string().nullable().default(null),
  resume_task_id: z.string().optional(),
  plan_version: z.number().int().optional().catch(undefined),
  interview_group_id: z.string().optional().catch(undefined),
  interview_position: z.number().int().optional().catch(undefined),
  sla_deadline_at: z.string().nullable().optional().catch(null),
  escalation_level: z.number().int().optional().catch(0),
  escalated_at: z.string().nullable().optional().catch(null),
  created_at: z.string().default(""),
  learned: z.object({
    signature: z.string().catch("").default(""),
    option_id: z.string().catch("").default(""),
    option_label: z.string().catch("").default(""),
    count: z.number().catch(0).default(0),
    total: z.number().catch(0).default(0),
    rate: z.number().catch(0).default(0),
    auto: z.boolean().catch(false).default(false),
    stake: z.string().catch("normal").default("normal"),
  }).loose().nullable().optional().catch(null),
}).loose();

export const IssueDecisionsResponseSchema = z.object({
  decisions: z.array(IssueDecisionSchema).catch([]).default([]),
}).loose();

export const IssueDecisionEnvelopeSchema = z.object({
  decision: IssueDecisionSchema,
}).loose();

// Outcome Contract (K12). An unknown proof_type or proof_state is kept as text
// and rendered by the UI's default branch; a malformed criterion drops the list.
export const AcceptanceCriterionSchema = z.object({
  id: z.string(),
  text: z.string().default(""),
  proof_type: z.string().optional(),
  proof_ref: z.string().optional(),
  proof_state: z.string().default("missing"),
  validated_by: z.string().optional(),
  proved_at: z.string().optional(),
}).loose();

export const AcceptanceCriteriaResponseSchema = z.object({
  criteria: z.array(AcceptanceCriterionSchema).catch([]).default([]),
}).loose();

export const ListIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

// GET /api/issues/:id/dependencies. `type` is relative to the requested
// issue ("blocks" = it blocks `issue`). Kept as a plain string so a relation
// type added server-side lands in `related` instead of failing the parse.
export const IssueDependencySchema = z.object({
  id: z.string(),
  type: z.string(),
  issue: IssueSchema,
}).loose();

export const IssueDependenciesResponseSchema = z.object({
  blocks: z.array(IssueDependencySchema).default([]),
  blocked_by: z.array(IssueDependencySchema).default([]),
  related: z.array(IssueDependencySchema).default([]),
  duplicate: z.array(IssueDependencySchema).default([]),
}).loose();

// Triage queue (M2). Payload is the stored capture JSONB — an object whose
// exact keys (`size` / `body` / `truncated`) are additive display data, so it
// is parsed as a loose record rather than a fixed shape.
export const TriageItemSchema = z.object({
  id: z.string(),
  source_id: z.string().default(""),
  source_name: z.string().default(""),
  source_kind: z.string().default(""),
  origin_type: z.string().default(""),
  origin_id: z.string().optional(),
  title: z.string().default(""),
  body_markdown: z.string().default(""),
  payload: z.record(z.string(), z.unknown()).default({}),
  state: z.string().default("pending"),
  collapse_count: z.number().default(1),
  drop_reason: z.string().optional(),
  resolution_reason: z.string().optional(),
  /** "member" when a human decided, "system" for an automatic resolution. */
  resolved_by_type: z.string().optional(),
  issue_id: z.string().optional(),
  duplicate_of_issue_id: z.string().optional(),
  /** Set while the item is parked by a snooze; cleared once it comes due. */
  snoozed_until: z.string().nullable().optional(),
  /** An agent's suggestion. Advisory: the item is still pending. */
  verdict: z.string().optional(),
  verdict_reason: z.string().optional(),
  verdict_agent_id: z.string().optional(),
  verdict_at: z.string().nullable().optional(),
  first_seen_at: z.string(),
  resolved_at: z.string().nullable().optional(),
  revision: z.number().default(0),
}).loose();

export const TriageItemsResponseSchema = z.object({
  items: z.array(TriageItemSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

// Also the shape PATCH /api/triage/sources/{id} answers with: the policy is a
// superset of what the stats carry, and the counters a patch response omits
// default to zero rather than making a second endpoint necessary.
export const TriageSourceSchema = z.object({
  id: z.string(),
  kind: z.string().default(""),
  ref_id: z.string().default(""),
  name: z.string().default(""),
  mode: z.string().default("direct"),
  auto_accept: z.boolean().default(false),
  cap_per_hour: z.number().default(0),
  expiry_days: z.number().default(0),
  pending: z.number().default(0),
  items_24h: z.number().default(0),
  dropped_24h: z.number().default(0),
}).loose();

export const EMPTY_TRIAGE_SOURCE: TriageSource = Object.freeze({
  id: "",
  kind: "",
  ref_id: "",
  name: "",
  mode: "direct",
  auto_accept: false,
  cap_per_hour: 0,
  expiry_days: 0,
  pending: 0,
  items_24h: 0,
  dropped_24h: 0,
}) as TriageSource;

export const TriageStatsSchema = z.object({
  pending: z.number().default(0),
  snoozed: z.number().default(0),
  shadow_pending: z.number().default(0),
  dropped_24h: z.number().default(0),
  oldest_pending_age_seconds: z.number().default(0),
  sources: z.array(TriageSourceSchema).default([]),
}).loose();

export const EMPTY_TRIAGE_STATS: TriageStats = Object.freeze({
  pending: 0,
  snoozed: 0,
  shadow_pending: 0,
  dropped_24h: 0,
  oldest_pending_age_seconds: 0,
  sources: [],
}) as TriageStats;

export const EMPTY_TRIAGE_ITEMS_RESPONSE: TriageItemsResponse = Object.freeze({
  items: [],
}) as TriageItemsResponse;

export const TriageBatchAcceptResultSchema = z.object({
  id: z.string(),
  outcome: z.string().default("error"),
  issue_id: z.string().optional(),
  duplicate_of_issue_id: z.string().optional(),
  duplicate_issue_identifier: z.string().optional(),
}).loose();

export const TriageBatchAcceptResponseSchema = z.object({
  items: z.array(TriageBatchAcceptResultSchema).default([]),
  stopped: z.string().optional(),
}).loose();

export const AcceptTriageItemResponseSchema = z.object({
  item_id: z.string(),
  state: z.string().default("accepted"),
  issue: IssueSchema.optional(),
}).loose();

export const DismissTriageItemResponseSchema = z.object({
  item_id: z.string(),
  state: z.string().default("dismissed"),
}).loose();

export const MergeTriageItemResponseSchema = z.object({
  item_id: z.string(),
  state: z.string().default("merged"),
  duplicate_of_issue_id: z.string().default(""),
  duplicate_issue_identifier: z.string().default(""),
}).loose();

export const SnoozeTriageItemResponseSchema = z.object({
  item_id: z.string(),
  state: z.string().default("pending"),
  snoozed_until: z.string().nullable().optional(),
}).loose();

export const TriageBatchDismissResultSchema = z.object({
  id: z.string(),
  outcome: z.string().default("error"),
}).loose();

export const TriageBatchDismissResponseSchema = z.object({
  items: z.array(TriageBatchDismissResultSchema).default([]),
}).loose();

// Email intake. The token comes back exactly once, when the endpoint is
// created or rotated; a response missing it is unusable, so it has no default.
export const TriageEmailSourceSchema = z.object({
  id: z.string(),
  mode: z.string().default("gate"),
  path: z.string().default(""),
  url: z.string().optional(),
  token: z.string().default(""),
}).loose();

export const EMPTY_TRIAGE_EMAIL_SOURCE: TriageEmailSource = Object.freeze({
  id: "",
  mode: "gate",
  path: "",
  token: "",
}) as TriageEmailSource;

// Postmortem autogen (k68). A drafted postmortem for a failed run, reviewed
// by a human (draft -> approved/discarded).
export const PostmortemSchema = z.object({
  id: z.string(),
  source_task_id: z.string().default(""),
  issue_id: z.string().optional(),
  agent_id: z.string().optional(),
  trigger: z.string().default("failed"),
  state: z.string().default("draft"),
  failure_reason: z.string().default(""),
  summary: z.string().default(""),
  root_cause: z.string().default(""),
  impact: z.string().default(""),
  preventive_rules: z.array(z.string()).default([]),
  cost_usd_ticks: z.number().optional(),
  llm_generated: z.boolean().default(false),
  resolved_at: z.string().nullable().optional(),
  revision: z.number().default(0),
  applied_rules: z.number().int().optional(),
  created_at: z.string(),
}).loose();

export const PostmortemsResponseSchema = z.object({
  items: z.array(PostmortemSchema).default([]),
  next_cursor: z.string().optional(),
}).loose();

export const PostmortemStatsSchema = z.object({
  draft: z.number().default(0),
  approved: z.number().default(0),
  discarded: z.number().default(0),
}).loose();

export const EMPTY_POSTMORTEM_STATS: PostmortemStats = Object.freeze({
  draft: 0,
  approved: 0,
  discarded: 0,
}) as PostmortemStats;

// Workspace Brain. One shared knowledge note; `revision` is the
// optimistic-concurrency token the PATCH must send back.
export const WorkspaceNoteSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  title: z.string().default(""),
  content: z.string().default(""),
  tags: z.array(z.string()).default([]),
  source: z.string().default("manual"),
  source_task_id: z.string().nullable().optional(),
  source_agent_id: z.string().nullable().optional(),
  pinned: z.boolean().default(false),
  archived_at: z.string().nullable().optional(),
  merged_into: z.string().nullable().optional(),
  created_by_type: z.string().default("member"),
  created_by_id: z.string().nullable().optional(),
  revision: z.number().default(0),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const WorkspaceNotesResponseSchema = z.object({
  items: z.array(WorkspaceNoteSchema).default([]),
  tags: z.array(z.string()).default([]),
}).loose();

export const EMPTY_WORKSPACE_NOTES_RESPONSE: WorkspaceNotesResponse = Object.freeze({
  items: [],
  tags: [],
}) as WorkspaceNotesResponse;

export const EMPTY_WORKSPACE_NOTE: WorkspaceNote = Object.freeze({
  id: "",
  workspace_id: "",
  title: "",
  content: "",
  tags: [],
  source: "manual",
  pinned: false,
  created_by_type: "member",
  revision: 0,
  created_at: "",
  updated_at: "",
}) as WorkspaceNote;

// Brain capture inbox (OS plan, vague B). Every enum stays a plain string so
// a kind / origin / action added server-side degrades to an unknown label
// instead of dropping the capture from the inbox.
export const BrainCaptureMergeTargetSchema = z.object({
  id: z.string().default(""),
  title: z.string().default(""),
}).loose();

export const BrainCaptureSuggestionSchema = z.object({
  title: z.string().default(""),
  tags: z.array(z.string()).default([]),
  summary: z.string().default(""),
  action: z.string().default("note"),
  merge_note: BrainCaptureMergeTargetSchema.nullable().optional(),
  candidates: z.array(BrainCaptureMergeTargetSchema).default([]),
  reason: z.string().default(""),
  model: z.string().optional(),
}).loose();

export const BrainCaptureSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  kind: z.string().default("text"),
  content: z.string().default(""),
  url: z.string().default(""),
  title_hint: z.string().default(""),
  // A malformed attachment must not cost the whole capture: degrade the field
  // to absent and the card falls back to its text.
  attachment: AttachmentResponseSchema.nullable().optional().catch(null),
  origin: z.string().default("web"),
  status: z.string().default("raw"),
  transcription_status: z.string().default("none"),
  // Same reasoning: a suggestion the model shaped wrong hides the suggestion
  // block, it does not hide the capture.
  suggestion: BrainCaptureSuggestionSchema.nullable().optional().catch(null),
  note_id: z.string().nullable().optional(),
  created_by_type: z.string().default("member"),
  created_by_id: z.string().nullable().optional(),
  source_task_id: z.string().nullable().optional(),
  organized_by: z.string().nullable().optional(),
  organized_at: z.string().nullable().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const BrainCapturesResponseSchema = z.object({
  captures: z.array(BrainCaptureSchema).default([]),
  raw_count: z.number().default(0),
}).loose();

export const BrainCaptureResponseSchema = z.object({
  capture: BrainCaptureSchema,
}).loose();

export const OrganizeBrainCaptureResponseSchema = z.object({
  capture: BrainCaptureSchema,
  note: WorkspaceNoteSchema.nullable().default(null),
}).loose();

// Ranked note search. `snippet` is the note's own text with <mark> inserted by
// ts_headline — raw, unescaped. It is rendered through `renderSnippet`
// (packages/core/brain/snippet.ts), never as HTML.
export const WorkspaceNoteSearchHitSchema = WorkspaceNoteSchema.extend({
  score: z.number().default(0),
  snippet: z.string().default(""),
  lex_rank: z.number().nullable().optional(),
  vec_rank: z.number().nullable().optional(),
}).loose();

export const WorkspaceNoteSearchResponseSchema = z.object({
  notes: z.array(WorkspaceNoteSearchHitSchema).default([]),
  vector: z.boolean().default(false),
}).loose();

export const EMPTY_BRAIN_CAPTURE: BrainCapture = Object.freeze({
  id: "",
  workspace_id: "",
  kind: "text",
  content: "",
  url: "",
  title_hint: "",
  attachment: null,
  origin: "web",
  status: "raw",
  transcription_status: "none",
  suggestion: null,
  note_id: null,
  created_by_type: "member",
  created_at: "",
  updated_at: "",
}) as BrainCapture;

export const EMPTY_BRAIN_CAPTURES_RESPONSE: BrainCapturesResponse = Object.freeze({
  captures: [],
  raw_count: 0,
}) as BrainCapturesResponse;

export const EMPTY_ORGANIZE_BRAIN_CAPTURE_RESPONSE: OrganizeBrainCaptureResponse =
  Object.freeze({
    capture: EMPTY_BRAIN_CAPTURE,
    note: null,
  }) as OrganizeBrainCaptureResponse;

export const EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE: WorkspaceNoteSearchResponse =
  Object.freeze({
    notes: [],
    vector: false,
  }) as WorkspaceNoteSearchResponse;

export const EMPTY_POSTMORTEMS_RESPONSE: PostmortemsResponse = Object.freeze({
  items: [],
}) as PostmortemsResponse;

// Response schema for POST /api/issues. Two tightenings over IssueSchema:
//
//   - `id` must be non-empty. A created issue always carries a real id, so an
//     empty/absent id means the create effectively failed. createIssue turns a
//     schema failure into a rejection (not a fabricated success), so tightening
//     id here routes an id-less body to that same failure path.
//   - `labels` is the backend-compatibility signal the create modal reads to
//     decide whether the backend attached labels in the create transaction
//     (present) or predates that (absent → fall back to per-label attach).
//     Validate it strictly as Label[] and degrade a malformed value to
//     `undefined` — the same as an absent field — so a wrong shape (null,
//     object, a garbage array) can never masquerade as "handled" and suppress
//     the fallback. Unlike the loose IssueSchema.labels (z.array(z.unknown())),
//     the elements are fully validated. See packages/views/modals/create-issue.tsx.
export const CreateIssueResponseSchema = IssueSchema.extend({
  id: z.string().min(1),
  labels: z.array(LabelSchema).optional().catch(undefined),
}).loose();

export const EMPTY_LIST_ISSUES_RESPONSE: ListIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchIssueResultSchema = IssueSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
  matched_description_snippet: z.string().optional(),
  matched_comment_snippet: z.string().optional(),
}).loose();

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
};

// Exported (was module-private, used only by SearchProjectResultSchema below)
// so JEF-321 batch C can reuse it for the plain project CRUD endpoints
// instead of a near-duplicate schema.
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
  // .default(null) so a project from an older backend (frontend deploys before
  // backend) that omits these keys parses to null instead of failing the whole
  // object — which would degrade a search/list batch to the empty fallback.
  start_date: z.string().nullable().default(null),
  due_date: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  issue_count: z.number().default(0),
  done_count: z.number().default(0),
  resource_count: z.number().default(0),
  goal_ids: z.array(z.string()).catch([]).default([]),
}).loose();

const SearchProjectResultSchema = ProjectSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
}).loose();

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
};

const IssueAssigneeGroupSchema = z.object({
  id: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

export const GroupedIssuesResponseSchema = z.object({
  groups: z.array(IssueAssigneeGroupSchema).default([]),
}).loose();

export const EMPTY_GROUPED_ISSUES_RESPONSE: GroupedIssuesResponse = {
  groups: [],
};

const IssueTableActorRefSchema = z.object({
  // Server-driven enums stay open so installed desktop clients survive a
  // backend that introduces another actor kind.
  type: z.string(),
  id: z.string(),
}).loose();

const IssueTableParentRefSchema = z.object({
  id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  status: z.string(),
}).loose();

// The group kind is server-driven and the catalogue is open (the facet list
// below already knows dimensions this union does not). A discriminated union
// with no fallback would fail one element, then the array, then the whole
// response through parseWithFallback — an empty grouped board on a client one
// release behind. The unknown branch keeps the row addressable instead.
const IssueTableGroupValueSchema = z.union([
  z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("status"),
    status: z.string(),
  }).loose(),
  z.object({
    kind: z.literal("assignee"),
    actor: IssueTableActorRefSchema.nullable(),
  }).loose(),
  z.object({
    kind: z.literal("project"),
    project_id: z.string().nullable().optional().default(null),
  }).loose(),
  z.object({
    kind: z.literal("parent"),
    parent_id: z.string().nullable().optional().default(null),
    parent: IssueTableParentRefSchema.nullable().optional().default(null),
    value_state: z.enum(["value", "unavailable", "unset"]),
  }).loose(),
  z.object({
    kind: z.literal("property"),
    property_id: z.string(),
    value: z.union([z.string(), z.boolean(), z.null()]).optional(),
    value_state: z.enum(["value", "unavailable", "unset"]).catch("value"),
  }).loose(),
  ]),
  // A kind this build does not know yet. Normalised to a literal so the union
  // stays discriminable on the client; the group keeps its key and count, so
  // the lane renders with a neutral label instead of the board going empty.
  z.object({ kind: z.string() }).loose().transform(() => ({ kind: "unknown" as const })),
]);

const IssueTableGroupDescriptorSchema: z.ZodType<IssueTableGroupDescriptor> = z.lazy(() => z.object({
  key: z.string(),
  value: IssueTableGroupValueSchema,
  count: z.number(),
  secondary_groups: z.array(IssueTableGroupDescriptorSchema).optional(),
}).loose());

export const IssueTableGroupsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  groups: z.array(IssueTableGroupDescriptorSchema).default([]),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_GROUPS_RESPONSE: IssueTableGroupsResponse = {
  query_fingerprint: "",
  total: 0,
  groups: [],
  next_cursor: null,
};

const IssueTableRowSchema = z.object({
  issue: IssueSchema,
  direct_child_count: z.number().default(0),
}).loose();

export const IssueTableRowsResponseSchema = z.object({
  query_fingerprint: z.string(),
  group_key: z.string().nullable().default(null),
  parent_id: z.string().nullable().default(null),
  total: z.number(),
  rows: z.array(IssueTableRowSchema).default([]),
  branch_total: z.number(),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_ROWS_RESPONSE: IssueTableRowsResponse = {
  query_fingerprint: "",
  group_key: null,
  parent_id: null,
  total: 0,
  rows: [],
  branch_total: 0,
  next_cursor: null,
};

/**
 * A per-issue refusal from a batch update. The endpoint answers 200 with the
 * applied count and this list, so a caller that reads only `updated` reports a
 * partial refusal as a clean success. `code` and `reason` are server-driven and
 * kept lenient on purpose: a refusal reason this build does not know must still
 * name the issue it refused.
 */
export const BatchUpdateRefusalSchema = z.object({
  issue_id: z.string(),
  code: z.string().default(""),
  reason: z.string().default(""),
  from: z.string().optional(),
  to: z.string().optional(),
  rule_id: z.string().optional(),
  requires_approval: z.boolean().default(false),
}).loose();

export const BatchUpdateIssuesResponseSchema = z.object({
  updated: z.number().default(0),
  refused: z.array(BatchUpdateRefusalSchema).default([]),
}).loose();

export type BatchUpdateRefusal = z.infer<typeof BatchUpdateRefusalSchema>;
export type BatchUpdateIssuesResponse = z.infer<typeof BatchUpdateIssuesResponseSchema>;

const IssueTableFacetValueSchema = z.object({
  key: z.string(),
  count: z.number(),
}).loose();

const IssueTableFacetSchema = z.object({
  kind: z.enum(["status", "priority", "assignee", "creator", "project", "label", "property", "working_agents"]).catch("status"),
  property_id: z.string().optional(),
  values: z.array(IssueTableFacetValueSchema).default([]),
}).loose();

export const IssueTableFacetsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  facets: z.array(IssueTableFacetSchema).default([]),
}).loose();

export const EMPTY_ISSUE_TABLE_FACETS_RESPONSE: IssueTableFacetsResponse = {
  query_fingerprint: "",
  total: 0,
  facets: [],
};

const SubscriberSchema = z.object({
  issue_id: z.string(),
  user_type: z.string(),
  user_id: z.string(),
  reason: z.string(),
  created_at: z.string(),
}).loose();

export const SubscribersListSchema = z.array(SubscriberSchema);

export const ChildIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
}).loose();

export const ChildIssueProgressResponseSchema = z.object({
  progress: z
    .array(
      z
        .object({
          parent_issue_id: z.string(),
          total: z.number(),
          done: z.number(),
        })
        .loose(),
    )
    .default([]),
}).loose();

export const CloudRuntimeNodeSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  instance_id: z.string(),
  region: z.string(),
  instance_type: z.string(),
  image_id: z.string(),
  subnet_id: z.string(),
  name: z.string(),
  status: z.string(),
  tags: z.record(z.string(), z.string()).default({}),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const CloudRuntimeNodeListSchema = z.array(CloudRuntimeNodeSchema);

// Power-action / status responses from the fleet service. That service is not
// part of this repo, so the schema is deliberately permissive: both fields are
// defaulted and unknown keys pass through. Only `status` is consumed, and an
// empty one means "nothing new to show" rather than a broken page.
export const CloudRuntimeNodeActionSchema = z.object({
  instance_id: z.string().default(""),
  status: z.string().default(""),
}).loose();

export const EMPTY_CLOUD_RUNTIME_NODE_ACTION: CloudRuntimeNodeActionResult = {
  instance_id: "",
  status: "",
};

export const EMPTY_CLOUD_RUNTIME_NODE_LIST: CloudRuntimeNode[] = [];

export const EMPTY_CLOUD_RUNTIME_NODE: CloudRuntimeNode = {
  id: "",
  owner_id: "",
  instance_id: "",
  region: "",
  instance_type: "",
  image_id: "",
  subnet_id: "",
  name: "",
  status: "",
  tags: {},
  metadata: {},
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// Workspace dashboard schemas
//
// The dashboard hits three independent rollup endpoints. Each returns a flat
// array, and every field is consumed by chart / KPI math — a missing number
// silently degrades to NaN downstream, so we coerce missing numbers to 0.
// String fields default to "" (no enum narrowing) to survive future model /
// agent ID drift, and so a single null from tz-aware SQL bucketing fails
// only that row instead of dropping the whole array to the `[]` fallback.
// ---------------------------------------------------------------------------

// Cost split carried by every usage row. `cost_usd_ticks` is what the provider
// itself charged for the rows behind this aggregate (1e-10 USD); the
// `uncosted_*` counts are the tokens from rows the provider did NOT price, and
// so are the only ones the client should run through its rate table.
//
// The `uncosted_*` fields are deliberately `.optional()` rather than
// `.default(0)`: a backend that predates them sends nothing, and defaulting
// those rows to "0 tokens left to estimate" would silently zero their cost.
// `undefined` means "this backend doesn't split", and the consumer falls back
// to the full token counts — i.e. exactly the old behaviour. A real 0 from a
// current backend means "everything here is already priced", which is a
// different thing and must stay distinguishable.
const CostSplitShape = {
  cost_usd_ticks: z.number().optional(),
  uncosted_input_tokens: z.number().optional(),
  uncosted_output_tokens: z.number().optional(),
  uncosted_cache_read_tokens: z.number().optional(),
  uncosted_cache_write_tokens: z.number().optional(),
};

const DashboardUsageDailySchema = z.object({
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageDailyListSchema = z.array(DashboardUsageDailySchema);

const DashboardUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageByAgentListSchema = z.array(DashboardUsageByAgentSchema);

// `cancelled_count` defaults to 0 so an installed client pointed at a
// backend that predates it still renders: those rows simply carry no
// cancelled segment, which is exactly what that backend measured.
const DashboardAgentRunTimeSchema = z.object({
  agent_id: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardAgentRunTimeListSchema = z.array(DashboardAgentRunTimeSchema);

const DashboardRunTimeDailySchema = z.object({
  date: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardRunTimeDailyListSchema = z.array(DashboardRunTimeDailySchema);

// Failure rollups. `failure_reason` is an open string on purpose — it carries
// the backend's canonical taxonomy, which grows as new classifier rules land
// (server/pkg/taskfailure). Pinning it to a z.enum would make an installed
// desktop client drop rows for a reason its build predates; the client folds
// unrecognised reasons into an "other" display class instead. The empty
// string is the succeeded bucket, so `.default("")` is a meaningful default
// only for a row that already lost its reason — such a row lands in the
// denominator rather than inventing a failure that never happened.
const DashboardFailureDailySchema = z.object({
  date: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureDailyListSchema = z.array(DashboardFailureDailySchema);

const DashboardFailureByAgentSchema = z.object({
  agent_id: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureByAgentListSchema = z.array(
  DashboardFailureByAgentSchema,
);

// ---------------------------------------------------------------------------
// Runtime usage schemas — the runtime-detail page's four usage endpoints
// (`/api/runtimes/:id/usage*`). Same leniency rules as the dashboard
// schemas above: numbers default to 0, strings to "", `.loose()` passes
// unknown fields.
// ---------------------------------------------------------------------------

const RuntimeUsageSchema = z.object({
  runtime_id: z.string().default(""),
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
}).loose();

export const RuntimeUsageListSchema = z.array(RuntimeUsageSchema);

const RuntimeHourlyActivitySchema = z.object({
  hour: z.number().default(0),
  count: z.number().default(0),
}).loose();

export const RuntimeHourlyActivityListSchema = z.array(RuntimeHourlyActivitySchema);

const RuntimeUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByAgentListSchema = z.array(RuntimeUsageByAgentSchema);

const RuntimeUsageByHourSchema = z.object({
  hour: z.number().default(0),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByHourListSchema = z.array(RuntimeUsageByHourSchema);

// ---------------------------------------------------------------------------
// Agent task responses. The base object stays loose so daemon/runtime fields
// can drift while task-list consumers still validate the fields they render.
// ---------------------------------------------------------------------------

// Human attribution (MUL-4302 §9): who an agent run is accountable to, and how
// that human was resolved. Every field is defensive so a departed member, an
// autopilot run (no originator), or an older backend degrades to a partial
// object instead of a parse failure.
const AttributionUserSchema = z.object({
  id: z.string().default(""),
  name: z.string().optional(),
  email: z.string().optional(),
  avatar_url: z.string().optional(),
}).loose();

const TaskEvidenceSchema = z.object({
  kind: z.string().default(""),
  ref_id: z.string().default(""),
}).loose();

const TaskAttributionSchema = z.object({
  source: z.string().default("unattributed"),
  precise: z.boolean().default(false),
  initiator: AttributionUserSchema.optional(),
  originator: AttributionUserSchema.optional(),
  evidence: TaskEvidenceSchema.optional(),
  rule_version_id: z.string().optional(),
  delegated_from_task_id: z.string().optional(),
  retry_of_task_id: z.string().optional(),
  rerun_of_task_id: z.string().optional(),
}).loose();

const OptionalStringArraySchema = z.preprocess(
  (value) =>
    Array.isArray(value) && value.every((item) => typeof item === "string")
      ? value
      : undefined,
  z.array(z.string()).optional(),
);

// One (provider, model) slice of a run's token usage. Token counts default to
// 0 rather than failing the row: a slice missing one counter is still worth
// pricing on the counters it does have, and the "we have no usage at all" case
// is carried by the field's absence, not by a zeroed entry.
const TaskUsageSchema = z.object({
  provider: z.string().optional(),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  cost_usd_ticks: z.number().optional(),
}).loose();

// ---------------------------------------------------------------------------
// Smart routing (JEF-237). The router's decision record rides on the task
// payload; per-candidate stats default rather than fail so one thin row in
// the scored shortlist still lets the decision render.
// ---------------------------------------------------------------------------

const RoutingCandidateSchema = z.object({
  runtime_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  samples: z.number().default(0),
  success_rate: z.number().default(0),
  wilson_lower: z.number().optional(),
  avg_cost_usd: z.number().nullable().optional(),
  avg_duration_secs: z.number().nullable().optional(),
  score: z.number().optional(),
  excluded_reason: z.string().optional(),
}).loose();

// `mode` stays an open string (only "auto" today) so an installed client
// keeps the decision when the backend grows new modes.
const RuntimeRoutingDecisionSchema = z.object({
  mode: z.string().default("auto"),
  chosen_runtime_id: z.string().default(""),
  chosen_model: z.string().optional(),
  reason: z.string().default(""),
  candidates: z.array(RoutingCandidateSchema).optional(),
}).loose();

// ---------------------------------------------------------------------------
// Run confidence scoring (JEF-240). The scorer's record rides on the task
// payload; `score` is the one field that defines the record, so a row without
// it degrades the whole record to "absent" rather than inventing a number.
// ---------------------------------------------------------------------------

const TaskConfidenceSchema = z.object({
  score: z.number(),
  rationale: z.string().default(""),
  model: z.string().optional(),
  threshold: z.number().optional(),
  below_threshold: z.boolean().optional(),
  producer_model: z.string().optional(),
  // Left as a plain string on purpose: a newer backend may name a relation
  // this build has never heard of, and coercing it to "independent" would be
  // the one wrong answer. Render an unrecognised value as unknown.
  judge_independence: z.string().optional(),
}).loose();

// ---------------------------------------------------------------------------
// Run escalation (JEF-272). When a below-threshold run is re-dispatched to a
// stronger runtime, the record rides on the NEW task the escalation created.
// `from_task_id` is the one field that defines the record — without it there
// is no origin to point at — so a row missing it degrades the whole record
// to "absent", same rule as the confidence record above.
// ---------------------------------------------------------------------------

const TaskEscalationSchema = z.object({
  from_task_id: z.string(),
  reason: z.string().default(""),
  attempt: z.number().int().default(1),
  from_runtime_id: z.string().default(""),
}).loose();

export const RuntimeRoutingStatsSchema = z.object({
  runtime_id: z.string().default(""),
  runtime_name: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  task_class: z.string().default(""),
  samples: z.number().default(0),
  benchmark_samples: z.number().default(0),
  success_rate: z.number().default(0),
  avg_cost_usd: z.number().nullable().default(null),
  avg_duration_secs: z.number().nullable().default(null),
}).loose();

export const RuntimeRoutingStatsResponseSchema = z.object({
  window_days: z.number().default(90),
  rows: z.array(RuntimeRoutingStatsSchema).default([]),
}).loose();

export const EMPTY_ROUTING_STATS_RESPONSE: RuntimeRoutingStatsResponse = {
  window_days: 90,
  rows: [],
};

// ---------------------------------------------------------------------------
// Workflow selector (JEF-273). The backend picks an execution strategy per
// task (single / cascade / critique), learned from the 90-day run history.
// ---------------------------------------------------------------------------

export const TaskWorkflowSchema = z.enum(["single", "cascade", "critique"]);

// Workflow policy (GET/PUT /api/workflow-policy-settings): "auto" learns the
// workflow from history, "off" always runs single. A malformed payload falls
// back to the safe default rather than breaking the settings screen.
export const WorkflowPolicySettingsSchema = z.object({
  mode: z.enum(["off", "auto"]).catch("off").default("off"),
}).loose();

// One (task_class, workflow) row of the workflow-stats rollup. `workflow`
// stays an open string so an installed client survives a newer backend's
// strategies; `avg_*` are null when the rollup has no priced / timed samples.
export const WorkflowStatsSchema = z.object({
  task_class: z.string().default(""),
  workflow: z.string().default(""),
  samples: z.number().default(0),
  success_rate: z.number().default(0),
  avg_cost_usd: z.number().nullable().default(null),
  avg_duration_secs: z.number().nullable().default(null),
}).loose();

export const WorkflowStatsResponseSchema = z.object({
  window_days: z.number().default(90),
  rows: z.array(WorkflowStatsSchema).default([]),
}).loose();

export const EMPTY_WORKFLOW_STATS_RESPONSE: WorkflowStatsResponse = {
  window_days: 90,
  rows: [],
};

// ---------------------------------------------------------------------------
// Living run plan (F04). The run publishes the checklist it is working
// through; the newest one replaces the last.
// ---------------------------------------------------------------------------

// `status` stays an OPEN string, not the three-value enum the server accepts
// today. The write side is closed (a POST with an unknown status is a 400), so
// only a NEWER server can produce one — and an installed desktop build meeting
// it must render the item with a neutral bullet, not drop the whole plan.
export const RunPlanItemSchema = z.object({
  text: z.string().default(""),
  status: z.string().default(""),
}).loose();

export const RunPlanSchema = z.object({
  items: z.array(RunPlanItemSchema).default([]),
  seq: z.number().default(0),
}).loose();
const TaskMemoryVersionSchema = z.object({
  id: z.string().uuid(),
  revision: z.number().int().min(1).max(2147483647),
});
const TaskMemoryContextSchema = z.object({
  dispatched_at: z.string().datetime({ offset: true }),
  agent_status: z.enum(["loaded", "unavailable"]),
  agent_versions: z.array(TaskMemoryVersionSchema).max(50),
  project_version: TaskMemoryVersionSchema.nullable(),
}).refine((value) =>
  (value.agent_status === "loaded" || value.agent_versions.length === 0) &&
  new Set(value.agent_versions.map((version) => version.id)).size === value.agent_versions.length,
);

export const AgentTaskSchema = z.object({
  // Invalid optional audit data is unknown, never a fabricated empty set.
  memory_context: TaskMemoryContextSchema.optional().catch(undefined),
  cancelled_by_comment_change: z.boolean().optional().catch(undefined),
  id: z.string(),
  agent_id: z.string().default(""),
  runtime_id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.string().default("cancelled"),
  priority: z.number().default(0),
  dispatched_at: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  last_activity_at: z.string().nullish(),
  result: z.unknown().default(null),
  error: z.string().nullable().default(null),
  failure_reason: z.string().optional(),
  created_at: z.string().default(""),
  chat_session_id: z.string().optional(),
  autopilot_run_id: z.string().optional(),
  parent_task_id: z.string().optional(),
  attempt: z.number().optional(),
  trigger_comment_id: z.string().optional(),
  // Coverage is additive display metadata. A mixed-version or partially
  // upgraded server must not make one malformed optional field erase the
  // entire execution log, so degrade that field to "absent" independently.
  coalesced_comment_ids: OptionalStringArraySchema,
  delivered_comment_ids: OptionalStringArraySchema,
  trigger_summary: z.string().optional(),
  kind: z.string().optional(),
  work_dir: z.string().optional().catch(undefined),
  relative_work_dir: z.string().optional().catch(undefined),
  durable_work_dir: z.string().optional().catch(undefined),
  relative_durable_work_dir: z.string().optional().catch(undefined),
  branch_name: z.string().optional().catch(undefined),
  attribution: TaskAttributionSchema.optional(),
  // Per-run token usage. Same independent-degradation rule as the coverage
  // arrays above: usage is additive display metadata, so one malformed entry
  // must cost the row its usage figure, not erase the whole execution log.
  // `.catch(undefined)` collapses a bad array to "no usage recorded", which
  // the UI already renders as an em dash.
  usage: z.array(TaskUsageSchema).optional().catch(undefined),
  // Smart-routing fields (JEF-237). Same independent-degradation rule as
  // `usage`: a malformed decision record costs the row its routing display,
  // not the whole execution log.
  task_class: z.string().optional().catch(undefined),
  routing: RuntimeRoutingDecisionSchema.nullable().optional().catch(undefined),
  // Off-peak batch lane (K45). Absent on older backends and on rows written
  // before the column, which read as the sync lane — so a missing value must
  // never render the off-peak badge.
  dispatch_lane: z.string().optional().catch(undefined),
  // Per-run confidence score (JEF-240). Same independent-degradation rule as
  // `routing`: a malformed record costs the row its confidence display, not
  // the whole execution log. Absent until the scorer has scored the run.
  confidence: TaskConfidenceSchema.nullable().optional().catch(undefined),
  // Workflow selector (JEF-273). Same independent-degradation rule: an
  // unknown strategy token costs the row its workflow display, not the whole
  // execution log. Absent on tasks that predate the selector.
  workflow: TaskWorkflowSchema.optional().catch(undefined),
  // Per-leg accounting (JEF-274). Both default to "" rather than undefined:
  // an empty role is the primary leg and an empty root means the run is its
  // own root, which is exactly what an older backend omitting them describes.
  leg_role: z.string().catch("").default(""),
  workflow_root_task_id: z.string().catch("").default(""),
  // Escalation origin (JEF-272): present on the child task a confidence
  // escalation created, absent on ordinary runs. Same independent-degradation
  // rule as `confidence` — a malformed record costs the row its escalation
  // display, not the whole execution log.
  escalation: TaskEscalationSchema.nullable().optional().catch(undefined),
  // Living run plan (F04). Same independent-degradation rule as `usage` and
  // `routing`: a malformed checklist costs the row its plan block, not the
  // whole execution log. Absent on runs that published none and on servers
  // that predate the feature, which the UI renders as no block at all.
  plan: RunPlanSchema.nullish().catch(undefined),
  // Turn checkpoints (F09). Same independent-degradation rule as `usage`: a
  // malformed value costs the row its revert action, not the whole execution
  // log. `revertable` is the affordance and it fails CLOSED — anything that is
  // not literally `true` reads as "not revertible", which is what a server
  // predating the feature produces and what the UI renders as no action at all.
  checkpoint_sha: z.string().optional().catch(undefined),
  turn_seq: z.number().optional().catch(undefined),
  revertable: z.boolean().optional().catch(undefined),
  // Worktree branch lifecycle (JEF-255): where this run's branch stands after
  // the run ended. `promoted_at` / `discarded_at` are the terminal markers,
  // `promote_pr_url` the pull request a promote opened ("" when none), and
  // `pending_branch_action` the promote/discard the daemon is executing right
  // now ("" when idle). Same independent-degradation rule as `revertable`:
  // absent on servers that predate the feature, which reads as "no action
  // taken, none in flight" — exactly what those servers describe.
  promoted_at: z.string().nullable().catch(null).default(null),
  discarded_at: z.string().nullable().catch(null).default(null),
  promote_pr_url: z.string().catch("").default(""),
  pending_branch_action: z.enum(["", "promote", "discard"]).catch("").default(""),
}).loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema);

// Worktree revert (F09). The response the enqueue endpoint and its poll
// endpoint return. `status` is a server-driven enum, so consumers switch on it
// with a default branch; the schema keeps it a plain string for that reason.
export const WorktreeRevertRequestSchema = z.object({
  request_id: z.string().default(""),
  status: z.string().default("failed"),
  error: z.string().optional().catch(undefined),
}).loose();

export type WorktreeRevertRequestResponse = z.infer<typeof WorktreeRevertRequestSchema>;

// Worktree run branch lifecycle (JEF-255). What GET /api/tasks/:id/diff
// returns: the stat and unified patch recorded when the run ended. Both are
// null when nothing was recorded; diff_truncated tells "the patch was too
// large to store" apart from "the run changed nothing", the same split the
// race attempts use. diff_stat stays `unknown` and goes through
// parseDiffStat — the daemon writes it as a JSONB blob no schema pins yet.
export const RunDiffSchema = z.object({
  diff_stat: z.unknown().nullable().catch(null).default(null),
  diff_unified: z.string().nullable().catch(null).default(null),
  diff_truncated: z.boolean().catch(false).default(false),
}).loose();

export type RunDiff = z.infer<typeof RunDiffSchema>;

// What POST …/runs/:taskId/promote and …/discard return: an acknowledgement,
// not a result — the branch lives on the user's machine, so the daemon does
// the work and the task row's promoted_at / discarded_at / pending_branch_action
// fields carry the outcome. `status` is a server-driven enum kept as a plain
// string for the same reason as WorktreeRevertRequestSchema's.
export const RunBranchActionResponseSchema = z.object({
  request_id: z.string().default(""),
  status: z.string().default("pending"),
}).loose();

export type RunBranchActionResponse = z.infer<typeof RunBranchActionResponseSchema>;

// Dead run-branch cleanup (JEF-388). What GET /api/runs/dead-branches
// returns: the workspace's finished-run branches that were never promoted,
// one entry per branch, grouped by runtime in the UI. `skip_reason` is a
// server-driven enum, but a client older than a new reason must still render
// the row, so unknown values degrade to null (reads as "actionable") only
// when `actionable` disagrees — the boolean is authoritative, the reason is
// display copy.
export const DeadBranchSkipReasonSchema = z
  .enum(["runtime_offline", "capability_missing", "action_pending"])
  .nullable()
  .catch(null)
  .default(null);

export const DeadBranchEntrySchema = z
  .object({
    task_id: z.string(),
    issue_id: z.string().nullable().catch(null).default(null),
    issue_identifier: z.string().nullable().catch(null).default(null),
    issue_title: z.string().nullable().catch(null).default(null),
    branch_name: z.string().catch("").default(""),
    runtime_id: z.string().catch("").default(""),
    runtime_name: z.string().catch("").default(""),
    finished_at: z.string().nullable().catch(null).default(null),
    actionable: z.boolean().catch(false).default(false),
    skip_reason: DeadBranchSkipReasonSchema,
  })
  .loose();

export type DeadBranchEntry = z.infer<typeof DeadBranchEntrySchema>;
export type DeadBranchSkipReason = DeadBranchEntry["skip_reason"];

export const DeadBranchPlanSchema = z
  .object({
    entries: z.array(DeadBranchEntrySchema).catch([]).default([]),
  })
  .loose();

export type DeadBranchPlan = z.infer<typeof DeadBranchPlanSchema>;

// What POST /api/runs/dead-branches/discard returns: how many discards were
// enqueued daemon-side and which task IDs were skipped (already actioned,
// runtime went away between plan and confirm). Like the single-run discard,
// the POST only enqueues — entries flip to skip_reason "action_pending" on
// the next plan fetch.
export const DeadBranchDiscardSkippedSchema = z
  .object({
    task_id: z.string().catch("").default(""),
    reason: z.string().catch("").default(""),
  })
  .loose();

export const DeadBranchDiscardResponseSchema = z
  .object({
    enqueued: z.number().catch(0).default(0),
    skipped: z.array(DeadBranchDiscardSkippedSchema).catch([]).default([]),
  })
  .loose();

export type DeadBranchDiscardResponse = z.infer<typeof DeadBranchDiscardResponseSchema>;
export type DeadBranchDiscardSkipped = z.infer<typeof DeadBranchDiscardSkippedSchema>;

// Task cancellation (`POST /api/tasks/:id/cancel`) is consumed directly by
// chat recovery. Its optional message payload must be well-formed before the
// UI deletes a message from cache or restores text into the input.
const CancelledChatMessageSchema = z.object({
  chat_session_id: z.string(),
  message_id: z.string(),
  content: z.string(),
  restore_to_input: z.boolean().default(false),
  // Attachments detached from the deleted message so a restored draft can
  // re-bind them on re-send. Absent on servers that predate the field.
  attachments: z.array(AttachmentSchema).optional(),
}).loose();

export const CancelTaskResponseSchema = AgentTaskSchema.extend({
  cancelled_chat_message: CancelledChatMessageSchema.nullish()
    .transform((value) => value ?? undefined),
}).loose();

const ChatLastMessageSchema = z.object({
  content: z.string().default(""),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  created_at: z.string().default(""),
  failure_reason: z.string().nullable().optional(),
  message_kind: z.enum([
    "message",
    "no_response",
    "onboarding_kickoff",
    "onboarding_opening",
  ]).optional().catch(undefined),
}).loose();

const ChatChannelSourceSchema = z.object({
  channel_type: z.string().default(""),
  installation_id: z.string().default(""),
  route_revision: z.number().default(0),
}).loose();

export const ChatSessionSchema: z.ZodType<ChatSession> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  creator_id: z.string().default(""),
  project_id: z.string().nullable().optional(),
  title: z.string().default(""),
  status: z.enum(["active", "archived"]).catch("active"),
  has_unread: z.boolean().default(false),
  unread_count: z.number().optional(),
  last_message: ChatLastMessageSchema.nullable().optional().catch(undefined),
  pinned: z.boolean().optional(),
  channel_source: ChatChannelSourceSchema.optional().catch(undefined),
  is_current_channel_route: z.boolean().optional().catch(undefined),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_CHAT_SESSION: ChatSession = {
  id: "",
  workspace_id: "",
  agent_id: "",
  creator_id: "",
  title: "",
  status: "active",
  has_unread: false,
  created_at: "",
  updated_at: "",
};
export const ChatSessionListSchema = z
  .array(ChatSessionSchema.catch(EMPTY_CHAT_SESSION))
  .transform((sessions) => sessions.filter((session) => session.id !== ""))
  .default([]);
export const EMPTY_CHAT_SESSION_LIST: ChatSession[] = [];

// Deferred-cancellation draft restores
// (`GET /api/chat/sessions/{id}/draft-restores`, #5219) feed the composer
// directly: `content` becomes the draft text, `attachments` re-bind on
// re-send, and `id` is the consume key. A malformed response falls back to
// an empty list — the durable row stays pending server-side, so nothing is
// lost by skipping a fetch.
const ChatDraftRestoreSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  task_id: z.string().optional(),
  content: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  created_at: z.string().optional(),
}).loose();

export const ChatDraftRestoresResponseSchema = z.object({
  restores: z.array(ChatDraftRestoreSchema).default([]),
}).loose();

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

// Root fields retain the legacy single-task response shape. Keep additive
// fields optional so callers can distinguish an older server from an empty
// queue. A malformed queue row is ignored without discarding a valid head.
export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
  supports_queue: z.boolean().optional(),
  queued_tasks: ChatQueuedTasksSchema.optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string().min(1),
  task_id: z.string().min(1),
  supports_queue: z.boolean().optional(),
  queued: z.boolean().optional().catch(undefined),
  created_at: z.string().min(1),
  attachment_ids: z.array(z.string()).nullish().transform((ids) => ids ?? undefined),
}).loose();

// `started` is the only field the flow branches on, and a malformed response
// must not be read as "the opening landed" — parseWithFallback's fallback says
// it did not, which leaves the flow's own retry as the recovery path.
export const StartMikaOnboardingResponseSchema: z.ZodType<StartMikaOnboardingResponse> = z.object({
  started: z.boolean(),
  message_id: z.string().nullish().transform((id) => id ?? undefined),
  created_at: z.string().nullish().transform((at) => at ?? undefined),
}).loose();

export const PrioritizeQueuedChatTaskResponseSchema:
  z.ZodType<PrioritizeQueuedChatTaskResponse> = z.object({
    task_id: z.string(),
    active_task_id: z.string().optional(),
  }).loose();

export const EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE:
  PrioritizeQueuedChatTaskResponse = { task_id: "" };

// GET /api/onboarding/checklist (OS plan, chantier 5). Drives the
// getting-started card shown on a fresh workspace. `runtime_kind` is open the
// same way IssueStatus is: the client only branches on the three keys the
// server documents today, and an unrecognized future kind still parses.
export interface OnboardingChecklistResponse {
  runtime_kind: "native" | "daemon" | "none";
  native_available: boolean;
  runtime_ready: boolean;
  agent_created: boolean;
  issue_created: boolean;
  first_run_completed: boolean;
  first_decision_answered: boolean;
  complete: boolean;
  agents: number;
  issues: number;
  completed_runs: number;
}

export const OnboardingChecklistSchema: z.ZodType<OnboardingChecklistResponse> = z.object({
  runtime_kind: z.enum(["native", "daemon", "none"]).catch("none"),
  native_available: BooleanWithDefaultSchema(false),
  runtime_ready: BooleanWithDefaultSchema(false),
  agent_created: BooleanWithDefaultSchema(false),
  issue_created: BooleanWithDefaultSchema(false),
  first_run_completed: BooleanWithDefaultSchema(false),
  first_decision_answered: BooleanWithDefaultSchema(false),
  complete: BooleanWithDefaultSchema(false),
  agents: z.number().catch(0),
  issues: z.number().catch(0),
  completed_runs: z.number().catch(0),
}).loose();

export const EMPTY_ONBOARDING_CHECKLIST: OnboardingChecklistResponse = {
  runtime_kind: "none",
  native_available: false,
  runtime_ready: false,
  agent_created: false,
  issue_created: false,
  first_run_completed: false,
  first_decision_answered: false,
  complete: false,
  agents: 0,
  issues: 0,
  completed_runs: 0,
};

export const EMPTY_CHAT_DRAFT_RESTORES: ChatDraftRestoresResponse = {
  restores: [],
};

export const EMPTY_CANCEL_TASK_RESPONSE: CancelTaskResponse = {
  id: "",
  agent_id: "",
  runtime_id: "",
  issue_id: "",
  status: "cancelled",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result: null,
  error: null,
  created_at: "",
};

export const AgentBuilderSessionSchema = z.object({
  session_id: z.string(),
  builder_agent_id: z.string(),
  runtime_id: z.string(),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION: AgentBuilderSession = {
  session_id: "",
  builder_agent_id: "",
  runtime_id: "",
};

/**
 * The stored configuration of a creation conversation. Every field falls back
 * to empty on its own: a draft written by a newer build (or truncated in
 * transit) must still restore the fields it does understand rather than
 * discarding the user's work wholesale.
 */
export const StoredAgentDraftSchema = z.object({
  name: z.string().catch(""),
  description: z.string().catch(""),
  instructions: z.string().catch(""),
  conversation_starters: z
    .array(
      z.object({
        label: z.string().catch(""),
        prompt: z.string().catch(""),
      }),
    )
    .catch([]),
  avatar_url: z.string().nullable().catch(null),
  model: z.string().catch(""),
  thinking_level: z.string().catch(""),
  service_tier: z.string().catch(""),
  skill_ids: z.array(z.string()).catch([]),
  permission_scope: z
    .enum(["private", "workspace", "members"])
    .catch("private"),
  member_ids: z.array(z.string()).catch([]),
  team_ids: z.array(z.string()).catch([]),
  applied_message_id: z.string().nullable().catch(null),
}).loose();

/**
 * One unfinished creation draft. Every field except the id has a safe empty
 * default: an older server that omits `runtime_id` must degrade to "let the
 * user pick" rather than dropping the whole row and losing the conversation.
 */
export const AgentBuilderSessionSummarySchema = z.object({
  session_id: z.string(),
  title: z.string().catch(""),
  runtime_id: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  last_message_content: z.string().catch(""),
  last_message_role: z.string().catch(""),
  last_message_at: z.string().catch(""),
  // Absent for a conversation the user has never hand-edited; the client then
  // replays the last <agent_draft> block instead of restoring a stored copy.
  draft: StoredAgentDraftSchema.nullish().catch(null),
}).loose();

export const AgentBuilderSessionListSchema = z.object({
  sessions: z.array(AgentBuilderSessionSummarySchema).catch([]),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION_LIST: {
  sessions: AgentBuilderSessionSummary[];
} = { sessions: [] };

export const AgentBuilderRuntimeSwitchSchema = z.object({
  runtime_id: z.string(),
}).loose();

// This endpoint returns 2xx only after the carrier has been bound to the
// runtime the caller asked for; anything else is a thrown error and no commit.
// So the safe fallback for an unparseable SUCCESS body is the requested id, not
// an empty one: the rebind did happen, and reporting "unknown" would leave the
// picker showing a runtime that is no longer executing — the exact split this
// endpoint exists to close.
export const agentBuilderRuntimeSwitchFallback = (
  requestedRuntimeID: string,
): AgentBuilderRuntimeSwitch => ({ runtime_id: requestedRuntimeID });

// Squad list responses carry lightweight membership previews used by hover
// cards. The preview fields are additive API fields, so older backends default
// cleanly to no preview instead of breaking newer frontends.
const SquadMemberPreviewSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  role: z.string().default(""),
}).loose();

export const SquadSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().default(""),
  instructions: z.string().default(""),
  system_key: z.string().optional(),
  system_instructions: z.string().optional(),
  avatar_url: z.string().nullable().optional().transform((v) => v ?? null),
  leader_id: z.string(),
  creator_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  archived_at: z.string().nullable().optional().transform((v) => v ?? null),
  archived_by: z.string().nullable().optional().transform((v) => v ?? null),
  member_count: z.number().default(0),
  member_preview: z.array(SquadMemberPreviewSchema).default([]),
}).loose();

export const SquadListSchema = z.array(SquadSchema);
export const EMPTY_SQUAD_LIST: Squad[] = [];
export const EMPTY_SQUAD: Squad = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "",
  creator_id: "",
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
  member_count: 0,
  member_preview: [],
};

// Squad member status — backs the Squad detail page's Members tab. status
// is `string | null` (not the narrow `SquadMemberStatusValue` union) so a
// new server-side status doesn't fail the parse; the UI defaults to a
// neutral pill for unknown values.
const SquadActiveIssueBriefSchema = z.object({
  issue_id: z.string(),
  identifier: z.string(),
  title: z.string(),
  issue_status: z.string(),
}).loose();

const SquadMemberStatusSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  status: z.string().nullable().optional().transform((v) => v ?? null),
  active_issues: z.array(SquadActiveIssueBriefSchema).default([]),
  last_active_at: z.string().nullable().optional().transform((v) => v ?? null),
}).loose();

export const SquadMemberStatusListResponseSchema = z.object({
  members: z.array(SquadMemberStatusSchema).default([]),
}).loose();

export const EMPTY_SQUAD_MEMBER_STATUS_LIST = { members: [] };

// ---------------------------------------------------------------------------
// Structured error body — POST /api/workspaces/:wsId/issues 409 conflict.
//
// When the server detects an active issue with the same title in the same
// workspace, it returns `{ code: "active_duplicate_issue", error, issue }`
// instead of letting the create through. The UI uses the embedded issue ref
// to offer "view existing" rather than dropping the user into a generic
// "create failed" toast.
//
// Strict guarantees:
//   - `code` is a literal so a future server rename (e.g. `duplicate_issue`)
//     fails the parse and falls back to a normal error toast — drift never
//     ships as a broken duplicate UI.
//   - `issue` is required; without an id/identifier/title the "view existing"
//     button has nothing to point at, so we'd rather fall back than guess.
//   - `issue.status` is intentionally OMITTED: the duplicate toast doesn't
//     render a StatusIcon (which has no fallback for unknown enum values),
//     so a future server-side rename of `status` must not knock this branch
//     out. `.loose()` lets the field pass through unchanged for any other
//     consumer.
// ---------------------------------------------------------------------------

export const DuplicateIssueErrorBodySchema = z.object({
  code: z.literal("active_duplicate_issue"),
  error: z.string().optional(),
  issue: z.object({
    id: z.string(),
    identifier: z.string(),
    title: z.string(),
  }).loose(),
}).loose();

export interface DuplicateIssueErrorBody {
  code: "active_duplicate_issue";
  error?: string;
  issue: {
    id: string;
    identifier: string;
    title: string;
  };
}

// ---------------------------------------------------------------------------
// Webhook delivery schemas — backing the Autopilot Deliveries section. Enums
// (`status`, `signature_status`, `provider`) are kept as `z.string()` so a
// future server-side value (e.g. a Stripe provider, a new dedupe state)
// degrades to a generic UI fallback rather than collapsing the list into
// the empty array. `.loose()` lets unknown fields pass through, matching
// the rule used by every other endpoint here.
// ---------------------------------------------------------------------------

const WebhookDeliverySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  autopilot_id: z.string(),
  trigger_id: z.string(),
  provider: z.string(),
  event: z.string(),
  dedupe_key: z.string().nullable(),
  dedupe_source: z.string().nullable(),
  signature_status: z.string(),
  status: z.string(),
  attempt_count: z.number().default(0),
  // Older servers predate the durable dispatch queue. Defaults preserve
  // compatibility while the UI rolls out alongside the new worker.
  dispatch_attempts: z.number().default(0),
  available_at: z.string().default(""),
  content_type: z.string().nullable(),
  response_status: z.number().nullable(),
  autopilot_run_id: z.string().nullable(),
  replayed_from_delivery_id: z.string().nullable(),
  error: z.string().nullable(),
  reason_code: z.string().nullable().default(null),
  replay_idempotency_key: z.string().nullable().default(null),
  received_at: z.string(),
  last_attempt_at: z.string(),
  created_at: z.string(),
  // Detail-only fields. The list endpoint omits them; the detail endpoint
  // populates raw_body / selected_headers / response_body.
  selected_headers: z.record(z.string(), z.unknown()).nullable().optional(),
  raw_body: z.string().nullable().optional(),
  response_body: z.string().nullable().optional(),
}).loose();

export const ListWebhookDeliveriesResponseSchema = z.object({
  deliveries: z.array(WebhookDeliverySchema).default([]),
  total: z.number().default(0),
}).loose();

export const WebhookDeliveryResponseSchema = WebhookDeliverySchema;

export const EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE: ListWebhookDeliveriesResponse = {
  deliveries: [],
  total: 0,
};

// ---------------------------------------------------------------------------
// Autopilot list schema. Enums (`status`, `execution_mode`, `trigger_kinds`,
// `last_run_status`) stay `z.string()` so future server-side values degrade
// to a generic UI fallback. The three derived fields (trigger_kinds /
// next_run_at / last_run_status) are list-endpoint-only and absent on older
// servers — optional by contract, the list renders "—" without them.
// ---------------------------------------------------------------------------

const AutopilotListItemSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable().optional(),
  project_id: z.string().nullable().optional(),
  // Older servers (pre-MUL-2429) omit assignee_type; "agent" is the
  // documented default.
  assignee_type: z.string().default("agent"),
  assignee_id: z.string(),
  status: z.string(),
  execution_mode: z.string(),
  // Off-peak batch lane (K45). Absent on older servers; false is the safe read
  // — an autopilot nobody opted in is never deferred.
  batch_eligible: z.boolean().catch(false).optional(),
  issue_title_template: z.string().nullable().optional(),
  created_by_type: z.string(),
  created_by_id: z.string(),
  last_run_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
  trigger_kinds: z.array(z.string()).optional(),
  next_run_at: z.string().nullable().optional(),
  last_run_status: z.string().nullable().optional(),
  // Per-caller write capability; absent on older servers (treated as unknown).
  can_write: z.boolean().optional(),
  // Narrower per-caller access-management capability (detail endpoint only).
  can_manage_access: z.boolean().optional(),
}).loose();

export const ListAutopilotsResponseSchema = z.object({
  autopilots: z.array(AutopilotListItemSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_AUTOPILOTS_RESPONSE = {
  autopilots: [],
  total: 0,
};

// Autopilot run (POST /trigger, GET /runs). Consumed by the "run now" flow,
// which branches on `status` to avoid a false-success toast (MUL-4525), so the
// response must be schema-parsed. `reason_code` is an additive, stable
// classification of a non-success run the UI localizes; older servers omit it.
// Defaults are conservative: an unreadable run degrades to a non-success status
// so the UI never shows success it cannot confirm. .loose() tolerates new fields.
export const AutopilotRunSchema = z.object({
  id: z.string().default(""),
  autopilot_id: z.string().default(""),
  trigger_id: z.string().nullable().default(null),
  source: z.string().default("manual"),
  status: z.string().default("failed"),
  issue_id: z.string().nullable().default(null),
  task_id: z.string().nullable().default(null),
  triggered_at: z.string().default(""),
  completed_at: z.string().nullable().default(null),
  failure_reason: z.string().nullable().default(null),
  reason_code: z.string().optional(),
  // Off-peak batch lane (K45): the lane of the run's linked task. Absent on
  // older servers and on runs with no task; the badge simply does not render.
  dispatch_lane: z.string().optional(),
  trigger_payload: z.unknown().default(null),
  result: z.unknown().default(null),
  created_at: z.string().default(""),
}).loose();

export const AutopilotQuotaUsageSchema = z.object({
  action: z.enum(["off", "observe", "enforce"]).catch("off"),
  used: z.number().nullable().default(null),
  reserved: z.number().nullable().default(null),
  total: z.number().nullable().default(null),
  limit: z.number().nullable().default(null),
  reached: z.boolean().nullable().default(null),
  period_start: z.string().nullable().default(null),
  period_end: z.string().nullable().default(null),
  reset_at: z.string().nullable().default(null),
  blocked_counts: z.record(z.string(), z.number().int().nonnegative()).nullable().catch(null).default(null),
}).loose();

export const FALLBACK_AUTOPILOT_RUN: AutopilotRun = {
  id: "",
  autopilot_id: "",
  trigger_id: null,
  source: "manual",
  status: "failed",
  issue_id: null,
  task_id: null,
  triggered_at: "",
  completed_at: null,
  failure_reason: null,
  trigger_payload: null,
  result: null,
  created_at: "",
};

// Cron preview: the server is the authority on the next occurrences. No
// `.default([])` here — a missing or reshaped field must fail validation so it
// degrades to the `next_runs: null` fallback ("preview unreadable") instead of
// masquerading as a valid empty list ("this expression never fires").
export const CronPreviewResponseSchema = z.object({
  next_runs: z.array(z.string()),
}).loose();

export const UNREADABLE_CRON_PREVIEW_RESPONSE: CronPreviewResponse = {
  next_runs: null,
};

// ---------------------------------------------------------------------------
// Trigger dry-runs. `reason_code` stays `z.string()` (the reason enum is
// server-canonical and the UI renders unknown codes verbatim), and
// `matched_filters` defaults to [] so an older server that omits it degrades
// to "no filter named" instead of collapsing the whole verdict.
//
// `would_run` has NO default: a verdict we cannot read must not masquerade as
// "this event would be dropped" — the fallbacks below say so explicitly.
// ---------------------------------------------------------------------------

export const WebhookTriggerDryRunSchema = z.object({
  would_run: z.boolean(),
  reason_code: z.string().nullable().default(null),
  explanation: z.string().default(""),
  matched_filters: z
    .array(z.object({ event: z.string(), actions: z.array(z.string()).optional() }).loose())
    .default([]),
  event: z.string().default(""),
}).loose();

export const ScheduleTriggerDryRunSchema = z.object({
  next_runs: z.array(z.string()),
  would_run: z.boolean(),
  reason_code: z.string().nullable().default(null),
  window_minutes: z.number().default(0),
}).loose();

// `unreadable` is the sentinel both dry-run surfaces branch on: it is neither
// "would run" nor a named blocking reason, so the UI says the preview could
// not be read rather than inventing a verdict.
export const UNREADABLE_WEBHOOK_DRY_RUN: WebhookTriggerDryRunResult = {
  would_run: false,
  reason_code: "unreadable",
  explanation: "",
  matched_filters: [],
  event: "",
};

export const UNREADABLE_SCHEDULE_DRY_RUN: ScheduleTriggerDryRunResult = {
  next_runs: [],
  would_run: false,
  reason_code: "unreadable",
  window_minutes: 0,
};

export const EMPTY_WEBHOOK_DELIVERY: WebhookDelivery = {
  id: "",
  workspace_id: "",
  autopilot_id: "",
  trigger_id: "",
  provider: "",
  event: "",
  dedupe_key: null,
  dedupe_source: null,
  signature_status: "not_required",
  status: "queued",
  attempt_count: 0,
  dispatch_attempts: 0,
  available_at: "",
  content_type: null,
  response_status: null,
  autopilot_run_id: null,
  replayed_from_delivery_id: null,
  error: null,
  reason_code: null,
  replay_idempotency_key: null,
  received_at: "",
  last_attempt_at: "",
  created_at: "",
};

// ---------------------------------------------------------------------------
// User (`/api/me` GET + PATCH). The auth store and Settings → Account both
// trust this shape — a drift here would knock both surfaces out. Kept
// lenient by the same rules as IssueSchema: enums stay `z.string()`,
// nullable fields are unioned with `null`, unknown server fields pass
// through via `.loose()`. `profile_description` is the field added in
// MUL-2406; the server emits `""` when unset (NOT NULL DEFAULT ''), so
// the schema defaults to `""` too — keeps the type tight without
// breaking older backends that don't return the column yet.
// ---------------------------------------------------------------------------

export const UserSchema = z.object({
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

// ---------------------------------------------------------------------------
// Cross-workspace unread inbox summary (`/api/inbox/unread-summary` GET).
// One entry per workspace the user belongs to that has unread items; the
// sidebar derives the workspace-switcher dot from it. Lenient per the usual
// rules so a future field addition can't blank the dot — on malformed JSON
// parseWithFallback returns the empty list, which simply hides the dot.
// ---------------------------------------------------------------------------

export const InboxUnreadSummarySchema = z.array(
  z
    .object({
      workspace_id: z.string(),
      count: z.number(),
    })
    .loose(),
);

export const EMPTY_INBOX_UNREAD_SUMMARY: InboxWorkspaceUnread[] = [];

// ---------------------------------------------------------------------------
// Inbox items (`/api/inbox` and `/api/inbox/archived` GET).
// Lenient per the usual rules: `severity` / `type` / `recipient_type` stay
// `z.string()` so a notification kind this client doesn't know yet still
// parses and renders (the UI's type-label lookup already tolerates unknown
// kinds). Nullable optional fields are declared optional as well, since older
// rows can omit them entirely. On malformed JSON parseWithFallback returns the
// empty list — the affected view then reads as empty rather than white-
// screening the inbox. Both endpoints share this boundary because they return
// the same row shape and both feed the status/priority filter UI.
// ---------------------------------------------------------------------------

export const InboxItemListSchema = z.array(
  z
    .object({
      id: z.string(),
      workspace_id: z.string(),
      recipient_type: z.string(),
      recipient_id: z.string(),
      type: z.string(),
      severity: z.string(),
      issue_id: z.string().nullish(),
      title: z.string(),
      body: z.string().nullish(),
      issue_status: z.string().nullish(),
      issue_priority: z.string().nullish(),
      read: z.boolean(),
      archived: z.boolean(),
      created_at: z.string(),
    })
    .loose(),
);

export const EMPTY_INBOX_ITEMS: InboxItem[] = [];

// Attention Inbox (K02): the same rows plus a server-computed risk.
// Inbox zero (K63): my pending Decision Cards, options included.
export const InboxDecisionsSchema = z.object({
  decisions: z.array(z.object({
    inbox_item_id: z.string().default(""),
    issue_id: z.string().default(""),
    issue_identifier: z.string().catch("").default(""),
    issue_title: z.string().catch("").default(""),
    risk_score: z.number().catch(0).default(0),
    decision: IssueDecisionSchema,
  }).loose()).catch([]).default([]),
  total: z.number().int().catch(0).default(0),
}).loose();

export const AttentionInboxListSchema = z.object({
  items: z.array(
    InboxItemListSchema.element.extend({
      risk_score: z.number().default(0),
      reason: z.string().default(""),
    }),
  ).catch([]).default([]),
}).loose();

// ---------------------------------------------------------------------------
// Billing schemas (cloud-billing proxy surface)
//
// All billing JSON we receive comes from multica-cloud verbatim — we proxy
// the bytes without re-shaping. These schemas use `loose()` so a future
// non-breaking field addition on the cloud side doesn't crash us; required
// fields are still strictly enforced. EMPTY_* constants supply the
// fallback parseWithFallback uses when the upstream response is malformed
// or unparseable.

export const BillingBalanceSchema = z.object({
  owner_id: z.string(),
  balance_micro: z.number(),
  balance_credit: z.number(),
  updated_at: z.string(),
}).loose();

export const EMPTY_BILLING_BALANCE: BillingBalance = {
  owner_id: "",
  balance_micro: 0,
  balance_credit: 0,
  updated_at: "",
};

// `tx_type` and `source` are kept as plain strings here; the cloud doc
// enumerates the canonical values but the frontend display tolerates
// unknown ones gracefully. Strict enums would crash the page on a future
// addition (e.g. a new `topup` source kind).
export const BillingTransactionSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  idempotency_key: z.string().default(""),
  tx_type: z.string(),
  source: z.string(),
  amount_micro: z.number(),
  balance_after: z.number(),
  reference_id: z.string().default(""),
  description: z.string().default(""),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
}).loose();

export const BillingTransactionsPageSchema = z.object({
  items: z.array(BillingTransactionSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TRANSACTIONS_PAGE: BillingTransactionsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingBatchSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  source_tx_id: z.string().default(""),
  source_type: z.string(),
  total_micro: z.number(),
  remaining_micro: z.number(),
  // Cloud either omits the key (never expires) or sends a string
  // timestamp. Null is also tolerated since some serializers emit
  // explicit nulls for absent timestamps.
  expires_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingBatchesPageSchema = z.object({
  items: z.array(BillingBatchSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_BATCHES_PAGE: BillingBatchesPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingTopupSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  amount_cents: z.number(),
  currency: z.string().default("usd"),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  status: z.string(),
  tier_id: z.string().default(""),
  stripe_checkout_id: z.string().default(""),
  // Only set after status reaches `credited` — leave optional rather
  // than coerce to "" so a UI can branch on existence.
  purchase_batch_id: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingTopupsPageSchema = z.object({
  items: z.array(BillingTopupSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TOPUPS_PAGE: BillingTopupsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingPriceTierSchema = z.object({
  id: z.string(),
  // Cloud doc says display_name falls back to id; tolerate empty too.
  display_name: z.string().default(""),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().optional(),
  bonus_expires_in: z.string().optional(),
}).loose();

export const BillingPriceTierListSchema = z.array(BillingPriceTierSchema);

export const EMPTY_BILLING_PRICE_TIER_LIST: BillingPriceTier[] = [];

export const CreateBillingCheckoutSessionResponseSchema = z.object({
  order_id: z.string(),
  session_id: z.string(),
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE: CreateBillingCheckoutSessionResponse = {
  order_id: "",
  session_id: "",
  url: "",
};

export const BillingCheckoutSessionStatusSchema = z.object({
  order_id: z.string(),
  status: z.string(),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  currency: z.string().default("usd"),
  tier_id: z.string().default(""),
}).loose();

export const EMPTY_BILLING_CHECKOUT_SESSION_STATUS: BillingCheckoutSessionStatus = {
  order_id: "",
  status: "pending",
  amount_cents: 0,
  credits: 0,
  bonus_credits: 0,
  currency: "usd",
  tier_id: "",
};

export const CreateBillingPortalSessionResponseSchema = z.object({
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE: CreateBillingPortalSessionResponse = {
  url: "",
};

// ---------------------------------------------------------------------------
// Workspace subscriptions (`/api/cloud-subscriptions/*`)
//
// These schemas are the compatibility boundary with multica-cloud. Three rules
// hold for all of them:
//
//  1. There is no fallback value. Callers get `null` on any parse failure and
//     must render "unavailable" — never a synthetic Free plan, because that
//     turns an upstream outage or an older cloud into a silent downgrade of a
//     paying workspace.
//  2. `.loose()` keeps unknown keys, so a cloud that adds fields does not break
//     an older client.
//  3. `plan` and `status` stay open strings. A new plan or Stripe status must
//     surface as unknown rather than be coerced into a known one.

const WorkspaceSubscriptionIntervalSchema = z.enum(["month", "year"]);

// Stripe hosts Checkout and Portal, so those URLs leave the app. `z.string()
// .url()` is not enough on its own — `new URL("javascript:...")` parses — and
// the caller hands this value to location.assign, so the scheme is pinned here.
const StripeHostedURLSchema = z.string().url().refine(
  (value) => value.startsWith("https://"),
  { message: "Stripe hosted URL must use HTTPS" },
);

const WorkspaceEntitlementLimitSchema = z
  .discriminatedUnion("mode", [
    z
      .object({
        mode: z.literal("limited"),
        limit: z.number().int().positive(),
      })
      .loose(),
    z.object({ mode: z.literal("unlimited") }).loose(),
  ])
  .transform(
    (value): WorkspaceSubscriptionEntitlements["limits"]["issueCount"] =>
      value.mode === "limited"
        ? { mode: "limited", limit: value.limit }
        : { mode: "unlimited", limit: null },
  );

export const WorkspaceSubscriptionEntitlementsSchema = z
  .object({
    workspace_id: z.string(),
    plan: z.string(),
    status: z.string(),
    // Cloud documents seats as >= 1, but accepting 0 costs nothing and keeps a
    // workspace that momentarily reports no human members readable instead of
    // failing the whole snapshot.
    seats: z.number().int().nonnegative(),
    limits: z
      .object({
        issue_count: WorkspaceEntitlementLimitSchema,
        autopilot_runs: WorkspaceEntitlementLimitSchema,
      })
      .loose(),
    current_period_end: z.string().nullable().optional(),
    snapshot_expires_at: z.string().nullable().optional(),
    version: z.number().int().nonnegative(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionEntitlements => ({
      workspaceId: value.workspace_id,
      plan: value.plan,
      status: value.status,
      seats: value.seats,
      limits: {
        issueCount: value.limits.issue_count,
        autopilotRuns: value.limits.autopilot_runs,
      },
      currentPeriodEnd: value.current_period_end ?? null,
      snapshotExpiresAt: value.snapshot_expires_at ?? null,
      version: value.version,
    }),
  );

export const WorkspaceSubscriptionSummarySchema = z
  .object({
    entitlement: WorkspaceSubscriptionEntitlementsSchema,
    billing_interval: WorkspaceSubscriptionIntervalSchema.nullable(),
    human_members: z.number().int().nonnegative(),
    seat_capacity: z
      .object({
        purchased: z.number().int().positive(),
        used: z.number().int().nonnegative(),
        reserved: z.number().int().nonnegative(),
        available: z.number().int().nonnegative(),
        overcommitted: z.boolean(),
        version: z.number().int().positive(),
        pending_quantity: z.number().int().positive().nullable(),
        active_purchase: z
          .object({
            request_id: z.string(),
            target_seats: z.number().int().positive(),
            status: z.enum(["pending", "processing", "submitted"]),
            expires_at: z.string().min(1).optional(),
          })
          .loose()
          .optional(),
      })
      .loose()
      .nullable(),
    cancel_at_period_end: z.boolean(),
    grace_until: z.string().nullable(),
    has_stripe_customer: z.boolean(),
    available_actions: z.object({
      checkout: z.boolean(),
      portal: z.boolean(),
      purchase_seats: z.boolean(),
    }).loose(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSummary => ({
      entitlement: value.entitlement,
      billingInterval: value.billing_interval,
      humanMembers: value.human_members,
      seatCapacity: value.seat_capacity
        ? {
            purchased: value.seat_capacity.purchased,
            used: value.seat_capacity.used,
            reserved: value.seat_capacity.reserved,
            available: value.seat_capacity.available,
            overcommitted: value.seat_capacity.overcommitted,
            version: value.seat_capacity.version,
            pendingQuantity: value.seat_capacity.pending_quantity,
            activePurchase: value.seat_capacity.active_purchase
              ? {
                  requestId:
                    value.seat_capacity.active_purchase.request_id,
                  targetSeats:
                    value.seat_capacity.active_purchase.target_seats,
                  status: value.seat_capacity.active_purchase.status,
                  expiresAt:
                    value.seat_capacity.active_purchase.expires_at ?? null,
                }
              : null,
          }
        : null,
      cancelAtPeriodEnd: value.cancel_at_period_end,
      graceUntil: value.grace_until,
      hasStripeCustomer: value.has_stripe_customer,
      availableActions: {
        checkout: value.available_actions.checkout,
        portal: value.available_actions.portal,
        purchaseSeats: value.available_actions.purchase_seats,
      },
    }),
  );

export const IssueLimitUsageSchema = z
  .object({
    used: z.number().int().nonnegative(),
    limit: z.number().int().positive(),
  })
  .loose()
  .transform(
    (value): IssueLimitUsage => ({
      used: value.used,
      limit: value.limit,
    }),
  );

const WorkspaceSubscriptionPriceSchema = (
  expected: "month" | "year",
) =>
  z
    .object({
      currency: z.string().min(1),
      // Reject 0 and negatives: a free or malformed Price must read as
      // "price unavailable", not as a real amount shown next to a purchase
      // button.
      unit_amount: z.number().int().positive(),
      // Pinned to the slot it arrived in. Cloud validates this too, but a
      // schema that accepted a yearly Price under `month` would let the UI
      // quote a yearly amount as a monthly one — the schema is an independent
      // boundary, so it checks the correspondence itself.
      interval: z.literal(expected),
      interval_count: z.literal(1),
    })
    .loose()
    .transform(
      (value): WorkspaceSubscriptionPrice => ({
        currency: value.currency,
        unitAmount: value.unit_amount,
        interval: value.interval,
        intervalCount: value.interval_count,
      }),
    );

export const WorkspaceSubscriptionPricesSchema = z
  .object({
    month: WorkspaceSubscriptionPriceSchema("month"),
    year: WorkspaceSubscriptionPriceSchema("year"),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionPrices => ({
      month: value.month,
      year: value.year,
    }),
  );

export const CreateWorkspaceSubscriptionCheckoutResponseSchema = z
  .object({
    request_id: z.string(),
    session_id: z.string(),
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionCheckoutResponse => ({
      requestId: value.request_id,
      sessionId: value.session_id,
      url: value.url,
    }),
  );

export const WorkspaceSubscriptionSeatReconcileResultSchema = z
  .object({
    workspace_id: z.string(),
    action: z.string(),
    version: z.number().int().nonnegative(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSeatReconcileResult => ({
      workspaceId: value.workspace_id,
      action: value.action,
      version: value.version,
    }),
  );

export const WorkspaceSeatPurchasePreviewSchema = z
  .object({
    current_seats: z.number().int().positive(),
    additional_seats: z.number().int().positive(),
    resulting_seats: z.number().int().positive(),
    purchase_version: z.number().int().positive(),
    currency: z.string().regex(/^[a-z]{3}$/),
    proration_amount: z.number().int().nonnegative(),
    next_invoice_amount: z.number().int().nonnegative(),
    quoted_at: z.string().min(1),
  })
  .loose()
  .transform(
    (value): WorkspaceSeatPurchasePreview => ({
      currentSeats: value.current_seats,
      additionalSeats: value.additional_seats,
      resultingSeats: value.resulting_seats,
      purchaseVersion: value.purchase_version,
      currency: value.currency,
      prorationAmount: value.proration_amount,
      nextInvoiceAmount: value.next_invoice_amount,
      quotedAt: value.quoted_at,
    }),
  );

export const PurchaseWorkspaceSeatsResponseSchema = z
  .object({
    request_id: z.string(),
    current_seats: z.number().int().positive(),
    additional_seats: z.number().int().positive(),
    resulting_seats: z.number().int().positive(),
    currency: z.string().regex(/^[a-z]{3}$/),
    proration_amount: z.number().int().nonnegative(),
    next_invoice_amount: z.number().int().nonnegative(),
    status: z.enum(["pending", "submitted", "confirmed"]),
  })
  .loose()
  .transform(
    (value): PurchaseWorkspaceSeatsResponse => ({
      requestId: value.request_id,
      currentSeats: value.current_seats,
      additionalSeats: value.additional_seats,
      resultingSeats: value.resulting_seats,
      currency: value.currency,
      prorationAmount: value.proration_amount,
      nextInvoiceAmount: value.next_invoice_amount,
      status: value.status,
    }),
  );

export const CreateWorkspaceSubscriptionPortalResponseSchema = z
  .object({
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionPortalResponse => ({
      url: value.url,
    }),
  );

// ---------------------------------------------------------------------------
// Runtime model discovery (`POST /api/runtimes/:id/models`,
// `GET /api/runtimes/:id/models/:requestId`). Both endpoints return the same
// request record, and the UI drives a state machine off `status`, so the two
// fields that decide behaviour are pinned: `status` gates the polling loop and
// `supported` gates whether the picker is usable at all. Everything else stays
// lenient per the rules at the top of this file.
//
// `status` deliberately stays `z.string()` (a newer server may add a state);
// `resolveRuntimeModels` treats anything it does not recognise as an explicit
// failure rather than a completed-but-empty catalog. `supported` defaults to
// true so a server old enough to omit it keeps the picker enabled instead of
// rendering "managed by runtime" off an `undefined`.
//
// `cached` / `cached_at` are additive markers for a snapshot served from the
// server-side catalog cache (MUL-5444); an older backend omits them.
// ---------------------------------------------------------------------------

const RuntimeModelThinkingLevelSchema = z.object({
  value: z.string(),
  label: z.string().default(""),
  description: z.string().optional(),
}).loose();

const RuntimeModelThinkingSchema = z.object({
  supported_levels: z.array(RuntimeModelThinkingLevelSchema).default([]),
  default_level: z.string().optional(),
}).loose();

const RuntimeModelServiceTierSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  description: z.string().optional(),
}).loose();

// A model entry with no `id` is unselectable — `onChange(m.id)` would persist
// an empty model — so `id` is required and a malformed entry drops the whole
// response to the fallback rather than rendering a dead row.
const RuntimeModelSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  provider: z.string().optional(),
  default: z.boolean().optional(),
  thinking: RuntimeModelThinkingSchema.nullable().optional()
    .transform((v) => v ?? undefined),
  service_tiers: z.array(RuntimeModelServiceTierSchema).optional(),
  supports_explicit_standard_service_tier: z.boolean().optional(),
}).loose();

// A row the runtime named but will not run (MUL-6961). Parsed from its own
// top-level list, never from `models`, so nothing here can become a selectable
// value. `id` is required for the same reason it is on RuntimeModelSchema — a
// row without one cannot even be keyed in a list.
const RuntimeUnavailableModelSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  reason: z.string().optional(),
}).loose();

export const RuntimeModelListRequestSchema = z.object({
  id: z.string().default(""),
  runtime_id: z.string().default(""),
  status: z.string(),
  models: z.array(RuntimeModelSchema).optional(),
  // Absent on any daemon or server older than the field, which simply means
  // the picker shows no unavailable section.
  unavailable_models: z.array(RuntimeUnavailableModelSchema).optional(),
  supported: z.boolean().default(true),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  cached: z.boolean().optional(),
  cached_at: z.string().optional(),
}).loose();

// Fallback for an unparseable model-discovery response. `failed` is the only
// honest choice: `completed` would fabricate an empty catalog (and silently
// clear a saved model when `supported` is read as false), while `pending`
// would spin the picker until the client-side poll timeout. `failed` surfaces
// "discovery failed" immediately and leaves the creatable manual-entry field
// working, which is the same degradation as a real discovery failure.
export const MALFORMED_RUNTIME_MODEL_LIST_REQUEST: RuntimeModelListRequest = {
  id: "",
  runtime_id: "",
  status: "failed",
  supported: true,
  error: "invalid model discovery response",
  created_at: "",
  updated_at: "",
};

export const RuntimeCliAuthRequestSchema = z.object({
  id: z.string(),
  runtime_id: z.string(),
  action: z.string(),
  status: z.string(),
  verification_url: z.string().url().optional(),
  user_code: z.string().optional(),
  authenticated: z.boolean().optional(),
  error: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
  expires_at: z.string(),
}).loose();

export const DingTalkInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
  installed_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  agent_available: z.boolean().optional(),
  bound_dingtalk_user_ids: z.array(z.string()).catch([]).default([]),
}).loose();

export const EMPTY_DINGTALK_INSTALLATION: DingTalkInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  installer_user_id: "",
  status: "revoked",
  installed_at: "",
  created_at: "",
  updated_at: "",
  bound_dingtalk_user_ids: [],
};

export const ListDingTalkInstallationsResponseSchema = z.object({
  installations: z.array(DingTalkInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_DINGTALK_INSTALLATIONS_RESPONSE: ListDingTalkInstallationsResponse = {
  installations: [],
  configured: false,
};

export const DingTalkGroupBotSchema = z.object({
  installation_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_name: z.string().default(""),
  bot_identity_issue: z.string().default(""),
  last_active_at: z.string().optional(),
  mention_count: z.number().int().nonnegative().optional(),
}).loose();

export const DingTalkGroupSchema = z.object({
  conversation_id: z.string(),
  conversation_title: z.string().default(""),
  bots: z.array(DingTalkGroupBotSchema).catch([]).default([]),
}).loose();

export const ListDingTalkGroupsResponseSchema = z.object({
  groups: z.array(DingTalkGroupSchema).default([]),
  group_discovery_supported: z.boolean().default(false),
  inactive_group_counts: z.record(z.string(), z.number().int().nonnegative()).optional(),
  bot_identities: z.record(z.string(), DingTalkGroupBotSchema).optional(),
  next_offset: z.number().int().nonnegative().optional(),
}).loose();

export const EMPTY_LIST_DINGTALK_GROUPS_RESPONSE: ListDingTalkGroupsResponse = {
  groups: [],
  group_discovery_supported: false,
};

export const RedeemDingTalkBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  dingtalk_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_DINGTALK_BINDING_TOKEN_RESPONSE: RedeemDingTalkBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  dingtalk_user_id: "",
};

// WeCom smart-bot ("智能机器人" / aibot) installation responses. `.loose()` so a
// newer backend field never fails the parse on an older desktop build (see
// CLAUDE.md → API Compatibility). Defaults are chosen so a malformed response
// degrades safely: `configured` defaults false (renders the "ask your operator"
// state rather than a Connect dialog whose submit is guaranteed to fail), and a
// missing `status` defaults to "revoked" rather than "active" so a broken read
// never shows a bot as connected when it may not be.
export const WecomInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
}).loose();

export const EMPTY_WECOM_INSTALLATION: WecomInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  bot_id: "",
  installer_user_id: "",
  status: "revoked",
};

export const ListWecomInstallationsResponseSchema = z.object({
  installations: z.array(WecomInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE: ListWecomInstallationsResponse = {
  installations: [],
  configured: false,
};

export const RedeemWecomBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  wecom_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE: RedeemWecomBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  wecom_user_id: "",
};

export const TelegramInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_id: z.string().default(""),
  bot_username: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
  installed_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_TELEGRAM_INSTALLATION: TelegramInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  bot_id: "",
  bot_username: "",
  installer_user_id: "",
  status: "revoked",
  installed_at: "",
  created_at: "",
  updated_at: "",
};

export const ListTelegramInstallationsResponseSchema = z.object({
  installations: z.array(TelegramInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_TELEGRAM_INSTALLATIONS_RESPONSE: ListTelegramInstallationsResponse = {
  installations: [],
  configured: false,
};

export const RedeemTelegramBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  telegram_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_TELEGRAM_BINDING_TOKEN_RESPONSE: RedeemTelegramBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  telegram_user_id: "",
};

// Skills. Introduced for `POST /api/skills/:id/refresh` (update a skill from
// its imported source). `config` stays a loose record: the server owns the
// `origin` provenance shape and may extend it freely.
export const SkillFileSchema = z.object({
  id: z.string(),
  skill_id: z.string(),
  path: z.string(),
  content: z.string().optional().default(""),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
}).loose();

export const SkillSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  content: z.string().optional().default(""),
  config: z.record(z.string(), z.unknown()).optional().default({}),
  created_by: z.string().nullable().optional().default(null),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
  files: z.array(SkillFileSchema).optional().default([]),
  status: z.string().catch("published").default("published"),
}).loose();

// Skill Miner (K58).
export const SkillDraftSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().catch("").default(""),
  name: z.string().catch("").default(""),
  description: z.string().catch("").default(""),
  config: z.record(z.string(), z.unknown()).catch({}).default({}),
  created_by: z.string().nullable().catch(null).default(null),
  created_at: z.string().catch("").default(""),
  updated_at: z.string().catch("").default(""),
  status: z.string().catch("draft").default("draft"),
  sources: z.array(z.object({
    issue_id: z.string().catch("").default(""),
    issue_number: z.number().catch(0).default(0),
    issue_title: z.string().catch("").default(""),
    comment_id: z.string().catch("").default(""),
    status_regressed: z.boolean().catch(false).default(false),
  }).loose()).catch([]).default([]),
}).loose();
export const SkillDraftListSchema = z.object({ drafts: z.array(SkillDraftSchema).catch([]).default([]) }).loose();

export const EMPTY_SKILL: Skill = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  content: "",
  config: {},
  created_by: null,
  created_at: "",
  updated_at: "",
  files: [],
};

export const SkillImportExistingSkillSchema = z.object({
  id: z.string(),
  name: z.string(),
  created_by: z.string().optional(),
  can_overwrite: z.boolean().optional(),
}).loose();

/**
 * Envelope of POST /api/skills/import.
 *
 * `status` stays a plain string (not an enum) so a status added by a newer
 * backend still parses and its `reason` survives to the user. `z.enum` here
 * would fail the whole envelope on an unknown value, drop the server's reason
 * and leave only a generic "Import failed" — the server field is a bare
 * `string`, so it is free to grow. `parseSkillImportResult` has the default
 * branch: anything outside created/updated is treated as a failure.
 */
export const SkillImportResultSchema = z.object({
  status: z.string().default("failed"),
  reason: z.string().optional().default(""),
  skill: SkillSchema.optional(),
  existing_skill: SkillImportExistingSkillSchema.optional(),
}).loose();

export const EMPTY_SKILL_IMPORT_RESULT: SkillImportResult = {
  status: "failed",
  reason: "",
};

// Agent persistent memories (JEF-236). `source` stays a plain string so a
// newer backend source kind still parses — consumers render it with a
// default-bearing branch. Fields default so a partial payload degrades to a
// renderable row rather than dropping the whole list.
export const AgentMemorySchema = z.object({
  source_review: z.object({
    review_id: z.string().uuid(), issue_id: z.string().uuid(), task_id: z.string().uuid(),
    feedback: z.string(), criteria: z.array(z.string()).max(20),
    assessments: z.array(z.object({ passed: z.boolean(), evidence: z.string() })).max(20),
    snapshot_token: z.string().regex(/^[a-f0-9]{64}$/),
    reviewed_by: z.string().uuid(), reviewed_at: z.iso.datetime({ offset: true }),
  }).nullable().optional(),
  id: z.string(),
  agent_id: z.string(),
  content: z.string().optional().default(""),
  source: z.string().optional().default("manual"),
  // Governance state (JEF-269). Tolerant default: a pre-governance server
  // omits the field, and every fact it stored was human-approved.
  state: z.enum(["draft", "approved"]).optional().default("approved"),
  source_task_id: z.string().nullable().optional().default(null),
  source_issue_id: z.string().nullable().optional().default(null),
  status: z.string().catch("unknown").default("unknown"),
  revision: z.number().int().positive().catch(0).default(0),
  reviewed_by: z.string().nullable().catch(null).default(null),
  reviewed_at: z.string().nullable().catch(null).default(null),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
  expires_at: z.iso.datetime({ offset: true }).nullable().default(null),
  expired: z.boolean().default(false),
}).loose();

export const EMPTY_AGENT_MEMORY: AgentMemory = {
  id: "",
  agent_id: "",
  content: "",
  source: "manual",
  state: "approved",
  source_task_id: null,
  source_issue_id: null,
  created_at: "",
  updated_at: "",
  status: "active",
  revision: 1,
  reviewed_by: null,
  reviewed_at: null,
  expires_at: null,
  expired: false,
};

// The list endpoint wraps the rows so it can report how many of them a run
// brief actually carries and whether the extraction pass is configured —
// neither is derivable from the rows.
export const AgentMemoryListSchema = z.object({
  memories: z.array(AgentMemorySchema).optional().default([]),
  briefed_count: z.number().optional().default(0),
  extraction_enabled: z.boolean().optional().default(false),
}).loose();

export const EMPTY_AGENT_MEMORY_LIST: AgentMemoryList = {
  memories: [],
  briefed_count: 0,
  extraction_enabled: false,
};

const MemoryEvaluationOutcomeSchema = z.object({
  status: z.string(), duration_ms: z.number().int().nonnegative(), artifact: z.string(), diagnostic: z.string(),
  cost_usd: z.number().finite().nonnegative().nullable().optional(),
  cost_source: z.literal("catalog_estimate").optional(),
  runtime: z.object({
    provider: z.string(), requested_model: z.string(), requested_effort: z.string(), status: z.string(),
    executable_hash: z.string().regex(/^[a-f0-9]{64}$/), prompt_hash: z.string().regex(/^[a-f0-9]{64}$/), brief_hash: z.string().regex(/^[a-f0-9]{64}$/),
    tool_calls: z.number().int().nonnegative(),
    usage: z.record(z.string(), z.object({ input_tokens: z.number().int().nonnegative(), output_tokens: z.number().int().nonnegative(), cache_read_tokens: z.number().int().nonnegative(), cache_write_tokens: z.number().int().nonnegative() }).passthrough()).nullable(),
  }).passthrough().optional(),
}).passthrough().refine((o) => (o.cost_usd == null) === (o.cost_source === undefined), "cost_usd requires catalog_estimate source");

export const AgentMemoryEvaluationSchema = z.object({
  execution_status: z.string().optional(), execution_runtime_id: z.string().optional(),
  id: z.string().uuid(), memory_id: z.string().uuid(), revision: z.number().int().positive(),
  uploaded_by: z.string().uuid(), created_at: z.string().datetime({ offset: true }),
  adopted_revision: z.number().int().positive().nullable(), eligible: z.boolean(), reason: z.string(),
  report_hash: z.string().regex(/^[a-f0-9]{64}$/).optional(),
  cost_status: z.enum(["unavailable", "estimated", "partial"]).optional(),
  estimated_cost_usd: z.number().finite().nonnegative().optional(),
  total: z.number().int().min(2).max(16), baseline_passed: z.number().int().nonnegative(),
  candidate_passed: z.number().int().nonnegative(), regressions: z.number().int().nonnegative(), errors: z.number().int().nonnegative(),
  report: z.object({
    candidate: z.object({ content: z.string(), revision: z.number().int().positive() }).passthrough(),
    suite: z.object({ image: z.string(), worker: z.array(z.string()), verifier: z.array(z.string()) }).passthrough(),
    cases: z.array(z.object({ id: z.string(), split: z.string(), input_hash: z.string(), checks_hash: z.string(), baseline: MemoryEvaluationOutcomeSchema, candidate: MemoryEvaluationOutcomeSchema }).passthrough()).nullable().transform((cases) => cases ?? []),
  }).passthrough().optional(),
}).refine((item) => item.baseline_passed <= item.total && item.candidate_passed <= item.total && item.regressions <= item.total && item.errors <= item.total && (!item.eligible || (item.candidate_passed === item.total && item.regressions === 0 && item.errors === 0)), "Invalid evaluation counts")
  .refine((item) => !item.report || (item.report.candidate.revision === item.revision && item.report.cases.length <= item.total && (!item.eligible || item.report.cases.length === item.total)), "Invalid evaluation detail")
  .refine((item) => item.estimated_cost_usd === undefined || item.cost_status === "estimated" || item.cost_status === "partial", "estimated_cost_usd requires cost_status");
export const AgentMemoryEvaluationListSchema = z.array(AgentMemoryEvaluationSchema).max(10);

export const AgentMemoryHistorySchema = z.object({
  versions: z.array(AgentMemorySchema.extend({
    content: z.string().min(1),
    revision: z.number().int().positive(),
    restored_from_revision: z.number().int().positive().optional(),
  })),
  next_before_revision: z.number().int().positive().nullable(),
});

const MemoryUsageCountSchema = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
export const AgentMemoryUsageSchema = z.object({
  since: z.string().datetime({ offset: true }),
  until: z.string().datetime({ offset: true }),
  started_runs: MemoryUsageCountSchema,
  recorded_runs: MemoryUsageCountSchema,
  unrecorded_runs: MemoryUsageCountSchema,
  load_failed_runs: MemoryUsageCountSchema,
  runs_with_agent_memory: MemoryUsageCountSchema,
  versions: z.array(z.object({
    memory_id: z.string().uuid(),
    revision: z.number().int().min(1).max(2147483647),
    prepared_runs: MemoryUsageCountSchema.refine((count) => count > 0),
    last_started_at: z.string().datetime({ offset: true }),
  })),
}).refine((value) => {
  const since = Date.parse(value.since), until = Date.parse(value.until);
  return until - since === 30 * 24 * 60 * 60 * 1000 &&
    value.started_runs === value.recorded_runs + value.unrecorded_runs &&
    value.recorded_runs >= value.load_failed_runs + value.runs_with_agent_memory &&
    (value.versions.length > 0) === (value.runs_with_agent_memory > 0) &&
    new Set(value.versions.map((version) => `${version.memory_id}:${version.revision}`)).size === value.versions.length &&
    value.versions.every((version) => version.prepared_runs <= value.runs_with_agent_memory &&
      Date.parse(version.last_started_at) >= since && Date.parse(version.last_started_at) < until);
});

export const MemoryExecutionConfigSchema = z.object({
  runtime_id: z.string().uuid(),
  provider: z.string(),
  model: z.string().min(1),
  effort: z.string(),
  config_hash: z.string().regex(/^[a-f0-9]{64}$/),
  max_cases: z.number().int().min(2).max(8),
  timeout_seconds: z.number().int().positive().max(60),
  check_modes: z.array(z.enum(["exact", "json", "javascript"])).catch(["exact"]).default(["exact"]),
});

/**
 * Read shape of one workspace MCP server.
 *
 * This is the ONLY schema in this file that must not be `.loose()`. Everywhere
 * else, keeping unknown fields is forward-compatibility; here it would be a
 * hole in the write-only boundary — a server that regressed to returning the
 * stored entry (or a `url` / `headers` on the summary) would have it land in
 * the parsed object and in the query cache. zod strips unknown keys by
 * default, so the client only ever holds the safe summary.
 *
 * `transport` stays a plain string (not an enum) so an unknown value from a
 * newer backend still parses — the UI has a default branch for it.
 */
const McpToolRiskSchema = z.enum(["read", "internal_write", "external_effect", "sensitive_data", "unknown"]).catch("unknown");
const McpToolClassSchema = z.enum(["act_alone", "ask", "never"]).catch("ask");
export const McpCatalogToolSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  schema_digest: z.string().optional(),
  risk: McpToolRiskSchema,
  risk_source: z.enum(["auto", "manual"]).catch("auto"),
  class: McpToolClassSchema.optional(),
  last_used_at: z.string().optional(),
});
export const McpToolPolicySchema = z.object({
  default: z.enum(["by_risk", "ask", "never"]).optional().catch(undefined),
  tools: z.record(z.string(), McpToolClassSchema).optional().catch(undefined),
});
export const WorkspaceMcpServerSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  name: z.string().default(""),
  transport: z.string().default("unknown"),
  enabled: z.boolean().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  // K77: still strict — only the governed fields are let through, never the entry.
  tool_count: z.number().catch(0).default(0),
  tool_policy: McpToolPolicySchema.optional().catch(undefined),
  tools: z.array(McpCatalogToolSchema).optional().catch(undefined),
});
export const McpServerToolCatalogSchema = z.object({
  tools: z.array(McpCatalogToolSchema).catch([]).default([]),
  discovered_at: z.string().nullable().catch(null).default(null),
  risks: z.array(McpToolRiskSchema).catch([]).default([]),
}).loose();

export const WorkspaceMcpServerListSchema = z.array(WorkspaceMcpServerSchema);

export const EMPTY_WORKSPACE_MCP_SERVER: WorkspaceMcpServer = {
  id: "",
  workspace_id: "",
  name: "",
  transport: "unknown",
  tool_count: 0,
  created_at: "",
  updated_at: "",
};

// Share links. Introduced with the workspace share-link invite flow; schemas
// mirror the API responses so malformed payloads fall back to safe defaults.
export const ShareLinkSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  code: z.string(),
  created_by: z.string(),
  role: z.string(),
  expires_at: z.string().nullable().optional().default(null),
  max_uses: z.number().nullable().optional().default(null),
  use_count: z.number().optional().default(0),
  is_active: z.boolean().optional().default(true),
  created_at: z.string().optional().default(""),
  creator_name: z.string().optional().default(""),
  creator_email: z.string().optional().default(""),
}).loose();

export const EMPTY_SHARE_LINK: ShareLink = {
  id: "",
  workspace_id: "",
  code: "",
  created_by: "",
  role: "member",
  expires_at: null,
  max_uses: null,
  use_count: 0,
  is_active: false,
  created_at: "",
};

export const ShareLinkListResponseSchema = z.array(ShareLinkSchema).default([]);

export const ShareLinkInfoSchema = z.object({
  workspace_name: z.string().optional().default(""),
  workspace_slug: z.string().optional().default(""),
  creator_name: z.string().optional().default(""),
  role: z.string().optional().default("member"),
}).loose();

export const EMPTY_SHARE_LINK_INFO: ShareLinkInfo = {
  workspace_name: "",
  workspace_slug: "",
  role: "member",
};

export const MemberWithUserSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  user_id: z.string(),
  role: z.string(),
  created_at: z.string().optional().default(""),
  name: z.string().optional().default(""),
  email: z.string().optional().default(""),
  avatar_url: z.string().nullable().optional().default(null),
}).loose();

export const JoinShareLinkResponseSchema = z.object({
  member: MemberWithUserSchema,
  workspace_id: z.string(),
  workspace_slug: z.string().optional().default(""),
}).loose();

export const EMPTY_JOIN_SHARE_LINK_RESPONSE: {
  member: MemberWithUser;
  workspace_id: string;
  workspace_slug: string;
} = {
  member: {
    id: "",
    workspace_id: "",
    user_id: "",
    role: "member",
    created_at: "",
    name: "",
    email: "",
    avatar_url: null,
  },
  workspace_id: "",
  workspace_slug: "",
};

// Review cockpit (K16). Each section degrades on its own so one broken
// source never blanks the page; failed_sections names what the server could
// not load.
const ReviewCockpitRunSchema = z.object({
  id: z.string(),
  status: z.string().default(""),
  agent_id: z.string().default(""),
  created_at: z.string().default(""),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  error: z.string().nullable().default(null),
  failure_reason: z.string().optional(),
  handoff_note: z.string().optional(),
}).loose();

export const ReviewCockpitSchema = z.object({
  issue: IssueSchema,
  run: ReviewCockpitRunSchema.nullable().catch(null).default(null),
  runs: z.array(ReviewCockpitRunSchema).catch([]).default([]),
  merge_readiness: MergeReadinessSchema.nullable().catch(null).default(null),
  usage: z.object({
    input_tokens: z.number().default(0),
    output_tokens: z.number().default(0),
    cache_read_tokens: z.number().default(0),
    cache_write_tokens: z.number().default(0),
    cost_usd_ticks: z.number().nullable().default(null),
    uncosted: z.boolean().default(false),
  }).loose().nullable().catch(null).default(null),
  open_questions: z.array(IssueDecisionSchema).catch([]).default([]),
  criteria: z.array(AcceptanceCriterionSchema).catch([]).default([]),
  plan_verification: PlanVerificationSchema.nullable().catch(null).default(null),
  self_review: z.unknown().nullable().default(null),
  failed_sections: z.array(z.string()).catch([]).default([]),
}).loose();

// Cost per deliverable (K04).
const DeliverableCostStatsSchema = z.object({
  count: z.number().default(0),
  mean_usd_ticks: z.number().default(0),
  median_usd_ticks: z.number().default(0),
  total_usd_ticks: z.number().default(0),
  uncosted_count: z.number().default(0),
  trend_pct: z.number().nullable().catch(null).default(null),
}).loose();

export const DashboardCostPerDeliverableSchema = z.object({
  days: z.number().default(30),
  issues: DeliverableCostStatsSchema.catch({ count: 0, mean_usd_ticks: 0, median_usd_ticks: 0, total_usd_ticks: 0, uncosted_count: 0, trend_pct: null }),
  pull_requests: DeliverableCostStatsSchema.catch({ count: 0, mean_usd_ticks: 0, median_usd_ticks: 0, total_usd_ticks: 0, uncosted_count: 0, trend_pct: null }),
}).loose();

// ROI per agent (JEF-252). Ratios stay nullable all the way through: an agent
// that closed nothing has no cost per issue, and defaulting that to 0 would
// rank it as the cheapest agent in the workspace.
const AgentRoiRowSchema = z.object({
  agent_id: z.string().catch(""),
  agent_name: z.string().catch(""),
  provider: z.string().catch(""),
  issues_closed: z.number().catch(0),
  prs_merged: z.number().catch(0),
  cost_usd_ticks: z.number().catch(0),
  uncosted_runs: z.number().catch(0),
  cost_per_issue_usd_ticks: z.number().nullable().catch(null),
  cost_per_pr_usd_ticks: z.number().nullable().catch(null),
  prev_cost_per_issue_usd_ticks: z.number().nullable().catch(null),
}).loose();

export const DashboardAgentRoiSchema = z.object({
  days: z.number().catch(30),
  agents: z.array(AgentRoiRowSchema).catch([]).default([]),
}).loose();

// Module ownership (K33).
export const ModuleOwnershipRuleSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  path_pattern: z.string().nullable().default(null),
  label_id: z.string().nullable().default(null),
  owner_user_id: z.string().default(""),
  referent_agent_id: z.string().nullable().default(null),
  priority: z.number().int().default(0),
  created_at: z.string().default(""),
}).loose();

export const ModuleOwnershipRuleEnvelopeSchema = z.object({
  rule: ModuleOwnershipRuleSchema,
}).loose();

export const ModuleOwnershipListSchema = z.object({
  rules: z.array(ModuleOwnershipRuleSchema).catch([]).default([]),
}).loose();

export const OwnershipSuggestionSchema = z.object({
  rule_id: z.string().default(""),
  owner_user_id: z.string(),
  referent_agent_id: z.string().nullable().default(null),
  matched: z.string().default(""),
  pattern: z.string().default(""),
}).loose();

export const OwnershipSuggestionEnvelopeSchema = z.object({
  suggestion: OwnershipSuggestionSchema.nullable().catch(null).default(null),
}).loose();

// Morning briefing (K30). A section that fails to parse is empty, and an
// empty section is hidden by the view, so a broken line never blanks the day.
const BriefingItemSchema = z.object({
  issue_id: z.string(),
  identifier: z.string().default(""),
  title: z.string().default(""),
  status: z.string().default(""),
  reason: z.string().optional(),
  pending_decisions: z.number().int().optional(),
}).loose();

export const MorningBriefingSchema = z.object({
  date: z.string().default(""),
  merged: z.array(BriefingItemSchema).catch([]).default([]),
  awaiting_review: z.array(BriefingItemSchema).catch([]).default([]),
  blocked: z.array(BriefingItemSchema).catch([]).default([]),
  sent_at: z.string().nullable().catch(null).default(null),
  already_sent: z.boolean().optional(),
  narrative: z.string().catch("").default(""),
  channels_delivered: z.array(z.string()).catch([]).default([]),
}).loose();

// Scorecards (K25).
const ScorecardTotalsShape = {
  runs_total: z.number().default(0),
  runs_failed: z.number().default(0),
  runs_cancelled: z.number().default(0),
  runs_accepted: z.number().default(0),
  runs_reopened: z.number().default(0),
  runs_no_intervention: z.number().default(0),
  cost_usd_ticks_total: z.number().default(0),
  low_sample: z.boolean().default(true),
};
const ScorecardTotalsSchema = z.object(ScorecardTotalsShape).loose();

export const AgentScorecardSchema = z.object({
  agent_id: z.string().default(""),
  days: z.number().default(30),
  totals: ScorecardTotalsSchema.catch({ runs_total: 0, runs_failed: 0, runs_cancelled: 0, runs_accepted: 0, runs_reopened: 0, runs_no_intervention: 0, cost_usd_ticks_total: 0, low_sample: true }),
  previous: ScorecardTotalsSchema.catch({ runs_total: 0, runs_failed: 0, runs_cancelled: 0, runs_accepted: 0, runs_reopened: 0, runs_no_intervention: 0, cost_usd_ticks_total: 0, low_sample: true }),
  series: z.array(z.object({ ...ScorecardTotalsShape, day: z.string() }).loose()).catch([]).default([]),
}).loose();

export const WorkspaceScorecardsSchema = z.object({
  days: z.number().default(30),
  rows: z.array(z.object({ ...ScorecardTotalsShape, agent_id: z.string(), runtime_id: z.string().optional() }).loose()).catch([]).default([]),
}).loose();

// Agent versions (K23).
export const AgentVersionSchema = z.object({
  id: z.string(),
  agent_id: z.string().default(""),
  version_number: z.number().int().default(0),
  instructions: z.string().default(""),
  model: z.string().default(""),
  skill_ids: z.array(z.string()).catch([]).default([]),
  tool_config: z.record(z.string(), z.unknown()).catch({}).default({}),
  note: z.string().optional(),
  created_by_type: z.string().default("system"),
  created_by_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  active: z.boolean().default(false),
}).loose();

export const AgentVersionsSchema = z.object({
  versions: z.array(AgentVersionSchema).catch([]).default([]),
}).loose();

export const AgentVersionDiffSchema = z.object({
  from: AgentVersionSchema,
  to: AgentVersionSchema,
  changed_fields: z.array(z.string()).catch([]).default([]),
}).loose();

export const AgentVersionEnvelopeSchema = z.object({ version: AgentVersionSchema }).loose();

// Audit log (K08).
export const AuditLogEntrySchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  occurred_at: z.string().default(""),
  actor_type: z.string().default("system"),
  actor_id: z.string().nullable().default(null),
  action: z.string().default(""),
  entity_type: z.string().default(""),
  entity_id: z.string().nullable().default(null),
  model: z.string().nullable().default(null),
  cost_usd_ticks: z.number().nullable().default(null),
  approver_type: z.string().nullable().default(null),
  approver_id: z.string().nullable().default(null),
  details: z.record(z.string(), z.unknown()).catch({}).default({}),
  chain_seq: z.number().optional(),
  prev_hash: z.string().nullable().optional(),
  hash: z.string().optional(),
}).loose();

export const AuditChainStatusSchema = z.object({
  ok: z.boolean().default(false),
  total: z.number().default(0),
  head_hash: z.string().default(""),
  broken_seq: z.number().nullable().default(null),
  broken_id: z.string().nullable().default(null),
}).loose();

export const AuditLogPageSchema = z.object({
  entries: z.array(AuditLogEntrySchema).catch([]).default([]),
  next_cursor: z.string().catch("").default(""),
}).loose();

// Decision memory (K29).
export const DecisionRecordSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  project_id: z.string().nullable().default(null),
  issue_id: z.string().default(""),
  issue_identifier: z.string().optional(),
  issue_title: z.string().optional(),
  run_id: z.string().default(""),
  source_message_seq: z.number().default(0),
  title: z.string().default(""),
  context: z.string().default(""),
  decision: z.string().default(""),
  consequences: z.string().nullable().default(null),
  author_type: z.string().default("agent"),
  author_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
}).loose();

export const DecisionRecordListSchema = z.object({
  decisions: z.array(DecisionRecordSchema).catch([]).default([]),
}).loose();

export const ADRRequirementSchema = z.object({
  required: z.boolean().default(false),
  satisfied: z.boolean().default(true),
  files: z.number().default(0),
  file_threshold: z.number().default(0),
  migration: z.boolean().default(false),
  decisions: z.number().default(0),
  run_id: z.string().optional(),
}).loose();

// Business rules (K53).
export const BusinessRuleSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  title: z.string().default(""),
  natural_language: z.string().default(""),
  predicate: z.unknown(),
  description: z.string().default(""),
  attach_point: z.string().default("project_create"),
  action: z.object({ kind: z.string().default("dismiss"), priority: z.string().optional(), assignee_type: z.string().optional(), assignee_id: z.string().optional() }).loose().nullable().optional(),
  action_description: z.string().optional(),
  status: z.string().default("draft"),
  created_by: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const BusinessRuleListSchema = z.object({
  rules: z.array(BusinessRuleSchema).catch([]).default([]),
  attach_points: z.array(z.string()).catch([]).default([]),
}).loose();

export const BusinessRuleEnvelopeSchema = z.object({ rule: BusinessRuleSchema }).loose();

export const BusinessRuleDryRunSchema = z.object({
  rule: BusinessRuleSchema,
  checked: z.number().default(0),
  violations: z.array(z.object({
    subject_type: z.string().default(""),
    subject_id: z.string().default(""),
    label: z.string().default(""),
    detail: z.string().default(""),
  }).loose()).catch([]).default([]),
}).loose();

export const BusinessRuleViolationListSchema = z.object({
  violations: z.array(z.object({
    id: z.string(),
    rule_id: z.string().default(""),
    subject_type: z.string().default(""),
    subject_id: z.string().default(""),
    detail: z.string().nullable().default(null),
    created_at: z.string().default(""),
  }).loose()).catch([]).default([]),
}).loose();

// Standup and retro (K34).
export const WeeklyRetroSchema = z.object({
  week_start: z.string().default(""),
  week_end: z.string().default(""),
  runs_total: z.number().default(0),
  runs_by_status: z.record(z.string(), z.number()).catch({}).default({}),
  median_minutes: z.number().default(0),
  failed: z.array(z.object({
    run_id: z.string().default(""),
    issue_id: z.string().default(""),
    identifier: z.string().default(""),
    title: z.string().default(""),
    status: z.string().default(""),
    agent_id: z.string().default(""),
    minutes: z.number().default(0),
    error: z.string().optional(),
  }).loose()).catch([]).default([]),
  agents: z.array(z.object({
    agent_id: z.string().default(""),
    name: z.string().default(""),
    runs_total: z.number().default(0),
    runs_failed: z.number().default(0),
    runs_accepted: z.number().default(0),
    runs_reopened: z.number().default(0),
    runs_no_intervention: z.number().default(0),
    cost_usd_ticks: z.number().default(0),
  }).loose()).catch([]).default([]),
  skill_proposals: z.array(z.object({ text: z.string().default(""), source: z.string().default("") }).loose()).catch([]).default([]),
  narrative: z.string().catch("").default(""),
  generated_at: z.string().nullable().catch(null).default(null),
}).loose();

// Why search (K55).
export const WhySearchResponseSchema = z.object({
  results: z.array(z.object({
    id: z.string(),
    source_type: z.string().default("comment"),
    source_id: z.string().default(""),
    issue_id: z.string().nullable().default(null),
    issue_identifier: z.string().optional(),
    issue_title: z.string().optional(),
    snippet: z.string().default(""),
    score: z.number().default(0),
    created_at: z.string().default(""),
  }).loose()).catch([]).default([]),
  query: z.string().default(""),
}).loose();

// Trust Dial (K26).
export const TrustModeEnvelopeSchema = z.object({
  agent_id: z.string().default(""),
  mode: z.string().default("propose"),
  modes: z.array(z.string()).catch([]).default([]),
}).loose();

// "Show me first" (K69).
export const EffectModeEnvelopeSchema = z.object({
  agent_id: z.string().default(""),
  mode: z.enum(["apply", "preview"]).catch("apply").default("apply"),
}).loose();

export const TrustSuggestionSchema = z.object({
  eligible: z.boolean().default(false),
  current_mode: z.string().default("propose"),
  suggested_mode: z.string().optional(),
  metrics: z.object({
    days: z.number().default(30),
    runs_total: z.number().default(0),
    accepted_rate: z.number().default(0),
    no_intervention_rate: z.number().default(0),
    reopen_rate: z.number().default(0),
  }).loose().catch({ days: 30, runs_total: 0, accepted_rate: 0, no_intervention_rate: 0, reopen_rate: 0 }),
  thresholds: z.object({
    days: z.number().default(30),
    min_runs: z.number().default(10),
    min_accepted_rate: z.number().default(0.8),
    min_no_intervention_rate: z.number().default(0.7),
    max_reopen_rate: z.number().default(0.1),
  }).loose().catch({ days: 30, min_runs: 10, min_accepted_rate: 0.8, min_no_intervention_rate: 0.7, max_reopen_rate: 0.1 }),
  reasons: z.array(z.string()).catch([]).default([]),
}).loose();

export const TrustHistorySchema = z.object({
  changes: z.array(z.object({
    id: z.string(),
    from_mode: z.string().default(""),
    to_mode: z.string().default(""),
    reason: z.string().nullable().default(null),
    triggered_by_type: z.string().default("member"),
    triggered_by_id: z.string().nullable().default(null),
    created_at: z.string().default(""),
    demotion: z.boolean().default(false),
  }).loose()).catch([]).default([]),
}).loose();

// Triage auto-ML (K61).
export const TriageSuggestionsResponseSchema = z.object({
  suggestions: z.record(z.string(), z.object({
    item_id: z.string().default(""),
    ready: z.boolean().default(false),
    examples: z.number().default(0),
    min_examples: z.number().default(20),
    suggested: z.string().optional(),
    confidence: z.number().default(0),
    neighbors: z.array(z.object({ id: z.string(), title: z.string().default(""), state: z.string().default(""), score: z.number().default(0) }).loose()).catch([]).default([]),
  }).loose()).catch({}).default({}),
  auto: z.object({ enabled: z.boolean().default(false), threshold: z.number().default(0.9), min_examples: z.number().default(20) }).loose().catch({ enabled: false, threshold: 0.9, min_examples: 20 }),
}).loose();

// Blast radius (K07).
export const BlastRadiusRuleSchema = z.object({
  id: z.string(),
  project_id: z.string().default(""),
  path_pattern: z.string().default(""),
  autonomy_level: z.string().default("dual_approval"),
  specificity: z.number().default(0),
  created_by: z.string().default(""),
  created_at: z.string().default(""),
}).loose();

export const BlastRadiusRulesSchema = z.object({
  rules: z.array(BlastRadiusRuleSchema).catch([]).default([]),
  levels: z.array(z.string()).catch([]).default([]),
}).loose();

export const BlastRadiusRuleEnvelopeSchema = z.object({ rule: BlastRadiusRuleSchema }).loose();

export const BlastRadiusPreviewSchema = z.object({
  path: z.string().default(""),
  level: z.string().default("inherit"),
  rule_id: z.string().optional(),
  path_pattern: z.string().optional(),
}).loose();

// Sandbox policies (JEF-256). Deliberately strict on network_mode: a
// malformed mode must fail the parse (the client throws) rather than render
// a falsely permissive "unrestricted" policy — the daemon enforces
// fail-closed, the UI must not claim otherwise.
export const SandboxPolicySchema = z.object({
  network_mode: z.enum(["unrestricted", "allowlist", "none"]),
  allowed_hosts: z.array(z.string()).catch([]).default([]),
  block_sensitive_files: z.boolean().catch(false).default(false),
}).loose();

// GET/PUT /api/projects/:id/sandbox-policy.
export const ProjectSandboxPolicyResponseSchema = z.object({
  policy: SandboxPolicySchema.nullable(),
  effective: SandboxPolicySchema,
}).loose();

// GET/PUT /api/issues/:id/sandbox-override.
export const IssueSandboxOverrideResponseSchema = z.object({
  override: SandboxPolicySchema.nullable(),
  effective: SandboxPolicySchema,
}).loose();

// Permission profiles (K06).
export const PermissionProfileSchema = z.object({
  id: z.string().default(""),
  name: z.string().default(""),
  description: z.string().default(""),
  read_only: z.boolean().default(false),
  denied_paths: z.array(z.string()).catch([]).default([]),
  allowed_commands: z.array(z.string()).catch([]).default([]),
  hidden_secrets: z.array(z.string()).catch([]).default([]),
  builtin: z.boolean().default(false),
}).loose();

export const PermissionProfilesEnvelopeSchema = z.object({
  profiles: z.array(PermissionProfileSchema).catch([]).default([]),
}).loose();

export const AgentPermissionAssignmentSchema = z.object({
  id: z.string().default(""),
  permission_profile_id: z.string().nullable().default(null),
}).loose();

// Run-scoped secrets (K09): keys and status only, never a value.
export const RunSecretSchema = z.object({
  id: z.string().default(""),
  task_id: z.string().default(""),
  key: z.string().default(""),
  status: z.enum(["active", "revoked", "expired"]).catch("revoked").default("revoked"),
  expires_at: z.string().default(""),
  revoked_at: z.string().nullable().default(null),
  revoke_reason: z.string().nullable().default(null),
  created_at: z.string().default(""),
}); // strips unknown fields on purpose: a value must never reach the client

export const RunSecretsEnvelopeSchema = z.object({
  secrets: z.array(RunSecretSchema).catch([]).default([]),
}).loose();

// Runtime pools (K28).
export const RuntimePoolSchema = z.object({
  id: z.string().default(""),
  name: z.string().default(""),
  runtime_ids: z.array(z.string()).catch([]).default([]),
  degraded_runtime_id: z.string().nullable().default(null),
  agent_count: z.number().catch(0).default(0),
  created_at: z.string().default(""),
}).loose();

export const RuntimePoolsEnvelopeSchema = z.object({
  pools: z.array(RuntimePoolSchema).catch([]).default([]),
}).loose();

export const FailoverEntrySchema = z.object({
  from_runtime_id: z.string().default(""),
  to_runtime_id: z.string().default(""),
  reason: z.string().default(""),
  degraded: z.boolean().catch(false).default(false),
  at: z.string().default(""),
}).loose();

export const FailoverHistoryEnvelopeSchema = z.object({
  runs: z.array(z.object({
    task_id: z.string().default(""),
    status: z.string().default(""),
    degraded: z.boolean().catch(false).default(false),
    failure_reason: z.string().optional(),
    moves: z.array(FailoverEntrySchema).catch([]).default([]),
  }).loose()).catch([]).default([]),
}).loose();

export const AgentPoolAssignmentSchema = z.object({
  id: z.string().default(""),
  runtime_pool_id: z.string().nullable().default(null),
}).loose();

// Issue router (K27).
export const RoutingDecisionSchema = z.object({
  risk_level: z.enum(["low", "normal", "high"]).catch("normal").default("normal"),
  matched_paths: z.array(z.string()).catch([]).default([]),
  target_pool_id: z.string().optional(),
  target_pool_name: z.string().optional(),
  runtime_id: z.string().optional(),
  escalated: z.boolean().catch(false).default(false),
  escalation_reason: z.string().optional(),
  decided_at: z.string().default(""),
}).loose();

export const IssueRoutingEnvelopeSchema = z.object({
  decision: RoutingDecisionSchema.nullable().catch(null).default(null),
  task_id: z.string().nullable().catch(null).default(null),
  task_status: z.string().optional(),
}).loose();

export const RoutingSettingsSchema = z.object({
  enabled: z.boolean().catch(false).default(false),
  pools: z.record(z.string(), z.string()).catch({}).default({}),
  escalation_failures: z.number().int().catch(2).default(2),
}).loose();

// Meetings: recorded conversations transcribed segment by segment, then
// summarized into a markdown summary plus one pending triage item per action.
export const MeetingActionSchema = z.object({
  triage_item_id: z.string(),
  title: z.string().default(""),
  state: z.string().default("pending"),
  issue_id: z.string().optional(),
}).loose();

export const MeetingSchema = z.object({
  id: z.string(),
  title: z.string().default(""),
  app_name: z.string().default(""),
  status: z.string().default("done"),
  // The list endpoint omits transcripts; only the detail endpoint carries one.
  transcript: z.string().default(""),
  summary_markdown: z.string().default(""),
  segment_count: z.number().default(0),
  created_by: z.string().default(""),
  started_at: z.string().default(""),
  ended_at: z.string().optional(),
  actions: z.array(MeetingActionSchema).catch([]).default([]),
  // Filled by the list endpoint (which omits `actions`); 0 on the detail endpoint.
  action_count: z.number().catch(0).default(0),
  summary_unavailable: z.boolean().default(false),
  // Absent on an older backend: default to false so the UI hides the
  // destructive affordances rather than offering ones the server will refuse.
  can_manage: z.boolean().catch(false).default(false),
}).loose();

export const RealtimeVoiceSessionSchema = z.object({
  url: z.string().default(""),
  model: z.string().default(""),
  token: z.string().default(""),
  expires_at: z.string().default(""),
  encoding: z.string().default("pcm_s16le"),
  sample_rate: z.number().catch(16000).default(16000),
}).loose();

export const EMPTY_REALTIME_VOICE_SESSION: RealtimeVoiceSession = Object.freeze({
  url: "",
  model: "",
  token: "",
  expires_at: "",
  encoding: "pcm_s16le",
  sample_rate: 16000,
});

export const VoiceTranscriptionSchema = z.object({
  text: z.string().default(""),
}).loose();

export const EMPTY_VOICE_TRANSCRIPTION: VoiceTranscription = Object.freeze({ text: "" });

export const MeetingListResponseSchema = z.object({
  meetings: z.array(MeetingSchema).default([]),
}).loose();

export const MeetingSegmentResponseSchema = z.object({
  seq: z.string().default(""),
  text: z.string().default(""),
  segment_count: z.number().default(0),
}).loose();

export const EMPTY_MEETING: Meeting = Object.freeze({
  id: "",
  title: "",
  app_name: "",
  status: "failed",
  transcript: "",
  summary_markdown: "",
  segment_count: 0,
  created_by: "",
  started_at: "",
  actions: [],
  action_count: 0,
  summary_unavailable: false,
  can_manage: false,
}) as Meeting;

export const EMPTY_MEETING_LIST: MeetingListResponse = Object.freeze({
  meetings: [],
}) as MeetingListResponse;

// Calendar subscription (ICS). Times stay strings: they are ISO instants the
// UI formats, and a malformed one must not fail the whole parse.
export const CalendarEventSchema = z.object({
  summary: z.string().default(""),
  url: z.string().optional(),
  start: z.string().default(""),
  end: z.string().default(""),
  in_progress: z.boolean().catch(false).default(false),
}).loose();

export const CalendarUpcomingSchema = z.object({
  events: z.array(CalendarEventSchema).catch([]).default([]),
  configured: z.boolean().catch(false).default(false),
}).loose();

export const CalendarFeedSchema = z.object({
  url: z.string().default(""),
  last_fetched_at: z.string().optional(),
  last_error: z.string().default(""),
}).loose();

export const EMPTY_CALENDAR_UPCOMING: CalendarUpcoming = Object.freeze({
  events: [],
  configured: false,
}) as CalendarUpcoming;

export const EMPTY_CALENDAR_FEED: CalendarFeed = Object.freeze({
  url: "",
  last_error: "",
}) as CalendarFeed;

export const EMPTY_MEETING_SEGMENT: MeetingSegmentResponse = Object.freeze({
  seq: "",
  text: "",
  segment_count: 0,
}) as MeetingSegmentResponse;

// Handoff packets (K17).
export const HandoffPacketSchema = z.object({
  id: z.string().default(""),
  run_id: z.string().default(""),
  issue_id: z.string().default(""),
  objective: z.string().default(""),
  decisions: z.array(z.string()).catch([]).default([]),
  evidence: z.array(z.string()).catch([]).default([]),
  failed_attempts: z.array(z.string()).catch([]).default([]),
  next_action: z.string().catch("").default(""),
  created_by_type: z.enum(["agent", "member", "system"]).catch("system").default("system"),
  created_by_id: z.string().nullable().catch(null).default(null),
  created_at: z.string().default(""),
}).loose();

export const HandoffPacketsEnvelopeSchema = z.object({
  packets: z.array(HandoffPacketSchema).catch([]).default([]),
}).loose();

export const LatestHandoffPacketEnvelopeSchema = z.object({
  packet: HandoffPacketSchema.nullable().catch(null).default(null),
}).loose();

// Run limits (K03).
export const RunLimitPolicySchema = z.object({
  id: z.string().default(""),
  scope_type: z.enum(["workspace", "project", "agent"]).catch("workspace").default("workspace"),
  scope_id: z.string().nullable().catch(null).default(null),
  max_cost_usd_ticks: z.number().nullable().catch(null).default(null),
  max_duration_seconds: z.number().nullable().catch(null).default(null),
  max_turns: z.number().nullable().catch(null).default(null),
  max_tool_calls: z.number().nullable().catch(null).default(null),
  warn_bps: z.number().catch(8000).default(8000),
  action: z.enum(["observe", "enforce"]).catch("enforce").default("enforce"),
  created_at: z.string().default(""),
}).loose();

export const RunLimitPoliciesEnvelopeSchema = z.object({
  policies: z.array(RunLimitPolicySchema).catch([]).default([]),
}).loose();

export const RunLimitEventSchema = z.object({
  task_id: z.string().default(""),
  gate: z.enum(["cost", "duration", "turns", "tool_calls"]).catch("cost").default("cost"),
  level: z.enum(["warn", "exceeded", "stopped"]).catch("warn").default("warn"),
  observed: z.number().catch(0).default(0),
  limit: z.number().catch(0).default(0),
  policy_id: z.string().default(""),
  created_at: z.string().default(""),
}).loose();

export const RunLimitEventsEnvelopeSchema = z.object({
  events: z.array(RunLimitEventSchema).catch([]).default([]),
}).loose();

// Pause, steer, resume (K19).
export const RunControlStateSchema = z.object({
  task_id: z.string().default(""),
  status: z.string().default(""),
  pause_pending: z.boolean().catch(false).default(false),
  instructions: z.array(z.string()).catch([]).default([]),
  resumed_by_task_id: z.string().nullable().catch(null).default(null),
}).loose();

export const RunControlEnvelopeSchema = z.object({
  run: RunControlStateSchema.nullable().catch(null).default(null),
  paused_task_id: z.string().optional(),
}).loose();

// Checkpoints (K20).
export const RunCheckpointStatusSchema = z.object({
  task_id: z.string().default(""),
  status: z.string().default(""),
  failure_reason: z.string().catch("").default(""),
  last_checkpoint_seq: z.number().nullable().catch(null).default(null),
  checkpointed_at: z.string().nullable().catch(null).default(null),
  attempts: z.number().catch(0).default(0),
  max_attempts: z.number().catch(3).default(3),
  resumed_from_task_id: z.string().nullable().catch(null).default(null),
  exhausted: z.boolean().catch(false).default(false),
}).loose();

export const RunCheckpointEnvelopeSchema = z.object({
  run: RunCheckpointStatusSchema.nullable().catch(null).default(null),
}).loose();

// Traffic control (K18).
export const TrafficConflictSchema = z.object({
  id: z.string().default(""),
  task_id: z.string().default(""),
  kind: z.enum(["human", "agent"]).catch("agent").default("agent"),
  paths: z.array(z.string()).catch([]).default([]),
  other_task_id: z.string().nullable().catch(null).default(null),
  handoff_packet_id: z.string().nullable().catch(null).default(null),
  status: z.enum(["active", "ignored", "resolved"]).catch("resolved").default("resolved"),
  created_at: z.string().default(""),
  resolved_at: z.string().nullable().catch(null).default(null),
}).loose();

export const TrafficConflictsEnvelopeSchema = z.object({
  conflicts: z.array(TrafficConflictSchema).catch([]).default([]),
}).loose();

// Drift detection (K40).
export const DriftPolicySchema = z.object({
  enabled: z.boolean().catch(true).default(true),
  repeated_action_threshold: z.number().int().catch(5).default(5),
  file_reread_threshold: z.number().int().catch(8).default(8),
}).loose();

// Preemption (K41).
export const PreemptionSchema = z.object({
  task_id: z.string().default(""),
  status: z.string().default(""),
  preempted_at: z.string().default(""),
  preempted_by_task_id: z.string().default(""),
  preempted_by_issue_id: z.string().nullable().catch(null).default(null),
  preempted_by_identifier: z.string().nullable().catch(null).default(null),
  resumed_by_task_id: z.string().nullable().catch(null).default(null),
}).loose();

export const PreemptionsEnvelopeSchema = z.object({
  preemptions: z.array(PreemptionSchema).catch([]).default([]),
}).loose();

// Pipelines (K37).
export const PipelineStageSchema = z.object({
  id: z.string().default(""),
  position: z.number().int().catch(0).default(0),
  name: z.string().default(""),
  executor_type: z.enum(["agent", "squad"]).catch("agent").default("agent"),
  executor_id: z.string().default(""),
  requires_human_gate: z.boolean().catch(false).default(false),
}).loose();

export const PipelineSchema = z.object({
  id: z.string().default(""),
  name: z.string().default(""),
  stages: z.array(PipelineStageSchema).catch([]).default([]),
  open_runs: z.number().catch(0).default(0),
  created_at: z.string().default(""),
}).loose();

export const PipelinesEnvelopeSchema = z.object({
  pipelines: z.array(PipelineSchema).catch([]).default([]),
}).loose();

export const PipelineRunSchema = z.object({
  id: z.string().default(""),
  pipeline_id: z.string().default(""),
  pipeline_name: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.enum(["active", "paused", "completed", "cancelled"]).catch("active").default("active"),
  current_stage_id: z.string().nullable().catch(null).default(null),
  current_index: z.number().int().catch(-1).default(-1),
  gate_decision_id: z.string().nullable().catch(null).default(null),
  last_error: z.string().nullable().catch(null).default(null),
  stages: z.array(PipelineStageSchema).catch([]).default([]),
  started_at: z.string().default(""),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

export const PipelineRunEnvelopeSchema = z.object({
  run: PipelineRunSchema.nullable().catch(null).default(null),
}).loose();

// Fan-out / fan-in (K38).
export const FanoutMemberSchema = z.object({
  id: z.string().default(""),
  child_issue_id: z.string().default(""),
  task_id: z.string().default(""),
  task_status: z.string().default(""),
  assignee_agent_id: z.string().default(""),
  description: z.string().default(""),
  outcome: z.enum(["completed", "failed"]).nullable().catch(null).default(null),
  settled_at: z.string().nullable().catch(null).default(null),
}).loose();

export const FanoutBatchSchema = z.object({
  id: z.string().default(""),
  parent_issue_id: z.string().default(""),
  leader_agent_id: z.string().default(""),
  status: z.enum(["pending", "partial_failure", "complete"]).catch("pending").default("pending"),
  expected_count: z.number().int().catch(0).default(0),
  completed_count: z.number().int().catch(0).default(0),
  failed_count: z.number().int().catch(0).default(0),
  synthesis_task_id: z.string().nullable().catch(null).default(null),
  members: z.array(FanoutMemberSchema).catch([]).default([]),
  created_at: z.string().default(""),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

export const FanoutEnvelopeSchema = z.object({
  batch: FanoutBatchSchema.nullable().catch(null).default(null),
}).loose();

// Agent duel (K39).
export const AgentDuelSideSchema = z.object({
  agent_id: z.string().default(""),
  task_id: z.string().default(""),
  task_status: z.string().default(""),
  outcome: z.enum(["completed", "failed"]).nullable().catch(null).default(null),
  cost_usd_ticks: z.number().catch(0).default(0),
  duration_seconds: z.number().catch(0).default(0),
  tool_calls: z.number().int().catch(0).default(0),
  quality_score: z.number().nullable().catch(null).default(null),
  summary: z.string().catch("").default(""),
}).loose();

const emptyDuelSide = AgentDuelSideSchema.parse({});

export const AgentDuelSchema = z.object({
  id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.enum(["running", "verdict_ready", "confirmed", "inconclusive"]).catch("running").default("running"),
  a: AgentDuelSideSchema.catch(emptyDuelSide).default(emptyDuelSide),
  b: AgentDuelSideSchema.catch(emptyDuelSide).default(emptyDuelSide),
  arbiter_winner: z.enum(["a", "b", "tie"]).nullable().catch(null).default(null),
  reasoning: z.string().catch("").default(""),
  arbiter_error: z.string().nullable().catch(null).default(null),
  winner: z.enum(["a", "b", "tie"]).nullable().catch(null).default(null),
  confirmed_by: z.string().nullable().catch(null).default(null),
  confirmed_at: z.string().nullable().catch(null).default(null),
  created_at: z.string().default(""),
  settled_at: z.string().nullable().catch(null).default(null),
}).loose();

export const AgentDuelEnvelopeSchema = z.object({
  duel: AgentDuelSchema.nullable().catch(null).default(null),
}).loose();

// Racing attempts (F11 / JEF-6): N attempts on one issue, the human keeps one.
// diff_unified is null both when nothing was recorded and when the patch was
// too large to store — diff_truncated is what tells those apart, so the UI can
// say "too large, read the branch" instead of "no changes".
// runtime_id / runtime_name (JEF-234) are empty strings when the attempt ran
// on the agent's own binding; cost_usd_ticks is 0 while unreported and
// duration_seconds is 0 while the attempt is still running.
export const RunGroupAttemptSchema = z.object({
  task_id: z.string().default(""),
  agent_id: z.string().default(""),
  status: z.string().catch("").default(""),
  model: z.string().catch("").default(""),
  runtime_id: z.string().catch("").default(""),
  runtime_name: z.string().catch("").default(""),
  cost_usd_ticks: z.number().catch(0).default(0),
  duration_seconds: z.number().catch(0).default(0),
  diff_stat: z.unknown().nullable().catch(null).default(null),
  diff_unified: z.string().nullable().catch(null).default(null),
  diff_truncated: z.boolean().catch(false).default(false),
  created_at: z.string().default(""),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

// LLM judge (JEF-234): null until a human asks for a verdict, then either
// "answered" with a winner and per-attempt scores, or "failed" when the judge
// model could not decide. It never settles the race — keeping one attempt
// stays a human decision. winner_task_id is an empty string (not null) when
// the judge failed, and cost_usd_ticks is null while unreported.
export const RunGroupJudgementScoreSchema = z.object({
  task_id: z.string().catch("").default(""),
  score: z.number().catch(0).default(0),
  rationale: z.string().catch("").default(""),
}).loose();

export const RunGroupJudgementSchema = z.object({
  status: z.enum(["answered", "failed"]).catch("failed").default("failed"),
  winner_task_id: z.string().catch("").default(""),
  justification: z.string().catch("").default(""),
  scores: z.array(RunGroupJudgementScoreSchema).catch([]).default([]),
  model: z.string().catch("").default(""),
  judged_at: z.string().catch("").default(""),
  cost_usd_ticks: z.number().nullable().catch(null).default(null),
}).loose();

export const RunGroupSchema = z.object({
  id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.enum(["running", "settled", "abandoned"]).catch("running").default("running"),
  attempt_count: z.number().int().catch(0).default(0),
  winner_task_id: z.string().nullable().catch(null).default(null),
  created_by: z.string().nullable().catch(null).default(null),
  created_at: z.string().default(""),
  settled_at: z.string().nullable().catch(null).default(null),
  attempts: z.array(RunGroupAttemptSchema).catch([]).default([]),
  judgement: RunGroupJudgementSchema.nullable().catch(null).default(null),
}).loose();

export const RunGroupEnvelopeSchema = z.object({
  group: RunGroupSchema.nullable().catch(null).default(null),
}).loose();

export const RunGroupListEnvelopeSchema = z.object({
  groups: z.array(RunGroupSchema).catch([]).default([]),
}).loose();

export type RunGroup = z.infer<typeof RunGroupSchema>;
export type RunGroupAttempt = z.infer<typeof RunGroupAttemptSchema>;
export type RunGroupJudgement = z.infer<typeof RunGroupJudgementSchema>;
export type RunGroupJudgementScore = z.infer<typeof RunGroupJudgementScoreSchema>;

/** Body of POST /api/issues/:id/run-groups. The server caps attempts at 5. */
export interface StartRunGroupInput {
  attempts: Array<{ agent_id: string; model?: string; runtime_id?: string }>;
  note?: string;
}

// Refactoring campaigns (K42).
export const CampaignBlockerSchema = z.object({
  kind: z.string().default(""),
  label: z.string().default(""),
  count: z.number().optional(),
  pr_number: z.number().optional(),
}).loose();

export const CampaignShardSchema = z.object({
  id: z.string().default(""),
  child_issue_id: z.string().default(""),
  task_id: z.string().default(""),
  task_status: z.string().default(""),
  run_outcome: z.enum(["completed", "failed"]).nullable().catch(null).default(null),
  assignee_agent_id: z.string().default(""),
  description: z.string().default(""),
  branch_name: z.string().default(""),
  merge_position: z.number().int().catch(0).default(0),
  merge_status: z.enum(["pending", "rebasing", "ready", "merged", "conflict", "skipped"]).catch("pending").default("pending"),
  merge_task_id: z.string().nullable().catch(null).default(null),
  blockers: z.array(CampaignBlockerSchema).catch([]).default([]),
  updated_at: z.string().default(""),
}).loose();

export const RefactorCampaignSchema = z.object({
  id: z.string().default(""),
  issue_id: z.string().default(""),
  fanout_batch_id: z.string().default(""),
  name: z.string().default(""),
  target_branch: z.string().default(""),
  status: z.enum(["running", "merging", "completed", "failed"]).catch("running").default("running"),
  shards: z.array(CampaignShardSchema).catch([]).default([]),
  created_at: z.string().default(""),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

export const RefactorCampaignEnvelopeSchema = z.object({
  campaign: RefactorCampaignSchema.nullable().catch(null).default(null),
}).loose();

// Learned competency (K43).
export const CompetencyRowSchema = z.object({
  agent_id: z.string().default(""),
  agent_name: z.string().catch("").default(""),
  domain_key: z.string().default(""),
  success_count: z.number().int().catch(0).default(0),
  total_count: z.number().int().catch(0).default(0),
  duel_wins: z.number().int().catch(0).default(0),
  duel_losses: z.number().int().catch(0).default(0),
  sample_size: z.number().int().catch(0).default(0),
  score: z.number().catch(0).default(0),
  reliable: z.boolean().catch(false).default(false),
  updated_at: z.string().catch("").default(""),
}).loose();

export const AgentCompetencySchema = z.object({
  agent_id: z.string().default(""),
  min_sample: z.number().int().catch(5).default(5),
  rows: z.array(CompetencyRowSchema).catch([]).default([]),
}).loose();

// Cross-provider self-review (K15).
export const CrossReviewChecklistResultSchema = z.object({
  item: z.string().catch("").default(""),
  pass: z.boolean().catch(false).default(false),
  note: z.string().catch("").default(""),
}).loose();

export const CrossReviewReportSchema = z.object({
  verdict: z.enum(["approve", "request_changes", "comment"]).catch("comment").default("comment"),
  risks: z.array(z.string()).catch([]).default([]),
  questions: z.array(z.string()).catch([]).default([]),
  suggestions: z.array(z.string()).catch([]).default([]),
  summary: z.string().catch("").default(""),
  // Per-item verdicts against the project's review checklist (JEF-238).
  // Optional: absent on reports produced before the checklist existed.
  checklist_results: z.array(CrossReviewChecklistResultSchema).optional().catch(undefined),
}).loose();

export const CrossReviewSchema = z.object({
  task_id: z.string().default(""),
  review_of_task_id: z.string().default(""),
  reviewer_agent_id: z.string().default(""),
  reviewer_name: z.string().catch("").default(""),
  reviewer_provider: z.string().catch("").default(""),
  status: z.string().default(""),
  report: CrossReviewReportSchema.nullable().catch(null).default(null),
  created_at: z.string().default(""),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

// CI auto-fix (K49).
export const CIAutoFixRunSchema = z.object({
  id: z.string().default(""),
  provider: z.string().default(""),
  pull_request_id: z.string().default(""),
  head_sha: z.string().default(""),
  issue_id: z.string().default(""),
  task_id: z.string().nullable().catch(null).default(null),
  task_status: z.string().catch("").default(""),
  attempt: z.number().int().catch(0).default(0),
  budget_usd_ticks: z.number().catch(0).default(0),
  manual: z.boolean().catch(false).default(false),
  created_at: z.string().default(""),
}).loose();

export const IssueCIAutoFixSchema = z.object({
  runs: z.array(CIAutoFixRunSchema).catch([]).default([]),
  enabled: z.boolean().catch(false).default(false),
  max_attempts: z.number().int().catch(3).default(3),
}).loose();

export const CIAutoFixRetryEnvelopeSchema = z.object({
  run: CIAutoFixRunSchema.nullable().catch(null).default(null),
}).loose();

export const CIAutoFixSettingsSchema = z.object({
  enabled: z.boolean().catch(false).default(false),
  max_attempts: z.number().int().catch(3).default(3),
  budget_usd_ticks: z.number().catch(0).default(0),
}).loose();

export const CrossReviewSettingsSchema = z.object({
  enabled: z.boolean().catch(true).default(true),
  opt_out_project_ids: z.array(z.string()).catch([]).default([]),
}).loose();

// Confidence review (JEF-240): the workspace gate that routes low-confidence
// runs to human review. The 0 < threshold ≤ 1 bound is enforced server-side;
// a malformed payload falls back to the product defaults rather than
// breaking the settings screen.
export const ConfidenceReviewSettingsSchema = z.object({
  enabled: z.boolean().catch(true).default(true),
  threshold: z.number().catch(0.5).default(0.5),
  // Cascade escalations (JEF-272): how many times a below-threshold run may
  // be re-dispatched to a stronger runtime. Server-side contract is an
  // integer in [0, 3]; anything else falls back to the product default.
  max_escalations: z.number().int().min(0).max(3).catch(2).default(2),
}).loose();

export const CrossReviewListSchema = z.object({
  reviews: z.array(CrossReviewSchema).catch([]).default([]),
}).loose();

// Undo for agent actions (K69).
export const AgentEffectSchema = z.object({
  id: z.string(),
  task_id: z.string().catch("").default(""),
  agent_id: z.string().catch("").default(""),
  agent_name: z.string().catch("").default(""),
  issue_id: z.string().nullable().catch(null).default(null),
  kind: z.string().catch("").default(""),
  target_type: z.string().catch("").default(""),
  target_id: z.string().catch("").default(""),
  before: z.record(z.string(), z.unknown()).catch({}).default({}),
  after: z.record(z.string(), z.unknown()).catch({}).default({}),
  reversible: z.boolean().catch(false).default(false),
  status: z.string().catch("applied").default("applied"),
  decision_id: z.string().nullable().catch(null).default(null),
  payload: z.record(z.string(), z.unknown()).catch({}).default({}),
  reversed_at: z.string().nullable().catch(null).default(null),
  reversed_by_type: z.string().nullable().catch(null).default(null),
  reverse_error: z.string().nullable().catch(null).default(null),
  within_window: z.boolean().catch(false).default(false),
  expires_at: z.string().catch("").default(""),
  created_at: z.string().catch("").default(""),
}).loose();

export const AgentEffectListSchema = z.object({
  effects: z.array(AgentEffectSchema).catch([]).default([]),
  window_hours: z.number().int().catch(24).default(24),
}).loose();

export const UndoReportSchema = z.object({
  reversed: z.number().int().catch(0).default(0),
  skipped: z.array(z.object({ id: z.string().catch("").default(""), kind: z.string().catch("").default(""), reason: z.string().catch("").default("") }).loose()).catch([]).default([]),
  breaker: z.object({ tripped: z.boolean().catch(false).default(false), trust_mode: z.string().catch("").default("") }).loose().catch({ tripped: false, trust_mode: "" }).default({ tripped: false, trust_mode: "" }),
  effects: z.array(AgentEffectSchema).catch([]).default([]),
}).loose();

export const UndoSettingsSchema = z.object({
  window_hours: z.number().int().catch(24).default(24),
  breaker_threshold: z.number().int().catch(5).default(5),
}).loose();

// Per-project review configuration (JEF-238): the checklist a reviewer agent
// checks each diff against, an optional fixed reviewer, the done-gate, and the
// rework-cycle cap. GET always answers 200 with these defaults when the
// project has no saved config.
export const ProjectReviewConfigSchema = z.object({
  project_id: z.string().default(""),
  checklist: z.array(z.string()).catch([]).default([]),
  reviewer_agent_id: z.string().nullable().catch(null).default(null),
  gate_enabled: z.boolean().catch(false).default(false),
  max_cycles: z.number().int().catch(3).default(3),
}).loose();

export const CompetencySettingsSchema = z.object({
  min_sample: z.number().int().catch(5).default(5),
}).loose();

// Workflow execution safety (JEF-275).
export const RoutingProblemSchema = z.object({
  code: z.string().catch("").default(""),
  message: z.string().catch("").default(""),
  fatal: z.boolean().catch(false).default(false),
}).loose();

export const RoutingCheckSchema = z.object({
  agent_id: z.string().catch("").default(""),
  ok: z.boolean().catch(true).default(true),
  fatal: z.boolean().catch(false).default(false),
  problems: z.array(RoutingProblemSchema).catch([]).default([]),
}).loose();

export const WorkflowLimitsSchema = z.object({
  max_legs: z.number().int().catch(8).default(8),
  max_cost_usd_ticks: z.number().int().catch(0).default(0),
  min_legs: z.number().int().catch(1).default(1),
  max_legs_allowed: z.number().int().catch(50).default(50),
}).loose();

// Vigil as an MCP server (OS plan, chantier 1). A workspace admin's saved
// settings, plus the tool catalogue and endpoint returned alongside them so
// the settings page never has to fetch two endpoints to render one form.
export const MCPToolDecisionSchema = z.enum(["allow", "ask", "deny"]);

export const MCPServerSettingsSchema = z.object({
  enabled: z.boolean().catch(true).default(true),
  default_surface: z.enum(["compound", "granular"]).catch("compound").default("compound"),
  // A single malformed override degrades the whole map to "no overrides"
  // rather than keeping the others — the safe read is the caller's own
  // ceiling, never a partially-trusted tightening.
  tools: z.record(z.string(), MCPToolDecisionSchema).catch({}).default({}),
}).loose();

export const MCPServerCatalogToolSchema = z.object({
  name: z.string().default(""),
  group: z.string().default(""),
  action: z.string().default(""),
  risk: z.string().catch("unknown").default("unknown"),
  description: z.string().default(""),
  agent_only: z.boolean().catch(false).default(false),
}).loose();

export const MCPServerSettingsEnvelopeSchema = z.object({
  settings: MCPServerSettingsSchema,
  tools: z.array(MCPServerCatalogToolSchema).catch([]).default([]),
  endpoint: z.string().catch("").default(""),
}).loose();

// Data residency (K46). Declared in packages/core/residency/schemas.ts and
// re-exported here so the API client imports every response schema from one
// module, like WorkflowLimitsSchema above.
export { DataResidencyPolicySchema } from "../residency/schemas";

export const AssigneeSuggestionSchema = z.object({
  domain_key: z.string().default(""),
  min_sample: z.number().int().catch(5).default(5),
  candidates: z.array(CompetencyRowSchema).catch([]).default([]),
  ownership: OwnershipSuggestionSchema.nullable().catch(null).default(null),
}).loose();

// What-if estimate (K44). Every measurement is nullable: the server sends
// null rather than a guess below min_sample, and a drifted backend that
// sends something else must degrade to "no estimate", never to a number.
const EstimateNumberSchema = z.number().nullable().catch(null).default(null);

export const IssueEstimateCandidateSchema = z.object({
  agent_id: z.string().catch("").default(""),
  agent_name: z.string().catch("").default(""),
  sample_size: z.number().catch(0).default(0),
  insufficient_history: z.boolean().catch(true).default(true),
  median_cost_usd_ticks: EstimateNumberSchema,
  cost_range_low_usd_ticks: EstimateNumberSchema,
  cost_range_high_usd_ticks: EstimateNumberSchema,
  median_duration_seconds: EstimateNumberSchema,
  duration_range_low_seconds: EstimateNumberSchema,
  duration_range_high_seconds: EstimateNumberSchema,
  exceeds_budget: z.boolean().catch(false).default(false),
}).loose();

export const IssueEstimateSchema = z.object({
  domain_key: z.string().catch("").default(""),
  min_sample: z.number().int().catch(5).default(5),
  candidates: z.array(IssueEstimateCandidateSchema).catch([]).default([]),
}).loose();

// Run replay (K70): one hash-chained event stream per run.
export const ReplayEventSchema = z.object({
  seq: z.number().catch(0).default(0),
  at: z.string().catch("").default(""),
  kind: z.string().catch("unknown").default("unknown"),
  actor: z.object({
    type: z.string().catch("").default(""),
    id: z.string().catch("").default(""),
    name: z.string().catch("").default(""),
  }).loose().catch({ type: "", id: "", name: "" }).default({ type: "", id: "", name: "" }),
  title: z.string().catch("").default(""),
  text: z.string().catch("").default(""),
  data: z.record(z.string(), z.unknown()).catch({}).default({}),
  data_class: z.string().catch("internal").default("internal"),
  in_plan: z.boolean().nullable().catch(null).default(null),
  source: z.string().catch("").default(""),
  source_id: z.string().catch("").default(""),
  prev_hash: z.string().catch("").default(""),
  hash: z.string().catch("").default(""),
}).loose();

export const RunReplaySchema = z.object({
  run: z.object({
    id: z.string().default(""),
    safe_mode: z.boolean().catch(false).default(false),
    snapshot: z.object({
      trust_mode: z.string().catch("").default(""),
      effect_mode: z.string().catch("").default(""),
      model: z.string().catch("").default(""),
      thinking_level: z.string().catch("").default(""),
      permission_profile_id: z.string().catch("").default(""),
      runtime_id: z.string().catch("").default(""),
      safe_mode: z.boolean().catch(false).default(false),
      plan_version: z.number().catch(0).default(0),
      recorded_at: z.string().catch("").default(""),
    }).loose().nullable().catch(null).default(null),
    plan: z.object({ version: z.number().catch(0).default(0), steps: z.number().catch(0).default(0) }).loose().nullable().catch(null).default(null),
    drift: z.number().catch(0).default(0),
    issue_id: z.string().catch("").default(""),
    agent_id: z.string().catch("").default(""),
    agent_name: z.string().catch("").default(""),
    status: z.string().catch("").default(""),
    trust_mode: z.string().catch("").default(""),
    effect_mode: z.string().catch("").default(""),
    model: z.string().catch("").default(""),
    created_at: z.string().nullable().catch(null).default(null),
    started_at: z.string().nullable().catch(null).default(null),
    completed_at: z.string().nullable().catch(null).default(null),
    links: z.array(z.object({
      relation: z.string().default(""),
      task_id: z.string().default(""),
      agent_id: z.string().catch("").default(""),
      agent_name: z.string().catch("").default(""),
    }).loose()).catch([]).default([]),
  }).loose(),
  events: z.array(ReplayEventSchema).catch([]).default([]),
  total: z.number().catch(0).default(0),
  next_cursor: z.number().nullable().catch(null).default(null),
  head_hash: z.string().catch("").default(""),
  cost: z.object({
    input_tokens: z.number().catch(0).default(0),
    output_tokens: z.number().catch(0).default(0),
    cost_usd_ticks: z.number().nullable().catch(null).default(null),
  }).loose().catch({ input_tokens: 0, output_tokens: 0, cost_usd_ticks: null }).default({ input_tokens: 0, output_tokens: 0, cost_usd_ticks: null }),
  sealed: z.object({
    events: z.number().catch(0).default(0),
    head_hash: z.string().catch("").default(""),
    sealed_at: z.string().catch("").default(""),
    verified: z.boolean().catch(false).default(false),
  }).loose().nullable().catch(null).default(null),
}).loose();

export const ReplayResumeResultSchema = z.object({
  task_id: z.string().default(""),
  from_seq: z.number().catch(0).default(0),
}).loose();

export const ReplaySimulateResultSchema = z.object({
  task_id: z.string().default(""),
  safe_mode: z.boolean().catch(true).default(true),
}).loose();

// Per-leg accounting (JEF-274). Every field is defaulted: a leg with a
// malformed figure still belongs to the workflow, and a workflow that lost one
// leg's cost is better than a panel that renders nothing. `legs: []` collapses
// a malformed array so the caller sees "no legs" and hides the summary.
export const WorkflowLegSchema = z.object({
  task_id: z.string().catch("").default(""),
  leg_role: z.string().catch("").default(""),
  status: z.string().catch("").default(""),
  agent_id: z.string().catch("").default(""),
  agent_name: z.string().catch("").default(""),
  runtime_id: z.string().catch("").default(""),
  runtime_name: z.string().catch("").default(""),
  provider: z.string().catch("").default(""),
  model: z.string().catch("").default(""),
  input_tokens: z.number().catch(0).default(0),
  output_tokens: z.number().catch(0).default(0),
  cost_usd_ticks: z.number().catch(0).default(0),
  duration_seconds: z.number().catch(0).default(0),
  created_at: z.string().nullable().catch(null).default(null),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

export const WorkflowLegsSchema = z.object({
  root_task_id: z.string().catch("").default(""),
  legs: z.array(WorkflowLegSchema).catch([]).default([]),
  totals: z.object({
    legs: z.number().catch(0).default(0),
    cost_usd_ticks: z.number().catch(0).default(0),
    input_tokens: z.number().catch(0).default(0),
    output_tokens: z.number().catch(0).default(0),
    duration_seconds: z.number().catch(0).default(0),
  }).loose().catch({ legs: 0, cost_usd_ticks: 0, input_tokens: 0, output_tokens: 0, duration_seconds: 0 })
    .default({ legs: 0, cost_usd_ticks: 0, input_tokens: 0, output_tokens: 0, duration_seconds: 0 }),
}).loose();

// Task watchdog (K73).
export const WatchdogSchema = z.object({
  id: z.string().default(""),
  issue_id: z.string().catch("").default(""),
  agent_id: z.string().catch("").default(""),
  agent_name: z.string().catch("").default(""),
  owner_id: z.string().catch("").default(""),
  instructions: z.string().catch("").default(""),
  rest_minutes: z.number().catch(30).default(30),
  enabled: z.boolean().catch(true).default(true),
  last_scan_task_id: z.string().nullable().catch(null).default(null),
  last_scanned_at: z.string().nullable().catch(null).default(null),
  motion_streak: z.number().catch(0).default(0),
  created_at: z.string().catch("").default(""),
}).loose();

export const WatchdogEnvelopeSchema = z.object({ watchdog: WatchdogSchema.nullable().catch(null).default(null) }).loose();

export const WatchdogFindingSchema = z.object({
  issue: z.string().catch("").default(""),
  issue_id: z.string().catch("").default(""),
  action: z.string().catch("none").default("none"),
  reason: z.string().catch("").default(""),
  missing_criterion: z.string().catch("").default(""),
}).loose();

export const WatchdogVerdictSchema = z.object({
  id: z.string().default(""),
  watchdog_id: z.string().catch("").default(""),
  issue_id: z.string().catch("").default(""),
  task_id: z.string().catch("").default(""),
  verdict: z.enum(["legitimate", "motion", "escalate"]).catch("escalate").default("escalate"),
  summary: z.string().catch("").default(""),
  findings: z.array(WatchdogFindingSchema).catch([]).default([]),
  dropped: z.array(WatchdogFindingSchema).catch([]).default([]),
  applied: z.record(z.string(), z.unknown()).catch({}).default({}),
  decision_id: z.string().nullable().catch(null).default(null),
  human_review: z.enum(["pending", "confirmed", "overturned"]).catch("pending").default("pending"),
  contract_revision: z.number().catch(0).default(0),
  created_at: z.string().catch("").default(""),
}).loose();

export const WatchdogVerdictListSchema = z.object({ verdicts: z.array(WatchdogVerdictSchema).catch([]).default([]) }).loose();

export const WatchdogScanResultSchema = z.object({ task_id: z.string().default("") }).loose();
export const WatchdogVerdictEnvelopeSchema = z.object({ verdict: WatchdogVerdictSchema }).loose();

// Goal loop: an agent works one issue toward a stated goal across bounded
// continuations. Named IssueGoal* to stay clear of the unrelated K74
// workspace-mission GoalSchema above.
const IssueGoalQuestionSchema = z.object({
  kind: z.enum(["text", "choice"]).catch("text").default("text"),
  prompt: z.string().catch("").default(""),
  options: z.array(z.string()).optional().catch(undefined),
  run_id: z.string().catch("").default(""),
  asked_at: z.string().catch("").default(""),
  answer: z.string().optional().catch(undefined),
  answered_by: z.string().optional().catch(undefined),
  answered_by_name: z.string().optional().catch(undefined),
  answered_at: z.string().optional().catch(undefined),
}).loose();

export const IssueGoalSchema = z.object({
  id: z.string().catch("").default(""),
  issue_id: z.string().catch("").default(""),
  goal: z.string().catch("").default(""),
  status: z.enum(["active", "paused", "waiting_user", "satisfied", "stopped"]).catch("active").default("active"),
  continuation: z.number().catch(0).default(0),
  max_continuations: z.number().catch(1).default(1),
  no_progress: z.number().catch(0).default(0),
  last_outcome: z.string().catch("").default(""),
  last_blocker: z.string().optional().catch(undefined),
  last_reason: z.string().optional().catch(undefined),
  next_step: z.string().optional().catch(undefined),
  evidence: z.array(z.string()).catch([]).default([]),
  question: IssueGoalQuestionSchema.optional().catch(undefined),
  last_run_id: z.string().optional().catch(undefined),
  chain_root_task_id: z.string().optional().catch(undefined),
  done_request_id: z.string().optional().catch(undefined),
  set_by_type: z.enum(["member", "agent", "system"]).catch("system").default("system"),
  updated_at: z.string().catch("").default(""),
}).loose();

export const IssueGoalEnvelopeSchema = z.object({ goal: IssueGoalSchema.nullable().catch(null).default(null) }).loose();

// Vigil learns you (K71).
export const WorkProfileObservationSchema = z.object({
  id: z.string().default(""),
  key: z.string().catch("").default(""),
  kind: z.string().catch("").default(""),
  value: z.record(z.string(), z.unknown()).catch({}).default({}),
  source: z.string().catch("").default(""),
  count: z.number().catch(0).default(0),
  corrections: z.number().catch(0).default(0),
  auto: z.boolean().catch(false).default(false),
  state: z.enum(["learned", "proposed"]).catch("learned").default("learned"),
  stake: z.string().catch("normal").default("normal"),
  first_observed_at: z.string().catch("").default(""),
  last_observed_at: z.string().catch("").default(""),
}).loose();

export const WorkProfileSchema = z.object({
  observations: z.array(WorkProfileObservationSchema).catch([]).default([]),
  examples: z.number().catch(0).default(0),
  auto_decided: z.number().catch(0).default(0),
  overturned: z.number().catch(0).default(0),
  review_load_seconds: z.number().catch(0).default(0),
  adaptation_surface: z.array(z.string()).catch([]).default([]),
}).loose();

// Goals with ancestry (K74).
const GoalStatusSchema = z.enum(["draft", "active", "done", "dropped"]).catch("draft");
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
  issue_count: z.number().catch(0).default(0),
  done_count: z.number().catch(0).default(0),
  project_ids: z.array(z.string()).catch([]).default([]),
}).loose();
export const ListGoalsResponseSchema = z.object({
  goals: z.array(GoalSchema).catch([]).default([]),
  total: z.number().catch(0).default(0),
}).loose();
export const GoalDetailResponseSchema = z.object({
  goal: GoalSchema,
  issues: z.array(IssueSchema).catch([]).default([]),
}).loose();
export const ProjectGoalsResponseSchema = z.object({
  goal_ids: z.array(z.string()).catch([]).default([]),
}).loose();

// Dated cycles (F29). Lenient like every other boundary schema: an unknown
// status or load unit still parses, and the UI's switches carry a default.
const CycleCapacitySideSchema = z.object({
  capacity: z.number().nullable().catch(null).default(null),
  load: z.number().catch(0).default(0),
}).loose();
export const CycleSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().catch(""),
  name: z.string().catch(""),
  description: z.string().catch("").default(""),
  start_date: z.string().catch(""),
  end_date: z.string().catch(""),
  rollover: z.boolean().catch(true).default(true),
  closed_at: z.string().nullable().catch(null).default(null),
  status: z.enum(["upcoming", "active", "closed"]).catch("active").default("active"),
  late: z.boolean().catch(false).default(false),
  load_unit: z.enum(["issues", "property"]).catch("issues").default("issues"),
  load_property_id: z.string().nullable().catch(null).default(null),
  issue_count: z.number().catch(0).default(0),
  done_count: z.number().catch(0).default(0),
  capacity: z.object({
    human: CycleCapacitySideSchema,
    agent: CycleCapacitySideSchema,
    unassigned_load: z.number().catch(0).default(0),
  }).loose().catch({
    human: { capacity: null, load: 0 },
    agent: { capacity: null, load: 0 },
    unassigned_load: 0,
  }),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();
export const ListCyclesResponseSchema = z.object({
  cycles: z.array(CycleSchema).catch([]).default([]),
  total: z.number().catch(0).default(0),
}).loose();
export const CycleBurndownSchema = z.object({
  days: z.array(z.object({
    date: z.string().catch(""),
    remaining_count: z.number().nullable().catch(null).default(null),
    remaining_load: z.number().nullable().catch(null).default(null),
    ideal_count: z.number().catch(0).default(0),
    ideal_load: z.number().catch(0).default(0),
    human_load: z.number().nullable().catch(null).default(null),
    agent_load: z.number().nullable().catch(null).default(null),
  }).loose()).catch([]).default([]),
  capacity: z.object({
    human: z.number().nullable().catch(null).default(null),
    agent: z.number().nullable().catch(null).default(null),
  }).loose().catch({ human: null, agent: null }),
  load_unit: z.enum(["issues", "property"]).catch("issues").default("issues"),
  load_property_id: z.string().nullable().catch(null).default(null),
  approximate_before: z.string().nullable().catch(null).default(null),
}).loose();
export const GoalProgressSchema = z.object({
  goal_id: z.string().catch(""),
  projects: z.array(z.object({
    project_id: z.string().catch(""),
    name: z.string().catch(""),
    total_count: z.number().catch(0).default(0),
    done_count: z.number().catch(0).default(0),
  }).loose()).catch([]).default([]),
  total_count: z.number().catch(0).default(0),
  done_count: z.number().catch(0).default(0),
}).loose();

// Contest (K72): a rival model's objections, the author's answers, the human verdict.
const ContestObjectionSchema = z.object({
  n: z.number().catch(0).default(0),
  severity: z.enum(["high", "medium", "low"]).catch("medium"),
  kind: z.enum(["missing", "false", "risky"]).catch("risky"),
  claim: z.string().catch("").default(""),
  evidence: z.string().catch("").default(""),
  expected_proof: z.string().catch("").default(""),
}).loose();
const ContestAnswerSchema = z.object({
  n: z.number().catch(0).default(0),
  verdict: z.enum(["accept", "refute", "fix"]).catch("accept"),
  note: z.string().catch("").default(""),
  proof: z.string().catch("").default(""),
}).loose();
export const ContestSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().nullable().catch(null).default(null),
  issue_id: z.string().nullable().catch(null).default(null),
  target_type: z.enum(["task_result", "plan", "triage_verdict", "meeting_summary"]).catch("task_result"),
  target_id: z.string().catch(""),
  target_excerpt: z.string().catch("").default(""),
  author_agent_id: z.string().nullable().catch(null).default(null),
  author_provider: z.string().catch("").default(""),
  challenger_kind: z.enum(["agent", "llm"]).catch("agent"),
  challenger_agent_id: z.string().nullable().catch(null).default(null),
  challenger_provider: z.string().catch("").default(""),
  same_vendor: z.boolean().catch(false).default(false),
  challenger_task_id: z.string().nullable().catch(null).default(null),
  answer_task_id: z.string().nullable().catch(null).default(null),
  round: z.number().catch(1).default(1),
  max_rounds: z.number().catch(1).default(1),
  objections: z.array(ContestObjectionSchema).catch([]).default([]),
  answers: z.array(ContestAnswerSchema).catch([]).default([]),
  nothing_to_contest: z.string().catch("").default(""),
  status: z.enum(["running", "objections_ready", "answering", "answered", "confirmed", "failed"]).catch("running"),
  human_verdict: z.enum(["upheld", "dismissed", "mixed"]).nullable().catch(null).default(null),
  verdict_note: z.string().catch("").default(""),
  confirmed_by: z.string().nullable().catch(null).default(null),
  confirmed_at: z.string().nullable().catch(null).default(null),
  auto: z.boolean().catch(false).default(false),
  created_by: z.string().nullable().catch(null).default(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();
export const ContestListSchema = z.object({
  contests: z.array(ContestSchema).catch([]).default([]),
}).loose();
export const ContestPreflightSchema = z.object({
  target_type: z.string().catch(""),
  target_id: z.string().catch(""),
  issue_id: z.string().nullable().catch(null).default(null),
  author_agent_id: z.string().nullable().catch(null).default(null),
  author_provider: z.string().catch("").default(""),
  challenger: z.object({
    kind: z.enum(["agent", "llm"]).catch("agent"),
    agent_id: z.string().catch("").default(""),
    name: z.string().catch("").default(""),
    provider: z.string().catch("").default(""),
    same_vendor: z.boolean().catch(false).default(false),
  }).loose(),
  estimated_cost_usd_ticks: z.number().catch(0).default(0),
  quota_used: z.number().catch(0).default(0),
  quota_limit: z.number().catch(0).default(0),
  max_rounds: z.number().catch(2).default(2),
  existing: z.number().catch(0).default(0),
}).loose();
export const ContestSettingsSchema = z.object({
  targets: z.record(z.string(), z.boolean()).catch({}).default({}),
  opt_out_project_ids: z.array(z.string()).catch([]).default([]),
}).loose();

// Executable org chart (K75).
const OrgMemberSchema = z.object({ type: z.enum(["member", "agent"]).catch("member"), id: z.string().catch(""), role: z.string().optional(), role_id: z.string().optional() }).loose();
const OrgRoleSchema = z.object({ id: z.string().catch(""), name: z.string().catch(""), responsibilities: z.string().optional(), keywords: z.array(z.string()).optional() }).loose();
const OrgUnitSchema = z.object({
  mission: z.string().optional(),
  id: z.string().catch(""),
  name: z.string().catch(""),
  kind: z.string().optional(),
  model: z.enum(["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"]).optional().catch(undefined),
  owner_id: z.string().optional(),
  squad_id: z.string().optional(),
  mission_goal_id: z.string().optional(),
  budget_usd_ticks: z.number().optional(),
  excludes: z.array(z.enum(["untrusted_input", "sensitive_data", "external_effects"])).catch([]).default([]),
  autonomy: z.enum(["read_only", "draft", "approve_payload", "auto"]).catch("draft"),
  allow: z.array(z.string()).catch([]).default([]),
  deny: z.array(z.string()).catch([]).default([]),
  escalation_quota_per_day: z.number().catch(5).default(5),
  human_approval: z.boolean().optional(),
  approval_risk: z.string().optional(),
  deciders: z.record(z.string(), z.string()).optional(),
  members: z.array(OrgMemberSchema).catch([]).default([]),
  roles: z.array(OrgRoleSchema).catch([]).default([]),
}).loose();
export const OrgDefinitionSchema = z.object({
  units: z.array(OrgUnitSchema).catch([]).default([]),
  edges: z.array(z.object({ from: z.string(), to: z.string(), kind: z.enum(["reports_to", "backs_up", "escalates_to", "consults"]).catch("reports_to"), human_approval: z.boolean().optional() }).loose()).catch([]).default([]),
  rules: z.array(z.object({ id: z.string().catch(""), labels: z.array(z.string()).optional(), paths: z.array(z.string()).optional(), keywords: z.array(z.string()).optional(), target_unit: z.string().catch(""), priority: z.number().catch(0).default(0) }).loose()).catch([]).default([]),
  committees: z.array(z.object({ decision_type: z.string(), unit_ids: z.array(z.string()).default([]), quorum: z.number().catch(1), max_rounds: z.number().catch(1) }).loose()).catch([]).default([]),
  market: z.object({ price_cap_usd_ticks: z.number().catch(0).default(0), offers_per_agent_per_day: z.number().catch(5).default(5), min_offers: z.number().catch(2).default(2) }).loose().catch({ price_cap_usd_ticks: 0, offers_per_agent_per_day: 5, min_offers: 2 }),
}).loose();
export const OrgStructureSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().nullable().catch(null).default(null),
  model: z.enum(["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"]).catch("owner_network"),
  name: z.string().catch(""),
  status: z.enum(["draft", "active", "paused", "dissolved"]).catch("draft"),
  revision: z.number().catch(1).default(1),
  revision_id: z.string().nullable().catch(null).default(null),
  definition: OrgDefinitionSchema.catch({ units: [], edges: [], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 5, min_offers: 2 } }),
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
export const OrgStructureListSchema = z.object({ structures: z.array(OrgStructureSchema).catch([]).default([]) }).loose();
export const OrgStructureDetailSchema = z.object({
  structure: OrgStructureSchema,
  revisions: z.array(z.object({ id: z.string(), revision: z.number().catch(0), model: z.string().catch(""), status: z.string().catch(""), definition: OrgDefinitionSchema.optional().catch(undefined), note: z.string().catch(""), changed_by: z.string().nullable().catch(null).default(null), created_at: z.string().catch("") }).loose()).catch([]).default([]),
}).loose();
export const OrgTemplateListSchema = z.object({
  templates: z.array(z.object({ model: z.string(), composite: z.boolean().optional().catch(undefined), name: z.string().catch(""), pattern: z.string().catch(""), description: z.string().catch(""), coordination_runs_per_issue: z.number().catch(0), definition: OrgDefinitionSchema }).loose()).catch([]).default([]),
}).loose();
export const OrgHealthSchema = z.object({
  structure_id: z.string().catch(""),
  window_days: z.number().catch(7),
  routed: z.number().catch(0),
  unrouted: z.number().catch(0),
  escalations: z.number().catch(0),
  stacked_escalations: z.number().catch(0),
  reassigned_outside: z.number().catch(0),
  market_short: z.number().catch(0),
  breakers: z.number().catch(0),
  human_review_items: z.number().catch(0),
  drift_rate: z.number().catch(0),
  units: z.array(z.object({ unit_id: z.string(), name: z.string().catch(""), routed: z.number().catch(0), escalations: z.number().catch(0), reassigned_outside: z.number().catch(0), vacant_roles: z.array(z.string()).catch([]), saturated_agents: z.array(z.string()).catch([]), paused: z.boolean().catch(false), spend_usd_ticks: z.number().catch(0), budget_usd_ticks: z.number().catch(0), human_review_items: z.number().catch(0) }).loose()).catch([]).default([]),
  proposals: z.array(z.object({ key: z.string(), unit_id: z.string().optional(), title: z.string().catch(""), body: z.string().catch(""), measure: z.string().catch("") }).loose()).catch([]).default([]),
}).loose();
export const OrgPreflightSchema = z.object({
  model: z.string().catch(""), pattern: z.string().catch(""), coordination_runs_per_issue: z.number().catch(0), coordination_cost_usd_ticks_per_issue: z.number().catch(0),
  human_review_items_per_issue: z.number().catch(0), human_review_seconds_per_issue: z.number().catch(0), units: z.number().catch(0), units_without_owner: z.number().catch(0), agents: z.number().catch(0),
  activation_requirements: z.array(z.string()).catch([]).default([]),
}).loose();
export const OrgOfferListSchema = z.object({
  offers: z.array(z.object({ id: z.string(), agent_id: z.string().catch(""), agent_name: z.string().catch(""), confidence: z.number().catch(0), cost_usd_ticks: z.number().catch(0), eta_hours: z.number().catch(0), status: z.enum(["pending", "won", "lost", "over_cap"]).catch("pending"), created_at: z.string().catch("") }).loose()).catch([]).default([]),
}).loose();
export const OrgResolveSchema = z.object({ structure: OrgStructureSchema.nullable().catch(null) }).loose();
const OrgSimulationRefSchema = z.object({ unit_id: z.string().catch(""), unit_name: z.string().catch("") }).loose();
const OrgSimulationActorSchema = z.object({ kind: z.enum(["agent", "member", "squad", "none"]).catch("none"), id: z.string().catch(""), name: z.string().catch("") }).loose();
// A simulation is shown as an answer, not merged into a list, so the three
// fields that carry its meaning stay required: a payload missing them is
// rejected outright (client.ts throws) rather than rendered as a confident
// "nobody prepares this, against no basis".
export const OrgSimulationSchema = z.object({
  basis: z.enum(["draft", "revision"]),
  structure_id: z.string().catch(""),
  revision: z.number().catch(0),
  unit: z.object({ id: z.string().catch(""), name: z.string().catch(""), model: z.string().catch(""), autonomy: z.string().catch("") }).loose().nullable().catch(null).default(null),
  receives: OrgSimulationRefSchema.nullable().catch(null).default(null),
  prepares: OrgSimulationActorSchema,
  decides: OrgSimulationActorSchema,
  escalation_path: z.array(OrgSimulationRefSchema).catch([]).default([]),
  blocking_denies: z.array(z.string()).catch([]).default([]),
  cost_estimate_usd_ticks: z.number().catch(0),
  notes: z.array(z.string()).catch([]).default([]),
}).loose();
export const IssueEnvelopeSchema = z.object({ issue: IssueSchema.nullable().catch(null) }).loose();

// Workspace export / import (K76).
const TransferSecretSchema = z.object({ scope: z.string().catch(""), name: z.string().catch(""), key: z.string().catch(""), scoped: z.boolean().catch(false) }).loose();
const TransferCollisionSchema = z.object({ kind: z.string().catch(""), name: z.string().catch(""), existing_id: z.string().catch("") }).loose();
const TransferCountsSchema = z.record(z.string(), z.number()).catch({}).default({});
export const TransferManifestSchema = z.object({
  format_version: z.number().catch(0),
  exported_at: z.string().catch(""),
  name: z.string().catch(""),
  template: z.boolean().catch(false),
  source: z.object({ Name: z.string().catch(""), Slug: z.string().catch("") }).loose().catch({ Name: "", Slug: "" }),
  counts: TransferCountsSchema,
  secrets: z.array(TransferSecretSchema).catch([]).default([]),
}).loose();
export const TransferPreviewSchema = z.object({
  manifest: TransferManifestSchema,
  collisions: z.array(TransferCollisionSchema).catch([]).default([]),
  secrets: z.array(TransferSecretSchema).catch([]).default([]),
  strategies: z.array(z.enum(["rename", "merge", "skip"])).catch(["rename", "merge", "skip"]).default(["rename", "merge", "skip"]),
}).loose();
export const TransferReportSchema = z.object({
  created: TransferCountsSchema,
  merged: TransferCountsSchema,
  skipped: z.array(TransferCollisionSchema).catch([]).default([]),
  secrets_pending: z.array(TransferSecretSchema).catch([]).default([]),
  warnings: z.array(z.string()).catch([]).default([]),
}).loose();
export const TransferImportResultSchema = z.object({ run_id: z.string().catch(""), report: TransferReportSchema }).loose();
export const TransferRunListSchema = z.object({
  runs: z.array(z.object({
    id: z.string(),
    direction: z.enum(["export", "import"]).catch("export"),
    status: z.enum(["running", "completed", "failed"]).catch("failed"),
    name: z.string().catch(""),
    template: z.boolean().catch(false),
    strategy: z.string().catch(""),
    source_name: z.string().catch(""),
    bundle_sha256: z.string().catch(""),
    report: z.record(z.string(), z.unknown()).catch({}).default({}),
    created_by: z.string().nullable().catch(null),
    created_at: z.string().catch(""),
    completed_at: z.string().nullable().catch(null),
  }).loose()).catch([]).default([]),
}).loose();
export const WorkspaceTemplateListSchema = z.object({
  templates: z.array(z.object({
    id: z.string(),
    name: z.string().catch(""),
    source_name: z.string().catch(""),
    workspace_name: z.string().catch(""),
    report: z.record(z.string(), z.unknown()).catch({}).default({}),
    created_at: z.string().catch(""),
  }).loose()).catch([]).default([]),
}).loose();

// ---------------------------------------------------------------------------
// Multiplayer chat participants (K31 / JEF-181)
// ---------------------------------------------------------------------------

export const ChatParticipantSchema = z.object({
  user_id: z.string(),
  name: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  // Server-driven enum: an unknown role degrades to the least-privileged one
  // so a future value can never grant a remove button by accident.
  role: z.enum(["owner", "participant"]).catch("participant"),
  joined_at: z.string().default(""),
  online: z.boolean().default(false),
}).loose();

export const ChatParticipantListSchema = z.object({
  participants: z.array(ChatParticipantSchema).catch([]).default([]),
}).loose();

export const EMPTY_CHAT_PARTICIPANT_LIST: ChatParticipantList = { participants: [] };

// ---------------------------------------------------------------------------
// Run transcript: task messages + the issue changes the run made (F03 / JEF-11)
// ---------------------------------------------------------------------------

export const TaskMessageSchema = z.object({
  task_id: z.string().default(""),
  issue_id: z.string().default(""),
  chat_session_id: z.string().optional(),
  seq: z.number().default(0),
  // Never a closed enum. The server writes eight types today and validates none
  // of them on ingest, so an installed build meets values it predates on every
  // backend upgrade. Coercing an unknown type to "text" would silently relabel
  // a future kind as agent prose; keeping it raw lets the presenter render it
  // as a neutral note that names itself.
  type: z.string().default("text"),
  tool: z.string().optional(),
  content: z.string().optional(),
  input: z.record(z.string(), z.unknown()).optional(),
  output: z.string().optional(),
  created_at: z.string().optional(),
}).loose();

export const RunActionSchema = z.object({
  kind: z.literal("action").catch("action"),
  action: z.string().default(""),
  // Either side may legitimately be empty — an issue created by a run has no
  // "before". The UI renders the missing half as a dash rather than dropping
  // the entry.
  before: z.string().default(""),
  after: z.string().default(""),
  at: z.string().default(""),
}).loose();

/**
 * `GET /api/tasks/:id/messages`.
 *
 * Two shapes are accepted on purpose. A server that predates F03 returns the
 * bare message array, and installed desktop builds outlive their backend in
 * both directions — so the old shape is normalised into the new one with an
 * empty action list rather than failing the whole transcript. This is the
 * one boundary where the tolerance is worth its weight: the alternative is a
 * blank transcript on every mismatched pair.
 */
export const TaskActivityResponseSchema = z.union([
  z.object({
    messages: z.array(TaskMessageSchema).catch([]).default([]),
    // Actions degrade independently: a malformed action list must not hide the
    // transcript it accompanies.
    actions: z.array(RunActionSchema).catch([]).default([]),
  }).loose(),
  z.array(TaskMessageSchema).transform((messages) => ({ messages, actions: [] })),
]);

export const EMPTY_TASK_ACTIVITY: TaskActivityResponse = { messages: [], actions: [] };
// Custom runtime profiles (MUL-3284). `protocol_family` is left as a bare
// string (not the closed union) so an unrecognized family from a newer
// server still parses instead of falling back to EMPTY_RUNTIME_PROFILE.
export const RuntimeProfileSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  display_name: z.string().catch(""),
  protocol_family: z.string().catch(""),
  command_name: z.string().catch(""),
  description: z.string().nullable().catch(null),
  fixed_args: z.array(z.string()).catch([]).default([]),
  visibility: z.enum(["workspace", "private"]).catch("workspace"),
  created_by: z.string().nullable().catch(null),
  enabled: z.boolean().catch(false),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const EMPTY_RUNTIME_PROFILE: RuntimeProfile = {
  id: "",
  workspace_id: "",
  display_name: "",
  protocol_family: "claude",
  command_name: "",
  description: null,
  fixed_args: [],
  visibility: "workspace",
  created_by: null,
  enabled: false,
  created_at: "",
  updated_at: "",
};

export const RuntimeProfileListSchema = z.object({
  runtime_profiles: z.array(RuntimeProfileSchema).catch([]).default([]),
}).loose();

// ---------------------------------------------------------------------------
// JEF-321 batch A — auth, issue writes, comments/reactions, agents, runtimes
// ---------------------------------------------------------------------------

// POST /auth/verify-code, POST /auth/google. No EMPTY_ fallback: an empty
// token would look like a successful login with no way to detect failure, so
// client.ts parses to null and throws — caught by the existing login-page
// try/catch, same path as any other login error.
export const LoginResponseSchema = z.object({
  token: z.string(),
  user: UserSchema,
}).loose();

// Standalone issue-level reaction (POST /api/issues/:id/reactions). Mirrors
// ReactionSchema's comment-level shape; issue_revision is additive so a
// caller on an older backend still gets a usable reaction.
export const IssueReactionSchema = z.object({
  id: z.string(),
  issue_id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  emoji: z.string(),
  created_at: z.string(),
  issue_revision: z.number().int().positive().optional(),
}).loose();

export const EMPTY_ISSUE_REACTION: IssueReaction = {
  id: "",
  issue_id: "",
  actor_type: "",
  actor_id: "",
  emoji: "",
  created_at: "",
};

export const AssigneeFrequencyEntrySchema = z.object({
  assignee_type: z.string(),
  assignee_id: z.string(),
  frequency: z.number().catch(0),
}).loose();

export const AssigneeFrequencyListSchema = z.array(AssigneeFrequencyEntrySchema);

const AgentConversationStarterSchema = z.object({
  label: z.string(),
  prompt: z.string(),
}).loose();

const AgentSkillSummarySchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  enabled: z.boolean().optional(),
}).loose();

const AgentInvocationTargetSchema = z.object({
  target_type: z.string(),
  target_id: z.string().nullable(),
}).loose();

// Agent (GET/POST/PUT /api/agents...). Kept lenient the same way IssueSchema
// is: enum-shaped fields stay z.string()/z.enum().catch(...) so an unknown
// value from a newer backend degrades instead of failing the whole object,
// and nested arrays default to [] so a malformed skill/invocation-target list
// doesn't blank the agent that carries it.
export const AgentSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  runtime_id: z.string().catch(""),
  runtime_bound: z.boolean().optional(),
  runtime_availability: z.enum(["online", "unstable", "offline"]).optional().catch(undefined),
  name: z.string().catch(""),
  description: z.string().catch(""),
  instructions: z.string().catch(""),
  conversation_starters: z.array(AgentConversationStarterSchema).optional().catch(undefined),
  system_key: z.string().optional(),
  system_instructions: z.string().optional(),
  avatar_url: z.string().nullable().catch(null),
  runtime_mode: z.string().catch("local"),
  runtime_config: z.record(z.string(), z.unknown()).catch({}).default({}),
  custom_args: z.array(z.string()).catch([]).default([]),
  has_custom_env: z.boolean().optional(),
  custom_env_key_count: z.number().optional(),
  mcp_config: z.unknown().nullish(),
  mcp_config_redacted: z.boolean().optional(),
  composio_toolkit_allowlist: z.array(z.string()).optional().catch(undefined),
  composio_toolkit_allowlist_redacted: z.boolean().optional(),
  visibility: z.enum(["workspace", "private"]).catch("private"),
  permission_mode: z.enum(["private", "public_to"]).catch("private"),
  invocation_targets: z.array(AgentInvocationTargetSchema).catch([]).default([]),
  status: z.enum(["idle", "working", "blocked", "error", "offline"]).catch("offline"),
  max_concurrent_tasks: z.number().catch(1),
  trust_mode: z.string().optional(),
  effect_mode: z.string().optional(),
  permission_profile_id: z.string().nullable().optional(),
  runtime_pool_id: z.string().nullable().optional(),
  model: z.string().catch(""),
  thinking_level: z.string().optional(),
  service_tier: z.string().optional(),
  runtime_routing: z.string().optional(),
  owner_id: z.string().nullable().catch(null),
  skills: z.array(AgentSkillSummarySchema).catch([]).default([]),
  disabled_runtime_skills: z.array(z.unknown()).optional().catch(undefined),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  archived_at: z.string().nullable().catch(null),
  archived_by: z.string().nullable().catch(null),
}).loose();

export const EMPTY_AGENT: Agent = {
  id: "",
  workspace_id: "",
  runtime_id: "",
  name: "",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "private",
  permission_mode: "private",
  invocation_targets: [],
  status: "offline",
  max_concurrent_tasks: 0,
  model: "",
  owner_id: null,
  skills: [],
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
};

export const AgentListSchema = z.array(AgentSchema);

// POST /api/agents/mika — the workspace's Mika plus its onboarding session,
// resolved together server-side. `onboarding_session` is validated loosely
// (not required) because the caller (bootstrapMika) already throws its own
// "session was not returned" error when it is absent.
export const MikaBootstrapResponseSchema = AgentSchema.extend({
  onboarding_session: ChatSessionSchema.optional(),
});

// GET/PUT /api/agents/:id/env. Deliberately no EMPTY_ fallback: a malformed
// response here must not present as "this agent has no custom env" (env-tab.tsx
// throws through its try/catch instead, same as a network failure).
export const AgentEnvResponseSchema = z.object({
  agent_id: z.string(),
  custom_env: z.record(z.string(), z.string()).catch({}).default({}),
  scoped_keys: z.array(z.string()).optional().catch(undefined),
}).loose();

const SandboxCapabilitiesSchema = z.object({
  os: z.string().optional(),
  docker: z.boolean().optional(),
  docker_version: z.string().optional(),
  bwrap: z.boolean().optional(),
  modes: z.array(z.string()).optional(),
}).loose();

// GET /api/runtimes. RuntimeDevice's optional K10/K46 fields (sandbox_*,
// compliance) stay lenient — additive metadata a malformed value must not
// take the whole runtime down with it.
export const AgentRuntimeSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  daemon_id: z.string().nullable().catch(null),
  name: z.string().catch(""),
  custom_name: z.string().nullable().optional(),
  runtime_mode: z.string().catch("local"),
  provider: z.string().catch(""),
  launch_header: z.string().catch(""),
  status: z.enum(["online", "offline"]).catch("offline"),
  device_info: z.string().catch(""),
  metadata: z.record(z.string(), z.unknown()).catch({}).default({}),
  owner_id: z.string().nullable().catch(null),
  visibility: z.enum(["private", "public"]).catch("private"),
  profile_id: z.string().nullable().optional(),
  sandbox_mode: z.string().optional(),
  sandbox_image: z.string().optional(),
  sandbox_allowed_hosts: z.array(z.string()).optional().catch(undefined),
  sandbox_capabilities: SandboxCapabilitiesSchema.optional().catch(undefined),
  sandbox_effective: z.string().optional(),
  compliance: z.object({
    region: z.string(),
    on_prem: z.boolean(),
  }).loose().nullable().optional().catch(null),
  last_seen_at: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const AgentRuntimeListSchema = z.array(AgentRuntimeSchema);
// ---------------------------------------------------------------------------
// JEF-321 batch B — runtime updates, local-skill discovery/import, working
// agents, agent-activity/run-count projections, issue usage, single-item
// inbox mutations, and workspaces.
// ---------------------------------------------------------------------------

// CLI/runtime version updates (`POST /api/runtimes/:id/update`, its poll
// endpoint). Same poll-while-pending state machine as the CLI auth / model
// discovery requests above, so a malformed body degrades to an explicit
// "failed" record instead of a fabricated "completed" or an endless spinner.
export const RuntimeUpdateSchema = z.object({
  id: z.string(),
  runtime_id: z.string().default(""),
  status: z.string().default("failed"),
  target_version: z.string().default(""),
  output: z.string().optional(),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const MALFORMED_RUNTIME_UPDATE: RuntimeUpdate = {
  id: "",
  runtime_id: "",
  status: "failed",
  target_version: "",
  error: "invalid update response",
  created_at: "",
  updated_at: "",
};

const RuntimeLocalMcpServerSummarySchema = z.object({
  name: z.string(),
  transport: z.enum(["stdio", "http", "sse", "unknown"]).optional(),
  source: z.string().optional(),
  enabled: z.boolean().default(false),
}).loose();

const RuntimeLocalSkillSummarySchema = z.object({
  key: z.string(),
  name: z.string().default(""),
  description: z.string().optional(),
  source_path: z.string().default(""),
  provider: z.string().default(""),
  root: z.enum(["provider", "universal", "plugin"]).optional(),
  plugin: z.string().optional(),
  can_disable: z.boolean().optional(),
  file_count: z.number().default(0),
}).loose();

export const RuntimeLocalSkillListRequestSchema = z.object({
  id: z.string(),
  runtime_id: z.string().default(""),
  status: z.string().default("failed"),
  skills: z.array(RuntimeLocalSkillSummarySchema).optional(),
  supported: z.boolean().default(true),
  mcp_servers: z.array(RuntimeLocalMcpServerSummarySchema).optional(),
  mcp_supported: z.boolean().optional(),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const MALFORMED_RUNTIME_LOCAL_SKILL_LIST_REQUEST: RuntimeLocalSkillListRequest = {
  id: "",
  runtime_id: "",
  status: "failed",
  supported: true,
  error: "invalid local-skill discovery response",
  created_at: "",
  updated_at: "",
};

const RuntimeLocalSkillImportConflictSchema = z.object({
  existing_skill_id: z.string().default(""),
  existing_created_by: z.string().optional(),
  can_overwrite: z.boolean().default(false),
}).loose();

// `skill` reuses SkillSchema (defined above) rather than a parallel shape —
// the import response embeds the same skill row the Skills API returns.
export const RuntimeLocalSkillImportRequestSchema = z.object({
  id: z.string(),
  runtime_id: z.string().default(""),
  skill_key: z.string().default(""),
  name: z.string().optional(),
  description: z.string().optional(),
  action: z.literal("overwrite").optional(),
  target_skill_id: z.string().optional(),
  supports_conflict: z.boolean().optional(),
  status: z.string().default("failed"),
  skill: SkillSchema.optional(),
  conflict: RuntimeLocalSkillImportConflictSchema.optional(),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const MALFORMED_RUNTIME_LOCAL_SKILL_IMPORT_REQUEST: RuntimeLocalSkillImportRequest = {
  id: "",
  runtime_id: "",
  skill_key: "",
  status: "failed",
  error: "invalid local-skill import response",
  created_at: "",
  updated_at: "",
};

// Workspace-level working-agent projection backing the Agents-list presence
// dots and the sub-issue header. `issue_ids` degrades independently since a
// malformed entry there should not drop the whole agent row.
export const WorkspaceWorkingAgentSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  running_task_count: z.number().default(0),
  issue_ids: z.array(z.string()).catch([]).default([]),
}).loose();

export const WorkspaceWorkingAgentListSchema = z.array(WorkspaceWorkingAgentSchema);
export const EMPTY_WORKSPACE_WORKING_AGENTS: WorkspaceWorkingAgent[] = [];

// Per-agent 30-day daily activity buckets, backing the Agents-list sparkline
// and the agent detail "Last 30 days" panel.
export const AgentActivityBucketSchema = z.object({
  agent_id: z.string(),
  bucket_at: z.string().default(""),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
}).loose();

export const AgentActivityBucketListSchema = z.array(AgentActivityBucketSchema);
export const EMPTY_AGENT_ACTIVITY_BUCKETS: AgentActivityBucket[] = [];

// Per-agent 30-day total run count, backing the Agents-list RUNS column.
export const AgentRunCountSchema = z.object({
  agent_id: z.string(),
  run_count: z.number().default(0),
}).loose();

export const AgentRunCountListSchema = z.array(AgentRunCountSchema);
export const EMPTY_AGENT_RUN_COUNTS: AgentRunCount[] = [];

// `GET /api/issues/:id/usage`. `uncosted_*` and `cost_usd_ticks` stay
// optional (not defaulted) — undefined there means "estimate from the full
// token counts", distinct from a real 0, same convention as the per-task
// usage rows (see RuntimeUsage / TaskUsageSchema above).
export const IssueUsageSummarySchema = z.object({
  total_input_tokens: z.number().default(0),
  total_output_tokens: z.number().default(0),
  total_cache_read_tokens: z.number().default(0),
  total_cache_write_tokens: z.number().default(0),
  cost_usd_ticks: z.number().optional(),
  uncosted_input_tokens: z.number().optional(),
  uncosted_output_tokens: z.number().optional(),
  uncosted_cache_read_tokens: z.number().optional(),
  uncosted_cache_write_tokens: z.number().optional(),
  task_count: z.number().default(0),
}).loose();

export const EMPTY_ISSUE_USAGE_SUMMARY: IssueUsageSummary = {
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_read_tokens: 0,
  total_cache_write_tokens: 0,
  task_count: 0,
};

// Fallback for cancelTask / rerunIssue, which return a single AgentTask.
// `status: "failed"` is the honest read for an unparseable response — it
// neither claims the cancel/rerun succeeded nor leaves the run mid-flight.
export const EMPTY_AGENT_TASK: AgentTask = {
  id: "",
  agent_id: "",
  runtime_id: "",
  issue_id: "",
  status: "failed",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result: null,
  error: null,
  created_at: "",
};

// Single-item inbox mutations (read/unread/archive/unarchive) return the same
// row shape as the list endpoints, so the schema is just the list's element.
export const InboxItemSchema = InboxItemListSchema.element;

export const EMPTY_INBOX_ITEM: InboxItem = {
  id: "",
  workspace_id: "",
  recipient_type: "member",
  recipient_id: "",
  actor_type: null,
  actor_id: null,
  type: "mentioned",
  severity: "info",
  issue_id: null,
  title: "",
  body: null,
  issue_status: null,
  issue_priority: null,
  read: false,
  archived: false,
  created_at: "",
  details: null,
};

export const WorkspaceRepoSchema = z.object({
  url: z.string(),
  description: z.string().optional(),
}).loose();

export const WorkspaceSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  slug: z.string(),
  description: z.string().nullable().default(null),
  context: z.string().nullable().default(null),
  settings: z.record(z.string(), z.unknown()).catch({}).default({}),
  repos: z.array(WorkspaceRepoSchema).catch([]).default([]),
  issue_prefix: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  postmortem_cost_threshold_usd_ticks: z.number().nullable().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  // Present only on the POST /api/workspaces response, when the new
  // workspace was seeded from a template run (K76) or a catalogue pack: the
  // seed report, or the reason the seed failed while the workspace itself was
  // created. Optional everywhere else, so EMPTY_WORKSPACE stays valid.
  template: z.record(z.string(), z.unknown()).optional(),
  template_error: z.string().optional(),
}).loose();

export const WorkspaceListSchema = z.array(WorkspaceSchema);
export const EMPTY_WORKSPACES: Workspace[] = [];

export const EMPTY_WORKSPACE: Workspace = {
  id: "",
  name: "",
  slug: "",
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "",
  avatar_url: null,
  created_at: "",
  updated_at: "",
};

// -----------------------------------------------------------------------
// JEF-321 batch C — members, invitations, skills, personal access tokens,
// chat sessions/pinned agents, attachments, projects.
// -----------------------------------------------------------------------

// Members. Reuses MemberWithUserSchema (defined above, next to ShareLink)
// for the list/patch/accept endpoints instead of a second definition.
export const MemberWithUserListSchema = z.array(MemberWithUserSchema).catch([]).default([]);

export const EMPTY_MEMBER_WITH_USER: MemberWithUser = {
  id: "",
  workspace_id: "",
  user_id: "",
  role: "member",
  created_at: "",
  name: "",
  email: "",
  avatar_url: null,
};

// Invitations. `role`/`status` stay lenient strings-with-catch so an
// unrecognized server value degrades to a safe default instead of failing
// the whole row.
export const InvitationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().optional().default(""),
  inviter_id: z.string().optional().default(""),
  invitee_email: z.string().optional().default(""),
  invitee_user_id: z.string().nullable().optional().default(null),
  role: z.string().optional().default("member"),
  status: z.enum(["pending", "accepted", "declined", "expired"]).catch("pending"),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
  expires_at: z.string().optional().default(""),
  inviter_name: z.string().optional(),
  inviter_email: z.string().optional(),
  workspace_name: z.string().optional(),
}).loose();

export const EMPTY_INVITATION: Invitation = {
  id: "",
  workspace_id: "",
  inviter_id: "",
  invitee_email: "",
  invitee_user_id: null,
  role: "member",
  status: "pending",
  created_at: "",
  updated_at: "",
  expires_at: "",
};

export const InvitationListSchema = z.array(InvitationSchema).catch([]).default([]);

// Skill summaries omit `content`/`files`; SkillSchema already defaults both,
// so it doubles as the summary shape without a second, near-identical schema.
export const SkillSummaryListSchema = z.array(SkillSchema).catch([]).default([]);
export const EMPTY_SKILL_SUMMARY_LIST: SkillSummary[] = [];

// Personal Access Tokens.
export const PersonalAccessTokenSchema = z.object({
  id: z.string(),
  name: z.string().optional().default(""),
  token_prefix: z.string().optional().default(""),
  expires_at: z.string().nullable().optional().default(null),
  last_used_at: z.string().nullable().optional().default(null),
  created_at: z.string().optional().default(""),
}).loose();

export const PersonalAccessTokenListSchema = z.array(PersonalAccessTokenSchema).catch([]).default([]);

// `token` only appears on the create response and is shown to the user
// exactly once — required (no default) so a response missing it fails the
// whole parse. client.ts feeds this to parseWithFallback<T | null>(..., null,
// ...) and throws on null, same null+throw convention as verifyCode /
// googleLogin: a silently blank secret would be worse than a loud failure.
export const CreatePersonalAccessTokenResponseSchema: z.ZodType<CreatePersonalAccessTokenResponse> = PersonalAccessTokenSchema.extend({
  token: z.string(),
}).loose();

// Chat sessions reuse ChatSessionSchema/EMPTY_CHAT_SESSION (defined above)
// for create/update/pin/archive — same shape as GET /api/chat/sessions/:id.

export const ChatPinnedAgentSchema = z.object({
  agent_id: z.string(),
  position: z.number().optional().default(0),
}).loose();

export const EMPTY_CHAT_PINNED_AGENT: ChatPinnedAgent = { agent_id: "", position: 0 };

export const ChatPinnedAgentListSchema = z.array(ChatPinnedAgentSchema).catch([]).default([]);

const PendingChatTaskItemSchema = z.object({
  task_id: z.string().optional().default(""),
  status: z.string().optional().default(""),
  chat_session_id: z.string().optional().default(""),
}).loose();

export const PendingChatTasksResponseSchema = z.object({
  tasks: z.array(PendingChatTaskItemSchema).catch([]).default([]),
}).loose();

export const EMPTY_PENDING_CHAT_TASKS_RESPONSE: PendingChatTasksResponse = { tasks: [] };

export const HasPendingChatTasksResponseSchema = z.object({
  has_pending: z.boolean().catch(false).default(false),
}).loose();

export const EMPTY_HAS_PENDING_CHAT_TASKS_RESPONSE: HasPendingChatTasksResponse = { has_pending: false };

// Issue attachments list reuses AttachmentResponseSchema (defined above,
// next to getAttachment/uploadFile) — same lenient shape, just wrapped.
export const AttachmentListSchema = z.array(AttachmentResponseSchema).catch([]).default([]);
export const EMPTY_ATTACHMENT_LIST: Attachment[] = [];

// Projects. Reuses the ProjectSchema defined above (next to
// SearchProjectResultSchema) for the plain project CRUD endpoints.
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

export const ListProjectsResponseSchema = z.object({
  projects: z.array(ProjectSchema).catch([]).default([]),
  total: z.number().optional().default(0),
}).loose();

export const EMPTY_LIST_PROJECTS_RESPONSE: ListProjectsResponse = { projects: [], total: 0 };

export const ProjectResourceSchema = z.object({
  id: z.string(),
  project_id: z.string().optional().default(""),
  workspace_id: z.string().optional().default(""),
  resource_type: z.enum(["github_repo", "local_directory"]).catch("github_repo"),
  resource_ref: z.record(z.string(), z.unknown()).catch({}).default({}),
  label: z.string().nullable().optional().default(null),
  position: z.number().optional().default(0),
  created_at: z.string().optional().default(""),
  created_by: z.string().nullable().optional().default(null),
}).loose();

export const EMPTY_PROJECT_RESOURCE: ProjectResource = {
  id: "",
  project_id: "",
  workspace_id: "",
  resource_type: "github_repo",
  resource_ref: {},
  label: null,
  position: 0,
  created_at: "",
  created_by: null,
};

export const ListProjectResourcesResponseSchema = z.object({
  resources: z.array(ProjectResourceSchema).catch([]).default([]),
  total: z.number().optional().default(0),
}).loose();

export const EMPTY_LIST_PROJECT_RESOURCES_RESPONSE: ListProjectResourcesResponse = { resources: [], total: 0 };

// ---------------------------------------------------------------------------
// JEF-321 batch D — pins, squad members, autopilots, VCS/Lark/Composio/Slack
// integrations (client.ts lines ~6900-7900)
// ---------------------------------------------------------------------------

// Pins (GET/POST /api/pins). Lists fall back to []; createPin throws on a
// malformed body rather than optimistically inserting a blank pin row into
// the sidebar cache (same "create is a failed mutation" rule as createIssue).
export const PinnedItemSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  user_id: z.string(),
  item_type: z.string(),
  item_id: z.string(),
  position: z.number().default(0),
  created_at: z.string(),
}).loose();

export const PinnedItemListSchema = z.array(PinnedItemSchema);
export const EMPTY_PINNED_ITEM_LIST: PinnedItem[] = [];

// Squad members. listSquadMembers/addSquadMember/updateSquadMemberRole mirror
// the existing SquadSchema/getSquad convention already in this file (EMPTY_*
// fallback, not throw) — every caller (squad-detail-page.tsx) discards the
// mutation's return value and refetches the member list on success, so an
// EMPTY_SQUAD_MEMBER placeholder is never rendered.
export const SquadMemberSchema = z.object({
  id: z.string(),
  squad_id: z.string(),
  member_type: z.string(),
  member_id: z.string(),
  role: z.string().default(""),
  created_at: z.string(),
}).loose();

export const SquadMemberListSchema = z.array(SquadMemberSchema);
export const EMPTY_SQUAD_MEMBER_LIST: SquadMember[] = [];
export const EMPTY_SQUAD_MEMBER: SquadMember = {
  id: "",
  squad_id: "",
  member_type: "agent",
  member_id: "",
  role: "",
  created_at: "",
};

// Autopilots. AutopilotSchema mirrors the Autopilot interface (superset of
// the private AutopilotListItemSchema used by listAutopilots, plus the
// detail-only subscribers/pause_reason fields) so getAutopilot/createAutopilot
// share one definition with GetAutopilotResponseSchema.
const AutopilotSubscriberSchema = z.object({
  user_type: z.string().default("member"),
  user_id: z.string(),
  created_at: z.string(),
}).loose();

export const AutopilotSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable().default(null),
  project_id: z.string().nullable().optional(),
  assignee_type: z.string().default("agent"),
  assignee_id: z.string(),
  status: z.string(),
  pause_reason: z.string().nullable().optional(),
  execution_mode: z.string(),
  batch_eligible: z.boolean().catch(false).optional(),
  issue_title_template: z.string().nullable().default(null),
  created_by_type: z.string(),
  created_by_id: z.string(),
  last_run_at: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  trigger_kinds: z.array(z.string()).optional(),
  next_run_at: z.string().nullable().optional(),
  last_run_status: z.string().nullable().optional(),
  subscribers: z.array(AutopilotSubscriberSchema).optional(),
  can_write: z.boolean().optional(),
  can_manage_access: z.boolean().optional(),
}).loose();

// Update is a patch response the caller never reads (useUpdateAutopilot's
// optimistic cache write comes from the request variables, not the mutation
// result) — falling back here must NOT throw, or a malformed-but-successful
// server response would trip onError and roll back an edit that actually saved.
export const EMPTY_AUTOPILOT: Autopilot = {
  id: "",
  workspace_id: "",
  title: "",
  description: null,
  assignee_type: "agent",
  assignee_id: "",
  status: "paused",
  execution_mode: "create_issue",
  issue_title_template: null,
  created_by_type: "",
  created_by_id: "",
  last_run_at: null,
  created_at: "",
  updated_at: "",
};

export const AutopilotCollaboratorSchema = z.object({
  user_type: z.string().default("member"),
  user_id: z.string(),
  granted_by: z.string(),
  created_at: z.string(),
}).loose();

export const AutopilotCollaboratorsResponseSchema = z.object({
  collaborators: z.array(AutopilotCollaboratorSchema).default([]),
}).loose();

export const EMPTY_AUTOPILOT_COLLABORATORS_RESPONSE: AutopilotCollaboratorsResponse = {
  collaborators: [],
};

export const AutopilotTriggerSchema = z.object({
  id: z.string(),
  autopilot_id: z.string(),
  kind: z.string(),
  enabled: z.boolean().default(false),
  cron_expression: z.string().nullable().default(null),
  timezone: z.string().nullable().default(null),
  next_run_at: z.string().nullable().default(null),
  window_minutes: z.number().optional(),
  webhook_token: z.string().nullable().default(null),
  webhook_path: z.string().nullable().optional(),
  webhook_url: z.string().nullable().optional(),
  label: z.string().nullable().default(null),
  event_filters: z.array(
    z.object({ event: z.string(), actions: z.array(z.string()).optional() }).loose(),
  ).nullable().optional(),
  event_match_criteria: z.string().optional(),
  provider: z.string().nullable().optional(),
  has_signing_secret: z.boolean().optional(),
  signing_secret_hint: z.string().nullable().optional(),
  last_fired_at: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

// createAutopilotTrigger/updateAutopilotTrigger callers (autopilots/mutations.ts)
// discard the mutation result and invalidate the autopilot detail query, so
// EMPTY_AUTOPILOT_TRIGGER is safe here — never rendered directly. Secret-bearing
// trigger writes (rotateAutopilotTriggerWebhookToken, setAutopilotTriggerSigningSecret)
// are handled separately below with a throw, since those responses ARE read
// directly by trigger-row.tsx / signing-secret-section.tsx.
export const EMPTY_AUTOPILOT_TRIGGER: AutopilotTrigger = {
  id: "",
  autopilot_id: "",
  kind: "schedule",
  enabled: false,
  cron_expression: null,
  timezone: null,
  next_run_at: null,
  webhook_token: null,
  label: null,
  last_fired_at: null,
  created_at: "",
  updated_at: "",
};

// getAutopilot (GET /api/autopilots/:id). Read directly by autopilot-detail-page.tsx
// (`const { autopilot, triggers } = data`); a malformed body must not render a
// blank autopilot, so this throws on failure like getIssue — the page's
// `if (!data)` branch already renders a "not found" state for that case.
export const GetAutopilotResponseSchema = z.object({
  autopilot: AutopilotSchema,
  triggers: z.array(AutopilotTriggerSchema).default([]),
  collaborators: z.array(AutopilotCollaboratorSchema).optional(),
}).loose();

export const ListAutopilotRunsResponseSchema = z.object({
  runs: z.array(AutopilotRunSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_AUTOPILOT_RUNS_RESPONSE: ListAutopilotRunsResponse = {
  runs: [],
  total: 0,
};

// VCS integration (Forgejo/Gitea/GitLab). webhook_url/webhook_path are
// legitimately empty when the server has no public URL configured (see
// VCSConnection doc comment), so they default rather than fail the parse.
export const VCSConnectionSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  provider: z.string(),
  instance_url: z.string(),
  account_login: z.string(),
  webhook_url: z.string().default(""),
  webhook_path: z.string().default(""),
  created_at: z.string(),
}).loose();

export const ListVCSConnectionsResponseSchema = z.object({
  connections: z.array(VCSConnectionSchema).default([]),
  available: z.boolean().default(true),
  configured: z.boolean().default(false),
  can_manage: z.boolean().default(false),
}).loose();

export const EMPTY_LIST_VCS_CONNECTIONS_RESPONSE: ListVCSConnectionsResponse = {
  connections: [],
  available: true,
  configured: false,
  can_manage: false,
};

// connectVCS / rotateVCSWebhook return the one-time plaintext webhook_secret
// (never retrievable afterwards) directly rendered by vcs-tab.tsx. No
// EMPTY_* fallback: an unreadable response must throw rather than hand the
// UI an invented empty secret.
export const ConnectVCSResponseSchema = VCSConnectionSchema.extend({
  webhook_secret: z.string(),
});

// Lark integration.
export const LarkInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  agent_id: z.string(),
  app_id: z.string(),
  tenant_key: z.string().nullable().optional(),
  bot_open_id: z.string(),
  installer_user_id: z.string(),
  status: z.string(),
  region: z.string().optional(),
  installed_at: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListLarkInstallationsResponseSchema = z.object({
  installations: z.array(LarkInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_LARK_INSTALLATIONS_RESPONSE: ListLarkInstallationsResponse = {
  installations: [],
  configured: false,
};

// beginLarkInstall / getLarkInstallStatus / redeemLarkBindingToken responses
// are read directly (QR url, polled status, redemption ids) by lark-tab.tsx /
// bind-page.tsx. No EMPTY_* fallback: throw on a malformed body rather than
// invent an empty QR url or a false "success".
export const BeginLarkInstallResponseSchema = z.object({
  session_id: z.string(),
  qr_code_url: z.string(),
  expires_in_seconds: z.number(),
  poll_interval_seconds: z.number(),
}).loose();

export const LarkInstallStatusResponseSchema = z.object({
  status: z.string(),
  installation_id: z.string().optional(),
  error_reason: z.string().optional(),
  error_message: z.string().optional(),
}).loose();

export const RedeemLarkBindingTokenResponseSchema = z.object({
  workspace_id: z.string(),
  installation_id: z.string(),
  lark_open_id: z.string(),
}).loose();

// Composio integration. Toolkit/connection lists fall back to []; the connect
// redirect_url is navigated to directly (`window.location.href = redirect_url`)
// so beginComposioConnect throws rather than invent an empty redirect target.
export const ComposioToolkitSchema = z.object({
  slug: z.string(),
  name: z.string(),
  logo: z.string().optional(),
  category: z.string().optional(),
  connectable: z.boolean().default(false),
}).loose();

export const ComposioToolkitListSchema = z.array(ComposioToolkitSchema);
export const EMPTY_COMPOSIO_TOOLKIT_LIST: ComposioToolkit[] = [];

export const ComposioConnectionSchema = z.object({
  id: z.string(),
  toolkit_slug: z.string(),
  status: z.string(),
  connected_at: z.string(),
  last_used_at: z.string().nullable().optional(),
}).loose();

export const ComposioConnectionListSchema = z.array(ComposioConnectionSchema);
export const EMPTY_COMPOSIO_CONNECTION_LIST: ComposioConnection[] = [];

export const ComposioConnectInitResponseSchema = z.object({
  redirect_url: z.string(),
}).loose();

// Slack integration (bring-your-own-app install, MUL-3666).
export const SlackInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  agent_id: z.string(),
  team_id: z.string(),
  bot_user_id: z.string(),
  installer_user_id: z.string(),
  status: z.string(),
  installed_at: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListSlackInstallationsResponseSchema = z.object({
  installations: z.array(SlackInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_SLACK_INSTALLATIONS_RESPONSE: ListSlackInstallationsResponse = {
  installations: [],
  configured: false,
};

// registerSlackBYO reuses SlackInstallationSchema and throws on a malformed
// body (no EMPTY_* fallback) — listed alongside the other one-time/write-only
// integration flows per JEF-321: never invent placeholder installation data.
export const RedeemSlackBindingTokenResponseSchema = z.object({
  workspace_id: z.string(),
  installation_id: z.string(),
  slack_user_id: z.string(),
}).loose();

// ---------------------------------------------------------------------------
// JEF-321 batch E — inbox bulk actions, issue batch-delete, agent task
// cancellation, OIDC login completion, CLI token issuance, quick-create,
// runtime unbind-and-delete (client-parse-guard.test.ts ALLOW_LIST closeout)
// ---------------------------------------------------------------------------

// markAllInboxRead / archiveAllInbox / archiveAllReadInbox / archiveCompletedInbox
// (inbox/mutations.ts) all share this shape and are all "count is only used
// to invalidate, never rendered" mutations — a malformed body must not throw,
// or a bulk action that actually succeeded server-side would surface as a
// failed mutation. Falls back to 0.
export const InboxBulkActionResponseSchema = z.object({
  count: z.number().catch(0).default(0),
}).loose();

export const EMPTY_INBOX_BULK_ACTION_RESPONSE: { count: number } = { count: 0 };

// batchDeleteIssues (POST /api/issues/batch-delete). useBatchDeleteIssues
// already removes the rows optimistically in onMutate and never reads the
// mutation result, so a malformed body falls back to 0 rather than throwing
// past a delete that already applied server-side.
export const BatchDeleteIssuesResponseSchema = z.object({
  deleted: z.number().catch(0).default(0),
}).loose();

export const EMPTY_BATCH_DELETE_ISSUES_RESPONSE: { deleted: number } = { deleted: 0 };

// cancelAgentTasks (POST /api/agents/:id/cancel-tasks). agent-row-actions.tsx
// reads `cancelled` only to word a toast ("no tasks to cancel" vs "cancelled
// N tasks") inside its own try/catch, so a fallback of 0 degrades to the more
// conservative message rather than throwing past a cancellation that already
// applied.
export const CancelAgentTasksResponseSchema = z.object({
  cancelled: z.number().catch(0).default(0),
}).loose();

export const EMPTY_CANCEL_AGENT_TASKS_RESPONSE: { cancelled: number } = { cancelled: 0 };

// completeOIDCLogin (POST /auth/oidc/callback). Extends LoginResponseSchema
// (batch A, above) with the workspace to land on. Same "no EMPTY_* fallback"
// rule as LoginResponseSchema: a malformed body must not look like a
// successful login with nowhere to go — sso-callback-page.tsx's catch already
// handles the throw.
export const OIDCLoginResponseSchema = LoginResponseSchema.extend({
  workspace_slug: z.string(),
});

// issueCliToken (POST /api/cli-token). login-page.tsx redirects the CLI
// callback with the token directly; an invented empty token would silently
// hand the CLI an unusable session instead of surfacing the existing
// try/catch failure state. No EMPTY_* fallback — throw.
export const IssueCliTokenResponseSchema = z.object({
  token: z.string(),
}).loose();

// quickCreateIssue (POST /api/issues/quick-create). Same "create is a failed
// mutation, not a safe-empty read" rule as createIssue/createComment above:
// an invented empty task_id would report success on a modal submission that
// actually failed to enqueue anything. No EMPTY_* fallback — throw.
export const QuickCreateIssueResponseSchema = z.object({
  task_id: z.string(),
}).loose();

// unbindAgentsAndDeleteRuntime (POST /api/runtimes/:id/unbind-agents-and-delete).
// delete-runtime-dialog.tsx discards the result and invalidates on success, so
// a malformed body falls back rather than throws past a delete that already
// applied. `agents_archived` is the server's deprecated mirror of
// `agents_unbound`, kept for installed clients (see client.ts doc comment).
export const UnbindAgentsAndDeleteRuntimeResponseSchema = z.object({
  status: z.string().catch(""),
  agents_unbound: z.number().optional(),
  agents_archived: z.number().optional(),
  tasks_cancelled: z.number().catch(0).default(0),
  autopilots_paused: z.number().optional(),
}).loose();

export const EMPTY_UNBIND_AGENTS_AND_DELETE_RUNTIME_RESPONSE: {
  status: string;
  agents_unbound?: number;
  agents_archived?: number;
  tasks_cancelled: number;
  autopilots_paused?: number;
} = { status: "", tasks_cancelled: 0 };
// Missing or malformed memory must never become a publishable empty draft.
export const ProjectMemorySchema = z.object({
  rules: z.array(z.string()),
  revision: z.number().int().nonnegative(),
  reviewed_by: z.string().nullable().catch(null),
  reviewed_at: z.string().nullable().catch(null),
  expires_at: z.iso.datetime({ offset: true }).nullable().default(null),
  expired: z.boolean().default(false),
  restored_from_revision: z.number().int().nonnegative().optional(),
  // Provenance is additive display metadata; degrade independently so a bad
  // source_review cannot erase the published rules.
  source_review: z.object({
    review_id: z.string(),
    issue_id: z.string(),
    task_id: z.string(),
    feedback: z.string(),
    criteria: z.array(z.string()),
    assessments: z.array(z.object({
      passed: z.boolean(),
      evidence: z.string(),
    })),
    snapshot_token: z.string().optional(),
    reviewed_by: z.string(),
    reviewed_at: z.string(),
    input_hash: z.string().optional(),
  }).optional().catch(undefined),
}).loose();
export const ProjectMemoryHistorySchema = z.object({
  versions: z.array(ProjectMemorySchema),
  next_before_revision: z.number().int().positive().nullable(),
});
export const EMPTY_PROJECT_MEMORY: ProjectMemory = {
  rules: [],
  revision: -1,
  reviewed_by: null,
  reviewed_at: null,
  expires_at: null,
  expired: false,
};


const DeliverySnapshotSchema = z.object({
  title: z.string(),
  description: z.string().nullable(),
  criteria: z.array(z.string().min(1)).max(20),
  revision: z.number().int().nonnegative(),
  run: z.object({
    id: z.string().uuid(), status: z.string(), result: z.unknown(),
    error: z.string().nullable(), completed_at: z.iso.datetime({ offset: true }).nullable(),
  }).transform(({ completed_at, ...run }) => ({ ...run, completedAt: completed_at })).nullable(),
  pull_requests: z.array(GitHubPullRequestSchema.extend({ head_sha: z.string() })
    .transform(({ head_sha, ...pr }) => ({ ...pr, headSha: head_sha }))),
}).transform(({ pull_requests, ...snapshot }) => ({ ...snapshot, pullRequests: pull_requests }));

const DeliveryUSD = z.string().regex(/^\d+\.\d{10}$/).refine((value) => Number.isFinite(Number(value)));
export const DeliveryUsageSnapshotSchema = z.object({
  captured_at: z.iso.datetime({ offset: true }),
  status: z.enum(["reported", "estimated", "partial", "unavailable"]),
  available_usd: DeliveryUSD.nullable(),
  reported_usd: DeliveryUSD,
  estimated_usd: DeliveryUSD,
  run_ids: z.array(z.string().uuid()),
  runs_without_usage: z.number().int().nonnegative(),
  nonterminal_runs: z.number().int().nonnegative(),
  unpriced_slices: z.number().int().nonnegative(),
}).refine((usage) => {
  if (![usage.available_usd, usage.reported_usd, usage.estimated_usd].every((value) => value === null || /^\d+\.\d{10}$/.test(value))) return false;
  if ((usage.status === "unavailable") !== (usage.available_usd === null)) return false;
  if (usage.available_usd !== null && BigInt(usage.available_usd.replace(".", "")) !== BigInt(usage.reported_usd.replace(".", "")) + BigInt(usage.estimated_usd.replace(".", ""))) return false;
  const gaps = usage.runs_without_usage + usage.nonterminal_runs + usage.unpriced_slices;
  if (usage.status === "partial" && gaps === 0) return false;
  if ((usage.status === "reported" || usage.status === "estimated") && (gaps > 0 || usage.run_ids.length === 0)) return false;
  return usage.runs_without_usage <= usage.run_ids.length && usage.nonterminal_runs <= usage.run_ids.length;
})
  .transform((usage) => ({ capturedAt: usage.captured_at, status: usage.status, availableUsd: usage.available_usd,
    reportedUsd: usage.reported_usd, estimatedUsd: usage.estimated_usd, runIds: usage.run_ids,
    runsWithoutUsage: usage.runs_without_usage, nonterminalRuns: usage.nonterminal_runs, unpricedSlices: usage.unpriced_slices }));

const DeliveryMetricsSchema = z.object({
  review_count: z.number().int().nonnegative(),
  reviewed_results: z.number().int().nonnegative(),
  accepted_results: z.number().int().nonnegative(),
  correction_requests: z.number().int().nonnegative(),
  acceptance_reversals: z.number().int().nonnegative(),
}).refine((m) => m.accepted_results <= m.reviewed_results && m.reviewed_results <= m.review_count)
  .transform((m) => ({ reviewCount: m.review_count, reviewedResults: m.reviewed_results, acceptedResults: m.accepted_results,
    correctionRequests: m.correction_requests, acceptanceReversals: m.acceptance_reversals }));

export const DeliveryReviewSchema = z.object({
  usage_snapshot: DeliveryUsageSnapshotSchema.nullable().optional().catch(null),
  review_delay_seconds: z.number().int().nonnegative().nullable().optional().catch(null),
  human_effort_seconds: z.number().int().nonnegative().nullable().optional().catch(null),
  id: z.string().uuid(),
  decision: z.enum(["accepted", "changes_requested"]),
  feedback: z.string(),
  assessments: z.array(z.object({ passed: z.boolean(), evidence: z.string() })).max(20),
  snapshot: DeliverySnapshotSchema,
  snapshot_token: z.string().regex(/^[a-f0-9]{64}$/),
  reviewed_by: z.string().uuid(),
  created_at: z.iso.datetime({ offset: true }),
  correction_task_id: z.string().uuid().nullable().optional(),
}).refine((review) => review.decision !== "accepted" || (
  review.snapshot.run?.status === "completed" && review.snapshot.run.completedAt !== null &&
  review.snapshot.criteria.length > 0 && review.assessments.length === review.snapshot.criteria.length &&
  review.assessments.every((a) => a.passed === true && a.evidence.trim().length > 0)
)).transform(({ snapshot_token, reviewed_by, created_at, correction_task_id, usage_snapshot, review_delay_seconds, human_effort_seconds, ...review }) => ({
  usageSnapshot: usage_snapshot ?? null, reviewDelaySeconds: review_delay_seconds ?? null,
  humanEffortSeconds: human_effort_seconds ?? null,
  ...review, snapshotToken: snapshot_token, reviewedBy: reviewed_by, createdAt: created_at, correctionTaskId: correction_task_id ?? null,
}));

export const IssueDeliverySchema = z.object({
  metrics: DeliveryMetricsSchema.nullable().optional().catch(null),
  snapshot_token: z.string().regex(/^[a-f0-9]{64}$/),
  latest_review: DeliveryReviewSchema.nullable(),
  review_stale: z.boolean(),
}).and(DeliverySnapshotSchema).transform(({ snapshot_token, latest_review, review_stale, ...snapshot }) => ({
  ...snapshot, snapshotToken: snapshot_token, latestReview: latest_review,
  reviewStale: review_stale || (latest_review !== null && latest_review.snapshotToken !== snapshot_token),
}));
export type IssueDelivery = z.infer<typeof IssueDeliverySchema>;
export type DeliveryReview = z.infer<typeof DeliveryReviewSchema>;

export const DeliveryCriteriaSchema = z.object({ criteria: z.array(z.string()), revision: z.number().int().nonnegative() });

export const DeliveryCorrectionSchema = z.object({ review_id: z.string().uuid(), task_id: z.string().uuid() })
  .transform(({ review_id, task_id }) => ({ reviewId: review_id, taskId: task_id }));
export type DeliveryCorrection = z.infer<typeof DeliveryCorrectionSchema>;

export const DeliveryHistorySchema = z.object({ reviews: z.array(DeliveryReviewSchema), next_before_id: z.string().uuid().nullable() })
  .transform(({ reviews, next_before_id }) => ({ reviews, nextBeforeId: next_before_id }));
export type DeliveryHistory = z.infer<typeof DeliveryHistorySchema>;

export const ProjectMemoryUsageSchema = z.object({
  since: z.string().datetime({ offset: true }),
  until: z.string().datetime({ offset: true }),
  started_runs: MemoryUsageCountSchema,
  recorded_runs: MemoryUsageCountSchema,
  unrecorded_runs: MemoryUsageCountSchema,
  runs_with_project_memory: MemoryUsageCountSchema,
  versions: z.array(z.object({
    project_id: z.string().uuid(),
    revision: z.number().int().min(1).max(2147483647),
    prepared_runs: MemoryUsageCountSchema.refine((count) => count > 0),
    last_started_at: z.string().datetime({ offset: true }),
  })),
}).refine((value) => {
  const since = Date.parse(value.since), until = Date.parse(value.until);
  return until - since === 30 * 24 * 60 * 60 * 1000 &&
    value.started_runs === value.recorded_runs + value.unrecorded_runs &&
    value.recorded_runs >= value.runs_with_project_memory &&
    (value.versions.length > 0) === (value.runs_with_project_memory > 0) &&
    new Set(value.versions.map((version) => `${version.project_id}:${version.revision}`)).size === value.versions.length &&
    value.versions.every((version) => version.prepared_runs <= value.runs_with_project_memory &&
      Date.parse(version.last_started_at) >= since && Date.parse(version.last_started_at) < until);
});

export const OrgTeamCatalogSchema = z.object({ templates: z.array(z.object({ id: z.string(), name: z.string(), description: z.string(), roles: z.array(z.string()), procedure: z.string() })) });

// ---------------------------------------------------------------------------
// Native calendar (OS plan, chantier 19). See
// server/internal/handler/calendar_events.go for the wire shapes.
// ---------------------------------------------------------------------------

export const CalendarParticipantSchema = z.object({
  type: z.string(),
  id: z.string(),
  name: z.string().optional(),
  response: z.string().default("pending"),
  required: z.boolean().default(true),
}).loose();

export const CalendarActorSchema = z.object({
  type: z.string(),
  id: z.string(),
  name: z.string().optional(),
}).loose();

const EMPTY_CALENDAR_ACTOR = { type: "member", id: "" };

// Named CalendarEventEntry (not CalendarEvent) to avoid colliding with the
// ICS-subscription CalendarEvent above ({summary, start, end, in_progress}) —
// a different, smaller shape for a different feature (the feed Multica
// *reads*, vs this workspace calendar Multica *owns*).
export const CalendarEventEntrySchema = z.object({
  id: z.string(),
  title: z.string().default(""),
  description: z.string().default(""),
  starts_at: z.string(),
  ends_at: z.string(),
  all_day: z.boolean().default(false),
  timezone: z.string().default("UTC"),
  location: z.string().default(""),
  issue_id: z.string().nullable().default(null),
  issue_identifier: z.string().optional(),
  project_id: z.string().nullable().default(null),
  status: z.string().default("scheduled"),
  created_by: CalendarActorSchema.default(EMPTY_CALENDAR_ACTOR),
  source: z.string().default("vigil"),
  external_id: z.string().optional(),
  decision_id: z.string().nullable().default(null),
  participants: z.array(CalendarParticipantSchema).default([]),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const CalendarEventsResponseSchema = z.object({
  events: z.array(CalendarEventEntrySchema).default([]),
  from: z.string().default(""),
  to: z.string().default(""),
}).loose();

export const EMPTY_CALENDAR_EVENTS_RESPONSE: CalendarEventsResponse = Object.freeze({
  events: [],
  from: "",
  to: "",
}) as CalendarEventsResponse;

export const CalendarEventResponseSchema = z.object({ event: CalendarEventEntrySchema }).loose();

export const EMPTY_CALENDAR_EVENT: CalendarEventEntry = Object.freeze({
  id: "",
  title: "",
  description: "",
  starts_at: "",
  ends_at: "",
  all_day: false,
  timezone: "UTC",
  location: "",
  issue_id: null,
  project_id: null,
  status: "scheduled",
  created_by: EMPTY_CALENDAR_ACTOR,
  source: "vigil",
  decision_id: null,
  participants: [],
  created_at: "",
  updated_at: "",
}) as CalendarEventEntry;

export const AgendaIssueSchema = z.object({
  id: z.string(),
  identifier: z.string().default(""),
  title: z.string().default(""),
  status: z.string().default(""),
  due_date: z.string().default(""),
  assignee_type: z.string().nullable().optional(),
  assignee_id: z.string().nullable().optional(),
}).loose();

export const AgendaCycleSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  start_date: z.string().default(""),
  end_date: z.string().default(""),
}).loose();

export const AgendaMeetingSchema = z.object({
  id: z.string(),
  title: z.string().default(""),
  status: z.string().default(""),
  started_at: z.string().default(""),
  ended_at: z.string().nullable().optional(),
}).loose();

// A wake-up in the agenda. `.catch([])` on the array so a follow-up shaped
// wrong by a newer server costs the wake-up band, never the whole agenda.
export const AgendaFollowupSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  identifier: z.string().default(""),
  issue_title: z.string().default(""),
  agent_id: z.string().default(""),
  agent_name: z.string().default(""),
  fires_at: z.string().default(""),
  note: z.string().default(""),
}).loose();

export const CalendarAgendaSchema = z.object({
  from: z.string().default(""),
  to: z.string().default(""),
  events: z.array(CalendarEventEntrySchema).default([]),
  issues_due: z.array(AgendaIssueSchema).default([]),
  cycles: z.array(AgendaCycleSchema).default([]),
  meetings: z.array(AgendaMeetingSchema).default([]),
  followups: z.array(AgendaFollowupSchema).catch([]).default([]),
}).loose();

export const EMPTY_CALENDAR_AGENDA: CalendarAgenda = Object.freeze({
  from: "",
  to: "",
  events: [],
  issues_due: [],
  cycles: [],
  meetings: [],
  followups: [],
}) as CalendarAgenda;

export const CalendarSlotSchema = z.object({
  starts_at: z.string(),
  ends_at: z.string(),
}).loose();

export const CalendarSlotsResponseSchema = z.object({
  slots: z.array(CalendarSlotSchema).default([]),
  duration_minutes: z.number().default(30),
  tz: z.string().default("UTC"),
}).loose();

export const EMPTY_CALENDAR_SLOTS_RESPONSE: CalendarSlotsResponse = Object.freeze({
  slots: [],
  duration_minutes: 30,
  tz: "UTC",
}) as CalendarSlotsResponse;

export const CalendarFeedTokenStatusSchema = z.object({
  configured: z.boolean().default(false),
  created_at: z.string().optional(),
}).loose();

export const EMPTY_CALENDAR_FEED_TOKEN_STATUS: CalendarFeedTokenStatus = Object.freeze({
  configured: false,
}) as CalendarFeedTokenStatus;

export const CalendarFeedTokenMintedSchema = z.object({
  url: z.string().default(""),
  path: z.string().default(""),
}).loose();

export const CalendarGoogleImportResultSchema = z.object({
  created: z.number().default(0),
  updated: z.number().default(0),
  seen: z.number().default(0),
}).loose();

export const EMPTY_CALENDAR_GOOGLE_IMPORT_RESULT: CalendarGoogleImportResult = Object.freeze({
  created: 0,
  updated: 0,
  seen: 0,
}) as CalendarGoogleImportResult;

// Follow-ups (OS plan, vague B): a deferred wake-up of an issue's agent.
// Server source of truth: server/internal/handler/followups.go.
export const FollowupSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  agent_id: z.string().default(""),
  agent_name: z.string().default(""),
  fires_at: z.string().default(""),
  note: z.string().default(""),
  // Open on the wire: an actor kind added server-side must not drop the row.
  scheduled_by_type: z.string().default("member"),
  scheduled_by_id: z.string().nullable().optional(),
  created_at: z.string().default(""),
}).loose();

// The budget is what the schedule dialog quotes when the server refuses with
// a 429; a malformed one must not cost the list, hence the per-field catch.
export const FollowupBudgetSchema = z.object({
  max_per_agent_per_day: z.number().catch(0).default(0),
  max_per_workspace_per_day: z.number().catch(0).default(0),
}).loose();

export const EMPTY_FOLLOWUP_BUDGET: FollowupBudget = Object.freeze({
  max_per_agent_per_day: 0,
  max_per_workspace_per_day: 0,
}) as FollowupBudget;

export const IssueFollowupsResponseSchema = z.object({
  followups: z.array(FollowupSchema).catch([]).default([]),
  budget: FollowupBudgetSchema.catch({ max_per_agent_per_day: 0, max_per_workspace_per_day: 0 }),
}).loose();

export const EMPTY_ISSUE_FOLLOWUPS: IssueFollowupsResponse = Object.freeze({
  followups: [],
  budget: EMPTY_FOLLOWUP_BUDGET,
}) as IssueFollowupsResponse;

export const FollowupResponseSchema = z.object({
  followup: FollowupSchema,
}).loose();

// A create whose body drifted still scheduled the wake-up server-side: the
// caller invalidates the list either way, so degrade rather than throw.
export const EMPTY_FOLLOWUP: Followup = Object.freeze({
  id: "",
  issue_id: "",
  agent_id: "",
  agent_name: "",
  fires_at: "",
  note: "",
  scheduled_by_type: "member",
  scheduled_by_id: null,
  created_at: "",
}) as Followup;

// Recurring issues (OS plan, table stakes): the rule of the series this issue
// belongs to, plus the series itself. Server source of truth:
// server/internal/handler/issue_recurrence.go.
//
// `mode` and `created_by_type` stay open strings — an added actor kind or mode
// must degrade in the switch, not drop the rule.
export const IssueRecurrenceSchema = z.object({
  id: z.string(),
  issue_id: z.string().default(""),
  cron_expression: z.string().default(""),
  timezone: z.string().default("UTC"),
  mode: z.string().default("schedule"),
  enabled: z.boolean().default(true),
  next_run_at: z.string().nullish().transform((v) => v ?? null),
  last_occurrence_id: z.string().nullish().transform((v) => v ?? null),
  occurrence_count: z.number().catch(0).default(0),
  created_by_type: z.string().default("member"),
  created_by_id: z.string().nullish(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const IssueRecurrenceSourceSchema = z.object({
  id: z.string().default(""),
  identifier: z.string().default(""),
  title: z.string().default(""),
}).loose();

export const IssueRecurrenceOccurrenceSchema = z.object({
  id: z.string(),
  identifier: z.string().default(""),
  title: z.string().default(""),
  status: z.string().default("todo"),
  created_at: z.string().default(""),
  due_date: z.string().nullish().transform((v) => v ?? null),
}).loose();

// No EMPTY_* fallback for this one: "no rule" is a real answer (the endpoint
// 404s), so an unreadable body must degrade to `null` — the same nothing the
// 404 produces — rather than to an invented rule with an empty cron, which the
// block would render as "this issue recurs at «»".
export const IssueRecurrenceResponseSchema = z.object({
  recurrence: IssueRecurrenceSchema,
  source: IssueRecurrenceSourceSchema.catch({ id: "", identifier: "", title: "" }),
  occurrences: z.array(IssueRecurrenceOccurrenceSchema).catch([]).default([]),
  next_runs: z.array(z.string()).catch([]).default([]),
}).loose();

// Autopilots from a sentence. `execution_mode` stays an open string: the
// preview renders it through a defaulted switch, never an exhaustive one.
export const AutopilotDraftSchema = z.object({
  title: z.string().default(""),
  cron_expression: z.string().default(""),
  timezone: z.string().default("UTC"),
  description: z.string().default(""),
  execution_mode: z.string().default("run_only"),
  issue_title_template: z.string().default("").catch(""),
  reason: z.string().default(""),
  next_runs: z.array(z.string()).catch([]).default([]),
  model: z.string().optional(),
}).loose();

export const AutopilotDraftResponseSchema = z.object({
  draft: AutopilotDraftSchema,
}).loose();

export const EMPTY_AUTOPILOT_DRAFT: AutopilotDraft = Object.freeze({
  title: "",
  cron_expression: "",
  timezone: "UTC",
  description: "",
  execution_mode: "run_only",
  issue_title_template: "",
  reason: "",
  next_runs: [],
}) as AutopilotDraft;

export const AutopilotProposalResponseSchema = z.object({
  autopilot: AutopilotSchema,
  decision_id: z.string().nullable().default(null),
  next_runs: z.array(z.string()).catch([]).default([]),
}).loose();

export const EMPTY_AUTOPILOT_PROPOSAL: AutopilotProposalResponse = Object.freeze({
  autopilot: EMPTY_AUTOPILOT,
  decision_id: null,
  next_runs: [],
}) as AutopilotProposalResponse;
