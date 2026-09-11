// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ApiError } from "./client";
import { isResourceMissingError } from "./load-error";

describe("isResourceMissingError", () => {
  it.each([
    ["404", new ApiError("issue not found", 404, "Not Found"), true],
    ["403", new ApiError("no access", 403, "Forbidden"), true],
    ["400", new ApiError("bad id", 400, "Bad Request"), true],
    ["500", new ApiError("boom", 500, "Internal Server Error"), false],
    ["502", new ApiError("bad gateway", 502, "Bad Gateway"), false],
    ["408 timeout", new ApiError("timeout", 408, "Request Timeout"), false],
    ["429 rate limit", new ApiError("slow down", 429, "Too Many Requests"), false],
    ["offline fetch", new TypeError("Failed to fetch"), false],
    ["aborted", new DOMException("aborted", "AbortError"), false],
    ["nothing", undefined, false],
  ])("%s", (_name, error, missing) => {
    expect(isResourceMissingError(error)).toBe(missing);
  });
});
