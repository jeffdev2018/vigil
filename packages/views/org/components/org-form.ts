import type { OrgModel, OrgStructure } from "@multica/core/types";

/**
 * The organization's own settings as the page edits them, before they are
 * published. The definition (teams, links, rules) travels separately as a
 * parsed OrgDefinition; this is only what sits beside it on the structure row.
 * Dates are kept in the viewer's local `YYYY-MM-DDTHH:mm` form and budgets in
 * the server's ticks, so a draft round-trips without precision loss.
 */
export interface OrgSettingsForm {
  name: string;
  owner_id: string;
  dissolve_at: string;
  end_condition: string;
  budget: string;
  model: OrgModel;
}

/** RFC 3339 → the viewer's local `YYYY-MM-DDTHH:mm`; empty when unset or unparsable. */
export function toLocalInput(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** The viewer's local `YYYY-MM-DDTHH:mm` → RFC 3339; null when empty or unparsable. */
export function toRFC3339(local: string): string | null {
  if (!local) return null;
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

export function orgSettingsFormOf(s: OrgStructure): OrgSettingsForm {
  return {
    name: s.name,
    owner_id: s.owner_id ?? "",
    dissolve_at: toLocalInput(s.dissolve_at),
    end_condition: s.end_condition ?? "",
    budget: String(s.budget_usd_ticks ?? 0),
    model: s.model,
  };
}
