// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { DocDriftProposal, DocDriftSettings } from "@multica/core/doc-drift";
import { renderWithI18n } from "../../test/i18n";

// Drift-tolerant parsing, the open-state rule, the status banding and the
// excerpt are covered canonically in packages/core/doc-drift/schemas.test.ts.

const state = vi.hoisted(() => ({
  settings: null as unknown,
  proposals: [] as unknown[],
  save: vi.fn(),
  check: vi.fn(),
  dismiss: vi.fn(),
  openPR: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const key = options.queryKey[0];
    if (key === "doc-drift-settings")
      return { data: state.settings, isPending: state.settings === null };
    if (key === "doc-drift-proposals") return { data: state.proposals, isPending: false };
    return { data: [{ id: "agent-1", name: "Alpha" }], isPending: false };
  },
}));

vi.mock("@multica/core/doc-drift", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/doc-drift")>()),
  docDriftSettingsOptions: (wsId: string) => ({ queryKey: ["doc-drift-settings", wsId] }),
  docDriftProposalsOptions: (wsId: string) => ({ queryKey: ["doc-drift-proposals", wsId] }),
  useSaveDocDriftSettings: () => ({ mutate: state.save, isPending: false }),
  useCheckDocDrift: () => ({ mutate: state.check, isPending: false }),
  useDismissDocDriftProposal: () => ({ mutate: state.dismiss, isPending: false }),
  useOpenDocDriftProposalPR: () => ({ mutate: state.openPR, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { DocDriftTab } from "./doc-drift-tab";

const settings = (over: Partial<DocDriftSettings> = {}): DocDriftSettings => ({
  enabled: false,
  agent_id: "",
  docs: ["CLAUDE.md", "AGENTS.md"],
  open_pr: true,
  last_checked: {},
  scan_tasks: {},
  repos: [],
  ...over,
});

const repo = (over: Partial<DocDriftSettings["repos"][number]> = {}) => ({
  repo_identifier: "git@example.com:team/app.git",
  last_indexed_commit: "def5678",
  last_checked_commit: "abc1234",
  due: true,
  scanning: false,
  ...over,
});

const proposal = (over: Partial<DocDriftProposal> = {}): DocDriftProposal => ({
  id: "p1",
  workspace_id: "ws-1",
  repo_identifier: "git@example.com:team/app.git",
  doc_path: "CLAUDE.md",
  detected_drift: "The commands section names a target that is gone.",
  proposed_patch: "-make serve\n+make server",
  detected_at_commit: "def5678",
  status: "draft",
  pull_request_url: "",
  scan_task_id: "t1",
  pr_task_id: null,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:10:00Z",
  ...over,
});

beforeEach(() => {
  state.settings = settings();
  state.proposals = [];
  state.save.mockReset();
  state.check.mockReset();
  state.dismiss.mockReset();
  state.openPR.mockReset();
  toast.success.mockReset();
  toast.error.mockReset();
});

describe("DocDriftTab", () => {
  it("explains itself and checks nothing before an agent is picked", () => {
    state.settings = settings({ repos: [repo()] });
    renderWithI18n(<DocDriftTab />);
    expect(screen.getByText(/Nothing runs until you pick an agent/)).toBeTruthy();
    expect(screen.getByTestId("doc-drift-check-now").getAttribute("disabled")).not.toBeNull();
  });

  it("says the document is up to date rather than showing an empty table", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    renderWithI18n(<DocDriftTab />);
    expect(screen.getByTestId("doc-drift-proposals-empty").textContent).toContain(
      "No drift detected",
    );
    // No indexed repository is its own empty state: this feature runs on the
    // shared index's commit, so there is nothing to check without one.
    expect(screen.getByTestId("doc-drift-repos-empty")).toBeTruthy();
  });

  it("saves the documents the admin typed, one per line", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    renderWithI18n(<DocDriftTab />);
    fireEvent.change(screen.getByLabelText("Documents"), {
      target: { value: "CLAUDE.md\n\n docs/conventions.mdx " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.save).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        agent_id: "agent-1",
        docs: ["CLAUDE.md", "docs/conventions.mdx"],
      }),
      expect.anything(),
    );
  });

  it("checks one repository on demand, and not while its scan is in flight", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1", repos: [repo()] });
    renderWithI18n(<DocDriftTab />);
    fireEvent.click(screen.getByTestId("doc-drift-check-now"));
    expect(state.check).toHaveBeenCalledWith(
      "git@example.com:team/app.git",
      expect.anything(),
    );

    state.settings = settings({
      enabled: true,
      agent_id: "agent-1",
      repos: [repo({ scanning: true })],
    });
    renderWithI18n(<DocDriftTab />);
    expect(
      screen.getAllByTestId("doc-drift-check-now").at(-1)?.getAttribute("disabled"),
    ).not.toBeNull();
  });

  it("offers the draft pull request and the dismissal on an open proposal", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1", repos: [repo()] });
    state.proposals = [proposal()];
    renderWithI18n(<DocDriftTab />);

    expect(screen.getByTestId("doc-drift-proposal-status").getAttribute("data-status")).toBe("draft");
    expect(screen.getByText(/names a target that is gone/)).toBeTruthy();

    fireEvent.click(screen.getByTestId("doc-drift-open-pr-action"));
    expect(state.openPR).toHaveBeenCalledWith("p1", expect.anything());

    fireEvent.click(screen.getByTestId("doc-drift-dismiss"));
    expect(state.dismiss).toHaveBeenCalledWith("p1", expect.anything());
  });

  it("links the draft pull request and stops offering to open a second one", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1", repos: [repo()] });
    state.proposals = [
      proposal({
        status: "opened_pr",
        pull_request_url: "https://example.test/acme/app/pull/42",
        pr_task_id: "t2",
      }),
    ];
    renderWithI18n(<DocDriftTab />);

    // Base UI renders the link-styled Button as an <a> carrying role="button"
    // (the repo-wide `nativeButton={false}` pattern), so this asserts on the
    // href rather than on the link role.
    const link = screen.getByTestId("doc-drift-pr-link");
    expect(link.getAttribute("href")).toBe("https://example.test/acme/app/pull/42");
    expect(link.textContent).toContain("Draft pull request");
    expect(screen.queryByTestId("doc-drift-open-pr-action")).toBeNull();
    // Still dismissible: an open pull request nobody wants is still a decision.
    expect(screen.getByTestId("doc-drift-dismiss")).toBeTruthy();
  });

  it("keeps a dismissed proposal readable and inert", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1", repos: [repo()] });
    state.proposals = [proposal({ status: "dismissed" })];
    renderWithI18n(<DocDriftTab />);
    expect(screen.getByTestId("doc-drift-proposal-status").getAttribute("data-status")).toBe("dismissed");
    expect(screen.queryByTestId("doc-drift-dismiss")).toBeNull();
    expect(screen.queryByTestId("doc-drift-open-pr-action")).toBeNull();
  });
});
