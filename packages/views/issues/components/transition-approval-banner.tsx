"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  issueTransitionRequestsOptions,
  pendingTransitionRequest,
  useDecideIssueTransitionRequest,
} from "@multica/core/issue-transitions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Transition rules (F28): a status change was requested and is held until an
 * approver decides. Renders nothing when nothing is pending.
 *
 * Approve/Reject are shown only to owners and admins. The rule's own
 * approver_roles can widen that, but the request does not carry them and
 * fetching the rule for a banner is a query nobody needs — the server refuses
 * a caller who may not decide, so a widened approver simply uses the inbox
 * item instead. The narrower affordance is the safe way round: showing a
 * button that 403s is worse than not showing one.
 */
export function TransitionApprovalBanner({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data } = useQuery(issueTransitionRequestsOptions(wsId, issueId));
  const pending = pendingTransitionRequest(data);
  const decide = useDecideIssueTransitionRequest(wsId, issueId);
  const [note, setNote] = useState("");

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const canDecide = useMemo(() => {
    if (!currentUser) return false;
    const role = members.find((m) => m.user_id === currentUser.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, currentUser]);

  if (!pending) return null;

  const run = (decision: "approve" | "reject") => {
    decide.mutate(
      { requestId: pending.id, decision, note: note.trim() || undefined },
      {
        onSuccess: () => setNote(""),
        onError: () => toast.error(t(($) => $.transitions.decide_failed)),
      },
    );
  };

  return (
    <div
      data-testid="transition-approval-banner"
      className="flex flex-col gap-1.5 rounded-md border border-warning/60 bg-warning/10 p-2 text-caption"
    >
      <div className="flex items-center gap-2 font-medium">
        <ShieldCheck className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <span>{t(($) => $.transitions.banner_title)}</span>
      </div>
      <p className="text-muted-foreground">
        {t(($) => $.transitions.banner_body, { status: pending.to_status })}
      </p>
      {canDecide && (
        <div className="flex flex-col gap-1.5 sm:flex-row sm:items-center">
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t(($) => $.transitions.note_placeholder)}
            className="h-7 flex-1 text-caption"
          />
          <div className="flex gap-1.5">
            <Button size="sm" disabled={decide.isPending} onClick={() => run("approve")}>
              {t(($) => $.transitions.approve)}
            </Button>
            <Button size="sm" variant="ghost" disabled={decide.isPending} onClick={() => run("reject")}>
              {t(($) => $.transitions.reject)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
