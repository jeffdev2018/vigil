// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("weekly retro client", () => {
  it("parses a retro, tolerates malformed sections and maps 404 to null", async () => {
    stubFetch({ week_start: "2026-08-31", week_end: "2026-09-06", runs_total: 3, runs_by_status: { completed: 2, failed: 1 }, failed: "nope", agents: [{ agent_id: "a", name: "Bot", runs_total: 3 }], narrative: 7 });
    const retro = await new ApiClient("https://api.example.test").getWeeklyRetro("2026-09-02");
    expect(retro?.runs_total).toBe(3);
    expect(retro?.failed).toEqual([]);
    expect(retro?.agents[0]?.name).toBe("Bot");
    expect(retro?.narrative).toBe("");
    stubFetch({ code: "retro_not_found", error: "none" }, 404);
    expect(await new ApiClient("https://api.example.test").getWeeklyRetro()).toBeNull();
    stubFetch({ error: "boom" }, 500);
    await expect(new ApiClient("https://api.example.test").getWeeklyRetro()).rejects.toThrow();
  });
});
