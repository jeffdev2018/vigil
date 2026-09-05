// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

// K60: tolerant parsing of the SSO state, SCIM tokens and project roles.
describe("access client", () => {
  it("parses sso, scim tokens and project members tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ connection: { issuer: "https://idp", enforced: "yes" }, configured: true });
    const sso = await client.getSSOConnection("w1");
    expect(sso.connection?.enforced).toBe(false);
    expect(sso.configured).toBe(true);
    stubFetch({ nope: 1 });
    expect((await client.getSSOConnection("w1")).connection).toBeNull();
    stubFetch({ id: "t1", token: "scim_abc", token_hint: "scim_abc…", active: true }, 201);
    expect((await client.createScimToken("w1")).token).toBe("scim_abc");
    stubFetch({ tokens: [{ id: "t1", active: true }] });
    expect((await client.listScimTokens("w1")).tokens[0]?.token).toBeUndefined();
    stubFetch({ members: [{ subject_type: "robot", subject_id: "a1", effective_role: "king", source: "override", override: "viewer" }] });
    const members = await client.listProjectMembers("p1");
    expect(members.members[0]?.subject_type).toBe("member");
    expect(members.members[0]?.effective_role).toBe("contributor");
    expect(members.members[0]?.override).toBe("viewer");
    stubFetch({ authorization_url: "https://idp/authorize?x=1" });
    expect((await client.startOIDCLogin("acme", "https://app/login/sso")).authorization_url).toContain("idp");
  });
});
