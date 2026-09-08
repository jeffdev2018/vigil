"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Maximize2, Minus, Plus, Undo2, User } from "lucide-react";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { orgProblemsForUnit, type OrgProblem } from "@multica/core/org/validate";
import { orgAddTeammate, orgPeople, orgPeopleLayout, type OrgPerson } from "@multica/core/org/people";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { Agent, Goal, MemberWithUser, OrgDefinition, OrgMember } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { AgentPresenceIndicator } from "../../agents/components/agent-presence-indicator";
import { useT } from "../../i18n";
import { OrgProblemList } from "./org-problem-list";
import { OrgUnitSheet } from "./org-unit-sheet";
import { moveMember, patchUnit, removeUnit } from "./org-canvas";

// The people chart: one card per human or agent, one line per "reports to".
// Units, rules and the trust dial stay behind the card — the record opens the
// unit's sheet under "Advanced" for whoever needs them.

const initialsOf = (name: string): string =>
  name.split(" ").map((w) => w[0] ?? "").join("").toUpperCase().slice(0, 2);

const ZOOM_STEP = 0.15;
const ZOOM_MIN = 0.4;
const ZOOM_MAX = 1.6;
const SELECT_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";

export interface OrgPeopleChartProps {
  wsId: string;
  definition: OrgDefinition;
  pausedUnits: string[];
  problems: OrgProblem[];
  members: MemberWithUser[];
  agents: Agent[];
  goals: Goal[];
  readOnly: boolean;
  onChange: (next: OrgDefinition) => void;
  undoDepth: number;
  onUndo: () => void;
  selectedUnitId: string | null;
  onSelectUnit: (id: string | null) => void;
}

export function OrgPeopleChart({
  wsId,
  definition,
  pausedUnits,
  problems,
  members,
  agents,
  goals,
  readOnly,
  onChange,
  undoDepth,
  onUndo,
  selectedUnitId,
  onSelectUnit,
}: OrgPeopleChartProps) {
  const { t } = useT("org");
  const scrollRef = useRef<HTMLDivElement>(null);
  const { byAgent: presence } = useWorkspacePresenceMap(wsId);
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const memberById = useMemo(() => new Map(members.map((m) => [m.user_id, m])), [members]);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const runtimeById = useMemo(() => new Map(runtimes.map((r) => [r.id, r])), [runtimes]);
  const nameOf = (p: Pick<OrgPerson, "type" | "id">): string =>
    (p.type === "agent" ? agentById.get(p.id)?.name : memberById.get(p.id)?.name) ?? p.id;
  const avatarOf = (p: Pick<OrgPerson, "type" | "id">): string | null =>
    (p.type === "agent" ? agentById.get(p.id)?.avatar_url : memberById.get(p.id)?.avatar_url) ?? null;
  /** Where the person runs: the agent's runtime, or plainly "human". */
  const whereOf = (p: OrgPerson): string => {
    if (p.type === "member") return t(($) => $.people.human);
    const runtime = runtimeById.get(agentById.get(p.id)?.runtime_id ?? "");
    return runtime === undefined ? t(($) => $.people.agent) : runtimeDisplayLabel(runtime);
  };

  const people = useMemo(() => orgPeople(definition), [definition]);
  const layout = useMemo(() => orgPeopleLayout(people), [people]);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selected = people.find((p) => p.key === selectedKey) ?? null;
  // The tester points at a unit: land on that unit's lead so the answer shows up here too.
  useEffect(() => {
    if (selectedUnitId === null || selected?.unitId === selectedUnitId) return;
    const lead = people.find((p) => p.unitId === selectedUnitId && p.lead) ?? people.find((p) => p.unitId === selectedUnitId);
    if (lead !== undefined) setSelectedKey(lead.key);
  }, [selectedUnitId, selected?.unitId, people]);
  const select = (p: OrgPerson | null) => {
    setSelectedKey(p?.key ?? null);
    onSelectUnit(p?.unitId ?? null);
  };

  const [zoom, setZoom] = useState(1);
  const fit = () => {
    const box = scrollRef.current;
    // Before the box has a width (first paint, jsdom) there is nothing to fit to.
    if (box === null || box.clientWidth === 0 || layout.width === 0) return;
    setZoom(Math.min(1, Math.max(ZOOM_MIN, (box.clientWidth - 24) / layout.width)));
  };
  // Fit once the chart has a size; a later edit keeps the zoom the user chose.
  const fitted = useRef(false);
  useEffect(() => {
    if (fitted.current || layout.width === 0) return;
    fitted.current = true;
    fit();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once, when the first layout lands
  }, [layout.width]);

  const [adding, setAdding] = useState(false);
  const unit = selected === null ? undefined : definition.units.find((u) => u.id === selected.unitId);
  const reports = selected === null ? [] : people.filter((p) => p.reportsTo === selected.key);
  const manager = selected?.reportsTo === null || selected === null ? null : people.find((p) => p.key === selected.reportsTo) ?? null;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {!readOnly && (
          <Button type="button" size="sm" className="gap-1" onClick={() => setAdding(true)}>
            <Plus className="size-3.5" />
            {t(($) => $.people.add)}
          </Button>
        )}
        {!readOnly && (
          <Button type="button" size="sm" variant="ghost" className="gap-1" disabled={undoDepth === 0} onClick={onUndo}>
            <Undo2 className="size-3.5" />
            {t(($) => $.canvas.undo, { count: undoDepth })}
          </Button>
        )}
        <span className="text-caption text-muted-foreground">{t(($) => $.people.hint)}</span>
      </div>

      <div className="grid gap-3 min-[820px]:grid-cols-[1fr_20rem]">
        <div className="relative">
          <div
            ref={scrollRef}
            data-testid="org-people-chart"
            className="min-h-[24rem] overflow-auto rounded-lg border bg-muted/30 p-3"
          >
            {people.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.people.empty)}</p>
            ) : (
              <div className="relative origin-top-left" style={{ width: layout.width * zoom, height: layout.height * zoom }}>
                <div className="absolute left-0 top-0 origin-top-left" style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
                  <svg aria-hidden className="pointer-events-none absolute inset-0 text-border" width={layout.width} height={layout.height}>
                    {layout.edges.map((e) => (
                      <path
                        key={e.to}
                        data-testid="org-person-edge"
                        d={e.d}
                        fill="none"
                        stroke="currentColor"
                        strokeWidth={1.5}
                        className={cn(selectedKey !== null && (e.from === selectedKey || e.to === selectedKey) && "text-primary")}
                      />
                    ))}
                  </svg>
                  {layout.nodes.map((n) => {
                    const p = people.find((x) => x.key === n.key);
                    if (p === undefined) return null;
                    const name = nameOf(p);
                    const detail = p.type === "agent" ? presence.get(p.id) : undefined;
                    const working = detail?.workload === "working";
                    return (
                      <button
                        key={n.key}
                        type="button"
                        data-testid="org-person"
                        data-person-key={p.key}
                        data-unit-id={p.unitId}
                        aria-pressed={selectedKey === p.key}
                        onClick={() => select(selectedKey === p.key ? null : p)}
                        className={cn(
                          "absolute flex items-center gap-3 rounded-xl border bg-card px-3 text-left shadow-sm transition-[box-shadow,border-color] hover:border-foreground/30 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                          selectedKey === p.key && "border-primary ring-1 ring-primary",
                          pausedUnits.includes(p.unitId) && "opacity-60",
                        )}
                        style={{ left: n.x, top: n.y, width: n.width, height: n.height }}
                      >
                        <ActorAvatar name={name} initials={initialsOf(name)} avatarUrl={avatarOf(p)} isAgent={p.type === "agent"} size="lg" />
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate text-body font-semibold">{name}</span>
                          <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
                            {p.type === "agent" ? <AgentPresenceIndicator detail={detail ?? null} compact /> : <User className="size-3 shrink-0" />}
                            <span className="truncate">{p.title} · {whereOf(p)}</span>
                          </span>
                        </span>
                        {working && (
                          <span className="absolute -top-2.5 right-3 rounded-full border border-success/30 bg-success/10 px-2 text-[11px] font-medium text-success">
                            {t(($) => $.people.active)}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
          <div className="absolute right-3 top-3 flex flex-col gap-1">
            <Button type="button" size="icon-sm" variant="outline" aria-label={t(($) => $.people.zoom_in)} onClick={() => setZoom((z) => Math.min(ZOOM_MAX, z + ZOOM_STEP))}>
              <Plus className="size-3.5" />
            </Button>
            <Button type="button" size="icon-sm" variant="outline" aria-label={t(($) => $.people.zoom_out)} onClick={() => setZoom((z) => Math.max(ZOOM_MIN, z - ZOOM_STEP))}>
              <Minus className="size-3.5" />
            </Button>
            <Button type="button" size="icon-sm" variant="outline" aria-label={t(($) => $.people.fit)} onClick={fit}>
              <Maximize2 className="size-3.5" />
            </Button>
          </div>
        </div>

        {selected === null || unit === undefined ? (
          <div className="flex flex-col gap-2 rounded-lg border p-3">
            <p className="text-caption text-muted-foreground">{t(($) => $.people.no_selection)}</p>
            <OrgProblemList problems={problems} />
          </div>
        ) : (
          <div className="flex flex-col gap-3 rounded-lg border p-3" data-testid="org-person-sheet">
            <div className="flex items-center gap-3">
              <ActorAvatar name={nameOf(selected)} initials={initialsOf(nameOf(selected))} avatarUrl={avatarOf(selected)} isAgent={selected.type === "agent"} size="lg" />
              <div className="min-w-0">
                <p className="truncate text-body font-semibold">{nameOf(selected)}</p>
                <p className="truncate text-caption text-muted-foreground">{selected.title} · {whereOf(selected)}</p>
              </div>
            </div>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-caption">
              <dt className="text-muted-foreground">{t(($) => $.people.reports_to)}</dt>
              <dd>
                {manager === null ? (
                  t(($) => $.people.root)
                ) : (
                  <button type="button" className="underline-offset-2 hover:underline" onClick={() => select(manager)}>{nameOf(manager)}</button>
                )}
              </dd>
              <dt className="text-muted-foreground">{t(($) => $.people.team)}</dt>
              <dd className="flex flex-wrap gap-x-2">
                {reports.length === 0
                  ? t(($) => $.people.team_none)
                  : reports.map((r) => (
                      <button key={r.key} type="button" className="underline-offset-2 hover:underline" onClick={() => select(r)}>{nameOf(r)}</button>
                    ))}
              </dd>
              <dt className="text-muted-foreground">{t(($) => $.people.unit)}</dt>
              <dd>{unit.name}</dd>
            </dl>
            <OrgProblemList problems={orgProblemsForUnit(problems, unit.id)} />
            <details className="group">
              <summary className="cursor-pointer text-caption text-muted-foreground">{t(($) => $.people.advanced)}</summary>
              <div className="mt-2">
                <OrgUnitSheet
                  unit={unit}
                  units={definition.units}
                  problems={orgProblemsForUnit(problems, unit.id)}
                  members={members}
                  goals={goals}
                  actorName={(type, id) => nameOf({ type, id })}
                  readOnly={readOnly}
                  onPatch={(patch) => onChange(patchUnit(definition, unit.id, patch))}
                  onMoveMember={(index, toUnitId) => onChange(moveMember(definition, unit.id, index, toUnitId))}
                  onDelete={() => {
                    onChange(removeUnit(definition, unit.id));
                    select(null);
                  }}
                  onClose={() => select(null)}
                />
              </div>
            </details>
          </div>
        )}
      </div>

      {adding && (
        <AddTeammateDialog
          people={people}
          members={members}
          agents={agents}
          nameOf={nameOf}
          defaultManager={selected?.key ?? people.find((p) => p.reportsTo === null)?.key ?? null}
          onClose={() => setAdding(false)}
          onAdd={(managerKey, member, unitName) => {
            const next = orgAddTeammate(definition, managerKey, member, unitName);
            onChange(next);
            setAdding(false);
            const added = orgPeople(next).find((p) => p.type === member.type && p.id === member.id && (managerKey === null || p.reportsTo === managerKey));
            if (added !== undefined) select(added);
          }}
        />
      )}
    </div>
  );
}

function AddTeammateDialog({
  people,
  members,
  agents,
  nameOf,
  defaultManager,
  onClose,
  onAdd,
}: {
  people: OrgPerson[];
  members: MemberWithUser[];
  agents: Agent[];
  nameOf: (p: Pick<OrgPerson, "type" | "id">) => string;
  defaultManager: string | null;
  onClose: () => void;
  onAdd: (managerKey: string | null, member: OrgMember, unitName: string) => void;
}) {
  const { t } = useT("org");
  const [kind, setKind] = useState<OrgMember["type"]>("agent");
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [manager, setManager] = useState(defaultManager ?? "");
  const choices = kind === "agent" ? agents.map((a) => ({ id: a.id, name: a.name })) : members.map((m) => ({ id: m.user_id, name: m.name }));
  const chosen = choices.find((c) => c.id === id);
  const submit = () => {
    if (chosen === undefined) return;
    const role = title.trim();
    onAdd(manager === "" ? null : manager, { type: kind, id: chosen.id, ...(role ? { role } : {}) }, role || chosen.name);
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.people.add)}</DialogTitle>
          <DialogDescription>{t(($) => $.people.dialog_description)}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="flex gap-1" role="radiogroup">
            {(["agent", "member"] as const).map((k) => (
              <Button key={k} type="button" size="sm" variant={kind === k ? "default" : "outline"} role="radio" aria-checked={kind === k} onClick={() => { setKind(k); setId(""); }}>
                {t(($) => (k === "agent" ? $.people.kind_agent : $.people.kind_human))}
              </Button>
            ))}
          </div>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.people.who)}
            <select className={SELECT_CLASS} value={id} onChange={(e) => setId(e.target.value)}>
              <option value="">{t(($) => $.people.pick)}</option>
              {choices.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.people.title)}
            <Input value={title} placeholder={t(($) => $.people.title_placeholder)} onChange={(e) => setTitle(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.people.reports_to)}
            <select className={SELECT_CLASS} value={manager} onChange={(e) => setManager(e.target.value)}>
              <option value="">{t(($) => $.people.manager_none)}</option>
              {people.map((p) => (
                <option key={p.key} value={p.key}>{nameOf(p)} · {p.title}</option>
              ))}
            </select>
          </label>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>{t(($) => $.people.cancel)}</Button>
          <Button type="button" disabled={chosen === undefined} onClick={submit}>{t(($) => $.people.confirm)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
