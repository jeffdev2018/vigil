/**
 * Note mention chip (JEF-417 / B06): an agent citing a Brain note in a
 * comment or run output, `[text](mention://note/<uuid>)`. Mirrors
 * project-mention-a11y.test.tsx's approach — the real AppLink and
 * NavigationProvider are used so navigation is asserted, not a mock of it.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => "just now",
  };
});

// vi.mock factories are hoisted above top-level declarations, so the class
// they reference must be hoisted too.
const { MockApiError } = vi.hoisted(() => ({
  MockApiError: class MockApiError extends Error {
    status: number;
    constructor(status: number) {
      super("mock api error");
      this.status = status;
    }
  },
}));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  ApiError: MockApiError,
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/brain/queries", () => ({
  brainNoteOptions: (wsId: string, id: string) => ({
    queryKey: ["brain", wsId, "detail", id],
    queryFn: () => {
      throw new Error("NoteMentionLink must never actively fetch a note");
    },
  }),
}));

const queryState = vi.hoisted(() => ({
  isError: false,
  error: null as unknown,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ isError: queryState.isError, error: queryState.error }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
  paths: {
    workspace: (slug: string) => ({
      brain: () => `/${slug}/brain`,
    }),
  },
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));

vi.mock("../projects/components/project-mention-card", () => ({
  ProjectMentionCard: ({ projectId }: { projectId: string }) => <span>{projectId}</span>,
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

import { RichContent } from "./rich-content";

const NOTE_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
const MENTION = `Deploys go through the release tag, see [the deploy note](mention://note/${NOTE_ID}).`;

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderMention(adapter: NavigationAdapter = makeAdapter()) {
  return render(
    <NavigationProvider value={adapter}>
      <RichContent content={MENTION} />
    </NavigationProvider>,
  );
}

describe("note mention chip", () => {
  beforeEach(() => {
    queryState.isError = false;
    queryState.error = null;
  });

  it("renders a link to the note page, labelled with the citation's link text", () => {
    const { container } = renderMention();
    const anchor = container.querySelector(`a[href="/acme/brain?note=${NOTE_ID}"]`);
    expect(anchor).not.toBeNull();
    expect(anchor?.tagName).toBe("A");
    expect(screen.getByText("the deploy note")).toBeTruthy();
  });

  it("navigates on click through the adapter", () => {
    const push = vi.fn();
    const { container } = renderMention(makeAdapter({ push }));
    fireEvent.click(container.querySelector("a") as HTMLAnchorElement);
    expect(push).toHaveBeenCalledWith(`/acme/brain?note=${NOTE_ID}`);
  });

  it("renders the citation muted, not as a link, when the note is cached as deleted", () => {
    queryState.isError = true;
    queryState.error = new MockApiError(404);
    const { container } = renderMention();
    expect(container.querySelector("a")).toBeNull();
    expect(screen.getByText("the deploy note").closest("span")?.title).toBe("Note not found");
  });
});
