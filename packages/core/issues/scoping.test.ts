// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { parseCriteriaLines, scopingDescription } from "./scoping";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("scoping helpers", () => {
  it("appends probable files to the description only when there are any", () => {
    expect(scopingDescription({ description: " Body ", probable_files: [] })).toBe("Body");
    expect(scopingDescription({ description: "Body", probable_files: [{ path: "a.go", reason: "list" }, { path: " " }, { path: "b.ts" }] })).toBe(
      "Body\n\n## Probable files (indicative)\n\n- `a.go` — list\n- `b.ts`",
    );
    expect(parseCriteriaLines("- one\n\n2. two  \n* three")).toEqual(["one", "two", "three"]);
  });
});

describe("proposeIssueScoping", () => {
  it("keeps a usable draft when parts of the answer are malformed", async () => {
    stubFetchJson({ proposal: { title: "T", description: 3, acceptance_criteria: "nope", probable_files: [{ path: "x" }, { nope: 1 }] } });
    const p = await new ApiClient("https://api.example.test").proposeIssueScoping({ raw_text: "x" });
    expect(p).toEqual({ title: "T", description: "", acceptance_criteria: [], probable_files: [] });
  });

  it("rejects an empty or shapeless proposal", async () => {
    stubFetchJson({ proposal: {} });
    await expect(new ApiClient("https://api.example.test").proposeIssueScoping({ raw_text: "x" })).rejects.toThrow();
    stubFetchJson("garbage");
    await expect(new ApiClient("https://api.example.test").proposeIssueScoping({ raw_text: "x" })).rejects.toThrow();
  });
});
