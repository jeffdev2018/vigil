import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "../api";
import { issueKeys } from "../issues/queries";

// Epic Mode (F18 / JEF-30): a project's PRD -> tech plan -> wireframe ->
// tickets pipeline, with a human gate between the steps.
//
// The gate is derived here as well as enforced on the server. That duplication
// is deliberate and bounded: the client needs it to disable a button and say
// why, the server needs it because a disabled button is not a permission. Only
// the ORDER is repeated — `nextEnabledStep` and `stepLockedBy` read the same
// approved/not-approved facts the server does, and every refusal still comes
// back as a 409 the UI surfaces.

/** Pipeline order. Step N requires step N-1 approved. */
export const EPIC_STEP_KINDS = ["prd", "tech_plan", "wireframe", "tickets"] as const;
export type EpicStepKind = (typeof EPIC_STEP_KINDS)[number];

/** Rail states. `superseded` and an unknown state both read as "not live". */
export type EpicArtifactState = "draft" | "approved" | "superseded";

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; the UI switches carry a default branch.
export const EpicArtifactSchema = z.object({
  id: z.string().catch(""),
  project_id: z.string().catch(""),
  kind: z.string().catch(""),
  version: z.number().catch(0).default(0),
  content: z.string().catch(""),
  payload: z.unknown().catch({}).default({}),
  state: z.string().catch("draft"),
  author_type: z.string().catch(""),
  author_id: z.string().catch(""),
  approved_by: z.string().catch(""),
  approved_at: z.string().nullable().catch(null).default(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  generated_by_task_id: z.string().catch(""),
}).loose();

export const EpicStepSchema = z.object({
  kind: z.string().catch(""),
  latest: EpicArtifactSchema.nullable().catch(null).default(null),
  approved: EpicArtifactSchema.nullable().catch(null).default(null),
  generating: z.boolean().catch(false),
}).loose();

export const EpicSchema = z.object({
  steps: z.record(z.string(), EpicStepSchema).catch({}).default({}),
  epic_issue_id: z.string().catch(""),
  next_kind: z.string().catch(""),
}).loose();

export const EpicGenerateSchema = z.object({
  task_id: z.string().catch(""),
  kind: z.string().catch(""),
}).loose();

export const EpicStepWriteSchema = z.object({
  artifact: EpicArtifactSchema,
  reopened_steps: z.array(z.string()).catch([]).default([]),
  epic_issue_id: z.string().catch(""),
  superseded_previous: z.boolean().catch(false),
}).loose();

export const EpicTicketSchema = z.object({
  external_key: z.string().catch(""),
  title: z.string().catch(""),
  description: z.string().catch(""),
  depends_on: z.array(z.string()).catch([]).default([]),
}).loose();

export const EpicApplySchema = z.object({
  created: z.array(z.unknown()).catch([]).default([]),
  existing: z.array(z.string()).catch([]).default([]),
  dependencies: z.number().catch(0).default(0),
  epic_issue_id: z.string().catch(""),
}).loose();

export interface EpicArtifact {
  id: string;
  project_id: string;
  /** Open string: a newer server may send a kind this build does not know. */
  kind: string;
  version: number;
  content: string;
  payload: unknown;
  /** Open string, for the same reason. Render an unknown state as inert. */
  state: string;
  author_type: string;
  author_id: string;
  approved_by: string;
  approved_at: string | null;
  created_at: string;
  updated_at: string;
  generated_by_task_id: string;
}

export interface EpicStep {
  kind: string;
  /** The newest finished artifact. Null while nothing has been written. */
  latest: EpicArtifact | null;
  approved: EpicArtifact | null;
  /** A run is out for this step. A real answer, not an error. */
  generating: boolean;
}

export interface Epic {
  steps: Record<string, EpicStep>;
  epic_issue_id: string;
  next_kind: string;
}

export interface EpicTicket {
  external_key: string;
  title: string;
  description: string;
  depends_on: string[];
}

export interface EpicApplyResult {
  created: unknown[];
  existing: string[];
  dependencies: number;
  epic_issue_id: string;
}

export const EMPTY_EPIC_STEP: EpicStep = {
  kind: "",
  latest: null,
  approved: null,
  generating: false,
};

/** A project with no epic yet is a real answer: four empty steps. */
export const EMPTY_EPIC: Epic = {
  steps: {},
  epic_issue_id: "",
  next_kind: "prd",
};

// ---------------------------------------------------------------------------
// Pure derivation — canonical layer for the gate the panel renders
// ---------------------------------------------------------------------------

/** One step, defaulted, whatever the server did or did not send for it. */
export function epicStep(epic: Epic | undefined, kind: string): EpicStep {
  return epic?.steps?.[kind] ?? { ...EMPTY_EPIC_STEP, kind };
}

/**
 * The step whose approval is missing before `kind` can be worked on, or null
 * when it is open. The first step is never locked.
 */
export function stepLockedBy(epic: Epic | undefined, kind: string): EpicStepKind | null {
  const index = (EPIC_STEP_KINDS as readonly string[]).indexOf(kind);
  if (index <= 0) return null;
  const previous = EPIC_STEP_KINDS[index - 1]!;
  return epicStep(epic, previous).approved ? null : previous;
}

/**
 * The step a human should work on next: the first one that is not approved and
 * is not locked. Empty when the pipeline is complete.
 *
 * The server sends `next_kind` too. This recomputes it rather than trusting it
 * so an older or newer server's answer cannot leave the rail pointing at a step
 * the client would then render as locked.
 */
export function nextEnabledStep(epic: Epic | undefined): string {
  for (const kind of EPIC_STEP_KINDS) {
    if (epicStep(epic, kind).approved) continue;
    return stepLockedBy(epic, kind) ? "" : kind;
  }
  return "";
}

/** Rail state of one step, for the single switch the panel renders. */
export type EpicRailState = "empty" | "generating" | "draft" | "approved" | "superseded" | "unknown";

export function epicRailState(step: EpicStep): EpicRailState {
  if (step.generating) return "generating";
  if (!step.latest) return "empty";
  switch (step.latest.state) {
    case "approved":
      return "approved";
    case "draft":
      return "draft";
    case "superseded":
      return "superseded";
    default:
      // A state a newer server introduced. It is not live and it is not a
      // draft; saying so beats guessing which of the two it behaves like.
      return "unknown";
  }
}

/**
 * The tickets a `tickets` artifact carries. LLM output round-tripped through
 * the server, so every field is defaulted rather than trusted.
 */
export function epicTickets(artifact: EpicArtifact | null | undefined): EpicTicket[] {
  const payload = artifact?.payload as { tickets?: unknown } | undefined;
  if (!payload || !Array.isArray(payload.tickets)) return [];
  return payload.tickets.map((raw) => {
    const parsed = EpicTicketSchema.safeParse(raw);
    return parsed.success
      ? (parsed.data as EpicTicket)
      : { external_key: "", title: "", description: "", depends_on: [] };
  });
}

/** The external keys an earlier apply already turned into issues. */
export function epicAppliedKeys(artifact: EpicArtifact | null | undefined): string[] {
  const payload = artifact?.payload as { applied?: unknown } | undefined;
  const applied = payload?.applied;
  if (!applied || typeof applied !== "object" || Array.isArray(applied)) return [];
  return Object.entries(applied as Record<string, unknown>)
    .filter(([, value]) => typeof value === "string" && value !== "")
    .map(([key]) => key);
}

/** Replaying apply only creates what is not applied yet — show only that. */
export function epicRemainingTickets(artifact: EpicArtifact | null | undefined): EpicTicket[] {
  const applied = new Set(epicAppliedKeys(artifact));
  return epicTickets(artifact).filter((ticket) => !applied.has(ticket.external_key));
}

// ---------------------------------------------------------------------------
// Query + mutations
// ---------------------------------------------------------------------------

// A step is written by an agent run, so `generating` resolves later. The
// realtime event is the fast path; polling is the floor.
const POLL_MS = 10_000;

export const epicKeys = {
  all: (wsId: string) => ["project-epic", wsId] as const,
  detail: (wsId: string, projectId: string) => ["project-epic", wsId, projectId] as const,
};

export function projectEpicOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: epicKeys.detail(wsId, projectId),
    queryFn: () => api.getProjectEpic(projectId),
    enabled: !!wsId && !!projectId,
    refetchInterval: (query) =>
      Object.values((query.state.data as Epic | undefined)?.steps ?? {}).some((s) => s?.generating)
        ? POLL_MS
        : false,
  });
}

export function useProjectEpic(wsId: string, projectId: string) {
  return projectEpicOptions(wsId, projectId);
}

export function useGenerateEpicStep(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { kind: string; agentId?: string }) =>
      api.generateProjectEpicStep(projectId, v.kind, v.agentId),
    onSettled: () => qc.invalidateQueries({ queryKey: epicKeys.detail(wsId, projectId) }),
  });
}

export function useSaveEpicStep(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { kind: string; content: string; payload?: unknown }) =>
      api.putProjectEpicStep(projectId, v.kind, v.content, v.payload),
    onSettled: () => qc.invalidateQueries({ queryKey: epicKeys.detail(wsId, projectId) }),
  });
}

export function useApproveEpicStep(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (kind: string) => api.approveProjectEpicStep(projectId, kind),
    onSettled: () => qc.invalidateQueries({ queryKey: epicKeys.detail(wsId, projectId) }),
  });
}

export function useApplyEpicTickets(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.applyProjectEpicTickets(projectId),
    onSettled: () => {
      // Apply creates issues under the epic host issue, so the issue lists and
      // the project counters are stale too, not just the epic.
      qc.invalidateQueries({ queryKey: epicKeys.detail(wsId, projectId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}
