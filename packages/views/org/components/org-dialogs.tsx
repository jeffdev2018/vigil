"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  orgDefinitionChanges,
  orgPreflightOptions,
  useDeleteOrgStructure,
  useSetOrgStructureStatus,
} from "@multica/core/org";
import { contestCostUsd } from "@multica/core/issues/contest";
import { useWorkspaceId } from "@multica/core/hooks";
import type { OrgDefinition, OrgStructure } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { orgActivationRequirementText, orgModelDescription } from "../labels";

export const orgErrorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

/**
 * Activation shows what the structure will cost to run before it runs, and
 * asks the person to attest they tested it: the attestation is stored with the
 * revision, so it is the record of who switched it on and why.
 */
export function OrgActivateDialog({ structure, onClose }: { structure: OrgStructure; onClose: () => void }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: pre, isError, refetch } = useQuery(orgPreflightOptions(wsId, structure.id, true));
  const setStatus = useSetOrgStructureStatus(wsId);
  const [attestation, setAttestation] = useState("");
  const submit = () =>
    setStatus.mutate(
      { id: structure.id, action: "activate", eval_attestation: attestation.trim() },
      { onSuccess: onClose, onError: (e) => toast.error(orgErrorMessage(e, t(($) => $.actions.error))) },
    );
  return (
    <Dialog open onOpenChange={(open) => !open && !setStatus.isPending && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.activate.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.activate.description)}</DialogDescription>
        </DialogHeader>
        {isError ? (
          <div role="alert" className="flex items-center justify-between gap-3 text-caption">
            {t(($) => $.form.error)}
            <Button size="sm" variant="outline" onClick={() => void refetch()}>
              {t(($) => $.catalog.retry)}
            </Button>
          </div>
        ) : !pre ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.activate.loading)}</p>
        ) : (
          <dl data-testid="org-preflight" className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-caption">
            <dt className="text-muted-foreground">{t(($) => $.activate.pattern)}</dt>
            <dd>{orgModelDescription(t, pre.model) || pre.pattern}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.runs)}</dt>
            <dd className="tabular-nums">{pre.coordination_runs_per_issue}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.cost)}</dt>
            <dd className="tabular-nums">{`$${contestCostUsd(pre.coordination_cost_usd_ticks_per_issue)}`}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.review_seconds)}</dt>
            <dd>{t(($) => $.activate.seconds, { count: pre.human_review_seconds_per_issue })}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.units)}</dt>
            <dd className="tabular-nums">{pre.units}</dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.units_without_owner)}</dt>
            <dd className={cn("tabular-nums", pre.units_without_owner > 0 && "text-warning-strong")}>
              {pre.units_without_owner}
            </dd>
            <dt className="text-muted-foreground">{t(($) => $.activate.agents)}</dt>
            <dd className="tabular-nums">{pre.agents}</dd>
            {pre.activation_requirements.length > 0 && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.activate.requirements)}</dt>
                <dd>
                  <ul className="list-disc space-y-0.5 pl-4 text-warning-strong">
                    {pre.activation_requirements.map((text, i) => (
                      <li key={text}>
                        {orgActivationRequirementText(t, text, pre.activation_requirement_codes?.[i])}
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            )}
          </dl>
        )}
        <label className="flex flex-col gap-1.5 text-caption text-muted-foreground">
          {t(($) => $.activate.attestation)}
          <Textarea
            value={attestation}
            onChange={(e) => setAttestation(e.target.value)}
            rows={3}
            placeholder={t(($) => $.activate.attestation_placeholder, { date: new Date().toLocaleDateString() })}
          />
        </label>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button
            size="sm"
            disabled={setStatus.isPending || !pre || isError || !attestation.trim()}
            onClick={submit}
          >
            {t(($) => $.activate.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export type OrgReasonAction = "pause" | "dissolve" | "delete";

/** Pausing and dissolving record a reason; deleting (only a draft or a dissolved structure) does not. */
export function OrgReasonDialog({
  structure,
  action,
  onClose,
  onDone,
}: {
  structure: OrgStructure;
  action: OrgReasonAction;
  onClose: () => void;
  onDone?: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const setStatus = useSetOrgStructureStatus(wsId);
  const del = useDeleteOrgStructure(wsId);
  const [reason, setReason] = useState("");
  const pending = setStatus.isPending || del.isPending;
  const opts = {
    onSuccess: () => {
      onClose();
      onDone?.();
    },
    onError: (e: unknown) => toast.error(orgErrorMessage(e, t(($) => $.actions.error))),
  };
  const submit = () => {
    if (action === "delete") del.mutate(structure.id, opts);
    else setStatus.mutate({ id: structure.id, action, reason: reason.trim() }, opts);
  };
  return (
    <Dialog open onOpenChange={(open) => !open && !pending && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $[action].title)}</DialogTitle>
          <DialogDescription>{t(($) => $[action].description)}</DialogDescription>
        </DialogHeader>
        {action !== "delete" && (
          <label className="flex flex-col gap-1.5 text-caption text-muted-foreground">
            {t(($) => $.actions.reason)}
            <Input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t(($) => $.actions.reason_placeholder)}
            />
          </label>
        )}
        <DialogFooter>
          <Button variant="outline" size="sm" disabled={pending} onClick={onClose}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button variant={action === "pause" ? "default" : "destructive"} size="sm" disabled={pending} onClick={submit}>
            {t(($) => $[action].submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Publishing a live structure changes how work is routed from the next issue
 * on, so the person sees which teams change before it happens.
 */
export function OrgPublishDialog({
  before,
  after,
  pending,
  onCancel,
  onConfirm,
}: {
  before: OrgDefinition;
  after: OrgDefinition;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("org");
  const changes = orgDefinitionChanges(before, after);
  return (
    <Dialog open onOpenChange={(open) => !open && !pending && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.coherence.publish)}</DialogTitle>
          <DialogDescription>{t(($) => $.coherence.publish_hint)}</DialogDescription>
        </DialogHeader>
        <ul className="space-y-1.5 text-body">
          {changes.map((c) => (
            <li key={c.id} className="flex items-center gap-2">
              <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-brand" />
              {c.after?.name ?? c.before?.name ?? t(($) => $.coherence.sections[c.section])}
            </li>
          ))}
        </ul>
        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onCancel}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button disabled={pending} onClick={onConfirm}>
            {t(($) => $.coherence.publish)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
