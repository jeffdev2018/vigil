"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Swords } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { contestCostUsd, contestPreflightOptions, useCreateContest, type ContestTargetType } from "@multica/core/issues/contest";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const ROUNDS = ["1", "2"] as const;

/**
 * Contest (K72): opens a dialog that names the challenger, the cost and the
 * daily quota, then launches a rival-model challenge on one output.
 * `variant="icon"` matches the row-action icon buttons of the execution log.
 */
export function ContestButton({ targetType, targetId, size = "sm", variant = "button", className }: { targetType: ContestTargetType; targetId: string; size?: "xs" | "sm"; variant?: "button" | "icon"; className?: string }) {
  const { t } = useT("contests");
  const [open, setOpen] = useState(false);
  const label = t(($) => $.button);
  const onClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setOpen(true);
  };
  return (
    <>
      {variant === "icon" ? (
        <Tooltip>
          <TooltipTrigger render={<button type="button" data-testid="contest-button" aria-label={label} className={cn("rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground", className)} onClick={onClick}><Swords aria-hidden className="size-3.5" /></button>} />
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ) : (
        <Button type="button" data-testid="contest-button" variant="outline" size={size} className={className} onClick={onClick}>
          <Swords aria-hidden className="size-3.5" />
          {label}
        </Button>
      )}
      {open && <ContestDialog targetType={targetType} targetId={targetId} open={open} onOpenChange={setOpen} />}
    </>
  );
}

function ContestDialog({ targetType, targetId, open, onOpenChange }: { targetType: ContestTargetType; targetId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useT("contests");
  const wsId = useWorkspaceId();
  const [rounds, setRounds] = useState<(typeof ROUNDS)[number]>("1");
  const { data: pre, isError } = useQuery(contestPreflightOptions(wsId, targetType, targetId, open));
  const create = useCreateContest(wsId);
  const quotaReached = !!pre && pre.quota_limit > 0 && pre.quota_used >= pre.quota_limit;
  const launch = () =>
    create.mutate(
      { target_type: targetType, target_id: targetId, max_rounds: pre?.challenger.kind === "llm" ? 1 : Number(rounds) },
      {
        onSuccess: () => {
          toast.success(t(($) => $.dialog.launched));
          onOpenChange(false);
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.dialog.launch_failed)),
      },
    );
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="contest-dialog" className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="inline-flex items-center gap-2">
            <Swords aria-hidden className="size-4 text-muted-foreground" />
            {t(($) => $.dialog.title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.dialog.description)}</DialogDescription>
        </DialogHeader>
        {isError ? (
          <p className="text-caption text-destructive">{t(($) => $.dialog.preflight_failed)}</p>
        ) : !pre ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.dialog.loading)}</p>
        ) : (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.dialog.challenger)}</dt>
            <dd data-testid="contest-challenger" className="flex flex-wrap items-center gap-1.5">
              <span className="font-medium">{pre.challenger.name || pre.challenger.provider}</span>
              <span className="text-muted-foreground">{pre.challenger.provider}</span>
              {pre.challenger.kind === "llm" && <span className="rounded bg-muted px-1 text-muted-foreground">{t(($) => $.dialog.service_model)}</span>}
              {pre.challenger.same_vendor && <span className="rounded bg-warning/20 px-1 text-warning">{t(($) => $.dialog.same_vendor)}</span>}
            </dd>
            <dt className="text-muted-foreground">{t(($) => $.dialog.cost)}</dt>
            <dd data-testid="contest-cost">{pre.estimated_cost_usd_ticks > 0 ? `$${contestCostUsd(pre.estimated_cost_usd_ticks)}` : "—"}</dd>
            <dt className="text-muted-foreground">{t(($) => $.dialog.quota)}</dt>
            <dd data-testid="contest-quota" className={cn(quotaReached && "text-destructive")}>{t(($) => $.dialog.quota_value, { used: pre.quota_used, limit: pre.quota_limit })}</dd>
            <dt className="text-muted-foreground">{t(($) => $.dialog.existing)}</dt>
            <dd data-testid="contest-existing">{pre.existing}</dd>
            {pre.challenger.kind !== "llm" && (
              <>
                <dt className="self-center text-muted-foreground">{t(($) => $.dialog.rounds)}</dt>
                <dd>
                  <Select items={ROUNDS.map((value) => ({ value, label: value === "1" ? t(($) => $.dialog.round_1) : t(($) => $.dialog.round_2) }))} value={rounds} onValueChange={(v) => v && setRounds(v)}>
                    <SelectTrigger size="sm" aria-label={t(($) => $.dialog.rounds)} className="w-32"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {ROUNDS.map((value) => <SelectItem key={value} value={value}>{value === "1" ? t(($) => $.dialog.round_1) : t(($) => $.dialog.round_2)}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </dd>
              </>
            )}
          </dl>
        )}
        {quotaReached && <p data-testid="contest-quota-reached" className="text-caption text-destructive">{t(($) => $.dialog.quota_reached)}</p>}
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" onClick={() => onOpenChange(false)}>{t(($) => $.dialog.cancel)}</Button>
          <Button type="button" size="sm" disabled={!pre || quotaReached || create.isPending} onClick={launch}>{t(($) => $.dialog.launch)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
