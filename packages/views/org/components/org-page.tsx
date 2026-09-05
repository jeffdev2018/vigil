"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Network, Plus } from "lucide-react";
import { toast } from "sonner";
import {
  orgDetailOptions,
  orgHealthOptions,
  orgListOptions,
  orgMermaid,
  orgPreflightOptions,
  orgTemplatesOptions,
  useCreateOrgStructure,
  useDeleteOrgStructure,
  useSetOrgStructureStatus,
  useUpdateOrgStructure,
} from "@multica/core/org";
import { contestCostUsd } from "@multica/core/issues/contest";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { OrgDefinition, OrgStatus, OrgStructure, OrgTemplate } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageHeader, CollectionPageHeaderAction, CollectionPageState } from "../../layout/collection-page";
import { MermaidDiagram } from "../../editor/mermaid-diagram";
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

/** Loose structural check: enough to build the units summary without a schema library in views. */
function parseDefinition(text: string): { def: OrgDefinition } | { error: string } {
  try {
    const raw: unknown = JSON.parse(text);
    if (!raw || typeof raw !== "object" || !Array.isArray((raw as { units?: unknown }).units)) return { error: "units[] missing" };
    return { def: raw as OrgDefinition };
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) };
  }
}

// ---------------------------------------------------------------------------
// Template picker — shared by the page and the project section.
// ---------------------------------------------------------------------------

export function OrgTemplateCards({ templates, onPick, disabled }: { templates: OrgTemplate[]; onPick: (t: OrgTemplate) => void; disabled?: boolean }) {
  const { t } = useT("org");
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {templates.map((tpl) => (
        <button
          key={tpl.model}
          type="button"
          data-testid="org-template"
          disabled={disabled}
          onClick={() => onPick(tpl)}
          className="flex flex-col items-start gap-1 rounded-md border p-3 text-left hover:bg-accent/70 disabled:opacity-50"
        >
          <span className="text-body font-medium">{t(($) => $.model[tpl.model])}</span>
          <span className="text-caption text-muted-foreground">{tpl.pattern}</span>
          <span className="text-caption">{tpl.description}</span>
          <span className="text-caption text-muted-foreground">{t(($) => $.new.runs_per_issue, { count: tpl.coordination_runs_per_issue })}</span>
        </button>
      ))}
    </div>
  );
}

function NewStructureDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: templates = [], isPending } = useQuery(orgTemplatesOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const create = useCreateOrgStructure(wsId);
  const [projectId, setProjectId] = useState("");

  const pick = (tpl: OrgTemplate) =>
    create.mutate(
      { project_id: projectId || null, model: tpl.model, name: tpl.name, definition: tpl.definition },
      {
        onSuccess: (s: unknown) => {
          onClose();
          // The shared mutation helper erases the response type; the server answers with the created structure.
          if (s && typeof s === "object" && "id" in s && typeof s.id === "string") onCreated(s.id);
        },
        onError: (e) => toast.error(errorMessage(e, t(($) => $.new.error))),
      },
    );

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.new.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.new.description)}</DialogDescription>
        </DialogHeader>
        <label className="flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.new.project)}
          <select className={SELECT_CLASS} value={projectId} onChange={(e) => setProjectId(e.target.value)}>
            <option value="">{t(($) => $.new.workspace_default)}</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>{p.title}</option>
            ))}
          </select>
        </label>
        {isPending ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.new.loading)}</p>
        ) : (
          <div className="max-h-[60vh] overflow-y-auto">
            <OrgTemplateCards templates={templates} onPick={pick} disabled={create.isPending} />
          </div>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" onClick={onClose}>{t(($) => $.new.cancel)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Status dialogs.
// ---------------------------------------------------------------------------

function ActivateDialog({ structure, onClose }: { structure: OrgStructure; onClose: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: pre } = useQuery(orgPreflightOptions(wsId, structure.id, true));
  const setStatus = useSetOrgStructureStatus(wsId);
  const [attestation, setAttestation] = useState("");
  const submit = () =>
    setStatus.mutate(
      { id: structure.id, action: "activate", eval_attestation: attestation.trim() },
      { onSuccess: onClose, onError: (e) => toast.error(errorMessage(e, t(($) => $.actions.error))) },
    );
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.activate.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.activate.description)}</DialogDescription>
        </DialogHeader>
        {!pre ? (
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
          <Button type="button" size="sm" disabled={setStatus.isPending || !attestation.trim()} onClick={submit}>{t(($) => $.activate.submit)}</Button>
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
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
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
          <Button type="button" variant="outline" size="sm" onClick={onClose}>{t(($) => $.actions.cancel)}</Button>
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
  const { data: health } = useQuery(orgHealthOptions(wsId, structureId));
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
  const { data, isPending } = useQuery(orgDetailOptions(wsId, id));
  if (isPending || !data) return <p className="px-4 py-2 text-caption text-muted-foreground">{t(($) => $.page.loading)}</p>;
  return <OrgDetailBody key={`${data.structure.id}:${data.structure.revision}`} structure={data.structure} revisions={data.revisions} onBack={onBack} onDeleted={onDeleted} />;
}

function OrgDetailBody({ structure, revisions, onBack, onDeleted }: { structure: OrgStructure; revisions: { id: string; revision: number; status: string; model: string; created_at: string }[]; onBack: () => void; onDeleted: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const update = useUpdateOrgStructure(wsId);
  const setStatus = useSetOrgStructureStatus(wsId);
  const [form, setForm] = useState({
    name: structure.name,
    owner_id: structure.owner_id ?? "",
    dissolve_at: toLocalInput(structure.dissolve_at),
    end_condition: structure.end_condition ?? "",
    budget: String(structure.budget_usd_ticks ?? 0),
    definition: JSON.stringify(structure.definition, null, 2),
  });
  const [dialog, setDialog] = useState<"activate" | ReasonAction | null>(null);
  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => setForm((f) => ({ ...f, [key]: value }));

  const parsed = useMemo(() => parseDefinition(form.definition), [form.definition]);
  const readOnly = structure.status === "dissolved";
  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m.name])), [members]);
  const canDelete = useMemo(() => {
    const me = members.find((m) => m.user_id === currentUser?.id);
    return structure.owner_id === currentUser?.id || me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser?.id, structure.owner_id]);

  const save = () => {
    if ("error" in parsed) return;
    update.mutate(
      {
        id: structure.id,
        data: {
          definition: parsed.def,
          name: form.name.trim(),
          owner_id: form.owner_id || null,
          dissolve_at: toRFC3339(form.dissolve_at),
          end_condition: form.end_condition,
          budget_usd_ticks: Number(form.budget) || 0,
        },
      },
      {
        onSuccess: () => toast.success(t(($) => $.form.saved)),
        onError: (e) => toast.error(errorMessage(e, t(($) => $.form.error))),
      },
    );
  };

  const resume = () => setStatus.mutate({ id: structure.id, action: "resume" }, { onError: (e) => toast.error(errorMessage(e, t(($) => $.actions.error))) });
  const chart = useMemo(() => orgMermaid(structure.definition, structure.paused_units), [structure.definition, structure.paused_units]);

  return (
    <div className="flex-1 overflow-y-auto px-4 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="ghost" size="sm" className="gap-1 px-2" onClick={onBack}>
          <ArrowLeft className="size-3.5" />
          {t(($) => $.page.back)}
        </Button>
        <h2 className="text-title font-medium">{structure.name}</h2>
        <Badge variant="outline">{t(($) => $.model[structure.model])}</Badge>
        <Badge className={STATUS_BADGE[structure.status]}>{t(($) => $.status[structure.status])}</Badge>
        <span className="text-caption text-muted-foreground">{t(($) => $.page.revision, { n: structure.revision })}</span>
        <div className="ml-auto flex items-center gap-1">
          {structure.status === "draft" && <Button type="button" size="sm" onClick={() => setDialog("activate")}>{t(($) => $.actions.activate)}</Button>}
          {structure.status === "active" && <Button type="button" size="sm" variant="outline" onClick={() => setDialog("pause")}>{t(($) => $.actions.pause)}</Button>}
          {structure.status === "paused" && <Button type="button" size="sm" disabled={setStatus.isPending} onClick={resume}>{t(($) => $.actions.resume)}</Button>}
          {(structure.status === "active" || structure.status === "paused") && (
            <Button type="button" size="sm" variant="outline" className="text-destructive" onClick={() => setDialog("dissolve")}>{t(($) => $.actions.dissolve)}</Button>
          )}
          {(structure.status === "draft" || structure.status === "dissolved") && canDelete && (
            <Button type="button" size="sm" variant="outline" className="text-destructive" onClick={() => setDialog("delete")}>{t(($) => $.actions.delete)}</Button>
          )}
        </div>
      </div>
      {structure.paused_reason && <p className="mt-1 text-caption text-warning">{structure.paused_reason}</p>}
      {readOnly && <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.page.read_only)}</p>}

      <section className="mt-4">
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.page.chart)}</h3>
        <MermaidDiagram chart={chart} />
      </section>

      <section className="mt-4 grid gap-3 lg:grid-cols-[1fr_18rem]">
        <div className="flex flex-col gap-3">
          <h3 className="text-caption font-medium">{t(($) => $.page.editor)}</h3>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.name)}
              <Input value={form.name} onChange={(e) => set("name", e.target.value)} disabled={readOnly} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.owner)}
              <select className={SELECT_CLASS} value={form.owner_id} onChange={(e) => set("owner_id", e.target.value)} disabled={readOnly}>
                <option value="">{t(($) => $.form.owner_none)}</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>{m.name}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.dissolve_at)}
              <input type="datetime-local" className={SELECT_CLASS} value={form.dissolve_at} onChange={(e) => set("dissolve_at", e.target.value)} disabled={readOnly} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.end_condition)}
              <select className={SELECT_CLASS} value={form.end_condition} onChange={(e) => set("end_condition", e.target.value)} disabled={readOnly}>
                {END_CONDITIONS.map((c) => (
                  <option key={c} value={c}>{c === "" ? t(($) => $.form.end_none) : c === "all_issues_done" ? t(($) => $.form.end_all_issues_done) : t(($) => $.form.end_budget_spent)}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.budget)}
              <Input type="number" min={0} value={form.budget} onChange={(e) => set("budget", e.target.value)} disabled={readOnly} />
            </label>
          </div>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.definition)}
            <Textarea value={form.definition} onChange={(e) => set("definition", e.target.value)} rows={16} spellCheck={false} className="font-mono text-caption" disabled={readOnly} />
          </label>
          {"error" in parsed && <p role="alert" className="text-caption text-destructive">{t(($) => $.form.invalid_json, { error: parsed.error })}</p>}
          {!readOnly && (
            <div>
              <Button type="button" size="sm" disabled={update.isPending || "error" in parsed || !form.name.trim()} onClick={save}>{t(($) => $.form.save)}</Button>
            </div>
          )}
        </div>
        <div>
          <h3 className="mb-1 text-caption font-medium">{t(($) => $.page.units_summary)}</h3>
          {"def" in parsed && parsed.def.units.length > 0 ? (
            <ul className="flex flex-col gap-1">
              {parsed.def.units.map((u) => (
                <li key={u.id} data-testid="org-unit" className="rounded-md border p-2 text-caption">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{u.name}</span>
                    {structure.paused_units.includes(u.id) && <Badge className={STATUS_BADGE.paused}>{t(($) => $.status.paused)}</Badge>}
                  </div>
                  <div className="text-muted-foreground">
                    {u.owner_id ? memberName.get(u.owner_id) ?? u.owner_id : t(($) => $.page.no_owner)}
                    {" · "}
                    {t(($) => $.autonomy[u.autonomy])}
                    {" · "}
                    {t(($) => $.page.members, { count: u.members?.length ?? 0 })}
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.page.no_units)}</p>
          )}
        </div>
      </section>

      <section className="mt-4">
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.health.title)}</h3>
        <OrgHealthSection structureId={structure.id} />
      </section>

      <section className="mt-4 pb-4">
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.page.revisions)}</h3>
        {revisions.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.page.no_revisions)}</p>
        ) : (
          <ul className="flex flex-col gap-0.5 text-caption">
            {revisions.map((r) => (
              <li key={r.id} className="flex items-center gap-2">
                <span>{t(($) => $.page.revision_row, { n: r.revision, status: r.status, model: r.model })}</span>
                <span className="text-muted-foreground">{new Date(r.created_at).toLocaleString()}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      {dialog === "activate" && <ActivateDialog structure={structure} onClose={() => setDialog(null)} />}
      {dialog && dialog !== "activate" && (
        <ReasonDialog structure={structure} action={dialog} onClose={() => setDialog(null)} onDone={dialog === "delete" ? onDeleted : undefined} />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function OrgPage() {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: structures = [], isLoading } = useQuery(orgListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const projectTitle = useMemo(() => new Map(projects.map((p) => [p.id, p.title])), [projects]);
  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m.name])), [members]);
  const sorted = useMemo(
    () => [...structures].sort((a, b) => Number(a.project_id !== null) - Number(b.project_id !== null) || a.created_at.localeCompare(b.created_at)),
    [structures],
  );

  if (selectedId) {
    return (
      <div className="relative flex flex-1 min-h-0 flex-col">
        <CollectionPageHeader icon={Network} title={t(($) => $.page.title)} />
        <OrgDetail id={selectedId} onBack={() => setSelectedId(null)} onDeleted={() => setSelectedId(null)} />
      </div>
    );
  }

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Network}
        title={t(($) => $.page.title)}
        count={structures.length}
        actions={<CollectionPageHeaderAction icon={Plus} label={t(($) => $.page.new_structure)} onClick={() => setCreating(true)} />}
      />
      {!isLoading && structures.length === 0 ? (
        <CollectionPageState
          icon={Network}
          title={t(($) => $.page.empty)}
          description={t(($) => $.page.empty_description)}
          actions={<Button size="sm" variant="outline" onClick={() => setCreating(true)}>{t(($) => $.page.new_structure)}</Button>}
        />
      ) : (
        <div className="flex-1 overflow-y-auto px-4 py-2">
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {sorted.map((s) => (
              <button
                key={s.id}
                type="button"
                data-testid="org-structure"
                onClick={() => setSelectedId(s.id)}
                className="flex flex-col items-start gap-1 rounded-md border p-3 text-left hover:bg-accent/70"
              >
                <span className="text-caption text-muted-foreground">
                  {s.project_id === null ? t(($) => $.page.workspace_default) : projectTitle.get(s.project_id) ?? t(($) => $.page.unknown_project)}
                </span>
                <span className="text-body font-medium">{s.name}</span>
                <span className="flex flex-wrap items-center gap-1.5">
                  <Badge variant="outline">{t(($) => $.model[s.model])}</Badge>
                  <Badge className={STATUS_BADGE[s.status]}>{t(($) => $.status[s.status])}</Badge>
                  <span className="text-caption text-muted-foreground">{t(($) => $.page.revision, { n: s.revision })}</span>
                </span>
                <span className="text-caption text-muted-foreground">
                  {s.owner_id ? memberName.get(s.owner_id) ?? s.owner_id : t(($) => $.page.no_owner)}
                  {s.paused_units.length > 0 && ` · ${t(($) => $.page.paused_units, { count: s.paused_units.length })}`}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
      {creating && <NewStructureDialog onClose={() => setCreating(false)} onCreated={setSelectedId} />}
    </div>
  );
}
