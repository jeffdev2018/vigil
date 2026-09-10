import { z } from "zod";
import type { IssueDecision } from "../types";

// Inline approvals (OS plan, chantier 3): everything a human is asked to
// decide, in one feed, answerable where the person already is.

export type ApprovalSource = "decision" | "transition" | "goal_question";
export type ApprovalKind =
  | "decision"
  | "gate"
  | "plan"
  | "interview"
  | "preview"
  | "watchdog"
  | "pipeline"
  | "goal_attach"
  | "org_assign"
  | "calendar_proposal"
  | "autopilot_proposal"
  | "transition"
  | "goal_question";

const OptionSchema = z.object({
  id: z.string().catch(""),
  label: z.string().catch(""),
  impact: z.string().catch(""),
}).loose();

export const ApprovalGateSchema = z.object({
  id: z.string().catch(""),
  task_id: z.string().catch(""),
  gate_type: z.string().catch(""),
  summary: z.string().catch(""),
  details: z.record(z.string(), z.unknown()).catch({}),
  status: z.string().catch("pending"),
  created_at: z.string().catch(""),
  expires_at: z.string().nullable().catch(null),
  resolved_at: z.string().nullable().catch(null),
}).loose();
export type ApprovalGate = z.infer<typeof ApprovalGateSchema>;

export const ApprovalTransitionSchema = z.object({
  request_id: z.string().catch(""),
  from_status: z.string().catch(""),
  to_status: z.string().catch(""),
  rule_id: z.string().nullable().catch(null),
  approver_roles: z.array(z.string()).catch([]),
}).loose();

export const ApprovalGoalQuestionSchema = z.object({
  kind: z.string().catch("text"),
  prompt: z.string().catch(""),
  options: z.array(z.string()).catch([]),
  run_id: z.string().catch(""),
  asked_at: z.string().catch(""),
}).loose();

export const ApprovalItemSchema = z.object({
  id: z.string().catch(""),
  source: z.string().catch("decision"),
  kind: z.string().catch("decision"),
  issue: z.object({
    id: z.string().catch(""),
    identifier: z.string().catch(""),
    title: z.string().catch(""),
    status: z.string().catch(""),
  }).loose().catch({ id: "", identifier: "", title: "", status: "" }),
  task_id: z.string().catch(""),
  asked_by: z.object({
    type: z.string().catch(""),
    id: z.string().catch(""),
    name: z.string().catch(""),
  }).loose().catch({ type: "", id: "", name: "" }),
  question: z.string().catch(""),
  options: z.array(OptionSchema).catch([]),
  recommended_option_id: z.string().catch(""),
  urgency: z.string().catch("normal"),
  created_at: z.string().catch(""),
  expires_at: z.string().nullable().catch(null),
  sla_deadline_at: z.string().nullable().catch(null),
  can_decide: z.boolean().catch(false),
  cannot_decide_reason: z.string().catch(""),
  decision: z.unknown().nullable().catch(null),
  gate: ApprovalGateSchema.nullable().catch(null),
  transition: ApprovalTransitionSchema.nullable().catch(null),
  goal_question: ApprovalGoalQuestionSchema.nullable().catch(null),
}).loose();
// Not Omit<>: a loose object's index signature makes Omit collapse every
// named field to unknown. The intersection narrows `decision` alone.
export type ApprovalItem = z.infer<typeof ApprovalItemSchema> & { decision: IssueDecision | null };

export const RunHaltSchema = z.object({
  halted: z.boolean().catch(false),
  reason: z.string().catch(""),
  halted_by: z.string().catch(""),
  halted_at: z.string().nullable().catch(null),
}).loose();
export type RunHalt = z.infer<typeof RunHaltSchema>;

export const EMPTY_RUN_HALT: RunHalt = { halted: false, reason: "", halted_by: "", halted_at: null };

export const ApprovalsResponseSchema = z.object({
  approvals: z.array(ApprovalItemSchema).catch([]),
  total: z.number().catch(0),
  run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
}).loose();
export interface ApprovalsResponse {
  approvals: ApprovalItem[];
  total: number;
  run_halt: RunHalt;
}

export const EMPTY_APPROVALS: ApprovalsResponse = { approvals: [], total: 0, run_halt: EMPTY_RUN_HALT };

/** The ask's deadline: the gate's expiry first, else the decision SLA. */
export function approvalDeadline(item: Pick<ApprovalItem, "expires_at" | "sla_deadline_at">): Date | null {
  const raw = item.expires_at ?? item.sla_deadline_at;
  if (!raw) return null;
  const d = new Date(raw);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Whole seconds left before the deadline, 0 when passed, null when none. */
export function approvalSecondsLeft(item: Pick<ApprovalItem, "expires_at" | "sla_deadline_at">, now: Date = new Date()): number | null {
  const deadline = approvalDeadline(item);
  if (!deadline) return null;
  return Math.max(0, Math.floor((deadline.getTime() - now.getTime()) / 1000));
}

/** Compact "12m" / "3h 05m" / "2d" countdown label; "" when none. */
/**
 * What a card stands for, as the UI should label it.
 *
 * An autopilot proposal ("Proposed autopilot · <title> · <cron>") is a plain
 * Decision Card whose options are `autopilot:activate:<id>` /
 * `autopilot:discard:<id>` — the approvals feed classifies goal_attach,
 * org_assign and calendar_proposal by that same option prefix but has no
 * branch for this one yet (server/internal/handler/approvals.go decisionKind),
 * so the prefix is read here. Once the server names the kind, its value wins
 * because `approval.kind` is only reached when no prefix matches.
 */
export function approvalKindOf(approval: Pick<ApprovalItem, "kind" | "options">): string {
  if (approval.options?.some((o) => o.id.startsWith("autopilot:"))) {
    return "autopilot_proposal";
  }
  return approval.kind;
}

export function formatCountdown(seconds: number | null): string {
  if (seconds === null) return "";
  if (seconds <= 0) return "0m";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${String(minutes % 60).padStart(2, "0")}m`;
  return `${Math.floor(hours / 24)}d`;
}

/** Gate arguments and paths the card shows, pulled out of the details bag. */
export function gateDetails(gate: ApprovalGate | null | undefined): {
  params: unknown;
  paths: string[];
  blastRadius: string;
  requiredApprovals: number;
  approvals: number;
} {
  const d = gate?.details ?? {};
  const paths = Array.isArray(d.paths) ? d.paths.filter((p): p is string => typeof p === "string") : [];
  const approvers = Array.isArray(d.approvers) ? d.approvers.length : 0;
  return {
    params: d.params ?? null,
    paths,
    blastRadius: typeof d.blast_radius === "string" ? d.blast_radius : "",
    requiredApprovals: typeof d.required_approvals === "number" ? d.required_approvals : 1,
    approvals: approvers,
  };
}

/** Pending asks the given agent filed — what a chat with that agent shows. */
export function approvalsAskedBy(items: ApprovalItem[], agentId: string): ApprovalItem[] {
  if (!agentId) return [];
  return items.filter((a) => a.asked_by?.type === "agent" && a.asked_by.id === agentId);
}
