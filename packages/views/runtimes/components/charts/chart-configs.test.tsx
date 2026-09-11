// @vitest-environment jsdom
/**
 * Chart series configs used to be module-scope `const`s with hardcoded
 * English labels ("Input", "Completed", "Run time", ...). ChartContainer's
 * tooltip resolves a series' label from THIS config via its Recharts
 * dataKey, never from a translated <Bar/Line name=...> prop, so the
 * tooltip showed English text in every locale regardless of the app's
 * selected language. They're now useX() hooks built from useT().
 */
import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../../locales";
import { useCostStackConfig } from "./daily-cost-chart";
import { useTasksChartConfig } from "./daily-tasks-chart";
import { useTimeChartConfig } from "./daily-time-chart";

function wrapper(locale: "en" | "fr" | "zh-Hans") {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={RESOURCES}>
        {children}
      </I18nProvider>
    );
  };
}

describe("chart series configs", () => {
  it("useCostStackConfig translates every segment label", () => {
    const en = renderHook(() => useCostStackConfig(), { wrapper: wrapper("en") });
    expect(en.result.current.input!.label).toBe("Input");

    const fr = renderHook(() => useCostStackConfig(), { wrapper: wrapper("fr") });
    expect(fr.result.current.input!.label).toBe("Entrée");
    expect(fr.result.current.cacheRead!.label).toBe("Lecture cache");
  });

  it("useTasksChartConfig translates every segment label", () => {
    const zh = renderHook(() => useTasksChartConfig(), { wrapper: wrapper("zh-Hans") });
    expect(zh.result.current.completed!.label).toBe("已完成");
    expect(zh.result.current.cancelled!.label).toBe("已取消");
    expect(zh.result.current.failed!.label).toBe("失败");
  });

  it("useTimeChartConfig translates the single series label", () => {
    const fr = renderHook(() => useTimeChartConfig(), { wrapper: wrapper("fr") });
    expect(fr.result.current.totalSeconds!.label).toBe("Durée d'exécution");
    expect(fr.result.current.totalSeconds!.label).not.toBe("Run time");
  });
});
