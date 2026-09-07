// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  MANUAL_RETRY_PRESERVES,
  activeRunGuidanceKind,
  liveWaitReason,
} from "./run-guidance";

describe("liveWaitReason", () => {
  it("returns trimmed text only while waiting_local_directory", () => {
    expect(
      liveWaitReason("waiting_local_directory", "  NuvioTV (held by task a1b2c3d4)  "),
    ).toBe("NuvioTV (held by task a1b2c3d4)");
  });

  it("drops the stored hold text once the task is no longer waiting", () => {
    for (const status of [
      "queued",
      "dispatched",
      "running",
      "completed",
      "failed",
      "cancelled",
    ] as const) {
      expect(liveWaitReason(status, "NuvioTV (held by task a1b2c3d4)")).toBeUndefined();
    }
  });

  it("treats blank or missing reasons as absent", () => {
    expect(liveWaitReason("waiting_local_directory", "   ")).toBeUndefined();
    expect(liveWaitReason("waiting_local_directory", null)).toBeUndefined();
    expect(liveWaitReason("waiting_local_directory", undefined)).toBeUndefined();
  });
});

describe("activeRunGuidanceKind", () => {
  it("names the waits that need a cause+action blurb", () => {
    expect(activeRunGuidanceKind("waiting_local_directory")).toBe(
      "waiting_local_directory",
    );
    expect(activeRunGuidanceKind("queued")).toBe("queued");
    expect(activeRunGuidanceKind("dispatched")).toBe("dispatched");
  });

  it("skips running and terminal rows", () => {
    expect(activeRunGuidanceKind("running")).toBeNull();
    expect(activeRunGuidanceKind("failed")).toBeNull();
    expect(activeRunGuidanceKind(undefined)).toBeNull();
  });
});

describe("MANUAL_RETRY_PRESERVES", () => {
  it("documents the MUL-4869 / RerunIssue honesty contract", () => {
    expect(MANUAL_RETRY_PRESERVES.workDirWhenAvailable).toBe(true);
    expect(MANUAL_RETRY_PRESERVES.sessionOnlyIfUnpoisoned).toBe(true);
    expect(MANUAL_RETRY_PRESERVES.externalSideEffects).toBe(false);
  });
});
