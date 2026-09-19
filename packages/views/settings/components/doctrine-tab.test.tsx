// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Doctrine, DoctrineReport, DoctrineVersion } from "@multica/core/doctrine";
import { renderWithI18n } from "../../test/i18n";

// The contract parsing, the byte count and the publish precondition are
// covered canonically in packages/core/doctrine/schemas.test.ts. This suite
// keeps the happy path, the wiring and the named regressions.

const state = vi.hoisted(() => ({
  doctrine: null as unknown,
  versions: [] as unknown[],
  reports: [] as unknown[],
  diff: null as unknown,
  publish: vi.fn(),
  approve: vi.fn(),
  reject: vi.fn(),
  restore: vi.fn(),
  acknowledge: vi.fn(),
  dismiss: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const kind = options.queryKey[2];
    if (kind === "doctrine") return { data: state.doctrine, isPending: false };
    if (kind === "reports") return { data: state.reports, isPending: false };
    if (kind === "diff") return { data: state.diff, isPending: false };
    if (options.queryKey[0] === "members")
      return { data: [{ user_id: "user-1", name: "Ada" }], isPending: false };
    if (options.queryKey[0] === "agents")
      return { data: [{ id: "agent-1", name: "Alpha" }], isPending: false };
    return { data: undefined, isPending: false };
  },
  useInfiniteQuery: () => ({
    data: { pages: [{ versions: state.versions, next_cursor: null }] },
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  }),
  useQueryClient: () => ({
    setQueryData: vi.fn(),
    getQueryData: vi.fn(),
    invalidateQueries: vi.fn(),
  }),
}));

vi.mock("@multica/core/doctrine", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/doctrine")>()),
  doctrineOptions: (wsId: string) => ({ queryKey: ["doctrine", wsId, "doctrine"] }),
  doctrineVersionsInfiniteOptions: (wsId: string) => ({ queryKey: ["doctrine", wsId, "versions"] }),
  doctrineReportsOptions: (wsId: string, status: string) => ({
    queryKey: ["doctrine", wsId, "reports", status],
  }),
  doctrineDiffOptions: (wsId: string, id: string) => ({
    queryKey: ["doctrine", wsId, "diff", id],
  }),
  usePublishDoctrine: () => ({ mutate: state.publish, isPending: false }),
  useApproveDoctrineVersion: () => ({ mutate: state.approve, isPending: false }),
  useRejectDoctrineVersion: () => ({ mutate: state.reject, isPending: false }),
  useRestoreDoctrineVersion: () => ({ mutate: state.restore, isPending: false }),
  useAcknowledgeDoctrineReport: () => ({ mutate: state.acknowledge, isPending: false }),
  useDismissDoctrineReport: () => ({ mutate: state.dismiss, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId] }),
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme", settings: {} }),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (selector?: (s: { user: { id: string } }) => unknown) =>
      selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: vi.fn() },
  ApiError: class ApiError extends Error {},
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { DoctrineTab } from "./doctrine-tab";

const version = (over: Partial<DoctrineVersion> = {}): DoctrineVersion => ({
  id: "v1",
  revision: 4,
  content: "Ship small.",
  status: "active",
  note: "",
  author_id: "user-1",
  reviewed_by: null,
  reviewed_at: null,
  review_note: "",
  restored_from_revision: null,
  created_at: "2026-09-09T09:00:00Z",
  bytes: 11,
  ...over,
});

const doctrine = (over: Partial<Doctrine> = {}): Doctrine => ({
  content: "Ship small.\nReview before you push.",
  revision: 4,
  updated_at: "2026-09-09T10:00:00Z",
  updated_by: "user-1",
  byte_limit: 32000,
  require_review: false,
  can_publish: true,
  active_version_id: "v1",
  pending: null,
  open_reports: 1,
  ...over,
});

const report = (over: Partial<DoctrineReport> = {}): DoctrineReport => ({
  id: "r1",
  doctrine_revision: 4,
  kind: "conflict",
  summary: "Two rules disagree on who approves a release.",
  passage: "Releases need an owner.",
  reporter_type: "agent",
  reporter_id: "agent-1",
  task_id: "t1",
  issue_id: "i1",
  status: "open",
  resolved_by: null,
  resolved_at: null,
  resolution_note: "",
  created_at: "2026-09-09T09:30:00Z",
  ...over,
});

beforeEach(() => {
  state.doctrine = doctrine();
  state.versions = [];
  state.reports = [];
  state.diff = null;
  state.publish.mockReset();
  state.approve.mockReset();
  state.reject.mockReset();
  state.restore.mockReset();
  state.acknowledge.mockReset();
  state.dismiss.mockReset();
  toast.success.mockReset();
  toast.error.mockReset();
});

describe("DoctrineTab", () => {
  it("shows the live document with its revision, author and byte count", () => {
    renderWithI18n(<DoctrineTab />);
    expect(screen.getAllByText("Revision 4").length).toBeGreaterThan(0);
    expect(screen.getByTestId("doctrine-updated").textContent).toContain("Ada");
    expect(screen.getByTestId("doctrine-bytes").textContent).toBe("35 / 32000 bytes");
    expect(screen.getByText("Open reports · 1")).toBeTruthy();
    expect((screen.getByLabelText("Doctrine") as HTMLTextAreaElement).value).toContain(
      "Review before you push.",
    );
  });

  it("publishes deliberately, carrying the revision it was editing", () => {
    renderWithI18n(<DoctrineTab />);
    const publish = screen.getByTestId("doctrine-publish");
    // Unchanged text is not publishable — the server 400s on it.
    expect(publish.getAttribute("disabled")).not.toBeNull();

    fireEvent.change(screen.getByLabelText("Doctrine"), {
      target: { value: "Ship small.\nReview twice." },
    });
    fireEvent.change(screen.getByLabelText("Note (optional)"), {
      target: { value: "tightened the review rule" },
    });
    fireEvent.click(screen.getByTestId("doctrine-publish"));

    expect(state.publish).toHaveBeenCalledWith(
      {
        content: "Ship small.\nReview twice.",
        expected_revision: 4,
        note: "tightened the review rule",
      },
      expect.anything(),
    );
  });

  it("flags an over-limit draft instead of letting the server refuse it", () => {
    state.doctrine = doctrine({ byte_limit: 20 });
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-over-limit")).toBeTruthy();
    expect(screen.getByTestId("doctrine-publish").getAttribute("disabled")).not.toBeNull();
  });

  it("renders the doctrine read-only for a member who cannot publish", () => {
    state.doctrine = doctrine({ can_publish: false });
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-readonly").textContent).toContain("Ship small.");
    expect(screen.queryByTestId("doctrine-publish")).toBeNull();
    expect(screen.queryByLabelText("Doctrine")).toBeNull();
  });

  it("offers Approve and Reject to a manager who is not the author", () => {
    state.doctrine = doctrine({
      pending: version({ id: "v2", revision: null, status: "pending", author_id: "user-2", note: "adds a rule" }),
    });
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-pending").textContent).toContain("adds a rule");

    fireEvent.change(screen.getByLabelText("Review note (optional)"), {
      target: { value: "looks right" },
    });
    fireEvent.click(screen.getByTestId("doctrine-approve"));
    expect(state.approve).toHaveBeenCalledWith(
      { id: "v2", note: "looks right" },
      expect.anything(),
    );

    fireEvent.click(screen.getByTestId("doctrine-reject"));
    expect(state.reject).toHaveBeenCalledWith(
      { id: "v2", note: "looks right" },
      expect.anything(),
    );
  });

  it("tells the author their own proposal is awaiting someone else", () => {
    state.doctrine = doctrine({
      pending: version({ id: "v2", revision: null, status: "pending", author_id: "user-1" }),
    });
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-pending-awaiting")).toBeTruthy();
    expect(screen.queryByTestId("doctrine-approve")).toBeNull();
    expect(screen.queryByTestId("doctrine-reject")).toBeNull();
  });

  it("lists the ledger and restores a revision through the current one", () => {
    state.versions = [version(), version({ id: "v0", revision: 3, status: "superseded" })];
    renderWithI18n(<DoctrineTab />);
    expect(screen.getAllByTestId("doctrine-version-row")).toHaveLength(2);

    // The live revision offers no Restore: only the superseded one does.
    expect(screen.getAllByTestId("doctrine-restore")).toHaveLength(1);
    fireEvent.click(screen.getByTestId("doctrine-restore"));
    fireEvent.click(screen.getByTestId("doctrine-restore-confirm"));
    expect(state.restore).toHaveBeenCalledWith(
      { id: "v0", expected_revision: 4 },
      expect.anything(),
    );
  });

  it("colours the diff by line kind in the compare dialog", () => {
    state.versions = [version()];
    state.diff = {
      from: { ...version({ id: "v0", revision: 3 }), content: "" },
      to: { ...version(), content: "" },
      lines: [
        { kind: "same", text: "Ship small." },
        { kind: "add", text: "Review twice." },
        { kind: "del", text: "Review once." },
      ],
      added: 1,
      removed: 1,
    };
    renderWithI18n(<DoctrineTab />);
    fireEvent.click(screen.getByTestId("doctrine-compare"));

    const lines = screen.getAllByTestId("doctrine-diff-line");
    expect(lines.map((l) => l.getAttribute("data-kind"))).toEqual(["same", "add", "del"]);
    expect(lines[1]!.className).toContain("bg-success/10");
    expect(lines[2]!.className).toContain("bg-destructive/10");
    expect(screen.getByText(/1 added, 1 removed/)).toBeTruthy();
  });

  it("resolves an open report with a note, and links its issue", () => {
    state.reports = [report()];
    renderWithI18n(<DoctrineTab />);
    const row = screen.getByTestId("doctrine-report-row");
    expect(row.textContent).toContain("Conflict");
    expect(row.textContent).toContain("Alpha");
    expect(screen.getByTestId("doctrine-report-issue").getAttribute("href")).toBe(
      "/acme/issues/i1",
    );

    fireEvent.change(screen.getByTestId("doctrine-report-note"), {
      target: { value: "rewriting the release rule" },
    });
    fireEvent.click(screen.getByTestId("doctrine-report-acknowledge"));
    expect(state.acknowledge).toHaveBeenCalledWith(
      { id: "r1", note: "rewriting the release rule" },
      expect.anything(),
    );
  });

  it("keeps a resolved report readable and inert", () => {
    state.reports = [report({ status: "dismissed", resolution_note: "not a conflict" })];
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-report-status").textContent).toBe("Dismissed");
    expect(screen.queryByTestId("doctrine-report-acknowledge")).toBeNull();
    expect(screen.queryByTestId("doctrine-report-dismiss")).toBeNull();
  });

  it("says nothing is there rather than showing empty lists", () => {
    state.doctrine = doctrine({ open_reports: 0 });
    renderWithI18n(<DoctrineTab />);
    expect(screen.getByTestId("doctrine-history-empty")).toBeTruthy();
    expect(screen.getByTestId("doctrine-reports-empty")).toBeTruthy();
  });
});
