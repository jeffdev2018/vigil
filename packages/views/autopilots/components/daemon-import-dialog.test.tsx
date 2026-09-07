// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DaemonImportPreview } from "@multica/core/autopilots";
import { renderWithI18n } from "../../test/i18n";

// Response parsing and the pure helpers are canonical in
// packages/core/autopilots/markdown.test.ts. What is tested here is the
// dialog's own wiring: what it shows for a valid preview, what it refuses to
// let the user submit, and the conflict choice.

const mocks = vi.hoisted(() => ({
  preview: vi.fn(),
  importDaemon: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: { previewDaemonImport: mocks.preview, importDaemon: mocks.importDaemon },
}));
vi.mock("@multica/core/autopilots", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/autopilots")>()),
  // The cron preview is a server round-trip of its own; stub it so the dialog
  // test is about the dialog.
  cronPreviewOptions: (_ws: string, expr: string) => ({
    queryKey: ["cron", expr],
    queryFn: async () => ({ next_runs: ["2026-09-05T09:00:00Z", "2026-09-06T09:00:00Z"] }),
  }),
}));

import { DaemonImportDialog } from "./daemon-import-dialog";

const DOC = `---
name: Nightly triage
role: Sort the inbound queue.
agent: Nova
---

# Nightly triage
`;

function validPreview(over: Partial<DaemonImportPreview> = {}): DaemonImportPreview {
  return {
    valid: true,
    errors: [],
    frontmatter: {
      name: "Nightly triage",
      role: "Sort the inbound queue.",
      agent: "Nova",
      outputs: "issue",
      triggers: [{ kind: "schedule", cron: "0 9 * * *", timezone: "UTC", label: "Morning" }],
    },
    body: "# Nightly triage\n",
    warnings: [],
    agent_id: "agent-1",
    agent_candidates: [],
    digest: "abc",
    unchanged: false,
    ...over,
  };
}

function renderDialog(props: Partial<React.ComponentProps<typeof DaemonImportDialog>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DaemonImportDialog open onOpenChange={() => {}} {...props} />
    </QueryClientProvider>,
  );
}

// The dialog debounces its preview by 300ms, so every assertion about what it
// shows has to wait for that round-trip to settle first. Waiting on the
// in-flight indicator to clear is the honest signal — a fixed sleep would be
// flaky in exactly the direction that hides a real regression.
async function typeDocument(user: ReturnType<typeof userEvent.setup>) {
  // paste, not type: the debounce would otherwise fire once per keystroke and
  // the test would spend its time waiting on timers.
  const box = screen.getByLabelText("DAEMON.md contents");
  await user.click(box);
  await user.paste(DOC);
  await waitFor(() => expect(mocks.preview).toHaveBeenCalled(), { timeout: 3000 });
  await waitFor(
    () => expect(screen.queryByText("Checking the declaration...")).toBeNull(),
    { timeout: 3000 },
  );
}

beforeEach(() => {
  mocks.preview.mockReset().mockResolvedValue(validPreview());
  mocks.importDaemon.mockReset().mockResolvedValue({
    status: "created",
    autopilot: { id: "ap-1" },
    triggers: [],
    skill_id: "sk-1",
    digest: "abc",
    warnings: [],
  });
});

describe("DaemonImportDialog", () => {
  it("shows what the server parsed, not what the client guessed", async () => {
    const user = userEvent.setup();
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText("Nightly triage")).toBeInTheDocument();
    expect(screen.getByText("Nova")).toBeInTheDocument();
    expect(screen.getByText("0 9 * * *")).toBeInTheDocument();
    // The body is the skill the agent will read, shown before writing anything.
    // Scoped to the rendered panel: the raw document is still in the textarea,
    // so an unscoped text query would pass on the input the user pasted.
    expect(screen.getByTestId("daemon-import-body")).toHaveTextContent("# Nightly triage");
    expect(mocks.preview).toHaveBeenCalledWith(DOC);
  });

  it("previews the next runs of a declared schedule", async () => {
    const user = userEvent.setup();
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText(/2026-09-05T09:00:00Z/)).toBeInTheDocument();
  });

  it("refuses to import a document the server called invalid, and says where", async () => {
    const user = userEvent.setup();
    mocks.preview.mockResolvedValue({
      valid: false,
      errors: [{ line: 5, message: 'unknown key "schedule"' }],
      body: "",
      warnings: [],
      agent_candidates: [],
      digest: "",
      unchanged: false,
    });
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText(/unknown key "schedule"/)).toBeInTheDocument();
    expect(screen.getByText("line 5")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();
    expect(mocks.importDaemon).not.toHaveBeenCalled();
  });

  it("keeps Import disabled until a document has been checked", () => {
    renderDialog();
    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();
  });

  it("surfaces an accepted-but-inert declaration instead of dropping it", async () => {
    const user = userEvent.setup();
    mocks.preview.mockResolvedValue(
      validPreview({ warnings: ["budget is recorded in the document but not enforced"] }),
    );
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText(/budget is recorded in the document but not enforced/)).toBeInTheDocument();
  });

  it("says a re-import would change nothing", async () => {
    const user = userEvent.setup();
    mocks.preview.mockResolvedValue(validPreview({ existing_autopilot_id: "ap-1", unchanged: true }));
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText(/already matches this file/)).toBeInTheDocument();
    // Nothing conflicts, so no strategy is asked for and Import stays available.
    expect(screen.queryByText("A daemon of this name already exists")).toBeNull();
    expect(screen.getByRole("button", { name: "Import" })).toBeEnabled();
  });

  it("asks how to resolve a name conflict before allowing the write", async () => {
    const user = userEvent.setup();
    mocks.preview.mockResolvedValue(validPreview({ existing_autopilot_id: "ap-9", unchanged: false }));
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText("A daemon of this name already exists")).toBeInTheDocument();
    // "Stop" is the default and it is not a choice that can be submitted:
    // importing anyway would 409, so the button must not offer it.
    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Replace it" }));
    expect(screen.getByRole("button", { name: "Import" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Import" }));
    await waitFor(() => expect(mocks.importDaemon).toHaveBeenCalledWith(DOC, "overwrite"), { timeout: 3000 });
  });

  it("imports and reports the new autopilot to its caller", async () => {
    const user = userEvent.setup();
    const onImported = vi.fn();
    const onOpenChange = vi.fn();
    renderDialog({ onImported, onOpenChange });
    await typeDocument(user);

    await waitFor(() => expect(screen.getByRole("button", { name: "Import" })).toBeEnabled(), { timeout: 3000 });
    await user.click(screen.getByRole("button", { name: "Import" }));

    await waitFor(() => expect(onImported).toHaveBeenCalledWith("ap-1"), { timeout: 3000 });
    expect(mocks.importDaemon).toHaveBeenCalledWith(DOC, "fail");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps the dialog open and shows the reason when the import fails", async () => {
    const user = userEvent.setup();
    mocks.importDaemon.mockRejectedValue(new Error("agent not found"));
    const onOpenChange = vi.fn();
    renderDialog({ onOpenChange });
    await typeDocument(user);

    await waitFor(() => expect(screen.getByRole("button", { name: "Import" })).toBeEnabled(), { timeout: 3000 });
    await user.click(screen.getByRole("button", { name: "Import" }));

    expect(await screen.findByText("agent not found")).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("names the agent the declaration could not resolve", async () => {
    const user = userEvent.setup();
    mocks.preview.mockResolvedValue(validPreview({ agent_id: undefined, agent_candidates: ["Nova", "Orion"] }));
    renderDialog();
    await typeDocument(user);

    expect(await screen.findByText(/no agent by that name in this workspace/)).toBeInTheDocument();
  });
});
