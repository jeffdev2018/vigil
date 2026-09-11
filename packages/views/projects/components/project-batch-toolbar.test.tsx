// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockCreatePinAsync = vi.hoisted(() => vi.fn(async () => ({})));
const mockDeleteProjectAsync = vi.hoisted(() => vi.fn(async () => ({})));

vi.mock("@multica/core/pins", () => ({
  useCreatePin: () => ({ mutateAsync: mockCreatePinAsync }),
}));
vi.mock("@multica/core/projects", () => ({
  useDeleteProject: () => ({ mutateAsync: mockDeleteProjectAsync }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { toast } from "sonner";
import { ProjectBatchToolbar } from "./projects-page";

function makeProject(id: string): Project {
  return {
    id,
    workspace_id: "ws-1",
    title: `Project ${id}`,
    description: null,
    icon: null,
    status: "in_progress",
    priority: "high",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-06-01T00:00:00Z",
    updated_at: "2026-06-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  };
}

beforeEach(() => {
  mockCreatePinAsync.mockReset();
  mockCreatePinAsync.mockResolvedValue({});
  mockDeleteProjectAsync.mockReset();
  mockDeleteProjectAsync.mockResolvedValue({});
  vi.mocked(toast.error).mockClear();
});

// Regression: both bulk loops used to be plain `for (const p of rows) mutate(...)`
// — fire-and-forget, no per-row failure tracking, selection cleared/dialog
// closed unconditionally right after firing the loop.
describe("ProjectBatchToolbar — bulk pin", () => {
  it("pins every unpinned row and clears the selection on a clean run", async () => {
    const onClear = vi.fn();
    renderWithI18n(
      <ProjectBatchToolbar
        rows={[makeProject("a"), makeProject("b")]}
        pinnedIds={new Set()}
        canDelete={false}
        onClear={onClear}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Pin to sidebar/ }));

    await waitFor(() => expect(mockCreatePinAsync).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(onClear).toHaveBeenCalled());
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps the selection and shows one partial toast when one row fails", async () => {
    mockCreatePinAsync.mockImplementation(async ({ item_id }: { item_id: string }) => {
      if (item_id === "b") throw new Error("network blip");
      return {};
    });
    const onClear = vi.fn();
    renderWithI18n(
      <ProjectBatchToolbar
        rows={[makeProject("a"), makeProject("b")]}
        pinnedIds={new Set()}
        canDelete={false}
        onClear={onClear}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Pin to sidebar/ }));

    await waitFor(() => expect(mockCreatePinAsync).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(toast.error).toHaveBeenCalledWith("Pinned 1, 1 failed");
    expect(onClear).not.toHaveBeenCalled();
  });
});

describe("ProjectBatchToolbar — bulk delete", () => {
  it("deletes every row, closes the confirm dialog and clears the selection on a clean run", async () => {
    const onClear = vi.fn();
    renderWithI18n(
      <ProjectBatchToolbar
        rows={[makeProject("a"), makeProject("b")]}
        pinnedIds={new Set(["a", "b"])}
        canDelete
        onClear={onClear}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Delete/ }));
    await screen.findByText("Delete project");
    const deleteButtons = screen.getAllByRole("button", { name: "Delete" });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]!);

    await waitFor(() => expect(mockDeleteProjectAsync).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(onClear).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.queryByText("Delete project")).not.toBeInTheDocument(),
    );
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps the confirm dialog open and shows one partial toast when one row fails", async () => {
    mockDeleteProjectAsync.mockImplementation(async (id: string) => {
      if (id === "b") throw new Error("in use");
      return {};
    });
    const onClear = vi.fn();
    renderWithI18n(
      <ProjectBatchToolbar
        rows={[makeProject("a"), makeProject("b")]}
        pinnedIds={new Set(["a", "b"])}
        canDelete
        onClear={onClear}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Delete/ }));
    await screen.findByText("Delete project");
    const deleteButtons = screen.getAllByRole("button", { name: "Delete" });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]!);

    await waitFor(() => expect(mockDeleteProjectAsync).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(toast.error).toHaveBeenCalledWith("Deleted 1, 1 failed");
    expect(onClear).not.toHaveBeenCalled();
    // Dialog stays open so the failed row's confirm affordance is still reachable.
    expect(screen.getByText("Delete project")).toBeInTheDocument();
  });
});
