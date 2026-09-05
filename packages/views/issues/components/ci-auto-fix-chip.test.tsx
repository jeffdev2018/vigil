// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CIAutoFixRun, IssueCIAutoFix } from "@multica/core/issues/ci-auto-fix";
import { renderWithI18n } from "../../test/i18n";

// State derivation: packages/core/issues/ci-auto-fix.test.ts.

const state = vi.hoisted(() => ({ retry: vi.fn() }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@multica/core/issues/ci-auto-fix", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/ci-auto-fix")>()),
  useRetryCIAutoFix: () => ({ mutate: state.retry, isPending: false }),
}));

import { CIAutoFixChip } from "./ci-auto-fix-chip";

const run = (task_status: string, attempt: number): CIAutoFixRun => ({ id: "r" + attempt, provider: "vcs", pull_request_id: "pr1", head_sha: "sha", issue_id: "i1", task_id: "t", task_status, attempt, budget_usd_ticks: 0, manual: false, created_at: "" });

function render(data: IssueCIAutoFix | undefined) {
  const qc = new QueryClient();
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CIAutoFixChip issueId="i1" wsId="ws-1" pullRequestId="pr1" data={data} />
    </QueryClientProvider>,
  );
}

describe("CIAutoFixChip", () => {
  it("renders nothing without runs or when the feature is off", () => {
    expect(render(undefined).container.innerHTML).toBe("");
    expect(render({ runs: [], enabled: false, max_attempts: 3 }).container.innerHTML).toBe("");
  });

  it("shows progress, success and the exhausted state with a manual retry", () => {
    const a = render({ runs: [run("running", 1)], enabled: true, max_attempts: 3 });
    expect(a.getByTestId("ci-auto-fix-chip").getAttribute("data-state")).toBe("in_progress");
    a.unmount();
    const b = render({ runs: [run("completed", 1)], enabled: true, max_attempts: 3 });
    expect(b.getByText("CI fixed automatically")).toBeTruthy();
    b.unmount();
    render({ runs: [run("failed", 2), run("failed", 1)], enabled: true, max_attempts: 2 });
    expect(screen.getByText("CI still red after 2 attempts")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.retry).toHaveBeenCalledWith("pr1", expect.anything());
  });
});
