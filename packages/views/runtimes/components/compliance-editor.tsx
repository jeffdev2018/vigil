"use client";

import { useState } from "react";
import { ShieldAlert, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  dataResidencyOptions,
  isResidencyRestrictive,
  runtimeSatisfiesResidency,
  useClearRuntimeCompliance,
  useDeclareRuntimeCompliance,
} from "@multica/core/residency";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";

/**
 * Data residency (K46): what this machine declares about where it runs.
 *
 * Admin-only, unlike the sandbox editor next to it — a residency declaration
 * is a compliance statement on the whole workspace's behalf, not a preference
 * about one's own machine. The declaration is never verified, and the copy
 * says so rather than implying the platform checked.
 */
export function ComplianceEditor({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const { data: policy } = useQuery(dataResidencyOptions(wsId));
  const declare = useDeclareRuntimeCompliance(wsId);
  const clear = useClearRuntimeCompliance(wsId);
  const declaration = runtime.compliance ?? null;
  const [region, setRegion] = useState(declaration?.region ?? "");
  const [onPrem, setOnPrem] = useState(declaration?.on_prem === true);
  const pending = declare.isPending || clear.isPending;

  const failed = (e: unknown) =>
    toast.error(e instanceof Error && e.message ? e.message : t(($) => $.detail.compliance.failed));

  const submit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const trimmed = region.trim();
    if (!trimmed) return;
    declare.mutate(
      { runtimeId: runtime.id, declaration: { region: trimmed, on_prem: onPrem } },
      { onError: failed },
    );
  };

  return (
    <div className="space-y-2">
      {canEdit ? (
        <form onSubmit={submit} className="space-y-2">
          <label className="block text-caption">
            <span className="text-muted-foreground">{t(($) => $.detail.compliance.region_label)}</span>
            <Input
              name="region"
              aria-label={t(($) => $.detail.compliance.region_label)}
              placeholder={t(($) => $.detail.compliance.region_placeholder)}
              className="mt-1 font-mono"
              value={region}
              disabled={pending}
              onChange={(e) => setRegion(e.target.value)}
            />
          </label>
          {/* Not a <label>: wrapping the Switch would make the label match
              two elements, and the switch already names itself. */}
          <div className="flex items-center justify-between gap-3 text-caption">
            <span className="text-muted-foreground">{t(($) => $.detail.compliance.on_prem_label)}</span>
            <Switch
              aria-label={t(($) => $.detail.compliance.on_prem_label)}
              checked={onPrem}
              disabled={pending}
              onCheckedChange={(checked: boolean) => setOnPrem(checked)}
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={pending || !region.trim()}>
              {declare.isPending ? t(($) => $.detail.compliance.saving) : t(($) => $.detail.compliance.save)}
            </Button>
            {declaration && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={pending}
                onClick={() =>
                  clear.mutate(runtime.id, {
                    onSuccess: () => {
                      setRegion("");
                      setOnPrem(false);
                    },
                    onError: failed,
                  })
                }
              >
                {t(($) => $.detail.compliance.clear)}
              </Button>
            )}
          </div>
        </form>
      ) : (
        <ComplianceReadout runtime={runtime} />
      )}

      <ComplianceStatus runtime={runtime} restrictive={isResidencyRestrictive(policy)} policy={policy} />
    </div>
  );
}

function ComplianceReadout({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const declaration = runtime.compliance ?? null;
  if (!declaration) {
    return (
      <p className="text-caption text-muted-foreground">{t(($) => $.detail.compliance.undeclared)}</p>
    );
  }
  return (
    <dl className="text-caption text-muted-foreground">
      <div className="flex gap-1.5">
        <dt>{t(($) => $.detail.compliance.region_label)}:</dt>
        <dd className="min-w-0 truncate font-mono">{declaration.region}</dd>
      </div>
      <div className="flex gap-1.5">
        <dt>{t(($) => $.detail.compliance.on_prem_label)}:</dt>
        <dd>
          {declaration.on_prem
            ? t(($) => $.detail.compliance.on_prem_yes)
            : t(($) => $.detail.compliance.on_prem_no)}
        </dd>
      </div>
    </dl>
  );
}

/**
 * Whether this machine is eligible right now. Silent when the workspace
 * declares no policy: an undeclared runtime in an unconstrained workspace is
 * the normal state, and badging it would invent a problem.
 */
function ComplianceStatus({
  runtime,
  restrictive,
  policy,
}: {
  runtime: AgentRuntime;
  restrictive: boolean;
  policy: Parameters<typeof runtimeSatisfiesResidency>[0];
}) {
  const { t } = useT("runtimes");
  if (!restrictive) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.detail.compliance.no_policy)}
      </p>
    );
  }
  const verdict = runtimeSatisfiesResidency(policy, runtime);
  if (verdict.ok) {
    return (
      <p role="status" className="flex items-start gap-2 text-caption text-success">
        <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
        <span>{t(($) => $.detail.compliance.eligible)}</span>
      </p>
    );
  }
  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-caption text-warning"
    >
      <ShieldAlert className="mt-0.5 size-3.5 shrink-0" />
      <span>{t(($) => $.detail.compliance.rejected[verdict.reason])}</span>
    </div>
  );
}
