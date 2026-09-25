// @vitest-environment node
import { describe, expect, it } from "vitest";
import { routeForNotification } from "./push-route";

const WORKSPACES = [
  { id: "ws-1", slug: "acme" },
  { id: "ws-2", slug: "globex" },
];

describe("routeForNotification (K64 tap-to-answer)", () => {
  it("routes a decision tap to the current workspace's decisions screen — the server payload pins no workspace", () => {
    expect(
      routeForNotification(
        { kind: "decision_request", issue_id: "i-1", decision_id: "d-1" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBe("/acme/inbox/decisions");
  });

  it("no-ops a decision tap when no workspace is active", () => {
    expect(
      routeForNotification(
        { kind: "decision_request", issue_id: "i-1", decision_id: "d-1" },
        { currentSlug: null, workspaces: WORKSPACES },
      ),
    ).toBeNull();
  });

  it("routes the morning briefing to the pinned workspace's decisions screen (workspace_id is a UUID, resolved to a slug)", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_id: "ws-2" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBe("/globex/inbox/decisions");
  });

  it("falls back to the current workspace's inbox tab when the pinned workspace_id is unknown", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_id: "ws-gone" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBe("/acme/inbox");
  });

  it("no-ops when the pinned workspace_id is unknown and no workspace is active", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_id: "ws-gone" },
        { currentSlug: null, workspaces: WORKSPACES },
      ),
    ).toBeNull();
  });

  it("falls back to the current workspace when the memberships list hasn't loaded (cold start)", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_id: "ws-2" },
        { currentSlug: "acme", workspaces: undefined },
      ),
    ).toBe("/acme/inbox");
  });

  it("accepts a pinned workspace_slug (future payload) that matches a membership", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_slug: "globex" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBe("/globex/inbox/decisions");
  });

  it("rejects a pinned workspace_slug that isn't a membership once the list is known", () => {
    expect(
      routeForNotification(
        { kind: "morning_briefing", workspace_slug: "bogus" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBe("/acme/inbox");
  });

  it("no-ops on an unknown kind", () => {
    expect(
      routeForNotification(
        { kind: "some_future_push", workspace_id: "ws-1" },
        { currentSlug: "acme", workspaces: WORKSPACES },
      ),
    ).toBeNull();
  });

  it("no-ops on missing or malformed payloads", () => {
    expect(routeForNotification({}, { currentSlug: "acme" })).toBeNull();
    expect(routeForNotification(null, { currentSlug: "acme" })).toBeNull();
    expect(routeForNotification("decision_request", { currentSlug: "acme" })).toBeNull();
    expect(routeForNotification({ kind: 42 }, { currentSlug: "acme" })).toBeNull();
  });
});
