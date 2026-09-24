"use client";

import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { Maximize2, Minus, Plus, TriangleAlert } from "lucide-react";
import type { Agent, OrgDefinition, OrgMember, OrgUnit } from "@multica/core/types";
import { orgRows } from "@multica/core/org";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { OrgAutonomyDial } from "./org-autonomy-dial";

/** What the inspector shows: the whole organization, one team, or one person in a team. */
export type OrgSelection =
  | { kind: "org" }
  | { kind: "unit"; unitId: string }
  | { kind: "person"; unitId: string; member: Pick<OrgMember, "type" | "id"> };

/**
 * A tested request drawn on the chart. The order is the order the work would
 * actually take, which is why it is numbered: receive, prepare, decide, then
 * each unit it would escalate to.
 */
export interface OrgPlanTrace {
  unitId: string;
  prepares?: Pick<OrgMember, "type" | "id"> | null;
  decides?: Pick<OrgMember, "type" | "id"> | null;
  escalation: string[];
}

/** People shown per team before the rest folds behind "+ N others". */
const VISIBLE_PEOPLE = 5;
const ZOOM_STEPS = [0.6, 0.75, 0.9, 1, 1.15, 1.3] as const;

const sameMember = (a: Pick<OrgMember, "type" | "id">, b: Pick<OrgMember, "type" | "id">) =>
  a.type === b.type && a.id === b.id;
const personKey = (unitId: string, m: Pick<OrgMember, "type" | "id">) => `${unitId}/${m.type}:${m.id}`;

export function OrgPlan({
  definition,
  selection,
  onSelect,
  trace,
  pausedUnits = [],
  agents,
  readOnly = false,
  onMoveMember,
  onAddMembers,
  onAddUnit,
  empty,
}: {
  definition: OrgDefinition;
  selection: OrgSelection;
  onSelect: (selection: OrgSelection) => void;
  trace?: OrgPlanTrace | null;
  pausedUnits?: string[];
  agents: Agent[];
  readOnly?: boolean;
  onMoveMember?: (fromUnit: string, toUnit: string, member: OrgMember) => void;
  onAddMembers?: (unitId: string) => void;
  onAddUnit?: () => void;
  /** Rendered in place of the chart when the organization has no team yet. */
  empty?: ReactNode;
}) {
  const { t } = useT("org");
  const rows = useMemo(() => orgRows(definition), [definition]);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const [zoom, setZoom] = useState(1);
  const stageRef = useRef<HTMLDivElement>(null);
  const links = useOrgLinks(stageRef, definition, trace, zoom);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );
  const onDragEnd = useCallback(
    (event: DragEndEvent) => {
      const drag = event.active.data.current as { unitId: string; member: OrgMember } | undefined;
      const target = event.over?.id;
      if (!drag || typeof target !== "string" || target === drag.unitId) return;
      onMoveMember?.(drag.unitId, target, drag.member);
    },
    [onMoveMember],
  );

  if (definition.units.length === 0) return <>{empty}</>;

  const zoomIndex = ZOOM_STEPS.indexOf(zoom as (typeof ZOOM_STEPS)[number]);
  const tracedUnits = trace ? new Set([trace.unitId, ...trace.escalation]) : null;

  return (
    <div
      className="relative min-h-0 flex-1 overflow-auto bg-page-canvas [background-image:radial-gradient(var(--surface-border)_1px,transparent_1px)] [background-size:18px_18px]"
      onClick={(e) => {
        // A click on the canvas itself, not on a team or a person, goes back
        // to the organization as a whole.
        if (e.target === e.currentTarget || (e.target as HTMLElement).dataset.orgCanvas === "true")
          onSelect({ kind: "org" });
      }}
    >
      <DndContext sensors={sensors} onDragEnd={onDragEnd}>
        <div
          ref={stageRef}
          data-org-canvas="true"
          className="relative mx-auto flex w-max min-w-full origin-top flex-col items-center gap-[72px] px-10 pb-28 pt-9 transition-transform duration-200 ease-out motion-reduce:transition-none"
          style={{ transform: zoom === 1 ? undefined : `scale(${zoom})` }}
        >
          <svg aria-hidden="true" className="pointer-events-none absolute inset-0 size-full overflow-visible">
            <defs>
              <marker id="org-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M0 0L8 4L0 8z" className="fill-faint-foreground" />
              </marker>
              <marker id="org-arrow-hot" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M0 0L8 4L0 8z" className="fill-brand" />
              </marker>
            </defs>
            {links.map((link) => (
              <path
                key={link.key}
                d={link.d}
                markerEnd={`url(#${link.hot ? "org-arrow-hot" : "org-arrow"})`}
                className={cn(
                  "fill-none transition-[stroke,opacity] duration-200",
                  link.hot ? "stroke-brand [stroke-dasharray:5_5] [stroke-width:2]" : "stroke-faint-foreground/60 [stroke-width:1.5]",
                  link.dashed && !link.hot && "[stroke-dasharray:5_5]",
                  tracedUnits && !link.hot && "opacity-30",
                )}
              />
            ))}
          </svg>

          {rows.map((units, row) => (
            <div key={row} data-org-canvas="true" className="flex items-start justify-center gap-12">
              {units.map((unit) => (
                <OrgTeamRegion
                  key={unit.id}
                  unit={unit}
                  agentById={agentById}
                  selection={selection}
                  onSelect={onSelect}
                  trace={trace}
                  dimmed={tracedUnits ? !tracedUnits.has(unit.id) : false}
                  paused={pausedUnits.includes(unit.id)}
                  readOnly={readOnly}
                  onAddMembers={onAddMembers}
                />
              ))}
            </div>
          ))}
          {/* The way to add a team sits where the next one would go, below the
              last row: beside it, it would push the chart under the inspector. */}
          {!readOnly && onAddUnit && (
            <button
              type="button"
              onClick={onAddUnit}
              className={cn(
                "-mt-12 flex items-center gap-1.5 rounded-lg border border-dashed border-surface-border px-4 py-2 text-caption text-muted-foreground transition-colors hover:border-faint-foreground hover:bg-surface-hover hover:text-foreground",
                trace && "opacity-40",
              )}
            >
              <Plus className="size-3.5" aria-hidden="true" />
              {t(($) => $.plan.add_team)}
            </button>
          )}
        </div>
      </DndContext>

      <div className="pointer-events-none sticky bottom-4 left-0 z-10 flex w-full items-end justify-between gap-3 px-4">
        <div className="pointer-events-auto flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-surface-border bg-surface px-3 py-2 text-caption text-muted-foreground shadow-sm">
          <span className="flex items-center gap-2">
            <svg width="24" height="8" aria-hidden="true">
              <path d="M0 4h24" className="stroke-faint-foreground [stroke-width:1.5]" />
            </svg>
            {t(($) => $.plan.legend_reports)}
          </span>
          <span className="flex items-center gap-2">
            <svg width="24" height="8" aria-hidden="true">
              <path d="M0 4h24" className="stroke-faint-foreground [stroke-dasharray:5_5] [stroke-width:1.5]" />
            </svg>
            {t(($) => $.plan.legend_escalates)}
          </span>
          {trace && (
            <span className="flex items-center gap-2 text-foreground">
              <svg width="24" height="8" aria-hidden="true">
                <path d="M0 4h24" className="stroke-brand [stroke-dasharray:5_5] [stroke-width:2]" />
              </svg>
              {t(($) => $.plan.legend_trace)}
            </span>
          )}
        </div>
        <div className="pointer-events-auto flex items-center gap-0.5 rounded-lg border border-surface-border bg-surface p-0.5 shadow-sm">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.plan.zoom_out)}
            disabled={zoomIndex <= 0}
            onClick={() => setZoom(ZOOM_STEPS[Math.max(0, zoomIndex - 1)] ?? 1)}
          >
            <Minus />
          </Button>
          <button
            type="button"
            className="h-7 min-w-12 rounded-md px-1 text-caption tabular-nums text-muted-foreground hover:bg-surface-hover"
            aria-label={t(($) => $.plan.zoom_reset)}
            onClick={() => setZoom(1)}
          >
            {Math.round(zoom * 100)}%
          </button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.plan.zoom_in)}
            disabled={zoomIndex >= ZOOM_STEPS.length - 1}
            onClick={() => setZoom(ZOOM_STEPS[Math.min(ZOOM_STEPS.length - 1, zoomIndex + 1)] ?? 1)}
          >
            <Plus />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.plan.zoom_fit)}
            onClick={() => setZoom(fitZoom(stageRef.current))}
          >
            <Maximize2 />
          </Button>
        </div>
      </div>
    </div>
  );
}

/** The largest step that fits the stage's natural width in its scroll box. */
function fitZoom(stage: HTMLElement | null): number {
  const box = stage?.parentElement;
  if (!stage || !box) return 1;
  const ratio = box.clientWidth / stage.scrollWidth;
  return [...ZOOM_STEPS].reverse().find((step) => step <= ratio) ?? ZOOM_STEPS[0];
}

function OrgTeamRegion({
  unit,
  agentById,
  selection,
  onSelect,
  trace,
  dimmed,
  paused,
  readOnly,
  onAddMembers,
}: {
  unit: OrgUnit;
  agentById: Map<string, Agent>;
  selection: OrgSelection;
  onSelect: (selection: OrgSelection) => void;
  trace?: OrgPlanTrace | null;
  dimmed: boolean;
  paused: boolean;
  readOnly: boolean;
  onAddMembers?: (unitId: string) => void;
}) {
  const { t } = useT("org");
  const [expanded, setExpanded] = useState(false);
  const { setNodeRef, isOver } = useDroppable({ id: unit.id, disabled: readOnly });
  const receives = trace?.unitId === unit.id;
  const escalationStep = trace ? trace.escalation.indexOf(unit.id) : -1;
  const lead: OrgMember | null = unit.owner_id ? { type: "member", id: unit.owner_id } : null;
  // The lead heads the team whether or not they are also listed as a member.
  const others = unit.members.filter((m) => !lead || !sameMember(m, lead));
  const traced = (m: OrgMember) =>
    (trace?.prepares && sameMember(trace.prepares, m)) || (trace?.decides && sameMember(trace.decides, m));
  // Whoever a tested request lands on is always visible, even past the fold.
  const shown = expanded ? others : others.filter((m, i) => i < VISIBLE_PEOPLE || (receives && traced(m)));
  const hidden = others.length - shown.length;
  const total = others.length + (lead ? 1 : 0);
  const step = (m: OrgMember): number | null => {
    if (!receives || !trace) return null;
    if (trace.prepares && sameMember(trace.prepares, m)) return 2;
    if (trace.decides && sameMember(trace.decides, m) && !(trace.prepares && sameMember(trace.prepares, trace.decides)))
      return 3;
    return null;
  };
  const decidesStep = trace?.decides && trace.prepares && !sameMember(trace.prepares, trace.decides) ? 3 : 2;
  const selectedUnit = selection.kind === "unit" && selection.unitId === unit.id;

  return (
    <section
      ref={setNodeRef}
      data-org-unit={unit.id}
      aria-label={t(($) => $.plan.team_label, { name: unit.name })}
      className={cn(
        "relative w-[296px] rounded-xl border border-surface-border bg-app-shell/70 p-3 transition-[opacity,box-shadow,border-color] duration-200",
        selectedUnit && "border-brand/50 ring-3 ring-brand/15",
        receives && "border-brand ring-3 ring-brand/20",
        escalationStep >= 0 && "border-brand/60",
        isOver && "border-brand ring-3 ring-brand/20",
        dimmed && "opacity-40",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect({ kind: "unit", unitId: unit.id })}
        aria-pressed={selectedUnit}
        className="group relative flex w-full items-start gap-2 rounded-lg px-1 pb-2 pt-0.5 text-left"
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-body font-semibold group-hover:underline group-hover:decoration-faint-foreground group-hover:underline-offset-4">
            {unit.name}
          </span>
          {unit.mission && (
            <span className="mt-0.5 line-clamp-2 block text-caption text-muted-foreground">{unit.mission}</span>
          )}
        </span>
        <span className="shrink-0 pt-0.5 text-caption tabular-nums text-faint-foreground">
          {t(($) => $.plan.members, { count: total })}
        </span>
        {receives && <StepBadge n={1} side="left" />}
        {escalationStep >= 0 && <StepBadge n={decidesStep + 1 + escalationStep} side="left" />}
      </button>
      <div className="flex flex-wrap items-center gap-2 px-1 pb-2.5">
        <OrgAutonomyDial autonomy={unit.autonomy} />
        {paused && (
          <span className="rounded-full border border-warning-border bg-warning-subtle px-2 py-px text-micro font-medium text-warning-subtle-foreground">
            {t(($) => $.plan.paused)}
          </span>
        )}
      </div>

      <ul className="flex flex-col gap-1.5">
        {lead ? (
          <OrgPersonCard
            unit={unit}
            member={lead}
            agent={null}
            isLead
            selection={selection}
            onSelect={onSelect}
            step={step(lead)}
            readOnly
          />
        ) : (
          <li className="flex items-start gap-2 rounded-lg border border-dashed border-warning-border bg-warning-subtle/50 px-2.5 py-2 text-caption text-warning-subtle-foreground">
            <TriangleAlert className="mt-px size-3.5 shrink-0" aria-hidden="true" />
            {t(($) => $.plan.no_lead)}
          </li>
        )}
        {shown.map((member) => (
          <OrgPersonCard
            key={personKey(unit.id, member)}
            unit={unit}
            member={member}
            agent={member.type === "agent" ? (agentById.get(member.id) ?? null) : null}
            selection={selection}
            onSelect={onSelect}
            step={step(member)}
            readOnly={readOnly}
          />
        ))}
      </ul>

      <div className="mt-1.5 flex items-center gap-1">
        {hidden > 0 && (
          <button
            type="button"
            onClick={() => setExpanded(true)}
            className="rounded-md px-2 py-1.5 text-caption text-muted-foreground hover:bg-surface-hover hover:text-foreground"
          >
            {t(($) => $.plan.more_people, { count: hidden })}
          </button>
        )}
        {expanded && others.length > VISIBLE_PEOPLE && (
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="rounded-md px-2 py-1.5 text-caption text-muted-foreground hover:bg-surface-hover hover:text-foreground"
          >
            {t(($) => $.plan.fewer_people)}
          </button>
        )}
        {!readOnly && onAddMembers && (
          <button
            type="button"
            onClick={() => onAddMembers(unit.id)}
            className="ml-auto flex items-center gap-1 rounded-md px-2 py-1.5 text-caption text-muted-foreground hover:bg-surface-hover hover:text-foreground"
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t(($) => $.plan.add_people)}
          </button>
        )}
      </div>
    </section>
  );
}

function OrgPersonCard({
  unit,
  member,
  agent,
  isLead = false,
  selection,
  onSelect,
  step,
  readOnly,
}: {
  unit: OrgUnit;
  member: OrgMember;
  agent: Agent | null;
  isLead?: boolean;
  selection: OrgSelection;
  onSelect: (selection: OrgSelection) => void;
  step: number | null;
  readOnly: boolean;
}) {
  const { t } = useT("org");
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: personKey(unit.id, member),
    data: { unitId: unit.id, member },
    disabled: readOnly || isLead,
  });
  const selected =
    selection.kind === "person" && selection.unitId === unit.id && sameMember(selection.member, member);
  const role = unit.roles.find((r) => r.id === member.role_id)?.name;
  const subtitle = isLead
    ? t(($) => $.plan.lead_subtitle)
    : (role ??
      (member.role === "lead"
        ? t(($) => $.plan.receives_first)
        : agent?.description?.trim() || (member.type === "agent" ? t(($) => $.plan.agent) : t(($) => $.plan.human))));

  return (
    <li ref={setNodeRef} className={cn(isDragging && "opacity-50")}>
      <button
        type="button"
        {...listeners}
        {...attributes}
        onClick={() => onSelect({ kind: "person", unitId: unit.id, member })}
        aria-pressed={selected}
        className={cn(
          "relative flex w-full items-center gap-2.5 rounded-lg border border-surface-border bg-surface px-2.5 text-left shadow-xs transition-[box-shadow,border-color] duration-150 hover:shadow-md",
          isLead ? "py-2.5" : "py-2",
          selected && "border-brand/50 ring-3 ring-brand/15",
          step !== null && "border-brand ring-3 ring-brand/20",
        )}
      >
        <ActorAvatar
          actorType={member.type}
          actorId={member.id}
          size="lg"
          showStatusDot={member.type === "agent"}
          profileLink={false}
        />
        <span className="min-w-0 flex-1">
          <PersonName member={member} />
          <span className="block truncate text-micro text-muted-foreground">{subtitle}</span>
        </span>
        <span className="shrink-0 rounded-full border border-surface-border px-1.5 py-px text-micro font-medium text-muted-foreground">
          {isLead ? t(($) => $.plan.lead) : member.type === "agent" ? t(($) => $.plan.agent) : t(($) => $.plan.human)}
        </span>
        {step !== null && <StepBadge n={step} />}
      </button>
    </li>
  );
}

/** The same live directory the avatar reads, so a renamed person or agent is never shown under an old name. */
function PersonName({ member }: { member: OrgMember }) {
  const { getActorName } = useActorName();
  return <span className="block truncate text-label font-medium">{getActorName(member.type, member.id)}</span>;
}

function StepBadge({ n, side = "right" }: { n: number; side?: "left" | "right" }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "absolute -top-2 grid size-5 animate-in place-items-center rounded-full bg-brand text-micro font-bold text-brand-foreground ring-3 ring-page-canvas zoom-in-50 duration-200 motion-reduce:animate-none",
        side === "left" ? "-left-2" : "-right-2",
      )}
    >
      {n}
    </span>
  );
}

interface OrgLink {
  key: string;
  d: string;
  dashed: boolean;
  hot: boolean;
}

/**
 * One line per pair of teams. When escalation follows the reporting line, a
 * second line beside the first would say nothing new, so the pair is drawn
 * once, solid. An escalation that goes somewhere else gets its own dashed
 * line. Consult and backup links change nothing at run time, so they are not
 * drawn; the team's card in the inspector lists them.
 */
function useOrgLinks(
  stageRef: React.RefObject<HTMLDivElement | null>,
  definition: OrgDefinition,
  trace: OrgPlanTrace | null | undefined,
  zoom: number,
): OrgLink[] {
  const [links, setLinks] = useState<OrgLink[]>([]);

  useLayoutEffect(() => {
    const stage = stageRef.current;
    if (!stage) return;
    const hotPairs = new Set<string>();
    if (trace) {
      let from = trace.unitId;
      for (const to of trace.escalation) {
        hotPairs.add(`${from}>${to}`);
        from = to;
      }
    }

    const measure = () => {
      const box = (id: string) => {
        const el = stage.querySelector<HTMLElement>(`[data-org-unit="${CSS.escape(id)}"]`);
        if (!el) return null;
        // offset* ignore the zoom transform, so the lines stay in the stage's
        // own coordinates and scale with it.
        let x = 0;
        let y = 0;
        for (let node: HTMLElement | null = el; node && node !== stage; node = node.offsetParent as HTMLElement | null) {
          x += node.offsetLeft;
          y += node.offsetTop;
        }
        return { x, y, w: el.offsetWidth, h: el.offsetHeight };
      };
      const pairs = new Map<string, Set<string>>();
      for (const edge of definition.edges) {
        if (edge.kind !== "reports_to" && edge.kind !== "escalates_to") continue;
        const key = `${edge.from}>${edge.to}`;
        pairs.set(key, (pairs.get(key) ?? new Set()).add(edge.kind));
      }
      const next: OrgLink[] = [];
      for (const [key, kinds] of pairs) {
        const [from, to] = key.split(">") as [string, string];
        const a = box(from);
        const b = box(to);
        if (!a || !b) continue;
        const x1 = a.x + a.w / 2;
        const y1 = a.y;
        const x2 = b.x + b.w / 2;
        const y2 = b.y + b.h;
        // A child below its parent bends halfway up the gap; one beside or
        // above it (an escalation across the chart) routes over the top.
        const d =
          y1 > y2
            ? `M${x1} ${y1} V${y2 + (y1 - y2) / 2} H${x2} V${y2 + 6}`
            : `M${x1} ${y1} V${Math.min(y1, b.y) - 20} H${x2} V${b.y - 6}`;
        next.push({ key, d, dashed: !kinds.has("reports_to"), hot: hotPairs.has(key) });
      }
      setLinks(next);
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(stage);
    stage.querySelectorAll("[data-org-unit]").forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, [stageRef, definition, trace, zoom]);

  return links;
}
