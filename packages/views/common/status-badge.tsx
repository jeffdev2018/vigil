"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { humanizeIdentifier } from "@multica/core/utils";

/**
 * Shared status color language (JEF-402). `eval-lab-tab`, `code-health-tab`,
 * `doc-drift-tab` and `integrations-tab` each redefined their own
 * status -> color/variant mapping; this is the single place that decision
 * lives now. Tone owns the color, `StatusBadge` owns turning it into a
 * `Badge`: destructive gets the filled destructive variant so errors stay
 * loud, every other tone rides the outline variant tinted with its token.
 */
export type StatusTone = "success" | "warning" | "info" | "destructive" | "muted";

const TONE_CLASS: Record<StatusTone, string> = {
  success: "text-success",
  warning: "text-warning",
  info: "text-info",
  destructive: "text-destructive",
  muted: "text-muted-foreground",
};

export type StatusBadgeConfig = Record<string, { tone: StatusTone; label: string }>;

export function StatusBadge({
  status,
  config,
  className,
  ...props
}: {
  status: string;
  config: StatusBadgeConfig;
} & Omit<React.ComponentProps<typeof Badge>, "variant" | "children">) {
  const entry = config[status];
  // Unknown status (server enum drift): humanize the raw value instead of a
  // translated label nobody wrote for it, and never claim success/failure.
  const tone = entry?.tone ?? "muted";
  const label = entry?.label ?? humanizeIdentifier(status);
  return (
    <Badge
      variant={tone === "destructive" ? "destructive" : "outline"}
      className={cn(TONE_CLASS[tone], className)}
      data-status={status}
      {...props}
    >
      {label}
    </Badge>
  );
}
