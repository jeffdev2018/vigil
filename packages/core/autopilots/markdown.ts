import { z } from "zod";
import { parseWithFallback } from "../api/schema";

// DAEMON.md (F24 / JEF-15) client contract.
//
// The SERVER owns validation: it is the boundary that decides what a
// declaration may say, and it reports every problem with the line it happened
// on. This module holds the response shapes and the small pure helpers the
// import dialog needs — it deliberately does not re-implement the parser,
// because a second opinion about what is valid would eventually disagree with
// the one that actually writes rows.

export const DAEMON_TRIGGER_KINDS = ["schedule", "webhook"] as const;
export const DAEMON_OUTPUTS = ["issue", "run_only"] as const;

// `kind` and `outputs` stay z.string(): a server that grows a new trigger kind
// must degrade to a generic row here, not blank the whole preview.
const DaemonTriggerSchema = z.object({
  kind: z.string(),
  cron: z.string().optional(),
  timezone: z.string().optional(),
  label: z.string().optional(),
  line: z.number().optional(),
}).loose();

const DaemonBudgetSchema = z.object({
  runs_per_day: z.number().optional(),
  max_minutes: z.number().optional(),
}).loose();

const DaemonFrontmatterSchema = z.object({
  name: z.string().default(""),
  role: z.string().default(""),
  agent: z.string().default(""),
  triggers: z.array(DaemonTriggerSchema).default([]),
  budget: DaemonBudgetSchema.nullish(),
  outputs: z.string().default("issue"),
  issue_title_template: z.string().optional(),
}).loose();

const DaemonParseErrorSchema = z.object({
  line: z.number().default(0),
  message: z.string().default(""),
}).loose();

export const DaemonImportPreviewSchema = z.object({
  valid: z.boolean().default(false),
  errors: z.array(DaemonParseErrorSchema).default([]),
  frontmatter: DaemonFrontmatterSchema.nullish(),
  body: z.string().default(""),
  warnings: z.array(z.string()).default([]),
  agent_id: z.string().optional(),
  agent_candidates: z.array(z.string()).default([]),
  digest: z.string().default(""),
  existing_autopilot_id: z.string().optional(),
  unchanged: z.boolean().default(false),
}).loose();

export const DaemonImportResultSchema = z.object({
  status: z.string().default(""),
  autopilot: z.object({ id: z.string().default("") }).loose(),
  triggers: z.array(DaemonTriggerSchema).default([]),
  skill_id: z.string().default(""),
  digest: z.string().default(""),
  warnings: z.array(z.string()).default([]),
}).loose();

export type DaemonTriggerDeclaration = z.infer<typeof DaemonTriggerSchema>;
export type DaemonBudgetDeclaration = z.infer<typeof DaemonBudgetSchema>;
export type DaemonFrontmatter = z.infer<typeof DaemonFrontmatterSchema>;
export type DaemonParseError = z.infer<typeof DaemonParseErrorSchema>;
export type DaemonImportPreview = z.infer<typeof DaemonImportPreviewSchema>;
export type DaemonImportResult = z.infer<typeof DaemonImportResultSchema>;

export type DaemonImportStrategy = "fail" | "overwrite" | "rename";

// An unreadable preview is NOT a valid one: degrading to `valid: true` would
// invite the dialog to offer an import the server is about to reject.
export const FALLBACK_DAEMON_IMPORT_PREVIEW: DaemonImportPreview = {
  valid: false,
  errors: [{ line: 0, message: "" }],
  body: "",
  warnings: [],
  agent_candidates: [],
  digest: "",
  unchanged: false,
};

export const FALLBACK_DAEMON_IMPORT_RESULT: DaemonImportResult = {
  status: "",
  autopilot: { id: "" },
  triggers: [],
  skill_id: "",
  digest: "",
  warnings: [],
};

export function parseDaemonImportPreview(raw: unknown, endpoint: string): DaemonImportPreview {
  return parseWithFallback(raw, DaemonImportPreviewSchema, FALLBACK_DAEMON_IMPORT_PREVIEW, { endpoint });
}

export function parseDaemonImportResult(raw: unknown, endpoint: string): DaemonImportResult {
  return parseWithFallback(raw, DaemonImportResultSchema, FALLBACK_DAEMON_IMPORT_RESULT, { endpoint });
}

// daemonErrorsByLine indexes the server's line errors so a source view can mark
// up the document. Several problems can land on one line, so the value is a
// list, and line 0 (a whole-document problem such as a missing required key)
// is kept under 0 rather than dropped.
export function daemonErrorsByLine(errors: readonly DaemonParseError[]): Map<number, DaemonParseError[]> {
  const byLine = new Map<number, DaemonParseError[]>();
  for (const error of errors) {
    const line = typeof error.line === "number" ? error.line : 0;
    const existing = byLine.get(line);
    if (existing) existing.push(error);
    else byLine.set(line, [error]);
  }
  return byLine;
}

// daemonScheduleCrons pulls the cron expressions worth previewing next runs
// for. Only schedule triggers have one; a webhook entry that somehow carries a
// cron (an older or newer server) is skipped rather than previewed as if it
// fired on a clock.
export function daemonScheduleCrons(
  triggers: readonly DaemonTriggerDeclaration[] | undefined,
): Array<{ cron: string; timezone: string; label: string }> {
  if (!triggers) return [];
  const out: Array<{ cron: string; timezone: string; label: string }> = [];
  for (const trigger of triggers) {
    if (trigger?.kind !== "schedule") continue;
    const cron = trigger.cron?.trim();
    if (!cron) continue;
    out.push({ cron, timezone: trigger.timezone?.trim() || "UTC", label: trigger.label ?? "" });
  }
  return out;
}
