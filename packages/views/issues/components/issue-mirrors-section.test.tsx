// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueMirrors } from "@multica/core/mirrors";
import { renderWithI18n } from "../../test/i18n";

// Tolerant parsing and isMirrorOpen: packages/core/mirrors/schemas.test.ts.

const state = vi.hoisted(() => ({
  data: { mirrors: [], mirror_of: null } as IssueMirrors,
  setSynced: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
}));
vi.mock("@multica/core/mirrors", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/mirrors")>()),
  issueMirrorsOptions: () => ({ queryKey: ["mirrors"], queryFn: async () => state.data }),
  useSetMirrorTypeSynced: () => ({ mutate: state.setSynced, isPending: false }),
}));
// Mocked at the context module rather than the barrel so <AppLink> stays the
// real component and the rendered href is what the test asserts.
vi.mock("../../navigation/context", () => ({
  useNavigation: () => ({
    push: vi.fn(), replace: vi.fn(), back: vi.fn(),
    pathname: "/acme/issues/i1", searchParams: new URLSearchParams(), hash: "",
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { IssueMirrorsSection } from "./issue-mirrors-section";

const mirror = (over: Partial<IssueMirrors["mirrors"][number]> = {}) => ({
  id: "m1", mirror_issue_id: "i2", identifier: "JIA-9", number: 9,
  title: "mirrored", status: "todo", project_id: "p2", project_title: "Backend",
  type_synced: false, ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueMirrorsSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { mirrors: [], mirror_of: null };
  state.setSynced = vi.fn();
});

describe("IssueMirrorsSection", () => {
  it("renders nothing when the issue is neither a source nor a mirror", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows one chip per mirror with its project, status and link", async () => {
    state.data = {
      mirrors: [mirror(), mirror({ id: "m2", mirror_issue_id: "i3", identifier: "JIA-10", project_title: "Infra", status: "done" })],
      mirror_of: null,
    };
    render();
    const chips = await screen.findAllByTestId("issue-mirror-chip");
    expect(chips).toHaveLength(2);
    expect(screen.getByText("Mirror in Backend")).toBeTruthy();
    expect(screen.getByText("Mirror in Infra")).toBeTruthy();
    expect(screen.getByText("JIA-9")).toBeTruthy();
    // The link points at the mirror issue, not at the current issue.
    const link = screen.getByText("Mirror in Backend").closest("a");
    expect(link?.getAttribute("href")).toBe("/acme/issues/i2");
    // No banner on a source.
    expect(screen.queryByTestId("issue-mirror-banner")).toBeNull();
  });

  it("toggles the per-mirror types-synced marker", async () => {
    state.data = { mirrors: [mirror()], mirror_of: null };
    render();
    const box = (await screen.findAllByRole("checkbox"))[0] as HTMLInputElement;
    expect(box.checked).toBe(false);
    fireEvent.click(box);
    expect(state.setSynced).toHaveBeenCalledWith(
      { mirrorId: "m1", value: true },
      expect.anything(),
    );
  });

  it("shows the provenance banner on a generated mirror and no chips", async () => {
    state.data = {
      mirrors: [],
      mirror_of: {
        id: "m1", source_issue_id: "i1", identifier: "JIA-3", number: 3,
        title: "the source", status: "in_progress", project_id: "p1",
        project_title: "Frontend", type_synced: false,
      },
    };
    render();
    const banner = await screen.findByTestId("issue-mirror-banner");
    expect(banner.textContent).toContain("Generated from JIA-3 (Frontend)");
    expect(screen.queryByTestId("issue-mirror-chip")).toBeNull();
    expect(screen.getByText("Open the source issue").closest("a")?.getAttribute("href")).toBe("/acme/issues/i1");
  });

  it("names a deleted target project instead of rendering an empty label", async () => {
    state.data = { mirrors: [mirror({ project_title: "" })], mirror_of: null };
    render();
    expect(await screen.findByText("Mirror in Deleted project")).toBeTruthy();
  });
});
