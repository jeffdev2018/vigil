// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const updateWorkspace = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { updateWorkspace: (...args: unknown[]) => updateWorkspace(...args) },
}));

import { PostmortemCostSetting } from "./postmortem-cost-setting";

const FIVE_DOLLARS = 5e10;

function workspace(ticks: number | null): Workspace {
  return {
    id: "ws-1",
    name: "Acme",
    slug: "acme",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACM",
    avatar_url: null,
    postmortem_cost_threshold_usd_ticks: ticks,
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
  };
}

function render(ticks: number | null, canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PostmortemCostSetting workspace={workspace(ticks)} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  updateWorkspace.mockReset();
  updateWorkspace.mockResolvedValue(workspace(FIVE_DOLLARS));
});

describe("PostmortemCostSetting", () => {
  it("hides the amount while the trigger is off", () => {
    render(null);

    expect(
      screen.getByLabelText("Draft a postmortem for expensive runs"),
    ).not.toBeChecked();
    expect(screen.queryByLabelText("Cost threshold ($)")).not.toBeInTheDocument();
  });

  it("arms the trigger with a default amount, in ticks", async () => {
    render(null);

    fireEvent.click(screen.getByLabelText("Draft a postmortem for expensive runs"));

    await waitFor(() =>
      expect(updateWorkspace).toHaveBeenCalledWith("ws-1", {
        postmortem_cost_threshold_usd_ticks: FIVE_DOLLARS,
      }),
    );
  });

  it("shows the stored threshold in dollars and saves an edit", async () => {
    render(FIVE_DOLLARS);

    const input = screen.getByLabelText("Cost threshold ($)") as HTMLInputElement;
    expect(input.value).toBe("5");

    fireEvent.change(input, { target: { value: "12.5" } });
    fireEvent.blur(input);

    await waitFor(() =>
      expect(updateWorkspace).toHaveBeenCalledWith("ws-1", {
        postmortem_cost_threshold_usd_ticks: 125e9,
      }),
    );
  });

  it("switches the trigger off with the documented 0 sentinel", async () => {
    render(FIVE_DOLLARS);

    fireEvent.click(screen.getByLabelText("Draft a postmortem for expensive runs"));

    await waitFor(() =>
      expect(updateWorkspace).toHaveBeenCalledWith("ws-1", {
        postmortem_cost_threshold_usd_ticks: 0,
      }),
    );
  });

  it("is read-only for a member who cannot manage the workspace", () => {
    render(FIVE_DOLLARS, false);

    expect(screen.getByLabelText("Cost threshold ($)")).toBeDisabled();
  });
});
