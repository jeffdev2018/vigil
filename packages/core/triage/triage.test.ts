// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { triageKeys } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const validItem = {
  id: "item-1",
  source_id: "src-1",
  source_name: "Sentry",
  source_kind: "autopilot_webhook",
  origin_type: "autopilot",
  title: "Payment gateway timeout",
  body_markdown: "",
  payload: { size: 12, body: { alert: "payment-gateway" } },
  state: "pending",
  collapse_count: 1,
  first_seen_at: "2026-01-01T00:00:00Z",
  revision: 1,
};

describe("listTriageItems", () => {
  it("parses a well-formed response and threads the cursor", async () => {
    stubFetchJson({ items: [validItem], next_cursor: "abc" });
    const res = await new ApiClient("https://api.example.test").listTriageItems({
      state: "pending",
    });
    expect(res.items).toHaveLength(1);
    expect(res.items[0]?.title).toBe("Payment gateway timeout");
    expect(res.next_cursor).toBe("abc");
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({
      items: [{ id: "item-2", first_seen_at: "2026-01-01T00:00:00Z" }],
    });
    const res = await new ApiClient("https://api.example.test").listTriageItems();
    expect(res.items).toHaveLength(1);
    expect(res.items[0]?.id).toBe("item-2");
    expect(res.items[0]?.title).toBe("");
    expect(res.items[0]?.state).toBe("pending");
    expect(res.items[0]?.collapse_count).toBe(1);
    expect(res.items[0]?.payload).toEqual({});
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ items: "not-an-array" });
    const res = await new ApiClient("https://api.example.test").listTriageItems();
    expect(res).toEqual({ items: [] });
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(
      new ApiClient("https://api.example.test").listTriageItems(),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("getTriageStats", () => {
  it("parses a well-formed stats payload", async () => {
    stubFetchJson({
      pending: 3,
      shadow_pending: 0,
      dropped_24h: 1,
      oldest_pending_age_seconds: 120,
      sources: [
        {
          id: "src-1",
          kind: "autopilot_webhook",
          ref_id: "ap-1",
          name: "Sentry",
          mode: "gate",
          items_24h: 4,
          dropped_24h: 1,
        },
      ],
    });
    const stats = await new ApiClient("https://api.example.test").getTriageStats();
    expect(stats.pending).toBe(3);
    expect(stats.sources).toHaveLength(1);
    expect(stats.sources[0]?.mode).toBe("gate");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ pending: 2, sources: "nope" });
    const stats = await new ApiClient("https://api.example.test").getTriageStats();
    expect(stats).toMatchObject({ pending: 0, sources: [] });
  });
});

describe("triageKeys", () => {
  it("nests items and stats under the workspace prefix", () => {
    expect(triageKeys.items("ws-1", "pending")).toEqual([
      ...triageKeys.all("ws-1"),
      "items",
      "pending",
      "due",
    ]);
    // The Snoozed tab lists the same server state with a wider filter, so it
    // must not share a cache entry with the default pending queue.
    expect(triageKeys.items("ws-1", "pending", true)).toEqual([
      ...triageKeys.all("ws-1"),
      "items",
      "pending",
      "snoozed",
    ]);
    expect(triageKeys.stats("ws-1")).toEqual([...triageKeys.all("ws-1"), "stats"]);
  });
});
