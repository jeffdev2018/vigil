// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { scorecardRate } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("scorecards", () => {
  it("defaults malformed totals and keeps a partial series", async () => {
    stubFetchJson({ agent_id: "a", days: 7, totals: { runs_total: "x" }, previous: null, series: [{ day: "2026-09-01", runs_total: 2 }, { nope: 1 }] });
    const s = await new ApiClient("https://api.example.test").getAgentScorecard("a", 7);
    expect(s.totals.runs_total).toBe(0);
    expect(s.totals.low_sample).toBe(true);
    expect(s.previous.runs_total).toBe(0);
    expect(s.series).toEqual([]);
    stubFetchJson({ rows: [{ agent_id: "a", runs_total: 12, runs_accepted: 9, low_sample: false }, { runs_total: 1 }] });
    expect(await new ApiClient("https://api.example.test").listWorkspaceScorecards(30)).toEqual([]);
    stubFetchJson({ rows: [{ agent_id: "a", runs_total: 12, runs_accepted: 9, low_sample: false }] });
    const rows = await new ApiClient("https://api.example.test").listWorkspaceScorecards(30);
    expect(rows[0]?.runs_accepted).toBe(9);
    expect(scorecardRate(9, 12)).toBe(75);
    expect(scorecardRate(0, 0)).toBeNull();
  });
});
