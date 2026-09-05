"use client";

import { useT } from "../../i18n";

// Every reason the server persists on a non-dispatched delivery — and the
// same enum a trigger dry-run answers with, which is why this lives in its own
// module rather than inside the deliveries section: one vocabulary, whether
// the decision already happened or is only being previewed. "Ignored" alone
// says a payload arrived and produced nothing; the reason says which of eight
// quite different causes it was — a paused autopilot and an LLM routing
// verdict need different fixes. Unknown codes render verbatim rather than
// disappearing, so a newer backend is never silently unexplained.
const DELIVERY_REASON_CODES = [
  "trigger_disabled",
  "autopilot_paused",
  "autopilot_archived",
  "event_filtered",
  "criteria_not_matched",
  "quota_exceeded",
  "invalid_signature",
  "missing_signature",
] as const;

type DeliveryReasonCode = (typeof DELIVERY_REASON_CODES)[number];

function isKnownReasonCode(code: string): code is DeliveryReasonCode {
  return (DELIVERY_REASON_CODES as readonly string[]).includes(code);
}

/** The classifier's own words, without the code the server prefixes them with.
 *  `criteria_not_matched: no production impact` reads as one sentence beside
 *  its label; the prefix repeated next to the badge does not. */
export function reasonExplanation(reasonCode: string | null, error: string | null): string | null {
  if (!error) return null;
  const trimmed = reasonCode !== null && error.startsWith(`${reasonCode}: `)
    ? error.slice(reasonCode.length + 2)
    : error;
  // An error that is only the code again explains nothing.
  return trimmed === reasonCode || trimmed.trim() === "" ? null : trimmed;
}

/** The localized name of a reason code, the raw code when the server sends one
 *  this build does not know, and null when there is none. */
export function useDeliveryReasonLabel(reasonCode: string | null): string | null {
  const { t } = useT("autopilots");
  if (!reasonCode) return null;
  return isKnownReasonCode(reasonCode)
    ? t(($) => $.deliveries.reason[reasonCode])
    : reasonCode;
}
