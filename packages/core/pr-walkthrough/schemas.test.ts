// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  EMPTY_PR_WALKTHROUGH,
  PR_WALKTHROUGH_DEFAULT_SETTINGS,
  PrWalkthroughSchema,
  PrWalkthroughSettingsSchema,
  groupHunkCount,
  middleTruncate,
  normalizeKind,
  orderedGroups,
  type PrWalkthroughGroup,
} from "./schemas";
import { parseWithFallback } from "../api/schema";

// Canonical layer for the walkthrough contract (F05). The component suite
// (packages/views/issues/components/pr-walkthrough-section.test.tsx) keeps the
// happy path and the wiring; the parsing matrix lives here.

const group = (over: Partial<PrWalkthroughGroup> = {}): PrWalkthroughGroup => ({
  title: "Retry the fetch on a 429",
  kind: "core",
  rationale: "A throttled response is retried instead of failing the run.",
  files: [
    {
      path: "api/client.go",
      hunks: [
        { old_start: 12, new_start: 12, lines: "@@ -12,7 +12,9 @@", explanation: "Wraps the call.", moved_from: "" },
      ],
    },
  ],
  ...over,
});

describe("PrWalkthroughSchema", () => {
  it("parses a well-formed response", () => {
    const parsed = parseWithFallback(
      {
        state: "ready",
        head_sha: "abc123",
        truncated: true,
        omitted_files: 25,
        groups: [group()],
        generated_at: "2026-01-02T03:04:05Z",
        error: "",
      },
      PrWalkthroughSchema,
      EMPTY_PR_WALKTHROUGH,
      { endpoint: "test" },
    );
    expect(parsed.state).toBe("ready");
    expect(parsed.omitted_files).toBe(25);
    expect(parsed.groups[0]?.files[0]?.hunks[0]?.explanation).toBe("Wraps the call.");
  });

  it("falls back to a pending walkthrough when the response is malformed", () => {
    // Acceptance 5: the section hides itself, the issue page stays intact.
    for (const raw of [null, undefined, "nope", 42, [], { groups: "not an array" }]) {
      const parsed = parseWithFallback(raw, PrWalkthroughSchema, EMPTY_PR_WALKTHROUGH, { endpoint: "test" });
      expect(parsed.groups).toEqual([]);
      expect(["pending", "ready", "failed"]).toContain(parsed.state);
    }
  });

  it("keeps a group whose fields arrive with the wrong types", () => {
    // A single bad field must not cost the whole narrative.
    const parsed = parseWithFallback(
      { state: "ready", groups: [{ title: 7, kind: null, rationale: {}, files: "nope" }] },
      PrWalkthroughSchema,
      EMPTY_PR_WALKTHROUGH,
      { endpoint: "test" },
    );
    expect(parsed.groups).toHaveLength(1);
    expect(parsed.groups[0]?.files).toEqual([]);
  });

  it("tolerates an unknown state and an unknown kind from a newer server", () => {
    const parsed = parseWithFallback(
      { state: "summarising", groups: [group({ kind: "vendored" })] },
      PrWalkthroughSchema,
      EMPTY_PR_WALKTHROUGH,
      { endpoint: "test" },
    );
    // Both stay raw here; the UI's default branch and normalizeKind decide.
    expect(parsed.state).toBe("summarising");
    expect(parsed.groups[0]?.kind).toBe("vendored");
    expect(normalizeKind("vendored")).toBe("noise");
  });
});

describe("PrWalkthroughSettingsSchema", () => {
  it("never enables the feature from a malformed response", () => {
    for (const raw of [null, "off", { enabled: "yes" }, {}]) {
      const parsed = parseWithFallback(raw, PrWalkthroughSettingsSchema, PR_WALKTHROUGH_DEFAULT_SETTINGS, {
        endpoint: "test",
      });
      expect(parsed.enabled).toBe(false);
    }
    expect(
      parseWithFallback({ enabled: true, agent_id: "a1" }, PrWalkthroughSettingsSchema, PR_WALKTHROUGH_DEFAULT_SETTINGS, {
        endpoint: "test",
      }).enabled,
    ).toBe(true);
  });
});

describe("orderedGroups", () => {
  it("renders core, test, generated, noise in that order whatever the server sent", () => {
    const ordered = orderedGroups([
      group({ kind: "noise", title: "n" }),
      group({ kind: "generated", title: "g" }),
      group({ kind: "test", title: "t" }),
      group({ kind: "core", title: "c" }),
    ]);
    expect(ordered.map((g) => g.title)).toEqual(["c", "t", "g", "n"]);
  });

  it("sorts an unknown kind last rather than ahead of core", () => {
    const ordered = orderedGroups([group({ kind: "vendored", title: "v" }), group({ kind: "core", title: "c" })]);
    expect(ordered.map((g) => g.title)).toEqual(["c", "v"]);
  });

  it("is stable within a kind and safe on an empty list", () => {
    const ordered = orderedGroups([group({ kind: "core", title: "a" }), group({ kind: "core", title: "b" })]);
    expect(ordered.map((g) => g.title)).toEqual(["a", "b"]);
    expect(orderedGroups([])).toEqual([]);
  });
});

describe("middleTruncate", () => {
  it("keeps both ends of a long path", () => {
    const truncated = middleTruncate("packages/views/issues/components/pr-walkthrough-section.tsx", 30);
    expect(truncated).toHaveLength(30);
    expect(truncated.startsWith("packages/")).toBe(true);
    expect(truncated.endsWith(".tsx")).toBe(true);
    expect(truncated).toContain("…");
  });

  it("leaves a short path alone", () => {
    expect(middleTruncate("api/client.go")).toBe("api/client.go");
    expect(middleTruncate("")).toBe("");
  });
});

describe("groupHunkCount", () => {
  it("counts every hunk across the group's files", () => {
    expect(groupHunkCount(group())).toBe(1);
    expect(groupHunkCount(group({ files: [] }))).toBe(0);
  });
});
