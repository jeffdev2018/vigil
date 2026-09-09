"use client";

import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Maximize, Minus, Plus, Network, Users, Trash2, X, ShieldCheck, ArrowUpRight, Bot, GitBranch } from "lucide-react";
import { addOrgMembers, removeOrgMember, moveOrgMember, orgUnitRemovalBlockers, orgLayout, orgWouldCycle, removeOrgUnit } from "@multica/core/org";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { OrgDefinition, OrgEdgeKind, OrgUnit, OrgModel } from "@multica/core/types";
import { ORG_NON_NEGOTIABLE_DENY, ORG_PROPERTIES, ORG_DECIDER_CLASSES, orgEffectiveModel } from "@multica/core/org/validate";
import { goalListOptions } from "@multica/core/goals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@multica/ui/components/ui/dialog";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@multica/ui/components/ui/sheet";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { OrgTeamBoard } from "./org-team-board";

const field = "h-9 w-full rounded-md border bg-background px-2 text-body focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";
const AUTONOMY = ["read_only", "draft", "approve_payload", "auto"] as const;
const MODELS: OrgModel[] = ["hierarchy", "squads", "matrix", "circles", "owner_network", "market"];
const initials = (name: string) => name.split(" ").map(n => n[0]).slice(0, 2).join("");

const EDGE_KINDS: OrgEdgeKind[] = ["reports_to", "escalates_to", "backs_up", "consults"];

export function OrgEditor({ definition, onChange, model, readOnly = false, pausedUnits = [], focusedUnit }: { focusedUnit?: string | null; model?: OrgModel; definition: OrgDefinition; onChange: (d: OrgDefinition) => void; readOnly?: boolean; pausedUnits?: string[] }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const agentQuery = useQuery(agentListOptions(wsId));
  const memberQuery = useQuery(memberListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: goals = [] } = useQuery(goalListOptions(wsId));
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const agents = agentQuery.data ?? [];
  const members = memberQuery.data ?? [];
  const [selected, setSelected] = useState<string | null>(null);
  useEffect(() => { if (focusedUnit) { setSelected(focusedUnit); setView("chart"); } }, [focusedUnit]);
  const [zoom, setZoom] = useState(1);
  const [panel, setPanel] = useState<"people" | "rules" | "connections">("people");
  const [search, setSearch] = useState("");
  const [view, setView] = useState<"board" | "chart" | "people">("board");
  const [creating, setCreating] = useState(false);
  const [teamName, setTeamName] = useState("");
  const [teamOwner, setTeamOwner] = useState("");
  const [teamParent, setTeamParent] = useState("");
  const [addingPeople, setAddingPeople] = useState(false);
  const [picked, setPicked] = useState<string[]>([]);
  const [removing, setRemoving] = useState(false);
  const [target, setTarget] = useState("");
  const [relation, setRelation] = useState<OrgEdgeKind>("reports_to");
  const [kind, setKind] = useState<OrgEdgeKind>("reports_to");
  const viewport = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  const marker = useId().replace(/:/g, "");
  const layout = useMemo(() => orgLayout({ ...definition, edges: relation === "reports_to" ? definition.edges.filter(e => e.kind === "reports_to") : [] }), [definition, relation]);
  const positions = new Map(layout.nodes.map(n => [n.unit.id, n]));
  const unit = definition.units.find(u => u.id === selected);
  const patch = (change: Partial<OrgUnit>) => { if (unit && !readOnly) onChange({ ...definition, units: definition.units.map(u => u.id === unit.id ? { ...u, ...change } : u) }); };
  const people = [...members.map(m => ({ id: m.user_id, name: m.name, avatar_url: m.avatar_url, type: "member" as const })), ...agents.map(a => ({ id: a.id, name: a.name, avatar_url: a.avatar_url, type: "agent" as const }))];
  const ownerName = (id?: string) => members.find(m => m.user_id === id)?.name ?? t($ => $.page.no_owner);
  const fit = () => { setZoom(Math.min(1, Math.max(.3, ((viewport.current?.clientWidth ?? layout.width) - 24) / layout.width))); viewport.current?.scrollTo?.({ left: 0, top: 0 }); };
  useEffect(() => {
    const container = viewport.current;
    if (!container || view !== "chart") return;
    const center = () => {
      const next = Math.min(1, Math.max(.3, (container.clientWidth - 24) / layout.width));
      setZoom(next);
      container.scrollTo?.({ left: Math.max(0, (layout.width / 2) * next - container.clientWidth / 2), top: 0 });
    };
    center();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(center);
    observer.observe(container);
    return () => observer.disconnect();
  }, [layout.width, layout.height, view]);
  const add = () => {
    if (!teamName.trim() || readOnly || (model === "hierarchy" && definition.units.length > 0 && !teamParent)) return;
    const id = crypto.randomUUID();
    const next: OrgUnit = { id, name: teamName.trim(), owner_id: teamOwner || undefined, kind: "unit", members: [], roles: model === "circles" ? [{ id: crypto.randomUUID(), name: t($ => $.visual.new_role) }] : [], autonomy: "draft", excludes: ["external_effects"], allow: ["read", "comment", "propose_plan"], deny: [], escalation_quota_per_day: 5 };
    onChange({ ...definition, units: [...definition.units, next], edges: teamParent ? [...definition.edges, { from: id, to: teamParent, kind: "reports_to" }] : definition.edges });
    setSelected(id); setView("board"); setCreating(false); setTeamName(""); setTeamOwner(""); setTeamParent(""); setPanel("people");
  };
  const addEdge = () => {
    if (readOnly || !unit || !target || (kind === "reports_to" && orgWouldCycle(definition, unit.id, target))) return;
    if (definition.edges.some(e => e.from === unit.id && e.to === target && e.kind === kind)) return;
    onChange({ ...definition, edges: [...definition.edges.filter(e => !(model === "hierarchy" && kind === "reports_to" && e.kind === "reports_to" && e.from === unit.id)), { from: unit.id, to: target, kind }] }); setTarget("");
  };
  const cyclic = !!unit && kind === "reports_to" && !!target && orgWouldCycle(definition, unit.id, target);

  const selectTeam = (id: string, tab: "people" | "rules" | "connections" = "people") => { setSelected(id); setPanel(tab); setTarget(""); };
  const addPeopleTo = (id: string) => { setSelected(id); setPanel("people"); setAddingPeople(true); setPicked([]); setSearch(""); };

  return <section className="relative rounded-xl border bg-card" aria-label={t($ => $.visual.title)}>
    <div className="flex flex-wrap items-center gap-2 border-b px-5 py-4">
      <Network className="size-4 text-muted-foreground" /><div className="flex rounded-lg bg-muted p-1">{(["board", "chart", "people"] as const).map(v => <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)} className={cn("rounded-md px-2 py-1 text-caption focus-visible:ring-2 focus-visible:ring-ring", view === v && "bg-background font-semibold shadow-sm")}>{t($ => $.studio.views[v])}</button>)}</div>
      <span className="text-caption text-muted-foreground">{t($ => $.visual.team_count, { count: definition.units.length })}</span>
      <div className="ml-auto flex max-w-full flex-wrap items-center gap-1">

        <div hidden={view !== "chart"} className={cn(view === "chart" && "flex", "items-center gap-1")}><Button size="sm" variant="ghost" aria-label={t($ => $.visual.zoom_out)} onClick={() => setZoom(z => Math.max(.3, z - .1))}><Minus className="size-4" /></Button>
        <output className="w-12 text-center text-caption tabular-nums">{Math.round(zoom * 100)}%</output>
        <Button size="sm" variant="ghost" aria-label={t($ => $.visual.zoom_in)} onClick={() => setZoom(z => Math.min(2, z + .1))}><Plus className="size-4" /></Button>
        <Button size="sm" variant="ghost" aria-label={t($ => $.visual.fit)} onClick={fit}><Maximize className="size-4" /></Button></div>
        {!readOnly && <Button size="sm" className="ml-2 gap-1" onClick={() => setCreating(true)}><Plus className="size-4" />{t($ => $.visual.add_team)}</Button>}
      </div>
    </div>
    {view === "board" && <OrgTeamBoard definition={definition} people={people} selected={selected} readOnly={readOnly} onSelect={selectTeam} onAdd={addPeopleTo} onChange={onChange} onCreateTeam={() => setCreating(true)} onCreateAgent={() => navigation.push(paths.newAgent())} />}
    {view === "people" && <div className="max-h-[640px] overflow-auto p-5"><p className="mb-4 text-caption text-muted-foreground">{t($ => $.coherence.directory_hint)}</p><div className="grid gap-3 sm:grid-cols-2">{people.map(person => <div key={`${person.type}:${person.id}`} className="rounded-xl border p-4"><div className="flex items-center gap-3"><ActorAvatar name={person.name} initials={initials(person.name)} isAgent={person.type === "agent"} size="lg" /><div><p className="text-body font-semibold">{person.name}</p><p className="text-caption text-muted-foreground">{t($ => $.visual[person.type])}</p></div></div><div className="mt-3 flex flex-wrap gap-2">{definition.units.filter(u => u.members.some(m => m.id === person.id && m.type === person.type)).map(u => <Button key={u.id} size="sm" variant="outline" onClick={() => { setSelected(u.id); setView("chart"); setPanel("people"); }}>{u.name}</Button>)}</div>{person.type === "member" && <div className="mt-2 flex flex-wrap gap-2">{definition.units.filter(u => u.owner_id === person.id).map(u => <Button key={u.id} size="sm" variant="ghost" onClick={() => { setSelected(u.id); setView("chart"); setPanel("rules"); }}>{t($ => $.coherence.owns_team, { name: u.name })}</Button>)}</div>}{!definition.units.some(u => u.members.some(m => m.id === person.id && m.type === person.type) || (person.type === "member" && u.owner_id === person.id)) && <p className="mt-3 text-caption text-muted-foreground">{t($ => $.coherence.unassigned)}</p>}</div>)}</div></div>}
    <div hidden={view !== "chart"} className={cn(view === "chart" && "grid")}>
      <div className="relative min-w-0 bg-muted/20" style={{ backgroundImage: "radial-gradient(var(--border) 1px, transparent 1px)", backgroundSize: "20px 20px" }}>
        <div className="flex flex-wrap items-center gap-3 px-4 py-3"><select aria-label={t($ => $.studio.relationships)} className={field + " max-w-64"} value={relation} onChange={e => setRelation(e.target.value as OrgEdgeKind)}>{EDGE_KINDS.map(k => <option key={k} value={k}>{t($ => $.studio.relations[k])}</option>)}</select><span className="text-caption text-muted-foreground">{t($ => $.studio.relation_hint[relation])}</span></div>
        <div ref={viewport} tabIndex={0} role="region" aria-label={t($ => $.visual.canvas)} className="h-[360px] lg:h-[580px] overflow-auto overscroll-contain cursor-grab focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          onPointerDown={e => { if ((e.target as HTMLElement).closest("button") || e.button !== 0 || e.pointerType === "touch") return; drag.current = { x: e.clientX, y: e.clientY, left: e.currentTarget.scrollLeft, top: e.currentTarget.scrollTop }; e.currentTarget.setPointerCapture(e.pointerId); }}
          onPointerMove={e => { if (drag.current) { e.currentTarget.scrollLeft = drag.current.left + drag.current.x - e.clientX; e.currentTarget.scrollTop = drag.current.top + drag.current.y - e.clientY; } }}
          onPointerUp={() => { drag.current = null; }} onPointerCancel={() => { drag.current = null; }}>
          <div className="mx-auto" style={{ width: layout.width * zoom, height: layout.height * zoom }}>
            <div className="relative origin-top-left" style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
              <svg width={layout.width} height={layout.height} className="absolute inset-0 text-muted-foreground/50" aria-hidden="true">
                <defs><marker id={marker} markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0L7 3.5L0 7Z" fill="currentColor" /></marker></defs>
                {definition.edges.filter(e => e.kind === relation).map((e, i) => {
                  const a = positions.get(e.from), b = positions.get(e.to); if (!a || !b) return null;
                  const above = b.y < a.y;
                  const y1 = a.y + (above ? 0 : 178), y2 = b.y + (above ? 178 : 0);
                  const sameRow = a.y === b.y;
                  const path = sameRow ? `M${a.x + 140},${a.y} C${a.x + 140},${a.y - 45} ${b.x + 140},${b.y - 45} ${b.x + 140},${b.y}` : `M${a.x + 140},${y1} C${a.x + 140},${(y1+y2)/2} ${b.x+140},${(y1+y2)/2} ${b.x+140},${y2}`;
                  return <g key={i}><path d={path} fill="none" stroke="currentColor" strokeWidth={e.kind === "reports_to" ? 2 : 1} strokeDasharray={e.kind === "reports_to" ? undefined : "5 5"} markerEnd={`url(#${marker})`} />{(e.from === selected || e.to === selected) && <text x={(a.x + b.x) / 2 + 140} y={sameRow ? a.y - 25 : (y1 + y2) / 2 - 6} textAnchor="middle" className="fill-muted-foreground text-caption" stroke="var(--background)" strokeWidth={4} paintOrder="stroke">{t($ => $.visual.edge[e.kind])}</text>}</g>;
                })}
              </svg>
              {layout.nodes.map(({ unit: u, x, y }) => {
                const roster = u.members.map(m => people.find(p => p.id === m.id && p.type === m.type)).filter(p => p !== undefined);
                const accent = pausedUnits.includes(u.id) ? "text-warning bg-warning/10" : "text-info bg-info/10";
                return <button key={u.id} type="button" data-testid="org-node" aria-pressed={selected === u.id} onClick={() => { setSelected(u.id); setTarget(""); setAddingPeople(false); setPicked([]); }} className={cn("group absolute flex h-[178px] w-[280px] flex-col overflow-hidden rounded-2xl border bg-card text-left shadow-sm transition-[box-shadow,border-color] duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", selected === u.id ? "border-info shadow-lg ring-4 ring-info/10" : "hover:border-info/50 hover:shadow-md")} style={{ left: x, top: y }}>
                  <span className="flex w-full items-start gap-3 px-4 pt-4"><span className={cn("flex size-9 shrink-0 items-center justify-center rounded-xl", accent)}><Users className="size-4" /></span><span className="min-w-0 flex-1"><span className="block truncate text-body font-semibold">{u.name}</span><span className="mt-0.5 block truncate text-caption text-muted-foreground">{u.mission || t($ => $.model[orgEffectiveModel(definition, u.id, model ?? "hierarchy")])}</span></span><ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" /></span>
                  <span className="mt-3 flex items-center gap-2 px-4"><ActorAvatar name={ownerName(u.owner_id)} initials={initials(ownerName(u.owner_id))} size="sm" /><span className="truncate text-caption text-muted-foreground">{ownerName(u.owner_id)}</span></span>
                  <span className="mt-2 flex items-center gap-1.5 truncate px-4 text-caption text-muted-foreground"><Bot className="size-3 shrink-0" /><span className="truncate">{roster.filter(p => p.type === "agent").map(p => p.name).join(", ") || t($ => $.visual.no_agents)}</span></span>
                  <span className="mt-auto flex w-full items-center justify-between border-t bg-muted/20 px-4 py-3"><span className="flex items-center"><span className="mr-2 flex -space-x-1.5">{roster.slice(0, 3).map(p => <ActorAvatar key={`${p.type}:${p.id}`} name={p.name} initials={initials(p.name)} isAgent={p.type === "agent"} size="md" className="ring-2 ring-card" />)}</span><span className="text-caption text-muted-foreground">{u.members.length}</span></span><span className={cn("rounded-full px-2 py-1 text-caption", pausedUnits.includes(u.id) ? "bg-warning/10 text-warning" : "bg-muted text-muted-foreground")}>{pausedUnits.includes(u.id) ? t($ => $.status.paused) : t($ => $.autonomy[u.autonomy])}</span></span>
                </button>;
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
      {unit && !addingPeople && !removing && <Sheet open onOpenChange={open => { if (!open) setSelected(null); }}><SheetContent showCloseButton={false} className="data-[side=right]:w-[min(92vw,520px)] data-[side=right]:sm:max-w-[520px] overflow-hidden p-5 pt-12"><SheetHeader className="p-0 pr-8"><SheetTitle>{unit.name}</SheetTitle><SheetDescription>{t($ => $.studio.inspector_hint)}</SheetDescription></SheetHeader><Button className="absolute right-4 top-3" variant="ghost" size="sm" aria-label={t($ => $.visual.close)} onClick={() => setSelected(null)}><X className="size-4" /></Button><aside className="min-h-0 flex-1 overflow-y-auto pr-1" aria-label={t($ => $.visual.edit_team)}>
        <div className="mb-5 flex rounded-lg bg-muted p-1" aria-label={t($ => $.visual.edit_team)}>{(["people", "rules", "connections"] as const).map(key => <button key={key} type="button" aria-pressed={panel === key} onClick={() => setPanel(key)} className={cn("flex flex-1 items-center justify-center gap-1.5 rounded-md px-2 py-2 text-caption transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", panel === key ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground")}>{key === "people" ? <Users className="size-3.5" /> : key === "rules" ? <ShieldCheck className="size-3.5" /> : <GitBranch className="size-3.5" />}{t($ => $.visual.tabs[key])}</button>)}</div>
        <fieldset disabled={readOnly} className="flex flex-col gap-4 disabled:opacity-70">
          <label className="space-y-1 text-caption">{t($ => $.visual.team_name)}<Input value={unit.name} onChange={e => patch({ name: e.target.value })} /></label>
          {!unit.name.trim() && <p role="alert" className="text-caption text-destructive">{t($ => $.problem.unit_name_required)}</p>}
          <div hidden={panel !== "people"} className="space-y-4">          <label className="space-y-1 text-caption">{t($ => $.coherence.mission)}<Textarea rows={2} maxLength={240} value={unit.mission ?? ""} onChange={e => patch({ mission: e.target.value })} /></label>
          <label className="space-y-1 text-caption">{t($ => $.coherence.owner)}<select className={field} value={unit.owner_id ?? ""} onChange={e => patch({ owner_id: e.target.value || undefined })}><option value="">{t($ => $.form.owner_none)}</option>{members.map(m => <option key={m.user_id} value={m.user_id}>{m.name}</option>)}</select></label>
</div>
          <div hidden={panel !== "rules"} className="space-y-4">
          <label className="block space-y-1 text-caption">{t($ => $.coherence.squad)}<select className={field} value={unit.squad_id ?? ""} onChange={e => patch({ squad_id: e.target.value || undefined })}><option value="">{t($ => $.coherence.no_squad)}</option>{squads.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select></label>

          <label className="block space-y-1 text-caption">{t($ => $.coherence.team_model)}<select className={field} value={unit.model ?? ""} onChange={e => patch({ model: e.target.value ? e.target.value as OrgModel : undefined })}><option value="">{t($ => $.coherence.inherit_model)}</option>{MODELS.map(m => <option key={m} value={m}>{t($ => $.model[m])}</option>)}</select></label>
          <label className="block space-y-1 text-caption">{t($ => $.visual.autonomy)}<select className={field} value={unit.autonomy} onChange={e => patch({ autonomy: e.target.value as OrgUnit["autonomy"] })}>{AUTONOMY.map(a => <option key={a} value={a}>{t($ => $.autonomy[a])}</option>)}</select></label>
          <p className="-mt-2 text-caption text-muted-foreground">{t($ => $.visual.trust_hint)}</p>
          <label className="space-y-1 text-caption">{t($ => $.visual.budget)}<Input type="number" min="0" step="0.01" value={(unit.budget_usd_ticks ?? 0) / 1_000_000} onChange={e => { const amount = Number(e.target.value); if (Number.isFinite(amount) && amount >= 0) patch({ budget_usd_ticks: Math.round(amount * 1_000_000) }); }} /></label>
          <label className="block space-y-1 text-caption">{t($ => $.coherence.goal)}<select className={field} value={unit.mission_goal_id ?? ""} onChange={e => patch({ mission_goal_id: e.target.value || undefined })}><option value="">{t($ => $.coherence.no_goal)}</option>{goals.map(g => <option key={g.id} value={g.id}>{g.title}</option>)}</select></label>
          <details className="space-y-3 rounded-lg border p-3"><summary className="cursor-pointer text-caption">{t($ => $.coherence.permissions)}</summary>
            {(["allow", "deny"] as const).map(key => <label key={key} className="block space-y-1 text-caption">{t($ => $.coherence[key])}<Textarea value={unit[key].join("\n")} onChange={e => patch({ [key]: e.target.value.split("\n") })} /></label>)}
            <p className="text-caption text-muted-foreground">{t($ => $.coherence.locked_deny)}: {ORG_NON_NEGOTIABLE_DENY.join(", ")}</p>
            <fieldset className="space-y-2"><legend className="text-caption font-medium">{t($ => $.coherence.exposures)}</legend>{ORG_PROPERTIES.map(property => <label key={property} className="flex items-center gap-2 text-caption"><input type="checkbox" checked={!unit.excludes.includes(property)} onChange={e => patch({ excludes: e.target.checked ? unit.excludes.filter(p => p !== property) : [...unit.excludes, property] })} />{t($ => $.coherence.properties[property])}</label>)}</fieldset>
            <label className="flex items-center gap-2 text-caption"><input type="checkbox" checked={unit.human_approval === true} onChange={e => patch({ human_approval: e.target.checked })} />{t($ => $.coherence.human_approval)}</label>
            {!unit.excludes.includes("external_effects") && ORG_DECIDER_CLASSES.map(key => <label key={key} className="block text-caption">{t($ => $.coherence.deciders[key])}<select className={field} value={unit.deciders?.[key] ?? ""} onChange={e => patch({ deciders: { ...unit.deciders, [key]: e.target.value } })}><option value="">{t($ => $.form.owner_none)}</option>{members.map(m => <option key={m.user_id} value={m.user_id}>{m.name}</option>)}</select></label>)}
            <label className="block space-y-1 text-caption">{t($ => $.coherence.escalation_quota)}<Input type="number" min={0} step={1} value={unit.escalation_quota_per_day} onChange={e => patch({ escalation_quota_per_day: Math.max(0, Math.floor(Number(e.target.value))) })} /></label>
          </details>
          </div>
          <div hidden={panel !== "people"} className="space-y-5">
              <label className="block space-y-1 text-caption">{t($ => $.coherence.recipient)}<select className={field} value={unit.members.find(m => m.type === "agent" && m.role === "lead")?.id ?? ""} onChange={e => patch({ members: unit.members.map(m => m.type === "agent" ? { ...m, role: m.id === e.target.value ? "lead" : m.role === "lead" ? undefined : m.role } : m) })}><option value="">{t($ => $.coherence.recipient_auto)}</option>{unit.members.filter(m => m.type === "agent").map(m => <option key={m.id} value={m.id}>{agents.find(a => a.id === m.id)?.name ?? m.id}</option>)}</select></label>
              <p className="text-caption text-muted-foreground">{t($ => $.coherence.recipient_hint)}</p>
            <div className="space-y-3">
              <div className="flex items-center justify-between"><h5 className="whitespace-nowrap text-body font-semibold">{t($ => $.coherence.members)} · {unit.members.length}</h5><Button size="sm" variant="outline" onClick={() => { setAddingPeople(true); setPicked([]); setSearch(""); }}><Plus className="mr-1 size-3" />{t($ => $.coherence.add_members)}</Button></div>
              <p className="text-caption text-muted-foreground">{t($ => $.coherence.members_hint)}</p>
              {(agentQuery.isError || memberQuery.isError) && <div role="alert" className="text-caption text-destructive">{t($ => $.coherence.people_error)} <Button variant="link" onClick={() => { void agentQuery.refetch(); void memberQuery.refetch(); }}>{t($ => $.catalog.retry)}</Button></div>}
              {unit.members.length === 0 && <div className="rounded-xl border border-dashed p-5 text-center text-caption text-muted-foreground">{t($ => $.coherence.empty_team)}</div>}
              {unit.members.map(member => {
                const person = people.find(p => p.id === member.id && p.type === member.type);
                const name = person?.name ?? member.id;
                return <div key={`${member.type}:${member.id}`} className="rounded-xl border p-3">
                  <div className="flex items-center gap-2"><ActorAvatar name={name} initials={initials(name)} isAgent={member.type === "agent"} size="lg" /><div className="min-w-0 flex-1"><p className="truncate text-body font-medium">{name}</p><p className="text-caption text-muted-foreground">{t($ => $.visual[member.type])}{member.role === "lead" ? ` · ${t($ => $.coherence.recipient)}` : ""}</p></div><Button size="sm" variant="ghost" aria-label={t($ => $.coherence.remove_member, { name })} onClick={() => onChange(removeOrgMember(definition, unit.id, member))}><X className="size-3.5" /></Button></div>
                  <select className={`${field} mt-2`} aria-label={t($ => $.coherence.move_member, { name })} value="" onChange={e => onChange(moveOrgMember(definition, unit.id, e.target.value, member))}><option value="">{t($ => $.coherence.move_hint)}</option>{definition.units.filter(u => u.id !== unit.id).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select>
                  <select className={`${field} mt-2`} aria-label={t($ => $.visual.assign_role)} value={member.role_id ?? ""} onChange={e => patch({ members: unit.members.map(m => m.id === member.id && m.type === member.type ? { ...m, role_id: e.target.value || undefined } : m) })}><option value="">{t($ => $.coherence.no_role)}</option>{unit.roles.map(role => <option key={role.id} value={role.id}>{role.name}</option>)}</select>
                </div>;
              })}

            </div>
          <div className="space-y-2"><h5 className="text-caption font-medium">{t($ => $.visual.roles)}</h5>{unit.roles.map(role => <div key={role.id} className="space-y-1 rounded-lg border bg-muted/20 p-3"><div className="flex justify-end"><Button variant="ghost" size="sm" aria-label={t($ => $.visual.remove_role)} disabled={orgEffectiveModel(definition, unit.id, model ?? "hierarchy") === "circles" && unit.roles.length <= 1} onClick={() => patch({ roles: unit.roles.filter(r => r.id !== role.id), members: unit.members.map(m => m.role_id === role.id ? { ...m, role_id: undefined } : m) })}><X className="size-3.5" /></Button></div><Input aria-label={t($ => $.visual.role_name)} value={role.name} onChange={e => patch({ roles: unit.roles.map(r => r.id === role.id ? { ...r, name: e.target.value } : r) })} /><Input aria-label={t($ => $.visual.keywords)} value={(role.keywords ?? []).join(", ")} onChange={e => patch({ roles: unit.roles.map(r => r.id === role.id ? { ...r, keywords: e.target.value.split(",").map(k => k.trim()) } : r) })} /><Input aria-label={t($ => $.visual.responsibilities)} value={role.responsibilities ?? ""} onChange={e => patch({ roles: unit.roles.map(r => r.id === role.id ? { ...r, responsibilities: e.target.value } : r) })} /></div>)}<Button size="sm" variant="outline" onClick={() => patch({ roles: [...unit.roles, { id: crypto.randomUUID(), name: t($ => $.visual.new_role) }] })}>{t($ => $.visual.add_role)}</Button></div>
          </div>
          <div hidden={panel !== "connections"} className="space-y-5"><div className="space-y-2"><h5 className="text-caption font-medium">{t($ => $.visual.routing)}</h5><p className="text-caption text-muted-foreground">{t($ => $.visual.routing_hint)}</p>{definition.rules.filter(r => r.target_unit === unit.id).map(rule => <div key={rule.id} className="space-y-2 rounded-lg border p-3">
              {(["keywords", "labels", "paths"] as const).map(key => <label key={key} className="block text-caption">{t($ => $.coherence.rule_fields[key])}<Input value={(rule[key] ?? []).join(", ")} onChange={e => onChange({ ...definition, rules: definition.rules.map(r => r.id === rule.id ? { ...r, [key]: e.target.value.split(",").map(v => v.trim()) } : r) })} /></label>)}
              <label className="block text-caption">{t($ => $.coherence.priority)}<Input type="number" value={rule.priority} onChange={e => onChange({ ...definition, rules: definition.rules.map(r => r.id === rule.id ? { ...r, priority: Number(e.target.value) || 0 } : r) })} /></label>
              <Button size="sm" variant="ghost" onClick={() => onChange({ ...definition, rules: definition.rules.filter(r => r.id !== rule.id) })}>{t($ => $.coherence.remove_rule)}</Button>
            </div>)}<Button size="sm" variant="outline" onClick={() => onChange({ ...definition, rules: [...definition.rules, { id: crypto.randomUUID(), target_unit: unit.id, keywords: [], priority: 1 }] })}>{t($ => $.coherence.add_rule)}</Button></div>
          <div className="space-y-2"><h5 className="text-caption font-medium">{t($ => $.visual.connections)}</h5>{definition.edges.filter(e => e.from === unit.id).map(e => <div key={`${e.kind}:${e.to}`} className="flex items-center gap-1 text-caption"><label className="flex items-center gap-1"><input type="checkbox" aria-label={t($ => $.coherence.human_approval)} checked={e.human_approval === true} onChange={event => onChange({ ...definition, edges: definition.edges.map(v => v === e ? { ...v, human_approval: event.target.checked } : v) })} /><span className="sr-only">{t($ => $.coherence.human_approval)}</span></label><span className="min-w-0 flex-1">{t($ => $.visual.edge[e.kind])} → {definition.units.find(u => u.id === e.to)?.name}</span><Button size="sm" variant="ghost" aria-label={t($ => $.visual.remove_connection)} onClick={() => onChange({ ...definition, edges: definition.edges.filter(v => v !== e) })}><X className="size-3" /></Button></div>)}
            <select aria-label={t($ => $.visual.connection_type)} className={field} value={kind} onChange={e => setKind(e.target.value as OrgEdgeKind)}>{EDGE_KINDS.map(k => <option key={k} value={k}>{t($ => $.visual.edge[k])}</option>)}</select>
            <select aria-label={t($ => $.visual.target)} className={field} value={target} onChange={e => setTarget(e.target.value)}><option value="">{t($ => $.visual.choose_team)}</option>{definition.units.filter(u => u.id !== unit.id).map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select>
            {cyclic && <p role="alert" className="text-caption text-destructive">{t($ => $.visual.cycle)}</p>}<Button variant="outline" size="sm" disabled={!target || cyclic} onClick={addEdge}>{t($ => $.visual.add_connection)}</Button>
          </div>
          </div>
          <Button variant="ghost" size="sm" className="justify-start gap-2 text-destructive" disabled={definition.units.length <= 1} onClick={() => setRemoving(true)}><Trash2 className="size-4" />{t($ => $.visual.remove_team)}</Button>
        </fieldset>
      </aside><Button variant="outline" onClick={() => setSelected(null)}>{t($ => $.studio.back_to_teams)}</Button></SheetContent></Sheet>}
    {creating && <Dialog open onOpenChange={setCreating}><DialogContent><DialogHeader><DialogTitle>{t($ => $.visual.add_team)}</DialogTitle><DialogDescription>{t($ => $.coherence.create_team_hint)}</DialogDescription></DialogHeader><form onSubmit={e => { e.preventDefault(); add(); }} className="space-y-4">
      <label className="block space-y-1 text-caption">{t($ => $.visual.team_name)}<Input autoFocus value={teamName} onChange={e => setTeamName(e.target.value)} /></label>
      <label className="block space-y-1 text-caption">{t($ => $.coherence.owner)}<select className={field} value={teamOwner} onChange={e => setTeamOwner(e.target.value)}><option value="">{t($ => $.form.owner_none)}</option>{members.map(m => <option key={m.user_id} value={m.user_id}>{m.name}</option>)}</select></label>
      <label className="block space-y-1 text-caption">{t($ => $.coherence.parent)}<select className={field} value={teamParent} onChange={e => setTeamParent(e.target.value)}><option value="">{t($ => $.coherence.no_parent)}</option>{definition.units.map(u => <option key={u.id} value={u.id}>{u.name}</option>)}</select></label>
      {model === "hierarchy" && !teamParent && <p className="text-caption text-muted-foreground">{t($ => $.studio.parent_required)}</p>}<DialogFooter><Button type="button" variant="outline" onClick={() => setCreating(false)}>{t($ => $.actions.cancel)}</Button><Button type="submit" disabled={!teamName.trim() || readOnly || (model === "hierarchy" && definition.units.length > 0 && !teamParent)}>{t($ => $.visual.add_team)}</Button></DialogFooter>
    </form></DialogContent></Dialog>}
    {addingPeople && unit && <Dialog open onOpenChange={setAddingPeople}><DialogContent><DialogHeader><DialogTitle>{t($ => $.coherence.add_to, { name: unit.name })}</DialogTitle><DialogDescription>{t($ => $.coherence.members_hint)}</DialogDescription></DialogHeader>
      <Input aria-label={t($ => $.visual.search)} placeholder={t($ => $.visual.search)} value={search} onChange={e => setSearch(e.target.value)} />
      <div className="max-h-80 space-y-1 overflow-y-auto">{people.filter(p => !unit.members.some(m => m.type === p.type && m.id === p.id) && p.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(p => {
        const key = `${p.type}:${p.id}`;
        return <label key={key} className="flex cursor-pointer items-center gap-3 rounded-lg p-3 hover:bg-muted has-[:checked]:bg-info/10"><input type="checkbox" checked={picked.includes(key)} onChange={e => setPicked(prev => e.target.checked ? [...prev, key] : prev.filter(v => v !== key))} /><ActorAvatar name={p.name} initials={initials(p.name)} isAgent={p.type === "agent"} size="lg" /><span className="min-w-0 flex-1 truncate text-body font-medium">{p.name}</span><span className="text-caption text-muted-foreground">{t($ => $.visual[p.type])}</span></label>;
      })}</div><DialogFooter><Button variant="outline" onClick={() => setAddingPeople(false)}>{t($ => $.actions.cancel)}</Button><Button disabled={picked.length === 0 || readOnly} onClick={() => { onChange(addOrgMembers(definition, unit.id, people.filter(p => picked.includes(`${p.type}:${p.id}`)).map(p => ({ id: p.id, type: p.type })))); setAddingPeople(false); setSelected(null); setPicked([]); }}>{t($ => $.coherence.add_selected, { count: picked.length })}</Button></DialogFooter>
    </DialogContent></Dialog>}
    {removing && unit && <Dialog open onOpenChange={setRemoving}><DialogContent><DialogHeader><DialogTitle>{t($ => $.visual.remove_team)}</DialogTitle><DialogDescription>{t($ => $.coherence.remove_team_hint, { name: unit.name, members: unit.members.length, edges: definition.edges.filter(e => e.from === unit.id || e.to === unit.id).length, rules: definition.rules.filter(r => r.target_unit === unit.id).length })}</DialogDescription></DialogHeader>
      {orgUnitRemovalBlockers(definition, unit.id).map(c => <p key={c.decision_type} role="alert" className="text-caption text-destructive">{t($ => $.coherence.quorum_block, { name: c.decision_type, quorum: c.quorum })}</p>)}<DialogFooter><Button variant="outline" onClick={() => setRemoving(false)}>{t($ => $.actions.cancel)}</Button><Button variant="destructive" disabled={readOnly || orgUnitRemovalBlockers(definition, unit.id).length > 0} onClick={() => { onChange(removeOrgUnit(definition, unit.id)); setSelected(null); setRemoving(false); }}>{t($ => $.visual.remove_team)}</Button></DialogFooter>
    </DialogContent></Dialog>}
  </section>;
}
