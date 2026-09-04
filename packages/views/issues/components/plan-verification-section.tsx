"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issuePlanOptions,
  latestPlanVerification,
  planVerificationsOptions,
  sortPlanFindings,
} from "@multica/core/issues/plan";
import type { IssuePlan, PlanFinding, PlanVerification } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { PlanGateBlock } from "./plan-gate-block";

/**
 * Plan verification (F17): the issue's plan (with its version history) and
 * the newest verification report, findings first by severity. Renders nothing
 * until the issue has a plan; every state of the verification run has a line.
 */
export function PlanVerificationSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [version, setVersion] = useState<number | null>(null);
  const { data: envelope } = useQuery(issuePlanOptions(wsId, issueId));
  const { data: verifications = [] } = useQuery({
    ...planVerificationsOptions(wsId, issueId),
    enabled: !!envelope?.plan,
  });
  if (!envelope?.plan) return null;

  const shown: IssuePlan =
    envelope.versions.find((v) => v.version === version) ?? envelope.plan;
  const latest = latestPlanVerification(verifications);

  return (
    <div data-testid="plan-verification" className="text-caption">
      <div className="mb-2 flex w-full items-center gap-1">
        <button
          type="button"
          className={cn(
            "flex min-w-0 items-center gap-1 rounded-md px-2 py-1 font-medium transition-colors hover:bg-accent/70",
            open ? "" : "text-muted-foreground hover:text-foreground",
          )}
          onClick={() => setOpen(!open)}
        >
          <span className="truncate">{t(($) => $.plan_verification.section)}</span>
          <ChevronRight
            className={cn("!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform", open ? "rotate-90" : "")}
          />
        </button>
        {latest && <VerificationBadge verification={latest} />}
      </div>
      {open && (
        <div className="flex flex-col gap-2 pl-2">
          <div className="flex items-center gap-2 text-muted-foreground">
            <span>{t(($) => $.plan_verification.plan_version, { version: shown.version })}</span>
            {envelope.versions.length > 1 && (
              <select
                aria-label={t(($) => $.plan_verification.version_picker)}
                className="rounded border bg-background px-1 py-0.5 text-caption"
                value={shown.version}
                onChange={(e) => setVersion(Number(e.target.value))}
              >
                {envelope.versions.map((v) => (
                  <option key={v.id} value={v.version}>
                    {`v${v.version}${v.superseded_at ? ` · ${t(($) => $.plan_verification.superseded)}` : ""}`}
                  </option>
                ))}
              </select>
            )}
          </div>
          <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded bg-muted/40 p-2 font-sans text-caption">
            {shown.content}
          </pre>
          {/* Plan Gate (K11): the steps and the approval that makes them sub-issues. */}
          <PlanGateBlock issueId={issueId} plan={shown} />
          {latest ? (
            <VerificationReport verification={latest} />
          ) : (
            <div className="text-muted-foreground">{t(($) => $.plan_verification.no_verification)}</div>
          )}
        </div>
      )}
    </div>
  );
}

function VerificationBadge({ verification }: { verification: PlanVerification }) {
  const { t } = useT("issues");
  switch (verification.state) {
    case "queued":
      return <span className="ml-auto text-warning">{t(($) => $.plan_verification.state_queued)}</span>;
    case "running":
      return (
        <span className="ml-auto inline-flex items-center gap-1 text-info">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-info" />
          {t(($) => $.plan_verification.state_running)}
        </span>
      );
    case "failed":
      return <span className="ml-auto text-destructive">{t(($) => $.plan_verification.state_failed)}</span>;
    case "reported":
      return (
        <span
          data-testid="plan-verification-verdict"
          className={cn("ml-auto font-medium", verification.critical_count > 0 ? "text-destructive" : "text-success")}
        >
          {verification.critical_count > 0
            ? t(($) => $.plan_verification.verdict_critical, { count: verification.critical_count })
            : t(($) => $.plan_verification.verdict_clean)}
        </span>
      );
    default:
      return <span className="ml-auto text-muted-foreground">{verification.state}</span>;
  }
}

function VerificationReport({ verification }: { verification: PlanVerification }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  if (verification.state !== "reported") {
    if (verification.state === "failed") {
      return <div className="text-destructive">{t(($) => $.plan_verification.failed_hint)}</div>;
    }
    return <div className="text-muted-foreground">{t(($) => $.plan_verification.pending_hint)}</div>;
  }
  const findings = sortPlanFindings(verification.findings);
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap gap-x-3 text-muted-foreground">
        <span>{t(($) => $.plan_verification.report_for, { version: verification.plan_version })}</span>
        {verification.reported_at && <span>{timeAgo(verification.reported_at)}</span>}
      </div>
      <div className="flex flex-wrap gap-2">
        <Counter label={t(($) => $.plan_verification.severity_critical)} count={verification.critical_count} tone="text-destructive" />
        <Counter label={t(($) => $.plan_verification.severity_major)} count={verification.major_count} tone="text-warning" />
        <Counter label={t(($) => $.plan_verification.severity_minor)} count={verification.minor_count} tone="text-muted-foreground" />
        <Counter label={t(($) => $.plan_verification.severity_outdated)} count={verification.outdated_count} tone="text-muted-foreground" />
      </div>
      {verification.summary && <div className="whitespace-pre-wrap">{verification.summary}</div>}
      {findings.length === 0 ? (
        <div className="text-success">{t(($) => $.plan_verification.no_findings)}</div>
      ) : (
        <ul className="flex flex-col gap-1">
          {findings.map((f, i) => (
            <FindingRow key={`${f.title}-${i}`} finding={f} />
          ))}
        </ul>
      )}
    </div>
  );
}

function Counter({ label, count, tone }: { label: string; count: number; tone: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1 tabular-nums", count > 0 ? tone : "text-faint-foreground")}>
      <span className="font-mono">{count}</span>
      {label}
    </span>
  );
}

const SEVERITY_TONE: Record<string, string> = {
  critical: "text-destructive",
  major: "text-warning",
  minor: "text-muted-foreground",
  outdated: "text-muted-foreground",
};

function FindingRow({ finding }: { finding: PlanFinding }) {
  const { t } = useT("issues");
  const severity = finding.severity.toLowerCase();
  const tone = SEVERITY_TONE[severity] ?? "text-muted-foreground";
  return (
    <li>
      <details>
        <summary className="flex cursor-pointer items-center gap-2">
          <span className={cn("shrink-0 font-medium uppercase", tone)}>{severity || "?"}</span>
          <span className="truncate" title={finding.title}>{finding.title}</span>
        </summary>
        <div className="mt-1 flex flex-col gap-1 pl-2">
          {finding.detail && <div className="whitespace-pre-wrap text-muted-foreground">{finding.detail}</div>}
          {finding.files && finding.files.length > 0 && (
            <div className="overflow-x-auto whitespace-nowrap font-mono text-muted-foreground">
              {finding.files.join("  ")}
            </div>
          )}
          {finding.plan_step_id && <div className="text-faint-foreground">{t(($) => $.plan_verification.step_ref, { id: finding.plan_step_id })}</div>}
        </div>
      </details>
    </li>
  );
}
