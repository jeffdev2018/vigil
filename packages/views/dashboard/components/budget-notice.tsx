"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ShieldAlert } from "lucide-react";
import { budgetStatusOptions } from "@multica/core/budgets";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function BudgetNotice({ onOpen }: { onOpen: () => void }) {
  const wsId = useWorkspaceId();
  const { t } = useT("settings");
  const { data = [] } = useQuery(budgetStatusOptions(wsId));
  const blocked = data.find(
    (status) =>
      status.policy.action === "enforce" &&
      status.reached &&
      !status.override_expires_at,
  );
  const warning = data.find((status) => {
    const total = status.spent_usd_ticks + status.reserved_usd_ticks;
    return total * 10_000 >= status.policy.limit_usd_ticks * status.policy.warn_bps;
  });
  const status = blocked ?? warning;
  if (!status) return null;
  const Icon = blocked ? ShieldAlert : AlertTriangle;
  return (
    <div
      role="status"
      className="mx-4 mt-4 flex items-center gap-3 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 sm:mx-6"
    >
      <Icon className="h-5 w-5 shrink-0 text-warning-foreground" />
      <p className="min-w-0 flex-1 text-sm">
        {blocked
          ? t(($) => $.budgets.notice_blocked)
          : t(($) => $.budgets.notice_warning)}
      </p>
      <Button size="sm" variant="outline" onClick={onOpen}>
        {t(($) => $.budgets.manage)}
      </Button>
    </div>
  );
}
