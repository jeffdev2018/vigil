"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Circle, CircleAlert, CircleHelp } from "lucide-react";
import {
  blockedRequiredStepIds,
  deriveActivationReadiness,
  minutesSinceIso,
  type ActivationStep,
  type ActivationStepId,
  type ActivationStepStatus,
} from "@multica/core/runtimes";
import { captureEvent } from "@multica/core/analytics";
import { memberNeedsMikaSetup } from "@multica/core/onboarding";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions } from "@multica/core/workspace/queries";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { githubInstallationsOptions } from "@multica/core/github";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import type { RuntimeMachine } from "./runtime-machines";

function stepIcon(status: ActivationStepStatus) {
  switch (status) {
    case "ready":
      return Check;
    case "not_applicable":
      return Circle;
    case "unknown":
      return CircleHelp;
    case "recommended":
      return Circle;
    case "blocked":
    default:
      return CircleAlert;
  }
}

function stepTone(status: ActivationStepStatus): string {
  switch (status) {
    case "ready":
      return "text-success";
    case "not_applicable":
    case "recommended":
      return "text-muted-foreground";
    case "unknown":
      return "text-warning";
    case "blocked":
    default:
      return "text-warning";
  }
}

/**
 * Only the repository step is fixed somewhere else. This card lives on
 * Runtimes, right above the machine list and the Mika card that resolve every
 * other step, so linking those rows would point at the page already open.
 */
function hrefForStep(
  id: ActivationStepId,
  paths: ReturnType<typeof useWorkspacePaths>,
): string | null {
  return id === "repo_linked" ? `${paths.settings()}?tab=repositories` : null;
}

function ActivationStepRow({
  step,
  href,
}: {
  step: ActivationStep;
  href: string | null;
}) {
  const { t } = useT("runtimes");
  const Icon = stepIcon(step.status);
  const label = t(($) => $.activation.steps[step.id].label);
  const description =
    step.status === "ready"
      ? t(($) => $.activation.steps[step.id].ready)
      : step.status === "not_applicable"
        ? t(($) => $.activation.steps[step.id].not_applicable)
        : t(($) => $.activation.steps[step.id].action);

  const body = (
    <div className="flex min-w-0 items-start gap-3">
      <Icon
        aria-hidden
        className={`mt-0.5 size-4 shrink-0 ${stepTone(step.status)}`}
      />
      <div className="min-w-0 flex-1">
        <p className="text-body font-medium">{label}</p>
        <p className="text-caption text-muted-foreground">{description}</p>
      </div>
      {step.kind === "recommended" && step.status === "recommended" && (
        <span className="shrink-0 text-micro text-muted-foreground">
          {t(($) => $.activation.optional)}
        </span>
      )}
    </div>
  );

  if (!href || step.status === "ready" || step.status === "not_applicable") {
    return <li className="rounded-lg px-3 py-2">{body}</li>;
  }

  return (
    <li>
      <AppLink
        href={href}
        className="block rounded-lg px-3 py-2 transition-colors hover:bg-muted/60"
      >
        {body}
      </AppLink>
    </li>
  );
}

/**
 * Readable preparation checklist for the first useful agent result.
 * Does not rebuild onboarding — it surfaces signals already on Runtimes /
 * Settings so a member can see what still blocks machine → auth → Mika.
 */
export function ActivationReadinessCard({
  machines,
}: {
  machines: RuntimeMachine[];
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const workspace = useCurrentWorkspace();
  const onboardedAt = useAuthStore((s) => s.user?.onboarded_at ?? null);
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: chatSessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: githubInstallations } = useQuery(githubInstallationsOptions(wsId));

  const readiness = useMemo(
    () =>
      deriveActivationReadiness({
        runtimes,
        machines: machines.map((machine) => ({
          mode: machine.mode,
          cliVersion: machine.cliVersion,
        })),
        hasWorkspaceRepos: (workspace?.repos?.length ?? 0) > 0,
        hasGithubInstallation:
          (githubInstallations?.installations?.length ?? 0) > 0,
        memberNeedsMikaSetup: memberNeedsMikaSetup(agents, chatSessions),
      }),
    [
      runtimes,
      machines,
      workspace?.repos,
      githubInstallations?.installations?.length,
      agents,
      chatSessions,
    ],
  );

  // Client-only funnel signal: backend never sees "member looked at Runtimes
  // while still blocked". Fire once per mount of a blocked checklist so
  // abandon/delay analysis can join onboarding_completed → this view →
  // issue_executed (server). Delay minutes use onboarded_at when present.
  const viewedRef = useRef(false);
  useEffect(() => {
    if (readiness.readyForFirstResult || viewedRef.current) return;
    viewedRef.current = true;
    const minutesSinceOnboarding = minutesSinceIso(onboardedAt);
    captureEvent("activation_checklist_viewed", {
      workspace_id: wsId,
      blocked_required_count: readiness.blockedRequiredCount,
      blocked_steps: blockedRequiredStepIds(readiness),
      ...(minutesSinceOnboarding !== null
        ? { minutes_since_onboarding: minutesSinceOnboarding }
        : {}),
    });
  }, [readiness, onboardedAt, wsId]);

  if (readiness.readyForFirstResult) return null;

  return (
    <section
      className="mb-6 rounded-xl border bg-card p-5"
      aria-label={t(($) => $.activation.title)}
    >
      <div className="mb-3">
        <h2 className="text-body font-semibold">{t(($) => $.activation.title)}</h2>
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.activation.description)}
        </p>
      </div>
      <ol className="space-y-1">
        {readiness.steps.map((step) => (
          <ActivationStepRow
            key={step.id}
            step={step}
            href={hrefForStep(step.id, paths)}
          />
        ))}
      </ol>
    </section>
  );
}
