// @vitest-environment node
import { describe, expect, it } from "vitest";
import { derivedTitle } from "./organize-dialog";

// Canonical matrix for the derived title; the dialog suite keeps the wiring.
describe("derivedTitle", () => {
  it("keeps a short first line", () => {
    expect(derivedTitle("pgbouncer listens on 6432\nsecond")).toBe("pgbouncer listens on 6432");
  });
  it("cuts at a word boundary past 80 characters", () => {
    expect(
      derivedTitle("les déploiements passent par le tag de release visuel, jamais par un push manuel sur le serveur"),
    ).toBe("les déploiements passent par le tag de release visuel, jamais par un push");
  });
  it("hard-cuts a single long word", () => {
    expect(derivedTitle("a".repeat(100))).toBe("a".repeat(80));
  });
});
