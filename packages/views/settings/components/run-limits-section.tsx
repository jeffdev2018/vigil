"use client";

import { useEffect, useState } from "react";
import { AlarmClock, Gauge, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  FOLLOWUP_BUDGET_MAX,
  FOLLOWUP_BUDGET_MIN,
  clampFollowupBudget,
  followupBudgetFromSettings,
  mergeFollowupBudget,
} from "@multica/core/followups";
import { followupKeys } from "@multica/core/followups";
import type { Workspace } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { formatGateValue, runLimitPoliciesOptions, useDeleteRunLimitPolicy, useSaveRunLimitPolicy, type RunLimitGate, type RunLimitPolicy, type RunLimitPolicyInput } from "@multica/core/budgets/run-limits";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";
import { SettingsSaveState, type SettingsSaveStatus } from "./settings-layout";

const TICKS_PER_USD = 1e10;

/**
 * Run limits (K03): caps on one run — cost, duration, turns, tool calls —
 * per workspace, project or agent. The most restrictive cap per gate wins;
 * enforce stops the run at 100%, observe only records; both warn at the
 * threshold. Period budgets stay above this section.
 */
export function RunLimitsSection({ canManage }: { canManage: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: policies = [] } = useQuery(runLimitPoliciesOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const save = useSaveRunLimitPolicy(wsId);
  const remove = useDeleteRunLimitPolicy(wsId);
  const [editing, setEditing] = useState<RunLimitPolicy | "new" | null>(null);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.run_limits.save_failed));
  const scopeName = (p: RunLimitPolicy) => {
    if (p.scope_type === "workspace") return t(($) => $.budgets.scopes.workspace);
    const target = p.scope_type === "project" ? projects.find((x) => x.id === p.scope_id)?.title : agents.find((x) => x.id === p.scope_id)?.name;
    return `${t(($) => $.budgets.scopes[p.scope_type])} · ${target ?? p.scope_id?.slice(0, 8) ?? ""}`;
  };
  const caps = (p: RunLimitPolicy): [RunLimitGate, number][] => {
    const out: [RunLimitGate, number][] = [];
    if (p.max_cost_usd_ticks) out.push(["cost", p.max_cost_usd_ticks]);
    if (p.max_duration_seconds) out.push(["duration", p.max_duration_seconds]);
    if (p.max_turns) out.push(["turns", p.max_turns]);
    if (p.max_tool_calls) out.push(["tool_calls", p.max_tool_calls]);
    return out;
  };

  return (
    <section data-testid="run-limits-section" className="space-y-3">
      <div className="flex items-center gap-2">
        <Gauge className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-title font-medium">{t(($) => $.run_limits.title)}</h3>
        {canManage && editing === null && (
          <Button type="button" size="sm" variant="outline" className="ml-auto" onClick={() => setEditing("new")}>{t(($) => $.run_limits.add)}</Button>
        )}
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.run_limits.intro)}</p>
      {policies.length === 0 && editing === null && <p className="text-caption italic text-muted-foreground">{t(($) => $.run_limits.empty)}</p>}
      <div className="flex flex-col gap-2">
        {policies.map((p) =>
          editing !== null && editing !== "new" && editing.id === p.id ? (
            <RunLimitEditor key={p.id} policy={p} projects={projects} agents={agents} pending={save.isPending} onCancel={() => setEditing(null)} onSave={(input) => save.mutate({ id: p.id, input }, { onError: fail, onSuccess: () => setEditing(null) })} />
          ) : (
            <div key={p.id} data-testid="run-limit-row" className="flex flex-wrap items-center gap-2 rounded-md border border-border px-3 py-2 text-caption">
              <span className="font-medium">{scopeName(p)}</span>
              <span className={p.action === "enforce" ? "rounded bg-destructive/15 px-1 text-destructive" : "rounded bg-muted px-1 text-muted-foreground"}>{t(($) => $.budgets.actions[p.action])}</span>
              {caps(p).map(([gate, v]) => (
                <span key={gate} className="rounded border border-border px-1 font-mono">{t(($) => $.run_limits.gates[gate])} ≤ {formatGateValue(gate, v)}</span>
              ))}
              <span className="text-muted-foreground">{t(($) => $.run_limits.warn_at, { percent: Math.round(p.warn_bps / 100) })}</span>
              {canManage && (
                <span className="ml-auto flex items-center gap-1">
                  <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(p)}>{t(($) => $.run_limits.edit)}</Button>
                  <Button type="button" size="icon-sm" variant="ghost" className="text-muted-foreground hover:text-destructive" aria-label={t(($) => $.run_limits.delete, { scope: scopeName(p) })} onClick={() => remove.mutate(p.id, { onError: fail })}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </span>
              )}
            </div>
          ),
        )}
        {editing === "new" && (
          <RunLimitEditor policy={null} projects={projects} agents={agents} pending={save.isPending} onCancel={() => setEditing(null)} onSave={(input) => save.mutate({ input }, { onError: fail, onSuccess: () => setEditing(null) })} />
        )}
      </div>
      <FollowupBudgetGroup canManage={canManage} />
    </section>
  );
}

/**
 * Follow-up budget: how many wake-ups ("réveil programmé") an agent and a
 * workspace may file per day. A cap on what runs, so it sits with the run
 * limits — but it lives in `workspace.settings.followups`, not in a policy
 * row, so it saves through the workspace endpoint the way
 * `settings.doctrine.require_review` does: read the current blob, merge this
 * one key, PATCH the whole thing back (the server replaces it wholesale).
 */
function FollowupBudgetGroup({ canManage }: { canManage: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const workspace = useCurrentWorkspace();
  const stored = followupBudgetFromSettings(workspace?.settings);
  const [perAgent, setPerAgent] = useState(String(stored.max_per_agent_per_day));
  const [perWorkspace, setPerWorkspace] = useState(String(stored.max_per_workspace_per_day));
  const [status, setStatus] = useState<SettingsSaveStatus>("idle");

  // The workspace list arrives after the first paint, so the fields follow the
  // stored values until the person edits them.
  useEffect(() => {
    setPerAgent(String(stored.max_per_agent_per_day));
    setPerWorkspace(String(stored.max_per_workspace_per_day));
  }, [stored.max_per_agent_per_day, stored.max_per_workspace_per_day]);

  const agentValue = clampFollowupBudget(perAgent, stored.max_per_agent_per_day);
  const workspaceValue = clampFollowupBudget(perWorkspace, stored.max_per_workspace_per_day);
  const outOfRange = (raw: string) => {
    const n = Number(raw.trim());
    return raw.trim() !== "" && (!Number.isFinite(n) || n < FOLLOWUP_BUDGET_MIN || n > FOLLOWUP_BUDGET_MAX);
  };
  const refused = outOfRange(perAgent) || outOfRange(perWorkspace);
  const dirty =
    agentValue !== stored.max_per_agent_per_day || workspaceValue !== stored.max_per_workspace_per_day;

  const save = async () => {
    if (!workspace) return;
    setStatus("saving");
    try {
      const merged = mergeFollowupBudget(workspace.settings, {
        max_per_agent_per_day: agentValue,
        max_per_workspace_per_day: workspaceValue,
      });
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      // The issue block quotes the budget from its own list response.
      await qc.invalidateQueries({ queryKey: followupKeys.all(wsId) });
      setStatus("saved");
    } catch (error) {
      setStatus("error");
      toast.error(error instanceof Error && error.message ? error.message : t(($) => $.followup_budget.save_failed));
    }
  };

  const field = (
    label: string,
    value: string,
    onChange: (v: string) => void,
  ) => (
    <label className="flex flex-col gap-1">
      {label}
      <Input
        type="number"
        min={FOLLOWUP_BUDGET_MIN}
        max={FOLLOWUP_BUDGET_MAX}
        step={1}
        aria-label={label}
        disabled={!canManage}
        value={value}
        onChange={(e) => { onChange(e.target.value); setStatus("idle"); }}
      />
    </label>
  );

  return (
    <div data-testid="followup-budget" className="space-y-2 border-t border-border pt-3">
      <div className="flex items-center gap-2">
        <AlarmClock className="h-4 w-4 text-muted-foreground" />
        <h4 className="font-medium">{t(($) => $.followup_budget.title)}</h4>
        <SettingsSaveState
          status={status}
          savingLabel={t(($) => $.followup_budget.saving)}
          savedLabel={t(($) => $.followup_budget.saved)}
          errorLabel={t(($) => $.followup_budget.save_failed)}
        />
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.followup_budget.intro)}</p>
      <div className="grid grid-cols-1 gap-2 text-caption md:grid-cols-2">
        {field(t(($) => $.followup_budget.per_agent), perAgent, setPerAgent)}
        {field(t(($) => $.followup_budget.per_workspace), perWorkspace, setPerWorkspace)}
      </div>
      {refused && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.followup_budget.out_of_range, { min: FOLLOWUP_BUDGET_MIN, max: FOLLOWUP_BUDGET_MAX })}
        </p>
      )}
      {canManage && (
        <Button type="button" size="sm" variant="outline" disabled={!dirty || refused || status === "saving"} onClick={() => void save()}>
          {t(($) => $.followup_budget.save)}
        </Button>
      )}
    </div>
  );
}

function RunLimitEditor({ policy, projects, agents, pending, onSave, onCancel }: {
  policy: RunLimitPolicy | null; projects: { id: string; title: string }[]; agents: { id: string; name: string }[]; pending: boolean;
  onSave: (input: RunLimitPolicyInput) => void; onCancel: () => void;
}) {
  const { t } = useT("settings");
  const [scope, setScope] = useState<RunLimitPolicy["scope_type"]>(policy?.scope_type ?? "workspace");
  const [scopeId, setScopeId] = useState(policy?.scope_id ?? "");
  const [cost, setCost] = useState(policy?.max_cost_usd_ticks ? String(policy.max_cost_usd_ticks / TICKS_PER_USD) : "");
  const [minutes, setMinutes] = useState(policy?.max_duration_seconds ? String(Math.round(policy.max_duration_seconds / 60)) : "");
  const [turns, setTurns] = useState(policy?.max_turns ? String(policy.max_turns) : "");
  const [tools, setTools] = useState(policy?.max_tool_calls ? String(policy.max_tool_calls) : "");
  const [warn, setWarn] = useState(String((policy?.warn_bps ?? 8000) / 100));
  const [action, setAction] = useState<RunLimitPolicy["action"]>(policy?.action ?? "enforce");
  const targets = scope === "project" ? projects.map((p) => ({ id: p.id, label: p.title })) : scope === "agent" ? agents.map((a) => ({ id: a.id, label: a.name })) : [];
  const num = (s: string) => (s.trim() === "" ? null : Math.max(0, Number(s) || 0) || null);
  const input: RunLimitPolicyInput = {
    scope_type: scope, scope_id: scope === "workspace" ? null : scopeId || null,
    max_cost_usd_ticks: num(cost) ? Math.round((num(cost) as number) * TICKS_PER_USD) : null,
    max_duration_seconds: num(minutes) ? Math.round((num(minutes) as number) * 60) : null,
    max_turns: num(turns) ? Math.round(num(turns) as number) : null,
    max_tool_calls: num(tools) ? Math.round(num(tools) as number) : null,
    warn_bps: Math.round(Math.min(100, Math.max(0, Number(warn) || 0)) * 100), action,
  };
  const valid = (scope === "workspace" || !!scopeId) && (input.max_cost_usd_ticks || input.max_duration_seconds || input.max_turns || input.max_tool_calls);
  return (
    <form data-testid="run-limit-editor" className="flex flex-col gap-2 rounded-md border border-border p-3 text-caption" onSubmit={(e) => { e.preventDefault(); if (valid) onSave(input); }}>
      {!policy && (
        <div className="flex flex-wrap gap-2">
          <select aria-label={t(($) => $.budgets.scope)} className="rounded-md border border-input bg-transparent px-2 py-1" value={scope} onChange={(e) => { setScope(e.target.value as RunLimitPolicy["scope_type"]); setScopeId(""); }}>
            {(["workspace", "project", "agent"] as const).map((s) => <option key={s} value={s}>{t(($) => $.budgets.scopes[s])}</option>)}
          </select>
          {scope !== "workspace" && (
            <select aria-label={t(($) => $.budgets.target)} className="rounded-md border border-input bg-transparent px-2 py-1" value={scopeId} onChange={(e) => setScopeId(e.target.value)}>
              <option value="">{t(($) => $.budgets.target)}</option>
              {targets.map((x) => <option key={x.id} value={x.id}>{x.label}</option>)}
            </select>
          )}
        </div>
      )}
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <label className="flex flex-col gap-1">{t(($) => $.run_limits.cost_usd)}<Input type="number" min={0} step={0.5} aria-label={t(($) => $.run_limits.gates.cost)} value={cost} onChange={(e) => setCost(e.target.value)} /></label>
        <label className="flex flex-col gap-1">{t(($) => $.run_limits.duration_min)}<Input type="number" min={0} step={1} aria-label={t(($) => $.run_limits.gates.duration)} value={minutes} onChange={(e) => setMinutes(e.target.value)} /></label>
        <label className="flex flex-col gap-1">{t(($) => $.run_limits.gates.turns)}<Input type="number" min={0} step={1} aria-label={t(($) => $.run_limits.gates.turns)} value={turns} onChange={(e) => setTurns(e.target.value)} /></label>
        <label className="flex flex-col gap-1">{t(($) => $.run_limits.gates.tool_calls)}<Input type="number" min={0} step={1} aria-label={t(($) => $.run_limits.gates.tool_calls)} value={tools} onChange={(e) => setTools(e.target.value)} /></label>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-1">{t(($) => $.budgets.warning)}<Input type="number" min={0} max={100} className="w-20" aria-label={t(($) => $.budgets.warning)} value={warn} onChange={(e) => setWarn(e.target.value)} /></label>
        <select aria-label={t(($) => $.budgets.action)} className="rounded-md border border-input bg-transparent px-2 py-1" value={action} onChange={(e) => setAction(e.target.value as RunLimitPolicy["action"])}>
          <option value="enforce">{t(($) => $.budgets.actions.enforce)}</option>
          <option value="observe">{t(($) => $.budgets.actions.observe)}</option>
        </select>
        <Button type="submit" size="sm" disabled={pending || !valid}>{t(($) => $.run_limits.save)}</Button>
        <Button type="button" size="sm" variant="ghost" onClick={onCancel}>{t(($) => $.budgets.cancel)}</Button>
      </div>
    </form>
  );
}
