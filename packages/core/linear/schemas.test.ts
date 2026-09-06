// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  DEFAULT_LINEAR_STATUS_MAP,
  EMPTY_LINEAR_INSTALLATION,
  LinearInstallationSchema,
  LinearLinkEnvelopeSchema,
  LinearOAuthStartSchema,
} from "./schemas";

// The canonical place these shapes are proven. The LinearTab suite covers the
// rendering and the wiring, not this matrix.

describe("LinearInstallationSchema", () => {
  it("parses a full installation", () => {
    const parsed = LinearInstallationSchema.parse({
      id: "inst-1",
      connected: true,
      configured: true,
      linear_org_id: "org_1",
      linear_org_name: "Acme",
      agent_id: "agent-1",
      agent_name: "Bridge",
      status: "active",
      last_error: "",
      status_map: { started: "in_progress" },
      installed_by: "user-1",
      created_at: "2026-09-05T00:00:00Z",
      updated_at: "2026-09-05T00:00:00Z",
      linear_state_types: ["backlog", "started"],
    });
    expect(parsed.linear_org_name).toBe("Acme");
    expect(parsed.status_map.started).toBe("in_progress");
  });

  it("keeps an unknown status rather than dropping the row", () => {
    // A newer server may add a status this build has never heard of. Refusing
    // the whole installation would hide a live connection behind a Connect
    // button; the components carry a default branch instead.
    const parsed = LinearInstallationSchema.parse({ id: "i", status: "quarantined" });
    expect(parsed.status).toBe("quarantined");
  });

  it("survives a malformed response field by field", () => {
    const parsed = LinearInstallationSchema.parse({
      id: "i",
      connected: "yes",
      status_map: 42,
      linear_state_types: "backlog",
      agent_name: null,
    });
    expect(parsed.connected).toBe(false);
    expect(parsed.status_map).toEqual({});
    expect(parsed.linear_state_types).toEqual([]);
    expect(parsed.agent_name).toBe("");
  });

  it("falls back to a disconnected installation when the payload is not an object", () => {
    // safeParse is what the client's parseWithFallback calls; a non-object
    // must fail so the caller substitutes EMPTY_LINEAR_INSTALLATION and the
    // tab offers Connect instead of claiming a connection.
    expect(LinearInstallationSchema.safeParse("nope").success).toBe(false);
    expect(EMPTY_LINEAR_INSTALLATION.connected).toBe(false);
  });
});

describe("LinearLinkEnvelopeSchema", () => {
  it("parses a link", () => {
    const parsed = LinearLinkEnvelopeSchema.parse({
      link: {
        id: "l1",
        issue_id: "i1",
        linear_issue_id: "iss_1",
        linear_issue_identifier: "ENG-7",
        linear_team_id: "team_1",
        linear_url: "https://linear.app/acme/issue/ENG-7",
        sync_state: "broken",
        last_synced_at: "2026-09-05T00:00:00Z",
        last_error: "token rejected",
      },
    });
    expect(parsed.link?.linear_issue_identifier).toBe("ENG-7");
    expect(parsed.link?.sync_state).toBe("broken");
  });

  it("treats a missing or malformed link as no link", () => {
    expect(LinearLinkEnvelopeSchema.parse({}).link).toBeNull();
    expect(LinearLinkEnvelopeSchema.parse({ link: 7 }).link).toBeNull();
  });
});

describe("LinearOAuthStartSchema", () => {
  it("defaults a missing authorize_url to empty so the caller can refuse to navigate", () => {
    expect(LinearOAuthStartSchema.parse({}).authorize_url).toBe("");
    expect(LinearOAuthStartSchema.parse({ authorize_url: "https://linear.app/oauth/authorize" }).authorize_url)
      .toBe("https://linear.app/oauth/authorize");
  });
});

describe("DEFAULT_LINEAR_STATUS_MAP", () => {
  it("matches the server's default map", () => {
    // Drift here is silent: the tab would show one mapping and the server
    // would apply another.
    expect(DEFAULT_LINEAR_STATUS_MAP).toEqual({
      triage: "todo",
      backlog: "backlog",
      unstarted: "todo",
      started: "in_progress",
      completed: "done",
      canceled: "cancelled",
    });
  });
});
