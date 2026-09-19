import { z } from "zod";

// Twenty CRM (OS plan, chantier 2): a workspace connected to a Twenty
// instance. Agents get Twenty's MCP server, CRM webhooks land in triage,
// accepted items link back as a Task on the record, members pair by email.

/** A connection is usable or refused by Twenty until reconnected. */
export type TwentyConnectionStatus = "connected" | "error";

// Enum-shaped fields stay `z.string()` with a `.catch()` so a value from a
// newer server still parses; components carry a default branch.
export const TwentyConnectionSchema = z.object({
  base_url: z.string().catch(""),
  status: z.string().catch("connected"),
  last_error: z.string().catch("").default(""),
  events: z.array(z.string()).catch([]).default([]),
  expose_to_agents: z.boolean().catch(false),
  webhook_registered: z.boolean().catch(false),
  inbound_path: z.string().catch("").default(""),
  inbound_token: z.string().catch("").default(""),
  twenty_workspace_name: z.string().catch("").default(""),
  mcp_url: z.string().catch(""),
  connected_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();
export type TwentyConnection = z.infer<typeof TwentyConnectionSchema>;

export const TwentyStatusSchema = z.object({
  available: z.boolean().catch(false),
  connected: z.boolean().catch(false),
  connection: TwentyConnectionSchema.nullable().catch(null).default(null),
  default_events: z.array(z.string()).catch([]).default([]),
}).loose();
export type TwentyStatus = z.infer<typeof TwentyStatusSchema>;

export const TwentyMemberLinkSchema = z.object({
  user_id: z.string().catch("").default(""),
  name: z.string().catch("").default(""),
  email: z.string().catch(""),
  twenty_id: z.string().catch("").default(""),
  twenty_name: z.string().catch("").default(""),
  linked: z.boolean().catch(false),
  twenty_only: z.boolean().catch(false).default(false),
}).loose();
export type TwentyMemberLink = z.infer<typeof TwentyMemberLinkSchema>;

export const TwentyMembersSchema = z.object({
  members: z.array(TwentyMemberLinkSchema).catch([]).default([]),
}).loose();

/** What the UI renders when the response is unusable: nothing connected. */
export const EMPTY_TWENTY_STATUS: TwentyStatus = {
  available: false,
  connected: false,
  connection: null,
  default_events: [],
};

/** The subscription a fresh connection proposes (mirrors the server default). */
export const DEFAULT_TWENTY_EVENTS = ["opportunity.*", "person.created", "company.created", "task.created"];

/** Twenty's `object.operation` form, with `*` allowed on either side. */
export const TWENTY_EVENT_PATTERN = /^[a-z][a-z0-9_]*\.(\*|[a-z][a-z0-9_]*)$|^\*\.[a-z][a-z0-9_]*$/;

export function normalizeTwentyEvents(input: string): string[] {
  const seen = new Set<string>();
  for (const raw of input.split(/[\s,]+/)) {
    const e = raw.trim().toLowerCase();
    if (e) seen.add(e);
  }
  return [...seen].sort();
}

export function invalidTwentyEvents(events: string[]): string[] {
  return events.filter((e) => !TWENTY_EVENT_PATTERN.test(e));
}

export interface TwentyConnectInput {
  base_url: string;
  api_key: string;
  events: string[];
  expose_to_agents: boolean;
}

export interface TwentySettingsInput {
  events: string[];
  expose_to_agents: boolean;
}
