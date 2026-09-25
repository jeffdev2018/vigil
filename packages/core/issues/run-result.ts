// A run's reported result (`AgentTask.result`, a delivery's `run.result`) is
// `unknown` on the wire: the daemon's completion payload (`output`, `pr_url`,
// `work_dir`, `session_id`, `goal_loop`, …) or an older `{ summary }`. People
// read the output, the pull request and the goal verdict; working directories,
// session ids and the judge's progress signature are machine state kept for
// resumption — local paths of someone's computer — and are never displayed.
// Pure and zod-free so mobile can import it (apps/mobile/CLAUDE.md).

/** Keys rendered on their own or never shown; everything else is "technical". */
const PRESENTED_KEYS = new Set(["output", "summary", "pr_url", "goal_loop"]);
const HIDDEN_KEYS = new Set(["work_dir", "durable_work_dir", "session_id", "retired_session_id", "env_root"]);
const HIDDEN_GOAL_KEYS = new Set(["signature"]);

export interface RunResultGoal {
  outcome: string;
  blocker: string | null;
  reason: string | null;
  nextStep: string | null;
  evidence: string[];
}

export interface RunResultView {
  /** Markdown the agent reported as its output, when any. */
  output: string | null;
  /** An http(s) pull request link; anything else is dropped. */
  prUrl: string | null;
  goal: RunResultGoal | null;
  /** Remaining fields for a folded "technical details", hidden keys removed; null when nothing is left. */
  technical: Record<string, unknown> | null;
}

function text(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function parseGoal(raw: unknown): { goal: RunResultGoal | null; rest: Record<string, unknown> } {
  const goal = record(raw);
  if (!goal) return { goal: null, rest: {} };
  // The server sends evidence as one line; older payloads carried a list.
  const evidence = Array.isArray(goal.evidence)
    ? goal.evidence.filter((e): e is string => typeof e === "string" && e.trim() !== "")
    : text(goal.evidence) ? [goal.evidence as string] : [];
  const rest: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(goal)) {
    if (!HIDDEN_GOAL_KEYS.has(key) && !["outcome", "blocker", "reason", "next_step", "evidence"].includes(key)) rest[key] = value;
  }
  return {
    goal: {
      outcome: typeof goal.outcome === "string" ? goal.outcome : "",
      blocker: text(goal.blocker),
      reason: text(goal.reason),
      nextStep: text(goal.next_step),
      evidence,
    },
    rest,
  };
}

/** Null when nothing was reported. */
export function parseRunResult(result: unknown): RunResultView | null {
  if (result == null) return null;
  if (typeof result === "string") return text(result) ? { output: result, prUrl: null, goal: null, technical: null } : null;
  const obj = record(result);
  if (!obj) return { output: null, prUrl: null, goal: null, technical: { value: result } };
  const prUrl = text(obj.pr_url);
  const { goal, rest: goalRest } = parseGoal(obj.goal_loop);
  const technical: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    if (!PRESENTED_KEYS.has(key) && !HIDDEN_KEYS.has(key) && value !== "" && value != null) technical[key] = value;
  }
  if (Object.keys(goalRest).length > 0) technical.goal_loop = goalRest;
  const view: RunResultView = {
    output: text(obj.output) ?? text(obj.summary),
    prUrl: prUrl && /^https?:\/\//i.test(prUrl) ? prUrl : null,
    goal,
    technical: Object.keys(technical).length > 0 ? technical : null,
  };
  return view.output || view.prUrl || view.goal || view.technical ? view : null;
}
