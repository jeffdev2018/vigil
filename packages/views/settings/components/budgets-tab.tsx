"use client";

import { useMemo, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Gauge, Pencil, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  budgetPolicyOptions,
  budgetStatusOptions,
  useCreateBudgetOverride,
  useCreateBudgetPolicy,
  useDeleteBudgetPolicy,
  useUpdateBudgetPolicy,
  type BudgetPolicy,
} from "@multica/core/budgets";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Progress } from "@multica/ui/components/ui/progress";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

const TICKS_PER_USD = 10_000_000_000;
type Scope = BudgetPolicy["scope_type"];

const dollars = (ticks: number) => new Intl.NumberFormat(undefined, {
  style: "currency", currency: "USD", maximumFractionDigits: 2,
}).format(ticks / TICKS_PER_USD);

export function BudgetsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";
  const policies = useQuery(budgetPolicyOptions(wsId));
  const statuses = useQuery(budgetStatusOptions(wsId));
  const projects = useQuery(projectListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const remove = useDeleteBudgetPolicy();
  const override = useCreateBudgetOverride();
  const [editor, setEditor] = useState<BudgetPolicy | "new" | null>(null);
  const statusByPolicy = useMemo(() => new Map((statuses.data ?? []).map((status) => [status.policy.id, status])), [statuses.data]);
  const targetNames = useMemo(() => new Map([
    ...(projects.data ?? []).map((project) => [project.id, project.title] as const),
    ...(agents.data ?? []).map((agent) => [agent.id, agent.name] as const),
  ]), [projects.data, agents.data]);

  const deletePolicy = (policy: BudgetPolicy) => {
    if (!window.confirm(t(($) => $.budgets.delete_confirm))) return;
    remove.mutate(policy.id, { onError: () => toast.error(t(($) => $.budgets.save_failed)) });
  };
  const grantOverride = (policy: BudgetPolicy) => {
    const reason = window.prompt(t(($) => $.budgets.override_reason));
    if (!reason?.trim()) return;
    override.mutate({ id: policy.id, reason: reason.trim() }, {
      onSuccess: () => toast.success(t(($) => $.budgets.override_created)),
      onError: () => toast.error(t(($) => $.budgets.save_failed)),
    });
  };

  return (
    <SettingsTab title={t(($) => $.budgets.title)} description={t(($) => $.budgets.description)}>
      <div className="space-y-4">
        <div className="flex justify-end">
          {canManage ? <Button size="sm" onClick={() => setEditor("new")}><Plus className="mr-1 h-4 w-4" />{t(($) => $.budgets.add)}</Button> : null}
        </div>
        {statuses.isError ? (
          <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive">
            {t(($) => $.budgets.load_failed)} <button className="underline" onClick={() => statuses.refetch()}>{t(($) => $.budgets.retry)}</button>
          </div>
        ) : null}
        {policies.isLoading ? (
          <div className="rounded-lg border p-10 text-center text-muted-foreground">{t(($) => $.budgets.loading)}</div>
        ) : (policies.data?.length ?? 0) === 0 ? (
          <div className="rounded-lg border border-dashed p-10 text-center">
            <Gauge className="mx-auto mb-3 h-8 w-8 text-muted-foreground" />
            <p className="font-medium">{t(($) => $.budgets.empty)}</p>
            <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.budgets.empty_hint)}</p>
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border bg-card">
            {policies.data?.map((policy) => {
              const status = statusByPolicy.get(policy.id);
              const used = (status?.spent_usd_ticks ?? 0) + (status?.reserved_usd_ticks ?? 0);
              const percent = Math.min(100, Math.round((used / policy.limit_usd_ticks) * 100));
              return <div key={policy.id} className="border-b p-4 last:border-b-0">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium capitalize">{t(($) => $.budgets.scopes[policy.scope_type])}</span>
                      {policy.scope_id ? <span className="max-w-52 truncate text-caption text-muted-foreground" title={targetNames.get(policy.scope_id) ?? policy.scope_id}>{targetNames.get(policy.scope_id) ?? policy.scope_id}</span> : null}
                      <Badge variant={policy.action === "enforce" ? "default" : "secondary"}>{t(($) => $.budgets.actions[policy.action])}</Badge>
                      {status?.override_expires_at ? <Badge variant="outline"><ShieldCheck className="mr-1 h-3 w-3" />{t(($) => $.budgets.override_active)}</Badge> : null}
                    </div>
                    <div className="mt-3 flex items-center justify-between text-caption">
                      <span>{dollars(used)} / {dollars(policy.limit_usd_ticks)}</span><span>{`${percent}% · ${t(($) => $.budgets.periods[policy.period])}`}</span>
                    </div>
                    <Progress value={percent} className="mt-2 h-2" />
                    <p className="mt-2 text-caption text-muted-foreground">{t(($) => $.budgets.reserved, { amount: dollars(status?.reserved_usd_ticks ?? 0) })}</p>
                  </div>
                  {canManage ? <div className="flex shrink-0 gap-1">
                    <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.budgets.edit)} onClick={() => setEditor(policy)}><Pencil className="h-4 w-4" /></Button>
                    <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.budgets.override)} onClick={() => grantOverride(policy)}><ShieldCheck className="h-4 w-4" /></Button>
                    <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.budgets.delete)} onClick={() => deletePolicy(policy)}><Trash2 className="h-4 w-4" /></Button>
                  </div> : null}
                </div>
              </div>;
            })}
          </div>
        )}
      </div>
      <BudgetEditor key={editor === "new" ? "new" : editor?.id ?? "closed"} open={editor !== null} policy={editor === "new" ? null : editor} onClose={() => setEditor(null)} projects={projects.data ?? []} agents={agents.data ?? []} />
    </SettingsTab>
  );
}

function BudgetEditor({ open, policy, onClose, projects, agents }: {
  open: boolean; policy: BudgetPolicy | null; onClose: () => void;
  projects: Array<{ id: string; title: string }>; agents: Array<{ id: string; name: string }>;
}) {
  const { t } = useT("settings");
  const create = useCreateBudgetPolicy();
  const update = useUpdateBudgetPolicy();
  const [scope, setScope] = useState<Scope>(policy?.scope_type ?? "workspace");
  const [scopeId, setScopeId] = useState(policy?.scope_id ?? "");
  const [limit, setLimit] = useState(policy ? String(policy.limit_usd_ticks / TICKS_PER_USD) : "100");
  const [period, setPeriod] = useState<BudgetPolicy["period"]>(policy?.period ?? "monthly");
  const [action, setAction] = useState<BudgetPolicy["action"]>(policy?.action ?? "enforce");
  const [warn, setWarn] = useState(String((policy?.warn_bps ?? 8000) / 100));
  const targets = scope === "project" ? projects : scope === "agent" ? agents.map((a) => ({ id: a.id, title: a.name })) : [];
  const pending = create.isPending || update.isPending;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const amount = Number(limit);
    const warnPercent = Number(warn);
    if (!Number.isFinite(amount) || amount <= 0 || !Number.isFinite(warnPercent) || warnPercent < 0 || warnPercent > 100 || (scope !== "workspace" && !scopeId)) return;
    const common = { limit_usd_ticks: Math.round(amount * TICKS_PER_USD), period, warn_bps: Math.round(warnPercent * 100), action };
    const mutation = policy ? update : create;
    const data = policy ? { id: policy.id, ...common, revision: policy.revision } : { scope_type: scope, scope_id: scope === "workspace" ? null : scopeId, ...common };
    mutation.mutate(data as never, { onSuccess: () => onClose(), onError: () => toast.error(t(($) => $.budgets.save_failed)) });
  };

  return <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
    <DialogContent>
      <DialogHeader><DialogTitle>{policy ? t(($) => $.budgets.edit_title) : t(($) => $.budgets.create_title)}</DialogTitle><DialogDescription>{t(($) => $.budgets.editor_hint)}</DialogDescription></DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        {!policy ? <><label className="block text-sm font-medium">{t(($) => $.budgets.scope)}</label><Select items={(["workspace", "project", "agent"] as Scope[]).map((value) => ({ value, label: t(($) => $.budgets.scopes[value]) }))} value={scope} onValueChange={(value) => { if (value) setScope(value); setScopeId(""); }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{(["workspace", "project", "agent"] as Scope[]).map((value) => <SelectItem key={value} value={value}>{t(($) => $.budgets.scopes[value])}</SelectItem>)}</SelectContent></Select>
          {scope !== "workspace" ? <Select items={targets.map((target) => ({ value: target.id, label: target.title }))} value={scopeId} onValueChange={(value) => setScopeId(value ?? "")}><SelectTrigger><SelectValue placeholder={t(($) => $.budgets.target)} /></SelectTrigger><SelectContent>{targets.map((target) => <SelectItem key={target.id} value={target.id}>{target.title}</SelectItem>)}</SelectContent></Select> : null}</> : null}
        <label className="block text-sm font-medium">{t(($) => $.budgets.limit)}</label><Input type="number" min="0.01" step="0.01" value={limit} onChange={(e) => setLimit(e.target.value)} required />
        <label className="block text-sm font-medium">{t(($) => $.budgets.period)}</label><Select items={(["daily", "weekly", "monthly"] as const).map((value) => ({ value, label: t(($) => $.budgets.periods[value]) }))} value={period} onValueChange={(value) => value && setPeriod(value)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{(["daily", "weekly", "monthly"] as const).map((value) => <SelectItem key={value} value={value}>{t(($) => $.budgets.periods[value])}</SelectItem>)}</SelectContent></Select>
        <label className="block text-sm font-medium">{t(($) => $.budgets.warning)}</label><Input type="number" min="0" max="100" step="1" value={warn} onChange={(e) => setWarn(e.target.value)} required />
        <label className="block text-sm font-medium">{t(($) => $.budgets.action)}</label><Select items={[{ value: "enforce", label: t(($) => $.budgets.actions.enforce) }, { value: "observe", label: t(($) => $.budgets.actions.observe) }]} value={action} onValueChange={(value) => value && setAction(value)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="enforce">{t(($) => $.budgets.actions.enforce)}</SelectItem><SelectItem value="observe">{t(($) => $.budgets.actions.observe)}</SelectItem></SelectContent></Select>
        <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{t(($) => $.budgets.cancel)}</Button><Button type="submit" disabled={pending}>{pending ? t(($) => $.budgets.saving) : t(($) => $.budgets.save)}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>;
}
