// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const api = new ApiClient("https://api.example.test");

const running = {
  id: "request-1",
  runtime_id: "runtime-1",
  action: "login",
  status: "running",
  verification_url: "https://github.com/login/device",
  user_code: "ABCD-EFGH",
  created_at: "2026-09-10T10:00:00Z",
  updated_at: "2026-09-10T10:00:00Z",
  expires_at: "2026-09-10T10:15:00Z",
};

function stub(body: unknown) {
  const fetch = vi.fn().mockImplementation(
    async () =>
      new Response(JSON.stringify(body), {
        headers: { "Content-Type": "application/json" },
      }),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

/** All three endpoints share one schema; each must hold the boundary on its own. */
async function everyEndpoint() {
  return [
    await api.initiateCliAuth("runtime-1"),
    await api.initiateCliLogout("runtime-1"),
    await api.getCliAuthResult("runtime-1", "request-1"),
  ];
}

afterEach(() => vi.unstubAllGlobals());

describe("CLI authentication API boundary", () => {
  it("routes login, logout and polling to their endpoints", async () => {
    const fetch = stub(running);
    expect(await api.initiateCliAuth("runtime-1")).toMatchObject(running);
    expect(fetch).toHaveBeenLastCalledWith(
      "https://api.example.test/api/runtimes/runtime-1/cli-auth",
      expect.objectContaining({ method: "POST" }),
    );
    await api.initiateCliLogout("runtime-1");
    expect(fetch).toHaveBeenLastCalledWith(
      "https://api.example.test/api/runtimes/runtime-1/cli-auth",
      expect.objectContaining({ method: "DELETE" }),
    );
    await api.getCliAuthResult("runtime-1", "request-1");
    expect(fetch).toHaveBeenLastCalledWith(
      "https://api.example.test/api/runtimes/runtime-1/cli-auth/request-1",
      expect.any(Object),
    );
  });

  // The sign-in panel renders verification_url as a link. A scheme other than
  // http(s) must never reach it, whichever endpoint produced the response.
  it.each([
    ["javascript:", "javascript:alert(document.cookie)"],
    ["data:", "data:text/html,<script>alert(1)</script>"],
    ["vbscript:", "vbscript:msgbox(1)"],
    ["file:", "file:///etc/passwd"],
  ])("refuses a %s verification link", async (_scheme, url) => {
    stub({ ...running, verification_url: url });
    for (const result of await everyEndpoint()) {
      expect(result.status).toBe("failed");
      expect(result.verification_url).toBeUndefined();
    }
  });

  it.each([
    ["null", null],
    ["an empty object", {}],
    ["an empty request id", { ...running, id: "" }],
    ["an empty runtime id", { ...running, runtime_id: "" }],
    ["a runaway user code", { ...running, user_code: "x".repeat(129) }],
    ["a stringly-typed outcome", { ...running, status: "completed", authenticated: "true" }],
    ["a completion that names no outcome", { ...running, status: "completed" }],
  ])("never turns %s into a success", async (_label, raw) => {
    stub(raw);
    for (const result of await everyEndpoint()) {
      expect(result.status).toBe("failed");
      expect(result.authenticated).toBeUndefined();
      expect(result.verification_url).toBeUndefined();
    }
  });

  it("keeps a completed outcome and an unknown future status", async () => {
    stub({ ...running, status: "completed", authenticated: false });
    expect(await api.getCliAuthResult("runtime-1", "request-1")).toMatchObject({
      status: "completed",
      authenticated: false,
    });

    // Drift the other way: a newer server's status and extra fields pass
    // through so an installed desktop build does not read them as failures.
    stub({ ...running, status: "awaiting-browser", new_field: true });
    expect((await api.initiateCliAuth("runtime-1")).status).toBe("awaiting-browser");
  });
});
