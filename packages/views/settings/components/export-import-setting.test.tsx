// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TransferPreview, TransferRun } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mocks = vi.hoisted(() => ({
  exportWorkspace: vi.fn(),
  previewWorkspaceImport: vi.fn(),
  importWorkspace: vi.fn(),
  listWorkspaceTransferRuns: vi.fn(async (): Promise<TransferRun[]> => []),
  saveTransferBlob: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: {
    exportWorkspace: mocks.exportWorkspace,
    previewWorkspaceImport: mocks.previewWorkspaceImport,
    importWorkspace: mocks.importWorkspace,
    listWorkspaceTransferRuns: mocks.listWorkspaceTransferRuns,
  },
}));
vi.mock("@multica/core/workspace/transfer", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/transfer")>()),
  saveTransferBlob: mocks.saveTransferBlob,
}));

import { ExportImportSetting } from "./export-import-setting";

const PREVIEW: TransferPreview = {
  manifest: {
    format_version: 1,
    exported_at: "2026-09-01T00:00:00Z",
    name: "Agency",
    template: false,
    source: { Name: "Agency HQ", Slug: "agency" },
    counts: { agent: 3, project: 2 },
    secrets: [],
  },
  collisions: [{ kind: "agent", name: "Mika", existing_id: "a-1" }],
  secrets: [{ scope: "agent", name: "Mika", key: "OPENAI_API_KEY", scoped: true }],
  strategies: ["rename", "merge", "skip"],
};

function renderSetting(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ExportImportSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

describe("ExportImportSetting", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listWorkspaceTransferRuns.mockResolvedValue([]);
  });

  it("exports with the chosen options and hands the blob to the browser", async () => {
    const blob = new Blob(["zip"]);
    mocks.exportWorkspace.mockResolvedValue({ blob, filename: "agency.zip", runId: "run-1" });
    renderSetting();
    fireEvent.click(screen.getByLabelText("Include issues"));
    fireEvent.click(screen.getByRole("switch", { name: "Save as template" }));
    fireEvent.change(screen.getByLabelText("Template name"), { target: { value: "Starter" } });
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    await waitFor(() => expect(mocks.saveTransferBlob).toHaveBeenCalledWith(blob, "agency.zip"));
    expect(mocks.exportWorkspace).toHaveBeenCalledWith({ include_issues: true, include_notes: false, template: true, name: "Starter" });
  });

  it("previews a picked bundle, imports with the strategy and typed secret, then shows the report", async () => {
    mocks.previewWorkspaceImport.mockResolvedValue(PREVIEW);
    mocks.importWorkspace.mockResolvedValue({
      run_id: "run-2",
      report: { created: { agent: 2, project: 2 }, merged: { agent: 1 }, skipped: [{ kind: "skill", name: "Docs", existing_id: "s-1" }], secrets_pending: [{ scope: "agent", name: "Mika", key: "SLACK_TOKEN", scoped: true }], warnings: ["autopilots imported paused"] },
    });
    renderSetting();
    const file = new File(["zip"], "agency.zip", { type: "application/zip" });
    fireEvent.change(screen.getByLabelText("Bundle file"), { target: { files: [file] } });

    await screen.findByTestId("transfer-preview");
    expect(mocks.previewWorkspaceImport).toHaveBeenCalledWith(file);
    expect(screen.getByText("From Agency HQ")).toBeInTheDocument();
    expect(screen.getByText("agent: 3")).toBeInTheDocument();
    expect(screen.getByText("agent · Mika")).toBeInTheDocument();
    expect(screen.getByText("agent · Mika · OPENAI_API_KEY")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("On collision"), { target: { value: "merge" } });
    fireEvent.change(screen.getByLabelText("Mika.OPENAI_API_KEY"), { target: { value: "sk-test" } });
    fireEvent.click(screen.getByRole("button", { name: "Import" }));

    await screen.findByTestId("transfer-report");
    expect(mocks.importWorkspace).toHaveBeenCalledWith(file, "merge", { Mika: { OPENAI_API_KEY: "sk-test" } });
    expect(screen.getByText("4 created · 1 merged")).toBeInTheDocument();
    expect(screen.getByText("Skipped: skill · Docs")).toBeInTheDocument();
    expect(screen.getByText("Secrets still pending: Mika.SLACK_TOKEN")).toBeInTheDocument();
    expect(screen.getByText("autopilots imported paused")).toBeInTheDocument();
  });

  it("lists past runs with a template badge, and an empty state otherwise", async () => {
    mocks.listWorkspaceTransferRuns.mockResolvedValue([
      { id: "r1", direction: "export", status: "completed", name: "Starter", template: true, strategy: "", source_name: "", bundle_sha256: "", report: {}, created_by: null, created_at: new Date().toISOString(), completed_at: null },
      { id: "r2", direction: "import", status: "failed", name: "", template: false, strategy: "skip", source_name: "Agency HQ", bundle_sha256: "", report: {}, created_by: null, created_at: new Date().toISOString(), completed_at: null },
    ]);
    renderSetting();
    const history = await screen.findByTestId("transfer-history");
    await waitFor(() => expect(history).toHaveTextContent("Starter"));
    expect(history).toHaveTextContent("Template");
    expect(history).toHaveTextContent("Agency HQ");
    expect(history).toHaveTextContent("Failed");
    expect(history).toHaveTextContent("skip");
  });

  it("shows the empty history state and disables controls without edit rights", async () => {
    renderSetting(false);
    expect(await screen.findByText("No export or import yet.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export" })).toBeDisabled();
    expect(screen.getByLabelText("Bundle file")).toBeDisabled();
  });
});
