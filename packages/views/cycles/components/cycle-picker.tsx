"use client";

import { useMemo, useState } from "react";
import { CalendarRange } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cycleListOptions } from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";

/**
 * Cycle picker for an issue (F29).
 *
 * Scoped to the issue's OWN project, because that is the only set the server
 * accepts — a cycle of another project is refused with 409. An issue with no
 * project has no cycles to offer, and the picker says why instead of showing
 * an empty list that looks like a loading state.
 *
 * Closed cycles are offered too: a planner correcting where finished work
 * belonged is a real move, and hiding them would make it impossible.
 */
export function CyclePicker({
  cycleId,
  projectId,
  onUpdate,
  triggerRender,
  trigger,
  open: controlledOpen,
  onOpenChange,
  align = "start",
  disabled = false,
}: {
  cycleId: string | null;
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  /** Overrides the default icon + current-cycle label (batch toolbar). */
  trigger?: React.ReactNode;
  /** Controlled open state, for toolbars that coordinate several pickers. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: "start" | "center" | "end";
  disabled?: boolean;
}) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const { data: cycles = [] } = useQuery({
    ...cycleListOptions(wsId, projectId ?? undefined),
    enabled: !!projectId,
  });
  const current = cycles.find((c) => c.id === cycleId);
  const [filter, setFilter] = useState("");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = disabled ? false : (controlledOpen ?? internalOpen);
  const setOpen = disabled
    ? () => {}
    : (next: boolean) => {
        setInternalOpen(next);
        onOpenChange?.(next);
      };

  const query = filter.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      query
        ? cycles.filter(
            (c) => c.name.toLowerCase().includes(query) || matchesPinyin(c.name, query),
          )
        : cycles,
    [cycles, query],
  );

  return (
    <div className="inline-flex min-w-0">
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        width="w-60"
        align={align}
        searchable
        searchPlaceholder={t(($) => $.picker.search_placeholder)}
        onSearchChange={setFilter}
        triggerRender={
          triggerRender ?? <button type="button" disabled={disabled} className={PICKER_TRIGGER_CLASS} />
        }
        trigger={
          trigger ?? (
            <>
              <CalendarRange className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{current ? current.name : t(($) => $.picker.none)}</span>
            </>
          )
        }
      >
        <PickerItem
          emptyValue
          selected={!cycleId}
          onClick={() => {
            onUpdate({ cycle_id: null });
            setOpen(false);
          }}
        >
          <CalendarRange className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.picker.none)}</span>
        </PickerItem>

        {filtered.map((cycle) => (
          <PickerItem
            key={cycle.id}
            selected={cycle.id === cycleId}
            onClick={() => {
              onUpdate({ cycle_id: cycle.id });
              setOpen(false);
            }}
          >
            <span className="truncate">{cycle.name}</span>
            <span className="ml-auto shrink-0 text-caption text-muted-foreground">
              {t(($) => $.status[cycle.status])}
            </span>
          </PickerItem>
        ))}

        {!projectId && (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">
            {t(($) => $.picker.needs_project)}
          </div>
        )}
        {projectId && cycles.length === 0 && (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
        {cycles.length > 0 && filtered.length === 0 && query && <PickerEmpty />}
      </PropertyPicker>
    </div>
  );
}
