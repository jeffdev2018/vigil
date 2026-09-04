// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Preemption } from "@multica/core/issues/preemption";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/issues/preemption.test.ts.

const state = vi.hoisted(() => ({ preemptions: [] as Preemption[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("@multica/core/issues/preemption", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/preemption")>()),
  issuePreemptionsOptions: () => ({ queryKey: ["pre"], queryFn: async () => state.preemptions }),
}));

import { PreemptedBadge } from "./preempted-badge";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PreemptedBadge issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.preemptions = [];
});

describe("PreemptedBadge", () => {
  it("renders nothing without a preemption", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("names the urgent issue with a link while suspended, and the resumed state after", async () => {
    state.preemptions = [{ task_id: "t1", status: "paused", preempted_at: "2026-09-04T10:00:00Z", preempted_by_task_id: "u1", preempted_by_issue_id: "iss-9", preempted_by_identifier: "JEF-9", resumed_by_task_id: null }];
    render();
    expect(await screen.findByText("Suspended to let an urgent issue go first")).toBeTruthy();
    const link = screen.getByText("JEF-9") as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe("/acme/issues/iss-9");
    expect(screen.getByTestId("preempted-badge").getAttribute("data-waiting")).toBe("true");
    state.preemptions = [{ ...state.preemptions[0]!, status: "paused", resumed_by_task_id: "t2" }];
    render();
    expect(await screen.findByText("Was suspended for an urgent issue and resumed")).toBeTruthy();
  });
});
