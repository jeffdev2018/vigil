"use client";

import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useCreateCycle, useUpdateCycle } from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { propertyListOptions } from "@multica/core/properties/queries";
import type { Cycle, CycleWriteRequest } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

const FIELD_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";

export type CycleFormTarget =
  | { mode: "create"; projectId: string | null }
  | { mode: "edit"; cycle: Cycle };

type FormState = {
  project_id: string;
  name: string;
  description: string;
  start_date: string;
  end_date: string;
  human_capacity: string;
  agent_capacity: string;
  load_property_id: string;
  rollover: boolean;
};

function initialForm(target: CycleFormTarget): FormState {
  if (target.mode === "edit") {
    const c = target.cycle;
    return {
      project_id: c.project_id,
      name: c.name,
      description: c.description,
      start_date: c.start_date,
      end_date: c.end_date,
      human_capacity: c.capacity.human.capacity === null ? "" : String(c.capacity.human.capacity),
      agent_capacity: c.capacity.agent.capacity === null ? "" : String(c.capacity.agent.capacity),
      load_property_id: c.load_property_id ?? "",
      rollover: c.rollover,
    };
  }
  return {
    project_id: target.projectId ?? "",
    name: "",
    description: "",
    start_date: "",
    end_date: "",
    human_capacity: "",
    agent_capacity: "",
    load_property_id: "",
    rollover: true,
  };
}

// An empty capacity field means "not declared", which is a real value distinct
// from zero — so it maps to null, never to 0. A negative number is neither:
// it is invalid input, so it is rejected explicitly (capacityInvalid below)
// rather than silently mapped to null, which used to save the field as
// "not declared" instead of surfacing the typo.
function capacityValue(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const n = Number(trimmed);
  return Number.isFinite(n) && n >= 0 ? Math.round(n) : null;
}

function capacityInvalid(raw: string): boolean {
  const trimmed = raw.trim();
  if (!trimmed) return false;
  const n = Number(trimmed);
  return !Number.isFinite(n) || n < 0;
}

function toRequest(f: FormState, mode: CycleFormTarget["mode"]): CycleWriteRequest {
  return {
    ...(mode === "create" ? { project_id: f.project_id } : {}),
    name: f.name.trim(),
    description: f.description,
    start_date: f.start_date,
    end_date: f.end_date,
    human_capacity: capacityValue(f.human_capacity),
    agent_capacity: capacityValue(f.agent_capacity),
    load_property_id: f.load_property_id || null,
    rollover: f.rollover,
  };
}

const errorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

export function CycleFormDialog({
  target,
  onClose,
}: {
  target: CycleFormTarget;
  onClose: () => void;
}) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: properties = [] } = useQuery(propertyListOptions(wsId));
  const createCycle = useCreateCycle(wsId);
  const updateCycle = useUpdateCycle(wsId);
  const [form, setForm] = useState<FormState>(() => initialForm(target));
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((f) => ({ ...f, [key]: value }));
  const pending = createCycle.isPending || updateCycle.isPending;

  // Only number properties can carry a load; anything else would sum to zero.
  const numberProperties = properties.filter((p) => p.type === "number");

  const datesOutOfOrder =
    !!form.start_date && !!form.end_date && form.end_date < form.start_date;
  const humanCapacityInvalid = capacityInvalid(form.human_capacity);
  const agentCapacityInvalid = capacityInvalid(form.agent_capacity);
  const complete =
    !!form.name.trim() && !!form.start_date && !!form.end_date &&
    (target.mode === "edit" || !!form.project_id) && !datesOutOfOrder &&
    !humanCapacityInvalid && !agentCapacityInvalid;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!complete) return;
    const data = toRequest(form, target.mode);
    const opts = {
      onSuccess: () => onClose(),
      onError: (err: unknown) => toast.error(errorMessage(err, t(($) => $.form.error))),
    };
    if (target.mode === "edit") updateCycle.mutate({ id: target.cycle.id, data }, opts);
    else createCycle.mutate(data, opts);
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>
              {target.mode === "edit" ? t(($) => $.form.edit_title) : t(($) => $.form.create_title)}
            </DialogTitle>
            <DialogDescription className="sr-only">
              {t(($) => $.page.empty_description)}
            </DialogDescription>
          </DialogHeader>

          {target.mode === "create" && (
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.project)}
              <Select
                items={[
                  { value: "", label: t(($) => $.form.project_placeholder) },
                  ...projects.map((p) => ({ value: p.id, label: p.title })),
                ]}
                value={form.project_id}
                onValueChange={(value) => value !== null && set("project_id", value)}
                required
              >
                <SelectTrigger className="w-full" aria-label={t(($) => $.form.project)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">{t(($) => $.form.project_placeholder)}</SelectItem>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>{p.title}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          )}

          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.name)}
            <Input
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
              placeholder={t(($) => $.form.name_placeholder)}
              maxLength={80}
              autoFocus
              required
            />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.description)}
            <Textarea
              value={form.description}
              onChange={(e) => set("description", e.target.value)}
              rows={2}
            />
          </label>

          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.start_date)}
              <input
                type="date"
                className={FIELD_CLASS}
                value={form.start_date}
                onChange={(e) => set("start_date", e.target.value)}
                required
              />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.end_date)}
              <input
                type="date"
                className={FIELD_CLASS}
                value={form.end_date}
                onChange={(e) => set("end_date", e.target.value)}
                required
              />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.human_capacity)}
              <Input
                type="number"
                min={0}
                value={form.human_capacity}
                aria-invalid={humanCapacityInvalid}
                onChange={(e) => set("human_capacity", e.target.value)}
              />
              {humanCapacityInvalid && (
                <p role="alert" className="text-destructive">{t(($) => $.form.capacity_negative)}</p>
              )}
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.agent_capacity)}
              <Input
                type="number"
                min={0}
                value={form.agent_capacity}
                aria-invalid={agentCapacityInvalid}
                onChange={(e) => set("agent_capacity", e.target.value)}
              />
              {agentCapacityInvalid && (
                <p role="alert" className="text-destructive">{t(($) => $.form.capacity_negative)}</p>
              )}
            </label>
          </div>
          <p className="text-caption text-muted-foreground">{t(($) => $.form.capacity_hint)}</p>

          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.load_property)}
            <Select
              items={[
                { value: "", label: t(($) => $.form.load_property_none) },
                ...numberProperties.map((p) => ({ value: p.id, label: p.name })),
              ]}
              value={form.load_property_id}
              onValueChange={(value) => value !== null && set("load_property_id", value)}
            >
              <SelectTrigger className="w-full" aria-label={t(($) => $.form.load_property)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">{t(($) => $.form.load_property_none)}</SelectItem>
                {numberProperties.map((p) => (
                  <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span>{t(($) => $.form.load_property_hint)}</span>
          </label>

          <label className="flex items-start gap-2 text-caption text-muted-foreground">
            <Checkbox
              className="mt-0.5"
              checked={form.rollover}
              onCheckedChange={(checked) => set("rollover", checked === true)}
            />
            <span>{t(($) => $.form.rollover)}</span>
          </label>

          {datesOutOfOrder && (
            <p className="text-caption text-destructive">{t(($) => $.form.date_order)}</p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.form.cancel)}
            </Button>
            <Button type="submit" size="sm" disabled={pending || !complete}>
              {target.mode === "edit" ? t(($) => $.form.save) : t(($) => $.form.create)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
