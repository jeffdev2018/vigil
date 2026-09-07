// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CriticVerdict, CriticVerdictList } from "@multica/core/critic";
import { renderWithI18n } from "../../test/i18n";

// Parsing, the unknown-verdict fallback and the cost formatting are canonical
// in packages/core/critic/schemas.test.ts; this covers what the card renders.

const state = vi.hoisted(() => ({ list: null as CriticVerdictList | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/critic", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/critic")>()),
  criticVerdictsOptions: () => ({ queryKey: ["critic-verdicts"], queryFn: async () => state.list }),
}));

import { CriticVerdictCard } from "./critic-verdict-card";

const verdict = (over: Partial<CriticVerdict> = {}): CriticVerdict => ({
  id: "v1",
  issue_id: "i1",
  subject_task_id: "t1",
  critic_task_id: null,
  phase: "change",
  verdict: "pass",
  reason: "",
  summary: "",
  findings: [],
  round: 1,
  cost_usd_ticks: 0,
  created_at: "",
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CriticVerdictCard issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.list = null;
});

describe("CriticVerdictCard", () => {
  it("renders nothing when the issue was never criticised", async () => {
    state.list = { verdicts: [] };
    render();
    expect(screen.queryByTestId("critic-verdicts")).toBeNull();
  });

  it("shows the three verdicts with distinct pills", async () => {
    state.list = {
      verdicts: [
        verdict({ id: "a", verdict: "pass", round: 1 }),
        verdict({ id: "b", verdict: "concerns", round: 2 }),
        verdict({ id: "c", verdict: "block", round: 3 }),
      ],
    };
    render();
    await screen.findByTestId("critic-verdicts");
    const rows = screen.getAllByTestId("critic-verdict");
    expect(rows.map((r) => r.getAttribute("data-verdict"))).toEqual(["pass", "concerns", "block"]);
    // Several rounds are numbered, so a reader follows the argument rather
    // than only its end.
    expect(screen.getAllByTestId("critic-verdict-round").map((r) => r.textContent)).toEqual(["Round 1", "Round 2", "Round 3"]);
  });

  it("does not number a single round", async () => {
    state.list = { verdicts: [verdict()] };
    render();
    await screen.findByTestId("critic-verdicts");
    expect(screen.queryByTestId("critic-verdict-round")).toBeNull();
  });

  it("labels a skipped review with its reason instead of showing it as an opinion", async () => {
    state.list = { verdicts: [verdict({ verdict: "pass", reason: "no_distinct_provider" })] };
    render();
    const reason = await screen.findByTestId("critic-verdict-reason");
    expect(reason.textContent).toContain("no critic on another provider");
    // A degraded pass is not a warning: nothing went wrong with the delivery.
    expect(screen.getByTestId("critic-verdict").className).not.toContain("border-warning");
  });

  it("renders a budget stop as a warning", async () => {
    state.list = { verdicts: [verdict({ verdict: "concerns", reason: "max_rounds" })] };
    render();
    const row = await screen.findByTestId("critic-verdict");
    expect(row.className).toContain("border-warning");
    expect(screen.getByTestId("critic-verdict-reason").textContent).toContain("round limit");
  });

  it("clamps the summary and hides the findings until asked", async () => {
    state.list = {
      verdicts: [
        verdict({
          verdict: "block",
          summary: "the retry path drops the error",
          cost_usd_ticks: 15_000_000_000,
          critic_task_id: "task-9",
          findings: [
            { severity: "bug", file: "src/a.py", line: 7, title: "swallowed error", note: "the caller never sees it" },
            { severity: "info", file: "", line: 0, title: "naming", note: "" },
          ],
        }),
      ],
    };
    render();
    const summary = await screen.findByTestId("critic-verdict-summary");
    expect(summary.className).toContain("line-clamp-4");
    expect(screen.queryByTestId("critic-verdict-findings")).toBeNull();

    const toggle = screen.getByTestId("critic-verdict-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(screen.getByTestId("critic-verdict-toggle").getAttribute("aria-expanded")).toBe("true");
    const findings = screen.getByTestId("critic-verdict-findings");
    expect(findings.textContent).toContain("swallowed error");
    expect(findings.textContent).toContain("src/a.py:7");
    // A finding with no anchor still renders — a remark about the shape of a
    // change has no line to point at.
    expect(findings.textContent).toContain("naming");

    expect(screen.getByTestId("critic-verdict").textContent).toContain("$1.50");
    expect(screen.getByTestId("critic-verdict-run").getAttribute("href")).toContain("task-9");
  });
});
