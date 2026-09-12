// "Where would this request go?" (K75). Pure shaping for the org tester: the
// free text a person types becomes the request the simulate endpoint matches,
// and the ladder it answers with becomes one readable line.

import type { OrgSimulationRef, OrgSimulationRequest } from "../types";

type OrgSimulationRequestBody = OrgSimulationRequest["request"];

/** The live matcher weighs the title on its own, so a title stays a title. */
const TITLE_MAX = 200;

/**
 * Free text as a request: the first line is the title, the rest the
 * description. A single line longer than a title keeps its whole text as the
 * description too, so no word the matcher reads is dropped by the split.
 */
export function orgRequestFromText(text: string): OrgSimulationRequestBody {
  const trimmed = text.trim();
  const lines = trimmed.split("\n");
  const head = (lines[0] ?? "").trim();
  if (head.length > TITLE_MAX) return { title: head.slice(0, TITLE_MAX), description: trimmed };
  return { title: head, description: lines.slice(1).join("\n").trim() };
}

/**
 * An existing issue as the tester's form: title and body as one editable free
 * text, label names apart because the matcher reads them as labels, not prose.
 */
export function orgFormFromIssue(issue: {
  title: string;
  description?: string | null;
  labels?: { name: string }[];
}): { text: string; labels: string[] } {
  const body = (issue.description ?? "").trim();
  const title = issue.title.trim();
  return {
    text: body === "" ? title : `${title}\n${body}`,
    labels: (issue.labels ?? []).map((l) => l.name.trim()).filter((n) => n !== ""),
  };
}

/**
 * The escalation ladder in reading order. `rootLabel` when the unit answers to
 * nobody — an empty line would read as "unknown" instead of "this is the top".
 */
export function orgEscalationLabel(path: OrgSimulationRef[], rootLabel: string): string {
  const names = path.map((r) => r.unit_name.trim() || r.unit_id.trim()).filter((n) => n !== "");
  return names.length === 0 ? rootLabel : names.join(" → ");
}
