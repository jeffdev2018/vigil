// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { checkpointKeys } from "./checkpoint";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("checkpoint client", () => {
  it("reads the checkpoint status with tolerant fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ run: { task_id: "t", status: "running", attempts: "x", last_checkpoint_seq: 12, exhausted: "no" } });
    const s = await client.getRunCheckpointStatus("i1");
    expect(s?.attempts).toBe(0);
    expect(s?.max_attempts).toBe(3);
    expect(s?.last_checkpoint_seq).toBe(12);
    expect(s?.exhausted).toBe(false);
    stubFetch("garbage");
    expect(await client.getRunCheckpointStatus("i1")).toBeNull();
    expect(checkpointKeys.status("w", "i")).toEqual(["run-checkpoint", "w", "i"]);
  });
});
