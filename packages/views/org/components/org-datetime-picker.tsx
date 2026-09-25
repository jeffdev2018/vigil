"use client";

import { useState } from "react";
import { CalendarClock } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Calendar } from "@multica/ui/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { useT } from "../../i18n";

const DEFAULT_TIME = "18:00";

/**
 * A day and a time, as an RFC 3339 string. Used for a task force's end date
 * in the wizard and in the organization's settings, instead of the native
 * `datetime-local`, which renders in the browser's locale rather than the app's.
 */
export function OrgDateTimePicker({
  value,
  onChange,
  placeholder,
}: {
  value: string | null;
  onChange: (iso: string) => void;
  placeholder: string;
}) {
  const { i18n } = useT("org");
  const [open, setOpen] = useState(false);
  const current = value ? new Date(value) : undefined;
  const valid = current && !Number.isNaN(current.getTime()) ? current : undefined;
  const time = valid
    ? `${String(valid.getHours()).padStart(2, "0")}:${String(valid.getMinutes()).padStart(2, "0")}`
    : DEFAULT_TIME;

  const combine = (day: Date, hhmm: string) => {
    const [h, m] = hhmm.split(":").map(Number);
    const next = new Date(day);
    next.setHours(h ?? 18, m ?? 0, 0, 0);
    return next.toISOString();
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button type="button" variant="outline" size="sm" className="w-fit justify-start gap-2" />}>
        <CalendarClock className="size-3.5" />
        {valid
          ? new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" }).format(valid)
          : placeholder}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={valid}
          onSelect={(day) => {
            if (day) onChange(combine(day, time));
          }}
        />
        <div className="flex items-center justify-end border-t p-2">
          <TimeInput value={time} onChange={(v) => onChange(combine(valid ?? new Date(), v))} disabled={!valid} />
        </div>
      </PopoverContent>
    </Popover>
  );
}
