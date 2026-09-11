import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { LabelsTab } from "./labels-tab";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const refetchLabels = vi.hoisted(() => vi.fn());
let labelsError = false;

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false, isError: labelsError, refetch: refetchLabels }),
}));

vi.mock("@multica/core/labels", () => ({
  labelListOptions: (wsId: string, resourceType: string) => ({
    queryKey: ["labels", wsId, "list", resourceType],
  }),
  useCreateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateLabel: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteLabel: () => ({ mutate: vi.fn(), isPending: false }),
}));

describe("LabelsTab scopes", () => {
  afterEach(() => {
    cleanup();
    labelsError = false;
    refetchLabels.mockClear();
  });

  // Agent labels were removed from the product (MUL-5600). The backend still
  // models the `agent` resource type, so the guard here is that the settings
  // UI never offers it as a manageable catalog again.
  it("offers only the issue and skill catalogs", () => {
    renderWithI18n(<LabelsTab />);

    expect(screen.getByRole("button", { name: /Issues/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Skills/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Agents/ })).toBeNull();
  });

  // Regression: `data: labels = [], isLoading` never read isError — a failed
  // fetch fell through to the exact same empty state as a workspace with no
  // labels yet.
  it("shows an error state with retry instead of a false empty catalog when the fetch fails", () => {
    labelsError = true;
    renderWithI18n(<LabelsTab />);

    expect(screen.getByRole("alert")).toHaveTextContent("Could not load labels.");
    screen.getByRole("button", { name: "Retry" }).click();
    expect(refetchLabels).toHaveBeenCalled();
  });
});
