import { describe, expect, it } from "vitest";
import {
  meetingActionStateLabel,
  meetingStatusDotClass,
  meetingStatusLabel,
} from "./meeting-display";

describe("meetingStatusLabel", () => {
  it("labels every server status", () => {
    expect(
      ["recording", "summarizing", "done", "failed"].map(meetingStatusLabel),
    ).toEqual(["Recording", "Summarizing", "Done", "Failed"]);
  });

  it("says Unknown for a status added server-side, never blank", () => {
    expect(meetingStatusLabel("paused")).toBe("Unknown");
  });
});

describe("meetingStatusDotClass", () => {
  it("maps each status to web's colour", () => {
    expect(meetingStatusDotClass("recording")).toBe("bg-red-500");
    expect(meetingStatusDotClass("summarizing")).toBe("bg-blue-500");
    expect(meetingStatusDotClass("done")).toBe("bg-emerald-500");
    expect(meetingStatusDotClass("failed")).toBe("bg-red-500");
  });

  it("still renders a dot for an unknown status", () => {
    expect(meetingStatusDotClass("paused")).toBe("bg-muted-foreground/40");
  });
});

describe("meetingActionStateLabel", () => {
  it("covers all seven triage action states web renders", () => {
    expect(
      [
        "pending",
        "accepted",
        "dismissed",
        "merged",
        "superseded",
        "expired",
        "dropped",
      ].map(meetingActionStateLabel),
    ).toEqual([
      "Pending",
      "Accepted",
      "Dismissed",
      "Merged",
      "Superseded",
      "Expired",
      "Dropped",
    ]);
  });

  it("falls back to the raw value rather than dropping the category", () => {
    expect(meetingActionStateLabel("quarantined")).toBe("quarantined");
  });
});
