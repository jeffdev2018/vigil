"use client";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { OrgSelect } from "./org-select";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Network, Plus, ArrowUpRight, GitBranch, Activity, History, Settings2, Check, Save } from "lucide-react";
import { toast } from "sonner";
import {
  parseEditableOrgDefinition,
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
import type { OrgStatus, OrgStructure, OrgRevision } from "@multica/core/types";
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
    <Tabs value={tab} onValueChange={v => setTab(v as typeof tab)} className="min-h-0 min-w-0 flex-1 overflow-y-auto px-4 pb-5 md:px-6">
      <div className="flex flex-wrap items-center gap-2 pt-4">
        <Button type="button" variant="ghost" size="sm" className="gap-1 px-2" onClick={() => onBack()}>
          <ArrowLeft className="size-3.5" />
          {t(($) => $.page.back)}
        </Button>
        <h2 className="text-title-sm font-semibold tracking-tight">{structure.name}</h2>
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

      <div className="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-3 border-b bg-background py-3">
        <TabsList variant="line" aria-label={t($ => $.page.title)}>{(["compose", "activity", "history", "settings"] as const).map(key => { const Icon = { compose: GitBranch, activity: Activity, history: History, settings: Settings2 }[key]; return <TabsTrigger value={key} key={key} className="gap-2 px-3"><Icon className="size-4" />{t($ => $.workspace.tabs[key])}</TabsTrigger>; })}</TabsList>
        <Button size="sm" variant="outline" aria-pressed={testing} onClick={() => { setTab("compose"); setTesting(v => !v); }}>{t($ => $.coherence.test)}</Button>
        {!readOnly && <div className="flex flex-wrap items-center gap-2"><span role="status" className="hidden items-center gap-1.5 text-caption text-muted-foreground sm:flex">{dirty ? <span className="size-1.5 rounded-full bg-warning" /> : <Check className="size-3.5 text-success" />}{dirty ? t($ => $.coherence.draft_saved) : t($ => $.workspace.saved)}</span><Button size="sm" variant="ghost" disabled={!dirty || update.isPending} onClick={discard}>{t($ => $.coherence.discard)}</Button><Button size="sm" disabled={!dirty || update.isPending || "error" in parsed || !form.name.trim() || problems.length > 0 || baseRevision !== structure.revision} onClick={() => structure.status === "active" ? setPublishing(true) : save()}><Save className="mr-1.5 size-3.5" />{structure.status === "active" ? t($ => $.coherence.publish) : t($ => $.form.save)}</Button></div>}
      </div>
      {baseRevision !== structure.revision && dirty && <p role="alert" className="mt-4 text-caption text-warning">{t($ => $.coherence.conflict)}</p>}
      <TabsContent value="compose" keepMounted className="mt-4 space-y-3">
        {testing && "def" in parsed && <OrgTester structureId={structure.id} definition={parsed.def} model={structure.model} status={structure.status} revision={structure.revision} dirty={dirty} goals={goals} onSelectUnit={setFocusedUnit} />}
        <OrgProblemList problems={problems} />
        {"def" in parsed && <OrgEditor focusedUnit={focusedUnit} definition={parsed.def} model={structure.model} pausedUnits={structure.paused_units} readOnly={readOnly || update.isPending} onChange={def => set("definition", JSON.stringify(def, null, 2))} />}
      </TabsContent>
      <TabsContent value="settings" keepMounted className="mx-auto mt-6 max-w-3xl rounded-2xl border bg-card p-6">
        <div className="flex flex-col gap-3">
          <h3 className="text-caption font-medium">{t(($) => $.page.editor)}</h3>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.name)}
              <Input value={form.name} onChange={(e) => set("name", e.target.value)} disabled={readOnly || update.isPending} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.owner)}
              <OrgSelect className="w-full" value={form.owner_id} onValueChange={(e) => set("owner_id", e)} disabled={readOnly || update.isPending} items={[{ value: "", label: t(($) => $.form.owner_none) }, ...members.map((m) => (
                  ({ value: m.user_id, label: m.name })
                ))]} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.dissolve_at)}
              <input type="datetime-local" className={SELECT_CLASS} value={form.dissolve_at} onChange={(e) => set("dissolve_at", e.target.value)} disabled={readOnly || update.isPending} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.end_condition)}
              <OrgSelect className="w-full" value={form.end_condition} onValueChange={(e) => set("end_condition", e)} disabled={readOnly || update.isPending} items={[...END_CONDITIONS.map((c) => (
                  ({ value: c, label: c === "" ? t(($) => $.form.end_none) : c === "all_issues_done" ? t(($) => $.form.end_all_issues_done) : t(($) => $.form.end_budget_spent) })
                ))]} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.budget)}
              <Input type="number" min={0} step="0.01" value={Number(form.budget) / 1_000_000} onChange={(e) => set("budget", String(Math.round(Number(e.target.value) * 1_000_000)))} disabled={readOnly || update.isPending} />
            </label>
          </div>
          {"def" in parsed && <details className="space-y-3 rounded-lg border p-4"><summary className="cursor-pointer text-body font-medium">{t($ => $.coherence.collective)}</summary>
            {parsed.def.committees.map((committee, index) => <div key={index} className="space-y-2 rounded-lg bg-muted/30 p-3"><label className="block text-caption">{t($ => $.coherence.decision_type)}<Input disabled={readOnly || update.isPending} value={committee.decision_type} onChange={e => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.map((c, i) => i === index ? { ...c, decision_type: e.target.value } : c) }, null, 2))} /></label>
              <fieldset disabled={readOnly || update.isPending} className="flex flex-wrap gap-3"><legend className="text-caption">{t($ => $.coherence.sections.units)}</legend>{parsed.def.units.map(unit => <label key={unit.id} className="flex items-center gap-1 text-caption"><Checkbox checked={committee.unit_ids.includes(unit.id)} onCheckedChange={e => set("definition", JSON.stringify({ ...parsed.def, committees: parsed.def.committees.map((c, i) => i === index ? { ...c, unit_ids: e ? [...c.unit_ids, unit.id] : c.unit_ids.filter(id => id !== unit.id) } : c) }, null, 2))} />{unit.name}</label>)}</fieldset>
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
      </TabsContent>

      <TabsContent value="activity" keepMounted className="mt-6 rounded-2xl border bg-card p-6">
        <h3 className="mb-4 text-title-sm font-semibold">{t(($) => $.health.title)}</h3>
        <OrgHealthSection structureId={structure.id} />
      </TabsContent>

      <TabsContent value="history" keepMounted className="mx-auto mt-6 max-w-4xl rounded-2xl border bg-card p-6">
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
      </TabsContent>

      {publishing && <Dialog open onOpenChange={open => { if (!update.isPending) setPublishing(open); }}><DialogContent><DialogHeader><DialogTitle>{t($ => $.coherence.publish)}</DialogTitle><DialogDescription>{t($ => $.coherence.publish_hint)}</DialogDescription></DialogHeader><ul className="space-y-2 text-body">{"def" in parsed && orgDefinitionChanges(structure.definition, parsed.def).map(c => <li key={c.id}>{c.after?.name ?? c.before?.name ?? t($ => $.coherence.sections[c.section])}</li>)}</ul><DialogFooter><Button variant="outline" disabled={update.isPending} onClick={() => setPublishing(false)}>{t($ => $.actions.cancel)}</Button><Button disabled={update.isPending} onClick={save}>{t($ => $.coherence.publish)}</Button></DialogFooter></DialogContent></Dialog>}
      {reviewRevision && <Dialog open onOpenChange={open => { if (!open) setReviewRevision(null); }}><DialogContent className="sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.history.title, { n: reviewRevision.revision })}</DialogTitle><DialogDescription>{t($ => $.history.description)}</DialogDescription></DialogHeader><div className="max-h-[60vh] space-y-4 overflow-auto">{reviewRevision.definition && <><div className="space-y-2">{orgDefinitionChanges(structure.definition, reviewRevision.definition).map(change => <div key={change.id} className="rounded-lg border p-3"><p className="text-body font-medium">{change.after?.name ?? change.before?.name ?? t($ => $.coherence.sections[change.section])}</p><div className="mt-2 grid gap-2 text-caption sm:grid-cols-2">{([change.before, change.after]).map((unit, i) => <div key={i} className="rounded-md bg-muted/50 p-3"><span className="text-muted-foreground">{i === 0 ? t($ => $.history.current) : t($ => $.history.previous)}</span><p className="mt-1">{unit ? `${unit.name} · ${t($ => $.autonomy[unit.autonomy])} · ${t($ => $.page.members, { count: unit.members.length })}` : t($ => $.history.absent)}</p><p>{unit?.roles.map(r => r.name).join(", ")}</p></div>)}</div></div>)}</div><p className="text-caption text-muted-foreground">{t($ => $.history.full_diff)}</p></>}<details><summary className="cursor-pointer text-caption font-medium">{t($ => $.visual.advanced)}</summary><div className="mt-3 grid gap-3 sm:grid-cols-2"><div><h4 className="mb-2 text-caption font-semibold">{t($ => $.history.current)}</h4><pre className="overflow-auto rounded-md bg-muted p-3 text-caption">{JSON.stringify(structure.definition, null, 2)}</pre></div><div><h4 className="mb-2 text-caption font-semibold">{t($ => $.history.previous)}</h4><pre className="overflow-auto rounded-md bg-muted p-3 text-caption">{JSON.stringify(reviewRevision.definition, null, 2)}</pre></div></div></details></div>{dirty && <p className="text-caption text-warning">{t($ => $.history.dirty)}</p>}<DialogFooter><Button variant="outline" onClick={() => setReviewRevision(null)}>{t($ => $.actions.cancel)}</Button><Button disabled={readOnly || dirty || update.isPending || reviewRevision.revision === structure.revision} onClick={restore}>{t($ => $.history.restore)}</Button></DialogFooter></DialogContent></Dialog>}
      {dialog === "activate" && <ActivateDialog structure={structure} onClose={() => setDialog(null)} />}
      {dialog && dialog !== "activate" && (
        <ReasonDialog structure={structure} action={dialog} onClose={() => setDialog(null)} onDone={dialog === "delete" ? onDeleted : undefined} />
      )}
    </Tabs>
  );
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
  const [overview, setOverview] = useState(false);
  const [search, setSearch] = useState("");
  const select = (id: string | null) => { useOrgDraftStore.getState().setDraft({ selectedId: id }); setOverview(false); };
  const [creating, setCreating] = useState(false);
  const [transferring, setTransferring] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const currentUser = useAuthStore(s => s.user);
  const canTransfer = members.some(m => m.user_id === currentUser?.id && (m.role === "owner" || m.role === "admin"));
  const projectTitle = new Map(projects.map(p => [p.id, p.title]));
  const sorted = [...structures].sort((a, b) => Number(a.project_id !== null) - Number(b.project_id !== null) || a.created_at.localeCompare(b.created_at));
  const current = sorted.find(s => s.id === selectedId) ?? sorted.find(s => s.project_id === null && s.status !== "dissolved") ?? sorted.find(s => s.status === "active") ?? sorted[0];
  const scope = (s: OrgStructure) => s.project_id ? projectTitle.get(s.project_id) ?? t($ => $.page.unknown_project) : t($ => $.page.workspace_default);

  return <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
    <CollectionPageHeader className="h-auto min-h-12 flex-wrap py-2 [&>div:last-child]:flex-wrap [&>div:last-child]:justify-start" icon={Network} title={t($ => $.page.title)} actions={<>
      {current && <OrgSelect aria-label={t($ => $.studio.organization)} data-testid="org-structure-picker" value={current.id} onValueChange={e => select(e)} className="w-full max-w-64" items={[...sorted.map(s => ({ value: s.id, label: [scope(s), " · ", s.name].join("") }))]} />}
      <Button size="sm" variant="ghost" onClick={() => setOverview(v => !v)}>{t($ => $.studio.browse)}</Button>
      <Button size="sm" variant="ghost" onClick={() => setCatalogOpen(true)}>{t($ => $.wizard.catalog)}</Button>
      <CollectionPageHeaderAction icon={Plus} label={t($ => $.page.new_structure)} onClick={() => setCreating(true)} />
    </>} />
    {isLoading || isError ? <CollectionPageState icon={Network} title={isError ? t($ => $.form.error) : t($ => $.page.loading)} actions={isError ? <Button onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button> : undefined} />
      : current && !overview ? <OrgDetail key={current.id} id={current.id} onBack={() => setOverview(true)} onDeleted={() => { select(null); setOverview(true); }} />
      : <div className="flex-1 overflow-auto p-5 md:p-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4"><div><h1 className="text-title-lg font-semibold">{t($ => $.studio.browse)}</h1><p className="mt-1 text-body text-muted-foreground">{t($ => $.studio.scope_hint)}</p></div><Button size="sm" variant="outline" onClick={() => setTransferring(true)}>{t($ => $.wizard.transfer)}</Button></div>
        {sorted.length > 0 && <Input className="mb-4 max-w-sm" aria-label={t($ => $.studio.search_org)} placeholder={t($ => $.studio.search_org)} value={search} onChange={e => setSearch(e.target.value)} />}
        <div className="divide-y rounded-xl border">{sorted.filter(s => `${s.name} ${scope(s)}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(s => <button key={s.id} type="button" data-testid="org-structure" onClick={() => select(s.id)} className="group flex w-full items-center gap-4 p-4 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-muted"><Network className="size-5 text-muted-foreground" /></span>
          <span className="min-w-0 flex-1"><span className="block truncate text-body font-semibold">{s.name}</span><span className="mt-1 block text-caption text-muted-foreground">{scope(s)} · {t($ => $.model[s.model])}</span></span>
          <span className="hidden text-caption text-muted-foreground sm:block">{t($ => $.visual.team_count, { count: s.definition.units.length })}</span><Badge className={STATUS_BADGE[s.status]}>{t($ => $.status[s.status])}</Badge><ArrowUpRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
        </button>)}</div>
        {sorted.length === 0 && <div className="mx-auto flex max-w-xl flex-col items-start gap-4 py-14"><Network className="size-10 text-muted-foreground" /><h2 className="text-title-lg font-semibold">{t($ => $.studio.empty_title)}</h2><p className="text-body text-muted-foreground">{t($ => $.studio.empty_hint)}</p><Button onClick={() => setCreating(true)}><Plus className="mr-2 size-4" />{t($ => $.page.new_structure)}</Button></div>}
      </div>}
    {transferring && <Dialog open onOpenChange={setTransferring}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.wizard.transfer)}</DialogTitle><DialogDescription>{t($ => $.wizard.transfer_hint)}</DialogDescription></DialogHeader><ExportImportSetting canEdit={canTransfer} /></DialogContent></Dialog>}
    {catalogOpen && <Dialog open onOpenChange={setCatalogOpen}><DialogContent className="max-h-[85vh] overflow-auto sm:max-w-3xl"><DialogHeader><DialogTitle>{t($ => $.wizard.catalog)}</DialogTitle><DialogDescription>{t($ => $.catalog.includes)}</DialogDescription></DialogHeader><OrgTeamCatalog canInstall={canTransfer} onInstalled={() => setCatalogOpen(false)} /></DialogContent></Dialog>}
    {creating && <OrgWizard onClose={() => setCreating(false)} onCreated={select} />}
  </div>;
}
