// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

// Client parsing / request body: packages/core/projects/decisions.test.ts.
// This file pins what the DIALOG hands that client — the endpoint refuses a
// seq it cannot find in the run (422 invalid_source), so the pairing of the
// cited message with the run it came from is the contract worth testing.

const state = vi.hoisted(() => ({
  issues: [] as Array<{ id: string; identifier: string; title: string }>,
  tasks: [] as Array<{ id: string; status: string; completed_at: string | null; created_at: string }>,
  messages: [] as Array<{ seq: number; type: string; content?: string }>,
  mutate: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", () => ({
  api: {
    listIssues: async () => ({ issues: state.issues, total: state.issues.length }),
    listTasksByIssue: async () => state.tasks,
    listTaskMessages: async () => state.messages,
  },
}));
vi.mock("@multica/core/projects/decisions", () => ({
  useCreateIssueDecision: () => ({ mutate: state.mutate, isPending: false }),
}));

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RecordDecisionDialog } from "./record-decision-dialog";

function renderDialog() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RecordDecisionDialog projectId="p1" open onOpenChange={vi.fn()} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.issues = [{ id: "i1", identifier: "JEFF-7", title: "Denormalize" }];
  state.tasks = [
    // Newer, but not completed: the endpoint only accepts a completed run.
    { id: "run-running", status: "running", completed_at: null, created_at: "2026-09-05T00:00:00Z" },
    { id: "run-old", status: "completed", completed_at: "2026-09-01T00:00:00Z", created_at: "2026-09-01T00:00:00Z" },
    { id: "run-latest", status: "completed", completed_at: "2026-09-03T00:00:00Z", created_at: "2026-09-03T00:00:00Z" },
  ];
  state.messages = [
    { seq: 1, type: "text", content: "Looked at the schema" },
    { seq: 2, type: "text", content: "Decided to keep it denormalized" },
    // Synthesized issue changes, not stored run messages — citing one is a 422.
    { seq: 3, type: "action" },
  ];
  state.mutate.mockReset();
});

/** Wait for the project's issues to land, then choose one. */
async function selectIssue() {
  await screen.findByRole("option", { name: /JEFF-7/ });
  fireEvent.change(screen.getByLabelText("Issue"), { target: { value: "i1" } });
}

/** …and then wait for its last completed run's messages. */
async function pickIssue() {
  await selectIssue();
  await waitFor(() =>
    expect(screen.getByLabelText("Cited run message").querySelectorAll("option").length).toBeGreaterThan(1),
  );
}

describe("RecordDecisionDialog", () => {
  it("submits exactly what the endpoint expects, against the last completed run", async () => {
    renderDialog();
    await pickIssue();

    fireEvent.change(screen.getByLabelText("Cited run message"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "  Keep it denormalized  " } });
    fireEvent.change(screen.getByLabelText("Context"), { target: { value: " one read path " } });
    fireEvent.change(screen.getByLabelText("Decision"), { target: { value: " No join " } });
    fireEvent.change(screen.getByLabelText("Consequences"), { target: { value: " writes fan out " } });

    fireEvent.click(screen.getByRole("button", { name: "Record" }));

    expect(state.mutate).toHaveBeenCalledTimes(1);
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({
      issueId: "i1",
      // Newest COMPLETED run, not the newest run — mirrors
      // GetLatestCompletedTaskForIssue. Sent explicitly so a run finishing
      // mid-dialog cannot rebind the seq we showed.
      runId: "run-latest",
      decision: {
        source_message_seq: 2,
        title: "Keep it denormalized",
        decision: "No join",
        context: "one read path",
        consequences: "writes fan out",
      },
    });
  });

  it("does not offer synthesized action rows as a citable message", async () => {
    renderDialog();
    await pickIssue();
    const values = Array.from(
      screen.getByLabelText("Cited run message").querySelectorAll("option"),
    ).map((o) => (o as HTMLOptionElement).value);
    expect(values).toEqual(["", "1", "2"]);
  });

  it("keeps Record disabled until an issue, a cited message, a title and a decision are all set", async () => {
    renderDialog();
    const record = () => screen.getByRole("button", { name: "Record" }) as HTMLButtonElement;
    expect(record().disabled).toBe(true);

    await pickIssue();

    fireEvent.change(screen.getByLabelText("Cited run message"), { target: { value: "2" } });
    expect(record().disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "t" } });
    expect(record().disabled).toBe(true);

    // Whitespace is not a decision — the server trims and then rejects it.
    fireEvent.change(screen.getByLabelText("Decision"), { target: { value: "   " } });
    expect(record().disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Decision"), { target: { value: "d" } });
    expect(record().disabled).toBe(false);
  });

  it("says so when the issue has no completed run to cite", async () => {
    state.tasks = [
      { id: "run-running", status: "running", completed_at: null, created_at: "2026-09-05T00:00:00Z" },
    ];
    renderDialog();
    await selectIssue();
    expect(await screen.findByTestId("record-decision-no-run")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Record" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
