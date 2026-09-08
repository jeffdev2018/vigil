"use client";

import { capacityFill } from "@multica/core/cycles";
import type { CycleCapacitySide } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

/**
 * One capacity bar. There are always TWO of these on a cycle — people and
 * agents — because the two pools are not interchangeable and a single "team
 * capacity" number hides the case where one of them is the problem.
 *
 * Overflow is shown rather than clipped: the bar fills and turns destructive,
 * and the label says how far over it is. A bar that silently stops at 100%
 * would make an impossible sprint look merely full.
 *
 * With no declared capacity there is no bar at all — only the load. Drawing an
 * empty track would imply a limit nobody set.
 */
export function CapacityBar({
  label,
  side,
  undeclaredLabel,
  valueLabel,
  overLabel,
  tone = "primary",
}: {
  label: string;
  side: CycleCapacitySide;
  undeclaredLabel: string;
  /** Rendered with load/capacity already interpolated. */
  valueLabel: (load: number, capacity: number) => string;
  overLabel: (over: number) => string;
  tone?: "primary" | "accent";
}) {
  const fill = capacityFill(side);
  const over = fill.over;
  const capacity = side.capacity ?? 0;

  return (
    <div className="flex flex-col gap-1" data-testid="capacity-bar" data-side={label}>
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-caption text-muted-foreground">{label}</span>
        <span
          className={cn(
            "font-mono text-caption tabular-nums",
            over ? "text-destructive" : "text-foreground",
          )}
        >
          {fill.declared
            ? valueLabel(side.load, capacity)
            : `${side.load} · ${undeclaredLabel}`}
        </span>
      </div>
      {fill.declared && (
        <div
          className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-label={label}
          aria-valuenow={Math.round(fill.ratio * 100)}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div
            className={cn(
              "h-full rounded-full",
              over ? "bg-destructive" : tone === "accent" ? "bg-chart-3" : "bg-primary",
            )}
            style={{ width: `${fill.ratio * 100}%` }}
          />
        </div>
      )}
      {over && (
        <span className="text-caption text-destructive">
          {overLabel(side.load - capacity)}
        </span>
      )}
    </div>
  );
}
