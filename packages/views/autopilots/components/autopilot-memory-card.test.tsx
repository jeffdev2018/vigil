// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";

const mockExportDaemon = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: { exportDaemon: (...args: unknown[]) => mockExportDaemon(...args) },
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({
    data: { content: "learned a thing", updated_at: "2026-01-01T00:00:00Z" },
    isLoading: false,
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { AutopilotMemoryCard } from "./autopilot-memory-card";

beforeEach(() => {
  mockExportDaemon.mockReset();
  vi.mocked(toast.error).mockClear();
});

// Regression: handleExport had no try/catch at all — a failed
// api.exportDaemon() rejected silently (bare `onClick={handleExport}`, no
// unhandledrejection handler anywhere), so clicking Export just did nothing.
describe("AutopilotMemoryCard — export failure", () => {
  it("shows a toast when the export request fails", async () => {
    mockExportDaemon.mockRejectedValue(new Error("daemon offline"));
    renderWithI18n(<AutopilotMemoryCard autopilotId="ap-1" />);

    fireEvent.click(screen.getByRole("button", { name: /Export DAEMON.md/ }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("daemon offline"));
  });

  it("does not show a toast when the export succeeds", async () => {
    mockExportDaemon.mockResolvedValue("# memory");
    // jsdom has no real anchor-click download machinery; only URL APIs need a stub.
    vi.stubGlobal("URL", { createObjectURL: vi.fn(() => "blob:mock"), revokeObjectURL: vi.fn() });
    renderWithI18n(<AutopilotMemoryCard autopilotId="ap-1" />);

    fireEvent.click(screen.getByRole("button", { name: /Export DAEMON.md/ }));

    await waitFor(() => expect(mockExportDaemon).toHaveBeenCalledWith("ap-1"));
    expect(toast.error).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
