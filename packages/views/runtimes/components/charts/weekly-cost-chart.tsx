import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Cell,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@multica/ui/components/ui/chart";
import type { WeeklyCostStackData } from "../../utils";
import { useT } from "../../../i18n";
import { labelOf } from "./chart-label";
import { useCostStackConfig } from "./daily-cost-chart";

// Same four-segment stack as DailyCostChart — keeping series, colours, and
// ordering identical so the user reads "Weekly" as a coarser cut of the same
// chart, not a different chart. Partial-week bars render at half-opacity so
// "this week is in progress" is visually obvious without a separate legend.
// Shares useCostStackConfig with the daily chart rather than a parallel copy.

export function WeeklyCostChart({ data }: { data: WeeklyCostStackData[] }) {
  const { t } = useT("runtimes");
  const weeklyCostStackConfig = useCostStackConfig();
  return (
    <ChartContainer config={weeklyCostStackConfig} className="aspect-[3/1] w-full">
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
              labelKey="rangeLabel"
              labelFormatter={(_label, payload) => {
                const row = payload[0]?.payload as WeeklyCostStackData | undefined;
                if (!row) return "";
                return row.partial
                  ? t(($) => $.usage.weekly_partial_label, {
                      range: row.rangeLabel,
                      covered: row.daysCovered,
                    })
                  : row.rangeLabel;
              }}
              formatter={(value, name) =>
                typeof value === "number"
                  ? `$${value.toFixed(2)} ${labelOf(weeklyCostStackConfig, name)}`
                  : `${value} ${labelOf(weeklyCostStackConfig, name)}`
              }
              footer={(payload) => {
                const total = payload.reduce(
                  (sum, item) =>
                    sum + (typeof item.value === "number" ? item.value : 0),
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
        <Bar dataKey="input" stackId="cost" fill="var(--color-input)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar dataKey="output" stackId="cost" fill="var(--color-output)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar dataKey="cacheRead" stackId="cost" fill="var(--color-cacheRead)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar
          dataKey="cacheWrite"
          stackId="cost"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        >
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}
