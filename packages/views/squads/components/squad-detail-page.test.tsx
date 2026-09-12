// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSquads from "../../locales/en/squads.json";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const api = vi.hoisted(() => ({
  getSquad: vi.fn((_id: string) => new Promise(() => {})),
  listSquadMembers: vi.fn((_id: string) => new Promise(() => {})),
  getSquadMemberStatus: vi.fn((_id: string) => new Promise(() => {})),
}));

vi.mock("@multica/core/api", () => ({ api }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: null }) => unknown) => selector({ user: null }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", name: "Acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { squads: (wsId: string) => ["squads", wsId] },
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => new Promise(() => {}) }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: () => new Promise(() => {}) }),
  squadMemberStatusOptions: (_wsId: string, squadId: string) => ({
    queryKey: ["squad-status", squadId],
    queryFn: () => api.getSquadMemberStatus(squadId),
  }),
}));

import { SquadDetailPage } from "./squad-detail-page";

describe("SquadDetailPage", () => {
  // Regression: the id was read off the shared pathname, so while desktop was
  // on /acme/triage the page asked the API for squad "triage" (three 400s).
  it("loads the squad the route names, not the last pathname segment", async () => {
    const navigation: NavigationAdapter = {
      push: vi.fn(),
      replace: vi.fn(),
      back: vi.fn(),
      pathname: "/acme/triage",
      searchParams: new URLSearchParams(),
      hash: "",
      getShareableUrl: (path) => path,
    };
    render(
      <I18nProvider locale="en" resources={{ en: { common: enCommon, squads: enSquads } }}>
        <NavigationProvider value={navigation}>
          <QueryClientProvider client={new QueryClient()}>
            <SquadDetailPage squadId="squad-1" />
          </QueryClientProvider>
        </NavigationProvider>
      </I18nProvider>,
    );

    await waitFor(() => expect(api.getSquad).toHaveBeenCalledWith("squad-1"));
    expect(api.listSquadMembers).toHaveBeenCalledWith("squad-1");
    expect(api.getSquadMemberStatus).toHaveBeenCalledWith("squad-1");
    expect(api.getSquad).not.toHaveBeenCalledWith("triage");
  });
});
