// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { TRUST_MODES, pct, trustKeys } from "./trust";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("trust dial client", () => {
  it("reads the mode, the suggestion and the history with tolerant fallbacks", async () => {
    stubFetch({ agent_id: "a", mode: "autonomous", modes: TRUST_MODES });
    expect((await new ApiClient("https://api.example.test").getAgentTrustMode("a")).mode).toBe("autonomous");
    stubFetch({ eligible: true, current_mode: "propose", suggested_mode: "approval", metrics: { runs_total: 12, accepted_rate: 1 }, reasons: [] });
    const s = await new ApiClient("https://api.example.test").getAgentTrustSuggestion("a");
    expect(s.suggested_mode).toBe("approval");
    expect(s.metrics.days).toBe(30);
    stubFetch("garbage");
    expect((await new ApiClient("https://api.example.test").getAgentTrustSuggestion("a")).eligible).toBe(false);
    stubFetch({ changes: [{ id: "c1", from_mode: "approval", to_mode: "observer", demotion: true }, { nope: 1 }] });
    expect(await new ApiClient("https://api.example.test").listAgentTrustHistory("a")).toEqual([]);
    stubFetch({ changes: [{ id: "c1", from_mode: "approval", to_mode: "observer", demotion: true }] });
    expect((await new ApiClient("https://api.example.test").listAgentTrustHistory("a"))[0]?.demotion).toBe(true);
    expect(pct(0.834)).toBe("83%");
    stubFetch({ agent_id: "a", mode: "preview" });
    expect((await new ApiClient("https://api.example.test").getAgentEffectMode("a")).mode).toBe("preview");
    stubFetch({ agent_id: "a", mode: "sideways" });
    expect((await new ApiClient("https://api.example.test").setAgentEffectMode("a", "preview")).mode).toBe("apply");
    expect(trustKeys.history("w", "a")).toEqual(["agents", "w", "a", "trust", "history"]);
  });
});
