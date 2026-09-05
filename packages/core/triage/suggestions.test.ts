// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { triageSuggestionsOptions } from "./queries";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("triage suggestions", () => {
  it("parses suggestions per item and tolerates a malformed map", async () => {
    stubFetch({ suggestions: { i1: { item_id: "i1", ready: true, examples: 30, suggested: "dismiss", confidence: 0.92, neighbors: [{ id: "n1", title: "bump", state: "dismissed", score: 0.4 }] } }, auto: { enabled: true, threshold: 0.9 } });
    const res = await new ApiClient("https://api.example.test").getTriageSuggestions(["i1"]);
    expect(res.suggestions.i1?.suggested).toBe("dismiss");
    expect(res.auto.min_examples).toBe(20);
    stubFetch({ suggestions: "nope" });
    expect((await new ApiClient("https://api.example.test").getTriageSuggestions(["i1"])).suggestions).toEqual({});
    expect(triageSuggestionsOptions("w", []).enabled).toBe(false);
    expect(triageSuggestionsOptions("w", ["b", "a"]).queryKey).toEqual(["triage", "w", "suggestions", "a,b"]);
  });
});
