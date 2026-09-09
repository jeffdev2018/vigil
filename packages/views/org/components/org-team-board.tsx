"use client";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { OrgSelect } from "./org-select";

import { useMemo, useState } from "react";
import { DndContext, DragOverlay, KeyboardSensor, PointerSensor, useDraggable, useDroppable, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { ArrowRight, Bot, Check, GripVertical, Plus, Search, Users, X } from "lucide-react";
import type { OrgDefinition, OrgMember, OrgUnit } from "@multica/core/types";
import { addOrgMembers, moveOrgMember } from "@multica/core/org";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export type OrgBoardPerson = OrgMember & { name: string; avatar_url?: string | null };

function PersonRow({ person, from, disabled, onClick }: { person: OrgBoardPerson; from?: string; disabled: boolean; onClick: () => void }) {
  const { t } = useT("org");
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: `${from ?? "directory"}:${person.type}:${person.id}`, data: { member: person, from }, disabled });
  return <div ref={setNodeRef} className={cn("relative flex items-center gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-muted", isDragging && "opacity-30")}>
    {!disabled && <button type="button" {...attributes} {...listeners} aria-label={t($ => $.studio.move_person, { name: person.name })} className="shrink-0 cursor-grab touch-none rounded p-1 text-muted-foreground/50 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"><GripVertical className="size-3.5" /></button>}
    <button type="button" onClick={onClick} className="flex min-w-0 flex-1 items-center gap-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <ActorAvatar name={person.name} avatarUrl={person.avatar_url} initials={person.name.slice(0, 2)} isAgent={person.type === "agent"} size="md" />
      <span className="min-w-0 flex-1 truncate text-caption font-medium">{person.name}</span>{person.role === "lead" && <ArrowRight className="size-3 text-info" />}
    </button>
  </div>;
}

function TeamColumn({ unit, people, selected, readOnly, onSelect, onAdd, onRecipient }: { unit: OrgUnit; people: OrgBoardPerson[]; selected: boolean; readOnly: boolean; onSelect: () => void; onAdd: () => void; onRecipient: () => void }) {
  const { t } = useT("org");
  const { setNodeRef, isOver } = useDroppable({ id: unit.id, disabled: readOnly });
  const owner = people.find(p => p.type === "member" && p.id === unit.owner_id);
  const roster = unit.members.map(m => ({ ...people.find(p => p.id === m.id && p.type === m.type), ...m, name: people.find(p => p.id === m.id && p.type === m.type)?.name ?? m.id }));
  const [expanded, setExpanded] = useState(false);
  const recipient = roster.find(m => m.type === "agent" && m.role === "lead") ?? roster.find(m => m.type === "agent");
  return <article ref={setNodeRef} data-testid="org-team" className={cn("flex min-w-0 flex-col self-start rounded-xl border bg-card transition-[border-color,box-shadow] duration-200", isOver ? "border-info ring-4 ring-info/15" : selected ? "border-info/60 shadow-sm" : "border-border/70")}>
    <button onClick={onSelect} type="button" aria-pressed={selected} className="group flex w-full items-start gap-3 rounded-t-xl p-4 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-info/10 text-info"><Users className="size-4" /></span><span className="min-w-0 flex-1"><span className="block text-body font-semibold">{unit.name}</span><span className="mt-1 block line-clamp-2 text-caption leading-relaxed text-muted-foreground">{unit.mission || t($ => $.studio.add_mission)}</span></span><span className="text-caption tabular-nums text-muted-foreground">{unit.members.length}</span>
    </button>
    <div className="mx-4 mb-3 flex items-center gap-2 border-b pb-3 text-caption text-muted-foreground"><ActorAvatar name={owner?.name ?? ""} initials={owner?.name.slice(0, 2) ?? "?"} size="sm" /><span className="truncate">{t($ => $.studio.owner)} <span className="text-foreground">{owner?.name ?? t($ => $.page.no_owner)}</span></span></div>
    <div className="px-2 pb-2">
      {roster.slice(0, expanded ? undefined : 5).map(person => <PersonRow key={`${person.type}:${person.id}`} person={person} from={unit.id} disabled={readOnly} onClick={onSelect} />)}
      {roster.length > 5 && <Button size="sm" variant="ghost" className="w-full text-muted-foreground" onClick={() => setExpanded(v => !v)}>{expanded ? t($ => $.studio.show_less) : t($ => $.studio.show_more, { count: roster.length - 5 })}</Button>}
      {roster.length === 0 && <p className="px-3 py-5 text-caption leading-relaxed text-muted-foreground">{owner ? t($ => $.studio.owner_only, { name: owner.name }) : t($ => $.coherence.empty_team)}</p>}
      {!readOnly && <Button size="sm" variant="ghost" className="mt-1 w-full justify-start gap-2 text-muted-foreground hover:text-foreground" onClick={onAdd}><Plus className="size-3.5" />{t($ => $.coherence.add_members)}</Button>}
    </div>
    <button type="button" onClick={onRecipient} className="flex items-center gap-2 rounded-b-xl border-t bg-muted/20 px-4 py-3 text-left text-caption text-muted-foreground hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring"><ArrowRight className="size-3.5 shrink-0" /><span className="min-w-0 truncate">{t($ => $.studio.receives)} <span className="text-foreground">{unit.squad_id ? t($ => $.studio.linked_squad) : recipient?.name ?? owner?.name ?? t($ => $.page.no_owner)}</span></span></button>
  </article>;
}

export function OrgTeamBoard({ definition, people, selected, readOnly, onSelect, onAdd, onChange, onCreateTeam, onCreateAgent }: {
  definition: OrgDefinition; people: OrgBoardPerson[]; selected: string | null; readOnly: boolean;
  onSelect: (id: string, panel?: "people" | "rules" | "connections") => void; onAdd: (id: string) => void; onChange: (definition: OrgDefinition) => void; onCreateTeam: () => void; onCreateAgent: () => void;
}) {
  const { t } = useT("org");
  const [directory, setDirectory] = useState(true);
  const [search, setSearch] = useState("");
  const [unassigned, setUnassigned] = useState(false);
  const [picked, setPicked] = useState<OrgBoardPerson | null>(null);
  const [target, setTarget] = useState("");
  const [notice, setNotice] = useState("");
  const [dragging, setDragging] = useState<OrgBoardPerson | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor));
  const assigned = useMemo(() => new Set(definition.units.flatMap(u => u.members.map(m => `${m.type}:${m.id}`))), [definition]);
  const missing = people.filter(p => !assigned.has(`${p.type}:${p.id}`));
  const showPeople = people.filter(p => (!unassigned || !assigned.has(`${p.type}:${p.id}`)) && p.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
  const assign = (person: OrgBoardPerson, to: string, from?: string) => {
    const team = definition.units.find(u => u.id === to);
    if (readOnly || !team || from === to || !people.some(p => p.id === person.id && p.type === person.type)) return;
    if (!from && team.members.some(m => m.id === person.id && m.type === person.type)) return;
    const next = from ? moveOrgMember(definition, from, to, person) : addOrgMembers(definition, to, [{ id: person.id, type: person.type }]);
    if (next !== definition) onChange(next);
    setNotice(t($ => $.studio.assigned, { name: person.name, team: definition.units.find(u => u.id === to)!.name }));
    setPicked(null); setTarget("");
  };
  const dropped = (event: DragEndEvent) => {
    setDragging(null);
    const data = event.active.data.current;
    if (data?.member && event.over) assign(data.member as OrgBoardPerson, String(event.over.id), data.from as string | undefined);
  };
  return <DndContext sensors={sensors} onDragStart={event => setDragging(event.active.data.current?.member ?? null)} onDragCancel={() => setDragging(null)} onDragEnd={dropped}>
    <div className="flex flex-wrap items-center gap-3 px-4 py-3 text-caption text-muted-foreground"><span>{t($ => $.studio.board_hint)}</span><Button size="sm" variant="ghost" aria-pressed={directory} className="ml-auto gap-2" onClick={() => setDirectory(v => !v)}><Users className="size-4" />{t($ => $.studio.directory)}<span className="rounded bg-muted px-1.5 tabular-nums">{missing.length}</span></Button></div>
    <div className={cn("grid items-start", directory && "lg:grid-cols-[minmax(0,1fr)_240px]")}>
      <div className="grid min-w-0 gap-4 p-4 sm:grid-cols-2 2xl:grid-cols-3">
        {definition.units.map(unit => <TeamColumn key={unit.id} unit={unit} people={people} readOnly={readOnly} selected={selected === unit.id} onSelect={() => onSelect(unit.id)} onAdd={() => onAdd(unit.id)} onRecipient={() => onSelect(unit.id, unit.squad_id ? "rules" : "people")} />)}
        {!readOnly && <button type="button" onClick={onCreateTeam} className="flex min-h-36 flex-col items-center justify-center gap-2 self-start rounded-xl border border-dashed p-6 text-caption text-muted-foreground transition-colors hover:border-info/50 hover:bg-info/5 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><Plus className="size-5" /><span className="font-medium">{t($ => $.visual.add_team)}</span><span>{t($ => $.studio.team_empty_hint)}</span></button>}
      </div>
      {directory && <aside className="order-first mx-4 mb-4 rounded-xl bg-muted/30 p-3 lg:order-last lg:ml-0" aria-label={t($ => $.studio.directory)}>
        <div className="mb-3 flex items-center justify-between"><h3 className="text-body font-semibold">{t($ => $.studio.directory)}</h3><Button size="sm" variant="ghost" aria-label={t($ => $.visual.close)} onClick={() => setDirectory(false)}><X className="size-3.5" /></Button></div>
        <div className="relative"><Search className="pointer-events-none absolute left-2.5 top-2.5 size-3.5 text-muted-foreground" /><Input className="h-9 pl-8 text-caption" aria-label={t($ => $.visual.search)} placeholder={t($ => $.visual.search)} value={search} onChange={e => setSearch(e.target.value)} /></div>
        <label className="my-3 flex items-center gap-2 text-caption text-muted-foreground"><Checkbox checked={unassigned} onCheckedChange={e => setUnassigned(e)} />{t($ => $.studio.only_unassigned)}</label>
        <p className="mb-2 text-caption leading-relaxed text-muted-foreground">{t($ => $.studio.directory_hint)}</p>
        <div className="max-h-44 overflow-y-auto lg:max-h-80">{showPeople.map(person => <PersonRow key={`${person.type}:${person.id}`} person={person} disabled={readOnly} onClick={() => { setPicked(person); setTarget(""); }} />)}{showPeople.length === 0 && <p className="p-3 text-caption text-muted-foreground">{t($ => $.studio.no_results)}</p>}</div>
        {picked && <div className="mt-3 space-y-2 rounded-lg border bg-background p-3"><p className="text-caption font-medium">{picked.name}</p><OrgSelect aria-label={t($ => $.studio.assign_to)} className="w-full" value={target} disabled={readOnly} onValueChange={e => setTarget(e)} items={[{ value: "", label: t($ => $.visual.choose_team) }, ...definition.units.filter(u => !u.members.some(m => m.id === picked.id && m.type === picked.type)).map(u => ({ value: u.id, label: u.name }))]} /><Button size="sm" className="w-full gap-2" disabled={!target || readOnly} onClick={() => assign(picked, target)}><Check className="size-3.5" />{t($ => $.studio.assign)}</Button></div>}
        {!readOnly && <Button size="sm" variant="outline" className="mt-4 w-full gap-2" onClick={onCreateAgent}><Bot className="size-3.5" />{t($ => $.coherence.create_agent)}</Button>}
      </aside>}
    </div>
    <DragOverlay dropAnimation={null}>{dragging && <div className="flex items-center gap-2 rounded-lg border border-info bg-card p-3 text-caption font-medium shadow-xl ring-4 ring-info/15"><ActorAvatar name={dragging.name} avatarUrl={dragging.avatar_url} initials={dragging.name.slice(0, 2)} isAgent={dragging.type === "agent"} size="md" />{dragging.name}</div>}</DragOverlay>
    <p role="status" aria-live="polite" className={cn("px-4 text-caption text-success", notice && "pb-3")}>{notice}</p>
  </DndContext>;
}
