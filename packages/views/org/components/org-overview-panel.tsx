"use client";

import { useState } from "react";
import type { MemberWithUser, OrgDefinition, OrgModel, OrgStructure } from "@multica/core/types";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Separator } from "@multica/ui/components/ui/separator";
import type { OrgProblem } from "@multica/core/org/validate";
import { toLocalInput, toRFC3339, type OrgSettingsForm } from "./org-form";
import { OrgSelect } from "./org-select";
import { OrgProblemList } from "./org-problem-list";
import { OrgHealth } from "./org-health";
import { useT } from "../../i18n";
import { OrgDateTimePicker } from "./org-datetime-picker";
import { OrgCommitteesEditor } from "./org-committees-editor";
import { ORG_END_CONDITION_VALUES, ORG_MODEL_VALUES, orgEndConditionLabel, orgModelDescription, orgModelLabel } from "../labels";

/** The whole organization: nothing selected on the chart. */
export function OrgOverviewPanel({
  structure,
  definition,
  onChange,
  form,
  onFormChange,
  jsonText,
  onJsonChange,
  jsonError,
  problems,
  readOnly,
  onSelect,
  members,
}: {
  structure: OrgStructure;
  definition: OrgDefinition;
  onChange: (next: OrgDefinition) => void;
  form: OrgSettingsForm;
  onFormChange: <K extends keyof OrgSettingsForm>(key: K, value: OrgSettingsForm[K]) => void;
  jsonText: string;
  onJsonChange: (text: string) => void;
  jsonError: string | null;
  problems: OrgProblem[];
  readOnly: boolean;
  onSelect: (unitId: string) => void;
  members: MemberWithUser[];
}) {
  const { t } = useT("org");
  const [endMode, setEndMode] = useState<"date" | "condition">(form.end_condition ? "condition" : "date");

  const humanIds = new Set<string>();
  const agentIds = new Set<string>();
  for (const u of definition.units) for (const m of u.members) (m.type === "member" ? humanIds : agentIds).add(m.id);

  const vacantRoles = definition.units.flatMap((u) =>
    u.roles.filter((r) => !u.members.some((m) => m.role_id === r.id)).map((r) => ({ unit: u.id, unitName: u.name, role: r.name })),
  );
  const unownedUnits = definition.units.filter((u) => !u.owner_id);
  const nothingToWatch = problems.length === 0 && vacantRoles.length === 0 && unownedUnits.length === 0;

  return (
    <div className="flex flex-col">
      <div className="border-b border-border px-4 py-3">
        <p className="text-micro font-semibold uppercase tracking-wide text-faint-foreground">{t(($) => $.inspector.overview.eyebrow)}</p>
        <h2 className="mt-1 text-title font-semibold tracking-tight">{form.name || structure.name}</h2>
        <p className="mt-1 text-label text-muted-foreground">{orgModelLabel(t, form.model)}</p>
        <p className="text-caption text-muted-foreground">{orgModelDescription(t, form.model)}</p>
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.inspector.overview.teams, { count: definition.units.length })}
          {" · "}
          {t(($) => $.inspector.overview.humans, { count: humanIds.size })}
          {" · "}
          {t(($) => $.inspector.overview.agents, { count: agentIds.size })}
        </p>
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.overview.watch_title)}</h3>
        {nothingToWatch ? (
          <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.inspector.overview.watch_empty)}</p>
        ) : (
          <ul className="mt-2 flex flex-col gap-1.5">
            <OrgProblemList problems={problems} />
            {vacantRoles.map((v) => (
              <li key={`${v.unit}:${v.role}`}>
                <button type="button" className="text-left text-caption text-warning-strong hover:underline" onClick={() => onSelect(v.unit)}>
                  {t(($) => $.inspector.overview.vacant_role, { role: v.role, unit: v.unitName })}
                </button>
              </li>
            ))}
            {unownedUnits.map((u) => (
              <li key={u.id}>
                <button type="button" className="text-left text-caption text-warning-strong hover:underline" onClick={() => onSelect(u.id)}>
                  {t(($) => $.inspector.overview.unit_no_owner, { unit: u.name })}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.overview.activity_title)}</h3>
        <div className="mt-2">
          <OrgHealth structureId={structure.id} />
        </div>
      </div>

      <Collapsible className="border-b border-border px-4 py-3">
        <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">
          {t(($) => $.inspector.overview.settings_title)}
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.name)}
            <Input value={form.name} disabled={readOnly} onChange={(e) => onFormChange("name", e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.coherence.owner)}
            <OrgSelect
              className="w-full"
              value={form.owner_id}
              disabled={readOnly}
              onValueChange={(v) => onFormChange("owner_id", v)}
              items={[{ value: "", label: t(($) => $.form.owner_none) }, ...members.map((m) => ({ value: m.user_id, label: m.name }))]}
            />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.budget)}
            <Input
              type="number"
              min={0}
              step="0.01"
              disabled={readOnly}
              value={Number(form.budget) / 1_000_000}
              onChange={(e) => onFormChange("budget", String(Math.round(Math.max(0, Number(e.target.value) || 0) * 1_000_000)))}
            />
          </label>
          {form.model === "taskforce" && (
            <div className="flex flex-col gap-2 rounded-md border border-border p-3">
              <RadioGroup value={endMode} onValueChange={(v) => setEndMode(v as typeof endMode)}>
                <label className="flex items-center gap-2 text-body">
                  <RadioGroupItem value="date" disabled={readOnly} />
                  {t(($) => $.wizard.flow.taskforce_termination_date)}
                </label>
                <label className="flex items-center gap-2 text-body">
                  <RadioGroupItem value="condition" disabled={readOnly} />
                  {t(($) => $.wizard.flow.taskforce_termination_condition)}
                </label>
              </RadioGroup>
              {endMode === "date" ? (
                <label className="flex flex-col gap-1 text-caption text-muted-foreground">
                  {t(($) => $.wizard.flow.taskforce_dissolve_label)}
                  <OrgDateTimePicker
                    value={toRFC3339(form.dissolve_at)}
                    onChange={(iso) => { onFormChange("dissolve_at", toLocalInput(iso)); onFormChange("end_condition", ""); }}
                    placeholder={t(($) => $.wizard.flow.taskforce_dissolve_placeholder)}
                  />
                </label>
              ) : (
                <label className="flex flex-col gap-1 text-caption text-muted-foreground">
                  {t(($) => $.wizard.flow.taskforce_end_condition_label)}
                  <OrgSelect
                    className="w-full"
                    disabled={readOnly}
                    value={form.end_condition}
                    onValueChange={(v) => { onFormChange("end_condition", v); onFormChange("dissolve_at", ""); }}
                    items={ORG_END_CONDITION_VALUES.filter((c) => c !== "").map((c) => ({ value: c, label: orgEndConditionLabel(t, c) }))}
                  />
                </label>
              )}
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>

      <Collapsible className="border-b border-border px-4 py-3">
        <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">
          {t(($) => $.inspector.overview.model_title)}
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3">
          <p className="mb-2 text-caption text-muted-foreground">{t(($) => $.inspector.overview.model_hint)}</p>
          <RadioGroup value={form.model} onValueChange={(v) => onFormChange("model", v as OrgModel)}>
            {ORG_MODEL_VALUES.map((m) => (
              <label key={m} className="flex items-start gap-2 py-1 text-body">
                <RadioGroupItem value={m} disabled={readOnly} className="mt-0.5" />
                <span>
                  <span className="block font-medium">{orgModelLabel(t, m)}</span>
                  <span className="block text-caption text-muted-foreground">{orgModelDescription(t, m)}</span>
                </span>
              </label>
            ))}
          </RadioGroup>
        </CollapsibleContent>
      </Collapsible>

      <Collapsible className="border-b border-border px-4 py-3">
        <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">
          {t(($) => $.coherence.collective)}
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3 flex flex-col gap-3">
          <div>
            <h4 className="text-caption font-medium">{t(($) => $.coherence.sections.committees)}</h4>
            <OrgCommitteesEditor definition={definition} onChange={onChange} readOnly={readOnly} />
            <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.inspector.overview.committee_no_effect)}</p>
          </div>
          <Separator />
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.coherence.market.price_cap_usd_ticks)}
            <Input
              type="number"
              min={0}
              step="0.01"
              disabled={readOnly}
              value={definition.market.price_cap_usd_ticks / 1_000_000}
              onChange={(e) => {
                const ticks = Math.round(Math.max(0, Number(e.target.value) || 0) * 1_000_000);
                onChange({ ...definition, market: { ...definition.market, price_cap_usd_ticks: ticks } });
              }}
            />
          </label>
        </CollapsibleContent>
      </Collapsible>

      <Collapsible className="px-4 py-3">
        <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">
          {t(($) => $.visual.advanced)}
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3">
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.definition)}
            <Textarea value={jsonText} onChange={(e) => onJsonChange(e.target.value)} rows={14} spellCheck={false} className="font-mono text-caption" disabled={readOnly} />
          </label>
          {jsonError && <p role="alert" className="mt-2 text-caption text-destructive">{t(($) => $.form.invalid_json, { error: jsonError })}</p>}
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
