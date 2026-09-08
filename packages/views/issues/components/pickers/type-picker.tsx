"use client";

import { useMemo, useState } from "react";
import { CircleSlash } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueTypes } from "@multica/core/issue-types/hooks";
import type { UpdateIssueRequest } from "@multica/core/types";
import { PropertyIconGlyph } from "../../../common/property-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { useT } from "../../../i18n";

/** Above this many options the flat list stops being scannable. Same threshold
 *  as the status picker, for the same reason. */
const SEARCH_THRESHOLD = 9;

/**
 * The work item type badge + picker (F30 / JEF-34).
 *
 * `null` means UNTYPED, which is a real value and not "loading": the list
 * therefore offers an explicit "No type" row rather than expecting the user to
 * discover that re-clicking the current row clears it.
 *
 * Archived types are excluded from the OFFERED list but still render as the
 * trigger when the issue carries one — archiving retires a type from future
 * assignment and leaves the issues already on it alone, so hiding the label
 * would turn a classified issue into an unexplained blank.
 */
export function TypePicker({
  issueType,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  /** The current type key, or `null` for untyped / a mixed batch selection. */
  issueType: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [query, setQuery] = useState("");
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { activeTypes, entryOf, labelOf, colorOf } = useIssueTypes(wsId);

  const options = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return activeTypes;
    return activeTypes.filter((entry) => entry.name.toLowerCase().includes(q));
  }, [activeTypes, query]);

  const current = entryOf(issueType);
  const searchable = activeTypes.length > SEARCH_THRESHOLD;

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
      searchPlaceholder={t(($) => $.issue_types.search)}
      onSearchChange={setQuery}
      trigger={
        customTrigger ??
        (issueType ? (
          <>
            <TypeGlyph icon={current?.icon} color={colorOf(issueType)} />
            <span className="truncate">{labelOf(issueType)}</span>
          </>
        ) : (
          <span className="truncate text-muted-foreground">
            {t(($) => $.issue_types.none)}
          </span>
        ))
      }
    >
      {/* Clearing is a first-class row, not a re-click of the selected one:
          untyped is the default state of every issue and has to be reachable
          the same way any other value is. */}
      <PickerItem
        selected={issueType === null}
        onClick={() => {
          onUpdate({ issue_type: null });
          setOpen(false);
          setQuery("");
        }}
      >
        <CircleSlash className="size-3.5 text-muted-foreground" />
        <span className="truncate text-muted-foreground">
          {t(($) => $.issue_types.none)}
        </span>
      </PickerItem>
      {options.map((entry) => (
        <PickerItem
          key={entry.key}
          selected={entry.key === issueType}
          onClick={() => {
            onUpdate({ issue_type: entry.key });
            setOpen(false);
            setQuery("");
          }}
        >
          <TypeGlyph icon={entry.icon} color={entry.color} />
          <span className="truncate">{entry.name}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}

/**
 * A type's glyph. Falls back to a coloured dot when the type carries no icon —
 * every type has a colour, none is required to have an icon, and a row with an
 * empty gutter reads as a broken image.
 */
export function TypeGlyph({
  icon,
  color,
  className,
}: {
  icon?: string | null;
  color?: string | null;
  className?: string;
}) {
  if (icon) {
    return (
      <PropertyIconGlyph
        icon={icon}
        className={className ?? "size-3.5 shrink-0"}
        style={color ? { color } : undefined}
      />
    );
  }
  return (
    <span
      aria-hidden
      className={className ?? "size-2 shrink-0 rounded-full"}
      style={{ backgroundColor: color ?? "var(--color-muted-foreground)" }}
    />
  );
}
