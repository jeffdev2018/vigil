import { z } from "zod";

// Code health autopilot (K22): a scheduled read-only agent run reports the
// maintenance work nobody scheduled — debt, stale or vulnerable dependencies,
// untested code — and the server opens one issue per finding it trusts,
// assigned to the maintenance agent.

/** Why a reported finding did not become an issue. */
export type CodeHealthSkipReason =
  | "low_confidence"
  | "duplicate"
  | "cap"
  | "budget"
  | "error";

export type CodeHealthScanStatus = "running" | "completed" | "failed" | "empty";
export type CodeHealthKind = "debt" | "dependency" | "tests" | "security";

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; the UI switches carry a default branch. The
// exported TS types are the narrow unions.
export const CodeHealthFindingSchema = z.object({
  kind: z.string().catch("debt"),
  title: z.string().catch(""),
  summary: z.string().catch(""),
  paths: z.array(z.string()).catch([]).default([]),
  confidence: z.number().catch(0),
  effort: z.string().catch("M"),
  evidence: z.string().catch(""),
  issue_id: z.string().catch(""),
  skipped: z.string().catch(""),
}).loose();

export const CodeHealthScanSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().nullable().catch(null),
  agent_id: z.string().catch(""),
  task_id: z.string().catch(""),
  status: z.string().catch("running"),
  findings: z.array(CodeHealthFindingSchema).catch([]).default([]),
  issues_created: z.number().catch(0),
  error: z.string().catch(""),
  created_at: z.string().catch(""),
  completed_at: z.string().nullable().catch(null),
}).loose();

export const CodeHealthScanListSchema = z.object({
  scans: z.array(CodeHealthScanSchema).catch([]).default([]),
}).loose();

export const CodeHealthScanEnvelopeSchema = z.object({
  scan: CodeHealthScanSchema.nullable().catch(null),
}).loose();

export const CodeHealthSettingsSchema = z.object({
  enabled: z.boolean().catch(false),
  cron: z.string().catch("0 3 * * 1"),
  timezone: z.string().catch("UTC"),
  agent_id: z.string().catch(""),
  project_id: z.string().catch(""),
  max_issues_per_scan: z.number().catch(5),
  min_confidence: z.number().catch(70),
  budget_policy_id: z.string().catch(""),
  enabled_at: z.string().catch(""),
  min_issues_allowed: z.number().catch(1),
  max_issues_allowed: z.number().catch(20),
}).loose();

export interface CodeHealthFinding {
  kind: CodeHealthKind;
  title: string;
  summary: string;
  paths: string[];
  confidence: number;
  effort: string;
  evidence: string;
  /** The issue this finding opened, empty when it opened none. */
  issue_id: string;
  skipped: CodeHealthSkipReason | "";
}

export interface CodeHealthScan {
  id: string;
  workspace_id: string;
  project_id: string | null;
  agent_id: string;
  task_id: string;
  status: CodeHealthScanStatus;
  findings: CodeHealthFinding[];
  issues_created: number;
  error: string;
  created_at: string;
  completed_at: string | null;
}

export interface CodeHealthSettings {
  enabled: boolean;
  cron: string;
  timezone: string;
  agent_id: string;
  project_id: string;
  max_issues_per_scan: number;
  min_confidence: number;
  budget_policy_id: string;
  /** Server-owned cron anchor; a client never sets it. */
  enabled_at: string;
  min_issues_allowed: number;
  max_issues_allowed: number;
}

/** What a PUT may carry. The server owns everything else on the settings row. */
export interface CodeHealthSettingsInput {
  enabled: boolean;
  cron: string;
  timezone: string;
  agent_id: string;
  project_id: string;
  max_issues_per_scan: number;
  min_confidence: number;
  budget_policy_id?: string;
}

export const CODE_HEALTH_DEFAULT_SETTINGS: CodeHealthSettings = {
  enabled: false,
  cron: "0 3 * * 1",
  timezone: "UTC",
  agent_id: "",
  project_id: "",
  max_issues_per_scan: 5,
  min_confidence: 70,
  budget_policy_id: "",
  enabled_at: "",
  min_issues_allowed: 1,
  max_issues_allowed: 20,
};

/**
 * A scan the server is still waiting on. Drives the polling cadence and the
 * "Scan now" button's disabled state — a second scan is refused with 409.
 */
export function hasRunningScan(scans: CodeHealthScan[] | undefined): boolean {
  return (scans ?? []).some((scan) => scan.status === "running");
}

/**
 * The findings a scan actually turned into issues, in report order. Keyed on
 * `issue_id` rather than on the absence of a skip reason: a finding refused by
 * budget admission still HAS its issue — what it does not have is a queued run
 * — and hiding that issue would leave it unreachable from the history.
 */
export function openedFindings(scan: CodeHealthScan): CodeHealthFinding[] {
  return (scan.findings ?? []).filter((f) => !!f.issue_id);
}

/**
 * Colour band for a scan row: a failed scan is a problem, a scan that found
 * nothing is not — it is the normal outcome of a healthy repository.
 */
export function scanTone(
  status: string,
): "success" | "warning" | "destructive" | "muted" {
  switch (status) {
    case "completed":
      return "success";
    case "running":
      return "warning";
    case "failed":
      return "destructive";
    default:
      return "muted";
  }
}
