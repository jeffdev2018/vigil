import { parseMentions } from "./comment-trigger-outcomes";

// Agent-to-agent messages (F19 / JEF-32).
//
// An A2A message is an ORDINARY comment carrying `a2a_intent`. The column is a
// free string with no CHECK on purpose (migration 828), so this client will
// eventually meet an intent it does not know — a newer backend, or a value
// written before this build shipped. That case renders as a plain comment: an
// unlabelled chip would be worse than no chip, and the body is fully visible
// either way.

export const A2A_INTENTS = ["question", "review", "handoff"] as const;
export type A2AIntent = (typeof A2A_INTENTS)[number];

/** The intent this build can label, or null for "render as an ordinary comment". */
export function knownA2AIntent(intent: string | null | undefined): A2AIntent | null {
  return A2A_INTENTS.includes(intent as A2AIntent) ? (intent as A2AIntent) : null;
}

export interface A2ARecipient {
  agentId: string;
  /** The label the SERVER wrote into the markup — the agent's name at send time. */
  label: string;
}

/**
 * The agent an A2A message addresses, read back out of the mention markup the
 * server composed. There is no recipient field on the wire and there should not
 * be one: the mention IS the address, and reading it back keeps the chip and the
 * run that was actually enqueued in agreement by construction.
 *
 * The FIRST agent mention wins. The composed body carries exactly one, and a
 * body that somehow carries more was still delivered to the first.
 */
export function a2aRecipientFromContent(content: string): A2ARecipient | null {
  for (const { label, type, id } of parseMentions(content ?? "")) {
    if (type === "agent") return { agentId: id, label };
  }
  return null;
}
