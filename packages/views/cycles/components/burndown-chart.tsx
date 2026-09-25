"use client";

import { useMemo } from "react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { CycleBurndown } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * The cycle's burndown: what is still open each day against the line that
 * would land it on zero by the end date.
 *
 * `remaining` is deliberately null on days the series cannot speak for — the
 * future, and the past before the first daily snapshot. Recharts draws a gap
 * there rather than a point at zero, which is the honest rendering: a zero
 * would read as "everything was done".
 */
export function BurndownChart({ burndown }: { burndown: CycleBurndown }) {
  const { t } = useT("cycles");
  const usesLoad = burndown.load_unit === "property";

  // Built inside the component (needs `t`) rather than module-scope: the
  // tooltip's getPayloadConfigFromPayload (packages/ui/components/ui/chart.tsx)
  // resolves a series' label from THIS config via its dataKey, never from a
  // <Line name=...> prop — a module-scope English chartConfig meant the
  // tooltip showed "Remaining"/"Ideal" in every locale regardless of the
  // translated `name` passed to <Line>. See daily-cost-chart.tsx and
  // siblings for the same labelOf(config, name) pattern on Bar tooltips.
  const chartConfig = useMemo(
    (): ChartConfig => ({
      remaining: { label: t(($) => $.detail.remaining), color: "var(--chart-1)" },
      ideal: { label: t(($) => $.detail.ideal), color: "var(--chart-3)" },
    }),
    [t],
  );

  const data = useMemo(
    () =>
      burndown.days.map((day) => ({
        date: day.date,
        label: day.date.slice(5),
        remaining: usesLoad ? day.remaining_load : day.remaining_count,
        ideal: usesLoad ? day.ideal_load : day.ideal_count,
      })),
    [burndown.days, usesLoad],
  );

  const hasHistory = data.some((d) => d.remaining !== null);

  return (
    <div className="flex flex-col gap-2">
      <ChartContainer config={chartConfig} className="aspect-[3/1] w-full">
        <LineChart data={data} margin={{ left: 0, right: 8, top: 8, bottom: 0 }}>
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            interval="preserveStartEnd"
          />
          <YAxis
            tickLine={false}
            axisLine={false}
            tickMargin={8}
            allowDecimals={usesLoad}
            width="auto"
          />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Line
            dataKey="ideal"
            type="linear"
            stroke="var(--color-ideal)"
            strokeDasharray="4 4"
            strokeWidth={1.5}
            dot={false}
          />
          <Line
            dataKey="remaining"
            type="monotone"
            stroke="var(--color-remaining)"
            strokeWidth={2}
            dot={false}
            // A null day is a real gap in the series, not a value to bridge.
            connectNulls={false}
          />
        </LineChart>
      </ChartContainer>
      {!hasHistory && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.burndown_empty)}
        </p>
      )}
      {burndown.approximate_before && (
        <p className="text-caption text-warning" data-testid="burndown-approximate">
          {t(($) => $.detail.burndown_approximate, { date: burndown.approximate_before })}
        </p>
      )}
      <p className="text-caption text-muted-foreground">
        {usesLoad
          ? t(($) => $.detail.load_unit_property)
          : t(($) => $.detail.load_unit_issues)}
      </p>
    </div>
  );
}
