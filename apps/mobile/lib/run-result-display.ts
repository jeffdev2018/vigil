/**
 * A run's reported result for the mobile delivery review.
 *
 * Parsing is the shared pure `parseRunResult` (packages/core/issues/run-result.ts,
 * also behind web's DeliveryResult): output, pull request and goal verdict are
 * shown; work_dir / session_id / the judge's signature never are. Mobile is
 * English-only, so the goal labels reuse lib/issue-goal-display.ts.
 */
import { parseRunResult } from "@multica/core/issues/run-result";
import { issueGoalBlockerLabel, issueGoalOutcomeLabel } from "./issue-goal-display";
import { previewJson } from "./run-replay-display";

export interface RunResultRow {
  label: string;
  value: string;
}

export interface RunResultDisplay {
  output: string | null;
  prUrl: string | null;
  rows: RunResultRow[];
  /** Pretty JSON of the remaining fields for a folded "Technical details"; "" when none. */
  technical: string;
}

export function runResultDisplay(result: unknown): RunResultDisplay | null {
  const view = parseRunResult(result);
  if (!view) return null;
  const rows: RunResultRow[] = [];
  const goal = view.goal;
  const outcome = goal ? issueGoalOutcomeLabel(goal.outcome) : null;
  if (outcome) rows.push({ label: "Goal", value: outcome });
  if (goal?.blocker) rows.push({ label: "Blocker", value: issueGoalBlockerLabel(goal.blocker) });
  if (goal?.reason) rows.push({ label: "Reason", value: goal.reason });
  if (goal && goal.evidence.length > 0) rows.push({ label: "Evidence", value: goal.evidence.join("\n") });
  if (goal?.nextStep) rows.push({ label: "Next step", value: goal.nextStep });
  return { output: view.output, prUrl: view.prUrl, rows, technical: previewJson(view.technical, 40) };
}
