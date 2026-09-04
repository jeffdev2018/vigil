// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { runControlKeys } from "./run-control";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("run control client", () => {
  it("reads the run state with tolerant fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ run: { task_id: "t1", status: "paused", pause_pending: "no", instructions: "x" } });
    const s = await client.getRunState("i1");
    expect(s?.status).toBe("paused");
    expect(s?.pause_pending).toBe(false);
    expect(s?.instructions).toEqual([]);
    stubFetch("garbage");
    expect(await client.getRunState("i1")).toBeNull();
    stubFetch({ run: { task_id: "t1", status: "running", pause_pending: true, instructions: [] } }, 202);
    expect((await client.pauseRun("i1"))?.pause_pending).toBe(true);
    stubFetch({ run: { task_id: "t1", status: "paused", instructions: ["fix it"] } }, 201);
    expect((await client.steerRun("i1", "fix it"))?.instructions).toEqual(["fix it"]);
    stubFetch({ run: { task_id: "t2", status: "queued", instructions: [] }, paused_task_id: "t1" }, 201);
    expect((await client.resumeRun("i1"))?.task_id).toBe("t2");
    expect(runControlKeys.state("w", "i")).toEqual(["run-state", "w", "i"]);
  });
});
