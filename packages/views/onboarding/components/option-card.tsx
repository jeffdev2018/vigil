"use client";

import { cn } from "@multica/ui/lib/utils";

/**
 * Radio marker for the onboarding questionnaire rows: thin ring resting,
 * filled dot once selected.
 *
 * The `OptionCard` / `OtherOptionCard` rows that used to live here were
 * superseded by `icon-option-card.tsx`, which every questionnaire step now
 * uses. Only the marker outlived them — `step-workspace.tsx` draws its own
 * rows and reuses it so the selected state reads identically across steps.
 */
export function RadioMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "relative inline-block h-4 w-4 shrink-0 rounded-full border-[1.5px] transition-colors",
        selected ? "border-foreground" : "border-border",
      )}
    >
      {selected && (
        <span className="absolute inset-[3px] rounded-full bg-foreground" />
      )}
    </span>
  );
}
