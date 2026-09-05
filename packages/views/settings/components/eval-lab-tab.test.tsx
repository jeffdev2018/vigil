// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { EvalCase, EvalRun, EvalSuite } from "@multica/core/eval";
import { renderWithI18n } from "../../test/i18n";

// Score banding and drift-tolerant parsing: packages/core/eval/schemas.test.ts.

const state = vi.hoisted(() => ({
  cases: [] as unknown[],
  suites: [] as unknown[],
  runs: [] as unknown[],
  versions: [] as unknown[],
  loading: false,
  create: vi.fn(),
  run: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    const key = options.queryKey[0];
    if (key === "eval-cases") return { data: state.cases, isLoading: state.loading };
    if (key === "eval-suites") return { data: state.suites, isLoading: state.loading };
    if (key === "eval-runs") return { data: state.runs, isLoading: false };
    if (key === "agent-versions") return { data: options.enabled === false ? undefined : state.versions, isLoading: false };
    return { data: [{ id: "agent-1", name: "Alpha" }, { id: "agent-2", name: "Beta" }], isLoading: false };
  },
}));

vi.mock("@multica/core/eval", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/eval")>()),
  evalCasesOptions: (wsId: string) => ({ queryKey: ["eval-cases", wsId] }),
  evalSuitesOptions: (wsId: string) => ({ queryKey: ["eval-suites", wsId] }),
  evalRunsOptions: (wsId: string) => ({ queryKey: ["eval-runs", wsId] }),
  useCreateEvalSuite: () => ({ mutateAsync: state.create, isPending: false }),
  useRunEvalSuite: () => ({ mutate: state.run, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
}));

vi.mock("@multica/core/agents/versions", () => ({
  agentVersionsOptions: (wsId: string, agentId: string) => ({ queryKey: ["agent-versions", wsId, agentId] }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { EvalLabTab } from "./eval-lab-tab";

const evalCase = (over: Partial<EvalCase> = {}): EvalCase => ({
  id: "case-1", workspace_id: "ws-1", source_issue_id: "i1", source_issue_number: 12,
  title: "Login flow", description: "", criteria: [{ id: "cr1", text: "logs in", proof_type: "test", proof_ref: "", proof_state: "satisfied" }],
  created_by: null, created_at: "2026-09-01T00:00:00Z", ...over,
});

const suite = (over: Partial<EvalSuite> = {}): EvalSuite => ({
  id: "suite-1", workspace_id: "ws-1", name: "Regression", case_ids: ["case-1"], case_count: 1,
  created_by: null, created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z", ...over,
});

const run = (over: Partial<EvalRun> = {}): EvalRun => ({
  id: "run-1", workspace_id: "ws-1", suite_id: "suite-1", suite_name: "Regression",
  agent_id: "agent-1", agent_version_id: "ver-1", agent_version_number: 3,
  status: "completed", score: 90, started_by: null, started_at: new Date().toISOString(),
  completed_at: null, cases: [], ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  state.cases = [];
  state.suites = [];
  state.runs = [];
  state.versions = [{ id: "ver-1", version_number: 3, note: "tuned prompt" }, { id: "ver-2", version_number: 2, note: "" }];
  state.loading = false;
  state.create.mockResolvedValue({});
});

describe("EvalLabTab", () => {
  it("tells the user to promote an issue first when nothing exists yet", () => {
    renderWithI18n(<EvalLabTab />);
    expect(screen.getByTestId("eval-suites-empty")).toBeTruthy();
    expect(screen.getByTestId("eval-cases-empty")).toBeTruthy();
    expect(screen.getByTestId("eval-runs-empty").textContent).toBe("No run yet");
    expect(screen.getAllByText(/Promote a resolved issue/).length).toBeGreaterThan(0);
    // Without a case there is nothing to name a suite after.
    expect(screen.queryByLabelText("Name")).toBeNull();
  });

  it("creates a suite from the promoted cases", async () => {
    state.cases = [evalCase(), evalCase({ id: "case-2", title: "Checkout", source_issue_number: 30 })];
    renderWithI18n(<EvalLabTab />);

    expect(screen.getByText("Issue #12 · 1 criteria")).toBeTruthy();
    const submit = screen.getByRole("button", { name: "Create suite" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Nightly" } });
    expect(submit.disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "Checkout" }));
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);

    await waitFor(() =>
      expect(state.create).toHaveBeenCalledWith({ name: "Nightly", case_ids: ["case-2"] }),
    );
    expect(toast.success).toHaveBeenCalled();
  });

  it("surfaces a create failure inline as a toast without clearing the form", async () => {
    state.cases = [evalCase()];
    state.create.mockRejectedValue(new Error("case not in workspace"));
    renderWithI18n(<EvalLabTab />);

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Nightly" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "Login flow" }));
    fireEvent.click(screen.getByRole("button", { name: "Create suite" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("case not in workspace"));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Nightly");
  });

  it("runs a suite against one agent version", () => {
    state.suites = [suite()];
    renderWithI18n(<EvalLabTab />);

    const row = within(screen.getByTestId("eval-suite"));
    expect(row.getByText(/1 cases/)).toBeTruthy();
    expect(row.getByText(/never run/)).toBeTruthy();

    fireEvent.click(row.getByRole("button", { name: "Run" }));
    const version = screen.getByLabelText("Version") as HTMLSelectElement;
    // No agent picked yet: the version list stays locked.
    expect(version.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "agent-2" } });
    expect(version.disabled).toBe(false);
    expect(screen.getByRole("option", { name: "v3 — tuned prompt" })).toBeTruthy();
    fireEvent.change(version, { target: { value: "ver-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    expect(state.run).toHaveBeenCalledWith(
      { suiteId: "suite-1", agent_id: "agent-2", agent_version_id: "ver-1" },
      expect.anything(),
    );
  });

  it("shows the last score and hides the run button while a run is in flight", () => {
    state.suites = [suite()];
    state.runs = [run({ status: "running", score: null }), run({ id: "run-0", score: 42 })];
    renderWithI18n(<EvalLabTab />);

    const row = within(screen.getByTestId("eval-suite"));
    expect(row.getByText(/last run by Alpha/)).toBeTruthy();
    // The newest run is the running one, which has no score yet.
    expect(row.getByText("—")).toBeTruthy();
    expect(row.queryByRole("button", { name: "Run" })).toBeNull();
    expect(row.getByText("Running")).toBeTruthy();
  });

  it("lists the run history and expands a run into its per-case verdicts", () => {
    state.runs = [
      run({
        id: "run-9",
        status: "failed",
        score: 33,
        cases: [
          { case_id: "case-1", case_title: "Login flow", issue_id: "i1", task_id: "t1", status: "passed", score: 100, detail: "3/3 criteria proved", settled_at: null },
          { case_id: "case-2", case_title: "Checkout", issue_id: "i2", task_id: "t2", status: "infra_failed", score: null, detail: "sandbox unavailable: docker missing", settled_at: null },
        ],
      }),
    ];
    renderWithI18n(<EvalLabTab />);

    const table = within(screen.getByRole("table"));
    expect(table.getByText("Alpha")).toBeTruthy();
    expect(table.getByText("v3")).toBeTruthy();
    expect(screen.getByTestId("eval-run-status").getAttribute("data-status")).toBe("failed");
    expect(table.getByText("33/100")).toBeTruthy();
    expect(screen.queryByTestId("eval-run-details")).toBeNull();

    fireEvent.click(table.getByRole("button", { name: "Regression" }));
    const details = within(screen.getByTestId("eval-run-details"));
    expect(details.getAllByTestId("eval-run-case")).toHaveLength(2);
    expect(details.getByText("Passed")).toBeTruthy();
    expect(details.getByText("Infrastructure failure")).toBeTruthy();
    expect(details.getByText("sandbox unavailable: docker missing")).toBeTruthy();
    expect(details.getByText("100/100")).toBeTruthy();
  });

  it("renders an unknown status from a newer server without breaking", () => {
    state.runs = [run({ status: "quantum" as EvalRun["status"], score: null })];
    renderWithI18n(<EvalLabTab />);
    expect(screen.getByTestId("eval-run-status").textContent).toBe("Unknown");
  });
});
