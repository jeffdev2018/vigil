// @vitest-environment node
import { describe, expect, it } from "vitest";
import { resolveSettingsLocation, settingsHref } from "./settings-navigation";

describe("settings location", () => {
  it.each([
    ["issue", "preferences", "issue", null],
    ["chat", "preferences", "chat", null],
    ["github", "integrations", null, "github"],
    ["lark", "integrations", null, "lark"],
  ])("resolves the retired %s entry", (old, tab, section, integration) => {
    expect(resolveSettingsLocation(new URLSearchParams({ tab: old! }))).toEqual(
      { tab, section, integration },
    );
  });

  it("keeps labs on its own page", () => {
    // Upstream retired the tab while it was an empty container; we kept the
    // container, so the bookmark must land on it rather than redirect.
    expect(resolveSettingsLocation(new URLSearchParams({ tab: "labs" }))).toEqual({
      tab: "labs",
      section: null,
      integration: null,
    });
  });

  it("preserves unrelated query state while replacing page-specific state", () => {
    expect(
      settingsHref(
        "/acme/settings",
        new URLSearchParams("tab=preferences&section=chat&keep=1"),
        "integrations",
        { integration: "slack" },
      ),
    ).toBe("/acme/settings?tab=integrations&keep=1&integration=slack");
  });
});
