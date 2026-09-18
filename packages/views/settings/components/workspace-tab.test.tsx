import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockLeaveAsync = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const mockReleaseLeaveGuard = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    description: "",
    context: "",
    issue_prefix: "TES",
    repos: [] as { url: string }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as "owner" | "admin" | "member" }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: membersRef.current, isFetched: true }),
  useQueryClient: () => ({
    setQueryData: vi.fn(),
    getQueryData: vi.fn(() => []),
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueKeys: { all: (workspaceId: string) => ["issues", workspaceId] },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useLeaveWorkspace: () => ({ mutateAsync: mockLeaveAsync }),
  useDeleteWorkspace: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/workspace/pending-delete", () => ({
  unmarkWorkspaceLeavePending: mockReleaseLeaveGuard,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (selector?: (state: { user: { id: string } }) => unknown) =>
      selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: mockPush,
    pathname: "/test-workspace/settings",
    searchParams: new URLSearchParams("tab=workspace"),
  }),
  AppLink: ({ href, children }: { href: string; children?: ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

// Module ownership (K33) has its own queries and tests; the tab test keeps
// to the workspace fields.
vi.mock("./module-ownership-setting", () => ({ ModuleOwnershipSetting: () => null }));
vi.mock("./morning-briefing-setting", () => ({ MorningBriefingSetting: () => null }));
vi.mock("./competency-setting", () => ({ CompetencySetting: () => null }));
vi.mock("./workflow-limits-setting", () => ({ WorkflowLimitsSetting: () => null }));
vi.mock("./mcp-server-setting", () => ({ MCPServerSetting: () => null }));
vi.mock("./data-residency-setting", () => ({ DataResidencySetting: () => null }));
vi.mock("./batch-window-setting", () => ({ BatchWindowSetting: () => null }));
vi.mock("./workflow-policy-setting", () => ({ WorkflowPolicySetting: () => null }));
vi.mock("./cross-review-setting", () => ({ CrossReviewSetting: () => null }));
vi.mock("./contest-setting", () => ({ ContestSetting: () => null }));
vi.mock("./export-import-setting", () => ({ ExportImportSetting: () => null }));
vi.mock("./confidence-review-setting", () => ({ ConfidenceReviewSetting: () => null }));
vi.mock("./ci-auto-fix-setting", () => ({ CIAutoFixSetting: () => null }));
vi.mock("./undo-setting", () => ({ UndoSetting: () => null }));
vi.mock("./adr-gate-setting", () => ({ AdrGateSetting: () => null }));
vi.mock("./business-rules-setting", () => ({ BusinessRulesSetting: () => null }));
vi.mock("./standup-setting", () => ({ StandupSetting: () => null }));
vi.mock("./triage-auto-setting", () => ({ TriageAutoSetting: () => null }));
vi.mock("./triage-email-source-setting", () => ({ TriageEmailSourceSetting: () => null }));
vi.mock("./approval-gates-setting", () => ({ ApprovalGatesSetting: () => null }));
vi.mock("./run-halt-setting", () => ({ RunHaltSetting: () => null }));
vi.mock("./branch-cleanup-setting", () => ({ BranchCleanupSetting: () => null }));
vi.mock("./permission-profiles-setting", () => ({ PermissionProfilesSetting: () => null }));
vi.mock("./runtime-pools-setting", () => ({ RuntimePoolsSetting: () => null }));
vi.mock("./issue-routing-setting", () => ({ IssueRoutingSetting: () => null }));
vi.mock("./traffic-control-setting", () => ({ TrafficControlSetting: () => null }));
vi.mock("./drift-detection-setting", () => ({ DriftDetectionSetting: () => null }));
vi.mock("./pr-walkthrough-setting", () => ({ PrWalkthroughSetting: () => null }));
vi.mock("./pipelines-setting", () => ({ PipelinesSetting: () => null }));

vi.mock("./delete-workspace-dialog", () => ({
  DeleteWorkspaceDialog: () => null,
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { WorkspaceTab } from "./workspace-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("WorkspaceTab — automatic updates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      issue_prefix: "TES",
      repos: [],
    };
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    mockUpdateWorkspace.mockImplementation(
      async (_id: string, payload: Record<string, unknown>) => ({
        ...workspaceRef.current,
        ...payload,
        issue_prefix:
          (payload.issue_prefix as string | undefined) ?? workspaceRef.current.issue_prefix,
      }),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function setupUser() {
    return userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  }

  it("renders the current prefix in the shared input control", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;
    expect(input.value).toBe("TES");
    expect(screen.queryByRole("button", { name: /^Save$/ })).toBeNull();
  });

  // The workspace context became the doctrine (its own tab, versioned and
  // reviewable), so this tab must not offer a second, silently auto-saved
  // editor for the same text.
  it("sends the doctrine to its own tab instead of editing the context here", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    expect(screen.queryByRole("textbox", { name: "Context" })).toBeNull();
    const link = screen.getByRole("link", { name: "Open Doctrine" });
    expect(link.getAttribute("href")).toBe("/test-workspace/settings?tab=doctrine");
  });

  it("renders the workspace slug in the shared read-only input control", () => {
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    const input = screen.getByRole("textbox", { name: "Slug" }) as HTMLInputElement;
    expect(input.value).toBe("test-workspace");
    expect(input.readOnly).toBe(true);
  });

  it("uppercases and strips non-alphanumeric prefix input", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "ab-12!cd");

    expect(input.value).toBe("AB12CD");
  });

  it("auto-saves ordinary workspace fields without invalidating issue caches", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const nameInput = screen.getByDisplayValue("Test Workspace");

    await user.clear(nameInput);
    await user.type(nameInput, "Renamed Workspace");
    await user.tab();

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        name: "Renamed Workspace",
        description: "",
      });
      expect(mockToastSuccess).toHaveBeenCalledWith(
        "Workspace settings saved",
        { id: "settings-auto-save" },
      );
    });
    expect(mockInvalidateQueries).not.toHaveBeenCalled();
  });

  it("asks for confirmation on prefix blur and persists only after confirmation", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "NEW");
    await user.tab();

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await screen.findByText(/Change issue prefix/i);
    expect(screen.getByText(/TES-N/)).toBeTruthy();
    expect(screen.getByText(/NEW-N/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        issue_prefix: "NEW",
      });
    });
    expect(mockInvalidateQueries).toHaveBeenCalledWith({
      queryKey: ["issues", "workspace-1"],
    });
    expect(mockToastSuccess).toHaveBeenCalledWith(
      "Workspace settings saved",
      { id: "settings-auto-save" },
    );
  });

  it("does not persist a prefix when the confirmation is cancelled", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.type(input, "NEW");
    await user.tab();
    await screen.findByText(/Change issue prefix/i);
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    expect(input.value).toBe("NEW");
  });

  it("marks an empty prefix invalid and does not persist it", async () => {
    const user = setupUser();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    const input = screen.getByPlaceholderText("TES") as HTMLInputElement;

    await user.clear(input);
    await user.tab();

    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
  });

  it("disables editable workspace controls for regular members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<WorkspaceTab />, { wrapper: I18nWrapper });

    expect(screen.getByPlaceholderText("TES")).toBeDisabled();
    expect(screen.getByDisplayValue("Test Workspace")).toBeDisabled();
  });
});

// Leave used to navigate BEFORE its request, to dodge the `member:removed`
// realtime relocate. That guard now lives in the self-initiated registry
// (core pending-delete.ts), so leave has the same await-then-navigate shape
// as delete: a refused leave must leave the user where they were.
describe("WorkspaceTab — leaving a workspace", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      issue_prefix: "TES",
      repos: [],
    };
    // An admin, not the sole owner: the Leave button is enabled.
    membersRef.current = [
      { user_id: "user-1", role: "admin" },
      { user_id: "user-2", role: "owner" },
    ];
  });

  async function confirmLeave() {
    const user = userEvent.setup();
    render(<WorkspaceTab />, { wrapper: I18nWrapper });
    await user.click(screen.getByRole("button", { name: "Leave workspace" }));
    await user.click(await screen.findByRole("button", { name: "Confirm" }));
  }

  it("navigates away only after the server confirms", async () => {
    let resolveLeave!: () => void;
    mockLeaveAsync.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveLeave = resolve;
      }),
    );

    await confirmLeave();

    expect(mockLeaveAsync).toHaveBeenCalledWith("workspace-1");
    expect(mockPush).not.toHaveBeenCalled();

    resolveLeave();
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith("/"));
    // The realtime guard is released once navigation is done.
    expect(mockReleaseLeaveGuard).toHaveBeenCalledWith("workspace-1");
  });

  it("keeps the user in place when the leave fails", async () => {
    mockLeaveAsync.mockRejectedValue(new Error("nope"));

    await confirmLeave();

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("nope"));
    expect(mockPush).not.toHaveBeenCalled();
    expect(mockReleaseLeaveGuard).not.toHaveBeenCalled();
    // Still a member: the Leave button is back, not stuck in "Leaving…".
    expect(
      screen.getByRole("button", { name: "Leave workspace" }),
    ).toBeEnabled();
  });
});
