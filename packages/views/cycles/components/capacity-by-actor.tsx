"use client";

import { useMemo, useState } from "react";
import { Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { cycleCapacitiesOptions, usePutCycleCapacities } from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { CycleActorCapacity, CycleActorType } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

const errorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

/** Points stay a string while editing so the input can be empty mid-edit. */
interface Row {
  actor_type: CycleActorType;
  actor_id: string;
  name: string;
  points: string;
}

const rowKey = (r: Pick<Row, "actor_type" | "actor_id">) => `${r.actor_type}:${r.actor_id}`;

const MAX_POINTS = 10000;

function parsePoints(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const n = Number(trimmed);
  return Number.isInteger(n) && n >= 0 && n <= MAX_POINTS ? n : null;
}

/**
 * Declared capacity per actor (JEF-246): who has committed how many points to
 * this cycle. The pool bars above say whether the cycle fits people vs agents;
 * this list says which person or agent each point belongs to. Saved as one
 * full-replace PUT, so the editor works on a local copy until Save.
 */
export function CapacityByActor({ cycleId }: { cycleId: string }) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const { data: capacities, isPending } = useQuery(cycleCapacitiesOptions(wsId, cycleId));

  return (
    <section className="flex flex-col gap-2" aria-label={t(($) => $.actors.title)}>
      <h2 className="text-caption font-medium text-muted-foreground">
        {t(($) => $.actors.title)}
      </h2>
      {isPending || !capacities ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.detail.loading)}</p>
      ) : (
        // Keying on the cycle resets the draft when the page switches cycles.
        <CapacityByActorEditor key={cycleId} cycleId={cycleId} initial={capacities} />
      )}
    </section>
  );
}

function CapacityByActorEditor({
  cycleId,
  initial,
}: {
  cycleId: string;
  initial: CycleActorCapacity[];
}) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const putCapacities = usePutCycleCapacities(wsId, cycleId);
  const [rows, setRows] = useState<Row[]>(() =>
    initial.map((c) => ({
      actor_type: c.actor_type,
      actor_id: c.actor_id,
      name: c.name,
      points: String(c.points),
    })),
  );
  const [dirty, setDirty] = useState(false);

  // Members key on user_id, agents on their own id — the same ids the issue
  // assignee pair uses, so a declared capacity lines up with done work.
  const options = useMemo(() => {
    const taken = new Set(rows.map(rowKey));
    const memberOptions = members
      .filter((m) => !taken.has(`member:${m.user_id}`))
      .map((m) => ({ value: `member:${m.user_id}`, label: m.name }));
    const agentOptions = agents
      .filter((a) => !a.archived_at && !taken.has(`agent:${a.id}`))
      .map((a) => ({ value: `agent:${a.id}`, label: a.name }));
    return { memberOptions, agentOptions };
  }, [members, agents, rows]);

  const patchRow = (index: number, patch: Partial<Row>) => {
    setRows((rs) => rs.map((r, i) => (i === index ? { ...r, ...patch } : r)));
    setDirty(true);
  };

  const addRow = () => {
    setRows((rs) => [...rs, { actor_type: "member", actor_id: "", name: "", points: "" }]);
    setDirty(true);
  };

  const removeRow = (index: number) => {
    setRows((rs) => rs.filter((_, i) => i !== index));
    setDirty(true);
  };

  const pickActor = (index: number, value: string) => {
    const [type, id] = value.split(":") as [CycleActorType, string];
    const name =
      type === "member"
        ? (members.find((m) => m.user_id === id)?.name ?? "")
        : (agents.find((a) => a.id === id)?.name ?? "");
    patchRow(index, { actor_type: type, actor_id: id, name });
  };

  const invalid =
    rows.some((r) => !r.actor_id) || rows.some((r) => parsePoints(r.points) === null);

  const save = () => {
    if (invalid) return;
    putCapacities.mutate(
      rows.map((r) => ({
        actor_type: r.actor_type,
        actor_id: r.actor_id,
        points: parsePoints(r.points) ?? 0,
      })),
      {
        onSuccess: () => setDirty(false),
        onError: (err) => toast.error(errorMessage(err, t(($) => $.actors.error))),
      },
    );
  };

  const optionLabel = (value: string) =>
    [...options.memberOptions, ...options.agentOptions].find((o) => o.value === value)?.label;

  return (
    <div className="flex flex-col gap-2">
      {rows.length === 0 && (
        <div className="flex flex-col gap-0.5" data-testid="actor-capacity-empty">
          <p className="text-caption text-muted-foreground">{t(($) => $.actors.empty)}</p>
          <p className="text-caption text-faint-foreground">{t(($) => $.actors.empty_hint)}</p>
        </div>
      )}

      {rows.map((row, index) => {
        const value = row.actor_id ? rowKey(row) : "";
        return (
          <div key={rowKey(row) + index} className="flex items-center gap-2" data-testid="actor-capacity-row">
            <Select
              items={[
                { value: "", label: t(($) => $.actors.actor_placeholder) },
                ...options.memberOptions,
                ...options.agentOptions,
                // The row's own actor is filtered out of `options` once taken;
                // keep it selectable for this row.
                ...(value && !optionLabel(value) ? [{ value, label: row.name }] : []),
              ]}
              value={value}
              onValueChange={(v) => v !== null && v !== "" && pickActor(index, v)}
            >
              <SelectTrigger size="sm" className="min-w-0 flex-1" aria-label={t(($) => $.actors.actor_label)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="" disabled>
                  {t(($) => $.actors.actor_placeholder)}
                </SelectItem>
                {options.memberOptions.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>{t(($) => $.detail.human)}</SelectLabel>
                    {options.memberOptions.map((o) => (
                      <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                    ))}
                  </SelectGroup>
                )}
                {options.agentOptions.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>{t(($) => $.detail.agent)}</SelectLabel>
                    {options.agentOptions.map((o) => (
                      <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                    ))}
                  </SelectGroup>
                )}
                {value && !optionLabel(value) && (
                  <SelectItem value={value}>{row.name}</SelectItem>
                )}
              </SelectContent>
            </Select>
            <Input
              type="number"
              min={0}
              max={MAX_POINTS}
              step={1}
              className="w-20"
              aria-label={t(($) => $.actors.points_label)}
              placeholder="0"
              value={row.points}
              onChange={(e) => patchRow(index, { points: e.target.value })}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-7 shrink-0"
              aria-label={t(($) => $.actors.remove)}
              onClick={() => removeRow(index)}
            >
              <X className="size-3.5" />
            </Button>
          </div>
        );
      })}

      {invalid && rows.some((r) => r.actor_id) && (
        <p className="text-caption text-destructive">{t(($) => $.actors.points_invalid)}</p>
      )}

      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={addRow}>
          <Plus className="size-3.5 mr-1.5" />
          {t(($) => $.actors.add)}
        </Button>
        {dirty && (
          <Button
            type="button"
            size="sm"
            disabled={invalid || putCapacities.isPending}
            onClick={save}
          >
            {t(($) => $.actors.save)}
          </Button>
        )}
      </div>
    </div>
  );
}
