// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useGettingStartedStore } from "@multica/core/onboarding";
import type { OnboardingChecklistResponse } from "@multica/core/onboarding";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// Behavior only — parsing/fallback for the checklist response is proven in
// packages/core/api/schemas.test.ts (OnboardingChecklistSchema).

const EMPTY_CHECKLIST: OnboardingChecklistResponse = {
  runtime_kind: "none",
  native_available: false,
  runtime_ready: false,
  agent_created: false,
  issue_created: false,
  first_run_completed: false,
  first_decision_answered: false,
  complete: false,
  agents: 0,
  issues: 0,
  completed_runs: 0,
};

const state = vi.hoisted(() => ({
  checklist: null as unknown,
}));

vi.mock("@multica/core/onboarding", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/onboarding")>()),
  onboardingChecklistOptions: (wsId: string) => ({
    queryKey: ["onboarding-checklist", wsId],
    queryFn: async () => state.checklist,
  }),
}));

import { GettingStartedCard } from "./getting-started-card";

const mockPush = vi.fn();
const navigationAdapter: NavigationAdapter = {
  push: (path: string) => mockPush(path),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/test",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path: string) => `https://test.local${path}`,
};

function renderCard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={navigationAdapter}>
        <GettingStartedCard wsId="ws-1" wsSlug="test-ws" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.checklist = EMPTY_CHECKLIST;
  useGettingStartedStore.setState({ dismissedByWorkspace: {} });
  mockPush.mockReset();
});

describe("GettingStartedCard", () => {
  it("renders every row with a link to where it happens", async () => {
    renderCard();
    expect(await screen.findByTestId("getting-started-card")).toBeInTheDocument();

    expect(screen.getByText(/connect a runtime/i).closest("a")).toHaveAttribute(
      "href",
      "/test-ws/runtimes",
    );
    expect(screen.getByText(/^create an agent$/i).closest("a")).toHaveAttribute(
      "href",
      "/test-ws/agents/new",
    );
    expect(screen.getByText(/^create an issue$/i).closest("a")).toHaveAttribute(
      "href",
      "/test-ws/issues",
    );
    expect(
      screen.getByText(/complete your first run/i).closest("a"),
    ).toHaveAttribute("href", "/test-ws/runs");
    expect(
      screen.getByText(/answer your first decision/i).closest("a"),
    ).toHaveAttribute("href", "/test-ws/inbox");
  });

  it("names the runtime kind once it is ready", async () => {
    state.checklist = { ...EMPTY_CHECKLIST, runtime_kind: "native", runtime_ready: true };
    renderCard();
    expect(await screen.findByText(/native runtime ready/i)).toBeInTheDocument();
  });

  it("marks the optional decision row so it reads as de-emphasized", async () => {
    renderCard();
    const row = await screen.findByText(/answer your first decision/i);
    expect(row.closest("a")).toHaveTextContent(/optional/i);
  });

  it("hides once the member dismisses it, and remembers that per workspace", async () => {
    renderCard();
    const dismissButton = await screen.findByRole("button", { name: /dismiss/i });
    fireEvent.click(dismissButton);
    await waitFor(() =>
      expect(screen.queryByTestId("getting-started-card")).not.toBeInTheDocument(),
    );
    expect(
      useGettingStartedStore.getState().dismissedByWorkspace["ws-1"],
    ).toBe(true);
  });

  it("auto-hides once the checklist reports complete, without touching the dismiss store", async () => {
    state.checklist = { ...EMPTY_CHECKLIST, complete: true };
    renderCard();
    await waitFor(() =>
      expect(screen.queryByTestId("getting-started-card")).not.toBeInTheDocument(),
    );
    expect(useGettingStartedStore.getState().dismissedByWorkspace["ws-1"]).toBeUndefined();
  });
});
