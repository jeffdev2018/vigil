// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_TWENTY_STATUS,
  TwentyConnectionSchema,
  TwentyMembersSchema,
  TwentyStatusSchema,
  invalidTwentyEvents,
  normalizeTwentyEvents,
} from "./schemas";

// The canonical place these shapes are proven. The TwentyTab suite covers the
// happy path and the admin gate only.

describe("TwentyStatusSchema", () => {
  it("parses a connected status", () => {
    const parsed = TwentyStatusSchema.parse({
      available: true,
      connected: true,
      connection: {
        base_url: "https://crm.example.com",
        status: "connected",
        events: ["opportunity.*"],
        expose_to_agents: true,
        webhook_registered: true,
        inbound_path: "/api/triage/inbound/twenty/mtw_x",
        mcp_url: "https://crm.example.com/mcp",
        connected_at: "2026-09-09T00:00:00Z",
        updated_at: "2026-09-09T00:00:00Z",
      },
      default_events: ["opportunity.*"],
    });
    expect(parsed.connected).toBe(true);
    expect(parsed.connection?.mcp_url).toBe("https://crm.example.com/mcp");
    expect(parsed.connection?.inbound_token).toBe("");
  });

  it("survives a malformed response with the empty status", () => {
    expect(parseWithFallback({ connected: "yes", connection: 42 }, TwentyStatusSchema, EMPTY_TWENTY_STATUS, { endpoint: "test" })).toEqual({
      available: false,
      connected: false,
      connection: null,
      default_events: [],
    });
    expect(parseWithFallback("nope", TwentyStatusSchema, EMPTY_TWENTY_STATUS, { endpoint: "test" })).toEqual(EMPTY_TWENTY_STATUS);
  });

  it("keeps an unknown status value from a newer server", () => {
    const parsed = TwentyConnectionSchema.parse({ base_url: "https://x", status: "degraded", mcp_url: "https://x/mcp" });
    expect(parsed.status).toBe("degraded");
    expect(parsed.events).toEqual([]);
  });
});

describe("TwentyMembersSchema", () => {
  it("drops a malformed list to empty", () => {
    expect(TwentyMembersSchema.parse({ members: "x" }).members).toEqual([]);
    const parsed = TwentyMembersSchema.parse({ members: [{ email: "a@b.c", linked: true, twenty_id: "t1" }, { email: "solo@b.c", twenty_only: true }] });
    expect(parsed.members[0]?.linked).toBe(true);
    expect(parsed.members[1]?.twenty_only).toBe(true);
    expect(parsed.members[1]?.user_id).toBe("");
  });
});

describe("event helpers", () => {
  it("normalises a typed list", () => {
    expect(normalizeTwentyEvents(" Person.created, opportunity.*\nperson.created ")).toEqual(["opportunity.*", "person.created"]);
  });
  it("flags patterns Twenty would refuse", () => {
    expect(invalidTwentyEvents(["person.created", "opportunity.*", "*.deleted", "person", "a b.c", "*.*"])).toEqual(["person", "a b.c", "*.*"]);
  });
});
