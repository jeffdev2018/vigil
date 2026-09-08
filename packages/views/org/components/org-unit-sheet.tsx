"use client";

import { useState } from "react";
import { Lock, Plus, Trash2, X } from "lucide-react";
import {
  ORG_AUTONOMY_ORDER,
  ORG_DECIDER_CLASSES,
  ORG_NON_NEGOTIABLE_DENY,
  ORG_PROPERTIES,
  type OrgProblem,
} from "@multica/core/org/validate";
import type { Goal, MemberWithUser, OrgAutonomy, OrgProperty, OrgUnit } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { OrgProblemList } from "./org-problem-list";

const SELECT_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";

export interface OrgUnitSheetProps {
  unit: OrgUnit;
  /** Every unit, so a member can be moved to any of them from the keyboard. */
  units: OrgUnit[];
  problems: OrgProblem[];
  members: MemberWithUser[];
  goals: Goal[];
  actorName: (type: "member" | "agent", id: string) => string;
  readOnly: boolean;
  onPatch: (patch: Partial<OrgUnit>) => void;
  onMoveMember: (memberIndex: number, toUnitId: string) => void;
  onDelete: () => void;
  onClose: () => void;
}

/** Editable list of verbs. The non-negotiable denials are rendered locked with
 *  their reason rather than hidden: the user has to see what is already refused. */
function VerbList({
  label,
  verbs,
  locked,
  lockedReason,
  readOnly,
  onChange,
}: {
  label: string;
  verbs: string[];
  locked?: readonly string[];
  lockedReason?: string;
  readOnly: boolean;
  onChange: (next: string[]) => void;
}) {
  const { t } = useT("org");
  const [draft, setDraft] = useState("");
  const lockedSet = new Set(locked ?? []);
  const add = () => {
    const value = draft.trim();
    if (value === "" || verbs.includes(value) || lockedSet.has(value)) return;
    onChange([...verbs, value]);
    setDraft("");
  };
  return (
    <div className="flex flex-col gap-1">
      <span className="text-caption text-muted-foreground">{label}</span>
      <ul className="flex flex-wrap gap-1">
        {[...lockedSet].map((verb) => (
          <li key={verb}>
            <Badge variant="outline" className="gap-1 text-muted-foreground" title={lockedReason}>
              <Lock className="size-3" aria-hidden />
              {verb}
            </Badge>
          </li>
        ))}
        {verbs
          .filter((v) => !lockedSet.has(v))
          .map((verb) => (
            <li key={verb}>
              <Badge variant="outline" className="gap-1">
                {verb}
                {!readOnly && (
                  <button
                    type="button"
                    aria-label={t(($) => $.unit.remove, { item: verb })}
                    onClick={() => onChange(verbs.filter((v) => v !== verb))}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="size-3" />
                  </button>
                )}
              </Badge>
            </li>
          ))}
      </ul>
      {!readOnly && (
        <div className="flex gap-1">
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
            className="h-8"
            placeholder={t(($) => $.unit.verb_placeholder)}
            aria-label={label}
          />
          <Button type="button" size="sm" variant="outline" onClick={add} aria-label={t(($) => $.unit.add)}>
            <Plus className="size-3.5" />
          </Button>
        </div>
      )}
    </div>
  );
}

/**
 * The unit's own record: identity, Trust Dial, what it may decide alone, what
 * it handles, who decides on its external effects, its budget and its members.
 * Every control writes into the same `definition` the advanced JSON edits.
 */
export function OrgUnitSheet({
  unit,
  units,
  problems,
  members,
  goals,
  actorName,
  readOnly,
  onPatch,
  onMoveMember,
  onDelete,
  onClose,
}: OrgUnitSheetProps) {
  const { t } = useT("org");
  const excluded = new Set(unit.excludes ?? []);
  const handles = (p: OrgProperty) => !excluded.has(p);
  const toggleProperty = (p: OrgProperty) => {
    const next = handles(p) ? [...excluded, p] : (unit.excludes ?? []).filter((x) => x !== p);
    onPatch({ excludes: next });
  };
  const trustIndex = Math.max(0, ORG_AUTONOMY_ORDER.indexOf(unit.autonomy));

  return (
    <aside data-testid="org-unit-sheet" aria-label={t(($) => $.unit.title)} className="flex flex-col gap-3 rounded-md border p-3">
      <div className="flex items-center gap-2">
        <h3 className="text-body font-medium">{t(($) => $.unit.title)}</h3>
        <div className="ml-auto flex items-center gap-1">
          {!readOnly && (
            <Button type="button" size="sm" variant="ghost" className="text-destructive" onClick={onDelete} aria-label={t(($) => $.unit.remove_unit)}>
              <Trash2 className="size-3.5" />
            </Button>
          )}
          <Button type="button" size="sm" variant="ghost" onClick={onClose} aria-label={t(($) => $.unit.close)}>
            <X className="size-3.5" />
          </Button>
        </div>
      </div>

      <OrgProblemList problems={problems} />

      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.unit.name)}
        <Input value={unit.name} onChange={(e) => onPatch({ name: e.target.value })} disabled={readOnly} />
      </label>

      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.unit.mission)}
        <select
          className={SELECT_CLASS}
          value={unit.mission_goal_id ?? ""}
          onChange={(e) => onPatch({ mission_goal_id: e.target.value })}
          disabled={readOnly}
        >
          <option value="">{t(($) => $.unit.mission_none)}</option>
          {goals.map((g) => (
            <option key={g.id} value={g.id}>{g.title}</option>
          ))}
        </select>
      </label>

      <fieldset className="flex flex-col gap-1" disabled={readOnly}>
        <legend className="text-caption text-muted-foreground">{t(($) => $.unit.trust)}</legend>
        <div role="radiogroup" aria-label={t(($) => $.unit.trust)} className="flex flex-col gap-0.5">
          {ORG_AUTONOMY_ORDER.map((level: OrgAutonomy, i) => (
            <button
              key={level}
              type="button"
              role="radio"
              aria-checked={unit.autonomy === level}
              disabled={readOnly}
              data-testid={`org-trust-${level}`}
              onClick={() => onPatch({ autonomy: level })}
              className={cn(
                "flex items-center gap-2 rounded-md border px-2 py-1 text-left text-caption hover:bg-accent/70",
                unit.autonomy === level && "border-brand font-medium data-[active=true]:hover:bg-accent",
              )}
              data-active={unit.autonomy === level}
            >
              <span aria-hidden className={cn("h-1.5 w-6 rounded-full", i <= trustIndex ? "bg-brand" : "bg-muted")} />
              {t(($) => $.unit.trust_level[level])}
            </button>
          ))}
        </div>
      </fieldset>

      <VerbList
        label={t(($) => $.unit.allow)}
        verbs={unit.allow ?? []}
        readOnly={readOnly}
        onChange={(allow) => onPatch({ allow })}
      />
      <VerbList
        label={t(($) => $.unit.deny)}
        verbs={unit.deny ?? []}
        locked={ORG_NON_NEGOTIABLE_DENY}
        lockedReason={t(($) => $.unit.locked_reason)}
        readOnly={readOnly}
        onChange={(deny) => onPatch({ deny })}
      />

      <fieldset className="flex flex-col gap-1" disabled={readOnly}>
        <legend className="text-caption text-muted-foreground">{t(($) => $.unit.properties)}</legend>
        {ORG_PROPERTIES.map((p) => (
          <label key={p} className="flex items-center gap-2 text-caption">
            <input type="checkbox" checked={handles(p)} onChange={() => toggleProperty(p)} disabled={readOnly} />
            {t(($) => $.unit.property[p])}
          </label>
        ))}
        <label className="flex items-center gap-2 text-caption">
          <input
            type="checkbox"
            checked={unit.human_approval === true}
            onChange={(e) => onPatch({ human_approval: e.target.checked })}
            disabled={readOnly}
          />
          {t(($) => $.unit.human_approval)}
        </label>
      </fieldset>

      {handles("external_effects") && (
        <fieldset className="flex flex-col gap-1" disabled={readOnly}>
          <legend className="text-caption text-muted-foreground">{t(($) => $.unit.deciders)}</legend>
          {ORG_DECIDER_CLASSES.map((klass) => (
            <label key={klass} className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.unit.decider[klass])}
              <select
                className={SELECT_CLASS}
                value={(unit.deciders ?? {})[klass] ?? ""}
                onChange={(e) => onPatch({ deciders: { ...(unit.deciders ?? {}), [klass]: e.target.value } })}
                disabled={readOnly}
              >
                <option value="">{t(($) => $.unit.decider_none)}</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>{m.name}</option>
                ))}
              </select>
            </label>
          ))}
        </fieldset>
      )}

      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.unit.budget)}
        <Input
          type="number"
          min={0}
          value={String(unit.budget_usd_ticks ?? 0)}
          onChange={(e) => onPatch({ budget_usd_ticks: Number(e.target.value) || 0 })}
          disabled={readOnly}
        />
      </label>

      <div className="flex flex-col gap-1">
        <span className="text-caption text-muted-foreground">{t(($) => $.unit.members)}</span>
        {(unit.members ?? []).length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.unit.no_members)}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {(unit.members ?? []).map((m, i) => (
              <li key={`${m.type}:${m.id}`} data-testid="org-sheet-member" className="flex items-center gap-2 text-caption">
                <span className="truncate">{actorName(m.type, m.id)}</span>
                <select
                  className={cn(SELECT_CLASS, "ml-auto w-40")}
                  value={unit.id}
                  disabled={readOnly}
                  aria-label={t(($) => $.unit.move_to, { name: actorName(m.type, m.id) })}
                  onChange={(e) => onMoveMember(i, e.target.value)}
                >
                  {units.map((u) => (
                    <option key={u.id} value={u.id}>{u.name}</option>
                  ))}
                </select>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}
