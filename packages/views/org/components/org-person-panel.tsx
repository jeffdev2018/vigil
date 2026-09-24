"use client";

import type { Agent, OrgDefinition, OrgMember } from "@multica/core/types";
import { moveOrgMember, removeOrgMember } from "@multica/core/org";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { OrgSelect } from "./org-select";
import { useT } from "../../i18n";
import { ORG_MEMBER_ROLE_VALUES, orgAgentTrustLabel, orgAutonomyHint, orgAutonomyLabel, orgMemberRoleLabel } from "../labels";

const sameMember = (a: OrgMember, b: Pick<OrgMember, "type" | "id">) => a.type === b.type && a.id === b.id;

/** One person, in the context of the team they were selected from. */
export function OrgPersonPanel({
  definition,
  unitId,
  member,
  onChange,
  onSelect,
  readOnly,
  agents,
}: {
  definition: OrgDefinition;
  unitId: string;
  member: Pick<OrgMember, "type" | "id">;
  onChange: (next: OrgDefinition) => void;
  onSelect: (unitId: string) => void;
  readOnly: boolean;
  agents: Agent[];
}) {
  const { t } = useT("org");
  const { getActorName } = useActorName();
  const unit = definition.units.find((u) => u.id === unitId);
  const membership = unit?.members.find((m) => sameMember(m, member));
  const name = getActorName(member.type, member.id);
  const agent = member.type === "agent" ? agents.find((a) => a.id === member.id) : undefined;
  const teams = definition.units.filter((u) => u.members.some((m) => sameMember(m, member)));
  const owns = definition.units.filter((u) => u.owner_id === member.id);
  const otherUnits = definition.units.filter((u) => u.id !== unitId);

  if (!unit || !membership) return null;

  return (
    <div className="flex flex-col">
      <div className="border-b border-border px-4 py-3">
        <p className="text-micro font-semibold uppercase tracking-wide text-faint-foreground">
          {member.type === "agent" ? t(($) => $.plan.agent) : t(($) => $.plan.human)}
        </p>
        <div className="mt-1 flex items-center gap-2.5">
          <ActorAvatar actorType={member.type} actorId={member.id} size="lg" showStatusDot={member.type === "agent"} profileLink />
          <h2 className="text-title-sm font-semibold">{name}</h2>
        </div>
        {agent?.description && <p className="mt-1 text-caption text-muted-foreground">{agent.description}</p>}
      </div>

      <div className="border-b border-border px-4 py-3">
        <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.person.teams_title)}</h3>
        {teams.length === 0 ? (
          <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.inspector.person.teams_none)}</p>
        ) : (
          <ul className="mt-2 flex flex-wrap gap-1.5">
            {teams.map((u) => {
              const m = u.members.find((x) => sameMember(x, member));
              const role = m ? orgMemberRoleLabel(t, m.role ?? "member") : "";
              return (
                <li key={u.id}>
                  <button type="button" onClick={() => onSelect(u.id)} className="rounded-md border border-surface-border bg-surface-hover px-2 py-0.5 text-caption hover:bg-surface-hover/70">
                    {u.name} · {role}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {member.type === "agent" ? (
        <div className="border-b border-border px-4 py-3">
          <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.person.agent_autonomy_title, { unit: unit.name })}</h3>
          <p className="mt-1.5 text-body">{orgAutonomyLabel(t, unit.autonomy)}</p>
          <p className="text-caption text-muted-foreground">{orgAutonomyHint(t, unit.autonomy)}</p>
          <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.inspector.person.agent_autonomy_hint)}</p>
          {agent?.trust_mode && (
            <p className="mt-2 text-caption text-muted-foreground">
              {t(($) => $.inspector.person.agent_trust_label)}: <span className="text-foreground">{orgAgentTrustLabel(t, agent.trust_mode)}</span>
            </p>
          )}
        </div>
      ) : (
        <div className="border-b border-border px-4 py-3">
          <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.person.owns_title)}</h3>
          {owns.length === 0 ? (
            <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.inspector.person.owns_none)}</p>
          ) : (
            <>
              <ul className="mt-2 flex flex-wrap gap-1.5">
                {owns.map((u) => (
                  <li key={u.id}>
                    <button type="button" onClick={() => onSelect(u.id)} className="rounded-md border border-surface-border bg-surface-hover px-2 py-0.5 text-caption hover:bg-surface-hover/70">{u.name}</button>
                  </li>
                ))}
              </ul>
              <p className="mt-1.5 text-caption text-muted-foreground">{t(($) => $.inspector.person.owns_hint)}</p>
            </>
          )}
        </div>
      )}

      {!readOnly && (
        <div className="px-4 py-3">
          <h3 className="text-caption font-semibold text-muted-foreground">{t(($) => $.inspector.unit.member_role_label)}</h3>
          <OrgSelect
            className="mt-2 w-full"
            aria-label={t(($) => $.inspector.unit.member_role_label)}
            value={membership.role ?? "member"}
            onValueChange={(v) => onChange({ ...definition, units: definition.units.map((u) => (u.id === unit.id ? { ...u, members: u.members.map((m) => (sameMember(m, member) ? { ...m, role: v } : m)) } : u)) })}
            items={ORG_MEMBER_ROLE_VALUES.map((r) => ({ value: r, label: orgMemberRoleLabel(t, r) }))}
          />
          {otherUnits.length > 0 && (
            <OrgSelect
              className="mt-2 w-full"
              aria-label={t(($) => $.coherence.move_member, { name })}
              value=""
              onValueChange={(v) => onChange(moveOrgMember(definition, unit.id, v, membership))}
              items={[{ value: "", label: t(($) => $.coherence.move_hint) }, ...otherUnits.map((u) => ({ value: u.id, label: u.name }))]}
            />
          )}
          <button type="button" className="mt-2 text-caption text-destructive hover:underline" onClick={() => onChange(removeOrgMember(definition, unit.id, membership))}>
            {t(($) => $.coherence.remove_member, { name })}
          </button>
        </div>
      )}
    </div>
  );
}
