// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { mcpCallFromReplayData } from "./mcp-calls";

afterEach(() => vi.unstubAllGlobals());

describe("mcp calls", () => {
  it("parses the list tolerantly", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            calls: [{ id: "c1", tool: "send_email", class: "ask", result: "success", gate_id: "g1", flags: "nope" }],
            total: "x",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    const r = await new ApiClient("https://api.example.test").listTaskMcpCalls("t1");
    expect(r.total).toBe(0);
    expect(r.calls).toHaveLength(1);
    expect(r.calls[0]).toMatchObject({ id: "c1", tool: "send_email", class: "ask", result: "success", gate_id: "g1", flags: [] });
  });

  it("maps replay event data into a panel row", () => {
    const row = mcpCallFromReplayData("e1", "2026-09-11T00:00:00Z", {
      server: "mail",
      tool: "send_email",
      class: "ask",
      result: "gated",
      gate_id: "g9",
      duration_ms: 40,
      flags: ["secret_masked"],
    });
    expect(row).toEqual({
      id: "e1",
      at: "2026-09-11T00:00:00Z",
      server: "mail",
      tool: "send_email",
      risk: "",
      class: "ask",
      result: "gated",
      gate_id: "g9",
      duration_ms: 40,
      flags: ["secret_masked"],
    });
  });
});
