// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// P3 audit finding: this tab's only useQuery ignored isError, so a failed
// fetch fell through to `preferences = {}` — every toggle rendered as its
// default state (enabled), silently misrepresenting a workspace whose real
// preferences (possibly all muted) simply failed to load.

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: vi.fn() }),
}));
vi.mock("./browser-notification-setting", () => ({
  BrowserNotificationSetting: () => null,
}));

const state = vi.hoisted(() => ({ fail: false }));
vi.mock("@multica/core/notification-preferences/queries", () => ({
  notificationPreferenceOptions: (wsId: string) => ({
    queryKey: ["notification-preferences", wsId],
    queryFn: async () => {
      if (state.fail) throw new Error("network down");
      return { workspace_id: wsId, preferences: {} };
    },
  }),
}));

import { NotificationsTab } from "./notifications-tab";

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NotificationsTab />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.fail = false;
});

describe("NotificationsTab", () => {
  it("renders the inbox group toggles once preferences load", async () => {
    renderTab();
    expect(await screen.findByLabelText("Assignments")).toBeInTheDocument();
  });

  it("shows a retry-able error instead of default-enabled toggles when the fetch fails", async () => {
    state.fail = true;
    renderTab();

    const alerts = await screen.findAllByRole("alert");
    expect(alerts.length).toBeGreaterThan(0);
    expect(screen.queryByLabelText("Assignments")).toBeNull();
  });
});
