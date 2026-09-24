"use client";

import { Plus, Trash2 } from "lucide-react";
import type { OrgCommittee, OrgDefinition } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Committees: which teams decide a type of decision together, with the
 * termination the server requires (quorum between 1 and the number of teams,
 * at least one round). Nothing reads them at run time yet, which the panel
 * says right under this list.
 */
export function OrgCommitteesEditor({
  definition,
  onChange,
  readOnly,
}: {
  definition: OrgDefinition;
  onChange: (next: OrgDefinition) => void;
  readOnly: boolean;
}) {
  const { t } = useT("org");
  const committees = definition.committees;

  const update = (index: number, patch: Partial<OrgCommittee>) =>
    onChange({ ...definition, committees: committees.map((c, i) => (i === index ? { ...c, ...patch } : c)) });

  const toggleUnit = (index: number, unitId: string, on: boolean) => {
    const c = committees[index];
    if (!c) return;
    const unit_ids = on ? [...c.unit_ids, unitId] : c.unit_ids.filter((id) => id !== unitId);
    // Keep the quorum reachable when a team leaves.
    update(index, { unit_ids, quorum: Math.min(c.quorum, Math.max(1, unit_ids.length)) });
  };

  if (committees.length === 0 && readOnly) {
    return <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.inspector.overview.committees_empty)}</p>;
  }

  return (
    <div className="mt-1 flex flex-col gap-3">
      {committees.length === 0 && (
        <p className="text-caption text-muted-foreground">{t(($) => $.inspector.overview.committees_empty)}</p>
      )}
      {committees.map((c, i) => (
        <fieldset key={i} className="flex flex-col gap-2 rounded-md border border-border p-2.5">
          <div className="flex items-center gap-2">
            <Input
              aria-label={t(($) => $.inspector.overview.committee_decision)}
              placeholder={t(($) => $.inspector.overview.committee_decision)}
              value={c.decision_type}
              disabled={readOnly}
              onChange={(e) => update(i, { decision_type: e.target.value })}
              className="h-8"
            />
            {!readOnly && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.inspector.overview.committee_remove)}
                onClick={() => onChange({ ...definition, committees: committees.filter((_, j) => j !== i) })}
              >
                <Trash2 />
              </Button>
            )}
          </div>
          <div>
            <p className="text-caption text-muted-foreground">{t(($) => $.inspector.overview.committee_teams)}</p>
            <ul className="mt-1 flex flex-col gap-1">
              {definition.units.map((u) => {
                const id = `committee-${i}-${u.id}`;
                return (
                  <li key={u.id} className="flex items-center gap-2 text-caption">
                    <Checkbox
                      id={id}
                      checked={c.unit_ids.includes(u.id)}
                      disabled={readOnly}
                      onCheckedChange={(on) => toggleUnit(i, u.id, on === true)}
                    />
                    <label htmlFor={id} className="truncate">{u.name}</label>
                  </li>
                );
              })}
            </ul>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.inspector.overview.committee_quorum)}
              <Input
                type="number"
                min={1}
                max={Math.max(1, c.unit_ids.length)}
                disabled={readOnly}
                value={c.quorum}
                onChange={(e) => update(i, { quorum: Math.max(1, Math.round(Number(e.target.value) || 1)) })}
                className="h-8"
              />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.inspector.overview.committee_rounds)}
              <Input
                type="number"
                min={1}
                disabled={readOnly}
                value={c.max_rounds}
                onChange={(e) => update(i, { max_rounds: Math.max(1, Math.round(Number(e.target.value) || 1)) })}
                className="h-8"
              />
            </label>
          </div>
        </fieldset>
      ))}
      {!readOnly && (
        <Button
          variant="outline"
          size="sm"
          className="w-fit"
          disabled={definition.units.length === 0}
          onClick={() =>
            onChange({ ...definition, committees: [...committees, { decision_type: "", unit_ids: [], quorum: 1, max_rounds: 3 }] })
          }
        >
          <Plus />
          {t(($) => $.inspector.overview.committee_add)}
        </Button>
      )}
    </div>
  );
}
