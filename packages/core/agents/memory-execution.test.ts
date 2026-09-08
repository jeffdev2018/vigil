// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
afterEach(() => vi.unstubAllGlobals());
it("validates connected runtime configuration and launch response identity", async () => {
 const fetch = vi.fn(); vi.stubGlobal("fetch", fetch); const client = new ApiClient("http://memory.test");
 const config = { runtime_id: "11111111-1111-4111-8111-111111111111", provider: "codex", model: "fixture", effort: "low", config_hash: "a".repeat(64), max_cases: 8, timeout_seconds: 60 };
 fetch.mockResolvedValue(new Response(JSON.stringify(config)));
 expect(await client.getMemoryExecutionConfig("agent", "memory")).toEqual({ ...config, check_modes: ["exact"] });
 fetch.mockResolvedValue(new Response(JSON.stringify({ ...config, check_modes: ["future-mode"] })));
 expect((await client.getMemoryExecutionConfig("agent", "memory")).check_modes).toEqual(["exact"]);
 fetch.mockResolvedValue(new Response(JSON.stringify({ ...config, config_hash: "invalid" })));
 await expect(client.getMemoryExecutionConfig("agent", "memory")).rejects.toThrow("Invalid evaluation runtime response");
 const request = { request_id: "request", expected_revision: 1, config_hash: config.config_hash, cases: [] };
 fetch.mockResolvedValue(new Response(JSON.stringify({ id: "wrong" })));
 await expect(client.startMemoryExecution("agent", "memory", request)).rejects.toThrow("Invalid evaluation launch response");
});
