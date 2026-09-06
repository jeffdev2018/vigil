// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  BATCH_WINDOW_DEFAULTS,
  BatchWindowSchema,
  batchWindowProblem,
  hasUsableBatchWindow,
  type BatchWindow,
} from "./schemas";

// Off-peak batch lane (K45). Installed desktop builds talk to newer servers, so
// a drifted or malformed response must degrade to "no off-peak window" rather
// than throw — and never to an ENABLED one, which would tell the user their
// autopilots are waiting when nothing is.

const window = (over: Partial<BatchWindow> = {}): BatchWindow => ({
  ...BATCH_WINDOW_DEFAULTS,
  ...over,
});

describe("BatchWindowSchema", () => {
  it("fills in every field a sparse response omits", () => {
    const parsed = BatchWindowSchema.parse({});
    expect(parsed.enabled).toBe(false);
    expect(parsed.start_local_time).toBe("");
    expect(parsed.end_local_time).toBe("");
    expect(parsed.timezone).toBe("UTC");
  });

  it("keeps fields a newer server added", () => {
    const parsed = BatchWindowSchema.parse({ enabled: true, future_field: "x" });
    expect(parsed.enabled).toBe(true);
    expect((parsed as Record<string, unknown>).future_field).toBe("x");
  });

  it("degrades malformed fields instead of throwing", () => {
    const parsed = BatchWindowSchema.parse({
      enabled: "yes",
      start_local_time: 2200,
      end_local_time: null,
      timezone: 7,
    });
    expect(parsed.enabled).toBe(false);
    expect(parsed.start_local_time).toBe("");
    expect(parsed.timezone).toBe("UTC");
  });

  it("falls back rather than throwing on a wholly malformed response", () => {
    for (const bad of [null, "nope", 42, []]) {
      expect(
        parseWithFallback(bad, BatchWindowSchema, BATCH_WINDOW_DEFAULTS, {
          endpoint: "GET /api/batch-window",
        }),
      ).toEqual(BATCH_WINDOW_DEFAULTS);
    }
  });
});

describe("hasUsableBatchWindow", () => {
  it("is false for no window at all", () => {
    expect(hasUsableBatchWindow(undefined)).toBe(false);
    expect(hasUsableBatchWindow(window())).toBe(false);
  });

  it("is true for a same-day window and one that crosses midnight", () => {
    expect(
      hasUsableBatchWindow(window({ enabled: true, start_local_time: "01:00", end_local_time: "05:00" })),
    ).toBe(true);
    expect(
      hasUsableBatchWindow(window({ enabled: true, start_local_time: "22:00", end_local_time: "06:00" })),
    ).toBe(true);
  });

  it("is false for an enabled window the scheduler could never apply", () => {
    // The server refuses to store these, but a drifted response can still
    // carry one, and the toggle must not offer a lane nothing would honour.
    expect(
      hasUsableBatchWindow(window({ enabled: true, start_local_time: "", end_local_time: "06:00" })),
    ).toBe(false);
    expect(
      hasUsableBatchWindow(window({ enabled: true, start_local_time: "03:00", end_local_time: "03:00" })),
    ).toBe(false);
    expect(
      hasUsableBatchWindow(window({ enabled: true, start_local_time: "24:00", end_local_time: "06:00" })),
    ).toBe(false);
  });
});

describe("batchWindowProblem", () => {
  it("says nothing about a disabled window, whatever its times", () => {
    expect(batchWindowProblem(window({ start_local_time: "nonsense" }))).toBeNull();
  });

  it("names the unusable cases the server would refuse", () => {
    expect(
      batchWindowProblem(window({ enabled: true, start_local_time: "", end_local_time: "06:00" })),
    ).toBe("invalid_time");
    expect(
      batchWindowProblem(window({ enabled: true, start_local_time: "22:60", end_local_time: "06:00" })),
    ).toBe("invalid_time");
    expect(
      batchWindowProblem(window({ enabled: true, start_local_time: "03:00", end_local_time: "03:00" })),
    ).toBe("equal_bounds");
  });

  it("accepts a window the server would store", () => {
    expect(
      batchWindowProblem(window({ enabled: true, start_local_time: "22:00", end_local_time: "06:00" })),
    ).toBeNull();
  });
});
