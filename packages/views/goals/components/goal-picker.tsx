"use client";

import { useMemo, useState } from "react";
import { Target } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { goalListOptions } from "@multica/core/goals";
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
import { flattenGoalTree } from "./goal-tree";

/**
 * Goal picker for an issue (K74). Lists the tree root-first, indented by
 * depth, plus the fixed empty row: a null goal means the issue inherits
 * whatever its project serves. Members set this; an agent only proposes
 * through a decision.
 */
export function GoalPicker({
  goalId,
  onUpdate,
  triggerRender,
  align = "start",
  disabled = false,
}: {
  goalId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  disabled?: boolean;
}) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const { data: goals = [] } = useQuery(goalListOptions(wsId));
  const current = goals.find((g) => g.id === goalId);
  const [filter, setFilter] = useState("");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = disabled ? false : internalOpen;
  const setOpen = disabled ? () => {} : setInternalOpen;

  const query = filter.trim().toLowerCase();
  const rows = useMemo(() => flattenGoalTree(goals), [goals]);
  // A search flattens the indentation: matches are shown as a plain list.
  const filtered = query
    ? rows.filter(({ goal }) => goal.title.toLowerCase().includes(query) || matchesPinyin(goal.title, query))
    : rows;

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
        triggerRender={triggerRender ?? <button type="button" disabled={disabled} className={PICKER_TRIGGER_CLASS} />}
        trigger={
          <>
            <Target className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{current ? current.title : t(($) => $.picker.no_goal)}</span>
          </>
        }
      >
        <PickerItem
          emptyValue
          selected={!goalId}
          onClick={() => {
            onUpdate({ goal_id: null });
            setOpen(false);
          }}
        >
          <Target className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.picker.no_goal)}</span>
        </PickerItem>

        {filtered.map(({ goal, depth }) => (
          <PickerItem
            key={goal.id}
            selected={goal.id === goalId}
            onClick={() => {
              onUpdate({ goal_id: goal.id });
              setOpen(false);
            }}
          >
            <span className="truncate" style={{ paddingLeft: query ? 0 : `${depth * 0.75}rem` }}>{goal.title}</span>
          </PickerItem>
        ))}

        {goals.length === 0 && (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.picker.empty)}</div>
        )}
        {goals.length > 0 && filtered.length === 0 && query && <PickerEmpty />}
      </PropertyPicker>
    </div>
  );
}
