"use client";

import { useMemo } from "react";
import { Workflow } from "lucide-react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import type { WorkflowStats } from "@multica/core/types";
import { formatUsd } from "../../runtimes/utils";
import { formatDuration } from "../utils";
import { useT } from "../../i18n";

/**
 * Workflow outcomes (JEF-273): the selector's 90-day track record per
 * (task class, workflow). This is the evidence the policy toggle learns from
 * — the reader's question is "does cascade/critique actually pay off for this
 * kind of task", answered with samples, success rate, cost and duration side
 * by side.
 *
 * The window is fixed server-side at 90 days (it does NOT follow the page's
 * time-range filter), so the card says so instead of borrowing the toolbar's
 * label.
 */
export function WorkflowOutcomesCard({
  rows,
  loading = false,
  lessThanMinuteLabel,
}: {
  rows: WorkflowStats[];
  loading?: boolean;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");

  // The API returns the classifier's raw class token. A switch, not a lookup
  // table, because the selector form of `t` needs a literal path; the default
  // branch keeps an unknown class from a newer backend readable.
  const classLabel = (taskClass: string): string => {
    switch (taskClass) {
      case "general":
        return t(($) => $.routing_benchmarks.class.general);
      case "bugfix":
        return t(($) => $.routing_benchmarks.class.bugfix);
      case "feature":
        return t(($) => $.routing_benchmarks.class.feature);
      case "refactor":
        return t(($) => $.routing_benchmarks.class.refactor);
      case "docs":
        return t(($) => $.routing_benchmarks.class.docs);
      case "tests":
        return t(($) => $.routing_benchmarks.class.tests);
      case "chore":
        return t(($) => $.routing_benchmarks.class.chore);
      default:
        return taskClass || "—";
    }
  };

  // Same open-string rule for the workflow: the three known strategies get a
  // label, a newer backend's strategy renders raw instead of blank.
  const workflowLabel = (workflow: string): string => {
    switch (workflow) {
      case "single":
        return t(($) => $.workflow_outcomes.workflow.single);
      case "cascade":
        return t(($) => $.workflow_outcomes.workflow.cascade);
      case "critique":
        return t(($) => $.workflow_outcomes.workflow.critique);
      default:
        return workflow || "—";
    }
  };

  // Most-measured first: a high success rate on 2 samples must not outrank a
  // battle-tested row.
  const sortedRows = useMemo(
    () =>
      rows.toSorted(
        (a, b) => b.samples - a.samples || b.success_rate - a.success_rate,
      ),
    [rows],
  );

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-baseline justify-between gap-2 border-b px-4 pt-4 pb-3">
        <h4 className="text-body font-semibold">
          {t(($) => $.workflow_outcomes.title)}
        </h4>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.workflow_outcomes.subtitle)}
        </span>
      </div>
      {loading ? (
        <div className="space-y-2 p-4" aria-hidden="true">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-4/5" />
          <Skeleton className="h-4 w-3/5" />
        </div>
      ) : sortedRows.length === 0 ? (
        <div className="flex flex-col items-center px-4 py-10 text-center">
          <Workflow className="h-5 w-5 text-faint-foreground" />
          <p className="mt-2 text-body font-medium">
            {t(($) => $.workflow_outcomes.empty_title)}
          </p>
          <p className="mt-1 max-w-md text-caption text-muted-foreground">
            {t(($) => $.workflow_outcomes.empty_body)}
          </p>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.workflow_outcomes.col_class)}</TableHead>
              <TableHead>{t(($) => $.workflow_outcomes.col_workflow)}</TableHead>
              <TableHead className="text-right">
                {t(($) => $.workflow_outcomes.col_samples)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.workflow_outcomes.col_success)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.workflow_outcomes.col_avg_cost)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.workflow_outcomes.col_avg_duration)}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sortedRows.map((row) => (
              <TableRow key={`${row.task_class}:${row.workflow}`}>
                <TableCell className="font-medium">
                  {classLabel(row.task_class)}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {workflowLabel(row.workflow)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {row.samples}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {Math.round(row.success_rate * 100)}%
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {row.avg_cost_usd != null ? formatUsd(row.avg_cost_usd) : "—"}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {row.avg_duration_secs != null
                    ? formatDuration(row.avg_duration_secs, lessThanMinuteLabel)
                    : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
