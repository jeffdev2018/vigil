// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { PluginInstallation, PluginSurface } from "@multica/core/types";
import enIssues from "../locales/en/issues.json";

const bridge = vi.hoisted(() => ({ connect: vi.fn(), pushTheme: vi.fn(), close: vi.fn() }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { url: "https://plugin-content.example.test/s", bridge_token: "proof" },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@multica/core/plugins", () => ({ pluginSurfaceLaunchOptions: () => ({ queryKey: ["surface"] }) }));
vi.mock("./surface-bridge", () => ({ createSurfaceBridge: () => bridge }));

import { PluginSurfaceFrame } from "./plugin-surface-frame";

const installation = { id: "installation-1", name: "Hello", package_version_id: "v1" } as PluginInstallation;
const surface = { key: "hello", name: "Hello", type: "issue_panel" } as PluginSurface;

describe("PluginSurfaceFrame", () => {
  // Regression: the theme reached the surface once, at connect. Switching
  // light/dark left an open plugin panel painted in the old theme.
  it("pushes the new theme to a connected surface when the app theme changes", async () => {
    const ui = () => (
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <PluginSurfaceFrame wsId="ws-1" installation={installation} surface={surface} issueId="issue-1" />
      </I18nProvider>
    );
    render(ui());
    expect(bridge.connect).toHaveBeenCalledTimes(1);
    bridge.pushTheme.mockClear();

    // What the theme provider does on a switch: it swaps the root class.
    document.documentElement.classList.add("dark");

    await waitFor(() => expect(bridge.pushTheme).toHaveBeenCalledTimes(1));
    expect(bridge.connect).toHaveBeenCalledTimes(1);
    document.documentElement.classList.remove("dark");
  });
});
