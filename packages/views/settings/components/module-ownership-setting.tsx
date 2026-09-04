"use client";

import { useState } from "react";
import { Route, X } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { moduleOwnershipOptions, useCreateModuleOwnership, useDeleteModuleOwnership } from "@multica/core/issues/ownership";
import { labelListOptions } from "@multica/core/labels/queries";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Module ownership (K33): the workspace's rules, one line each, and a form
 * to add one. A rule names a path pattern (`packages/core/billing/**`) or a
 * label, the owner member and an optional referent agent.
 */
export function ModuleOwnershipSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = workspace.id;
  const { data: rules = [] } = useQuery(moduleOwnershipOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: labels = [] } = useQuery(labelListOptions(wsId));
  const create = useCreateModuleOwnership(wsId);
  const remove = useDeleteModuleOwnership(wsId);
  const [pattern, setPattern] = useState("");
  const [labelId, setLabelId] = useState("");
  const [owner, setOwner] = useState("");
  const [agent, setAgent] = useState("");

  const memberName = (userId: string) => {
    const m = members.find((x) => x.user_id === userId);
    return m?.name || m?.email || userId.slice(0, 8);
  };
  const agentName = (id: string | null) => (id ? (agents.find((a) => a.id === id)?.name ?? id.slice(0, 8)) : null);
  const labelName = (id: string | null) => (id ? (labels.find((l) => l.id === id)?.name ?? id.slice(0, 8)) : null);

  const submit = () => {
    if (!owner || (pattern.trim() === "" && labelId === "")) return;
    create.mutate(
      { path_pattern: pattern.trim() || undefined, label_id: labelId || undefined, owner_user_id: owner, referent_agent_id: agent || undefined },
      {
        onSuccess: () => {
          setPattern("");
          setLabelId("");
          toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.ownership_failed)),
      },
    );
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Route className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.ownership_section)}
        </span>
      }
    >
      <SettingsCard>
        <div className="flex flex-col gap-2 p-3 text-caption">
          <p className="text-muted-foreground">{t(($) => $.workspace.ownership_description)}</p>
          {rules.length === 0 ? (
            <p data-testid="ownership-empty" className="text-muted-foreground">{t(($) => $.workspace.ownership_empty)}</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {rules.map((r) => (
                <li key={r.id} data-testid="ownership-rule" className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate font-mono">
                    {r.path_pattern ?? `${t(($) => $.workspace.ownership_label_prefix)} ${labelName(r.label_id)}`}
                  </span>
                  <span className="shrink-0 text-muted-foreground">
                    {memberName(r.owner_user_id)}
                    {agentName(r.referent_agent_id) && ` + ${agentName(r.referent_agent_id)}`}
                  </span>
                  {canEdit && (
                    <button type="button" aria-label={t(($) => $.workspace.ownership_remove)} className="text-faint-foreground hover:text-foreground" onClick={() => remove.mutate(r.id)}>
                      <X className="size-3.5" />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {canEdit && (
            <div className="flex flex-wrap items-center gap-2">
              <Input
                aria-label={t(($) => $.workspace.ownership_pattern)}
                placeholder={t(($) => $.workspace.ownership_pattern)}
                className="h-8 w-56 font-mono"
                value={pattern}
                onChange={(e) => setPattern(e.target.value)}
              />
              <select aria-label={t(($) => $.workspace.ownership_label)} className="h-8 rounded-md border bg-background px-2" value={labelId} onChange={(e) => setLabelId(e.target.value)}>
                <option value="">{t(($) => $.workspace.ownership_no_label)}</option>
                {labels.map((l) => (
                  <option key={l.id} value={l.id}>{l.name}</option>
                ))}
              </select>
              <select aria-label={t(($) => $.workspace.ownership_owner)} className="h-8 rounded-md border bg-background px-2" value={owner} onChange={(e) => setOwner(e.target.value)}>
                <option value="">{t(($) => $.workspace.ownership_owner)}</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>{m.name || m.email}</option>
                ))}
              </select>
              <select aria-label={t(($) => $.workspace.ownership_agent)} className="h-8 rounded-md border bg-background px-2" value={agent} onChange={(e) => setAgent(e.target.value)}>
                <option value="">{t(($) => $.workspace.ownership_no_agent)}</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
              <Button type="button" size="sm" disabled={create.isPending || !owner || (pattern.trim() === "" && labelId === "")} onClick={submit}>
                {t(($) => $.workspace.ownership_add)}
              </Button>
            </div>
          )}
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
