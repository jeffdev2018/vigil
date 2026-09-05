"use client";

import { useMemo } from "react";
import { Sparkles } from "lucide-react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import type { RuntimeRoutingStats } from "@multica/core/types";
import { formatUsd } from "../../runtimes/utils";
import { formatDuration } from "../utils";
import { useT } from "../../i18n";

/**
 * Smart routing benchmarks (JEF-237): the router's 90-day track record per
 * (runtime, model, task class). This is the evidence behind Auto mode — the
 * reader's question is "does the router actually pick well here", answered
 * with samples, success rate, cost and duration side by side.
 *
 * The window is fixed server-side at 90 days (it does NOT follow the page's
 * time-range filter), so the card says so instead of borrowing the toolbar's
 * label.
 */
export function RoutingBenchmarksCard({
  rows,
  loading = false,
  lessThanMinuteLabel,
}: {
  rows: RuntimeRoutingStats[];
  loading?: boolean;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");

  // The API returns the router's raw class token (service/task_classify.go).
  // A switch, not a lookup table, because the selector form of `t` needs a
  // literal path; the default branch keeps an unknown class from a newer
  // backend readable instead of blank.
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
          {t(($) => $.routing_benchmarks.title)}
        </h4>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.routing_benchmarks.subtitle)}
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
          <Sparkles className="h-5 w-5 text-faint-foreground" />
          <p className="mt-2 text-body font-medium">
            {t(($) => $.routing_benchmarks.empty_title)}
          </p>
          <p className="mt-1 max-w-md text-caption text-muted-foreground">
            {t(($) => $.routing_benchmarks.empty_body)}
          </p>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t(($) => $.routing_benchmarks.col_runtime)}</TableHead>
              <TableHead>{t(($) => $.routing_benchmarks.col_model)}</TableHead>
              <TableHead>{t(($) => $.routing_benchmarks.col_class)}</TableHead>
              <TableHead className="text-right">
                {t(($) => $.routing_benchmarks.col_samples)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.routing_benchmarks.col_success)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.routing_benchmarks.col_avg_cost)}
              </TableHead>
              <TableHead className="text-right">
                {t(($) => $.routing_benchmarks.col_avg_duration)}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sortedRows.map((row) => (
              <TableRow
                key={`${row.runtime_id}:${row.provider}:${row.model}:${row.task_class}`}
              >
                <TableCell className="font-medium">
                  {row.runtime_name || row.runtime_id}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {row.provider}/{row.model}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {classLabel(row.task_class)}
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
