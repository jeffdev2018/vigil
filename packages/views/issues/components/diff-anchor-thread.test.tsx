// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { AnchoredThread, Comment, CommentAnchor } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Comment threads anchored to a diff line (F07 / JEF-21).
//
// The line-number arithmetic and the anchor parsing have their own canonical
// suites (packages/core/pr-walkthrough/hunk-lines.test.ts and
// packages/core/api/schemas.test.ts). What is pinned here is what THIS layer
// decides: whether a thread renders at all, whether the chip appears, and how
// a long thread folds.

const mutations = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  resolve: vi.fn(),
  react: vi.fn(),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateComment: () => ({ mutateAsync: mutations.create }),
  useUpdateComment: () => ({ mutateAsync: mutations.update }),
  useDeleteComment: () => ({ mutate: mutations.remove }),
  useResolveComment: () => ({ mutate: mutations.resolve }),
  useToggleCommentReaction: () => ({ mutate: mutations.react }),
}));

// CommentCard has its own suite; here it only has to prove that the thread
// handed it a root and the replies it was supposed to show.
vi.mock("./comment-card", () => ({
  CommentCard: ({ entry, replies }: { entry: { id: string; content?: string }; replies: { id: string }[] }) => (
    <div data-testid="comment-card" data-root={entry.id} data-replies={replies.map((r) => r.id).join(",")}>
      {entry.content}
    </div>
  ),
}));

vi.mock("./reply-input", () => ({
  ReplyInput: ({ placeholder }: { placeholder?: string }) => (
    <div data-testid="reply-input">{placeholder}</div>
  ),
}));

import { AnchorAskButton, AnchorComposer, DiffAnchorThread } from "./diff-anchor-thread";
import { AnchorChip } from "./anchor-chip";

const anchor = (over: Partial<CommentAnchor> = {}): CommentAnchor => ({
  kind: "diff_line",
  pr_source: "github",
  pr_id: "pr-1",
  head_sha: "abc1234",
  file_path: "server/internal/handler/comment.go",
  line_start: 41,
  line_end: 41,
  side: "new",
  review_flag_id: null,
  ...over,
});

const comment = (id: string, parent: string | null = null): Comment => ({
  id,
  issue_id: "issue-1",
  author_type: "member",
  author_id: "user-1",
  content: `body of ${id}`,
  type: "comment",
  parent_id: parent,
  reactions: [],
  attachments: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  resolved_at: null,
  resolved_by_type: null,
  resolved_by_id: null,
});

const thread = (replyCount: number, over: Partial<AnchoredThread> = {}): AnchoredThread => ({
  root: comment("root"),
  replies: Array.from({ length: replyCount }, (_, i) => comment(`r${i + 1}`, "root")),
  anchor: anchor(),
  anchor_stale: false,
  ...over,
});

beforeEach(() => vi.clearAllMocks());

describe("AnchorChip", () => {
  it("renders the path and the line range", () => {
    renderWithI18n(<AnchorChip anchor={anchor({ line_end: 44 })} />);
    expect(screen.getByText(/comment\.go:41-44/)).toBeTruthy();
  });

  it("collapses a single-line range to one number", () => {
    renderWithI18n(<AnchorChip anchor={anchor()} />);
    expect(screen.getByText(/comment\.go:41$/)).toBeTruthy();
  });

  it("marks a stale anchor without hiding it", () => {
    renderWithI18n(<AnchorChip anchor={anchor()} stale />);
    expect(screen.getByTestId("anchor-stale")).toBeTruthy();
    expect(screen.getByText(/comment\.go:41/)).toBeTruthy();
  });

  it("renders nothing for an anchor kind this build does not know", () => {
    const { container } = renderWithI18n(<AnchorChip anchor={anchor({ kind: "diff_symbol" })} />);
    expect(container.textContent).toBe("");
  });

  it("renders nothing when there is no anchor at all", () => {
    const { container } = renderWithI18n(<AnchorChip anchor={null} />);
    expect(container.textContent).toBe("");
  });

  it("is a plain label with no thread to jump to, and a button with one", () => {
    const plain = renderWithI18n(<AnchorChip anchor={anchor()} />);
    expect(plain.container.querySelector("button")).toBeNull();
    plain.unmount();

    renderWithI18n(<AnchorChip anchor={anchor()} rootId="root" />);
    expect(screen.getByRole("button", { name: /comment\.go:41/ })).toBeTruthy();
  });
});

describe("DiffAnchorThread", () => {
  it("renders the thread inline and exposes the chip's scroll target", () => {
    const { container } = renderWithI18n(
      <DiffAnchorThread issueId="issue-1" thread={thread(1)} currentUserId="user-1" />,
    );
    expect(container.querySelector("#anchored-thread-root")).toBeTruthy();
    expect(screen.getByTestId("comment-card").getAttribute("data-root")).toBe("root");
  });

  it("shows every reply of a short thread with no fold control", () => {
    renderWithI18n(<DiffAnchorThread issueId="issue-1" thread={thread(3)} />);
    expect(screen.getByTestId("comment-card").getAttribute("data-replies")).toBe("r1,r2,r3");
    expect(screen.queryByTestId("anchor-thread-fold")).toBeNull();
  });

  it("folds to the last three replies and expands on demand", () => {
    renderWithI18n(<DiffAnchorThread issueId="issue-1" thread={thread(6)} />);
    const card = () => screen.getByTestId("comment-card").getAttribute("data-replies");
    // Newest three, so the end of the conversation is what stays visible.
    expect(card()).toBe("r4,r5,r6");

    fireEvent.click(screen.getByTestId("anchor-thread-fold"));
    expect(card()).toBe("r1,r2,r3,r4,r5,r6");

    fireEvent.click(screen.getByTestId("anchor-thread-fold"));
    expect(card()).toBe("r4,r5,r6");
  });

  it("still renders a thread whose anchor kind is unknown", () => {
    renderWithI18n(
      <DiffAnchorThread issueId="issue-1" thread={thread(1, { anchor: anchor({ kind: "diff_symbol" }) })} />,
    );
    expect(screen.getByTestId("comment-card").getAttribute("data-root")).toBe("root");
  });

  it("marks a stale thread so the walkthrough can say the code moved", () => {
    renderWithI18n(<DiffAnchorThread issueId="issue-1" thread={thread(1, { anchor_stale: true })} />);
    expect(screen.getByTestId("diff-anchor-thread").getAttribute("data-stale")).toBe("true");
  });
});

describe("AnchorAskButton / AnchorComposer", () => {
  it("calls back when asked rather than opening its own composer", () => {
    const onAsk = vi.fn();
    renderWithI18n(<AnchorAskButton label="Ask about this line" onAsk={onAsk} />);
    fireEvent.click(screen.getByRole("button", { name: "Ask about this line" }));
    expect(onAsk).toHaveBeenCalledTimes(1);
  });

  it("names the place being asked about in the composer placeholder", () => {
    renderWithI18n(
      <AnchorComposer
        issueId="issue-1"
        location="comment.go:41"
        onDone={vi.fn()}
        anchor={{ pr_id: "pr-1", file_path: "comment.go", line_start: 41 }}
      />,
    );
    expect(screen.getByTestId("reply-input").textContent).toContain("comment.go:41");
  });
});
