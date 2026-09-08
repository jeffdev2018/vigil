"use client";

import { useMemo, useRef, useState } from "react";
import { DndContext, PointerSensor, useDraggable, useDroppable, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { AlertTriangle, GripVertical, Plus, Undo2 } from "lucide-react";
import { orgMermaid } from "@multica/core/org";
import { orgEffectiveModel, orgProblemsForUnit, type OrgProblem } from "@multica/core/org/validate";
import { orgLayout } from "@multica/core/org/layout";
import type { Goal, MemberWithUser, OrgDefinition, OrgMember, OrgModel, OrgUnit } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { MermaidDiagram } from "../../editor/mermaid-diagram";
import { useT } from "../../i18n";
import { OrgProblemList } from "./org-problem-list";
import { OrgUnitSheet } from "./org-unit-sheet";

/** The two models this canvas draws itself. The other five keep the diagram
 *  until their own layout lands. */
const LAID_OUT: OrgModel[] = ["hierarchy", "squads"];
const MAX_AVATARS = 6;

const initialsOf = (name: string): string =>
  name.split(" ").map((w) => w[0] ?? "").join("").toUpperCase().slice(0, 2);

// --- pure edits on the definition -------------------------------------------
// Every canvas gesture goes through one of these, so the advanced JSON editor
// and the canvas always describe the same object.

function patchUnit(def: OrgDefinition, id: string, patch: Partial<OrgUnit>): OrgDefinition {
  return { ...def, units: def.units.map((u) => (u.id === id ? { ...u, ...patch } : u)) };
}

function moveMember(def: OrgDefinition, fromId: string, index: number, toId: string): OrgDefinition {
  if (fromId === toId) return def;
  const from = def.units.find((u) => u.id === fromId);
  const member: OrgMember | undefined = from?.members?.[index];
  if (member === undefined) return def;
  return {
    ...def,
    units: def.units.map((u) => {
      if (u.id === fromId) return { ...u, members: u.members.filter((_, i) => i !== index) };
      if (u.id === toId) {
        if (u.members.some((m) => m.type === member.type && m.id === member.id)) return u;
        return { ...u, members: [...u.members, member] };
      }
      return u;
    }),
  };
}

/** True when `ancestorId` is reachable from `id` by following reports_to. */
function reportsInto(def: OrgDefinition, id: string, ancestorId: string): boolean {
  const seen = new Set<string>();
  let cur: string | undefined = id;
  while (cur !== undefined && !seen.has(cur)) {
    seen.add(cur);
    if (cur === ancestorId) return true;
    cur = def.edges.find((e) => e.from === cur && e.kind === "reports_to")?.to;
  }
  return false;
}

/** Re-parent `childId` under `parentId`, refusing the move that would make the
 *  chart loop — the server would reject it and the canvas could not draw it. */
function setReportsTo(def: OrgDefinition, childId: string, parentId: string): OrgDefinition {
  if (childId === parentId || reportsInto(def, parentId, childId)) return def;
  const edges = def.edges.filter((e) => !(e.from === childId && e.kind === "reports_to"));
  return { ...def, edges: [...edges, { from: childId, to: parentId, kind: "reports_to" }] };
}

function addUnit(def: OrgDefinition, name: string): { def: OrgDefinition; id: string } {
  let n = def.units.length + 1;
  while (def.units.some((u) => u.id === `unit-${n}`)) n += 1;
  const id = `unit-${n}`;
  const unit: OrgUnit = {
    id,
    name,
    excludes: ["external_effects"],
    autonomy: "draft",
    allow: [],
    deny: [],
    escalation_quota_per_day: 5,
    members: [],
    roles: [],
  };
  return { def: { ...def, units: [...def.units, unit] }, id };
}

function removeUnit(def: OrgDefinition, id: string): OrgDefinition {
  return {
    ...def,
    units: def.units.filter((u) => u.id !== id),
    edges: def.edges.filter((e) => e.from !== id && e.to !== id),
    rules: def.rules.filter((r) => r.target_unit !== id),
    committees: def.committees.map((c) => ({ ...c, unit_ids: c.unit_ids.filter((u) => u !== id) })),
  };
}

// --- cards -------------------------------------------------------------------

function MemberChip({
  member,
  unitId,
  index,
  name,
  avatarUrl,
  draggable,
}: {
  member: OrgMember;
  unitId: string;
  index: number;
  name: string;
  avatarUrl: string | null;
  draggable: boolean;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `member:${unitId}:${index}`,
    disabled: !draggable,
  });
  return (
    <span
      ref={setNodeRef}
      {...(draggable ? listeners : {})}
      {...attributes}
      data-testid="org-member-chip"
      title={name}
      className={cn("inline-flex", draggable && "cursor-grab", isDragging && "opacity-40")}
    >
      <ActorAvatar name={name} initials={initialsOf(name)} avatarUrl={avatarUrl} isAgent={member.type === "agent"} size="sm" />
    </span>
  );
}

interface CardProps {
  unit: OrgUnit;
  model: OrgModel;
  paused: boolean;
  selected: boolean;
  problems: OrgProblem[];
  actorName: (type: "member" | "agent", id: string) => string;
  actorAvatar: (type: "member" | "agent", id: string) => string | null;
  readOnly: boolean;
  onSelect: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
}

function OrgUnitCard({ unit, model, paused, selected, problems, actorName, actorAvatar, readOnly, onSelect, onKeyDown }: CardProps) {
  const { t } = useT("org");
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: `unit:${unit.id}` });
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({ id: `unit:${unit.id}`, disabled: readOnly });
  const members = unit.members ?? [];
  const shown = members.slice(0, MAX_AVATARS);
  const overflow = members.length - shown.length;

  return (
    <div
      ref={setDropRef}
      data-testid="org-unit-card"
      data-unit-id={unit.id}
      style={{ borderInlineStartColor: `var(--org-${model.replace(/_/g, "-")})` }}
      className={cn(
        "flex h-full w-full flex-col gap-1.5 rounded-md border border-l-[3px] bg-card p-2",
        isOver && "ring-2 ring-ring",
        isDragging && "opacity-40",
        selected && "border-brand shadow-sm",
      )}
    >
      <div className="flex items-start gap-1">
        {!readOnly && (
          <span
            ref={setDragRef}
            {...listeners}
            {...attributes}
            aria-hidden
            className="mt-0.5 cursor-grab text-muted-foreground"
          >
            <GripVertical className="size-3.5" />
          </span>
        )}
        <button
          type="button"
          data-org-card
          aria-pressed={selected}
          onClick={onSelect}
          onKeyDown={onKeyDown}
          className="min-w-0 flex-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="block truncate text-body font-medium" title={unit.name}>{unit.name}</span>
          <span className="block truncate text-caption text-muted-foreground">{t(($) => $.autonomy[unit.autonomy])}</span>
        </button>
        {paused && <Badge className="bg-warning/10 text-warning">{t(($) => $.status.paused)}</Badge>}
      </div>

      {problems.length > 0 && (
        <span data-testid="org-card-problem" className="flex items-center gap-1 text-caption text-warning">
          <AlertTriangle className="size-3 shrink-0" aria-hidden />
          <span className="truncate" title={problems.map((p) => p.code).join(", ")}>
            {t(($) => $.canvas.problem_count, { count: problems.length })}
          </span>
        </span>
      )}

      <div className="mt-auto flex flex-wrap items-center gap-1">
        {shown.map((m, i) => (
          <MemberChip
            key={`${m.type}:${m.id}`}
            member={m}
            unitId={unit.id}
            index={i}
            name={actorName(m.type, m.id)}
            avatarUrl={actorAvatar(m.type, m.id)}
            draggable={!readOnly}
          />
        ))}
        {overflow > 0 && (
          <span className="text-caption text-muted-foreground">{t(($) => $.canvas.more_members, { count: overflow })}</span>
        )}
      </div>
    </div>
  );
}

// --- canvas ------------------------------------------------------------------

export interface OrgCanvasProps {
  definition: OrgDefinition;
  /** The structure's model; a unit may still run another one. */
  model: OrgModel;
  pausedUnits: string[];
  problems: OrgProblem[];
  members: MemberWithUser[];
  agents: { id: string; name: string; avatar_url: string | null }[];
  goals: Goal[];
  readOnly: boolean;
  onChange: (next: OrgDefinition) => void;
  undoDepth: number;
  onUndo: () => void;
}

/**
 * Fill the org chart by looking at it: units as cards on the levels their
 * reports_to edges describe, members as avatars you drag between them, and the
 * unit's own record beside the chart. Every gesture rewrites the same
 * `definition` the advanced JSON editor shows.
 */
export function OrgCanvas({
  definition,
  model,
  pausedUnits,
  problems,
  members,
  agents,
  goals,
  readOnly,
  onChange,
  undoDepth,
  onUndo,
}: OrgCanvasProps) {
  const { t } = useT("org");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m])), [members]);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const actorName = (type: "member" | "agent", id: string): string =>
    (type === "agent" ? agentById.get(id)?.name : memberName.get(id)?.name) ?? id;
  const actorAvatar = (type: "member" | "agent", id: string): string | null =>
    (type === "agent" ? agentById.get(id)?.avatar_url : memberName.get(id)?.avatar_url) ?? null;

  const layout = useMemo(() => orgLayout(definition), [definition]);
  const selected = definition.units.find((u) => u.id === selectedId) ?? null;
  const drawn = LAID_OUT.includes(model);

  const onDragEnd = (e: DragEndEvent) => {
    const active = String(e.active.id);
    const over = e.over === null ? "" : String(e.over.id);
    if (!over.startsWith("unit:")) return;
    const target = over.slice("unit:".length);
    if (active.startsWith("member:")) {
      const [, fromId, index] = active.split(":");
      if (fromId === undefined || index === undefined) return;
      onChange(moveMember(definition, fromId, Number(index), target));
      return;
    }
    if (active.startsWith("unit:")) onChange(setReportsTo(definition, active.slice("unit:".length), target));
  };

  /** Arrow keys walk the cards in layout order; Enter opens the one in focus. */
  const onCardKeyDown = (unitId: string) => (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") return; // the button's own click handles it
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (step === 0) return;
    e.preventDefault();
    const cards = [...(containerRef.current?.querySelectorAll<HTMLElement>("[data-org-card]") ?? [])];
    const ids = layout.nodes.map((n) => n.id);
    const next = ids[(ids.indexOf(unitId) + step + ids.length) % ids.length];
    cards.find((c) => c.closest("[data-unit-id]")?.getAttribute("data-unit-id") === next)?.focus();
  };

  const create = () => {
    const { def, id } = addUnit(definition, t(($) => $.canvas.new_unit_name));
    onChange(def);
    setSelectedId(id);
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {!readOnly && (
          <Button type="button" size="sm" variant="outline" className="gap-1" onClick={create}>
            <Plus className="size-3.5" />
            {t(($) => $.canvas.add_unit)}
          </Button>
        )}
        {!readOnly && (
          <Button type="button" size="sm" variant="ghost" className="gap-1" disabled={undoDepth === 0} onClick={onUndo}>
            <Undo2 className="size-3.5" />
            {t(($) => $.canvas.undo, { count: undoDepth })}
          </Button>
        )}
        <span className="text-caption text-muted-foreground">{t(($) => $.canvas.hint)}</span>
      </div>

      <div className="grid gap-3 min-[820px]:grid-cols-[1fr_22rem]">
        <div ref={containerRef} data-testid="org-canvas" className="overflow-x-auto rounded-md border p-2">
          {definition.units.length === 0 ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.canvas.empty)}</p>
          ) : !drawn ? (
            <div className="flex flex-col gap-1">
              <p className="text-caption text-muted-foreground">{t(($) => $.canvas.layout_soon, { model: t(($) => $.model[model]) })}</p>
              <MermaidDiagram chart={orgMermaid(definition, pausedUnits)} />
            </div>
          ) : (
            <DndContext sensors={sensors} onDragEnd={onDragEnd}>
              <div className="relative" style={{ width: layout.width, height: layout.height }}>
                <svg
                  aria-hidden
                  className="pointer-events-none absolute inset-0 text-muted-foreground"
                  width={layout.width}
                  height={layout.height}
                >
                  {layout.edges.map((e) => (
                    <path
                      key={`${e.from}-${e.to}-${e.kind}`}
                      data-testid="org-edge"
                      data-kind={e.kind}
                      d={e.d}
                      fill="none"
                      stroke="currentColor"
                      strokeWidth={1.5}
                      strokeDasharray={e.kind === "reports_to" ? undefined : "4 3"}
                    />
                  ))}
                </svg>
                {layout.nodes.map((n) => {
                  const unit = definition.units.find((u) => u.id === n.id);
                  if (unit === undefined) return null;
                  return (
                    <div key={n.id} className="absolute" style={{ left: n.x, top: n.y, width: n.width, height: n.height }}>
                      <OrgUnitCard
                        unit={unit}
                        model={orgEffectiveModel(definition, unit.id, model)}
                        paused={pausedUnits.includes(unit.id)}
                        selected={selectedId === unit.id}
                        problems={orgProblemsForUnit(problems, unit.id)}
                        actorName={actorName}
                        actorAvatar={actorAvatar}
                        readOnly={readOnly}
                        onSelect={() => setSelectedId(unit.id)}
                        onKeyDown={onCardKeyDown(unit.id)}
                      />
                    </div>
                  );
                })}
              </div>
            </DndContext>
          )}
        </div>

        {selected === null ? (
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <p className="text-caption text-muted-foreground">{t(($) => $.canvas.no_selection)}</p>
            <OrgProblemList problems={problems.filter((p) => p.unit_id === undefined)} />
          </div>
        ) : (
          <OrgUnitSheet
            unit={selected}
            units={definition.units}
            problems={orgProblemsForUnit(problems, selected.id)}
            members={members}
            goals={goals}
            actorName={actorName}
            readOnly={readOnly}
            onPatch={(patch) => onChange(patchUnit(definition, selected.id, patch))}
            onMoveMember={(index, toUnitId) => onChange(moveMember(definition, selected.id, index, toUnitId))}
            onDelete={() => {
              onChange(removeUnit(definition, selected.id));
              setSelectedId(null);
            }}
            onClose={() => setSelectedId(null)}
          />
        )}
      </div>
    </div>
  );
}
