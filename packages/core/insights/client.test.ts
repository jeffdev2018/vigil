// @vitest-environment node
import { describe, expect, it, vi, afterEach } from "vitest";
import { ApiClient } from "../api/client";
import type { InsightQuery } from "./schemas";
import { EMPTY_INSIGHT_ASK_RESPONSE, EMPTY_INSIGHT_RUN_RESPONSE } from "./schemas";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respondWith(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(typeof body === "string" ? body : JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const runQuery: InsightQuery = { entity: "issue", metric: "count", group_by: [], filters: [] };

describe("insight endpoints under a drifted backend", () => {
  it("keeps a run response whose rows are well-formed", async () => {
    respondWith({
      rows: [{ status: "blocked", value: 3 }],
      shape: "donut",
      warnings: ["labels double-count"],
      duration_ms: 12,
    });
    const client = new ApiClient("https://api.example.test");

    const result = await client.runInsight({ ...runQuery });
    expect(result.rows).toEqual([{ status: "blocked", value: 3 }]);
    expect(result.shape).toBe("donut");
  });

  it("falls back instead of throwing when a run response is malformed", async () => {
    // rows is the field the chart iterates. A backend that sends an object
    // where the contract says an array must degrade to an empty card, never
    // to an unhandled throw inside a render.
    respondWith({ rows: { status: "blocked" }, shape: 7 });
    const client = new ApiClient("https://api.example.test");

    await expect(client.runInsight({ ...runQuery })).resolves.toEqual(
      EMPTY_INSIGHT_RUN_RESPONSE,
    );
  });

  it("falls back when an ask response is malformed", async () => {
    // Valid JSON, wrong contract: `rows` is a string. This is the drift a
    // schema is for — a body that is not JSON at all fails earlier, in the
    // transport, and is not this boundary's job.
    respondWith({ query: "issue count", rows: "three" });
    const client = new ApiClient("https://api.example.test");

    await expect(client.askInsight("how many issues")).resolves.toEqual(
      EMPTY_INSIGHT_ASK_RESPONSE,
    );
  });

  it("keeps an unknown shape rather than rejecting the response", async () => {
    // `shape` is lenient by design: a newer backend may name a chart this
    // build has never heard of, and the tab must still render.
    respondWith({ rows: [], shape: "sankey", warnings: null, duration_ms: 1 });
    const client = new ApiClient("https://api.example.test");

    const result = await client.runInsight({ ...runQuery });
    expect(result.shape).toBe("sankey");
    expect(result.warnings).toBeNull();
  });

  it("keeps a widget list usable when one widget is missing fields", async () => {
    respondWith([{ id: "w1" }]);
    const client = new ApiClient("https://api.example.test");

    const widgets = await client.listInsightWidgets();
    expect(widgets).toHaveLength(1);
    expect(widgets[0]?.name).toBe("");
    expect(widgets[0]?.revision).toBe(1);
    expect(widgets[0]?.query).toBeNull();
  });

  it("falls back to an empty list when the widget listing is not an array", async () => {
    respondWith({ items: [] });
    const client = new ApiClient("https://api.example.test");

    await expect(client.listInsightWidgets()).resolves.toEqual([]);
  });
});
