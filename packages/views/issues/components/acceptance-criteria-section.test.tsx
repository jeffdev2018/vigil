// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AcceptanceCriterion } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Helpers and schema fallbacks: packages/core/issues/acceptance.test.ts.

const state = vi.hoisted(() => ({
  criteria: [] as AcceptanceCriterion[],
  set: vi.fn(),
  prove: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/acceptance", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/acceptance")>()),
  acceptanceCriteriaOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "acceptance", issueId],
    queryFn: async () => state.criteria,
  }),
  useSetAcceptanceCriteria: () => ({ mutate: state.set, isPending: false }),
  useProveAcceptanceCriterion: () => ({ mutate: state.prove, isPending: false }),
}));

import { AcceptanceCriteriaSection } from "./acceptance-criteria-section";

const missing: AcceptanceCriterion = { id: "a", text: "Tests pass", proof_state: "missing" };
const proven: AcceptanceCriterion = {
  id: "b", text: "Wording reviewed", proof_type: "human_validation", proof_state: "satisfied", validated_by: "u1",
};

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AcceptanceCriteriaSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.criteria = [];
  state.set.mockReset();
  state.prove.mockReset();
});

describe("AcceptanceCriteriaSection", () => {
  it("offers only an add affordance without criteria, then adds one on Enter", async () => {
    renderSection();
    fireEvent.click(await screen.findByText("Add an acceptance criterion"));
    const input = screen.getByLabelText("Add a criterion…");
    fireEvent.change(input, { target: { value: "  Build is green " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(state.set).toHaveBeenCalledWith({ issueId: "i1", criteria: [{ text: "Build is green" }] }, expect.anything());
  });

  it("shows each criterion with its proof state and the progress count", async () => {
    state.criteria = [missing, proven];
    renderSection();
    expect(await screen.findByTestId("acceptance-progress")).toHaveTextContent("1/2");
    const rows = screen.getAllByTestId("acceptance-criterion");
    expect(rows.map((r) => r.dataset.state)).toEqual(["missing", "satisfied"]);
    expect(screen.getByText(/Human validation/)).toBeTruthy();
  });

  it("attaches a typed proof and validates as a human", async () => {
    state.criteria = [missing];
    renderSection();
    fireEvent.click(await screen.findByText("Attach a proof"));
    fireEvent.change(screen.getByLabelText("Proof type"), { target: { value: "url" } });
    fireEvent.change(screen.getByLabelText("Test command, path or URL"), { target: { value: "https://ci/1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.prove).toHaveBeenCalledWith(
      { issueId: "i1", criterionId: "a", proof_type: "url", proof_ref: "https://ci/1" },
      expect.anything(),
    );
    // The mocked mutate never resolves, so the form stays open; leave it.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByText("Validate myself"));
    expect(state.prove).toHaveBeenLastCalledWith(
      { issueId: "i1", criterionId: "a", proof_type: "human_validation", proof_ref: undefined },
      expect.anything(),
    );
  });

  it("removes a criterion by resubmitting the others", async () => {
    state.criteria = [missing, proven];
    renderSection();
    fireEvent.click((await screen.findAllByLabelText("Remove"))[0]!);
    expect(state.set).toHaveBeenCalledWith({ issueId: "i1", criteria: [{ id: "b", text: "Wording reviewed" }] }, expect.anything());
  });
});
