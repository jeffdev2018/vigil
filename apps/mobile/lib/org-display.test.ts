// @vitest-environment node
import { expect, it } from "vitest";
import { orgIsLive, orgModelLabel, orgStatusLabel } from "./org-display";

it("orgModelLabel names the seven models and falls back to the raw value", () => {
  expect(orgModelLabel("matrix")).toBe("Competence × project matrix");
  expect(orgModelLabel("owner_network")).toBe("Owner network");
  expect(orgModelLabel("holacracy")).toBe("holacracy");
});

it("orgStatusLabel falls back to the raw value", () => {
  expect(orgStatusLabel("paused")).toBe("Paused");
  expect(orgStatusLabel("weird")).toBe("weird");
});

it("orgIsLive is true only for active structures", () => {
  expect(orgIsLive({ status: "active" })).toBe(true);
  expect(orgIsLive({ status: "paused" })).toBe(false);
  expect(orgIsLive({ status: "draft" })).toBe(false);
});
