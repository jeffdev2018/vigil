// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { CodeHealthScan, CodeHealthSettings } from "@multica/core/code-health";
import { renderWithI18n } from "../../test/i18n";

// Drift-tolerant parsing, the opened/skipped split and the status banding are
// covered canonically in packages/core/code-health/schemas.test.ts.

const state = vi.hoisted(() => ({
  settings: null as unknown,
  scans: [] as unknown[],
  scansError: false,
  agentsError: false,
  projectsError: false,
  refetchScans: vi.fn(),
  refetchAgents: vi.fn(),
  refetchProjects: vi.fn(),
  save: vi.fn(),
  trigger: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const key = options.queryKey[0];
    if (key === "code-health-settings")
      return { data: state.settings, isPending: state.settings === null };
    if (key === "code-health-scans")
      return { data: state.scans, isPending: false, isError: state.scansError, refetch: state.refetchScans };
    if (key === "projects")
      return {
        data: [{ id: "p1", title: "Core" }],
        isPending: false,
        isError: state.projectsError,
        refetch: state.refetchProjects,
      };
    return {
      data: [{ id: "agent-1", name: "Alpha" }],
      isPending: false,
      isError: state.agentsError,
      refetch: state.refetchAgents,
    };
  },
}));

vi.mock("@multica/core/code-health", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/code-health")>()),
  codeHealthSettingsOptions: (wsId: string) => ({ queryKey: ["code-health-settings", wsId] }),
  codeHealthScansOptions: (wsId: string) => ({ queryKey: ["code-health-scans", wsId] }),
  useSaveCodeHealthSettings: () => ({ mutate: state.save, isPending: false }),
  useTriggerCodeHealthScan: () => ({ mutate: state.trigger, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: (wsId: string) => ({ queryKey: ["projects", wsId] }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

// Mocked at the context module rather than the barrel so <AppLink> stays the
// real component and the rendered href is what the test asserts.
vi.mock("../../navigation/context", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=code-health"),
    hash: "",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { CodeHealthTab } from "./code-health-tab";

const settings = (over: Partial<CodeHealthSettings> = {}): CodeHealthSettings => ({
  enabled: false, cron: "0 3 * * 1", timezone: "UTC", agent_id: "", project_id: "",
  max_issues_per_scan: 5, min_confidence: 70, budget_policy_id: "", enabled_at: "",
  min_issues_allowed: 1, max_issues_allowed: 20, ...over,
});

const scan = (over: Partial<CodeHealthScan> = {}): CodeHealthScan => ({
  id: "scan-1", workspace_id: "ws-1", project_id: null, agent_id: "agent-1", task_id: "t1",
  status: "completed", issues_created: 1, error: "", created_at: "2026-09-01T00:00:00Z",
  completed_at: "2026-09-01T00:10:00Z",
  findings: [
    { kind: "tests", title: "Cover the claim fence", summary: "", paths: [], confidence: 90, effort: "M", evidence: "", issue_id: "issue-1", skipped: "" },
    { kind: "debt", title: "Maybe split the router", summary: "", paths: [], confidence: 40, effort: "L", evidence: "", issue_id: "", skipped: "low_confidence" },
  ],
  ...over,
});

beforeEach(() => {
  state.settings = settings();
  state.scans = [];
  state.scansError = false;
  state.agentsError = false;
  state.projectsError = false;
  state.refetchScans.mockReset();
  state.refetchAgents.mockReset();
  state.refetchProjects.mockReset();
  state.save.mockReset();
  state.trigger.mockReset();
  toast.success.mockReset();
  toast.error.mockReset();
});

describe("CodeHealthTab", () => {
  it("explains itself and refuses to scan before a maintenance agent is picked", () => {
    renderWithI18n(<CodeHealthTab />);
    expect(screen.getByText(/Nothing runs until you pick a maintenance agent/)).toBeTruthy();
    expect(screen.getByTestId("code-health-scan-now").getAttribute("disabled")).not.toBeNull();
    expect(screen.getByTestId("code-health-scans-empty")).toBeTruthy();
  });

  it("drops the onboarding once configured and saves what the admin submitted", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1", min_confidence: 80 });
    renderWithI18n(<CodeHealthTab />);
    expect(screen.queryByText(/Nothing runs until you pick a maintenance agent/)).toBeNull();

    fireEvent.change(screen.getByLabelText("Minimum confidence"), { target: { value: "55" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.save).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: true, agent_id: "agent-1", min_confidence: 55 }),
      expect.anything(),
    );
  });

  it("lists every finding with what happened to it, and links the issues it opened", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    state.scans = [scan()];
    renderWithI18n(<CodeHealthTab />);

    const row = screen.getByTestId("code-health-scan-row");
    expect(row.getAttribute("data-status")).toBe("completed");
    // The dropped finding is still on the record, with its reason.
    expect(screen.getByText(/below the confidence threshold/)).toBeTruthy();
    // Only the finding that opened an issue is a link.
    const link = screen.getByRole("link", { name: "Cover the claim fence" });
    expect(link.getAttribute("href")).toBe("/acme/issues/issue-1");
  });

  it("blocks a second scan while one is running", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    state.scans = [scan({ status: "running", issues_created: 0, findings: [] })];
    renderWithI18n(<CodeHealthTab />);
    expect(screen.getByTestId("code-health-scan-now").getAttribute("disabled")).not.toBeNull();
    expect(screen.getByTestId("code-health-scan-status").getAttribute("data-status")).toBe("running");
  });

  it("triggers a manual scan when nothing is running", () => {
    state.settings = settings({ enabled: false, agent_id: "agent-1" });
    renderWithI18n(<CodeHealthTab />);
    fireEvent.click(screen.getByTestId("code-health-scan-now"));
    expect(state.trigger).toHaveBeenCalled();
  });

  // Regression: scans/agents/projects ignored isError entirely — a failed
  // fetch fell through to the exact same "No scan yet" empty state as a
  // workspace that genuinely never scanned, with no way to tell them apart.
  it("shows an error state with retry instead of a false empty state when scans fail to load", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    state.scansError = true;
    renderWithI18n(<CodeHealthTab />);

    expect(screen.queryByTestId("code-health-scans-empty")).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load code health.");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchScans).toHaveBeenCalled();
  });

  it("shows the same error state when the agent picker's query fails", () => {
    state.settings = settings({ enabled: true, agent_id: "agent-1" });
    state.agentsError = true;
    renderWithI18n(<CodeHealthTab />);

    expect(screen.getByRole("alert")).toHaveTextContent("Could not load code health.");
  });
});
