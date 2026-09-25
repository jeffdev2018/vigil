import { describe, expect, it, vi, beforeEach } from "vitest";
import type { ReactElement } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

// Machines are derived from two reads — the runtime list and the runtime
// profiles — and both default to `[]`. That default is why the page could
// answer "no machine yet" for a workspace whose reads failed, which is the
// regression these tests pin. Keyed by the query key so each read can fail
// independently.
const queryState = vi.hoisted(() => ({
  runtimes: { isError: false },
  profiles: { isError: false },
}));
const refetchRuntimes = vi.hoisted(() => vi.fn());
const refetchProfiles = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const kind = options.queryKey[0];
    if (kind === "runtimes") {
      return {
        data: [],
        isLoading: false,
        isError: queryState.runtimes.isError,
        refetch: refetchRuntimes,
      };
    }
    if (kind === "runtime-profiles") {
      return {
        data: [],
        isLoading: false,
        isError: queryState.profiles.isError,
        refetch: refetchProfiles,
      };
    }
    return { data: [], isLoading: false, isError: false, refetch: vi.fn() };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/auth", () => {
  const state = () => ({ isLoading: false, user: { id: "user-1" } });
  return {
    useAuthStore: Object.assign(
      (selector?: (s: ReturnType<typeof state>) => unknown) =>
        selector ? selector(state()) : state(),
      { getState: state },
    ),
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => undefined }));

vi.mock("@multica/core/onboarding", () => ({
  memberNeedsMikaSetup: () => false,
  useBootstrapMika: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useRequiredWorkspaceSlug: () => "acme",
  useWorkspacePaths: () => ({
    chat: () => "/acme/chat",
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
  }),
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["agent-task-snapshot"] }),
}));
vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["chat-sessions"] }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeProfileListOptions: () => ({ queryKey: ["runtime-profiles"] }),
}));
vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeKeys: { all: () => ["runtimes"] },
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("./connect-remote-dialog", () => ({
  ConnectRemoteDialog: () => <div data-testid="connect-remote-dialog" />,
}));
vi.mock("./cloud-runtime-dialog", () => ({
  CloudRuntimeDialog: () => <div data-testid="cloud-runtime-dialog" />,
}));

import { RuntimesPage } from "./runtimes-page";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/runtimes",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderPage(ui: ReactElement) {
  return renderWithI18n(
    <NavigationProvider value={adapter}>{ui}</NavigationProvider>,
  );
}

describe("RuntimesPage load failures", () => {
  beforeEach(() => {
    cleanup();
    queryState.runtimes.isError = false;
    queryState.profiles.isError = false;
    refetchRuntimes.mockClear();
    refetchProfiles.mockClear();
  });

  it("shows the real empty state when both reads answered with nothing", () => {
    renderPage(<RuntimesPage />);
    expect(screen.getByText("No runtimes yet")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("reports a failed runtime list instead of claiming no machine", () => {
    queryState.runtimes.isError = true;
    renderPage(<RuntimesPage />);

    expect(screen.queryByText("No runtimes yet")).toBeNull();
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Couldn't load this page");

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(refetchRuntimes).toHaveBeenCalledTimes(1);
    expect(refetchProfiles).not.toHaveBeenCalled();
  });

  it("reports a failed profile read too, and retries only what failed", () => {
    queryState.profiles.isError = true;
    renderPage(<RuntimesPage />);

    expect(screen.queryByText("No runtimes yet")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(refetchProfiles).toHaveBeenCalledTimes(1);
    expect(refetchRuntimes).not.toHaveBeenCalled();
  });

  it("keeps the local machine visible when the list read fails", () => {
    // Desktop knows this device exists without the server, so there is
    // something true to show and the page must not blank it out.
    queryState.runtimes.isError = true;
    renderPage(<RuntimesPage hasLocalMachine />);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
