import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { AgentRuntime } from "../types";

/**
 * `metadata.skipped_agents` maps a provider the daemon DID find on the machine
 * to the reason its last probe round refused to register it: version below the
 * documented minimum, version undetectable, binary not executable.
 *
 * Without it, "CLI not installed" and "CLI installed but rejected" both render
 * as an absent runtime, which leaves the user with nothing to act on.
 *
 * The map is machine-level, written onto every runtime row of the daemon at
 * registration, so any row of the machine carries the same snapshot.
 */
export const RuntimeSkippedAgentsSchema = z.record(z.string(), z.string());

export interface RuntimeSkippedAgent {
  provider: string;
  reason: string;
}

const EMPTY_SKIPPED_AGENTS: Record<string, string> = {};

/**
 * Reads the skip map off one runtime's metadata. Returns `null` when the
 * runtime predates the field (so callers can tell "nothing skipped" apart from
 * "this row never reported"), and `[]` when the daemon reported an empty map.
 */
export function readRuntimeSkippedAgents(
  metadata: Record<string, unknown> | undefined,
): RuntimeSkippedAgent[] | null {
  const raw = metadata?.skipped_agents;
  if (raw === undefined || raw === null) return null;
  if (typeof raw !== "object" || Array.isArray(raw)) return null;
  const parsed = parseWithFallback(raw, RuntimeSkippedAgentsSchema, EMPTY_SKIPPED_AGENTS, {
    endpoint: "runtime.metadata.skipped_agents",
  });
  return Object.entries(parsed)
    .map(([provider, reason]) => ({
      provider: provider.trim(),
      reason: reason.trim(),
    }))
    .filter(({ provider, reason }) => provider !== "" && reason !== "")
    .sort((a, b) => a.provider.localeCompare(b.provider));
}

/**
 * The machine's current skip set: the snapshot from the runtime row that
 * reported most recently. An older row's snapshot must not win, otherwise a
 * repaired CLI would keep being reported by whichever runtime went offline
 * before the fix.
 */
export function machineSkippedAgents(
  runtimes: AgentRuntime[],
): RuntimeSkippedAgent[] {
  let freshest: { at: number; skipped: RuntimeSkippedAgent[] } | null = null;
  for (const runtime of runtimes) {
    const skipped = readRuntimeSkippedAgents(runtime.metadata);
    if (skipped === null) continue;
    const at = runtime.last_seen_at
      ? new Date(runtime.last_seen_at).getTime()
      : 0;
    const seenAt = Number.isFinite(at) ? at : 0;
    if (!freshest || seenAt > freshest.at) freshest = { at: seenAt, skipped };
  }
  return freshest?.skipped ?? [];
}
