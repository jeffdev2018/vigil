// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Schema parsing, citation formatting and forge links: packages/core/projects/wiki.test.ts.
// This suite covers the wiring and the states a reader must be able to tell
// apart: no repository, nothing generated, generating, stale, and a page that
// cites nothing.

const state = vi.hoisted(() => ({
  wiki: null as unknown,
  page: null as unknown,
  refreshed: 0,
  refreshResult: { started: true, reason: "" } as { started: boolean; reason: string },
  toasts: [] as string[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({
  toast: {
    success: (m: string) => state.toasts.push("success:" + m),
    error: (m: string) => state.toasts.push("error:" + m),
    message: (m: string) => state.toasts.push("message:" + m),
  },
}));
vi.mock("@multica/core/projects", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/projects/wiki")>(
    "../../../core/projects/wiki",
  );
  return {
    ...actual,
    projectCodeWikiOptions: () => ({ queryKey: ["wiki"], queryFn: async () => state.wiki }),
    projectCodeWikiPageOptions: (_ws: string, _p: string, slug: string) => ({
      queryKey: ["wiki-page", slug],
      queryFn: async () => state.page,
    }),
    useRefreshProjectCodeWiki: () => ({
      isPending: false,
      mutate: (_v: unknown, opts?: { onSuccess?: (r: unknown) => void }) => {
        state.refreshed += 1;
        opts?.onSuccess?.(state.refreshResult);
      },
    }),
  };
});

import { WikiPanel } from "./wiki-panel";

const wiki = (over: Record<string, unknown> = {}) => ({
  resource: { id: "r1", resource_ref: { url: "https://github.com/acme/widget" } },
  snapshot: {
    id: "s1",
    project_resource_id: "r1",
    commit_sha: "abc1234def",
    state: "published",
    page_count: 2,
    created_at: "2026-01-01T00:00:00Z",
    published_at: "2026-01-02T09:00:00Z",
    generated: true,
    stale: false,
  },
  pages: [
    { id: "p1", slug: "overview", title: "Overview", citation_count: 1 },
    { id: "p2", slug: "architecture", title: "Architecture", citation_count: 2 },
  ],
  building: false,
  generated: true,
  ...over,
});

const page = (over: Record<string, unknown> = {}) => ({
  id: "p1",
  slug: "overview",
  title: "Overview",
  content: "# Overview\n\nHow this repository is laid out.",
  citations: [{ path: "src/app.py", start_line: 3, end_line: 9, commit_sha: "abc1234def" }],
  commit_sha: "abc1234def",
  generated: true,
  stale: false,
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WikiPanel projectId="p1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.wiki = wiki();
  state.page = page();
  state.refreshed = 0;
  state.refreshResult = { started: true, reason: "" };
  state.toasts = [];
});

describe("WikiPanel", () => {
  it("shows the provenance banner beside the content", async () => {
    render();
    const banner = await screen.findByTestId("wiki-banner");
    // The commit and the date stay on screen while the page is read: the
    // reader has to be able to see what this documentation describes.
    expect(banner.textContent).toContain("abc1234");
    expect(banner.textContent).toContain("2026-01-02");
    expect(banner.textContent?.toLowerCase()).toContain("generated");
  });

  it("renders the first page and links its citations into the forge", async () => {
    render();
    expect(await screen.findByTestId("wiki-page")).toBeTruthy();
    const link = (await screen.findAllByTestId("wiki-citation"))[0]?.querySelector("a");
    expect(link?.getAttribute("href")).toBe(
      "https://github.com/acme/widget/blob/abc1234def/src/app.py#L3-L9",
    );
    expect(link?.textContent).toContain("src/app.py:3-9");
  });

  it("switches page from the table of contents", async () => {
    render();
    const entries = await screen.findAllByTestId("wiki-toc-entry");
    expect(entries).toHaveLength(2);
    expect(entries[0]?.getAttribute("data-active")).toBe("true");
    state.page = page({ slug: "architecture", title: "Architecture", content: "# Architecture" });
    fireEvent.click(entries[1] as HTMLElement);
    expect((await screen.findAllByTestId("wiki-toc-entry"))[1]?.getAttribute("data-active")).toBe("true");
  });

  it("keeps a page with no citations readable and says it is unsourced", async () => {
    // Acceptance 7: a page returned without citations must not break the panel.
    state.page = page({ citations: [] });
    render();
    expect(await screen.findByTestId("wiki-page")).toBeTruthy();
    expect(await screen.findByTestId("wiki-no-citations")).toBeTruthy();
    expect(screen.queryByTestId("wiki-citation")).toBeNull();
  });

  it("warns when the snapshot is behind the repository head", async () => {
    state.wiki = wiki({ snapshot: { ...(wiki().snapshot as object), stale: true } });
    render();
    expect(await screen.findByTestId("wiki-stale")).toBeTruthy();
    // Stale content stays readable — an old page beats no page.
    expect(await screen.findByTestId("wiki-page")).toBeTruthy();
  });

  it("offers to generate when nothing is published, and reports the outcome", async () => {
    state.wiki = wiki({ snapshot: null, pages: [] });
    render();
    expect(await screen.findByTestId("wiki-empty")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));
    expect(state.refreshed).toBe(1);
    expect(state.toasts[0]).toContain("success:");
  });

  it("reports the server's reason when a generation did not start", async () => {
    state.wiki = wiki({ snapshot: null, pages: [] });
    state.refreshResult = { started: false, reason: "a wiki generation is already running" };
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Generate" }));
    expect(state.toasts[0]).toBe("message:a wiki generation is already running");
  });

  it("shows a badge and blocks the button while a generation runs", async () => {
    state.wiki = wiki({ building: true });
    render();
    expect(await screen.findByTestId("wiki-building")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Regenerate" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("explains itself when the project tracks no repository", async () => {
    state.wiki = { resource: null, snapshot: null, pages: [], building: false, generated: true };
    render();
    expect(await screen.findByTestId("wiki-no-repo")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Generate" })).toBeNull();
  });
});
