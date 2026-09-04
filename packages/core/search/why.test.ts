// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { isWhyQuery, stripHeadlineMarks, whySearchOptions } from "./why";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("why search", () => {
  it("only asks for multi-word or question queries", () => {
    expect(isWhyQuery("auth")).toBe(false);
    expect(isWhyQuery("why chi?")).toBe(true);
    expect(isWhyQuery("why chi over gin")).toBe(true);
    expect(isWhyQuery("ab cd")).toBe(true);
    expect(isWhyQuery("a?")).toBe(false);
    expect(whySearchOptions("w", "auth").enabled).toBe(false);
    expect(stripHeadlineMarks("we chose <b>Chi</b> over <b>Gin</b>")).toBe("we chose Chi over Gin");
  });

  it("parses results, drops a malformed list and surfaces the short-query error", async () => {
    stubFetch({ results: [{ id: "c1", source_type: "comment", source_id: "s1", issue_id: "i1", issue_identifier: "JEFF-3", snippet: "<b>Chi</b> over Gin", score: 0.4, created_at: "" }], query: "why chi" });
    const res = await new ApiClient("https://api.example.test").searchWhy("why chi");
    expect(res.results[0]?.issue_identifier).toBe("JEFF-3");
    stubFetch({ results: "nope" });
    expect((await new ApiClient("https://api.example.test").searchWhy("why chi")).results).toEqual([]);
    stubFetch({ code: "search_query_too_short", error: "q needs at least 3 characters" }, 400);
    await expect(new ApiClient("https://api.example.test").searchWhy("ab")).rejects.toThrow();
  });
});
