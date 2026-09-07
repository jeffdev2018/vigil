// @vitest-environment node

import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeCliAuthRequest } from "../types/agent";
import {
  readRuntimeCliAuthState,
  resolveRuntimeCliAuth,
} from "./cli-auth";

const { initiateCliAuth, initiateCliLogout, getCliAuthResult } = vi.hoisted(() => ({
  initiateCliAuth: vi.fn(),
  initiateCliLogout: vi.fn(),
  getCliAuthResult: vi.fn(),
}));

vi.mock("../api", () => ({
  api: { initiateCliAuth, initiateCliLogout, getCliAuthResult },
  ApiError: class ApiError extends Error {},
}));

function request(
  overrides: Partial<RuntimeCliAuthRequest> = {},
): RuntimeCliAuthRequest {
  return {
    id: "req-1",
    runtime_id: "rt-1",
    action: "login",
    status: "pending",
    created_at: "2099-01-01T00:00:00Z",
    updated_at: "2099-01-01T00:00:00Z",
    expires_at: "2099-01-01T00:10:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  initiateCliAuth.mockReset();
  initiateCliLogout.mockReset();
  getCliAuthResult.mockReset();
});

describe("resolveRuntimeCliAuth", () => {
  it("reports device-code progress before completion", async () => {
    initiateCliAuth.mockResolvedValue(request());
    getCliAuthResult
      .mockResolvedValueOnce(
        request({
          status: "running",
          verification_url: "https://auth.example/device",
          user_code: "ABCD-EFGH",
        }),
      )
      .mockResolvedValueOnce(
        request({ status: "completed", authenticated: true }),
      );
    const progress = vi.fn();

    const resultPromise = resolveRuntimeCliAuth("rt-1", "login", progress);
    await vi.runAllTimersAsync();
    const result = await resultPromise;

    expect(result.authenticated).toBe(true);
    expect(progress).toHaveBeenCalledWith(
      expect.objectContaining({ user_code: "ABCD-EFGH" }),
    );
  });

  it("surfaces the server timeout reason", async () => {
    initiateCliAuth.mockResolvedValue(request());
    getCliAuthResult.mockResolvedValue(
      request({ status: "timeout", error: "daemon did not respond" }),
    );
    const promise = resolveRuntimeCliAuth("rt-1", "login");
    const assertion = expect(promise).rejects.toThrow("daemon did not respond");
    await vi.runAllTimersAsync();
    await assertion;
  });

  it("does not treat an unknown status as success", async () => {
    initiateCliAuth.mockResolvedValue(request({ status: "paused" }));
    await expect(resolveRuntimeCliAuth("rt-1", "login")).rejects.toThrow(
      "status: paused",
    );
  });

  it("uses the logout endpoint and returns the signed-out state", async () => {
    initiateCliLogout.mockResolvedValue(
      request({ action: "logout", status: "completed", authenticated: false }),
    );

    await expect(resolveRuntimeCliAuth("rt-1", "logout")).resolves.toEqual(
      expect.objectContaining({ action: "logout", authenticated: false }),
    );
    expect(initiateCliLogout).toHaveBeenCalledWith("rt-1");
    expect(initiateCliAuth).not.toHaveBeenCalled();
  });
});

describe("readRuntimeCliAuthState", () => {
  it("keeps an absent or malformed state unknown", () => {
    expect(readRuntimeCliAuthState({})).toBeNull();
    expect(readRuntimeCliAuthState({ cli_auth: { authenticated: "yes" } })).toBeNull();
  });

  it("reads a durable false state without turning it into unknown", () => {
    expect(readRuntimeCliAuthState({ cli_auth: { authenticated: false } })).toEqual({
      authenticated: false,
      checked_at: undefined,
      provider: undefined,
      reason: undefined,
    });
  });
});
