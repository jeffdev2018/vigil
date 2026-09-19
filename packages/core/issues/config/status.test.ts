// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ALL_STATUSES, STATUS_ORDER } from "./status";

describe("status category lists", () => {
  it("keeps ALL_STATUSES and STATUS_ORDER in lockstep", () => {
    expect(ALL_STATUSES).toEqual(STATUS_ORDER);
  });
});
