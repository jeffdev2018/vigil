"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, Circle, X } from "lucide-react";
import {
  onboardingChecklistOptions,
  selectGettingStartedDismissed,
  useGettingStartedStore,
} from "@multica/core/onboarding";
import { paths } from "@multica/core/paths";
import { Card, CardContent, CardHeader } from "@multica/ui/components/ui/card";
import { Progress } from "@multica/ui/components/ui/progress";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

interface ChecklistRow {
  key: string;
  done: boolean;
  label: string;
  href: string;
  optional?: boolean;
}

/**
 * Getting-started checklist (OS plan, chantier 5). Mounted at the top of the
 * page a fresh workspace lands on (issues — there is no separate dashboard
 * route; both the root workspace route and the post-onboarding redirect
 * point at it). Auto-hides once the server reports `complete: true`, or
 * once the member dismisses it — `useGettingStartedStore` persists that
 * choice per workspace so it does not resurface on reload.
 */
export function GettingStartedCard({
  wsId,
  wsSlug,
}: {
  wsId: string;
  wsSlug: string;
}) {
  const { t } = useT("onboarding");
  const { data, isPending } = useQuery(onboardingChecklistOptions(wsId));
  const dismissed = useGettingStartedStore(selectGettingStartedDismissed(wsId));
  const dismiss = useGettingStartedStore((s) => s.dismiss);

  if (isPending || !data || dismissed || data.complete) return null;

  const ws = paths.workspace(wsSlug);
  const runtimeLabel =
    data.runtime_kind === "native"
      ? t(($) => $.getting_started.row_runtime_native)
      : data.runtime_kind === "daemon"
        ? t(($) => $.getting_started.row_runtime_daemon)
        : t(($) => $.getting_started.row_runtime_none);

  const rows: ChecklistRow[] = [
    {
      key: "runtime",
      done: data.runtime_ready,
      label: runtimeLabel,
      href: ws.runtimes(),
    },
    {
      key: "agent",
      done: data.agent_created,
      label: t(($) => $.getting_started.row_agent),
      href: ws.newAgent(),
    },
    {
      key: "issue",
      done: data.issue_created,
      label: t(($) => $.getting_started.row_issue),
      href: ws.issues(),
    },
    {
      key: "first_run",
      done: data.first_run_completed,
      label: t(($) => $.getting_started.row_first_run),
      href: ws.runs(),
    },
    {
      key: "first_decision",
      done: data.first_decision_answered,
      label: t(($) => $.getting_started.row_first_decision),
      href: ws.inbox(),
      optional: true,
    },
  ];

  const doneCount = rows.filter((row) => row.done).length;

  return (
    <Card
      size="sm"
      className="mx-4 mt-4 shrink-0"
      data-testid="getting-started-card"
    >
      <CardHeader className="flex flex-row items-center justify-between gap-3 px-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <span className="text-body font-medium text-foreground">
            {t(($) => $.getting_started.title)}
          </span>
          <Progress value={(doneCount / rows.length) * 100} />
        </div>
        <button
          type="button"
          aria-label={t(($) => $.getting_started.dismiss)}
          onClick={() => dismiss(wsId)}
          className="shrink-0 rounded-sm p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        >
          <X className="size-3.5" />
        </button>
      </CardHeader>
      <CardContent className="flex flex-col gap-0.5 px-4">
        {rows.map((row) => (
          <AppLink
            key={row.key}
            href={row.href}
            className={cn(
              "flex items-center gap-2 rounded-md px-2 py-1.5 text-caption transition-colors hover:bg-muted/60",
              row.done
                ? "text-muted-foreground line-through"
                : row.optional
                  ? "text-muted-foreground"
                  : "text-foreground",
            )}
          >
            {row.done ? (
              <CheckCircle2
                className="size-3.5 shrink-0 text-success"
                aria-hidden
              />
            ) : (
              <Circle className="size-3.5 shrink-0" aria-hidden />
            )}
            <span className="truncate">{row.label}</span>
            {row.optional && !row.done && (
              <span className="shrink-0 text-micro text-muted-foreground">
                {t(($) => $.getting_started.optional_suffix)}
              </span>
            )}
          </AppLink>
        ))}
      </CardContent>
    </Card>
  );
}
