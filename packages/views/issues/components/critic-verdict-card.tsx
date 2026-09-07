"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Swords } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { criticCostUsd, criticVerdictsOptions, type CriticVerdict } from "@multica/core/critic";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Adversarial review (F25): every verdict recorded on this issue, oldest
 * round first, so a reader follows the argument rather than only its end.
 *
 * A verdict the platform wrote itself — the policy could not be honoured, or
 * the loop hit its budget — is labelled with that reason instead of being
 * shown as an opinion the critic held. Nothing here is hidden behind a
 * tooltip: the reason a review did not happen is the part a reader needs.
 */
export function CriticVerdictCard({ issueId }: { issueId: string }) {
  const { t } = useT("critic");
  const wsId = useWorkspaceId();
  const { data } = useQuery(criticVerdictsOptions(wsId, issueId));
  const verdicts = data?.verdicts ?? [];
  if (verdicts.length === 0) return null;
  return (
    <div data-testid="critic-verdicts" className="flex flex-col gap-1.5 rounded-md border border-border p-2 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <Swords className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.card.title)}</span>
      </div>
      {verdicts.map((v) => (
        <CriticVerdictRow key={v.id} verdict={v} showRound={verdicts.length > 1} />
      ))}
    </div>
  );
}

const REASON_KEYS = {
  no_distinct_provider: "reason_no_distinct_provider",
  max_rounds: "reason_max_rounds",
  max_cost: "reason_max_cost",
  no_verdict: "reason_no_verdict",
} as const;

/** A budget stop is a warning, not a normal verdict: the loop ended early. */
function isBudgetReason(reason: string): boolean {
  return reason === "max_rounds" || reason === "max_cost";
}

function CriticVerdictRow({ verdict, showRound }: { verdict: CriticVerdict; showRound: boolean }) {
  const { t } = useT("critic");
  const [open, setOpen] = useState(false);
  const reasonKey = REASON_KEYS[verdict.reason as keyof typeof REASON_KEYS];
  const budget = isBudgetReason(verdict.reason);
  const cost = criticCostUsd(verdict.cost_usd_ticks);
  return (
    <div
      data-testid="critic-verdict"
      data-verdict={verdict.verdict}
      data-reason={verdict.reason}
      className={cn("flex flex-col gap-1 rounded border p-1.5", budget ? "border-warning/50 bg-warning/10" : "border-transparent bg-muted/40")}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span
          data-testid="critic-verdict-pill"
          className={cn(
            "rounded px-1 font-medium",
            verdict.verdict === "pass"
              ? "bg-success/15 text-success"
              : verdict.verdict === "block"
                ? "bg-destructive/15 text-destructive"
                : "bg-warning/20 text-warning",
          )}
        >
          {verdict.verdict === "pass"
            ? t(($) => $.card.verdict_pass)
            : verdict.verdict === "block"
              ? t(($) => $.card.verdict_block)
              : t(($) => $.card.verdict_concerns)}
        </span>
        {showRound && (
          <span data-testid="critic-verdict-round" className="text-muted-foreground">
            {t(($) => $.card.round, { round: verdict.round })}
          </span>
        )}
        {cost !== "" && <span className="text-muted-foreground">{t(($) => $.card.cost, { cost })}</span>}
        {verdict.critic_task_id && (
          <a
            data-testid="critic-verdict-run"
            className="ml-auto text-muted-foreground underline underline-offset-2"
            href={`/tasks/${verdict.critic_task_id}`}
          >
            {t(($) => $.card.open_run)}
          </a>
        )}
      </div>

      {reasonKey && (
        <p data-testid="critic-verdict-reason" className={cn(budget ? "text-warning" : "text-muted-foreground")}>
          {t(($) => $.card[reasonKey])}
        </p>
      )}

      {verdict.summary !== "" && (
        <p data-testid="critic-verdict-summary" className="line-clamp-4 text-muted-foreground">{verdict.summary}</p>
      )}

      {verdict.findings.length > 0 && (
        <div>
          <button
            type="button"
            data-testid="critic-verdict-toggle"
            aria-expanded={open}
            className="flex items-center gap-1 text-muted-foreground"
            onClick={() => setOpen((v) => !v)}
          >
            {open ? <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" /> : <ChevronRight className="h-3.5 w-3.5" aria-hidden="true" />}
            {open ? t(($) => $.card.hide_findings) : t(($) => $.card.findings, { count: verdict.findings.length })}
          </button>
          {open && (
            <ul data-testid="critic-verdict-findings" className="mt-1 flex flex-col gap-0.5">
              {verdict.findings.map((f, i) => (
                <li key={i} data-severity={f.severity} className="flex min-w-0 flex-wrap items-baseline gap-1.5">
                  <span
                    className={cn(
                      "rounded px-1",
                      f.severity === "bug" ? "bg-destructive/15 text-destructive" : f.severity === "warning" ? "bg-warning/20 text-warning" : "bg-muted text-muted-foreground",
                    )}
                  >
                    {f.severity}
                  </span>
                  {f.file !== "" && (
                    <span className="font-mono text-muted-foreground">
                      {f.file}
                      {f.line > 0 ? `:${f.line}` : ""}
                    </span>
                  )}
                  <span className="min-w-0 break-words">{f.title}</span>
                  {f.note !== "" && <span className="text-muted-foreground">— {f.note}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
