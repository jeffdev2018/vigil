"use client";

import { useState } from "react";
import type { TFunction } from "i18next";
import { X, Plus } from "lucide-react";
import type {
  Goal, MemberWithUser, OrgDefinition, OrgEdgeKind, OrgMember, OrgModel, OrgUnit, Squad,
} from "@multica/core/types";
import { moveOrgMember, orgUnitRemovalBlockers, orgWouldCycle, removeOrgMember, removeOrgUnit } from "@multica/core/org";
import {
  ORG_AUTONOMY_ORDER, ORG_DECIDER_CLASSES, ORG_NON_NEGOTIABLE_DENY, ORG_PROPERTIES,
  orgEffectiveModel, orgProblemsForUnit, orgUnitProperties, type OrgProblem,
} from "@multica/core/org/validate";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { Switch } from "@multica/ui/components/ui/switch";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Button } from "@multica/ui/components/ui/button";
import { Separator } from "@multica/ui/components/ui/separator";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { OrgSelect } from "./org-select";
import { useT } from "../../i18n";
import {
  ORG_APPROVAL_RISK_VALUES, ORG_EDGE_KIND_VALUES, ORG_MEMBER_ROLE_VALUES, ORG_MODEL_VALUES, ORG_UNIT_KIND_VALUES,
  orgApprovalRiskLabel, orgAutonomyHint, orgAutonomyLabel, orgCapabilityEnforced, orgCapabilityLabel,
  orgDeciderClassLabel, orgEdgeKindEffectLabel, orgEdgeKindHasEffect, orgEdgeKindLabel, orgMemberRoleLabel,
  orgModelLabel, orgProblemParams, orgUnitKindLabel,
} from "../labels";

const sameMember = (a: OrgMember, b: Pick<OrgMember, "type" | "id">) => a.type === b.type && a.id === b.id;

function CapabilityChips({ verbs, locked, onRemove, t }: { verbs: string[]; locked?: boolean; onRemove?: (v: string) => void; t: TFunction<"org"> }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {verbs.map((v) => {
        const enforced = locked || orgCapabilityEnforced(v);
        return (
          <span
            key={v}
            title={enforced ? undefined : t(($) => $.unit.capability_enforcement.instruction)}
            className="inline-flex items-center gap-1 rounded-md border border-surface-border bg-surface-hover px-2 py-0.5 text-caption"
          >
            {orgCapabilityLabel(t, v)}
            {!enforced && "*"}
            {!locked && onRemove && (
              <button type="button" aria-label={t(($) => $.unit.remove, { item: orgCapabilityLabel(t, v) })} onClick={() => onRemove(v)} className="text-faint-foreground hover:text-foreground">
                <X className="size-3" />
              </button>
            )}
          </span>
        );
      })}
    </div>
  );
}

function AddCapability({ onAdd, t }: { onAdd: (v: string) => void; t: TFunction<"org"> }) {
  const [value, setValue] = useState("");
  const submit = () => { const v = value.trim(); if (v) { onAdd(v); setValue(""); } };
  return (
    <div className="mt-1.5 flex gap-1.5">
      <Input value={value} placeholder={t(($) => $.unit.verb_placeholder)} onChange={(e) => setValue(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); submit(); } }} />
      <Button type="button" size="sm" variant="outline" onClick={submit}>{t(($) => $.unit.add)}</Button>
    </div>
  );
}

/** One team, selected on the chart. Every edit here rebuilds `definition`
 *  and hands it to `onChange` — no local copy of unit state ever exists. */
export function OrgUnitPanel({
  definition,
  unit,
  orgModel,
  onChange,
  onRemoved,
  onAddMembers,
  problems,
  readOnly,
  members,
  squads,
  goals,
}: {
  definition: OrgDefinition;
  unit: OrgUnit;
  orgModel: OrgModel;
  onChange: (next: OrgDefinition) => void;
  onRemoved: () => void;
  onAddMembers: (unitId: string) => void;
  problems: OrgProblem[];
  readOnly: boolean;
  members: MemberWithUser[];
  squads: Squad[];
  goals: Goal[];
}) {
  const { t } = useT("org");
  const { getActorName } = useActorName();
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [newEdge, setNewEdge] = useState<{ kind: OrgEdgeKind; target: string }>({ kind: "reports_to", target: "" });

  const patch = (change: Partial<OrgUnit>) => { if (!readOnly) onChange({ ...definition, units: definition.units.map((u) => (u.id === unit.id ? { ...u, ...change } : u)) }); };
  const unitProblems = orgProblemsForUnit(problems, unit.id);
  const blockers = orgUnitRemovalBlockers(definition, unit.id);
  const effectiveModel = orgEffectiveModel(definition, unit.id, orgModel);
  const exposedToExternal = orgUnitProperties(unit).includes("external_effects");
  const otherUnits = definition.units.filter((u) => u.id !== unit.id);

  const reportsTo = definition.edges.find((e) => e.from === unit.id && e.kind === "reports_to");
  const escalatesTo = definition.edges.filter((e) => e.from === unit.id && e.kind === "escalates_to");
  const backsUp = definition.edges.filter((e) => e.from === unit.id && e.kind === "backs_up");
  const consults = definition.edges.filter((e) => e.from === unit.id && e.kind === "consults");
  const unitName = (id: string) => definition.units.find((u) => u.id === id)?.name ?? id;
  const removeEdge = (edge: (typeof definition.edges)[number]) => onChange({ ...definition, edges: definition.edges.filter((e) => e !== edge) });
  const cyclic = newEdge.kind === "reports_to" && !!newEdge.target && orgWouldCycle(definition, unit.id, newEdge.target);
  const addEdge = () => {
    if (readOnly || !newEdge.target || newEdge.target === unit.id) return;
    if (newEdge.kind === "reports_to" && cyclic) return;
    if (definition.edges.some((e) => e.from === unit.id && e.to === newEdge.target && e.kind === newEdge.kind)) return;
    onChange({
      ...definition,
      edges: [
        ...definition.edges.filter((e) => !(newEdge.kind === "reports_to" && e.from === unit.id && e.kind === "reports_to")),
        { from: unit.id, to: newEdge.target, kind: newEdge.kind },
      ],
    });
    setNewEdge({ kind: newEdge.kind, target: "" });
  };

  const rules = definition.rules.filter((r) => r.target_unit === unit.id);
  const receivesWords = rules.flatMap((r) => r.keywords ?? []);
  const patchRuleList = (id: string, key: "keywords" | "labels" | "paths", value: string[]) =>
    onChange({ ...definition, rules: definition.rules.map((r) => (r.id === id ? { ...r, [key]: value } : r)) });
  const patchRulePriority = (id: string, priority: number) =>
    onChange({ ...definition, rules: definition.rules.map((r) => (r.id === id ? { ...r, priority } : r)) });

  return (
    <div className="flex flex-col">
      <div className="border-b border-border px-4 py-3">
        <p className="text-micro font-semibold uppercase tracking-wide text-faint-foreground">{t(($) => $.inspector.unit.eyebrow)}</p>
        {unitProblems.length > 0 && (
          <ul role="alert" className="mt-2 flex flex-col gap-0.5 text-caption text-warning">
            {unitProblems.map((p, i) => <li key={`${p.code}:${i}`}>{t(($) => $.problem[p.code], orgProblemParams(t, p))}</li>)}
          </ul>
        )}
        <label className="mt-2 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.visual.team_name)}
          <Input value={unit.name} disabled={readOnly} onChange={(e) => patch({ name: e.target.value })} />
        </label>
        <label className="mt-2 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.coherence.mission)}
          <Textarea rows={2} maxLength={240} value={unit.mission ?? ""} disabled={readOnly} onChange={(e) => patch({ mission: e.target.value })} />
          <span className="self-end text-micro text-faint-foreground">{t(($) => $.inspector.unit.mission_count, { count: (unit.mission ?? "").length })}</span>
        </label>
        <label className="mt-2 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.inspector.unit.kind_label)}
          <OrgSelect className="w-full" value={unit.kind ?? "unit"} disabled={readOnly} onValueChange={(v) => patch({ kind: v })} items={ORG_UNIT_KIND_VALUES.map((k) => ({ value: k, label: orgUnitKindLabel(t, k) }))} />
        </label>
        <div className="mt-3">
          {blockers.length > 0 && (
            <ul role="alert" className="mb-2 flex flex-col gap-0.5 text-caption text-destructive">
              {blockers.map((c) => <li key={c.decision_type}>{t(($) => $.coherence.quorum_block, { name: c.decision_type, quorum: c.quorum })}</li>)}
            </ul>
          )}
          <Button type="button" size="sm" variant={confirmRemove ? "destructive" : "outline"} disabled={readOnly || blockers.length > 0} onClick={() => { if (confirmRemove) { onChange(removeOrgUnit(definition, unit.id)); onRemoved(); } else setConfirmRemove(true); }}>
            {confirmRemove ? t(($) => $.inspector.unit.remove_confirm) : t(($) => $.visual.remove_team)}
          </Button>
          {confirmRemove && <Button type="button" size="sm" variant="ghost" className="ml-1.5" onClick={() => setConfirmRemove(false)}>{t(($) => $.actions.cancel)}</Button>}
        </div>
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.autonomy_title)}</h3>
        <RadioGroup className="mt-2" value={unit.autonomy} onValueChange={(v) => patch({ autonomy: v as OrgUnit["autonomy"] })}>
          {ORG_AUTONOMY_ORDER.map((a) => (
            <label key={a} className="flex items-center gap-2 text-body">
              <RadioGroupItem value={a} disabled={readOnly} />
              {orgAutonomyLabel(t, a)}
            </label>
          ))}
        </RadioGroup>
        <p className="mt-1 text-caption text-muted-foreground">{orgAutonomyHint(t, unit.autonomy)}</p>
        {unitProblems.filter((p) => p.code === "autonomy_over_trust").map((p, i) => (
          <p key={i} role="alert" className="mt-1 text-caption text-warning">{t(($) => $.problem.autonomy_over_trust, orgProblemParams(t, p))}</p>
        ))}
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.accountable_title)}</h3>
        <label className="mt-2 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.coherence.owner)}
          <OrgSelect className="w-full" value={unit.owner_id ?? ""} disabled={readOnly} onValueChange={(v) => patch({ owner_id: v || undefined })} items={[{ value: "", label: t(($) => $.form.owner_none) }, ...members.map((m) => ({ value: m.user_id, label: m.name }))]} />
        </label>
        <label className="mt-3 flex items-center gap-2 text-caption text-muted-foreground">
          <Switch checked={unit.human_approval === true} disabled={readOnly} onCheckedChange={(v) => patch({ human_approval: v })} />
          {t(($) => $.coherence.human_approval)}
        </label>
        <label className="mt-3 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.inspector.unit.approval_risk_label)}
          <OrgSelect className="w-full" value={unit.approval_risk ?? ""} disabled={readOnly} onValueChange={(v) => patch({ approval_risk: v || undefined })} items={[{ value: "", label: t(($) => $.inspector.unit.approval_risk_none) }, ...ORG_APPROVAL_RISK_VALUES.map((r) => ({ value: r, label: orgApprovalRiskLabel(t, r) }))]} />
        </label>
        {exposedToExternal && (
          <div className="mt-3">
            <p className="text-caption text-muted-foreground">{t(($) => $.inspector.unit.deciders_hint)}</p>
            {ORG_DECIDER_CLASSES.map((cls) => (
              <label key={cls} className="mt-1.5 flex flex-col gap-1 text-caption text-muted-foreground">
                {orgDeciderClassLabel(t, cls)}
                <OrgSelect className="w-full" value={unit.deciders?.[cls] ?? ""} disabled={readOnly} onValueChange={(v) => patch({ deciders: { ...unit.deciders, [cls]: v } })} items={[{ value: "", label: t(($) => $.form.owner_none) }, ...members.map((m) => ({ value: m.user_id, label: m.name }))]} />
              </label>
            ))}
          </div>
        )}
      </div>

      <div className="border-b border-border px-4 py-3">
        <div className="flex items-center justify-between">
          <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.coherence.members)}</h3>
          {!readOnly && <Button type="button" size="sm" variant="outline" onClick={() => onAddMembers(unit.id)}><Plus className="mr-1 size-3" />{t(($) => $.coherence.add_members)}</Button>}
        </div>
        {unit.members.length === 0 && <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.coherence.empty_team)}</p>}
        <ul className="mt-2 flex flex-col gap-2">
          {unit.members.map((member) => {
            const name = getActorName(member.type, member.id);
            return (
              <li key={`${member.type}:${member.id}`} className="rounded-lg border border-surface-border p-2.5">
                <div className="flex items-center gap-2">
                  <ActorAvatar actorType={member.type} actorId={member.id} size="md" />
                  <span className="min-w-0 flex-1 truncate text-body font-medium">{name}</span>
                  {!readOnly && (
                    <button type="button" aria-label={t(($) => $.coherence.remove_member, { name })} onClick={() => onChange(removeOrgMember(definition, unit.id, member))} className="text-faint-foreground hover:text-foreground">
                      <X className="size-3.5" />
                    </button>
                  )}
                </div>
                <div className="mt-2 grid grid-cols-2 gap-1.5">
                  <OrgSelect
                    aria-label={t(($) => $.inspector.unit.member_role_label)}
                    value={member.role ?? "member"}
                    disabled={readOnly}
                    onValueChange={(v) => patch({ members: unit.members.map((m) => (sameMember(m, member) ? { ...m, role: v } : m)) })}
                    items={ORG_MEMBER_ROLE_VALUES.map((r) => ({ value: r, label: orgMemberRoleLabel(t, r) }))}
                  />
                  <OrgSelect
                    aria-label={t(($) => $.visual.assign_role)}
                    value={member.role_id ?? ""}
                    disabled={readOnly}
                    onValueChange={(v) => patch({ members: unit.members.map((m) => (sameMember(m, member) ? { ...m, role_id: v || undefined } : m)) })}
                    items={[{ value: "", label: t(($) => $.coherence.no_role) }, ...unit.roles.map((r) => ({ value: r.id, label: r.name }))]}
                  />
                </div>
                {!readOnly && otherUnits.length > 0 && (
                  <OrgSelect
                    className="mt-1.5"
                    aria-label={t(($) => $.coherence.move_member, { name })}
                    value=""
                    onValueChange={(v) => onChange(moveOrgMember(definition, unit.id, v, member))}
                    items={[{ value: "", label: t(($) => $.coherence.move_hint) }, ...otherUnits.map((u) => ({ value: u.id, label: u.name }))]}
                  />
                )}
              </li>
            );
          })}
        </ul>
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.visual.roles)}</h3>
        <ul className="mt-2 flex flex-col gap-2">
          {unit.roles.map((role) => {
            const vacant = !unit.members.some((m) => m.role_id === role.id);
            return (
              <li key={role.id} className="rounded-lg border border-surface-border bg-surface-hover/40 p-2.5">
                <div className="flex items-center justify-between gap-2">
                  {vacant && <span className="rounded-full border border-warning-border bg-warning-subtle px-1.5 py-px text-micro text-warning-subtle-foreground">{t(($) => $.inspector.unit.role_vacant)}</span>}
                  {!readOnly && (
                    <button
                      type="button"
                      aria-label={t(($) => $.visual.remove_role)}
                      disabled={effectiveModel === "circles" && unit.roles.length <= 1}
                      onClick={() => patch({ roles: unit.roles.filter((r) => r.id !== role.id), members: unit.members.map((m) => (m.role_id === role.id ? { ...m, role_id: undefined } : m)) })}
                      className="ml-auto text-faint-foreground hover:text-foreground disabled:opacity-40"
                    >
                      <X className="size-3.5" />
                    </button>
                  )}
                </div>
                <Input className="mt-1.5" aria-label={t(($) => $.visual.role_name)} value={role.name} disabled={readOnly} onChange={(e) => patch({ roles: unit.roles.map((r) => (r.id === role.id ? { ...r, name: e.target.value } : r)) })} />
                <Input className="mt-1.5" aria-label={t(($) => $.visual.keywords)} placeholder={t(($) => $.visual.keywords)} value={(role.keywords ?? []).join(", ")} disabled={readOnly} onChange={(e) => patch({ roles: unit.roles.map((r) => (r.id === role.id ? { ...r, keywords: e.target.value.split(",").map((k) => k.trim()) } : r)) })} />
                <Input className="mt-1.5" aria-label={t(($) => $.visual.responsibilities)} placeholder={t(($) => $.visual.responsibilities)} value={role.responsibilities ?? ""} disabled={readOnly} onChange={(e) => patch({ roles: unit.roles.map((r) => (r.id === role.id ? { ...r, responsibilities: e.target.value } : r)) })} />
              </li>
            );
          })}
        </ul>
        {!readOnly && <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => patch({ roles: [...unit.roles, { id: crypto.randomUUID(), name: t(($) => $.visual.new_role) }] })}>{t(($) => $.visual.add_role)}</Button>}
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.receives_title)}</h3>
        {rules.length === 0 ? (
          <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.inspector.unit.receives_none)}</p>
        ) : (
          <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.inspector.unit.receives_intro)} <span className="text-foreground">{receivesWords.length ? receivesWords.join(", ") : t(($) => $.inspector.unit.receives_none)}</span></p>
        )}
        <ul className="mt-2 flex flex-col gap-2">
          {rules.map((rule) => (
            <li key={rule.id} className="rounded-lg border border-surface-border p-2.5">
              {(["keywords", "labels", "paths"] as const).map((key) => (
                <label key={key} className="mb-1.5 block text-caption text-muted-foreground">
                  {t(($) => $.coherence.rule_fields[key])}
                  <Input value={(rule[key] ?? []).join(", ")} disabled={readOnly} onChange={(e) => patchRuleList(rule.id, key, e.target.value.split(",").map((v) => v.trim()))} />
                </label>
              ))}
              <label className="block text-caption text-muted-foreground">
                {t(($) => $.coherence.priority)}
                <Input type="number" value={rule.priority} disabled={readOnly} onChange={(e) => patchRulePriority(rule.id, Number(e.target.value) || 0)} />
              </label>
              {!readOnly && <Button type="button" size="sm" variant="ghost" className="mt-1.5" onClick={() => onChange({ ...definition, rules: definition.rules.filter((r) => r.id !== rule.id) })}>{t(($) => $.coherence.remove_rule)}</Button>}
            </li>
          ))}
        </ul>
        {!readOnly && <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => onChange({ ...definition, rules: [...definition.rules, { id: crypto.randomUUID(), target_unit: unit.id, keywords: [], priority: 1 }] })}>{t(($) => $.coherence.add_rule)}</Button>}
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.can_title)}</h3>
        <CapabilityChips verbs={unit.allow} onRemove={readOnly ? undefined : (v) => patch({ allow: unit.allow.filter((x) => x !== v) })} t={t} />
        {!readOnly && <AddCapability t={t} onAdd={(v) => patch({ allow: unit.allow.includes(v) ? unit.allow : [...unit.allow, v] })} />}
        <h3 className="mt-4 text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.cannot_title)}</h3>
        <div className="mt-1 text-caption text-muted-foreground" title={t(($) => $.inspector.unit.capability_locked)}>
          <CapabilityChips verbs={[...ORG_NON_NEGOTIABLE_DENY]} locked t={t} />
        </div>
        <div className="mt-1.5">
          <CapabilityChips verbs={unit.deny.filter((v) => !(ORG_NON_NEGOTIABLE_DENY as readonly string[]).includes(v))} onRemove={readOnly ? undefined : (v) => patch({ deny: unit.deny.filter((x) => x !== v) })} t={t} />
        </div>
        {!readOnly && <AddCapability t={t} onAdd={(v) => patch({ deny: unit.deny.includes(v) ? unit.deny : [...unit.deny, v] })} />}
        <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.inspector.unit.capability_instruction_note)}</p>
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.upward_title)}</h3>
        <p className="mt-2 text-caption">
          <span className="text-muted-foreground">{t(($) => $.visual.edge.reports_to)}: </span>
          {reportsTo ? <span className="text-foreground">{unitName(reportsTo.to)}</span> : t(($) => $.inspector.unit.reports_to_none)}
          {reportsTo && !readOnly && <button type="button" className="ml-2 text-faint-foreground hover:text-foreground" aria-label={t(($) => $.visual.remove_connection)} onClick={() => removeEdge(reportsTo)}><X className="inline size-3" /></button>}
        </p>
        {([["escalates_to", escalatesTo], ["backs_up", backsUp], ["consults", consults]] as const).map(([kind, edges]) => edges.length > 0 && (
          <div key={kind} className="mt-2">
            <p className="text-caption text-muted-foreground">{orgEdgeKindLabel(t, kind)}{!orgEdgeKindHasEffect(kind) && ` — ${orgEdgeKindEffectLabel(t, kind)}`}</p>
            <ul className="mt-1 flex flex-col gap-1">
              {edges.map((e) => (
                <li key={`${e.kind}:${e.to}`} className="flex items-center gap-2 text-caption">
                  <span className="text-foreground">{unitName(e.to)}</span>
                  {kind === "escalates_to" && (
                    <label className="flex items-center gap-1 text-muted-foreground">
                      <Switch checked={e.human_approval === true} disabled={readOnly} onCheckedChange={(v) => onChange({ ...definition, edges: definition.edges.map((x) => (x === e ? { ...x, human_approval: v } : x)) })} />
                      {t(($) => $.coherence.human_approval)}
                    </label>
                  )}
                  {!readOnly && <button type="button" aria-label={t(($) => $.visual.remove_connection)} onClick={() => removeEdge(e)} className="text-faint-foreground hover:text-foreground"><X className="size-3" /></button>}
                </li>
              ))}
            </ul>
          </div>
        ))}
        <label className="mt-3 flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.coherence.escalation_quota)}
          <Input type="number" min={0} step={1} value={unit.escalation_quota_per_day} disabled={readOnly} onChange={(e) => patch({ escalation_quota_per_day: Math.max(0, Math.floor(Number(e.target.value) || 0)) })} />
        </label>
        {!readOnly && otherUnits.length > 0 && (
          <div className="mt-3 flex flex-col gap-1.5 rounded-md border border-border p-2.5">
            <OrgSelect aria-label={t(($) => $.visual.connection_type)} className="w-full" value={newEdge.kind} onValueChange={(v) => setNewEdge({ kind: v as OrgEdgeKind, target: newEdge.target })} items={ORG_EDGE_KIND_VALUES.map((k) => ({ value: k, label: t(($) => $.visual.edge[k]) }))} />
            <OrgSelect aria-label={t(($) => $.visual.target)} className="w-full" value={newEdge.target} onValueChange={(v) => setNewEdge({ kind: newEdge.kind, target: v })} items={[{ value: "", label: t(($) => $.visual.choose_team) }, ...otherUnits.map((u) => ({ value: u.id, label: u.name }))]} />
            {cyclic && <p role="alert" className="text-caption text-destructive">{t(($) => $.visual.cycle)}</p>}
            <Button type="button" size="sm" variant="outline" disabled={!newEdge.target || cyclic} onClick={addEdge}>{t(($) => $.visual.add_connection)}</Button>
          </div>
        )}
      </div>

      <Collapsible className="px-4 py-3">
        <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">{t(($) => $.inspector.unit.advanced_title)}</CollapsibleTrigger>
        <CollapsibleContent className="mt-3 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.visual.budget)}
            <Input type="number" min={0} step="0.01" disabled={readOnly} value={(unit.budget_usd_ticks ?? 0) / 1_000_000} onChange={(e) => patch({ budget_usd_ticks: Math.round(Math.max(0, Number(e.target.value) || 0) * 1_000_000) })} />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.coherence.team_model)}
            <OrgSelect className="w-full" value={unit.model ?? ""} disabled={readOnly} onValueChange={(v) => patch({ model: v ? (v as OrgModel) : undefined })} items={[{ value: "", label: t(($) => $.coherence.inherit_model) }, ...ORG_MODEL_VALUES.map((m) => ({ value: m, label: orgModelLabel(t, m) }))]} />
          </label>
          <fieldset disabled={readOnly} className="flex flex-col gap-1.5">
            <legend className="text-caption font-medium text-muted-foreground">{t(($) => $.coherence.exposures)}</legend>
            {ORG_PROPERTIES.map((property) => (
              <label key={property} className="flex items-center gap-2 text-caption">
                <Checkbox checked={!unit.excludes.includes(property)} onCheckedChange={(e) => patch({ excludes: e ? unit.excludes.filter((p) => p !== property) : [...unit.excludes, property] })} />
                {t(($) => $.coherence.properties[property])}
              </label>
            ))}
          </fieldset>
          <Separator />
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.coherence.squad)}
            <OrgSelect className="w-full" value={unit.squad_id ?? ""} disabled={readOnly} onValueChange={(v) => patch({ squad_id: v || undefined })} items={[{ value: "", label: t(($) => $.coherence.no_squad) }, ...squads.map((s) => ({ value: s.id, label: s.name }))]} />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.coherence.goal)}
            <OrgSelect className="w-full" value={unit.mission_goal_id ?? ""} disabled={readOnly} onValueChange={(v) => patch({ mission_goal_id: v || undefined })} items={[{ value: "", label: t(($) => $.coherence.no_goal) }, ...goals.map((g) => ({ value: g.id, label: g.title }))]} />
          </label>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
