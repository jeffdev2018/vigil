// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
const api = new ApiClient("https://api.example.test");

const response = {
  id: "request-1", runtime_id: "runtime-1", action: "login", status: "running",
  verification_url: "https://auth.example/device", user_code: "ABCD-EFGH",
};
afterEach(() => vi.unstubAllGlobals());
function stub(body: unknown) {
  const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("CLI authentication API boundary", () => {
  it("wires login, logout and polling through the same validated boundary", async () => {
    const fetch = stub(response);
    expect(await api.initiateCliAuth("runtime-1")).toMatchObject(response);
    expect(fetch).toHaveBeenLastCalledWith("https://api.example.test/api/runtimes/runtime-1/cli-auth", expect.objectContaining({ method: "POST" }));
    await api.initiateCliLogout("runtime-1");
    expect(fetch).toHaveBeenLastCalledWith("https://api.example.test/api/runtimes/runtime-1/cli-auth", expect.objectContaining({ method: "DELETE" }));
    await api.getCliAuthResult("runtime-1", "request-1");
    expect(fetch).toHaveBeenLastCalledWith("https://api.example.test/api/runtimes/runtime-1/cli-auth/request-1", expect.any(Object));
  });

  it("never turns malformed responses or unsafe URLs into authenticated success", async () => {
    for (const raw of [null, {}, { ...response, id: "" }, { ...response, verification_url: "javascript:alert(1)" }, { ...response, status: "completed" }, { ...response, authenticated: "true" }]) {
      stub(raw);
      for (const result of [await api.initiateCliAuth("runtime-1"), await api.initiateCliLogout("runtime-1"), await api.getCliAuthResult("runtime-1", "request-1")]) {
        expect(result.status).toBe("failed");
        expect(result.authenticated).toBeUndefined();
        expect(result.verification_url).toBeUndefined();
      }
    }
    stub({ ...response, status: "future-status", new_field: true });
    expect((await api.initiateCliAuth("runtime-1")).status).toBe("future-status");
  });
});
