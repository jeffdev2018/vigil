// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_RUN_PREVIEW,
  EMPTY_TASK_SHARE_LINKS,
  RunPreviewSchema,
  TaskShareLinkListSchema,
  isPreviewLocalOnly,
  isPreviewOpenable,
  previewNeedsShareLink,
  truncatePreviewUrl,
} from "./schemas";

// Canonical parsing + state matrix for F12. The card and the chip keep the
// happy path and the wiring; they do not re-run this.

describe("run preview parsing", () => {
  it("falls back to no preview on a malformed response", () => {
    const parsed = parseWithFallback(
      { status: 12, scheme: [], relay_available: "yes" },
      RunPreviewSchema,
      EMPTY_RUN_PREVIEW,
      { endpoint: "GET /api/tasks/{taskId}/preview" },
    );
    // Field-level .catch() keeps the object, so the shape survives; what
    // matters is that nothing in it can be mistaken for an openable preview.
    expect(isPreviewOpenable(parsed)).toBe(false);
    expect(parsed.relay_available).toBe(false);
  });

  it("falls back when the whole payload is not an object", () => {
    const parsed = parseWithFallback("nope", RunPreviewSchema, EMPTY_RUN_PREVIEW, {
      endpoint: "GET /api/tasks/{taskId}/preview",
    });
    expect(parsed).toEqual(EMPTY_RUN_PREVIEW);
  });

  it("keeps an unknown status verbatim instead of coercing it", () => {
    // An older client against a newer server must be able to SHOW the state it
    // does not know, so the panel says something rather than nothing.
    const parsed = RunPreviewSchema.parse({ status: "hibernating", scheme: "relay", url: "https://x/preview/c/" });
    expect(parsed.status).toBe("hibernating");
  });

  it("refuses to open an unknown status even when a url came with it", () => {
    // The rule that matters: only `ready` is openable. Excluding the states we
    // know are bad would hand a reviewer a link for every state added later.
    const parsed = RunPreviewSchema.parse({ status: "hibernating", scheme: "relay", url: "https://x/preview/c/" });
    expect(isPreviewOpenable(parsed)).toBe(false);
  });

  it("only opens ready previews that actually carry a url", () => {
    expect(isPreviewOpenable(RunPreviewSchema.parse({ status: "ready", url: "http://127.0.0.1:21000" }))).toBe(true);
    expect(isPreviewOpenable(RunPreviewSchema.parse({ status: "ready", url: "" }))).toBe(false);
    expect(isPreviewOpenable(undefined)).toBe(false);
    for (const status of ["starting", "stale", "stopped", "error"]) {
      expect(isPreviewOpenable(RunPreviewSchema.parse({ status, url: "http://x" }))).toBe(false);
    }
  });

  it("tells a local preview from a relayed one", () => {
    const loopback = RunPreviewSchema.parse({ status: "ready", scheme: "loopback", url: "http://127.0.0.1:21000" });
    const relay = RunPreviewSchema.parse({ status: "ready", scheme: "relay", url: "https://api/preview/abc/" });
    expect(isPreviewLocalOnly(loopback)).toBe(true);
    expect(isPreviewLocalOnly(relay)).toBe(false);
    // A stopped loopback preview is not "local", it is gone — the copy for the
    // two states is different.
    expect(isPreviewLocalOnly(RunPreviewSchema.parse({ status: "stopped", scheme: "loopback" }))).toBe(false);
  });

  it("knows when a relayed preview is waiting for its first link", () => {
    expect(previewNeedsShareLink(RunPreviewSchema.parse({ status: "ready", scheme: "relay", url: "" }))).toBe(true);
    expect(previewNeedsShareLink(RunPreviewSchema.parse({ status: "ready", scheme: "relay", url: "http://x" }))).toBe(false);
    expect(previewNeedsShareLink(RunPreviewSchema.parse({ status: "ready", scheme: "loopback", url: "" }))).toBe(false);
  });

  it("carries the error text of a preview that never came up", () => {
    const parsed = RunPreviewSchema.parse({ status: "error", error: "ERR_MODULE_NOT_FOUND: vite" });
    expect(parsed.error).toContain("ERR_MODULE_NOT_FOUND");
    expect(isPreviewOpenable(parsed)).toBe(false);
  });
});

describe("share link parsing", () => {
  it("falls back to an empty list on a malformed response", () => {
    const parsed = parseWithFallback({ links: "nope" }, TaskShareLinkListSchema, EMPTY_TASK_SHARE_LINKS, {
      endpoint: "GET /api/tasks/{taskId}/share-links",
    });
    expect(parsed.links).toEqual([]);
  });

  it("drops nothing from a well-formed list", () => {
    const parsed = TaskShareLinkListSchema.parse({
      links: [{ id: "l1", code: "abc", url: "https://api/preview/abc/", capabilities: ["preview"], use_count: 3 }],
    });
    expect(parsed.links).toHaveLength(1);
    expect(parsed.links[0]?.capabilities).toEqual(["preview"]);
  });

  it("keeps a capability this build does not know", () => {
    // Capabilities are a growing set (F16 adds view / steer); a link must not
    // lose one just because this client shipped earlier.
    const parsed = TaskShareLinkListSchema.parse({
      links: [{ id: "l1", capabilities: ["preview", "narrate"] }],
    });
    expect(parsed.links[0]?.capabilities).toEqual(["preview", "narrate"]);
  });
});

describe("truncatePreviewUrl", () => {
  it("leaves a short url alone", () => {
    expect(truncatePreviewUrl("http://127.0.0.1:21000")).toBe("http://127.0.0.1:21000");
  });

  it("keeps both ends of a long url so the host and the code stay readable", () => {
    const long = "https://api.multica.example/preview/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/";
    const short = truncatePreviewUrl(long, 24);
    expect(short.length).toBeLessThanOrEqual(24);
    expect(short.startsWith("https://")).toBe(true);
    expect(short.endsWith("/")).toBe(true);
    expect(short).toContain("…");
  });
});
