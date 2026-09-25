"use client";

import { ORG_AUTONOMY_ORDER } from "@multica/core/org/validate";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { orgAutonomyHint, orgAutonomyLabel } from "../labels";

/**
 * How far a team may act without a person, as four notches. It is drawn as a
 * gauge and never as a pill: a pill beside a team reads as its status, and the
 * old "Draft" badge was taken for "this team is a draft".
 */
export function OrgAutonomyDial({ autonomy, className }: { autonomy: string; className?: string }) {
  const { t } = useT("org");
  // An autonomy this client does not know yet shows an empty gauge rather
  // than guessing a level.
  const level = ORG_AUTONOMY_ORDER.indexOf(autonomy as (typeof ORG_AUTONOMY_ORDER)[number]) + 1;
  const label = orgAutonomyLabel(t, autonomy);
  const hint = orgAutonomyHint(t, autonomy);
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)} title={hint || undefined}>
      <span
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={ORG_AUTONOMY_ORDER.length}
        aria-valuenow={level}
        className="inline-flex gap-0.5"
      >
        {ORG_AUTONOMY_ORDER.map((notch, i) => (
          <span
            key={notch}
            className={cn("h-1 w-2.5 rounded-full", i < level ? "bg-foreground" : "bg-surface-border")}
          />
        ))}
      </span>
      <span className="text-caption text-muted-foreground">{label}</span>
    </span>
  );
}
