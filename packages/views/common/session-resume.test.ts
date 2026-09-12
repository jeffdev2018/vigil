// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { clearSessionResume, readSessionResume, saveSessionResume } from "./session-resume";

const isEmail = (v: unknown): v is { email: string } =>
  typeof v === "object" && v !== null && typeof (v as { email?: unknown }).email === "string";

afterEach(() => window.sessionStorage.clear());

describe("session resume", () => {
  it("returns a saved value until it expires, then drops it", () => {
    saveSessionResume("k", { email: "a@b.c" }, 1000, 0);
    expect(readSessionResume("k", isEmail, 999)).toEqual({ email: "a@b.c" });
    expect(readSessionResume("k", isEmail, 1000)).toBeNull();
    expect(window.sessionStorage.getItem("k")).toBeNull();
  });

  it("rejects a value that fails validation or is corrupt", () => {
    saveSessionResume("k", { email: 42 }, 1000, 0);
    expect(readSessionResume("k", isEmail, 1)).toBeNull();
    window.sessionStorage.setItem("k", "{not json");
    expect(readSessionResume("k", isEmail, 1)).toBeNull();
  });

  it("clears on demand", () => {
    saveSessionResume("k", { email: "a@b.c" }, 1000, 0);
    clearSessionResume("k");
    expect(readSessionResume("k", isEmail, 1)).toBeNull();
  });
});
