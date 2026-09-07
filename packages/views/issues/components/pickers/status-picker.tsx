"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  effectiveFor,
  effectiveIssueTransitionsOptions,
} from "@multica/core/issue-transitions";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { useT } from "../../../i18n";
import { useStatusLabel } from "../../utils/status-label";
import { useStatusOptions } from "../../utils/status-options";

/** Above this many options the flat list stops being scannable. */
const SEARCH_THRESHOLD = 9;

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  issueId,
}: {
  /**
   * The currently-selected status, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * statuses) — no row is checked. Single-issue callers always pass a concrete
   * status.
   */
  status: IssueStatus | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /**
   * The issue this picker acts on. Optional because the batch toolbar and the
   * create-issue modal have no single issue — those keep every option
   * offerable, and the server is still the authority either way. When present,
   * transition rules (F28) grey the options the viewer may not pick and badge
   * the ones that would go to an approver.
   */
  issueId?: string;
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [query, setQuery] = useState("");
  const { t } = useT("issues");
  // Every StatusPicker call site lives inside the workspace shell (issue
  // detail, table, board batch toolbar, create-issue modal), so the provider
  // is guaranteed here.
  const wsId = useWorkspaceId();
  const { categoryOf, colorOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  /**
   * Offerable statuses as one flat list, in canonical category order.
   *
   * Archived statuses are excluded: archiving retires a status from future
   * assignment while leaving the issues already on it untouched. Falls back to
   * the 7 built-ins until the catalog lands, so a cold render offers exactly
   * what it always did instead of an empty popover. (MUL-6243)
   */
  const allOptions = useStatusOptions(wsId);

  const options = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return allOptions;
    return allOptions.filter((o) => o.label.toLowerCase().includes(q));
  }, [allOptions, query]);

  const searchable = allOptions.length > SEARCH_THRESHOLD;

  // Transition rules (F28). Only fetched when the popover is open and the
  // picker knows its issue — one query per issue, cached by React Query, so
  // reopening the popover costs nothing and a board full of pickers costs
  // nothing at all until one is opened.
  const { data: effective } = useQuery({
    ...effectiveIssueTransitionsOptions(wsId, issueId ?? ""),
    enabled: !!wsId && !!issueId && open,
  });

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        if (!v) setQuery("");
        setOpen(v);
      }}
      width="w-52"
      align={align}
      triggerRender={triggerRender}
      searchable={searchable}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setQuery}
      trigger={
        customTrigger ??
        (status != null ? (
          <>
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              color={colorOf(status)}
              className="h-3.5 w-3.5 shrink-0"
            />
            <span className="truncate">{labelOf(status)}</span>
          </>
        ) : null)
      }
    >
      {options.map((option) => {
        // Defaults to allowed: an unloaded query, a category the server did
        // not answer for, and a workspace with no rules all mean "free move".
        const rule = effectiveFor(effective, option.category);
        const blocked = issueId != null && rule.allowed === false && option.key !== status;
        return (
          <PickerItem
            key={option.key}
            selected={option.key === status}
            disabled={blocked}
            tooltip={blocked ? t(($) => $.transitions.not_allowed) : undefined}
            hoverClassName={STATUS_CONFIG[option.category].hoverBg}
            onClick={() => {
              if (blocked) return;
              onUpdate({ status: option.key });
              setOpen(false);
              setQuery("");
            }}
          >
            <StatusIcon
              status={option.key}
              category={option.category}
              color={option.color}
              className="h-3.5 w-3.5"
            />
            <span className="truncate">{option.label}</span>
            {!blocked && rule.requires_approval && (
              <span className="ml-auto shrink-0 rounded-sm bg-muted px-1 text-caption text-muted-foreground">
                {t(($) => $.transitions.needs_approval)}
              </span>
            )}
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
