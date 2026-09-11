"use client";

import { useMemo, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  useCreateCalendarEvent,
  useUpdateCalendarEvent,
  calendarSlotsOptions,
  utcISOToZonedWallClock,
  zonedWallClockToUtcISO,
} from "@multica/core/calendar-events";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { TimezoneSelect } from "../../common/timezone-select";
import type {
  CalendarEventEntry,
  CalendarEventInput,
  CalendarEventParticipantInput,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { X } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../../issues/components/pickers/property-picker";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT } from "../../i18n";

const FIELD_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";

export type CalendarEventDialogTarget =
  | { mode: "create"; defaultStart?: string; defaultEnd?: string }
  | { mode: "edit"; event: CalendarEventEntry };

interface FormState {
  title: string;
  description: string;
  startsAt: string; // datetime-local wall clock, in `timezone`
  endsAt: string;
  allDay: boolean;
  timezone: string;
  location: string;
  issueId: string;
  /** Display label for the linked issue (identifier); empty when none. */
  issueLabel: string;
  participants: CalendarEventParticipantInput[];
}

function roundedNowPlus(minutes: number): Date {
  const d = new Date(Date.now() + minutes * 60_000);
  d.setSeconds(0, 0);
  return d;
}

function initialForm(target: CalendarEventDialogTarget, tz: string): FormState {
  if (target.mode === "edit") {
    const e = target.event;
    return {
      title: e.title,
      description: e.description,
      startsAt: utcISOToZonedWallClock(e.starts_at, e.timezone || tz),
      endsAt: utcISOToZonedWallClock(e.ends_at, e.timezone || tz),
      allDay: e.all_day,
      timezone: e.timezone || tz,
      location: e.location,
      issueId: e.issue_id ?? "",
      issueLabel: e.issue_identifier || e.issue_id || "",
      participants: e.participants
        .filter((p) => p.type === "member" || p.type === "agent")
        .map((p) => ({ type: p.type as "member" | "agent", id: p.id, required: p.required })),
    };
  }
  const start = target.defaultStart
    ? new Date(target.defaultStart)
    : roundedNowPlus(30);
  const end = target.defaultEnd
    ? new Date(target.defaultEnd)
    : new Date(start.getTime() + 30 * 60_000);
  return {
    title: "",
    description: "",
    startsAt: utcISOToZonedWallClock(start.toISOString(), tz),
    endsAt: utcISOToZonedWallClock(end.toISOString(), tz),
    allDay: false,
    timezone: tz,
    location: "",
    issueId: "",
    issueLabel: "",
    participants: [],
  };
}

function toRequest(f: FormState): CalendarEventInput {
  return {
    title: f.title.trim(),
    description: f.description,
    starts_at: zonedWallClockToUtcISO(f.startsAt, f.timezone),
    ends_at: zonedWallClockToUtcISO(f.endsAt, f.timezone),
    all_day: f.allDay,
    timezone: f.timezone,
    location: f.location,
    issue_id: f.issueId.trim() || undefined,
    participants: f.participants,
  };
}

const errorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

/** Multi-select participants (members + agents). Draft mode only — the
 *  dialog holds the selection until the event is created/saved. */
function ParticipantsPicker({
  wsId,
  selected,
  onChange,
}: {
  wsId: string;
  selected: CalendarEventParticipantInput[];
  onChange: (next: CalendarEventParticipantInput[]) => void;
}) {
  const { t } = useT("calendar-events");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const selectedSet = useMemo(
    () => new Set(selected.map((p) => `${p.type}:${p.id}`)),
    [selected],
  );
  const query = filter.trim().toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(query));
  const filteredAgents = agents.filter(
    (a) => !a.archived_at && a.name.toLowerCase().includes(query),
  );

  const toggle = (type: "member" | "agent", id: string) => {
    const key = `${type}:${id}`;
    onChange(
      selectedSet.has(key)
        ? selected.filter((p) => `${p.type}:${p.id}` !== key)
        : [...selected, { type, id, required: true }],
    );
  };

  const nameOf = (type: "member" | "agent", id: string) =>
    type === "member"
      ? (members.find((m) => m.user_id === id)?.name ?? id)
      : (agents.find((a) => a.id === id)?.name ?? id);

  return (
    <div className="flex flex-col gap-1.5">
      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selected.map((p) => (
            <span
              key={`${p.type}:${p.id}`}
              className="flex items-center gap-1 rounded-full border bg-muted/40 py-0.5 pl-1 pr-1.5 text-caption"
            >
              <ActorAvatar actorType={p.type} actorId={p.id} size="sm" />
              <span className="max-w-32 truncate">{nameOf(p.type, p.id)}</span>
              <button
                type="button"
                aria-label={t(($) => $.form.cancel)}
                className="text-muted-foreground hover:text-foreground"
                onClick={() => toggle(p.type, p.id)}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      <PropertyPicker
        open={open}
        onOpenChange={(v: boolean) => {
          setOpen(v);
          if (!v) setFilter("");
        }}
        width="w-64"
        align="start"
        searchable
        searchPlaceholder={t(($) => $.form.add_participants)}
        onSearchChange={setFilter}
        trigger={
          <span className="text-muted-foreground">{t(($) => $.form.add_participants)}</span>
        }
      >
        {filteredMembers.length > 0 && (
          <PickerSection label={t(($) => $.form.members_group)}>
            {filteredMembers.map((m) => (
              <PickerItem
                key={m.user_id}
                selected={selectedSet.has(`member:${m.user_id}`)}
                onClick={() => toggle("member", m.user_id)}
              >
                <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
                <span className="truncate">{m.name}</span>
              </PickerItem>
            ))}
          </PickerSection>
        )}
        {filteredAgents.length > 0 && (
          <PickerSection label={t(($) => $.form.agents_group)}>
            {filteredAgents.map((a) => (
              <PickerItem
                key={a.id}
                selected={selectedSet.has(`agent:${a.id}`)}
                onClick={() => toggle("agent", a.id)}
              >
                <ActorAvatar actorType="agent" actorId={a.id} size="sm" />
                <span className="truncate">{a.name}</span>
              </PickerItem>
            ))}
          </PickerSection>
        )}
        {filteredMembers.length === 0 && filteredAgents.length === 0 && <PickerEmpty />}
      </PropertyPicker>
    </div>
  );
}

/** Looks up free windows for the selected participants and offers a
 *  one-click fill of start/end. Enabled only once someone is picked. */
function FindSlotPanel({
  wsId,
  participants,
  tz,
  durationMinutes,
  onPick,
}: {
  wsId: string;
  participants: CalendarEventParticipantInput[];
  tz: string;
  durationMinutes: number;
  onPick: (startIso: string, endIso: string) => void;
}) {
  const { t } = useT("calendar-events");
  const [active, setActive] = useState(false);
  const participantsParam = participants.map((p) => `${p.type}:${p.id}`).join(",");
  const from = useMemo(() => new Date().toISOString(), []);
  const to = useMemo(() => new Date(Date.now() + 14 * 24 * 60 * 60_000).toISOString(), []);
  const { data, isFetching } = useQuery({
    ...calendarSlotsOptions(wsId, participantsParam, durationMinutes, from, to, tz),
    enabled: active && participantsParam.length > 0,
  });

  if (!active) {
    return (
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={participants.length === 0}
        onClick={() => setActive(true)}
      >
        {t(($) => $.form.find_slot)}
      </Button>
    );
  }
  if (participants.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.form.find_slot_pick_participants)}</p>;
  }
  if (isFetching) {
    return (
      <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <Spinner className="size-3.5" /> {t(($) => $.form.find_slot_searching)}
      </p>
    );
  }
  const slots = data?.slots ?? [];
  if (slots.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.form.find_slot_empty)}</p>;
  }
  return (
    <div className="flex flex-wrap gap-1.5">
      {slots.map((s) => (
        <Button
          key={s.starts_at}
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-caption"
          onClick={() => {
            onPick(s.starts_at, s.ends_at);
            setActive(false);
          }}
        >
          {new Date(s.starts_at).toLocaleString(undefined, {
            timeZone: tz,
            month: "short",
            day: "numeric",
            hour: "numeric",
            minute: "2-digit",
          })}
        </Button>
      ))}
    </div>
  );
}

export function CalendarEventDialog({
  target,
  onClose,
}: {
  target: CalendarEventDialogTarget;
  onClose: () => void;
}) {
  const { t } = useT("calendar-events");
  const wsId = useWorkspaceId();
  const viewingTz = useViewingTimezone();
  const [form, setForm] = useState<FormState>(() => initialForm(target, viewingTz));
  const [pickingIssue, setPickingIssue] = useState(false);
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const createEvent = useCreateCalendarEvent(wsId);
  const updateEvent = useUpdateCalendarEvent(wsId);
  const pending = createEvent.isPending || updateEvent.isPending;

  const durationMinutes = useMemo(() => {
    const start = zonedWallClockToUtcISO(form.startsAt, form.timezone);
    const end = zonedWallClockToUtcISO(form.endsAt, form.timezone);
    const ms = Date.parse(end) - Date.parse(start);
    return ms > 0 ? Math.round(ms / 60_000) : 30;
  }, [form.startsAt, form.endsAt, form.timezone]);

  const datesOutOfOrder =
    !!form.startsAt && !!form.endsAt &&
    zonedWallClockToUtcISO(form.endsAt, form.timezone) <= zonedWallClockToUtcISO(form.startsAt, form.timezone);
  const complete = !!form.title.trim() && !!form.startsAt && !!form.endsAt && !datesOutOfOrder;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!complete) return;
    const data = toRequest(form);
    const opts = {
      onSuccess: () => onClose(),
      onError: (err: unknown) => toast.error(errorMessage(err, t(($) => $.form.error))),
    };
    if (target.mode === "edit") updateEvent.mutate({ id: target.event.id, data }, opts);
    else createEvent.mutate(data, opts);
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pickingIssue) onClose(); }}>
      <DialogContent
        // Base UI has no outside-press handlers (those are Radix props); the
        // dialog is controlled and onOpenChange above ignores a close while
        // an issue is being picked, which keeps it open through the picker.
        className="sm:max-w-lg overflow-y-auto max-h-[90vh]"
      >
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>
              {target.mode === "edit" ? t(($) => $.form.edit_title) : t(($) => $.form.create_title)}
            </DialogTitle>
            <DialogDescription className="sr-only">
              {t(($) => $.form.create_title)}
            </DialogDescription>
          </DialogHeader>

          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.title)}
            <Input
              value={form.title}
              onChange={(e) => set("title", e.target.value)}
              placeholder={t(($) => $.form.title_placeholder)}
              maxLength={300}
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

          <label className="flex items-center gap-2 text-caption text-muted-foreground">
            <Checkbox
              checked={form.allDay}
              onCheckedChange={(checked) => set("allDay", checked === true)}
            />
            <span>{t(($) => $.form.all_day)}</span>
          </label>

          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.starts_at)}
              <input
                type={form.allDay ? "date" : "datetime-local"}
                className={FIELD_CLASS}
                value={form.allDay ? form.startsAt.slice(0, 10) : form.startsAt}
                onChange={(e) => set("startsAt", form.allDay ? `${e.target.value}T00:00` : e.target.value)}
                required
              />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.ends_at)}
              <input
                type={form.allDay ? "date" : "datetime-local"}
                className={FIELD_CLASS}
                value={form.allDay ? form.endsAt.slice(0, 10) : form.endsAt}
                onChange={(e) => set("endsAt", form.allDay ? `${e.target.value}T00:00` : e.target.value)}
                required
              />
            </label>
          </div>

          {!form.allDay && (
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.timezone)}
              <TimezoneSelect
                value={form.timezone}
                onValueChange={(tz) => set("timezone", tz)}
                browserSuffix=""
              />
            </label>
          )}

          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.location)}
            <Input
              value={form.location}
              onChange={(e) => set("location", e.target.value)}
              placeholder={t(($) => $.form.location_placeholder)}
            />
          </label>

          <div className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.issue)}
            {form.issueId ? (
              <div className="flex h-8 items-center gap-2 rounded-md border bg-background px-2">
                <span className="min-w-0 flex-1 truncate text-body text-foreground">
                  {form.issueLabel || form.issueId}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 shrink-0 px-1.5 text-caption"
                  onClick={() => setPickingIssue(true)}
                >
                  {t(($) => $.form.issue_change)}
                </Button>
                <button
                  type="button"
                  className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                  aria-label={t(($) => $.form.issue_clear)}
                  onClick={() => setForm((f) => ({ ...f, issueId: "", issueLabel: "" }))}
                >
                  <X className="size-3.5" />
                </button>
              </div>
            ) : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 justify-start font-normal text-muted-foreground"
                onClick={() => setPickingIssue(true)}
              >
                {t(($) => $.form.issue_pick)}
              </Button>
            )}
          </div>

          <div className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.participants)}
            <ParticipantsPicker
              wsId={wsId}
              selected={form.participants}
              onChange={(next) => set("participants", next)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <FindSlotPanel
              wsId={wsId}
              participants={form.participants}
              tz={form.timezone}
              durationMinutes={durationMinutes}
              onPick={(startIso, endIso) => {
                set("startsAt", utcISOToZonedWallClock(startIso, form.timezone));
                set("endsAt", utcISOToZonedWallClock(endIso, form.timezone));
              }}
            />
          </div>

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

        <IssuePickerModal
          open={pickingIssue}
          onOpenChange={setPickingIssue}
          title={t(($) => $.form.issue_picker_title)}
          description={t(($) => $.form.issue_picker_description)}
          excludeIds={form.issueId ? [form.issueId] : []}
          onSelect={(issue) => {
            setForm((f) => ({
              ...f,
              issueId: issue.id,
              issueLabel: issue.identifier || issue.title,
            }));
            setPickingIssue(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
