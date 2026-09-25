"use client";

import { Server } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { InfoBanner } from "../../common/info-banner";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";

export function RuntimeRequiredBanner({
  agentId,
  agentName,
}: {
  agentId: string;
  agentName?: string;
}) {
  const { t } = useT("chat");
  const paths = useWorkspacePaths();
  const name = agentName?.trim() || t(($) => $.runtime_required_banner.fallback_name);

  return (
    <div className={cn(CHAT_GUTTER, "mb-1.5")}>
      <InfoBanner
        role="warning"
        icon={Server}
        compact
        className={CHAT_COLUMN}
        action={
          <Button
            variant="outline"
            size="sm"
            className="h-6 shrink-0 bg-background/70 text-caption"
            render={
              <AppLink href={`${paths.agentDetail(agentId)}?view=general`} />
            }
            nativeButton={false}
          >
            {t(($) => $.runtime_required_banner.action)}
          </Button>
        }
      >
        {t(($) => $.runtime_required_banner.message, { name })}
      </InfoBanner>
    </div>
  );
}
