// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  packCountTotal,
  packCountsDetail,
  packCountsSummary,
  packDomainLabel,
  packInstallStatusLabel,
  packKindLabel,
  packStrategyHelp,
  packStrategyLabel,
} from "./packs-display";

describe("packCountsSummary", () => {
  it("orders by count, drops zeros and caps at four kinds", () => {
    expect(
      packCountsSummary({
        labels: 5,
        views: 3,
        agents: 2,
        projects: 1,
        goals: 1,
        notes: 0,
      }),
    ).toBe("5 labels · 3 views · 2 agents · 1 projects");
  });

  it("is empty when nothing has a count", () => {
    expect(packCountsSummary({})).toBe("");
    expect(packCountsSummary({ labels: 0 })).toBe("");
  });

  it("names an unknown kind by its server key rather than dropping it", () => {
    expect(packCountsSummary({ dashboards: 2 })).toBe("2 dashboards");
  });
});

describe("packCountsDetail", () => {
  it("lists every non-zero kind", () => {
    expect(packCountsDetail({ labels: 2, views: 1, notes: 0 })).toBe(
      "2 labels, 1 views",
    );
  });
});

describe("packCountTotal", () => {
  it("sums every kind", () => {
    expect(packCountTotal({ labels: 2, views: 1 })).toBe(3);
    expect(packCountTotal({})).toBe(0);
  });
});

describe("labels", () => {
  it("translates the server keys web translates", () => {
    expect(packKindLabel("issue_types")).toBe("work item types");
    expect(packDomainLabel("hr")).toBe("People");
    expect(packStrategyLabel("merge")).toBe("Merge");
    expect(packInstallStatusLabel("removed")).toBe("Removed");
    expect(packStrategyHelp("skip")).toContain("left exactly as it is");
  });

  it("falls back to the raw key for a value this build does not know", () => {
    expect(packKindLabel("dashboards")).toBe("dashboards");
    expect(packDomainLabel("logistics")).toBe("logistics");
    expect(packStrategyLabel("replace")).toBe("replace");
    expect(packInstallStatusLabel("pending")).toBe("pending");
    expect(packStrategyHelp("replace")).toBe("");
  });
});
