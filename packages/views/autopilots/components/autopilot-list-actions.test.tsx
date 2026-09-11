// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { Autopilot } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockDeleteAutopilot = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/autopilots", () => ({
  useDeleteAutopilot: () => ({ mutateAsync: mockDeleteAutopilot }),
  useUpdateAutopilot: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ autopilotDetail: (id: string) => `/autopilots/${id}` }),
}));
vi.mock("../../navigation", () => ({
  useIntentNavigate: () => vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { toast } from "sonner";
import { DeleteAutopilotsDialog } from "./autopilot-list-actions";

function makeRow(id: string, title = `Autopilot ${id}`): Autopilot {
  return { id, title } as Autopilot;
}

beforeEach(() => {
  mockDeleteAutopilot.mockReset();
  vi.mocked(toast.error).mockClear();
});

// Regression: the batch delete loop used to be a plain `for...await` that
// stopped at the first rejection, left the dialog open on ANY failure (even
// once earlier rows genuinely deleted), and never told the user which rows
// succeeded. DeleteAutopilotsDialog now runs every row independently via
// runBulk and reports a single partial-failure summary.
describe("DeleteAutopilotsDialog — partial failure", () => {
  it("keeps the dialog open and shows one summary toast when one row fails", async () => {
    mockDeleteAutopilot.mockImplementation(async (id: string) => {
      if (id === "b") throw new Error("network blip");
      return {};
    });
    const onOpenChange = vi.fn();
    const onDeleted = vi.fn();

    renderWithI18n(
      <DeleteAutopilotsDialog
        rows={[makeRow("a"), makeRow("b"), makeRow("c")]}
        open
        onOpenChange={onOpenChange}
        onDeleted={onDeleted}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Delete permanently" }));

    await waitFor(() => expect(mockDeleteAutopilot).toHaveBeenCalledTimes(3));
    // All three rows attempted (order-independent — allSettled runs them in parallel).
    expect(mockDeleteAutopilot.mock.calls.map((c) => c[0]).sort()).toEqual(["a", "b", "c"]);

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(toast.error).toHaveBeenCalledWith("2 deleted, 1 failed.");

    // Partial failure: dialog stays open, onDeleted never fires.
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("closes the dialog and fires onDeleted when every row succeeds", async () => {
    mockDeleteAutopilot.mockResolvedValue({});
    const onOpenChange = vi.fn();
    const onDeleted = vi.fn();

    renderWithI18n(
      <DeleteAutopilotsDialog
        rows={[makeRow("a"), makeRow("b")]}
        open
        onOpenChange={onOpenChange}
        onDeleted={onDeleted}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Delete permanently" }));

    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(toast.error).not.toHaveBeenCalled();
  });
});
