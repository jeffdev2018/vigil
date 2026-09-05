/**
 * Runtime display helpers.
 *
 * MIRRORED from packages/core/runtimes/{display,skipped-agents,cli-auth}.ts.
 * Those modules are pure enough to read, but `packages/core/package.json`
 * exports only `./runtimes`, `./runtimes/queries`, `./runtimes/mutations`,
 * `./runtimes/cli-auth` and `./runtimes/pools` — every one of which pulls in
 * the web API client instance and TanStack Query. Copy the design, not the
 * import (apps/mobile/CLAUDE.md); when the core versions change, sync here.
 *
 * `groupRuntimesByMachine` has no core counterpart: web's equivalent lives in
 * packages/views/runtimes/components/runtime-machines.ts and carries workload
 * summaries, section splitting, filters and a synthetic "this machine" row —
 * none of which mobile renders. This is the grouping only.
 */
import type { RuntimeDevice } from "@multica/core/types";

/**
 * Provider slugs whose display name isn't just a capitalization of the slug.
 * Mirrors PROVIDER_DISPLAY_NAMES in packages/core/runtimes/display.ts, itself
 * mirroring the daemon's `runtimeDisplayNameOverrides`.
 */
const PROVIDER_DISPLAY_NAMES: Record<string, string> = {
  codearts: "CodeArts",
  dsh: "DeepSeek Harness",
  qoderclicn: "Qoder CN",
  traecli: "Trae",
  qwen: "Qwen Code",
  qwenpaw: "QwenPaw",
  mcode: "MiniMax Code",
  omp: "Oh-My-Pi",
  zeroclaw: "ZeroClaw",
};

export function providerDisplayName(provider: string): string {
  const slug = provider.trim();
  if (!slug) return "";
  return (
    PROVIDER_DISPLAY_NAMES[slug] ?? slug.charAt(0).toUpperCase() + slug.slice(1)
  );
}

/**
 * The name to show for a runtime: the user's alias when set, otherwise the
 * daemon-proposed default. Defends against a backend that omits `custom_name`
 * and against a whitespace-only override.
 */
export function runtimeDisplayName(
  runtime: Pick<RuntimeDevice, "name" | "custom_name">,
): string {
  const custom = runtime.custom_name?.trim();
  return custom ? custom : runtime.name;
}

/** The daemon's own CLI version, stamped onto `metadata.cli_version`. */
export function runtimeCliVersion(
  metadata: Record<string, unknown> | undefined,
): string | null {
  const value = metadata?.cli_version;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

/** One CLI the daemon found on the machine but refused to register. */
export interface RuntimeSkippedAgent {
  provider: string;
  reason: string;
}

/**
 * Reads `metadata.skipped_agents` — a provider the daemon DID find mapped to
 * why its probe refused it (version below minimum, version undetectable,
 * binary not executable). Returns `null` when the row predates the field, so
 * callers can tell "nothing skipped" from "this row never reported"; `[]` when
 * the daemon reported an empty map.
 */
export function readRuntimeSkippedAgents(
  metadata: Record<string, unknown> | undefined,
): RuntimeSkippedAgent[] | null {
  const raw = metadata?.skipped_agents;
  if (raw === undefined || raw === null) return null;
  if (typeof raw !== "object" || Array.isArray(raw)) return null;
  return Object.entries(raw as Record<string, unknown>)
    .map(([provider, reason]) => ({
      provider: provider.trim(),
      reason: typeof reason === "string" ? reason.trim() : "",
    }))
    .filter(({ provider, reason }) => provider !== "" && reason !== "")
    .sort((a, b) => a.provider.localeCompare(b.provider));
}

/**
 * The machine's current skip set: the snapshot from the row that reported most
 * recently. An older row must not win, or a repaired CLI keeps being reported
 * by whichever runtime went offline before the fix.
 */
export function machineSkippedAgents(
  runtimes: RuntimeDevice[],
): RuntimeSkippedAgent[] {
  let freshest: { at: number; skipped: RuntimeSkippedAgent[] } | null = null;
  for (const runtime of runtimes) {
    const skipped = readRuntimeSkippedAgents(runtime.metadata);
    if (skipped === null) continue;
    const parsed = runtime.last_seen_at
      ? new Date(runtime.last_seen_at).getTime()
      : 0;
    const seenAt = Number.isFinite(parsed) ? parsed : 0;
    if (!freshest || seenAt > freshest.at) freshest = { at: seenAt, skipped };
  }
  return freshest?.skipped ?? [];
}

/** Whether the machine's CLI is signed in to its provider. */
export interface RuntimeCliAuthState {
  authenticated: boolean;
  checked_at?: string;
  provider?: string;
  reason?: string;
}

/**
 * Hand-rolled rather than schema-parsed, exactly like the core version: an
 * entry without a real boolean `authenticated` is treated as absent, so a
 * malformed record can never render as "signed in".
 */
export function readRuntimeCliAuthState(
  metadata: Record<string, unknown> | undefined,
): RuntimeCliAuthState | null {
  const value = metadata?.cli_auth;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const raw = value as Record<string, unknown>;
  if (typeof raw.authenticated !== "boolean") return null;
  return {
    authenticated: raw.authenticated,
    checked_at: typeof raw.checked_at === "string" ? raw.checked_at : undefined,
    provider: typeof raw.provider === "string" ? raw.provider : undefined,
    reason: typeof raw.reason === "string" ? raw.reason : undefined,
  };
}

/** One machine: the daemon and every CLI runtime it registered. */
export interface RuntimeMachine {
  /** `daemon_id` when the rows carry one, else the single runtime's id. */
  id: string;
  /** The row the screen navigates by — the freshest of the group. */
  representativeId: string;
  title: string;
  runtimes: RuntimeDevice[];
  onlineCount: number;
  lastSeenAt: string | null;
  cliVersion: string | null;
}

function seenAt(runtime: RuntimeDevice): number {
  const parsed = runtime.last_seen_at
    ? new Date(runtime.last_seen_at).getTime()
    : 0;
  return Number.isFinite(parsed) ? parsed : 0;
}

/**
 * Groups runtimes by the daemon that registered them. A cloud runtime with no
 * `daemon_id` is its own machine, which is also what web does with the rows it
 * cannot attribute to a host.
 *
 * Ordering: machines by most-recently-seen first, runtimes inside a machine by
 * provider name, so the list is stable across refetches rather than following
 * whatever order the endpoint happened to return.
 */
export function groupRuntimesByMachine(
  runtimes: RuntimeDevice[],
): RuntimeMachine[] {
  const groups = new Map<string, RuntimeDevice[]>();
  for (const runtime of runtimes) {
    const key = runtime.daemon_id ?? runtime.id;
    const group = groups.get(key) ?? [];
    group.push(runtime);
    groups.set(key, group);
  }

  const machines: RuntimeMachine[] = [];
  for (const [id, group] of groups) {
    const freshest = group.reduce((best, r) =>
      seenAt(r) > seenAt(best) ? r : best,
    );
    const sorted = [...group].sort((a, b) =>
      providerDisplayName(a.provider).localeCompare(
        providerDisplayName(b.provider),
      ),
    );
    machines.push({
      id,
      representativeId: freshest.id,
      // The daemon bakes the host into every row's name ("Codex (host)"), so
      // the freshest row's device_info is the closest thing to a machine name.
      title: freshest.device_info || runtimeDisplayName(freshest),
      runtimes: sorted,
      onlineCount: group.filter((r) => r.status === "online").length,
      lastSeenAt: freshest.last_seen_at,
      cliVersion: runtimeCliVersion(freshest.metadata),
    });
  }

  return machines.sort((a, b) => {
    const at = a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : 0;
    const bt = b.lastSeenAt ? new Date(b.lastSeenAt).getTime() : 0;
    return bt - at;
  });
}
