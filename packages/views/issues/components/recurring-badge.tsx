"use client";

import { Repeat } from "lucide-react";
import { useT } from "../../i18n";

/**
 * Marks an issue that belongs to a recurrence series (source or occurrence)
 * on list rows and board cards. The detail's Recurrence block carries the
 * rule itself; this is only the glance.
 */
export function RecurringBadge({ className }: { className?: string }) {
  const { t } = useT("issues");
  const label = t(($) => $.recurrence.badge);
  return (
    <span
      data-testid="recurring-badge"
      title={label}
      aria-label={label}
      className={
        "inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground " +
        (className ?? "")
      }
    >
      <Repeat className="size-3" aria-hidden />
      <span className="hidden lg:inline">{label}</span>
    </span>
  );
}
