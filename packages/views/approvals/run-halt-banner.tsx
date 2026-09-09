"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldOff } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { runHaltOptions, useSetRunHalt } from "@multica/core/run-halt";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../i18n";

/**
 * Fleet halt (K05 / m169): every gated action is refused while a workspace
 * is halted. Shown at the top of the issues list and the issue detail page
 * — the two surfaces where a refusal would otherwise look like the product
 * silently breaking rather than a switch someone threw on purpose. Renders
 * nothing while the workspace is not halted.
 */
export function RunHaltBanner({ wsId }: { wsId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { data: halt } = useQuery(runHaltOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { getMemberName } = useActorName();
  const currentUser = useAuthStore((s) => s.user);
  const setRunHalt = useSetRunHalt(wsId);

  const canLift = useMemo(() => {
    if (!currentUser) return false;
    const role = members.find((m) => m.user_id === currentUser.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, currentUser]);

  if (!halt?.halted) return null;

  const lift = () => {
    setRunHalt.mutate(
      { halted: false, reason: "" },
      { onError: () => toast.error(t(($) => $.approvals.run_halt_lift_failed)) },
    );
  };

  return (
    <div
      data-testid="run-halt-banner"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-destructive/40 bg-destructive/10 px-4 py-2 text-caption"
    >
      <div className="flex items-center gap-1.5 font-medium text-destructive">
        <ShieldOff className="size-3.5 shrink-0" aria-hidden="true" />
        {t(($) => $.approvals.run_halt_title)}
      </div>
      <span className="text-muted-foreground">
        {t(($) => $.approvals.run_halt_by, {
          who: halt.halted_by ? getMemberName(halt.halted_by) : t(($) => $.approvals.run_halt_unknown_actor),
          when: halt.halted_at ? timeAgo(halt.halted_at) : "",
        })}
        {halt.reason ? ` · ${t(($) => $.approvals.run_halt_reason_prefix, { reason: halt.reason })}` : ""}
      </span>
      {canLift ? (
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          disabled={setRunHalt.isPending}
          onClick={lift}
        >
          {t(($) => $.approvals.run_halt_lift)}
        </Button>
      ) : null}
    </div>
  );
}
