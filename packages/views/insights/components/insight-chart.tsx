"use client";

import { useMemo } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  XAxis,
  YAxis,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { InsightQuery, InsightRow } from "@multica/core/insights";
import { insightLabelKey, insightRowValue, resolveInsightShape } from "@multica/core/insights";
import { useT } from "../../i18n";

/**
 * The one chart every insight renders through. It is generic on purpose: the
 * document decides the shape, so a per-metric component would be a component
 * per question.
 *
 * Fixed-metric dashboard charts (packages/views/runtimes/components/charts)
 * stay as they are — they know their axes at build time and this one cannot.
 */

// Past this many series a chart stops being readable: a donut becomes a ring
// of slivers and a bar chart becomes a picket fence. The table is not a
// degraded fallback, it is the honest rendering of that many categories.
const MAX_SERIES = 12;

// Axis labels are workspace data (a project name, a custom status), so they
// are truncated for layout and carried in full by the title attribute.
const MAX_AXIS_LABEL = 14;

export interface InsightChartProps {
  query: InsightQuery | null | undefined;
  rows: InsightRow[];
  shape?: string | null;
  /** The question that produced these rows. Always rendered by the caller. */
  emptyLabel?: string;
}

interface ChartDatum {
  label: string;
  fullLabel: string;
  value: number;
  fill: string;
}

export function InsightChart({ query, rows, shape, emptyLabel }: InsightChartProps) {
  const { t } = useT("usage");
  const resolved = resolveInsightShape(shape, query);
  const labelKey = insightLabelKey(query);

  const data = useMemo<ChartDatum[]>(() => {
    return rows.map((row, index) => {
      const raw = labelKey ? row[labelKey] : null;
      const fullLabel = raw === null || raw === undefined ? "—" : String(raw);
      const value = insightRowValue(row) ?? 0;
      return {
        label: truncate(fullLabel, MAX_AXIS_LABEL),
        fullLabel,
        value,
        // The palette is the chart token ramp, cycled. A document can group by
        // anything, so there is no stable per-series colour to assign.
        fill: `var(--chart-${(index % 5) + 1})`,
      };
    });
  }, [rows, labelKey]);

  const config = useMemo<ChartConfig>(() => ({ value: { label: t(($) => $.insights.value) } }), [t]);

  if (rows.length === 0) {
    return (
      <p className="py-6 text-center text-caption text-muted-foreground">
        {emptyLabel ?? t(($) => $.insights.no_rows)}
      </p>
    );
  }

  if (resolved === "number") {
    const value = data[0]?.value ?? 0;
    return (
      <div className="flex min-h-24 items-center justify-center">
        <span className="font-mono text-title tabular-nums">{formatNumber(value)}</span>
      </div>
    );
  }

  if (data.length > MAX_SERIES) {
    return <InsightTable data={data} />;
  }

  if (resolved === "line") {
    return (
      <ChartContainer config={config} className="aspect-[3/1] w-full">
        <LineChart data={data} margin={{ left: 0, right: 8, top: 4, bottom: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            interval="preserveStartEnd"
          />
          <YAxis tickLine={false} axisLine={false} tickMargin={8} width="auto" />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Line
            dataKey="value"
            type="monotone"
            stroke="var(--chart-1)"
            strokeWidth={2}
            dot={false}
          />
        </LineChart>
      </ChartContainer>
    );
  }

  if (resolved === "donut") {
    return (
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <ChartContainer config={config} className="aspect-square h-40 shrink-0">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent nameKey="fullLabel" />} />
            <Pie data={data} dataKey="value" nameKey="fullLabel" innerRadius="55%">
              {data.map((datum) => (
                <Cell key={datum.fullLabel} fill={datum.fill} />
              ))}
            </Pie>
          </PieChart>
        </ChartContainer>
        {/* A donut with a dozen slices needs a scrollable legend, not a
            wrapping one that pushes the chart off the card. */}
        <ul className="max-h-40 min-w-0 flex-1 overflow-y-auto text-caption">
          {data.map((datum) => (
            <li key={datum.fullLabel} className="flex items-center gap-2 py-0.5">
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: datum.fill }}
              />
              <span className="min-w-0 flex-1 truncate" title={datum.fullLabel}>
                {datum.fullLabel}
              </span>
              <span className="shrink-0 font-mono tabular-nums">{formatNumber(datum.value)}</span>
            </li>
          ))}
        </ul>
      </div>
    );
  }

  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval={0} />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} width="auto" />
        <ChartTooltip content={<ChartTooltipContent nameKey="fullLabel" />} />
        <Bar dataKey="value" radius={[3, 3, 0, 0]}>
          {data.map((datum) => (
            <Cell key={datum.fullLabel} fill={datum.fill} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}

function InsightTable({ data }: { data: ChartDatum[] }) {
  const { t } = useT("usage");
  return (
    <div className="max-h-64 overflow-auto">
      <table className="w-full text-caption">
        <caption className="pb-2 text-left text-caption text-muted-foreground">
          {t(($) => $.insights.too_many_series, { count: data.length })}
        </caption>
        <tbody>
          {data.map((datum) => (
            <tr key={datum.fullLabel} className="border-b last:border-0">
              <td className="max-w-0 truncate py-1 pr-2" title={datum.fullLabel}>
                {datum.fullLabel}
              </td>
              <td className="py-1 text-right font-mono tabular-nums">
                {formatNumber(datum.value)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max - 1)}…` : value;
}

function formatNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}
