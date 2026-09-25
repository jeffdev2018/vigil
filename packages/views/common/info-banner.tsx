import type { ComponentType, ReactNode } from "react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@multica/ui/components/ui/alert";
import { cn } from "@multica/ui/lib/utils";

export type InfoBannerRole = "warning" | "success" | "info" | "danger";

/**
 * Shared inline notice for a semantic state (warning/success/info/danger).
 * Wraps the design system's `Alert` tinted-role variants so call sites stop
 * hand-picking a Tailwind color pair (bg-amber-50/text-amber-900/ring-amber-200
 * and friends) per file — the pairing, and its measured AA contrast, lives in
 * `tokens.css` once.
 *
 * `compact` renders a single row (icon, text, action) for space-constrained
 * spots like a banner above a chat input. The default renders a title +
 * description block, matching the underlying `Alert`'s grid layout.
 */
export function InfoBanner({
  role,
  title,
  children,
  icon: Icon,
  action,
  compact = false,
  className,
}: {
  role: InfoBannerRole;
  title?: ReactNode;
  children: ReactNode;
  icon?: ComponentType<{ className?: string }>;
  action?: ReactNode;
  compact?: boolean;
  className?: string;
}) {
  return (
    <Alert
      variant={role}
      className={cn(
        "flex items-start gap-2",
        compact && "items-center gap-1.5 px-2.5 py-1.5 text-caption",
        className,
      )}
    >
      {Icon && (
        <Icon
          className={cn("shrink-0", compact ? "size-3.5" : "mt-0.5 size-4")}
        />
      )}
      <div className="min-w-0 flex-1">
        {title && (
          <AlertTitle className={compact ? "text-caption" : undefined}>
            {title}
          </AlertTitle>
        )}
        <AlertDescription
          className={compact ? "min-w-0 truncate text-caption" : undefined}
        >
          {children}
        </AlertDescription>
      </div>
      {action && (
        <div className="ml-auto flex shrink-0 items-center">{action}</div>
      )}
    </Alert>
  );
}
