// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { parseList, permissionProfileKeys } from "./permission-profiles";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("permission profiles client", () => {
  it("reads the list, tolerates malformed rows and parses lists", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ profiles: [{ id: "p1", name: "code", denied_paths: ".env", hidden_secrets: ["*PROD*"] }] });
    const [code] = await client.listPermissionProfiles();
    expect(code?.name).toBe("code");
    expect(code?.denied_paths).toEqual([]);
    expect(code?.hidden_secrets).toEqual(["*PROD*"]);
    expect(code?.read_only).toBe(false);
    stubFetch("garbage");
    expect(await client.listPermissionProfiles()).toEqual([]);
    stubFetch({ id: "a1", permission_profile_id: null });
    expect((await client.setAgentPermissionProfile("a1", null)).permission_profile_id).toBeNull();
    stubFetch({ id: "p1", name: "code", read_only: true });
    expect((await client.updatePermissionProfile("p1", { read_only: true })).read_only).toBe(true);
    expect(parseList(" .env, infra/**,\n.env ,")).toEqual([".env", "infra/**"]);
    expect(permissionProfileKeys.list("w")).toEqual(["permission-profiles", "w"]);
  });
});
