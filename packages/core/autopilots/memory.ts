import { z } from "zod";

// Autopilot execution memory (F24 / JEF-15): the document a daemon's runs
// leave for the next one. Read-only from the UI — the write side is authorized
// by a run's task token, so there is no mutation here on purpose.

export const AutopilotMemorySchema = z.object({
  autopilot_id: z.string().default(""),
  content: z.string().default(""),
  // The If-Match token. Kept even though the UI never writes: it is what a
  // support conversation quotes when two runs collided.
  revision: z.number().default(0),
  updated_by_task_id: z.string().nullish(),
  updated_at: z.string().default(""),
  truncated: z.boolean().optional(),
}).loose();

export type AutopilotMemory = z.infer<typeof AutopilotMemorySchema>;

// An unreadable response degrades to "no memory", which renders the empty
// state. The alternative — showing stale or partial text as the daemon's
// current memory — would misinform the person deciding whether to trust it.
export const EMPTY_AUTOPILOT_MEMORY: AutopilotMemory = {
  autopilot_id: "",
  content: "",
  revision: 0,
  updated_by_task_id: null,
  updated_at: "",
};

// hasAutopilotMemory is the empty-state predicate. Whitespace-only content is
// empty: the server stores what a run sent, and a run that wrote "\n" has told
// the next one nothing.
export function hasAutopilotMemory(memory: AutopilotMemory | undefined): boolean {
  return (memory?.content ?? "").trim().length > 0;
}
