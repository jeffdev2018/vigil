import type { AutopilotTrigger, WebhookEventFilter } from "@multica/core/types";

/** One ready-to-send sample event for the dry-run editor. */
export interface DryRunSample {
  /** Stable identity for the picker; also what the option shows. */
  id: string;
  /** Pretty-printed JSON body. */
  payload: string;
  /** Headers the server's event inference reads. */
  headers: Record<string, string>;
}

// A generic webhook (no declared filters) has no event vocabulary we can
// derive, so the fallback sample is a plausible deployment event — the shape
// most people are testing a routing criterion against.
const GENERIC_SAMPLE: DryRunSample = {
  id: "deploy.finished",
  payload: JSON.stringify(
    {
      event: "deploy.finished",
      eventPayload: { environment: "production", status: "failed", service: "api" },
    },
    null,
    2,
  ),
  headers: {},
};

/**
 * Build one sample per declared event filter, so the editor opens on a payload
 * the trigger would actually accept instead of on an empty box the user has to
 * guess at. Filters with several actions yield one sample per action: the
 * action is what the matcher keys on, and a single sample would silently only
 * ever exercise the first one.
 *
 * GitHub triggers get the `X-GitHub-Event` header because that — not the body —
 * is where the server infers the event name from for that provider.
 */
export function dryRunSamples(
  trigger: Pick<AutopilotTrigger, "provider" | "event_filters">,
): DryRunSample[] {
  const filters = trigger.event_filters ?? [];
  const isGitHub = trigger.provider === "github";
  const samples: DryRunSample[] = [];
  for (const filter of filters) {
    if (!filter?.event) continue;
    const actions = filter.actions && filter.actions.length > 0 ? filter.actions : [null];
    for (const action of actions) {
      samples.push(sampleFor(filter, action, isGitHub));
    }
  }
  return samples.length > 0 ? samples : [GENERIC_SAMPLE];
}

function sampleFor(
  filter: WebhookEventFilter,
  action: string | null,
  isGitHub: boolean,
): DryRunSample {
  const id = action ? `${filter.event}.${action}` : filter.event;
  const body: Record<string, unknown> = action ? { action } : {};
  if (isGitHub) {
    // The header carries the event; the body only needs the action the
    // matcher reads out of it.
    return {
      id,
      payload: JSON.stringify(body, null, 2),
      headers: { "X-GitHub-Event": filter.event },
    };
  }
  return {
    id,
    payload: JSON.stringify({ event: id, eventPayload: body }, null, 2),
    headers: {},
  };
}

/** Parse the editor's text. Returns the value, or the message to show under
 *  the box. An empty box is its own case: nothing to send, nothing to shout
 *  about yet. */
export function parseDryRunPayload(
  text: string,
): { ok: true; value: unknown } | { ok: false; empty: boolean; message: string } {
  const trimmed = text.trim();
  if (trimmed === "") return { ok: false, empty: true, message: "" };
  try {
    const value: unknown = JSON.parse(trimmed);
    // Scalars are valid JSON but the server rejects them — say so here rather
    // than spend a round-trip on a 400.
    if (value === null || typeof value !== "object") {
      return { ok: false, empty: false, message: "object_or_array" };
    }
    return { ok: true, value };
  } catch (e) {
    return { ok: false, empty: false, message: e instanceof Error ? e.message : "invalid" };
  }
}
