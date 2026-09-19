"use client";

import type { ReactNode } from "react";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useT } from "../i18n";

/**
 * Shown while the server cannot be reached (auth status `recovering`, or a
 * workspace list whose first load failed). The auth initializer keeps retrying
 * in the background; this screen tells the user why nothing loads and lets
 * them retry now. It never signs anyone out.
 */
export function AuthRecoveryPage({
  onRetry,
  isRetrying = false,
  topSlot,
}: {
  onRetry?: () => void;
  isRetrying?: boolean;
  /** Platform chrome above the content, e.g. the desktop drag strip. */
  topSlot?: ReactNode;
}) {
  const { t } = useT("auth");
  const retryAuthentication = useAuthStore(
    (state) => state.retryAuthentication,
  );

  return (
    <div className="flex h-svh flex-col">
      {topSlot}
      <div className="flex flex-1 items-center justify-center p-8">
        <div className="flex max-w-sm flex-col items-center text-center">
          <MulticaIcon bordered size="lg" />
          <h1 className="mt-6 text-title font-semibold">
            {t(($) => $.recovery.title)}
          </h1>
          <p className="mt-2 text-body text-muted-foreground">
            {t(($) => $.recovery.description)}
          </p>
          <Button
            className="mt-6"
            disabled={isRetrying}
            onClick={onRetry ?? retryAuthentication}
          >
            {isRetrying
              ? t(($) => $.recovery.retrying)
              : t(($) => $.recovery.retry)}
          </Button>
        </div>
      </div>
    </div>
  );
}
