import { z } from "zod";

// Linear Bridge (K21): Multica installed as a Linear app. A Linear issue
// assigned to the app's user is mirrored into a Multica issue owned by an
// agent; comments and status then flow both ways.

/** Linear's coarse workflow-state buckets, the keys of the status map. */
export type LinearStateType = "triage" | "backlog" | "unstarted" | "started" | "completed" | "canceled";

/** An installation is usable, refused by Linear, or gone. */
export type LinearInstallationStatus = "active" | "broken" | "revoked";

/** A link is syncing, deliberately stopped, or wedged on an API failure. */
export type LinearSyncState = "active" | "paused" | "broken";

// Enum-shaped fields stay `z.string()` with a safe `.catch()` so a value from a
// newer server still parses; the components carry a default branch. The
// exported TS types are the narrow unions.
export const LinearInstallationSchema = z.object({
  id: z.string().catch(""),
  connected: z.boolean().catch(false),
  configured: z.boolean().catch(false),
  linear_org_id: z.string().catch(""),
  linear_org_name: z.string().catch(""),
  agent_id: z.string().catch(""),
  agent_name: z.string().catch(""),
  status: z.string().catch("active"),
  last_error: z.string().catch(""),
  status_map: z.record(z.string(), z.string()).catch({}).default({}),
  installed_by: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  linear_state_types: z.array(z.string()).catch([]).default([]),
}).loose();
export type LinearInstallation = z.infer<typeof LinearInstallationSchema>;

export const LinearLinkSchema = z.object({
  id: z.string().catch(""),
  issue_id: z.string().catch(""),
  linear_issue_id: z.string().catch(""),
  linear_issue_identifier: z.string().catch(""),
  linear_team_id: z.string().catch(""),
  linear_url: z.string().catch(""),
  sync_state: z.string().catch("active"),
  last_synced_at: z.string().catch(""),
  last_error: z.string().catch(""),
}).loose();
export type LinearLink = z.infer<typeof LinearLinkSchema>;

export const LinearLinkEnvelopeSchema = z.object({
  link: LinearLinkSchema.nullable().catch(null).default(null),
}).loose();

export const LinearOAuthStartSchema = z.object({
  authorize_url: z.string().catch(""),
}).loose();

/**
 * The installation the UI falls back to when the response is unusable. It
 * reads as "nothing connected here", which is the safe thing to render: it
 * offers Connect rather than claiming a connection that may not exist.
 */
export const EMPTY_LINEAR_INSTALLATION: LinearInstallation = {
  id: "",
  connected: false,
  configured: false,
  linear_org_id: "",
  linear_org_name: "",
  agent_id: "",
  agent_name: "",
  status: "active",
  last_error: "",
  status_map: {},
  installed_by: "",
  created_at: "",
  updated_at: "",
  linear_state_types: [],
};

/** The default map the tab shows before a workspace customises one. */
export const DEFAULT_LINEAR_STATUS_MAP: Record<string, string> = {
  triage: "todo",
  backlog: "backlog",
  unstarted: "todo",
  started: "in_progress",
  completed: "done",
  canceled: "cancelled",
};

/** Linear state types in the order the editor lists them. */
export const LINEAR_STATE_TYPES: LinearStateType[] = [
  "triage",
  "backlog",
  "unstarted",
  "started",
  "completed",
  "canceled",
];
