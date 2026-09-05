import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  configureShortcutPlatform,
  createShortcutChord,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { GlobalShortcuts } from "./global-shortcuts";

// The destination map is the only place a `go*` action becomes a route; a new
// action added to GLOBAL_ACTIONS without an entry there is a silent no-op.
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  useSidebar: () => ({ toggleSidebar: vi.fn() }),
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: { getState: () => ({ floatingChatEnabled: false }) },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/w/inbox",
    triage: () => "/w/triage",
    chat: () => "/w/chat",
    myIssues: () => "/w/my-issues",
    issues: () => "/w/issues",
    projects: () => "/w/projects",
    autopilots: () => "/w/autopilots",
    agents: () => "/w/agents",
    squads: () => "/w/squads",
    usage: () => "/w/usage",
    runtimes: () => "/w/runtimes",
    skills: () => "/w/skills",
    settings: () => "/w/settings",
  }),
}));

describe("GlobalShortcuts navigation destinations", () => {
  beforeEach(() => {
    configureShortcutPlatform("macos");
  });

  afterEach(() => {
    useShortcutStore.getState().resetShortcut("goTriage");
    configureShortcutPlatform(null);
  });

  it("navigates to the triage queue on the bound chord", () => {
    const chord = createShortcutChord("G", { primary: true, shift: true });
    useShortcutStore.getState().setShortcut("goTriage", chord);

    const push = vi.fn();
    const adapter: NavigationAdapter = {
      push,
      replace: vi.fn(),
      back: vi.fn(),
      pathname: "/w/issues",
      searchParams: new URLSearchParams(),
      hash: "",
      getShareableUrl: (path) => path,
    };
    const { unmount } = render(
      <NavigationProvider value={adapter}>
        <GlobalShortcuts />
      </NavigationProvider>,
    );

    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "G", metaKey: true, shiftKey: true, bubbles: true, cancelable: true }),
    );
    expect(push).toHaveBeenCalledWith("/w/triage");
    unmount();
  });
});
