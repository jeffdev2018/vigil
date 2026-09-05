import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Run replay (K70): the run as one ordered, hash-chained event stream, and
// a resume point that starts a new run with a new instruction.

export interface ReplayActor {
  type: string;
  id: string;
  name: string;
}

export interface ReplayEvent {
  seq: number;
  at: string;
  kind: string;
  actor: ReplayActor;
  title: string;
  text: string;
  data: Record<string, unknown>;
  /** internal, or confidential when the server redacted a secret out of it */
  data_class: string;
  /** on tool calls when the run had a plan: false = the call is not in the plan (drift) */
  in_plan: boolean | null;
  source: string;
  source_id: string;
  prev_hash: string;
  hash: string;
}

export interface ReplayLink {
  relation: string;
  task_id: string;
  agent_id: string;
  agent_name: string;
}

export interface ReplaySnapshot {
  trust_mode: string;
  effect_mode: string;
  model: string;
  thinking_level: string;
  permission_profile_id: string;
  runtime_id: string;
  safe_mode: boolean;
  plan_version: number;
  recorded_at: string;
}

export interface RunReplay {
  run: {
    id: string;
    safe_mode: boolean;
    snapshot: ReplaySnapshot | null;
    plan: { version: number; steps: number } | null;
    drift: number;
    issue_id: string;
    agent_id: string;
    agent_name: string;
    status: string;
    trust_mode: string;
    effect_mode: string;
    model: string;
    created_at: string | null;
    started_at: string | null;
    completed_at: string | null;
    links: ReplayLink[];
  };
  events: ReplayEvent[];
  total: number;
  next_cursor: number | null;
  head_hash: string;
  cost: { input_tokens: number; output_tokens: number; cost_usd_ticks: number | null };
  sealed: { events: number; head_hash: string; sealed_at: string; verified: boolean } | null;
}

export interface ReplayResumeResult {
  task_id: string;
  from_seq: number;
}

export interface ReplaySimulateResult {
  task_id: string;
  safe_mode: boolean;
}

export const replayKeys = {
  task: (wsId: string, taskId: string) => ["run-replay", wsId, taskId] as const,
};

const REPLAY_PAGE = 200;
const REPLAY_MAX_PAGES = 25;

/** Follows the cursor so the scrubber always has the whole run. */
export async function fetchWholeReplay(taskId: string): Promise<RunReplay> {
  const first = await api.getTaskReplay(taskId, 0, REPLAY_PAGE);
  let events = first.events;
  let cursor = first.next_cursor;
  let pages = 1;
  while (cursor !== null && pages < REPLAY_MAX_PAGES) {
    const page = await api.getTaskReplay(taskId, cursor, REPLAY_PAGE);
    events = events.concat(page.events);
    cursor = page.next_cursor;
    pages += 1;
  }
  return { ...first, events, next_cursor: cursor };
}

export function taskReplayOptions(wsId: string, taskId: string) {
  return queryOptions({ queryKey: replayKeys.task(wsId, taskId), queryFn: () => fetchWholeReplay(taskId), staleTime: 30_000 });
}

export function useResumeTaskReplay(wsId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { seq: number; instruction: string }) => api.resumeTaskReplay(taskId, v.seq, v.instruction),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: replayKeys.task(wsId, taskId) });
    },
  });
}

export function useSimulateTaskReplay(wsId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.simulateTaskReplay(taskId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export interface ReplayCounts {
  tool_calls: number;
  effects: number;
  decisions: number;
  steers: number;
  errors: number;
  handoffs: number;
  drift: number;
  redacted: number;
}

/** What happened up to and including position `seq` (inclusive index into events). */
export function replayCountsUpTo(events: ReplayEvent[], seq: number): ReplayCounts {
  const c: ReplayCounts = { tool_calls: 0, effects: 0, decisions: 0, steers: 0, errors: 0, handoffs: 0, drift: 0, redacted: 0 };
  for (const e of events.slice(0, Math.max(0, seq + 1))) {
    if (e.data_class === "confidential") c.redacted += 1;
    if (e.in_plan === false) c.drift += 1;
    if (e.kind === "tool_use") c.tool_calls += 1;
    else if (e.kind === "effect") c.effects += 1;
    else if (e.kind === "decision_asked") c.decisions += 1;
    else if (e.kind === "steer") c.steers += 1;
    else if (e.kind === "error") c.errors += 1;
    else if (e.kind === "handoff") c.handoffs += 1;
  }
  return c;
}

export type SealState = "verified" | "broken" | "unsealed";

export function sealState(replay: Pick<RunReplay, "sealed">): SealState {
  if (!replay.sealed) return "unsealed";
  return replay.sealed.verified === true ? "verified" : "broken";
}

/** True once the run can be resumed from a point (it is no longer live). */
export function replayResumable(status: string): boolean {
  return !["queued", "dispatched", "running", "parked"].includes(status);
}
