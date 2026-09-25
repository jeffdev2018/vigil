import { useMemo } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { DailyCostStackData } from "../../utils";
import { useT } from "../../../i18n";
import { labelOf } from "./chart-label";

/**
 * Four-segment stack (input / output / cache read / cache write) — every
 * category `estimateCost` bills for, so the bars add up to the same money the
 * Cost KPI reports. The tooltip derives Total by summing the segments it can
 * see, so a category left out of this config is a category left out of the
 * user's total (MUL-6334: cache read alone was >50% of some buckets).
 *
 * Series → CSS chart token: input/output/cache-write keep chart-1/2/3, and
 * cache read takes chart-4 in the output→cache-write slot — the same colour
 * and position DailyTokensChart gives it, so the cost and token views of the
 * same day read as one chart at two scales.
 *
 * Labels reuse usage.legend_* — the same keys the parent's ChartLegend
 * (usage-section.tsx) already renders for this exact stack, so the legend
 * and the tooltip say the same thing in every locale.
 */
export function useCostStackConfig(): ChartConfig {
  const { t } = useT("runtimes");
  return useMemo(
    () => ({
      input: { label: t(($) => $.usage.legend_input), color: "var(--chart-1)" },
      output: { label: t(($) => $.usage.legend_output), color: "var(--chart-2)" },
      cacheRead: { label: t(($) => $.usage.legend_cache_read), color: "var(--chart-4)" },
      cacheWrite: { label: t(($) => $.usage.legend_cache_write), color: "var(--chart-3)" },
    }),
    [t],
  );
}

export function DailyCostChart({ data }: { data: DailyCostStackData[] }) {
  const { t } = useT("runtimes");
  const costStackConfig = useCostStackConfig();
  // No internal empty-state — the parent decides what to show in place of
  // the chart (often a diagnostic explaining *why* there's no cost). Letting
  // recharts render an empty axis would be both ugly and uninformative.
  return (
    <ChartContainer config={costStackConfig} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
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
          tickFormatter={(v: number) => `$${v}`}
          width="auto"
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) =>
                typeof value === "number"
                  ? `$${value.toFixed(2)} ${labelOf(costStackConfig, name)}`
                  : `${value} ${labelOf(costStackConfig, name)}`
              }
              footer={(payload) => {
                const total = payload.reduce(
                  (sum, item) =>
                    sum +
                    (typeof item.value === "number" ? item.value : 0),
                  0,
                );
                return (
                  <div className="flex items-center justify-between gap-2 font-medium">
                    <span>{t(($) => $.charts.tooltip_total)}</span>
                    <span className="font-mono tabular-nums">
                      ${total.toFixed(2)}
                    </span>
                  </div>
                );
              }}
            />
          }
        />
        {/* Legend is intentionally rendered by the parent (in the chart card
            header, top-right) so the chart body stays clean and gets the full
            vertical real estate. */}
        <Bar
          dataKey="input"
          stackId="cost"
          fill="var(--color-input)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="output"
          stackId="cost"
          fill="var(--color-output)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="cacheRead"
          stackId="cost"
          fill="var(--color-cacheRead)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="cacheWrite"
          stackId="cost"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
