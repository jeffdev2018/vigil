"use client";

import { WifiOff } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";

/**
 * A detail page whose read got no answer (offline, 5xx): the resource may
 * exist, so it says the page could not load and offers a retry — never the
 * page's "does not exist" state. Branch with `isResourceMissingError` from
 * `@multica/core/api/load-error`.
 */
export function LoadErrorState({ onRetry }: { onRetry: () => void }) {
  const { t } = useT("common");
  return (
    <div
      role="alert"
      className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 px-6 py-16 text-center"
    >
      <WifiOff aria-hidden="true" className="size-8 text-muted-foreground" />
      <div>
        <p className="text-body font-medium">{t(($) => $.load_error.title)}</p>
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.load_error.description)}
        </p>
      </div>
      <Button type="button" variant="outline" size="sm" onClick={onRetry}>
        {t(($) => $.load_error.retry)}
      </Button>
    </div>
  );
}
