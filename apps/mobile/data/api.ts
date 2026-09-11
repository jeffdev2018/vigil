/**
 * Mobile-owned fetch wrapper. Mirrors the surface area of
 * packages/core/api/client.ts that mobile actually uses, but lives in
 * apps/mobile/ so we control retry/timeout/error handling independently.
 *
 * Types are imported via `import type` from @multica/core/types — zero
 * runtime coupling. Zod schemas + fallbacks are imported from
 * @multica/core/api/schemas (pure data, on the mobile sharing whitelist).
 *
 * Design checklist (apps/mobile/CLAUDE.md "Lessons → ApiClient capability list"):
 *   1. Zod parseWithFallback for endpoints with schemas (drift defense)
 *   2. onUnauthorized callback on 401 (auto sign-out, avoids retry loops)
 *   3. X-Request-ID per request + structured logger (debug + tracing)
 *   4. Bearer auth + X-Workspace-Slug — NOT cookie auth (no CSRF, no credentials)
 */
import type {
  Agent,
  AgentTask,
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  Comment,
  CreateCommentSubIssueManualRequest,
  CreateIssueRequest,
  CreateLabelRequest,
  CreateProjectRequest,
  CreateProjectResourceRequest,
  InboxItem,
  Issue,
  IssueLabelsResponse,
  Label,
  IssueReaction,
  SourceContextPreview,
  ListIssuesParams,
  ListIssuesResponse,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListGoalsResponse,
  ListProjectsResponse,
  MemberWithUser,
  PinnedItem,
  PinnedItemType,
  Project,
  ProjectResource,
  Reaction,
  ReorderPinsRequest,
  RuntimeDevice,
  SearchIssuesResponse,
  SearchProjectsResponse,
  ListIssueStatusesResponse,
  SendChatMessageResponse,
  Squad,
  NotificationPreferenceResponse,
  NotificationPreferences,
  TaskMessagePayload,
  TimelineEntry,
  AcceptTriageItemResponse,
  DismissTriageItemResponse,
  TriageItemState,
  TriageItemsResponse,
  TriageStats,
  Postmortem,
  PostmortemState,
  PostmortemStats,
  PostmortemsResponse,
  Meeting,
  MeetingListResponse,
  VoiceTranscription,
  UpdateIssueRequest,
  UpdateMeRequest,
  UpdateProjectRequest,
  User,
  Workspace,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import {
  AppConfigSchema,
  EMPTY_APP_CONFIG,
  EMPTY_LIST_ISSUE_STATUSES_RESPONSE,
  EMPTY_LIST_ISSUES_RESPONSE,
  EMPTY_TIMELINE_ENTRIES,
  IssueSchema,
  SourceContextPreviewSchema,
  ListIssuesResponseSchema,
  ListIssueStatusesResponseSchema,
  TimelineEntriesSchema,
  WorkspaceSubscriptionSummarySchema,
  AcceptTriageItemResponseSchema,
  DismissTriageItemResponseSchema,
  EMPTY_TRIAGE_ITEMS_RESPONSE,
  EMPTY_TRIAGE_STATS,
  TriageItemsResponseSchema,
  TriageStatsSchema,
  EMPTY_POSTMORTEMS_RESPONSE,
  EMPTY_POSTMORTEM_STATS,
  PostmortemSchema,
  PostmortemStatsSchema,
  PostmortemsResponseSchema,
  EMPTY_MEETING,
  EMPTY_MEETING_LIST,
  EMPTY_VOICE_TRANSCRIPTION,
  MeetingListResponseSchema,
  MeetingSchema,
  VoiceTranscriptionSchema,
  AgentEffectListSchema,
  UndoReportSchema,
  TaskActivityResponseSchema,
  EMPTY_TASK_ACTIVITY,
  IssueDeliverySchema,
  DeliveryReviewSchema,
  DeliveryCriteriaSchema,
  DeliveryCorrectionSchema,
  type IssueDelivery,
  type DeliveryReview,
  type DeliveryCorrection,
} from "@multica/core/api/schemas";
import type { AppConfigResponse } from "@multica/core/api/schemas";
import {
  ActiveTasksResponseSchema,
  AgentListSchema,
  AgentTaskListSchema,
  AttachmentListSchema,
  AttachmentSchema,
  ChatMessageListSchema,
  CommentSchema,
  ChatPendingTaskSchema,
  ChatSessionListSchema,
  ChatSessionSchema,
  EMPTY_ACTIVE_TASKS_RESPONSE,
  EMPTY_AGENT_LIST,
  EMPTY_AGENT_TASK_LIST,
  EMPTY_ATTACHMENT_LIST,
  EMPTY_CHAT_MESSAGE_LIST,
  EMPTY_CHAT_PENDING_TASK,
  EMPTY_CHAT_SESSION_LIST,
  EMPTY_COMMENT,
  EMPTY_INBOX_LIST,
  EMPTY_ISSUE_FALLBACK,
  EMPTY_SOURCE_CONTEXT_PREVIEW,
  EMPTY_LIST_LABELS_RESPONSE,
  EMPTY_LIST_PROJECT_RESOURCES_RESPONSE,
  EMPTY_LIST_GOALS_RESPONSE,
  EMPTY_LIST_PROJECTS_RESPONSE,
  EMPTY_MEMBER_LIST,
  EMPTY_NOTIFICATION_PREFERENCES,
  EMPTY_ORG_STRUCTURE_LIST,
  EMPTY_PIN_LIST,
  EMPTY_PROJECT,
  EMPTY_RUNTIME_LIST,
  EMPTY_SEARCH_ISSUES_RESPONSE,
  EMPTY_SEARCH_PROJECTS_RESPONSE,
  EMPTY_SQUAD_LIST,
  EMPTY_USER,
  EMPTY_WORKSPACE_LIST,
  InboxListSchema,
  NotificationPreferenceResponseSchema,
  OrgStructureListSchema,
  type OrgStructureList,
  ListLabelsResponseSchema,
  ListProjectResourcesResponseSchema,
  ListGoalsResponseSchema,
  ListProjectsResponseSchema,
  MemberListSchema,
  PinListSchema,
  PinnedItemSchema,
  ProjectSchema,
  RuntimeListSchema,
  SearchIssuesResponseSchema,
  SearchProjectsResponseSchema,
  SendChatMessageResponseSchema,
  SquadListSchema,
  UserSchema,
  WorkspaceListSchema,
} from "./schemas";
import type { ZodType } from "zod";
import { getCurrentSlug } from "./workspace-store";
import { parseWithFallback } from "@/lib/parse-response";
import { InboxDecisionsSchema, type InboxDecisions } from "./schemas";
import {
  EMPTY_VOICE_ISSUE_DRAFT,
  VoiceIssueDraftSchema,
  type VoiceIssueDraft,
} from "./schemas";
import { RunReplaySchema, type RunReplay } from "./schemas";
import {
  EMPTY_AGENT_EFFECT_LIST,
  EMPTY_UNDO_REPORT,
  type AgentEffectList,
  type UndoReport,
} from "./schemas";
import {
  EMPTY_ISSUE_GOAL_RESPONSE,
  IssueGoalResponseSchema,
  type IssueGoalResponse,
} from "./schemas";
import {
  EMPTY_APPROVALS,
  ApprovalsResponseSchema,
  type ApprovalsResponse,
} from "./schemas";
import {
  EMPTY_RUNS_RESPONSE,
  RunsResponseSchema,
  type RunsResponse,
  EMPTY_CANCEL_RUNS_RESPONSE,
  CancelRunsResponseSchema,
  type CancelRunsResponse,
  EMPTY_KILL_SWITCH_RESPONSE,
  KillSwitchResponseSchema,
  type KillSwitchResponse,
  EMPTY_RUN_HALT,
  RunHaltSchema,
  type RunHalt,
} from "./schemas";
import {
  DoctrineSchema,
  DoctrineDiffSchema,
  DoctrineReportsResponseSchema,
  DoctrineVersionsResponseSchema,
  EMPTY_DOCTRINE,
  EMPTY_DOCTRINE_DIFF,
  EMPTY_DOCTRINE_REPORTS,
  EMPTY_DOCTRINE_VERSIONS,
  type Doctrine,
  type DoctrineDiff,
  type DoctrineReportsResponse,
  type DoctrineVersionsResponse,
} from "./schemas";
import {
  PackCatalogueSchema,
  PackDetailSchema,
  PackInstallDetailSchema,
  PackInstallListSchema,
  PackInstallResultSchema,
  PackPreviewSchema,
  PackUninstallResultSchema,
  EMPTY_PACK_CATALOGUE,
  EMPTY_PACK_DETAIL,
  EMPTY_PACK_INSTALL_DETAIL,
  EMPTY_PACK_INSTALL_LIST,
  EMPTY_PACK_INSTALL_RESULT,
  EMPTY_PACK_PREVIEW,
  EMPTY_PACK_UNINSTALL_RESULT,
  type PackCatalogue,
  type PackDetail,
  type PackInstallDetail,
  type PackInstallList,
  type PackInstallResult,
  type PackPreview,
  type PackStrategy,
  type PackUninstallResult,
} from "./schemas";
import { createRequestId } from "@/lib/request-id";
import { buildCommentUpdateBody } from "./revision";
import {
  CalendarAgendaSchema,
  CalendarEventResponseSchema,
  EMPTY_CALENDAR_AGENDA,
} from "@multica/core/api/schemas";
import type { CalendarAgenda, CalendarEventEntry, CalendarEventInput } from "@multica/core/types";
// Réveil programmé (JEF-373). Schemas and fallbacks are the shared ones in
// @multica/core/api/schemas — pure zod, on the mobile sharing whitelist — so
// mobile and web parse the same bytes the same way.
import {
  AutopilotDraftResponseSchema,
  AutopilotProposalResponseSchema,
  FollowupResponseSchema,
  IssueFollowupsResponseSchema,
  EMPTY_AUTOPILOT_DRAFT,
  EMPTY_AUTOPILOT_PROPOSAL,
  EMPTY_FOLLOWUP,
  EMPTY_ISSUE_FOLLOWUPS,
} from "@multica/core/api/schemas";
import type {
  AutopilotDraft,
  AutopilotProposalResponse,
  DraftAutopilotInput,
  Followup,
  IssueFollowupsResponse,
  IssueRecurrenceResponse,
  ProposeAutopilotInput,
  ScheduleFollowupInput,
  SetIssueRecurrenceInput,
} from "@multica/core/types";
// Recurring issues (OS plan, table stakes). Same shared-zod arrangement as
// the follow-ups above; there is deliberately no EMPTY_* fallback because
// "no rule" is a real answer — see the comment on the schema in core.
import { IssueRecurrenceResponseSchema } from "@multica/core/api/schemas";
// Workspace Brain (notes + capture inbox + ranked search). Schemas and
// fallbacks are the shared ones in @multica/core/api/schemas — pure zod, on
// the mobile sharing whitelist — so mobile and web parse the same bytes the
// same way instead of drifting through two copies.
import {
  BrainCaptureResponseSchema,
  BrainCapturesResponseSchema,
  OrganizeBrainCaptureResponseSchema,
  WorkspaceNoteSchema,
  WorkspaceNoteSearchResponseSchema,
  WorkspaceNotesResponseSchema,
  EMPTY_BRAIN_CAPTURE,
  EMPTY_BRAIN_CAPTURES_RESPONSE,
  EMPTY_ORGANIZE_BRAIN_CAPTURE_RESPONSE,
  EMPTY_WORKSPACE_NOTE,
  EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE,
  EMPTY_WORKSPACE_NOTES_RESPONSE,
} from "@multica/core/api/schemas";
import type {
  BrainCapture,
  BrainCaptureStatus,
  BrainCapturesResponse,
  CreateBrainCaptureInput,
  CreateWorkspaceNoteInput,
  OrganizeBrainCaptureInput,
  OrganizeBrainCaptureResponse,
  UpdateWorkspaceNoteInput,
  WorkspaceNote,
  WorkspaceNoteSearchResponse,
  WorkspaceNotesResponse,
} from "@multica/core/types";

const API_URL = process.env.EXPO_PUBLIC_API_URL;

if (!API_URL) {
  throw new Error(
    "EXPO_PUBLIC_API_URL is not set. Add it to apps/mobile/.env.development.local " +
      "(see apps/mobile/.env.staging for an example).",
  );
}

export interface LoginResponse {
  token: string;
  user: User;
}

/** Mobile file payload for `uploadFile`. RN doesn't have a browser `File`
 *  object; the fetch `FormData` polyfill accepts `{ uri, name, type }`
 *  directly and streams from disk. expo-image-picker / expo-document-picker
 *  return assets that map straight onto this shape. */
export interface FileAsset {
  uri: string;
  name: string;
  type: string;
}

/** Web mirrors this from `packages/core/constants/upload.ts`. Mobile keeps
 *  its own copy per the `mirror, don't import` rule in apps/mobile/CLAUDE.md. */
const MAX_FILE_SIZE = 100 * 1024 * 1024;

/** Hard ceiling for every HTTP request. Mobile-specific because iOS may
 *  suspend a backgrounded network task without ever resolving/rejecting
 *  the JS-side fetch promise (facebook/react-native#35384). Without this
 *  timeout, a refetch fired after returning to foreground can leave the
 *  query stuck in `isRefetching` state forever (visible as the
 *  pull-to-refresh spinner never going away). 30s is generous for any
 *  reasonable Multica payload size on cellular. */
const FETCH_TIMEOUT_MS = 30_000;

export class ApiError extends Error {
  readonly status: number;
  readonly body?: unknown;
  constructor(message: string, status: number, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export interface ApiClientOptions {
  /** Called once when the server returns 401. The platform layer wires this
   *  to clear the token + navigate to /login so a stale token doesn't keep
   *  every subsequent request looping on 401. */
  onUnauthorized?: () => void;
}

class ApiClient {
  private token: string | null = null;
  private options: ApiClientOptions = {};

  setToken(token: string | null) {
    this.token = token;
  }

  setOptions(options: ApiClientOptions) {
    this.options = { ...this.options, ...options };
  }

  private async fetch<T>(
    path: string,
    init: RequestInit & { signal?: AbortSignal } = {},
  ): Promise<T> {
    const rid = createRequestId();
    const start = Date.now();
    const method = init.method ?? "GET";

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "X-Client-Platform": "mobile",
      "X-Client-OS": "ios",
      "X-Client-Version": "0.1.0",
      "X-Request-ID": rid,
      ...((init.headers as Record<string, string>) ?? {}),
    };
    if (this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }
    // Backend middleware (server/internal/middleware/workspace.go) resolves
    // slug → ws UUID and gates membership. Mirrors packages/core/api/client.ts.
    const slug = getCurrentSlug();
    if (slug && !headers["X-Workspace-Slug"]) {
      headers["X-Workspace-Slug"] = slug;
    }

    // Timeout + caller-signal forwarding.
    //
    // Hermes does NOT support AbortSignal.timeout() or AbortSignal.any() —
    // see facebook/react-native#42042 and livekit#4014. So we manually
    // compose a single controller that aborts on:
    //   (a) caller-side signal (TQ cancelling a stale/inactive query, etc),
    //   (b) 30s timeout (defends against iOS suspending the network task
    //       silently during background — fetch() then never resolves;
    //       facebook/react-native#35384). Without this, a refetch
    //       triggered by WS reconnect can leave the FlatList pull-to-refresh
    //       spinner stuck on the screen indefinitely.
    const controller = new AbortController();
    const timeoutId = setTimeout(() => {
      controller.abort(new Error(`request timed out after ${FETCH_TIMEOUT_MS}ms`));
    }, FETCH_TIMEOUT_MS);
    const callerSignal = init.signal;
    const onCallerAbort = () => controller.abort(callerSignal?.reason);
    if (callerSignal) {
      if (callerSignal.aborted) controller.abort(callerSignal.reason);
      else callerSignal.addEventListener("abort", onCallerAbort);
    }

    console.log(`[api] → ${method} ${path}`, { rid });

    let res: Response;
    try {
      res = await fetch(`${API_URL}${path}`, {
        ...init,
        signal: controller.signal,
        headers,
      });
    } catch (err) {
      clearTimeout(timeoutId);
      callerSignal?.removeEventListener("abort", onCallerAbort);
      // Re-throw with a clearer message if this was our own timeout abort.
      if (
        err instanceof Error &&
        err.name === "AbortError" &&
        !callerSignal?.aborted
      ) {
        const duration = Date.now() - start;
        console.warn(`[api] ← TIMEOUT ${path}`, {
          rid,
          duration: `${duration}ms`,
        });
        throw new ApiError(
          `Request timed out after ${FETCH_TIMEOUT_MS}ms`,
          0,
          undefined,
        );
      }
      throw err;
    }
    clearTimeout(timeoutId);
    callerSignal?.removeEventListener("abort", onCallerAbort);
    const duration = Date.now() - start;

    if (!res.ok) {
      // 401 sign-out hook: invoke once, let the platform layer (auth-store)
      // clear the token + navigate. Subsequent requests in flight will also
      // 401 and re-enter here, so the callback must be idempotent.
      if (res.status === 401) {
        this.options.onUnauthorized?.();
      }

      let body: unknown;
      try {
        body = await res.json();
      } catch {
        body = undefined;
      }
      const message =
        (body && typeof body === "object" && "message" in body
          ? String((body as { message: unknown }).message)
          : null) ?? `${res.status} ${res.statusText}`;

      const level = res.status === 404 ? "warn" : "error";
      console[level](`[api] ← ${res.status} ${path}`, {
        rid,
        duration: `${duration}ms`,
        error: message,
      });

      throw new ApiError(message, res.status, body);
    }

    console.log(`[api] ← ${res.status} ${path}`, {
      rid,
      duration: `${duration}ms`,
    });

    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  /**
   * Read-side helper: GET + zod parse + fallback in one call. Collapses
   * the boilerplate that every list/detail endpoint repeats:
   *
   *   const raw = await this.fetch<unknown>(path, { signal: opts?.signal });
   *   return parseWithFallback(raw, Schema, FALLBACK, { endpoint: "name" });
   *
   * Always uses GET (no method arg) — write endpoints that need parsing
   * still go through `this.fetch` + `parseWithFallback` directly because
   * they carry a body and care about method semantics. Use
   * `fetchValidatedWith` for those (PATCH / PUT / POST).
   *
   * The `endpoint` label defaults to the request path — override only when
   * the path has dynamic segments and you want stable telemetry labels.
   */
  private async fetchValidated<T>(
    path: string,
    schema: ZodType,
    fallback: T,
    opts?: { signal?: AbortSignal; endpoint?: string },
  ): Promise<T> {
    const raw = await this.fetch<unknown>(path, { signal: opts?.signal });
    return parseWithFallback(raw, schema, fallback, {
      endpoint: opts?.endpoint ?? path,
    });
  }

  /** Same as fetchValidated but supports any HTTP method + body. Used by
   *  PATCH/PUT/POST endpoints whose response we still want to validate
   *  (e.g. updateMe returns User, updateNotificationPreferences returns
   *  NotificationPreferenceResponse). */
  private async fetchValidatedWith<T>(
    path: string,
    schema: ZodType,
    fallback: T,
    init: RequestInit,
    opts?: { signal?: AbortSignal; endpoint?: string },
  ): Promise<T> {
    // `opts.signal` wins if both are passed, but absent opts.signal does
    // NOT clear init.signal — important because forgetting `?? init.signal`
    // would silently strip a caller's abort signal when they used the
    // RequestInit shape but no opts.
    const raw = await this.fetch<unknown>(path, {
      ...init,
      signal: opts?.signal ?? init.signal ?? undefined,
    });
    return parseWithFallback(raw, schema, fallback, {
      endpoint: opts?.endpoint ?? `${init.method ?? "GET"} ${path}`,
    });
  }

  // --- Auth ---
  async sendCode(email: string): Promise<void> {
    await this.fetch<void>("/auth/send-code", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }

  async verifyCode(email: string, code: string): Promise<LoginResponse> {
    return this.fetch<LoginResponse>("/auth/verify-code", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    });
  }

  // Mobile push (K64): this device's Expo token.
  async registerPushToken(token: string, platform: "ios" | "android"): Promise<void> {
    await this.fetch<unknown>("/api/me/push-token", { method: "PUT", body: JSON.stringify({ token, platform }) });
  }

  async unregisterPushToken(token: string): Promise<void> {
    await this.fetch<unknown>("/api/me/push-token", { method: "DELETE", body: JSON.stringify({ token }) });
  }

  async getMe(opts?: { signal?: AbortSignal }): Promise<User> {
    return this.fetchValidated(
      "/api/me",
      UserSchema,
      EMPTY_USER,
      { ...opts, endpoint: "getMe" },
    );
  }

  async getConfig(opts?: { signal?: AbortSignal }): Promise<AppConfigResponse> {
    return this.fetchValidated<AppConfigResponse>(
      "/api/config",
      AppConfigSchema,
      EMPTY_APP_CONFIG,
      { ...opts, endpoint: "getConfig" },
    );
  }

  async getWorkspaceSubscriptionSummary(opts?: {
    signal?: AbortSignal;
  }): Promise<WorkspaceSubscriptionSummary | null> {
    return this.fetchValidated<WorkspaceSubscriptionSummary | null>(
      "/api/cloud-subscriptions/summary",
      WorkspaceSubscriptionSummarySchema,
      null,
      { ...opts, endpoint: "getWorkspaceSubscriptionSummary" },
    );
  }

  // PATCH /api/me — name, avatar_url, language. Server returns the updated
  // user; we parse so a partial drift doesn't bleed into the auth store.
  async updateMe(data: UpdateMeRequest): Promise<User> {
    return this.fetchValidatedWith(
      "/api/me",
      UserSchema,
      EMPTY_USER,
      { method: "PATCH", body: JSON.stringify(data) },
      { endpoint: "updateMe" },
    );
  }

  // --- Notification preferences ---
  async getNotificationPreferences(
    opts?: { signal?: AbortSignal },
  ): Promise<NotificationPreferenceResponse> {
    return this.fetchValidated(
      "/api/notification-preferences",
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCES,
      { ...opts, endpoint: "getNotificationPreferences" },
    );
  }

  async updateNotificationPreferences(
    preferences: NotificationPreferences,
    workspaceSlug?: string,
  ): Promise<NotificationPreferenceResponse> {
    return this.fetchValidatedWith(
      "/api/notification-preferences",
      NotificationPreferenceResponseSchema,
      EMPTY_NOTIFICATION_PREFERENCES,
      {
        method: "PATCH",
        headers: workspaceSlug
          ? { "X-Workspace-Slug": workspaceSlug }
          : undefined,
        body: JSON.stringify({ preferences }),
      },
      { endpoint: "updateNotificationPreferences" },
    );
  }

  // --- Workspaces ---
  async listWorkspaces(opts?: {
    signal?: AbortSignal;
  }): Promise<Workspace[]> {
    const raw = await this.fetch<unknown>("/api/workspaces", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, WorkspaceListSchema, EMPTY_WORKSPACE_LIST, {
      endpoint: "listWorkspaces",
    });
  }

  // --- Inbox ---
  // Inbox zero (K63): the decisions waiting for me, five at most plus the total.
  async listInboxDecisions(opts?: { signal?: AbortSignal }): Promise<InboxDecisions> {
    const raw = await this.fetch<unknown>("/api/inbox/decisions", { signal: opts?.signal });
    return parseWithFallback(raw, InboxDecisionsSchema, { decisions: [], total: 0 } as InboxDecisions, {
      endpoint: "listInboxDecisions",
    });
  }

  // Decision Cards (K01): the same respond endpoint web uses.
  async respondIssueDecision(
    issueId: string,
    decisionId: string,
    answer: { option_id?: string; modified_text?: string },
  ): Promise<void> {
    await this.fetch<unknown>(
      `/api/issues/${encodeURIComponent(issueId)}/decisions/${encodeURIComponent(decisionId)}/respond`,
      { method: "POST", body: JSON.stringify(answer) },
    );
  }

  async listInbox(opts?: { signal?: AbortSignal }): Promise<InboxItem[]> {
    const raw = await this.fetch<unknown>("/api/inbox", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, InboxListSchema, EMPTY_INBOX_LIST, {
      endpoint: "listInbox",
    });
  }

  async markInboxRead(id: string): Promise<InboxItem> {
    return this.fetch<InboxItem>(`/api/inbox/${id}/read`, { method: "POST" });
  }

  // Archive endpoints — write surface. Match web's surface in
  // packages/core/api/client.ts:981-1003. No parseWithFallback (mirrors
  // markInboxRead above and the project write endpoints): a malformed
  // archive response should surface naturally so the optimistic patch
  // rolls back.
  async archiveInbox(id: string): Promise<InboxItem> {
    return this.fetch<InboxItem>(`/api/inbox/${id}/archive`, { method: "POST" });
  }

  async markAllInboxRead(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/mark-all-read", {
      method: "POST",
    });
  }

  async archiveAllInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-all", {
      method: "POST",
    });
  }

  async archiveAllReadInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-all-read", {
      method: "POST",
    });
  }

  async archiveCompletedInbox(): Promise<{ count: number }> {
    return this.fetch<{ count: number }>("/api/inbox/archive-completed", {
      method: "POST",
    });
  }

  // --- Members & Agents (for actor name/avatar lookup) ---
  async listMembers(
    workspaceId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<MemberWithUser[]> {
    const raw = await this.fetch<unknown>(
      `/api/workspaces/${workspaceId}/members`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, MemberListSchema, EMPTY_MEMBER_LIST, {
      endpoint: "listMembers",
    });
  }

  async listAgents(opts?: { signal?: AbortSignal }): Promise<Agent[]> {
    const raw = await this.fetch<unknown>("/api/agents", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, AgentListSchema, EMPTY_AGENT_LIST, {
      endpoint: "listAgents",
    });
  }

  // Workspace runtimes — feeds the presence dot's availability dimension
  // (runtime.status + last_seen_at). Backend route registered in
  // server/cmd/server/router.go:514 (GET /api/runtimes).
  async listRuntimes(opts?: { signal?: AbortSignal }): Promise<RuntimeDevice[]> {
    const raw = await this.fetch<unknown>("/api/runtimes", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, RuntimeListSchema, EMPTY_RUNTIME_LIST, {
      endpoint: "listRuntimes",
    });
  }

  // Workspace-wide active agent tasks + each agent's most recent terminal —
  // feeds the workload dimension of presence (currently unused in the mobile
  // dot; reserved for the P1 long-press peek sheet). Listed here now so the
  // realtime invalidation path can be wired in one PR. Backend route at
  // server/cmd/server/router.go:539 (GET /api/agent-task-snapshot).
  async listAgentTaskSnapshot(
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    const raw = await this.fetch<unknown>("/api/agent-task-snapshot", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, AgentTaskListSchema, EMPTY_AGENT_TASK_LIST, {
      endpoint: "listAgentTaskSnapshot",
    });
  }

  // --- Run replay (k70) ---
  // GET /api/tasks/{taskId}/replay?cursor=N&limit=N — one page of the
  // hash-chained event log. Read-only on mobile; the resume endpoint is
  // web/desktop only. A null return covers both 404 and an untrusted shape.
  async getTaskReplay(
    taskId: string,
    params: { cursor?: number; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<RunReplay | null> {
    const search = new URLSearchParams();
    if (params.cursor !== undefined) search.set("cursor", String(params.cursor));
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    const qs = search.toString();
    return this.fetchValidated<RunReplay | null>(
      `/api/tasks/${encodeURIComponent(taskId)}/replay${qs ? `?${qs}` : ""}`,
      RunReplaySchema,
      null,
      { ...opts, endpoint: "GET /api/tasks/:id/replay" },
    );
  }

  async listSquads(opts?: { signal?: AbortSignal }): Promise<Squad[]> {
    const raw = await this.fetch<unknown>("/api/squads", {
      signal: opts?.signal,
    });
    return parseWithFallback(raw, SquadListSchema, EMPTY_SQUAD_LIST, {
      endpoint: "listSquads",
    });
  }

  // --- Triage queue (M2) ---
  // Same wire contract as web `packages/core/api/client.ts` (getTriageStats /
  // listTriageItems / acceptTriageItem / dismissTriageItem /
  // reopenTriageItem). Schemas + fallbacks come from
  // @multica/core/api/schemas — pure zod, on the mobile sharing whitelist —
  // so a backend field drift degrades identically on both platforms.
  async getTriageStats(opts?: { signal?: AbortSignal }): Promise<TriageStats> {
    return this.fetchValidated<TriageStats>(
      "/api/triage/stats",
      TriageStatsSchema,
      EMPTY_TRIAGE_STATS,
      { ...opts, endpoint: "GET /api/triage/stats" },
    );
  }

  async listTriageItems(
    params?: { state?: TriageItemState; limit?: number; cursor?: string },
    opts?: { signal?: AbortSignal },
  ): Promise<TriageItemsResponse> {
    const search = new URLSearchParams();
    if (params?.state) search.set("state", params.state);
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    if (params?.cursor) search.set("cursor", params.cursor);
    const qs = search.toString();
    return this.fetchValidated<TriageItemsResponse>(
      `/api/triage/items${qs ? `?${qs}` : ""}`,
      TriageItemsResponseSchema,
      EMPTY_TRIAGE_ITEMS_RESPONSE,
      { ...opts, endpoint: "GET /api/triage/items" },
    );
  }

  async acceptTriageItem(itemId: string): Promise<AcceptTriageItemResponse> {
    return this.fetchValidatedWith<AcceptTriageItemResponse>(
      `/api/triage/items/${encodeURIComponent(itemId)}/accept`,
      AcceptTriageItemResponseSchema,
      { item_id: itemId, state: "accepted" },
      { method: "POST" },
      { endpoint: "POST /api/triage/items/:id/accept" },
    );
  }

  async dismissTriageItem(itemId: string): Promise<DismissTriageItemResponse> {
    return this.fetchValidatedWith<DismissTriageItemResponse>(
      `/api/triage/items/${encodeURIComponent(itemId)}/dismiss`,
      DismissTriageItemResponseSchema,
      { item_id: itemId, state: "dismissed" },
      { method: "POST" },
      { endpoint: "POST /api/triage/items/:id/dismiss" },
    );
  }

  /** Sends a dismissed item back to `pending`. Response body is unused. */
  async reopenTriageItem(itemId: string): Promise<void> {
    await this.fetch<unknown>(
      `/api/triage/items/${encodeURIComponent(itemId)}/reopen`,
      { method: "POST" },
    );
  }

  // --- Postmortems (k68) ---
  // Same wire contract as web `packages/core/api/client.ts`. Approve/discard
  // return the updated postmortem (approve also carries `applied_rules`, the
  // number of preventive rules copied into the agent's memory), so both are
  // parsed rather than fire-and-forget.
  async getPostmortemStats(opts?: {
    signal?: AbortSignal;
  }): Promise<PostmortemStats> {
    return this.fetchValidated<PostmortemStats>(
      "/api/postmortems/stats",
      PostmortemStatsSchema,
      EMPTY_POSTMORTEM_STATS,
      { ...opts, endpoint: "GET /api/postmortems/stats" },
    );
  }

  async listPostmortems(
    params?: { state?: PostmortemState; limit?: number; cursor?: string },
    opts?: { signal?: AbortSignal },
  ): Promise<PostmortemsResponse> {
    const search = new URLSearchParams();
    if (params?.state) search.set("state", params.state);
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    if (params?.cursor) search.set("cursor", params.cursor);
    const qs = search.toString();
    return this.fetchValidated<PostmortemsResponse>(
      `/api/postmortems${qs ? `?${qs}` : ""}`,
      PostmortemsResponseSchema,
      EMPTY_POSTMORTEMS_RESPONSE,
      { ...opts, endpoint: "GET /api/postmortems" },
    );
  }

  async getPostmortem(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Postmortem | null> {
    return this.fetchValidated<Postmortem | null>(
      `/api/postmortems/${encodeURIComponent(id)}`,
      PostmortemSchema,
      null,
      { ...opts, endpoint: "GET /api/postmortems/:id" },
    );
  }

  // ── Undo for agent actions (K69) ────────────────────────────────────
  // Mirrors packages/core/api/client.ts listIssueAgentEffects / undoTask /
  // undoAgentEffect.

  async listIssueAgentEffects(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentEffectList> {
    return this.fetchValidated<AgentEffectList>(
      `/api/issues/${encodeURIComponent(issueId)}/agent-effects`,
      AgentEffectListSchema,
      EMPTY_AGENT_EFFECT_LIST,
      { ...opts, endpoint: "GET /api/issues/:id/agent-effects" },
    );
  }

  async undoTask(taskId: string): Promise<UndoReport> {
    return this.fetchValidatedWith<UndoReport>(
      `/api/tasks/${encodeURIComponent(taskId)}/undo`,
      UndoReportSchema,
      EMPTY_UNDO_REPORT,
      { method: "POST" },
      { endpoint: "POST /api/tasks/:id/undo" },
    );
  }

  async undoAgentEffect(effectId: string): Promise<UndoReport> {
    return this.fetchValidatedWith<UndoReport>(
      `/api/agent-effects/${encodeURIComponent(effectId)}/undo`,
      UndoReportSchema,
      EMPTY_UNDO_REPORT,
      { method: "POST" },
      { endpoint: "POST /api/agent-effects/:id/undo" },
    );
  }

  // ── Goal loop — mirrors server/internal/handler/issue_goal.go ──────
  // Every endpoint answers `{goal: State | null}`; schema/fallback are
  // mobile-local (see EMPTY_ISSUE_GOAL_RESPONSE comment in ./schemas).

  async getIssueGoal(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<IssueGoalResponse> {
    return this.fetchValidated<IssueGoalResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/goal`,
      IssueGoalResponseSchema,
      EMPTY_ISSUE_GOAL_RESPONSE,
      { ...opts, endpoint: "GET /api/issues/:id/goal" },
    );
  }

  // Server validates (server/internal/service/goal_loop.go SetGoal): goal
  // <= 6000 chars, max_continuations in [1,20] → 400 with {"error": "..."}
  // otherwise. Mobile mirrors the same bounds client-side for instant
  // feedback (lib/issue-goal-display.ts issueGoalFormError) but this is the
  // authoritative check.
  async setIssueGoal(
    issueId: string,
    body: { goal: string; max_continuations?: number },
  ): Promise<IssueGoalResponse> {
    return this.fetchValidatedWith<IssueGoalResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/goal`,
      IssueGoalResponseSchema,
      EMPTY_ISSUE_GOAL_RESPONSE,
      { method: "PUT", body: JSON.stringify(body) },
      { endpoint: "PUT /api/issues/:id/goal" },
    );
  }

  async pauseIssueGoal(issueId: string): Promise<IssueGoalResponse> {
    return this.fetchValidatedWith<IssueGoalResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/goal/pause`,
      IssueGoalResponseSchema,
      EMPTY_ISSUE_GOAL_RESPONSE,
      { method: "POST" },
      { endpoint: "POST /api/issues/:id/goal/pause" },
    );
  }

  async resumeIssueGoal(issueId: string): Promise<IssueGoalResponse> {
    return this.fetchValidatedWith<IssueGoalResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/goal/resume`,
      IssueGoalResponseSchema,
      EMPTY_ISSUE_GOAL_RESPONSE,
      { method: "POST" },
      { endpoint: "POST /api/issues/:id/goal/resume" },
    );
  }

  // 409 (nothing waiting) surfaces as ApiError with status 409 — the caller
  // (useAnswerIssueGoal) branches on that instead of a generic error toast.
  async answerIssueGoal(
    issueId: string,
    answer: string,
  ): Promise<IssueGoalResponse> {
    return this.fetchValidatedWith<IssueGoalResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/goal/answer`,
      IssueGoalResponseSchema,
      EMPTY_ISSUE_GOAL_RESPONSE,
      { method: "POST", body: JSON.stringify({ answer }) },
      { endpoint: "POST /api/issues/:id/goal/answer" },
    );
  }

  // ── Inline approvals — mirrors packages/core/api/client.ts listApprovals /
  // decideIssueTransitionRequest. Schema is mobile-local (see the Inline
  // approvals section of ./schemas — @multica/core/approvals is not on the
  // mobile sharing whitelist, see the comment there).

  /** Every pending ask, or one issue's, from the unified approvals feed. */
  async listApprovals(
    issueId?: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ApprovalsResponse> {
    const query = issueId ? `?issue_id=${encodeURIComponent(issueId)}` : "";
    return this.fetchValidated<ApprovalsResponse>(
      `/api/approvals${query}`,
      ApprovalsResponseSchema,
      EMPTY_APPROVALS,
      { ...opts, endpoint: "GET /api/approvals" },
    );
  }

  // Transition gate (F28): approve/reject a status change held for
  // approval. Mirrors packages/core/api/client.ts decideIssueTransitionRequest.
  async decideIssueTransitionRequest(
    requestId: string,
    decision: "approve" | "reject",
    note?: string,
  ): Promise<void> {
    await this.fetch(
      `/api/issue-transition-requests/${encodeURIComponent(requestId)}/${decision}`,
      { method: "POST", body: JSON.stringify({ note: note ?? "" }) },
    );
  }

  // ── Runs fleet (OS plan, chantier 4) — mirrors
  // server/internal/handler/runs.go / apps/docs/content/docs/runs.mdx.
  // Schema is mobile-local for the same reason as Inline approvals above.

  /** One page of the fleet, newest first, plus the header counts. */
  async listRuns(
    params: { state?: "active" | "terminal" | "all"; cursor?: string; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<RunsResponse> {
    const search = new URLSearchParams();
    if (params.state) search.set("state", params.state);
    if (params.cursor) search.set("cursor", params.cursor);
    if (params.limit !== undefined) search.set("limit", String(params.limit));
    const qs = search.toString();
    return this.fetchValidated<RunsResponse>(
      `/api/runs${qs ? `?${qs}` : ""}`,
      RunsResponseSchema,
      EMPTY_RUNS_RESPONSE,
      { ...opts, endpoint: "GET /api/runs" },
    );
  }

  /** Stops each run and reports every outcome — a cancel of many never
   *  gives up because one of them could not stop. */
  async cancelRuns(taskIds: string[]): Promise<CancelRunsResponse> {
    return this.fetchValidatedWith<CancelRunsResponse>(
      "/api/runs/cancel",
      CancelRunsResponseSchema,
      EMPTY_CANCEL_RUNS_RESPONSE,
      { method: "POST", body: JSON.stringify({ task_ids: taskIds }) },
      { endpoint: "POST /api/runs/cancel" },
    );
  }

  /** Owner/admin only: halts the fleet, then cancels every run in flight. */
  async killSwitch(reason: string): Promise<KillSwitchResponse> {
    return this.fetchValidatedWith<KillSwitchResponse>(
      "/api/runs/kill-switch",
      KillSwitchResponseSchema,
      EMPTY_KILL_SWITCH_RESPONSE,
      { method: "POST", body: JSON.stringify({ reason }) },
      { endpoint: "POST /api/runs/kill-switch" },
    );
  }

  /** Lifts (or would set) the halt alone, with no cancellation. The Runs
   *  banner only ever calls this with `halted: false` — setting the halt
   *  is the kill switch's job so a halt is never left without the cancel
   *  sweep that makes it safe to leave on. */
  async putRunHalt(input: { halted: boolean; reason: string }): Promise<RunHalt> {
    return this.fetchValidatedWith<RunHalt>(
      "/api/run-halt",
      RunHaltSchema,
      EMPTY_RUN_HALT,
      { method: "PUT", body: JSON.stringify(input) },
      { endpoint: "PUT /api/run-halt" },
    );
  }

  async approvePostmortem(id: string): Promise<Postmortem | null> {
    return this.fetchValidatedWith<Postmortem | null>(
      `/api/postmortems/${encodeURIComponent(id)}/approve`,
      PostmortemSchema,
      null,
      { method: "POST" },
      { endpoint: "POST /api/postmortems/:id/approve" },
    );
  }

  async discardPostmortem(id: string): Promise<Postmortem | null> {
    return this.fetchValidatedWith<Postmortem | null>(
      `/api/postmortems/${encodeURIComponent(id)}/discard`,
      PostmortemSchema,
      null,
      { method: "POST" },
      { endpoint: "POST /api/postmortems/:id/discard" },
    );
  }

  // --- Meetings ---
  // Read + manage only. Recording (POST /api/meetings, /segments, /finish) is
  // deliberately absent: capturing audio is out of scope for the mobile app,
  // which reads meetings recorded from web/desktop and turns their action
  // items into triage work.
  async listMeetings(
    params?: { limit?: number; offset?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<MeetingListResponse> {
    const search = new URLSearchParams();
    if (params?.limit !== undefined) search.set("limit", String(params.limit));
    if (params?.offset !== undefined)
      search.set("offset", String(params.offset));
    const qs = search.toString();
    return this.fetchValidated<MeetingListResponse>(
      `/api/meetings${qs ? `?${qs}` : ""}`,
      MeetingListResponseSchema,
      EMPTY_MEETING_LIST,
      { ...opts, endpoint: "GET /api/meetings" },
    );
  }

  async getMeeting(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Meeting> {
    return this.fetchValidated<Meeting>(
      `/api/meetings/${encodeURIComponent(id)}`,
      MeetingSchema,
      EMPTY_MEETING,
      { ...opts, endpoint: "GET /api/meetings/:id" },
    );
  }

  /** Renames a meeting. Title is the only mutable field. */
  async updateMeeting(id: string, data: { title: string }): Promise<Meeting> {
    return this.fetchValidatedWith<Meeting>(
      `/api/meetings/${encodeURIComponent(id)}`,
      MeetingSchema,
      EMPTY_MEETING,
      { method: "PATCH", body: JSON.stringify(data) },
      { endpoint: "PATCH /api/meetings/:id" },
    );
  }

  // --- Native calendar (OS plan, chantier 19) ---
  // See apps/mobile/data/schemas.ts for why these schemas are mobile-local
  // rather than @multica/core/api/schemas.

  async getCalendarAgenda(
    from: string,
    to: string,
    opts?: { signal?: AbortSignal },
  ): Promise<CalendarAgenda> {
    const search = new URLSearchParams({ from, to });
    return this.fetchValidated<CalendarAgenda>(
      `/api/calendar/agenda?${search.toString()}`,
      CalendarAgendaSchema,
      EMPTY_CALENDAR_AGENDA,
      { ...opts, endpoint: "GET /api/calendar/agenda" },
    );
  }

  async getCalendarEvent(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<CalendarEventEntry | null> {
    const raw = await this.fetch<unknown>(
      `/api/calendar/events/${encodeURIComponent(id)}`,
      { signal: opts?.signal },
    );
    const parsed = parseWithFallback<{ event: CalendarEventEntry } | null>(raw, CalendarEventResponseSchema, null, {
      endpoint: "GET /api/calendar/events/:id",
    });
    return parsed?.event ?? null;
  }

  async createCalendarEvent(body: CalendarEventInput): Promise<CalendarEventEntry | null> {
    const raw = await this.fetch<unknown>("/api/calendar/events", {
      method: "POST",
      body: JSON.stringify(body),
    });
    const parsed = parseWithFallback<{ event: CalendarEventEntry } | null>(raw, CalendarEventResponseSchema, null, {
      endpoint: "POST /api/calendar/events",
    });
    return parsed?.event ?? null;
  }

  async respondCalendarEvent(
    id: string,
    response: "accepted" | "declined" | "tentative",
  ): Promise<CalendarEventEntry | null> {
    const raw = await this.fetch<unknown>(
      `/api/calendar/events/${encodeURIComponent(id)}/respond`,
      { method: "POST", body: JSON.stringify({ response }) },
    );
    const parsed = parseWithFallback<{ event: CalendarEventEntry } | null>(raw, CalendarEventResponseSchema, null, {
      endpoint: "POST /api/calendar/events/:id/respond",
    });
    return parsed?.event ?? null;
  }

  /** DELETE cancels rather than erasing — 204, nothing to parse. */
  async cancelCalendarEvent(id: string): Promise<void> {
    await this.fetch<void>(`/api/calendar/events/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  // --- Follow-ups / autopilots from a sentence (JEF-373) ---
  // A follow-up is a deferred run of the issue's agent
  // (server/internal/handler/followups.go); an autopilot proposal is a
  // paused automation behind a Decision Card
  // (server/internal/handler/autopilot_draft.go).

  async listIssueFollowups(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<IssueFollowupsResponse> {
    return this.fetchValidated<IssueFollowupsResponse>(
      `/api/issues/${encodeURIComponent(issueId)}/followups`,
      IssueFollowupsResponseSchema,
      EMPTY_ISSUE_FOLLOWUPS,
      { ...opts, endpoint: "GET /api/issues/:id/followups" },
    );
  }

  /** `when` is RFC 3339 or "+<minutes>"; `agent_id` is required unless the
   *  issue is already assigned to an agent. A 429 carries the budget
   *  sentence the sheet shows inline. */
  async scheduleIssueFollowup(
    issueId: string,
    body: ScheduleFollowupInput,
  ): Promise<{ followup: Followup }> {
    return this.fetchValidatedWith<{ followup: Followup }>(
      `/api/issues/${encodeURIComponent(issueId)}/followups`,
      FollowupResponseSchema,
      { followup: EMPTY_FOLLOWUP },
      { method: "POST", body: JSON.stringify(body) },
      { endpoint: "POST /api/issues/:id/followups" },
    );
  }

  /** 204 on success; 409 when it already fired or was cancelled. */
  async cancelIssueFollowup(issueId: string, followupId: string): Promise<void> {
    await this.fetch<void>(
      `/api/issues/${encodeURIComponent(issueId)}/followups/${encodeURIComponent(followupId)}`,
      { method: "DELETE" },
    );
  }

  // --- Recurring issues (OS plan, table stakes) ---
  // The rule lives on the source issue and every occurrence carries it, so
  // the same GET from any member of the series answers with the same rule
  // (server/internal/handler/issue_recurrence.go).

  /**
   * The rule of the series this issue belongs to, or null when it doesn't
   * recur. The 404 the server sends for that case is not an error condition —
   * it is the answer — so it is caught here rather than turned into a query
   * error that would put a red state on an issue with nothing wrong with it.
   * Any other status still throws.
   */
  async getIssueRecurrence(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<IssueRecurrenceResponse | null> {
    try {
      return await this.fetchValidated<IssueRecurrenceResponse | null>(
        `/api/issues/${encodeURIComponent(issueId)}/recurrence`,
        IssueRecurrenceResponseSchema,
        null,
        { ...opts, endpoint: "GET /api/issues/:id/recurrence" },
      );
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      throw err;
    }
  }

  /** Creates the rule on this issue, or updates the rule of its series.
   *  400 for a bad cron / timezone / mode, 403 for a non-member (an agent
   *  run's task token). Answers with the same payload as the GET. */
  async setIssueRecurrence(
    issueId: string,
    body: SetIssueRecurrenceInput,
  ): Promise<IssueRecurrenceResponse | null> {
    return this.fetchValidatedWith<IssueRecurrenceResponse | null>(
      `/api/issues/${encodeURIComponent(issueId)}/recurrence`,
      IssueRecurrenceResponseSchema,
      null,
      { method: "PUT", body: JSON.stringify(body) },
      { endpoint: "PUT /api/issues/:id/recurrence" },
    );
  }

  /** 204 — the series stops and past occurrences stay as ordinary issues. */
  async clearIssueRecurrence(issueId: string): Promise<void> {
    await this.fetch<void>(
      `/api/issues/${encodeURIComponent(issueId)}/recurrence`,
      { method: "DELETE" },
    );
  }

  /** Writes nothing — 503 when the workspace has no model configured. */
  async draftAutopilot(
    body: DraftAutopilotInput,
  ): Promise<{ draft: AutopilotDraft }> {
    return this.fetchValidatedWith<{ draft: AutopilotDraft }>(
      "/api/autopilots/draft",
      AutopilotDraftResponseSchema,
      { draft: EMPTY_AUTOPILOT_DRAFT },
      { method: "POST", body: JSON.stringify(body) },
      { endpoint: "POST /api/autopilots/draft" },
    );
  }

  /** Files the autopilot paused; with `issue_id` a Decision Card lands on
   *  that issue and its id comes back as `decision_id`. */
  async proposeAutopilot(
    body: ProposeAutopilotInput,
  ): Promise<AutopilotProposalResponse> {
    return this.fetchValidatedWith<AutopilotProposalResponse>(
      "/api/autopilots/propose",
      AutopilotProposalResponseSchema,
      EMPTY_AUTOPILOT_PROPOSAL,
      { method: "POST", body: JSON.stringify(body) },
      { endpoint: "POST /api/autopilots/propose" },
    );
  }

  // --- Workspace doctrine (OS plan, chantier 22) ---
  // Read + review + resolve only: writing the doctrine (PUT, restore) stays
  // on web/desktop, so no publish method here.

  async getDoctrine(opts?: { signal?: AbortSignal }): Promise<Doctrine> {
    return this.fetchValidated<Doctrine>(
      "/api/workspace/doctrine",
      DoctrineSchema,
      EMPTY_DOCTRINE,
      { ...opts, endpoint: "GET /api/workspace/doctrine" },
    );
  }

  async listDoctrineVersions(
    params?: { cursor?: string | null; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<DoctrineVersionsResponse> {
    const search = new URLSearchParams();
    if (params?.cursor) search.set("cursor", params.cursor);
    if (params?.limit) search.set("limit", String(params.limit));
    const query = search.toString();
    return this.fetchValidated<DoctrineVersionsResponse>(
      `/api/workspace/doctrine/versions${query ? `?${query}` : ""}`,
      DoctrineVersionsResponseSchema,
      EMPTY_DOCTRINE_VERSIONS,
      { ...opts, endpoint: "GET /api/workspace/doctrine/versions" },
    );
  }

  /** Compares a version with what came before it (the live doctrine for a
   *  proposal, the previous revision for an activated one). */
  async getDoctrineVersionDiff(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<DoctrineDiff> {
    return this.fetchValidated<DoctrineDiff>(
      `/api/workspace/doctrine/versions/${encodeURIComponent(id)}/diff`,
      DoctrineDiffSchema,
      EMPTY_DOCTRINE_DIFF,
      { ...opts, endpoint: "GET /api/workspace/doctrine/versions/:id/diff" },
    );
  }

  async listDoctrineReports(
    status: "open" | "acknowledged" | "dismissed" | "all",
    opts?: { signal?: AbortSignal },
  ): Promise<DoctrineReportsResponse> {
    return this.fetchValidated<DoctrineReportsResponse>(
      `/api/workspace/doctrine/reports?status=${status}`,
      DoctrineReportsResponseSchema,
      EMPTY_DOCTRINE_REPORTS,
      { ...opts, endpoint: "GET /api/workspace/doctrine/reports" },
    );
  }

  /** Approve / reject a pending revision. The response carries the doctrine
   *  and the reviewed version, but the caller invalidates rather than
   *  patches (the review moves the live revision, the ledger and the inbox
   *  at once), so nothing here reaches a render path unparsed. */
  async reviewDoctrineVersion(
    id: string,
    decision: "approve" | "reject",
    note?: string,
  ): Promise<void> {
    await this.fetch<void>(
      `/api/workspace/doctrine/versions/${encodeURIComponent(id)}/${decision}`,
      { method: "POST", body: JSON.stringify({ note: note ?? "" }) },
    );
  }

  /** Acknowledge / dismiss a doctrine report. Same reasoning as above. */
  async resolveDoctrineReport(
    id: string,
    resolution: "acknowledge" | "dismiss",
    note?: string,
  ): Promise<void> {
    await this.fetch<void>(
      `/api/workspace/doctrine/reports/${encodeURIComponent(id)}/${resolution}`,
      { method: "POST", body: JSON.stringify({ note: note ?? "" }) },
    );
  }

  // --- Packs (OS plan, vague B) ---
  // Read + preview + install + uninstall. Upload and export stay on
  // web/desktop: both are file-system flows (pick a .yaml, save a download)
  // that have no phone equivalent worth the surface.

  async listPacks(opts?: { signal?: AbortSignal }): Promise<PackCatalogue> {
    return this.fetchValidated<PackCatalogue>(
      "/api/packs",
      PackCatalogueSchema,
      EMPTY_PACK_CATALOGUE,
      { ...opts, endpoint: "GET /api/packs" },
    );
  }

  async getPack(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<PackDetail> {
    return this.fetchValidated<PackDetail>(
      `/api/packs/${encodeURIComponent(id)}`,
      PackDetailSchema,
      EMPTY_PACK_DETAIL,
      { ...opts, endpoint: "GET /api/packs/:id" },
    );
  }

  async listPackInstalls(opts?: {
    signal?: AbortSignal;
  }): Promise<PackInstallList> {
    return this.fetchValidated<PackInstallList>(
      "/api/packs/installed",
      PackInstallListSchema,
      EMPTY_PACK_INSTALL_LIST,
      { ...opts, endpoint: "GET /api/packs/installed" },
    );
  }

  async getPackInstall(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<PackInstallDetail> {
    return this.fetchValidated<PackInstallDetail>(
      `/api/packs/installed/${encodeURIComponent(id)}`,
      PackInstallDetailSchema,
      EMPTY_PACK_INSTALL_DETAIL,
      { ...opts, endpoint: "GET /api/packs/installed/:id" },
    );
  }

  /** Dry run: collisions, problems, the strategy the server would pick, and
   *  `blocked` when this pack cannot be installed at all. */
  async previewPack(
    id: string,
    strategy?: PackStrategy,
  ): Promise<PackPreview> {
    return this.fetchValidatedWith<PackPreview>(
      `/api/packs/${encodeURIComponent(id)}/preview`,
      PackPreviewSchema,
      EMPTY_PACK_PREVIEW,
      { method: "POST", body: JSON.stringify({ strategy: strategy ?? "" }) },
      { endpoint: "POST /api/packs/:id/preview" },
    );
  }

  /** 409 when the preview said blocked and `force` is not set. */
  async installPack(
    id: string,
    strategy?: PackStrategy,
    force?: boolean,
  ): Promise<PackInstallResult> {
    return this.fetchValidatedWith<PackInstallResult>(
      `/api/packs/${encodeURIComponent(id)}/install`,
      PackInstallResultSchema,
      EMPTY_PACK_INSTALL_RESULT,
      {
        method: "POST",
        body: JSON.stringify({
          strategy: strategy ?? "",
          force: force === true,
        }),
      },
      { endpoint: "POST /api/packs/:id/install" },
    );
  }

  /** Removes the configuration the pack created; the content it brought stays. */
  async uninstallPack(id: string): Promise<PackUninstallResult> {
    return this.fetchValidatedWith<PackUninstallResult>(
      `/api/packs/installed/${encodeURIComponent(id)}/uninstall`,
      PackUninstallResultSchema,
      EMPTY_PACK_UNINSTALL_RESULT,
      { method: "POST" },
      { endpoint: "POST /api/packs/installed/:id/uninstall" },
    );
  }

  /** Removes a meeting and its transcript. 204, no body. */
  async deleteMeeting(id: string): Promise<void> {
    await this.fetch<void>(`/api/meetings/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  /**
   * Replays the summary + action-item extraction for a meeting that already
   * stopped recording. 409 `meeting_recording` when it has not, and
   * `meeting_summarizing` while a finish is still running.
   */
  async resummarizeMeeting(id: string): Promise<Meeting> {
    return this.fetchValidatedWith<Meeting>(
      `/api/meetings/${encodeURIComponent(id)}/resummarize`,
      MeetingSchema,
      EMPTY_MEETING,
      { method: "POST" },
      { endpoint: "POST /api/meetings/:id/resummarize" },
    );
  }

  // --- Issues ---
  async listIssues(
    params: ListIssuesParams = {},
    opts?: { signal?: AbortSignal },
  ): Promise<ListIssuesResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      if (Array.isArray(v)) {
        // Backend parses comma-separated lists (server/internal/handler/issue.go
        // uses strings.Split on a single query value). Match web's serialization
        // in packages/core/api/client.ts:407 — repeated keys would silently
        // collapse to the first value only.
        if (v.length > 0) search.set(k, v.map(String).join(","));
      } else {
        search.set(k, String(v));
      }
    }
    const qs = search.toString();
    const raw = await this.fetch<unknown>(
      `/api/issues${qs ? `?${qs}` : ""}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, ListIssuesResponseSchema, EMPTY_LIST_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues",
    });
  }

  /** Workspace-wide issue search. Backend `GET /api/issues/search` with
   *  workspace resolved by the `X-Workspace-Slug` middleware (same as
   *  `listIssues`). Caller passes its own `AbortController.signal` so the
   *  search modal can cancel an in-flight request when the user types
   *  again — see app/(app)/[workspace]/search.tsx. */
  async searchIssues(
    params: { q: string; limit?: number; include_closed?: boolean; offset?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<SearchIssuesResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      search.set(k, String(v));
    }
    const raw = await this.fetch<unknown>(
      `/api/issues/search?${search.toString()}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, SearchIssuesResponseSchema, EMPTY_SEARCH_ISSUES_RESPONSE, {
      endpoint: "GET /api/issues/search",
    });
  }

  async getIssue(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Issue> {
    return this.fetchValidated(
      `/api/issues/${id}`,
      IssueSchema,
      EMPTY_ISSUE_FALLBACK,
      { ...opts, endpoint: "getIssue" },
    );
  }

  // Write endpoint — mirrors POST /api/issues
  // (server/cmd/server/router.go:320, server/internal/handler/issue.go
  // CreateIssue). Mobile sends only the fields the form fills in; backend
  // applies its own defaults for anything omitted.
  // Voice-dictated issue draft (K36): a transcript in, an editable draft
  // out. Creates nothing — the draft screen calls createIssue afterwards.
  async issueDraftFromVoice(transcript: string): Promise<VoiceIssueDraft> {
    return this.fetchValidatedWith(
      "/api/issues/from-voice-transcript",
      VoiceIssueDraftSchema,
      EMPTY_VOICE_ISSUE_DRAFT,
      { method: "POST", body: JSON.stringify({ transcript }) },
      { endpoint: "issueDraftFromVoice" },
    );
  }

  async createIssue(body: CreateIssueRequest): Promise<Issue> {
    return this.fetch<Issue>("/api/issues", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // Timeline returns the full ASC entry list in one shot — server-side
  // pagination was dropped in #2322 (p99 ~30 entries per issue, cursors
  // were pure overhead and split reply threads at page boundaries).
  // Call WITHOUT pagination params: the legacy `limit/before/after/around`
  // path returns the old wrapped shape for back-compat, which mobile must
  // NOT trigger. See server/internal/handler/activity.go:60-69.
  async listTimeline(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TimelineEntry[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/timeline`,
      TimelineEntriesSchema,
      EMPTY_TIMELINE_ENTRIES,
      { ...opts, endpoint: "GET /api/issues/:id/timeline" },
    );
  }

  // GET /api/issues/:id/attachments — list of file attachments hooked to
  // the issue (or its comments). Mobile uses this to resolve `mc://file/<id>`
  // markdown image URIs to their `download_url` HTTPS endpoint; without it,
  // iOS image loader doesn't understand the mc: scheme and renders broken.
  async listAttachments(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Attachment[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/attachments`,
      AttachmentListSchema,
      EMPTY_ATTACHMENT_LIST,
      { ...opts, endpoint: "GET /api/issues/:id/attachments" },
    );
  }

  // Active tasks for an issue (status in queued/dispatched/running). Returns
  // the inner `tasks` array directly — handler wraps it in `{ tasks: [] }`
  // (server/internal/handler/daemon.go:1866) so the response object survives
  // future field additions without breaking the cache shape.
  async listActiveTasksForIssue(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    const parsed = await this.fetchValidated(
      `/api/issues/${issueId}/active-task`,
      ActiveTasksResponseSchema,
      EMPTY_ACTIVE_TASKS_RESPONSE,
      { ...opts, endpoint: "GET /api/issues/:id/active-task" },
    );
    return parsed.tasks;
  }

  // All tasks (any status) for an issue — drives the "Runs" history section.
  // Path is `/task-runs` (server/cmd/server/router.go:353), NOT `/tasks` —
  // the latter doesn't exist on this scope.
  async listTasksByIssue(
    issueId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentTask[]> {
    return this.fetchValidated(
      `/api/issues/${issueId}/task-runs`,
      AgentTaskListSchema,
      EMPTY_AGENT_TASK_LIST,
      { ...opts, endpoint: "GET /api/issues/:id/task-runs" },
    );
  }

  /**
   * Delivery evidence for human accept / request-changes.
   * Fail closed: malformed payloads become null (never a fake empty delivery).
   * Mirrors packages/core/api/client.ts getIssueDelivery.
   */
  async getIssueDelivery(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<IssueDelivery | null> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${encodeURIComponent(id)}/delivery`,
      { signal: opts?.signal },
    );
    return parseWithFallback<IssueDelivery | null>(
      raw,
      IssueDeliverySchema,
      null,
      { endpoint: "GET /api/issues/:id/delivery" },
    );
  }

  async updateIssueDeliveryCriteria(
    id: string,
    criteria: string[],
    expectedRevision: number,
  ): Promise<{ criteria: string[]; revision: number } | null> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${encodeURIComponent(id)}/delivery/criteria`,
      {
        method: "PUT",
        body: JSON.stringify({
          criteria,
          expected_revision: expectedRevision,
        }),
      },
    );
    return parseWithFallback<{ criteria: string[]; revision: number } | null>(
      raw,
      DeliveryCriteriaSchema,
      null,
      { endpoint: "PUT /api/issues/:id/delivery/criteria" },
    );
  }

  async reviewIssueDelivery(
    id: string,
    input: {
      reviewId: string;
      expectedReviewId: string;
      snapshotToken: string;
      decision: "accepted" | "changes_requested";
      feedback: string;
      assessments: { passed: boolean; evidence: string }[];
      humanEffortSeconds?: number | null;
    },
  ): Promise<DeliveryReview | null> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${encodeURIComponent(id)}/delivery/reviews`,
      {
        method: "POST",
        body: JSON.stringify({
          review_id: input.reviewId,
          expected_review_id: input.expectedReviewId,
          snapshot_token: input.snapshotToken,
          decision: input.decision,
          feedback: input.feedback,
          assessments: input.assessments,
          ...(input.humanEffortSeconds != null
            ? { human_effort_seconds: input.humanEffortSeconds }
            : {}),
        }),
      },
    );
    return parseWithFallback<DeliveryReview | null>(
      raw,
      DeliveryReviewSchema,
      null,
      { endpoint: "POST /api/issues/:id/delivery/reviews" },
    );
  }

  async startIssueDeliveryCorrection(
    id: string,
    reviewId: string,
  ): Promise<DeliveryCorrection | null> {
    const raw = await this.fetch<unknown>(
      `/api/issues/${encodeURIComponent(id)}/delivery/correction`,
      {
        method: "POST",
        body: JSON.stringify({ review_id: reviewId }),
      },
    );
    return parseWithFallback<DeliveryCorrection | null>(
      raw,
      DeliveryCorrectionSchema.refine((receipt) => receipt.reviewId === reviewId),
      null,
      { endpoint: "POST /api/issues/:id/delivery/correction" },
    );
  }

  async createComment(
    issueId: string,
    content: string,
    opts?: { parentId?: string; type?: string; attachmentIds?: string[] },
  ): Promise<Comment> {
    // Body shape mirrors backend `CreateCommentRequest`
    // (server/internal/handler/comment.go:165). `parent_id` is sent only
    // when present so top-level comments don't carry an explicit null.
    // `type` defaults to "comment" matching web client.ts:686.
    return this.fetchValidatedWith(
      `/api/issues/${issueId}/comments`,
      CommentSchema,
      EMPTY_COMMENT,
      {
        method: "POST",
        body: JSON.stringify({
          content,
          type: opts?.type ?? "comment",
          ...(opts?.parentId ? { parent_id: opts.parentId } : {}),
          ...(opts?.attachmentIds ? { attachment_ids: opts.attachmentIds } : {}),
        }),
      },
      { endpoint: "createComment" },
    );
  }

  // PUT /api/comments/:id — content edit (+ optional attachment swap).
  async updateComment(
    commentId: string,
    content: string,
    attachmentIds?: string[],
    contentBase?: string,
  ): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}`,
      CommentSchema,
      EMPTY_COMMENT,
      {
        method: "PUT",
        body: JSON.stringify(
          buildCommentUpdateBody(content, attachmentIds, contentBase),
        ),
      },
      { endpoint: "updateComment" },
    );
  }

  // DELETE /api/comments/:id — 204 No Content on success; this.fetch
  // already short-circuits 204 → undefined.
  async deleteComment(commentId: string): Promise<void> {
    await this.fetch<void>(`/api/comments/${commentId}`, { method: "DELETE" });
  }

  // POST /api/comments/:id/resolve — marks the thread root resolved; only
  // meaningful for root comments. Backend mirrors web semantics.
  async resolveComment(commentId: string): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}/resolve`,
      CommentSchema,
      EMPTY_COMMENT,
      { method: "POST" },
      { endpoint: "resolveComment" },
    );
  }

  // DELETE /api/comments/:id/resolve — un-resolves the thread.
  async unresolveComment(commentId: string): Promise<Comment> {
    return this.fetchValidatedWith(
      `/api/comments/${commentId}/resolve`,
      CommentSchema,
      EMPTY_COMMENT,
      { method: "DELETE" },
      { endpoint: "unresolveComment" },
    );
  }

  // GET /api/comments/:id/sub-issue-preview — captures the source issue +
  // comment thread as of now and returns a short-lived `capture_token` the
  // create call below must echo back. Mirrors
  // packages/core/api/client.ts:1544 getCommentSubIssuePreview. Mobile does
  // not render the snapshot (no source-context comparison UI, see
  // comment-context-menu.tsx) — only `capture_token` is read — but the full
  // response still goes through the shared schema so a drifted response
  // shape degrades to the sentinel below instead of an `as` cast.
  async getCommentSubIssuePreview(
    anchorCommentId: string,
  ): Promise<SourceContextPreview> {
    const preview = await this.fetchValidated(
      `/api/comments/${anchorCommentId}/sub-issue-preview`,
      SourceContextPreviewSchema,
      EMPTY_SOURCE_CONTEXT_PREVIEW,
      { endpoint: "GET /api/comments/:id/sub-issue-preview" },
    );
    if (!preview.capture_token) {
      throw new Error("Invalid source context preview response");
    }
    return preview;
  }

  // POST /api/comments/:id/sub-issues — creates a sub-issue anchored on
  // this comment, with the captured thread attached as source context.
  // Manual mode only: mobile's create-issue form (new-issue.tsx) has no
  // agent-quick-create panel, so it doesn't gain one here either — same
  // divergence, same reason (see apps/mobile/CLAUDE.md UI waterfall: no
  // new surface without an existing pattern to extend). Mirrors
  // packages/core/api/client.ts:1556 createCommentSubIssue (manual overload
  // only).
  async createCommentSubIssue(
    anchorCommentId: string,
    data: CreateCommentSubIssueManualRequest,
  ): Promise<Issue> {
    const issue = await this.fetchValidatedWith(
      `/api/comments/${anchorCommentId}/sub-issues`,
      IssueSchema,
      EMPTY_ISSUE_FALLBACK,
      { method: "POST", body: JSON.stringify(data) },
      { endpoint: "POST /api/comments/:id/sub-issues" },
    );
    if (!issue.id) throw new Error("Invalid sub-issue response");
    return issue;
  }

  // --- Reactions ---
  // Comment reactions: POST/DELETE /api/comments/{id}/reactions
  // Issue reactions:   POST/DELETE /api/issues/{id}/reactions
  // Mirror surface from packages/core/api/client.ts:541-573.
  async addReaction(commentId: string, emoji: string): Promise<Reaction> {
    return this.fetch<Reaction>(`/api/comments/${commentId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeReaction(commentId: string, emoji: string): Promise<void> {
    await this.fetch<void>(`/api/comments/${commentId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  async addIssueReaction(
    issueId: string,
    emoji: string,
  ): Promise<IssueReaction> {
    return this.fetch<IssueReaction>(`/api/issues/${issueId}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    });
  }

  async removeIssueReaction(issueId: string, emoji: string): Promise<void> {
    await this.fetch<void>(`/api/issues/${issueId}/reactions`, {
      method: "DELETE",
      body: JSON.stringify({ emoji }),
    });
  }

  // --- Issue update ---
  // Write endpoint — the mutation surface handles errors via rollback, so
  // we let bad responses surface naturally (no parseWithFallback).
  // Method is PUT to match backend router (server/cmd/server/router.go:327)
  // and web client (packages/core/api/client.ts:465).
  async updateIssue(id: string, body: UpdateIssueRequest): Promise<Issue> {
    return this.fetch<Issue>(`/api/issues/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  // Backend returns 204 No Content on success
  // (server/internal/handler/issue.go DeleteIssue). this.fetch already
  // short-circuits 204 → undefined (api.ts:270), so no body parsing needed.
  async deleteIssue(id: string): Promise<void> {
    await this.fetch<void>(`/api/issues/${id}`, { method: "DELETE" });
  }

  // --- Labels ---
  async listLabels(opts?: {
    signal?: AbortSignal;
  }): Promise<ListLabelsResponse> {
    const raw = await this.fetch<unknown>("/api/labels", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ListLabelsResponseSchema,
      EMPTY_LIST_LABELS_RESPONSE,
      { endpoint: "GET /api/labels" },
    );
  }

  // Create a new label and return it. Response is consumed by the
  // create-and-attach flow in label picker, so raw `this.fetch<Label>` is
  // used — same convention as createProject (cache rollback on failure is
  // preferable to a parseWithFallback fallback that would mask server errors).
  async createLabel(body: CreateLabelRequest): Promise<Label> {
    return this.fetch<Label>("/api/labels", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async attachLabel(
    issueId: string,
    labelId: string,
  ): Promise<IssueLabelsResponse> {
    return this.fetch<IssueLabelsResponse>(
      `/api/issues/${issueId}/labels`,
      {
        method: "POST",
        body: JSON.stringify({ label_id: labelId }),
      },
    );
  }

  async detachLabel(
    issueId: string,
    labelId: string,
  ): Promise<IssueLabelsResponse> {
    return this.fetch<IssueLabelsResponse>(
      `/api/issues/${issueId}/labels/${labelId}`,
      { method: "DELETE" },
    );
  }

  // --- Issue status catalog (MUL-6243) ---
  /**
   * The workspace's issue statuses — the 7 built-ins plus any custom ones an
   * admin defined. Reads are open to every workspace member; the catalog
   * mutations are owner/admin only and live on web's settings screen, which is
   * why mobile ships the read alone.
   *
   * `include_archived` is on by design. Archiving retires a status from FUTURE
   * assignment but leaves the issues already on it, and those issues must keep
   * their real name, colour and category — dropping archived rows here would
   * degrade them to a raw key with a guessed category. Pickers filter them out
   * via `IssueStatusCatalog.activeStatuses` instead.
   */
  async listIssueStatuses(
    includeArchived = false,
    opts?: { signal?: AbortSignal },
  ): Promise<ListIssueStatusesResponse> {
    const query = includeArchived ? "?include_archived=true" : "";
    return this.fetchValidated(
      `/api/issue-statuses${query}`,
      ListIssueStatusesResponseSchema,
      EMPTY_LIST_ISSUE_STATUSES_RESPONSE,
      { ...opts, endpoint: "GET /api/issue-statuses" },
    );
  }

  // --- Projects ---
  async listProjects(opts?: {
    signal?: AbortSignal;
  }): Promise<ListProjectsResponse> {
    const raw = await this.fetch<unknown>("/api/projects", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ListProjectsResponseSchema,
      EMPTY_LIST_PROJECTS_RESPONSE,
      { endpoint: "GET /api/projects" },
    );
  }

  // Executable org chart (K75). Read-only on mobile; mirrors core `listOrgStructures`.
  async listOrgStructures(opts?: { signal?: AbortSignal }): Promise<OrgStructureList> {
    return this.fetchValidated(
      "/api/org",
      OrgStructureListSchema,
      EMPTY_ORG_STRUCTURE_LIST,
      opts,
    );
  }

  // Goals with ancestry (K74). Read-only on mobile; mirrors core `listGoals`.
  async listGoals(opts?: { signal?: AbortSignal }): Promise<ListGoalsResponse> {
    return this.fetchValidated(
      "/api/goals",
      ListGoalsResponseSchema,
      EMPTY_LIST_GOALS_RESPONSE,
      opts,
    );
  }

  /** Workspace-wide project search. See `searchIssues` for the signal
   *  contract. */
  async searchProjects(
    params: { q: string; limit?: number; include_closed?: boolean; offset?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<SearchProjectsResponse> {
    const search = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null) continue;
      search.set(k, String(v));
    }
    const raw = await this.fetch<unknown>(
      `/api/projects/search?${search.toString()}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      SearchProjectsResponseSchema,
      EMPTY_SEARCH_PROJECTS_RESPONSE,
      { endpoint: "GET /api/projects/search" },
    );
  }

  async getProject(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Project> {
    const raw = await this.fetch<unknown>(`/api/projects/${id}`, {
      signal: opts?.signal,
    });
    // Drift-safe parse — UI checks `data.id === ""` to render the
    // "project not found / shape drifted" error state instead of a
    // half-populated detail page.
    return parseWithFallback(raw, ProjectSchema, EMPTY_PROJECT, {
      endpoint: "GET /api/projects/:id",
    });
  }

  // Write endpoints — no parseWithFallback (mirrors updateIssue:430). A
  // malformed write response surfaces as an error so the optimistic
  // patch rolls back; pretending the write succeeded with empty data
  // would silently desync caches.
  async createProject(body: CreateProjectRequest): Promise<Project> {
    return this.fetch<Project>("/api/projects", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async updateProject(
    id: string,
    body: UpdateProjectRequest,
  ): Promise<Project> {
    return this.fetch<Project>(`/api/projects/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  async deleteProject(id: string): Promise<void> {
    await this.fetch<void>(`/api/projects/${id}`, { method: "DELETE" });
  }

  // --- Project resources ---
  async listProjectResources(
    projectId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ListProjectResourcesResponse> {
    const raw = await this.fetch<unknown>(
      `/api/projects/${projectId}/resources`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ListProjectResourcesResponseSchema,
      EMPTY_LIST_PROJECT_RESOURCES_RESPONSE,
      { endpoint: "GET /api/projects/:id/resources" },
    );
  }

  async createProjectResource(
    projectId: string,
    body: CreateProjectResourceRequest,
  ): Promise<ProjectResource> {
    return this.fetch<ProjectResource>(
      `/api/projects/${projectId}/resources`,
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    );
  }

  async deleteProjectResource(
    projectId: string,
    resourceId: string,
  ): Promise<void> {
    await this.fetch<void>(
      `/api/projects/${projectId}/resources/${resourceId}`,
      { method: "DELETE" },
    );
  }

  // --- Chat ---
  // Mirrors the surface area of packages/core/api/client.ts chat methods.
  // v1 omits getChatSession + updateChatSession (rename) — see the v1 cut
  // list in /Users/qingnaiyuan/.claude/plans/plan-velvety-puddle.md.

  async listChatSessions(
    opts?: { signal?: AbortSignal },
  ): Promise<ChatSession[]> {
    const raw = await this.fetch<unknown>("/api/chat/sessions", {
      signal: opts?.signal,
    });
    return parseWithFallback(
      raw,
      ChatSessionListSchema,
      EMPTY_CHAT_SESSION_LIST,
      { endpoint: "GET /api/chat/sessions" },
    );
  }

  async createChatSession(
    data: { agent_id: string; title?: string },
  ): Promise<ChatSession> {
    // Strict parse — a malformed create response derails the optimistic
    // burst (we need the new session id to seed caches). Fallback would
    // be worse than the throw.
    const raw = await this.fetch<unknown>("/api/chat/sessions", {
      method: "POST",
      body: JSON.stringify(data),
    });
    const parsed = ChatSessionSchema.safeParse(raw);
    if (!parsed.success) {
      console.error("[api] ← shape mismatch POST /api/chat/sessions", {
        issues: parsed.error.issues,
      });
      throw new ApiError("Create chat session response invalid", 0, raw);
    }
    return parsed.data;
  }

  async deleteChatSession(id: string): Promise<void> {
    await this.fetch<void>(`/api/chat/sessions/${id}`, { method: "DELETE" });
  }

  async listChatMessages(
    sessionId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ChatMessage[]> {
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/messages`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ChatMessageListSchema,
      EMPTY_CHAT_MESSAGE_LIST,
      { endpoint: "GET /api/chat/sessions/:id/messages" },
    );
  }

  async sendChatMessage(
    sessionId: string,
    content: string,
    opts?: { attachmentIds?: string[] },
  ): Promise<SendChatMessageResponse> {
    // Strict parse — we need task_id + created_at to anchor the optimistic
    // StatusPill. Fallback would silently break the elapsed-time timer.
    //
    // `attachment_ids` mirrors the comment / issue create payloads —
    // server-side `chat.go` back-fills `chat_message_id` on the listed
    // attachments after the message row is inserted (see
    // server/internal/handler/chat.go:410-456).
    const body: { content: string; attachment_ids?: string[] } = { content };
    if (opts?.attachmentIds && opts.attachmentIds.length > 0) {
      body.attachment_ids = opts.attachmentIds;
    }
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/messages`,
      {
        method: "POST",
        body: JSON.stringify(body),
      },
    );
    const parsed = SendChatMessageResponseSchema.safeParse(raw);
    if (!parsed.success) {
      console.error("[api] ← shape mismatch POST /api/chat/sessions/:id/messages", {
        issues: parsed.error.issues,
      });
      throw new ApiError("Send message response invalid", 0, raw);
    }
    return parsed.data;
  }

  async getPendingChatTask(
    sessionId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ChatPendingTask> {
    const raw = await this.fetch<unknown>(
      `/api/chat/sessions/${sessionId}/pending-task`,
      { signal: opts?.signal },
    );
    return parseWithFallback(
      raw,
      ChatPendingTaskSchema,
      EMPTY_CHAT_PENDING_TASK,
      { endpoint: "GET /api/chat/sessions/:id/pending-task" },
    );
  }

  async markChatSessionRead(sessionId: string): Promise<void> {
    await this.fetch<void>(
      `/api/chat/sessions/${sessionId}/read`,
      { method: "POST" },
    );
  }

  async cancelTaskById(taskId: string): Promise<void> {
    await this.fetch<void>(`/api/tasks/${taskId}/cancel`, { method: "POST" });
  }

  // POST /api/issues/:id/rerun — re-runs the named failed task. The
  // response (AgentTask) isn't rendered anywhere on mobile (same as web's
  // TaskCommentRetryButton, which only reacts to success/failure) so this
  // stays an unconsumed write per the ApiClient helper rules. Mirrors
  // packages/core/api/client.ts:5122 rerunIssue.
  async rerunIssue(issueId: string, taskId: string): Promise<void> {
    await this.fetch<void>(`/api/issues/${issueId}/rerun`, {
      method: "POST",
      body: JSON.stringify({ task_id: taskId }),
    });
  }

  /** Live execution timeline for a task — used by the chat screen to
   *  render the "thinking → tool_use → tool_result → final text" trace
   *  beneath an in-flight assistant bubble. `task:message` WS events
   *  append to the same cache key in real time (see
   *  use-chat-session-realtime.ts). */
  async listTaskMessages(
    taskId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TaskMessagePayload[]> {
    // The endpoint now answers `{ messages, actions }`; a server that predates
    // that still answers the bare array. The shared schema accepts both, so an
    // installed app renders its trace against either — parsing only the old
    // shape would blank the timeline the moment the backend upgraded. Mobile
    // has no run-action lane yet, so the actions half is read and dropped;
    // taking it is a UI decision, not a data one.
    const activity = await this.fetchValidated(
      `/api/tasks/${taskId}/messages`,
      TaskActivityResponseSchema,
      EMPTY_TASK_ACTIVITY,
      { ...opts, endpoint: "GET /api/tasks/:id/messages" },
    );
    return activity.messages;
  }

  // --- Pins ---
  //
  // Pin metadata only — title / status / icon for each row come from
  // `issueDetailOptions` / `projectDetailOptions` on the consumer side.
  // Endpoints mirror packages/core/api/client.ts:1551-1572.

  async listPins(opts?: { signal?: AbortSignal }): Promise<PinnedItem[]> {
    return this.fetchValidated(
      "/api/pins",
      PinListSchema,
      EMPTY_PIN_LIST,
      { ...opts, endpoint: "listPins" },
    );
  }

  async createPin(data: {
    item_type: PinnedItemType;
    item_id: string;
  }): Promise<PinnedItem> {
    return this.fetchValidatedWith(
      "/api/pins",
      PinnedItemSchema,
      // Mirror EMPTY_PIN_LIST element shape — onSuccess uses the returned
      // pin's id/position so a stub with empty id is detectable downstream.
      {
        id: "",
        workspace_id: "",
        user_id: "",
        item_type: data.item_type,
        item_id: data.item_id,
        position: 0,
        created_at: "",
      },
      { method: "POST", body: JSON.stringify(data) },
      { endpoint: "createPin" },
    );
  }

  async deletePin(itemType: PinnedItemType, itemId: string): Promise<void> {
    await this.fetch<void>(`/api/pins/${itemType}/${itemId}`, {
      method: "DELETE",
    });
  }

  async reorderPins(data: ReorderPinsRequest): Promise<void> {
    await this.fetch<void>("/api/pins/reorder", {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  // --- Workspace Brain: notes ---

  /**
   * The Brain listing. `search` and `tag` are server filters (full-text runs
   * on the GIN index), and `tags` ships alongside the items because the chips
   * need every live tag, not just the tags of the filtered page — mirrors
   * `packages/core/api/client.ts:listWorkspaceNotes` parameter for parameter.
   */
  async listWorkspaceNotes(
    params?: { search?: string; tag?: string; archived?: boolean; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<WorkspaceNotesResponse> {
    const qs = new URLSearchParams();
    if (params?.search) qs.set("search", params.search);
    if (params?.tag) qs.set("tag", params.tag);
    if (params?.archived === true) qs.set("archived", "true");
    if (params?.limit !== undefined) qs.set("limit", String(params.limit));
    const query = qs.toString();
    return this.fetchValidated(
      `/api/workspace/notes${query ? `?${query}` : ""}`,
      WorkspaceNotesResponseSchema,
      EMPTY_WORKSPACE_NOTES_RESPONSE,
      { signal: opts?.signal, endpoint: "GET /api/workspace/notes" },
    );
  }

  async getWorkspaceNote(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<WorkspaceNote> {
    return this.fetchValidated(
      `/api/workspace/notes/${encodeURIComponent(id)}`,
      WorkspaceNoteSchema,
      EMPTY_WORKSPACE_NOTE,
      { signal: opts?.signal, endpoint: "GET /api/workspace/notes/:id" },
    );
  }

  /**
   * Ranked search: lexical rank fused with a pgvector rank by RRF when an
   * embeddings model is configured (`vector` says which). Separate endpoint
   * from the listing because a hit carries a score and a `<mark>`-annotated
   * snippet instead of the tag facets.
   */
  async searchWorkspaceNotes(
    params: { q: string; tag?: string; archived?: boolean; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<WorkspaceNoteSearchResponse> {
    const qs = new URLSearchParams({ q: params.q });
    if (params.tag) qs.set("tag", params.tag);
    if (params.archived === true) qs.set("archived", "true");
    if (params.limit !== undefined) qs.set("limit", String(params.limit));
    return this.fetchValidated(
      `/api/workspace/notes/search?${qs.toString()}`,
      WorkspaceNoteSearchResponseSchema,
      EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE,
      { signal: opts?.signal, endpoint: "GET /api/workspace/notes/search" },
    );
  }

  async createWorkspaceNote(
    input: CreateWorkspaceNoteInput,
  ): Promise<WorkspaceNote> {
    return this.fetchValidatedWith(
      "/api/workspace/notes",
      WorkspaceNoteSchema,
      EMPTY_WORKSPACE_NOTE,
      { method: "POST", body: JSON.stringify(input) },
      { endpoint: "POST /api/workspace/notes" },
    );
  }

  /** `input.revision` is the value the client read; a 409 means someone (or
   *  the curation pass) wrote first. */
  async updateWorkspaceNote(
    id: string,
    input: UpdateWorkspaceNoteInput,
  ): Promise<WorkspaceNote> {
    return this.fetchValidatedWith(
      `/api/workspace/notes/${encodeURIComponent(id)}`,
      WorkspaceNoteSchema,
      EMPTY_WORKSPACE_NOTE,
      { method: "PATCH", body: JSON.stringify(input) },
      { endpoint: "PATCH /api/workspace/notes/:id" },
    );
  }

  async setWorkspaceNoteArchived(
    id: string,
    archived: boolean,
  ): Promise<WorkspaceNote> {
    return this.fetchValidatedWith(
      `/api/workspace/notes/${encodeURIComponent(id)}/${archived ? "archive" : "unarchive"}`,
      WorkspaceNoteSchema,
      EMPTY_WORKSPACE_NOTE,
      { method: "POST" },
      { endpoint: "POST /api/workspace/notes/:id/archive" },
    );
  }

  /** 403 unless the caller is a workspace owner/admin or the note's author
   *  (server/internal/handler/workspace_note.go canDeleteWorkspaceNote). */
  async deleteWorkspaceNote(id: string): Promise<void> {
    await this.fetch<void>(`/api/workspace/notes/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  // --- Workspace Brain: capture inbox ---

  async listBrainCaptures(
    params?: { status?: BrainCaptureStatus | "all"; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<BrainCapturesResponse> {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.limit !== undefined) qs.set("limit", String(params.limit));
    const query = qs.toString();
    return this.fetchValidated(
      `/api/brain/captures${query ? `?${query}` : ""}`,
      BrainCapturesResponseSchema,
      EMPTY_BRAIN_CAPTURES_RESPONSE,
      { signal: opts?.signal, endpoint: "GET /api/brain/captures" },
    );
  }

  async getBrainCapture(
    id: string,
    opts?: { signal?: AbortSignal },
  ): Promise<BrainCapture> {
    return this.captureFrom(
      this.fetchValidated(
        `/api/brain/captures/${encodeURIComponent(id)}`,
        BrainCaptureResponseSchema,
        { capture: EMPTY_BRAIN_CAPTURE },
        { signal: opts?.signal, endpoint: "GET /api/brain/captures/:id" },
      ),
    );
  }

  /** Text / link / todo. `origin` is "mobile" so the inbox can say where a
   *  capture came in from; image, audio and file go through the upload route
   *  (the server rejects those kinds here with a 400). */
  async createBrainCapture(
    input: CreateBrainCaptureInput,
  ): Promise<BrainCapture> {
    return this.captureFrom(
      this.fetchValidatedWith(
        "/api/brain/captures",
        BrainCaptureResponseSchema,
        { capture: EMPTY_BRAIN_CAPTURE },
        { method: "POST", body: JSON.stringify({ origin: "mobile", ...input }) },
        { endpoint: "POST /api/brain/captures" },
      ),
    );
  }

  /**
   * A photo, a voice memo or any file, multipart. The server derives the kind
   * from the content type (image/* → image, audio/* → audio, else file) and
   * queues transcription for audio. 503 when no storage is configured.
   */
  async uploadBrainCapture(
    asset: FileAsset,
    fields?: { content?: string; title_hint?: string },
  ): Promise<BrainCapture> {
    const raw = await this.postMultipart("/api/brain/captures/upload", asset, {
      origin: "mobile",
      content: fields?.content ?? "",
      title_hint: fields?.title_hint ?? "",
    });
    return parseWithFallback(
      raw,
      BrainCaptureResponseSchema,
      { capture: EMPTY_BRAIN_CAPTURE },
      { endpoint: "POST /api/brain/captures/upload" },
    ).capture as BrainCapture;
  }

  /** Re-ask the model. 503 when none is configured, 502 when the call failed —
   *  neither is an error the user caused, so callers say so rather than toast. */
  async suggestBrainCapture(id: string): Promise<BrainCapture> {
    return this.captureFrom(
      this.fetchValidatedWith(
        `/api/brain/captures/${encodeURIComponent(id)}/suggest`,
        BrainCaptureResponseSchema,
        { capture: EMPTY_BRAIN_CAPTURE },
        { method: "POST" },
        { endpoint: "POST /api/brain/captures/:id/suggest" },
      ),
    );
  }

  /** note | merge | discard. Only from status "raw" (409 otherwise). */
  async organizeBrainCapture(
    id: string,
    input: OrganizeBrainCaptureInput,
  ): Promise<OrganizeBrainCaptureResponse> {
    return this.fetchValidatedWith(
      `/api/brain/captures/${encodeURIComponent(id)}/organize`,
      OrganizeBrainCaptureResponseSchema,
      EMPTY_ORGANIZE_BRAIN_CAPTURE_RESPONSE,
      { method: "POST", body: JSON.stringify(input) },
      { endpoint: "POST /api/brain/captures/:id/organize" },
    );
  }

  /** A discarded capture back to raw (409 from any other status). */
  async reopenBrainCapture(id: string): Promise<BrainCapture> {
    return this.captureFrom(
      this.fetchValidatedWith(
        `/api/brain/captures/${encodeURIComponent(id)}/reopen`,
        BrainCaptureResponseSchema,
        { capture: EMPTY_BRAIN_CAPTURE },
        { method: "POST" },
        { endpoint: "POST /api/brain/captures/:id/reopen" },
      ),
    );
  }

  /** Gone for good, with its file unless a note already holds it. */
  async deleteBrainCapture(id: string): Promise<void> {
    await this.fetch<void>(`/api/brain/captures/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  /** Every capture read/write answers `{capture}`; unwrap it once. */
  private async captureFrom(
    envelope: Promise<{ capture: BrainCapture }>,
  ): Promise<BrainCapture> {
    return (await envelope).capture;
  }

  /**
   * Voice memo / conversation turn: one audio file in, its text out
   * (POST /api/voice/transcribe). Mirrors `packages/core/api/client.ts:transcribeVoice`
   * with the RN-shaped `FileAsset` instead of a browser `Blob`.
   *
   * `language` is an ISO-639-1 code; "" leaves the server on MULTICA_STT_LANGUAGE.
   * 409 `stt_not_configured` surfaces as ApiError with that `code` on the body.
   */
  async transcribeVoice(
    asset: FileAsset,
    language = "",
  ): Promise<VoiceTranscription> {
    const rid = createRequestId();
    const start = Date.now();
    const path = "/api/voice/transcribe";

    const headers: Record<string, string> = {
      "X-Client-Platform": "mobile",
      "X-Client-OS": "ios",
      "X-Client-Version": "0.1.0",
      "X-Request-ID": rid,
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers["X-Workspace-Slug"] = slug;

    const formData = new FormData();
    formData.append(
      "file",
      { uri: asset.uri, name: asset.name, type: asset.type } as never,
    );
    if (language) formData.append("language", language);

    console.log(`[api] → POST ${path}`, { rid, filename: asset.name });

    const res = await fetch(`${API_URL}${path}`, {
      method: "POST",
      headers,
      body: formData,
    });
    const duration = Date.now() - start;

    if (!res.ok) {
      if (res.status === 401) this.options.onUnauthorized?.();
      let body: unknown;
      try {
        body = await res.json();
      } catch {
        body = undefined;
      }
      const message =
        (body && typeof body === "object" && "message" in body
          ? String((body as { message: unknown }).message)
          : null) ?? `Transcription failed: ${res.status}`;
      console.error(`[api] ← ${res.status} ${path}`, {
        rid,
        duration: `${duration}ms`,
        error: message,
      });
      throw new ApiError(message, res.status, body);
    }

    const raw = (await res.json()) as unknown;
    console.log(`[api] ← ${res.status} ${path}`, {
      rid,
      duration: `${duration}ms`,
    });
    return parseWithFallback(
      raw,
      VoiceTranscriptionSchema,
      EMPTY_VOICE_TRANSCRIPTION,
      { endpoint: "POST /api/voice/transcribe" },
    );
  }

  // --- File Upload ---

  /**
   * The multipart shell every file-upload endpoint shares: auth + slug
   * headers, request id, structured logging, 401 hook, ApiError on non-2xx,
   * parsed JSON body back.
   *
   * Does NOT go through `this.fetch` because:
   *   - FormData must not have a `Content-Type` header preset (the RN fetch
   *     polyfill needs to set the multipart boundary itself).
   *   - `this.fetch` hard-codes `application/json`.
   *
   * No timeout / signal plumbing, unlike `this.fetch`: uploads are mutations
   * (TanStack Query hands `mutationFn` no signal) and a 30s ceiling would
   * abort a legitimate 40 MB upload on cellular. The 401 hook and the
   * ApiError contract are the parts callers depend on, so they stay.
   */
  private async postMultipart(
    path: string,
    asset: FileAsset,
    fields: Record<string, string> = {},
  ): Promise<unknown> {
    const rid = createRequestId();
    const start = Date.now();

    const headers: Record<string, string> = {
      // No Content-Type — let fetch set the multipart boundary.
      "X-Client-Platform": "mobile",
      "X-Client-OS": "ios",
      "X-Client-Version": "0.1.0",
      "X-Request-ID": rid,
    };
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;
    const slug = getCurrentSlug();
    if (slug) headers["X-Workspace-Slug"] = slug;

    const formData = new FormData();
    // RN's FormData accepts `{ uri, name, type }` as the file value.
    // `as never` quiets TS (the global FormData type expects `Blob | string`).
    formData.append(
      "file",
      { uri: asset.uri, name: asset.name, type: asset.type } as never,
    );
    for (const [key, value] of Object.entries(fields)) {
      if (value !== "") formData.append(key, value);
    }

    console.log(`[api] → POST ${path}`, { rid, filename: asset.name });

    const res = await fetch(`${API_URL}${path}`, {
      method: "POST",
      headers,
      body: formData,
    });
    const duration = Date.now() - start;

    if (!res.ok) {
      if (res.status === 401) this.options.onUnauthorized?.();
      let body: unknown;
      try {
        body = await res.json();
      } catch {
        body = undefined;
      }
      // The Go handlers answer `{"error": "..."}` (handler.go writeError);
      // `message` is checked first only because a few endpoints predate it.
      const message =
        (body && typeof body === "object" && "message" in body
          ? String((body as { message: unknown }).message)
          : body && typeof body === "object" && "error" in body
            ? String((body as { error: unknown }).error)
            : null) ?? `Upload failed: ${res.status}`;
      console.error(`[api] ← ${res.status} ${path}`, {
        rid,
        duration: `${duration}ms`,
        error: message,
      });
      throw new ApiError(message, res.status, body);
    }

    console.log(`[api] ← ${res.status} ${path}`, {
      rid,
      duration: `${duration}ms`,
    });
    return (await res.json()) as unknown;
  }

  /**
   * Multipart-stream a file to `/api/upload-file`. Mirrors the web
   * implementation in `packages/core/api/client.ts:uploadFile` but with the
   * RN-shaped `FileAsset` instead of a browser `File`. The fetch FormData
   * polyfill recognises `{ uri, name, type }` and reads the file off disk.
   *
   * `opts.issueId` / `opts.commentId` link the attachment record. Pass
   * `issueId` when uploading from a comment composer / reply input; leave
   * both empty when uploading from a not-yet-created issue (the attachment
   * is hooked to the issue once it's created — same flow as web).
   */
  async uploadFile(
    asset: FileAsset,
    opts?: { issueId?: string; commentId?: string },
  ): Promise<Attachment> {
    const path = "/api/upload-file";
    const fields: Record<string, string> = {};
    if (opts?.issueId) fields["issue_id"] = opts.issueId;
    if (opts?.commentId) fields["comment_id"] = opts.commentId;

    // Strict validation: parseWithFallback's silent-fallback pattern doesn't
    // fit here — an attachment without a `url` would be inserted into the
    // user's text as `![](undefined)`. Throw on shape mismatch so the
    // caller's Alert path fires instead of letting a broken link land in
    // the editor.
    const json = await this.postMultipart(path, asset, fields);
    const parsed = AttachmentSchema.safeParse(json);
    if (!parsed.success) {
      console.error(`[api] ← shape mismatch ${path}`, {
        error: parsed.error.message,
      });
      // 200 with a body we cannot use: the upload did happen, so the status
      // is honest, but the caller must treat it as a failure.
      throw new ApiError("Upload response invalid", 200, json);
    }
    return parsed.data;
  }
}

export { MAX_FILE_SIZE };

export const api = new ApiClient();
