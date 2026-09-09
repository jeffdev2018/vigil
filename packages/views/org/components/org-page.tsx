"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Network, Plus, ArrowUpRight, Users, GitBranch, Activity, History, Settings2, Layers, Check, Save } from "lucide-react";
import { toast } from "sonner";
import {
  parseEditableOrgDefinition,
  orgLayout,
  orgDefinitionChanges,
  orgDetailOptions,
  orgHealthOptions,
  orgListOptions,
  orgPreflightOptions,
  useDeleteOrgStructure,
  useSetOrgStructureStatus,
  useUpdateOrgStructure,
} from "@multica/core/org";
import { contestCostUsd } from "@multica/core/issues/contest";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { OrgDefinition, OrgStatus, OrgStructure, OrgRevision } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageHeader, CollectionPageHeaderAction, CollectionPageState } from "../../layout/collection-page";
import { OrgEditor } from "./org-editor";
import { useOrgDraftStore, saveOrgDraft, clearOrgDraft } from "@multica/core/org/draft-store";
import { goalListOptions } from "@multica/core/goals";
import { OrgWizard } from "./org-wizard";
import { OrgTester } from "./org-tester";
import { validateOrgDefinition } from "@multica/core/org/validate";
import { OrgProblemList } from "./org-problem-list";
import { OrgTeamCatalog } from "./org-team-catalog";
import { ExportImportSetting } from "../../settings/components/export-import-setting";
import { useT } from "../../i18n";

const STATUS_BADGE: Record<OrgStatus, string> = {
  draft: "bg-muted text-muted-foreground",
  active: "bg-success/10 text-success",
  paused: "bg-warning/10 text-warning",
  dissolved: "bg-destructive/10 text-destructive",
};

const SELECT_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";
const END_CONDITIONS = ["", "all_issues_done", "budget_spent"] as const;

const errorMessage = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback);

/** RFC3339 → `datetime-local` value in the viewer's zone; empty when unset or unparsable. */
function toLocalInput(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function toRFC3339(local: string): string | null {
  if (!local) return null;
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

// ---------------------------------------------------------------------------
// Template picker — shared by the page and the project section.
// ---------------------------------------------------------------------------

export { OrgTemplateCards } from "./org-template-cards";

// ---------------------------------------------------------------------------
// Status dialogs.
// ---------------------------------------------------------------------------

function ActivateDialog({ structure, onClose }: { structure: OrgStructure; onClose: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: pre, isError, refetch } = useQuery(orgPreflightOptions(wsId, structure.id, true));
  const setStatus = useSetOrgStructureStatus(wsId);
  const [attestation, setAttestation] = useState("");
  const submit = () =>
    setStatus.mutate(
      { id: structure.id, action: "activate", eval_attestation: attestation.trim() },
      { onSuccess: onClose, onError: (e) => toast.error(errorMessage(e, t(($) => $.actions.error))) },
    );
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !setStatus.isPending) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.activate.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.activate.description)}</DialogDescription>
        </DialogHeader>
        {isError ? <Button onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button> : !pre ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.activate.loading)}</p>
        ) : (
          <dl data-testid="org-preflight" className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.activate.pattern)}</dt>
            <dd>{pre.pattern}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.runs)}</dt>
            <dd>{pre.coordination_runs_per_issue}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.cost)}</dt>
            <dd>{`$${contestCostUsd(pre.coordination_cost_usd_ticks_per_issue)}`}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.review_seconds)}</dt>
            <dd>{t(($) => $.activate.seconds, { count: pre.human_review_seconds_per_issue })}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.units)}</dt>
            <dd>{pre.units}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.units_without_owner)}</dt>
            <dd className={cn(pre.units_without_owner > 0 && "text-warning")}>{pre.units_without_owner}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.agents)}</dt>
            <dd>{pre.agents}</dd>
            {pre.activation_requirements.length > 0 && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.activate.requirements)}</dt>
                <dd>
                  <ul className="list-disc pl-4 text-warning">
                    {pre.activation_requirements.map((r) => <li key={r}>{r}</li>)}
                  </ul>
                </dd>
              </>
            )}
          </dl>
        )}
        <label className="flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.activate.attestation)}
          <Textarea value={attestation} onChange={(e) => setAttestation(e.target.value)} rows={3} placeholder={t(($) => $.activate.attestation_placeholder, { date: new Date().toLocaleDateString() })} />
        </label>
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" onClick={onClose}>{t(($) => $.actions.cancel)}</Button>
          <Button type="button" size="sm" disabled={setStatus.isPending || !pre || isError || !attestation.trim()} onClick={submit}>{t(($) => $.activate.submit)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type ReasonAction = "pause" | "dissolve" | "delete";

function ReasonDialog({ structure, action, onClose, onDone }: { structure: OrgStructure; action: ReasonAction; onClose: () => void; onDone?: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const setStatus = useSetOrgStructureStatus(wsId);
  const del = useDeleteOrgStructure(wsId);
  const [reason, setReason] = useState("");
  const pending = setStatus.isPending || del.isPending;
  const opts = { onSuccess: () => { onClose(); onDone?.(); }, onError: (e: unknown) => toast.error(errorMessage(e, t(($) => $.actions.error))) };
  const submit = () => {
    if (action === "delete") del.mutate(structure.id, opts);
    else setStatus.mutate({ id: structure.id, action, reason: reason.trim() }, opts);
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pending) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $[action].title)}</DialogTitle>
          <DialogDescription>{t(($) => $[action].description)}</DialogDescription>
        </DialogHeader>
        {action !== "delete" && (
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.actions.reason)}
            <Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder={t(($) => $.actions.reason_placeholder)} />
          </label>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={pending} onClick={onClose}>{t(($) => $.actions.cancel)}</Button>
          <Button type="button" variant={action === "pause" ? "default" : "destructive"} size="sm" disabled={pending} onClick={submit}>{t(($) => $[action].submit)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Detail — chart, editor, actions, health, revisions.
// ---------------------------------------------------------------------------

function OrgHealthSection({ structureId }: { structureId: string }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: health, isError, refetch } = useQuery(orgHealthOptions(wsId, structureId));
  if (isError) return <div role="alert"><p className="text-caption">{t($ => $.form.error)}</p><Button onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button></div>;
  if (!health) return <p className="text-caption text-muted-foreground">{t(($) => $.health.loading)}</p>;
  const counters: [string, string | number][] = [
    [t(($) => $.health.routed), health.routed],
    [t(($) => $.health.unrouted), health.unrouted],
    [t(($) => $.health.escalations), health.escalations],
    [t(($) => $.health.stacked), health.stacked_escalations],
    [t(($) => $.health.reassigned_outside), health.reassigned_outside],
    [t(($) => $.health.market_short), health.market_short],
    [t(($) => $.health.breakers), health.breakers],
    [t(($) => $.health.human_review), health.human_review_items],
    [t(($) => $.health.drift), `${Math.round(health.drift_rate * 100)}%`],
  ];
  return (
    <div data-testid="org-health" className="flex flex-col gap-3">
      <p className="text-caption text-muted-foreground">{t(($) => $.health.window, { count: health.window_days })}</p>
      <dl className="grid grid-cols-3 gap-2 text-caption sm:grid-cols-5">
        {counters.map(([label, value]) => (
          <div key={label} className="rounded-md border p-2">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="text-title tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>
      {health.units.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-caption">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.unit)}</th>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.routed)}</th>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.escalations)}</th>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.vacant_roles)}</th>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.saturated)}</th>
                <th className="py-1 pr-2 font-normal">{t(($) => $.health.paused)}</th>
                <th className="py-1 font-normal">{t(($) => $.health.spend)}</th>
              </tr>
            </thead>
            <tbody>
              {health.units.map((u) => (
                <tr key={u.unit_id} data-testid="org-health-unit" className="border-t border-border/60">
                  <td className="py-1 pr-2 font-medium">{u.name}</td>
                  <td className="py-1 pr-2 tabular-nums">{u.routed}</td>
                  <td className="py-1 pr-2 tabular-nums">{u.escalations}</td>
                  <td className="py-1 pr-2">{u.vacant_roles.length ? u.vacant_roles.join(", ") : t(($) => $.health.none)}</td>
                  <td className="py-1 pr-2">{u.saturated_agents.length ? u.saturated_agents.join(", ") : t(($) => $.health.none)}</td>
                  <td className="py-1 pr-2">{u.paused ? t(($) => $.status.paused) : t(($) => $.health.none)}</td>
                  <td className="py-1 tabular-nums">{`$${contestCostUsd(u.spend_usd_ticks)} / $${contestCostUsd(u.budget_usd_ticks)}`}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div>
        <h4 className="text-caption font-medium">{t(($) => $.health.proposals)}</h4>
        {health.proposals.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.health.no_proposals)}</p>
        ) : (
          <ul className="mt-1 flex flex-col gap-1.5">
            {health.proposals.map((p) => (
              <li key={p.key} data-testid="org-proposal" className="rounded-md border p-2 text-caption">
                <div className="font-medium">{p.title}</div>
                <div className="text-muted-foreground">{p.body}</div>
                {p.measure && <div className="text-muted-foreground">{t(($) => $.health.measure, { measure: p.measure })}</div>}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function OrgDetail({ id, onBack, onDeleted }: { id: string; onBack: () => void; onDeleted: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data, isPending, isError, refetch } = useQuery(orgDetailOptions(wsId, id));
  if (isError || (!isPending && !data)) return <div className="space-y-3 p-6"><p role="alert" className="text-body text-destructive">{t($ => $.form.error)}</p><Button variant="outline" onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button><Button variant="ghost" onClick={onBack}>{t($ => $.page.back)}</Button></div>;
  if (isPending || !data) return <p className="px-4 py-2 text-caption text-muted-foreground">{t(($) => $.page.loading)}</p>;
  return <OrgDetailBody key={data.structure.id} structure={data.structure} revisions={data.revisions} onBack={onBack} onDeleted={onDeleted} />;
}

function OrgDetailBody({ structure, revisions, onBack, onDeleted }: { structure: OrgStructure; revisions: OrgRevision[]; onBack: () => void; onDeleted: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const update = useUpdateOrgStructure(wsId);
  const setStatus = useSetOrgStructureStatus(wsId);
  const { data: goals = [] } = useQuery(goalListOptions(wsId));
  const original = useMemo(() => ({
    name: structure.name,
    owner_id: structure.owner_id ?? "",
    dissolve_at: toLocalInput(structure.dissolve_at),
    end_condition: structure.end_condition ?? "",
    budget: String(structure.budget_usd_ticks ?? 0),
    definition: JSON.stringify(structure.definition, null, 2),
  }), [structure]);
  const [form, setForm] = useState(() => useOrgDraftStore.getState().draft.edits[structure.id]?.form ?? original);
  const [baseRevision, setBaseRevision] = useState(() => useOrgDraftStore.getState().draft.edits[structure.id]?.revision ?? structure.revision);
  useEffect(() => {
    if (!useOrgDraftStore.getState().draft.edits[structure.id]) { setForm(original); setBaseRevision(structure.revision); }
  }, [structure, original]);
  const [leaving, setLeaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [focusedUnit, setFocusedUnit] = useState<string | null>(null);
  const [publishing, setPublishing] = useState(false);
  const [tab, setTab] = useState<"compose" | "activity" | "history" | "settings">("compose");
  const [reviewRevision, setReviewRevision] = useState<OrgRevision | null>(null);
  const [dialog, setDialog] = useState<"activate" | ReasonAction | null>(null);
  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    const next = { ...form, [key]: value };
    setForm(next);
    saveOrgDraft(structure.id, { revision: baseRevision, form: next });
  };
  const discard = () => { clearOrgDraft(structure.id); setForm(original); setBaseRevision(structure.revision); };

  const parsed = useMemo(() => parseEditableOrgDefinition(form.definition), [form.definition]);
  const readOnly = structure.status === "dissolved";
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const problems = "def" in parsed ? validateOrgDefinition(parsed.def, { model: structure.model, agentTrust: Object.fromEntries(agents.map(a => [a.id, a.trust_mode ?? ""])), agentName: Object.fromEntries(agents.map(a => [a.id, a.name])) }) : [];
  const canDelete = useMemo(() => {
    const me = members.find((m) => m.user_id === currentUser?.id);
    return structure.owner_id === currentUser?.id || me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser?.id, structure.owner_id]);

  const save = () => {
    if ("error" in parsed || problems.length) return;
    update.mutate(
      {
        id: structure.id,
        data: {
          expected_revision: baseRevision,
          definition: parsed.def,
          name: form.name.trim(),
          owner_id: form.owner_id,
          dissolve_at: toRFC3339(form.dissolve_at) ?? "",
          end_condition: form.end_condition,
          budget_usd_ticks: Number(form.budget) || 0,
        },
      },
      {
        onSuccess: saved => { clearOrgDraft(structure.id); if (saved) { setBaseRevision(saved.revision); setForm({ name: saved.name, owner_id: saved.owner_id ?? "", dissolve_at: toLocalInput(saved.dissolve_at), end_condition: saved.end_condition, budget: String(saved.budget_usd_ticks), definition: JSON.stringify(saved.definition, null, 2) }); } setPublishing(false); toast.success(t(($) => $.form.saved)); },
        onError: (e) => toast.error(errorMessage(e, t(($) => $.form.error))),
      },
    );
  };

  const resume = () => setStatus.mutate({ id: structure.id, action: "resume" }, { onError: (e) => toast.error(errorMessage(e, t(($) => $.actions.error))) });
  const dirty = form.definition !== JSON.stringify(structure.definition, null, 2) || form.name !== structure.name || form.owner_id !== (structure.owner_id ?? "") || form.budget !== String(structure.budget_usd_ticks ?? 0) || form.dissolve_at !== toLocalInput(structure.dissolve_at) || form.end_condition !== (structure.end_condition ?? "");
  useEffect(() => {
    if (!dirty) return;
    const preventLoss = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", preventLoss);
    return () => window.removeEventListener("beforeunload", preventLoss);
  }, [dirty]);
  const restore = () => {
    if (!reviewRevision) return;
    update.mutate({ id: structure.id, data: { restore_revision_id: reviewRevision.id, expected_revision: structure.revision } }, { onSuccess: () => { setReviewRevision(null); toast.success(t($ => $.history.restored)); }, onError: e => toast.error(errorMessage(e, t($ => $.form.error))) });
  };

  return (
    <div className="min-w-0 flex-1 overflow-y-auto px-4 py-5 md:px-8">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="ghost" size="sm" className="gap-1 px-2" onClick={() => dirty ? setLeaving(true) : onBack()}>
          <ArrowLeft className="size-3.5" />
          {t(($) => $.page.back)}
        </Button>
        <h2 className="text-title-lg font-semibold tracking-tight">{structure.name}</h2>
        <Badge variant="outline">{t(($) => $.model[structure.model])}</Badge>
        <Badge className={STATUS_BADGE[structure.status]}>{t(($) => $.status[structure.status])}</Badge>
        <span className="text-caption text-muted-foreground">{t(($) => $.page.revision, { n: structure.revision })}</span>
        <div className="ml-auto flex items-center gap-1">
          {structure.status === "draft" && <Button type="button" size="sm" disabled={dirty} onClick={() => setDialog("activate")}>{t(($) => $.actions.activate)}</Button>}
          {structure.status === "active" && <Button type="button" size="sm" variant="outline" disabled={dirty} onClick={() => setDialog("pause")}>{t(($) => $.actions.pause)}</Button>}
          {structure.status === "paused" && <Button type="button" size="sm" disabled={dirty || setStatus.isPending} onClick={resume}>{t(($) => $.actions.resume)}</Button>}
          {(structure.status === "active" || structure.status === "paused") && (
            <Button type="button" size="sm" variant="outline" className="text-destructive" disabled={dirty} onClick={() => setDialog("dissolve")}>{t(($) => $.actions.dissolve)}</Button>
          )}
          {(structure.status === "draft" || structure.status === "dissolved") && canDelete && (
            <Button type="button" size="sm" variant="outline" className="text-destructive" disabled={dirty} onClick={() => setDialog("delete")}>{t(($) => $.actions.delete)}</Button>
          )}
        </div>
      </div>
      {structure.paused_reason && <p className="mt-1 text-caption text-warning">{structure.paused_reason}</p>}
      {readOnly && <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.page.read_only)}</p>}

      <div className="mt-6 flex flex-wrap items-center justify-between gap-3 border-b pb-3">
        <div className="flex flex-wrap gap-1" aria-label={t($ => $.page.title)}>{(["compose", "activity", "history", "settings"] as const).map(key => { const Icon = { compose: GitBranch, activity: Activity, history: History, settings: Settings2 }[key]; return <button type="button" key={key} aria-pressed={tab === key} onClick={() => setTab(key)} className={cn("flex items-center gap-2 rounded-lg px-3 py-2 text-body transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", tab === key ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted")}><Icon className="size-4" />{t($ => $.workspace.tabs[key])}</button>; })}</div>
        {!readOnly && <div className="flex items-center gap-3"><span role="status" className="hidden items-center gap-1.5 text-caption text-muted-foreground sm:flex">{dirty ? <span className="size-1.5 rounded-full bg-warning" /> : <Check className="size-3.5 text-success" />}{dirty ? t($ => $.coherence.draft_saved) : t($ => $.workspace.saved)}</span><Button size="sm" variant="ghost" disabled={!dirty || update.isPending} onClick={discard}>{t($ => $.coherence.discard)}</Button><Button size="sm" disabled={!dirty || update.isPending || "error" in parsed || !form.name.trim() || problems.length > 0 || baseRevision !== structure.revision} onClick={() => structure.status === "active" ? setPublishing(true) : save()}><Save className="mr-1.5 size-3.5" />{structure.status === "active" ? t($ => $.coherence.publish) : t($ => $.form.save)}</Button></div>}
      </div>
      {baseRevision !== structure.revision && dirty && <p role="alert" className="mt-4 text-caption text-warning">{t($ => $.coherence.conflict)}</p>}
      <div hidden={tab !== "compose"} className="mt-5 space-y-4">
        <div className="flex justify-end"><Button variant="outline" onClick={() => setTesting(v => !v)}>{t($ => $.coherence.test)}</Button></div>
        {testing && "def" in parsed && <OrgTester structureId={structure.id} definition={parsed.def} model={structure.model} status={structure.status} revision={structure.revision} dirty={dirty} goals={goals} onSelectUnit={setFocusedUnit} />}
        <OrgProblemList problems={problems} />
        {"def" in parsed && <OrgEditor focusedUnit={focusedUnit} definition={parsed.def} model={structure.model} pausedUnits={structure.paused_units} readOnly={readOnly || update.isPending} onChange={def => set("definition", JSON.stringify(def, null, 2))} />}
      </div>
      <section hidden={tab !== "settings"} className="mx-auto mt-6 max-w-3xl rounded-2xl border bg-card p-6">
        <div className="flex flex-col gap-3">
          <h3 className="text-caption font-medium">{t(($) => $.page.editor)}</h3>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.name)}
              <Input value={form.name} onChange={(e) => set("name", e.target.value)} disabled={readOnly || update.isPending} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.owner)}
              <select className={SELECT_CLASS} value={form.owner_id} onChange={(e) => set("owner_id", e.target.value)} disabled={readOnly || update.isPending}>
                <option value="">{t(($) => $.form.owner_none)}</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>{m.name}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.dissolve_at)}
              <input type="datetime-local" className={SELECT_CLASS} value={form.dissolve_at} onChange={(e) => set("dissolve_at", e.target.value)} disabled={readOnly || update.isPending} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.end_condition)}
              <select className={SELECT_CLASS} value={form.end_condition} onChange={(e) => set("end_condition", e.target.value)} disabled={readOnly || update.isPending}>
                {END_CONDITIONS.map((c) => (
                  <option key={c} value={c}>{c === "" ? t(($) => $.form.end_none) : c === "all_issues_done" ? t(($) => $.form.end_all_issues_done) : t(($) => $.form.end_budget_spent)}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.budget)}
              <Input type="number" min={0} step="0.01" value={Number(form.budget) / 1_000_000} onChange={(e) => set("budget", String(Math.round(Number(e.target.value) * 1_000_000)))} disabled={readOnly || update.isPending} />
            </label>
          </div>
          {"def" in parsed && <details className="space-y-3 rounded-lg border p-4"><summary className="cursor-pointer text-body font-medium">{t($ => $.coherence.collective)}</summary>
            {parsed.def.committees.map((committee, index) => <div key={index} className="space-y-2 rounded-lg bg-muted/30 p-3"><label className="block text-caption">{t($ => $.coherence.decision_type)}<Input disabled={readOnly || update.isPending} value={committee.decision_type} onChange={e => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.map((c, i) => i === index ? { ...c, decision_type: e.target.value } : c) }, null, 2))} /></label>
              <fieldset disabled={readOnly || update.isPending} className="flex flex-wrap gap-3"><legend className="text-caption">{t($ => $.coherence.sections.units)}</legend>{parsed.def.units.map(unit => <label key={unit.id} className="flex items-center gap-1 text-caption"><input type="checkbox" checked={committee.unit_ids.includes(unit.id)} onChange={e => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.map((c, i) => i === index ? { ...c, unit_ids: e.target.checked ? [...c.unit_ids, unit.id] : c.unit_ids.filter(id => id !== unit.id) } : c) }, null, 2))} />{unit.name}</label>)}</fieldset>
              {(["quorum", "max_rounds"] as const).map(key => <label key={key} className="block text-caption">{t($ => $.coherence[key])}<Input disabled={readOnly || update.isPending} type="number" min={1} step={1} value={committee[key]} onChange={e => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.map((c, i) => i === index ? { ...c, [key]: Number(e.target.value) } : c) }, null, 2))} /></label>)}
              <Button disabled={readOnly || update.isPending} variant="ghost" size="sm" onClick={() => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.filter((_, i) => i !== index) }, null, 2))}>{t($ => $.coherence.remove_committee)}</Button>
            </div>)}<Button disabled={readOnly || update.isPending} size="sm" variant="outline" onClick={() => set("definition", JSON.stringify({ ...parsed.def, committees: [...parsed.def.committees, { decision_type: "", unit_ids: [], quorum: 1, max_rounds: 3 }] }, null, 2))}>{t($ => $.coherence.add_committee)}</Button>
            <h4 className="text-body font-medium">{t($ => $.coherence.sections.market)}</h4>{(["price_cap_usd_ticks", "offers_per_agent_per_day", "min_offers"] as const).map(key => <label key={key} className="block text-caption">{t($ => $.coherence.market[key])}<Input disabled={readOnly || update.isPending} type="number" min={0} step={key === "price_cap_usd_ticks" ? .01 : 1} value={parsed.def.market[key] / (key === "price_cap_usd_ticks" ? 1_000_000 : 1)} onChange={e => set("definition", JSON.stringify({ ...parsed.def, market: { ...parsed.def.market, [key]: Math.round(Number(e.target.value) * (key === "price_cap_usd_ticks" ? 1_000_000 : 1)) } }, null, 2))} /></label>)}
          </details>}
          <details className="rounded-md border p-3"><summary className="cursor-pointer text-caption font-medium">{t($ => $.visual.advanced)}</summary>
          <label className="mt-3 flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.definition)}
            <Textarea value={form.definition} onChange={(e) => set("definition", e.target.value)} rows={16} spellCheck={false} className="font-mono text-caption" disabled={readOnly || update.isPending} />
          </label></details>
          {"error" in parsed && <p role="alert" className="text-caption text-destructive">{t(($) => $.form.invalid_json, { error: parsed.error })}</p>}
        </div>
      </section>

      <section hidden={tab !== "activity"} className="mt-6 rounded-2xl border bg-card p-6">
        <h3 className="mb-4 text-title-sm font-semibold">{t(($) => $.health.title)}</h3>
        <OrgHealthSection structureId={structure.id} />
      </section>

      <section hidden={tab !== "history"} className="mx-auto mt-6 max-w-4xl rounded-2xl border bg-card p-6">
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.page.revisions)}</h3>
        {revisions.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.no_revisions)}</p>
        ) : (
          <ul className="mt-4 flex flex-col divide-y text-body">
            {revisions.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center gap-3 py-4"><span className="flex size-9 items-center justify-center rounded-full bg-info/10 text-info"><History className="size-4" /></span>
                <span>{t(($) => $.page.revision_row, { n: r.revision, status: t($ => $.status[r.status as OrgStatus]), model: t($ => $.model[r.model as OrgStructure["model"]]) })}</span>
                <span className="text-muted-foreground">{new Date(r.created_at).toLocaleString()}</span>
                {r.definition && <Button size="sm" variant="ghost" onClick={() => setReviewRevision(r)}>{t($ => $.history.compare)}</Button>}
              </li>
            ))}
          </ul>
        )}
      </section>

      {publishing && <Dialog open onOpenChange={open => { if (!update.isPending) setPublishing(open); }}><DialogContent><DialogHeader><DialogTitle>{t($ => $.coherence.publish)}</DialogTitle><DialogDescription>{t($ => $.coherence.publish_hint)}</DialogDescription></DialogHeader><ul className="space-y-2 text-body">{"def" in parsed && orgDefinitionChanges(structure.definition, parsed.def).map(c => <li key={c.id}>{c.after?.name ?? c.before?.name ?? t($ => $.coherence.sections[c.section])}</li>)}</ul><DialogFooter><Button variant="outline" disabled={update.isPending} onClick={() => setPublishing(false)}>{t($ => $.actions.cancel)}</Button><Button disabled={update.isPending} onClick={save}>{t($ => $.coherence.publish)}</Button></DialogFooter></DialogContent></Dialog>}
      {leaving && <Dialog open onOpenChange={setLeaving}><DialogContent><DialogHeader><DialogTitle>{t($ => $.workspace.leave_title)}</DialogTitle><DialogDescription>{t($ => $.visual.unsaved)}</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setLeaving(false)}>{t($ => $.workspace.keep_editing)}</Button><Button variant="destructive" onClick={() => { clearOrgDraft(structure.id); onBack(); }}>{t($ => $.workspace.discard)}</Button></DialogFooter></DialogContent></Dialog>}
      {reviewRevision && <Dialog open onOpenChange={open => { if (!open) setReviewRevision(null); }}><DialogContent className="sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.history.title, { n: reviewRevision.revision })}</DialogTitle><DialogDescription>{t($ => $.history.description)}</DialogDescription></DialogHeader><div className="max-h-[60vh] space-y-4 overflow-auto">{reviewRevision.definition && <><div className="space-y-2">{orgDefinitionChanges(structure.definition, reviewRevision.definition).map(change => <div key={change.id} className="rounded-lg border p-3"><p className="text-body font-medium">{change.after?.name ?? change.before?.name ?? t($ => $.coherence.sections[change.section])}</p><div className="mt-2 grid gap-2 text-caption sm:grid-cols-2">{([change.before, change.after]).map((unit, i) => <div key={i} className="rounded-md bg-muted/50 p-3"><span className="text-muted-foreground">{i === 0 ? t($ => $.history.current) : t($ => $.history.previous)}</span><p className="mt-1">{unit ? `${unit.name} · ${t($ => $.autonomy[unit.autonomy])} · ${t($ => $.page.members, { count: unit.members.length })}` : t($ => $.history.absent)}</p><p>{unit?.roles.map(r => r.name).join(", ")}</p></div>)}</div></div>)}</div><p className="text-caption text-muted-foreground">{t($ => $.history.full_diff)}</p></>}<details><summary className="cursor-pointer text-caption font-medium">{t($ => $.visual.advanced)}</summary><div className="mt-3 grid gap-3 sm:grid-cols-2"><div><h4 className="mb-2 text-caption font-semibold">{t($ => $.history.current)}</h4><pre className="overflow-auto rounded-md bg-muted p-3 text-caption">{JSON.stringify(structure.definition, null, 2)}</pre></div><div><h4 className="mb-2 text-caption font-semibold">{t($ => $.history.previous)}</h4><pre className="overflow-auto rounded-md bg-muted p-3 text-caption">{JSON.stringify(reviewRevision.definition, null, 2)}</pre></div></div></details></div>{dirty && <p className="text-caption text-warning">{t($ => $.history.dirty)}</p>}<DialogFooter><Button variant="outline" onClick={() => setReviewRevision(null)}>{t($ => $.actions.cancel)}</Button><Button disabled={readOnly || dirty || update.isPending || reviewRevision.revision === structure.revision} onClick={restore}>{t($ => $.history.restore)}</Button></DialogFooter></DialogContent></Dialog>}
      {dialog === "activate" && <ActivateDialog structure={structure} onClose={() => setDialog(null)} />}
      {dialog && dialog !== "activate" && (
        <ReasonDialog structure={structure} action={dialog} onClose={() => setDialog(null)} onDone={dialog === "delete" ? onDeleted : undefined} />
      )}
    </div>
  );
}

function OrgMiniMap({ definition }: { definition: OrgDefinition }) {
  const layout = orgLayout(definition);
  const nodes = new Map(layout.nodes.map(n => [n.unit.id, n]));
  return <svg viewBox={`0 0 ${layout.width} ${layout.height}`} className="h-36 w-full text-info" aria-hidden="true">{definition.edges.map((edge, i) => { const a = nodes.get(edge.from), b = nodes.get(edge.to); return a && b ? <path key={i} d={`M${a.x + 140},${a.y + 60} L${b.x + 140},${b.y + 60}`} stroke="currentColor" opacity=".3" strokeWidth="5" strokeDasharray={edge.kind === "reports_to" ? undefined : "10 10"} /> : null; })}{layout.nodes.map(({ unit, x, y }, i) => <g key={unit.id}><rect x={x} y={y} width="280" height="120" rx="20" fill="var(--card)" stroke="currentColor" strokeOpacity=".25" strokeWidth="3" /><circle cx={x + 38} cy={y + 38} r="14" fill="currentColor" opacity={i === 0 ? 1 : .45} /><rect x={x + 65} y={y + 28} width="150" height="14" rx="7" fill="var(--foreground)" opacity=".5" /><rect x={x + 25} y={y + 72} width="100" height="10" rx="5" fill="var(--muted-foreground)" opacity=".3" /></g>)}</svg>;
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function OrgPage() {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: structures = [], isLoading, isError, refetch } = useQuery(orgListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const selectedId = useOrgDraftStore(s => s.draft.selectedId);
  const setSelectedId = (id: string | null) => useOrgDraftStore.getState().setDraft({ selectedId: id });
  const [creating, setCreating] = useState(false);
  const [transferring, setTransferring] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const currentUser = useAuthStore(s => s.user);
  const canTransfer = members.some(m => m.user_id === currentUser?.id && (m.role === "owner" || m.role === "admin"));

  const projectTitle = useMemo(() => new Map(projects.map((p) => [p.id, p.title])), [projects]);
  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m.name])), [members]);
  const sorted = useMemo(
    () => [...structures].sort((a, b) => Number(a.project_id !== null) - Number(b.project_id !== null) || a.created_at.localeCompare(b.created_at)),
    [structures],
  );

  if (selectedId) {
    return (
      <div className="relative flex min-w-0 flex-1 min-h-0 flex-col">
        <CollectionPageHeader icon={Network} title={t(($) => $.page.title)} />
        <OrgDetail id={selectedId} onBack={() => setSelectedId(null)} onDeleted={() => setSelectedId(null)} />
      </div>
    );
  }

  return (
    <div className="relative flex min-w-0 flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Network}
        title={t(($) => $.page.title)}
        count={structures.length}
        actions={<><Button size="sm" variant="outline" onClick={() => setCatalogOpen(true)}>{t($ => $.wizard.catalog)}</Button><Button size="sm" variant="outline" onClick={() => setTransferring(true)}>{t($ => $.wizard.transfer)}</Button><CollectionPageHeaderAction icon={Plus} label={t(($) => $.page.new_structure)} onClick={() => setCreating(true)} /></>}
      />
      {isLoading || isError ? (
        <CollectionPageState icon={Network} title={isError ? t($ => $.form.error) : t($ => $.page.loading)} actions={isError ? <Button onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button> : undefined} />
      ) : (
        <div className="flex-1 overflow-y-auto px-5 py-8 md:px-10">
          <section className="mb-9 flex flex-wrap items-end justify-between gap-6"><div className="max-w-xl"><p className="mb-3 flex items-center gap-2 text-caption font-medium text-info"><Layers className="size-4" />{t($ => $.workspace.eyebrow)}</p><h1 className="text-display font-semibold tracking-tight">{t($ => $.workspace.title)}</h1><p className="mt-3 max-w-lg text-body-lg leading-relaxed text-muted-foreground">{t($ => $.workspace.description)}</p></div><div className="flex gap-7 rounded-2xl border bg-muted/20 px-6 py-5">{[[structures.length, t($ => $.workspace.structures)], [structures.reduce((n, s) => n + s.definition.units.length, 0), t($ => $.workspace.teams)], [structures.filter(s => s.status === "active").length, t($ => $.workspace.active)]].map(([count, label]) => <div key={label}><p className="text-display-sm font-semibold tabular-nums">{count}</p><p className="mt-1 text-caption text-muted-foreground">{label}</p></div>)}</div></section>
          <div className="mb-4 flex items-center gap-2"><h2 className="text-title-sm font-semibold">{t($ => $.workspace.your_structures)}</h2><span className="rounded-full bg-muted px-2 py-0.5 text-caption text-muted-foreground">{structures.length}</span></div>
          <div className="grid gap-5 md:grid-cols-2 2xl:grid-cols-3">
            {sorted.map((s) => (
              <button
                key={s.id}
                type="button"
                data-testid="org-structure"
                onClick={() => setSelectedId(s.id)}
                className="group flex min-w-0 flex-col overflow-hidden rounded-2xl border bg-card text-left shadow-sm transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-1 hover:border-info/40 hover:shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transform-none"
              >
                <span className="relative block w-full border-b bg-info/5 px-8 py-4"><OrgMiniMap definition={s.definition} /><span className="absolute right-4 top-4 rounded-full bg-background p-2 text-muted-foreground transition-colors group-hover:bg-info group-hover:text-background"><ArrowUpRight className="size-4" /></span></span>
                <span className="flex w-full flex-col gap-3 p-5"><span className="text-caption text-muted-foreground">
                  {s.project_id === null ? t(($) => $.page.workspace_default) : projectTitle.get(s.project_id) ?? t(($) => $.page.unknown_project)}
                </span>
                <span className="text-title font-semibold tracking-tight">{s.name}</span>
                <span className="flex flex-wrap items-center gap-1.5">
                  <Badge variant="outline">{t(($) => $.model[s.model])}</Badge>
                  <Badge className={STATUS_BADGE[s.status]}>{t(($) => $.status[s.status])}</Badge>
                  <span className="text-caption text-muted-foreground">{t(($) => $.page.revision, { n: s.revision })}</span>
                </span>
                <span className="mt-2 flex items-center gap-2 border-t pt-4 text-caption text-muted-foreground"><ActorAvatar name={s.owner_id ? memberName.get(s.owner_id) ?? "" : ""} initials={(s.owner_id ? memberName.get(s.owner_id) ?? "" : "").slice(0, 2)} size="md" />
                  {s.owner_id ? memberName.get(s.owner_id) ?? s.owner_id : t(($) => $.page.no_owner)}
                  {s.paused_units.length > 0 && ` · ${t(($) => $.page.paused_units, { count: s.paused_units.length })}`}
                <span className="ml-auto flex items-center gap-1"><Users className="size-3.5" />{t($ => $.visual.team_count, { count: s.definition.units.length })}</span></span></span>
              </button>
            ))}
            <button type="button" onClick={() => setCreating(true)} className="group flex min-h-64 flex-col items-center justify-center gap-3 rounded-2xl border border-dashed bg-muted/10 p-8 text-center transition-colors hover:border-info/50 hover:bg-info/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><span className="mb-1 rounded-2xl border bg-background p-4 shadow-sm transition-transform group-hover:scale-110 motion-reduce:transform-none"><Plus className="size-6 text-info" /></span><span className="text-title-sm font-semibold">{t($ => $.workspace.add_title)}</span><span className="max-w-60 text-caption leading-relaxed text-muted-foreground">{t($ => $.workspace.add_description)}</span><span className="mt-2 flex items-center gap-1 text-caption font-medium text-info">{t($ => $.workspace.explore)}<ArrowUpRight className="size-3.5" /></span></button>
          </div>
        </div>
      )}
      {transferring && <Dialog open onOpenChange={setTransferring}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.wizard.transfer)}</DialogTitle><DialogDescription>{t($ => $.wizard.transfer_hint)}</DialogDescription></DialogHeader><ExportImportSetting canEdit={canTransfer} /></DialogContent></Dialog>}
      {catalogOpen && <Dialog open onOpenChange={setCatalogOpen}><DialogContent className="max-h-[85vh] overflow-auto sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.wizard.catalog)}</DialogTitle><DialogDescription>{t($ => $.catalog.includes)}</DialogDescription></DialogHeader><OrgTeamCatalog canInstall={canTransfer} onInstalled={() => setCatalogOpen(false)} /></DialogContent></Dialog>}
      {creating && <OrgWizard onClose={() => setCreating(false)} onCreated={setSelectedId} />}
    </div>
  );
}
