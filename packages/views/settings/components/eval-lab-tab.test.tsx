// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { BenchmarkCorpus, BenchmarkRun, EvalCase, EvalRun, EvalSuite } from "@multica/core/eval";
import { renderWithI18n } from "../../test/i18n";

// Score banding and drift-tolerant parsing: packages/core/eval/schemas.test.ts.

const state = vi.hoisted(() => ({
  cases: [] as unknown[],
  suites: [] as unknown[],
  runs: [] as unknown[],
  versions: [] as unknown[],
  benchmarks: [] as unknown[],
  corpus: null as BenchmarkCorpus | null,
  runtimes: [] as unknown[],
  loading: false,
  create: vi.fn(),
  run: vi.fn(),
  benchmark: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    const key = options.queryKey[0];
    if (key === "eval-cases") return { data: state.cases, isLoading: state.loading };
    if (key === "eval-suites") return { data: state.suites, isLoading: state.loading };
    if (key === "eval-runs") return { data: state.runs, isLoading: false };
    if (key === "eval-benchmarks") return { data: state.benchmarks, isLoading: false };
    if (key === "eval-suite-corpus") return { data: state.corpus, isLoading: false };
    if (key === "runtimes") return { data: state.runtimes, isLoading: false };
    if (key === "agent-versions") return { data: options.enabled === false ? undefined : state.versions, isLoading: false };
    return { data: [{ id: "agent-1", name: "Alpha" }, { id: "agent-2", name: "Beta" }], isLoading: false };
  },
}));

vi.mock("@multica/core/eval", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/eval")>()),
  evalCasesOptions: (wsId: string) => ({ queryKey: ["eval-cases", wsId] }),
  evalSuitesOptions: (wsId: string) => ({ queryKey: ["eval-suites", wsId] }),
  evalRunsOptions: (wsId: string) => ({ queryKey: ["eval-runs", wsId] }),
  benchmarksOptions: (wsId: string) => ({ queryKey: ["eval-benchmarks", wsId] }),
  evalSuiteCorpusOptions: (suiteId: string) => ({ queryKey: ["eval-suite-corpus", suiteId] }),
  useCreateEvalSuite: () => ({ mutateAsync: state.create, isPending: false }),
  useRunEvalSuite: () => ({ mutate: state.run, isPending: false }),
  useRunBenchmark: () => ({ mutate: state.benchmark, isPending: false }),
}));

// The real runtimeDisplayLabel is kept: rendering a raw `runtime.name` in a
// <select> label is a convention violation, so the picker must go through it.
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: (wsId: string) => ({ queryKey: ["runtimes", wsId] }),
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

const benchmarkRun = (over: Partial<BenchmarkRun> = {}): BenchmarkRun => ({
  ...run(), id: "bench-1", benchmark: true, runtime_id: "rt-1", runtime_name: "Codex (host)",
  model: "gpt-5", baseline_run_id: null, per_class: {}, delta_score: null, ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  state.cases = [];
  state.suites = [];
  state.runs = [];
  state.versions = [{ id: "ver-1", version_number: 3, note: "tuned prompt" }, { id: "ver-2", version_number: 2, note: "" }];
  state.benchmarks = [];
  state.corpus = null;
  state.runtimes = [
    { id: "rt-1", name: "Codex (host)", custom_name: null, provider: "codex" },
    { id: "rt-2", name: "Claude (host)", custom_name: "Laptop", provider: "claude" },
  ];
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

// Delta banding, corpus ordering and drift-tolerant benchmark parsing:
// packages/core/eval/schemas.test.ts.
describe("EvalLabTab benchmarks", () => {
  it("benchmarks a suite against several runtime/model candidates", () => {
    state.suites = [suite()];
    state.benchmarks = [benchmarkRun({ id: "bench-old", score: 70, runtime_name: "Codex (host)", model: "gpt-5" })];
    renderWithI18n(<EvalLabTab />);

    fireEvent.click(within(screen.getByTestId("eval-suite")).getByRole("button", { name: "Benchmark" }));
    const submit = screen.getByTestId("eval-benchmark-form").querySelector("button[type=submit]") as HTMLButtonElement;
    // Nothing picked yet: an agent version and at least one candidate are required.
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "agent-1" } });
    fireEvent.change(screen.getByLabelText("Version"), { target: { value: "ver-1" } });
    expect(submit.disabled).toBe(true);

    // A custom alias must not hide which CLI backs the runtime.
    expect(screen.getByRole("option", { name: "Laptop (Claude)" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Runtime 1"), { target: { value: "rt-1" } });
    fireEvent.change(screen.getByLabelText("Model 1"), { target: { value: "gpt-5" } });
    expect(submit.disabled).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Add a candidate" }));
    fireEvent.change(screen.getByLabelText("Runtime 2"), { target: { value: "rt-2" } });
    // Second candidate keeps the empty model: the server reads it as "the
    // runtime's default", so it must be sent, not dropped.
    fireEvent.change(screen.getByLabelText("Baseline"), { target: { value: "bench-old" } });
    fireEvent.click(submit);

    expect(state.benchmark).toHaveBeenCalledWith(
      {
        suiteId: "suite-1",
        agent_id: "agent-1",
        agent_version_id: "ver-1",
        candidates: [{ runtime_id: "rt-1", model: "gpt-5" }, { runtime_id: "rt-2", model: "" }],
        baseline_run_id: "bench-old",
      },
      expect.anything(),
    );
  });

  it("drops a candidate row whose runtime was never picked", () => {
    state.suites = [suite()];
    renderWithI18n(<EvalLabTab />);
    fireEvent.click(within(screen.getByTestId("eval-suite")).getByRole("button", { name: "Benchmark" }));
    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "agent-1" } });
    fireEvent.change(screen.getByLabelText("Version"), { target: { value: "ver-1" } });
    fireEvent.change(screen.getByLabelText("Runtime 1"), { target: { value: "rt-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add a candidate" }));

    fireEvent.click(screen.getByTestId("eval-benchmark-form").querySelector("button[type=submit]") as HTMLButtonElement);
    expect(state.benchmark).toHaveBeenCalledWith(
      expect.objectContaining({ candidates: [{ runtime_id: "rt-1", model: "" }] }),
      expect.anything(),
    );
  });

  it("shows what the suite is made of and flags a skewed corpus", () => {
    state.suites = [suite()];
    state.corpus = {
      suite_id: "suite-1", suite_name: "Regression", cases: 4, balanced: false,
      classes: { docs: { count: 1, share: 0.25 }, bugfix: { count: 3, share: 0.75 } },
    };
    renderWithI18n(<EvalLabTab />);

    // Heaviest class first, whatever order the server serialised the map in.
    expect(screen.getByTestId("eval-corpus").textContent).toContain("Bug fix 3 (75%) · Docs 1 (25%)");
    expect(screen.getByTestId("eval-corpus-balance").textContent).toBe("Skewed");
  });

  it("lists benchmark runs with their per-class chips and delta", () => {
    state.benchmarks = [
      benchmarkRun({
        id: "bench-9", score: 82, delta_score: 12, runtime_name: "Codex (host)", model: "gpt-5",
        per_class: {
          bugfix: { cases: 3, passed: 3, score: 100, cost_usd_ticks: 4200, duration_seconds: 90 },
          docs: { cases: 1, passed: 0, score: 0, cost_usd_ticks: null, duration_seconds: null },
        },
      }),
    ];
    renderWithI18n(<EvalLabTab />);

    const row = within(screen.getByTestId("benchmark-run"));
    expect(row.getByText("Codex (host)")).toBeTruthy();
    expect(row.getByText("gpt-5")).toBeTruthy();
    expect(row.getByText("82/100")).toBeTruthy();
    expect(row.getAllByTestId("benchmark-class-chip").map((chip) => chip.textContent)).toEqual([
      "Bug fix 3/3",
      "Docs 0/1",
    ]);
    expect(row.getByTestId("benchmark-delta").textContent).toBe("+12");
    expect(screen.queryByTestId("benchmark-runs-empty")).toBeNull();
  });

  it("renders an unknown task class from a newer backend as its raw token", () => {
    // Regression: a class the client has never heard of used to render blank,
    // which reads as "this class scored nothing" instead of "new class".
    state.benchmarks = [
      benchmarkRun({
        model: "",
        delta_score: 0,
        per_class: { telemetry: { cases: 2, passed: 1, score: null, cost_usd_ticks: null, duration_seconds: null } },
      }),
    ];
    renderWithI18n(<EvalLabTab />);

    const row = within(screen.getByTestId("benchmark-run"));
    expect(row.getByTestId("benchmark-class-chip").textContent).toBe("telemetry 1/2");
    // An empty model means the runtime's default, not a missing value.
    expect(row.getByText("Default")).toBeTruthy();
    // No movement is a warning, not a regression.
    expect(row.getByTestId("benchmark-delta").className).toContain("text-warning");
  });
});
