// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  canReviewDoctrineVersion,
  countOtherManagers,
  doctrineReportKindLabel,
  doctrineVersionStatusLabel,
} from "./doctrine-display";

const base = {
  canPublish: true,
  status: "pending",
  authorId: "author",
  userId: "reviewer",
  otherManagerCount: 1,
};

describe("canReviewDoctrineVersion", () => {
  it("lets another manager review a pending proposal", () => {
    expect(canReviewDoctrineVersion(base)).toBe(true);
  });

  it("refuses a member who cannot publish", () => {
    expect(canReviewDoctrineVersion({ ...base, canPublish: false })).toBe(
      false,
    );
  });

  it("refuses a version that is not awaiting review", () => {
    for (const status of ["active", "rejected", "superseded"]) {
      expect(canReviewDoctrineVersion({ ...base, status })).toBe(false);
    }
  });

  it("refuses the author while another manager could review", () => {
    expect(
      canReviewDoctrineVersion({ ...base, userId: "author" }),
    ).toBe(false);
  });

  it("lets the author through when they are the only manager", () => {
    expect(
      canReviewDoctrineVersion({
        ...base,
        userId: "author",
        otherManagerCount: 0,
      }),
    ).toBe(true);
  });

  it("refuses when the viewer is unknown", () => {
    expect(canReviewDoctrineVersion({ ...base, userId: null })).toBe(false);
  });
});

describe("countOtherManagers", () => {
  it("counts owners and admins other than the viewer", () => {
    const members = [
      { user_id: "me", role: "owner" },
      { user_id: "a", role: "admin" },
      { user_id: "b", role: "member" },
      { user_id: "c", role: "owner" },
    ];
    expect(countOtherManagers(members, "me")).toBe(2);
    expect(countOtherManagers(members, null)).toBe(3);
  });
});

describe("labels", () => {
  it("falls back to the raw value for unknown enums", () => {
    expect(doctrineReportKindLabel("conflict")).toBe("Conflict");
    expect(doctrineReportKindLabel("something_new")).toBe("something_new");
    expect(doctrineVersionStatusLabel("pending")).toBe("Awaiting review");
    expect(doctrineVersionStatusLabel("weird")).toBe("weird");
  });
});
