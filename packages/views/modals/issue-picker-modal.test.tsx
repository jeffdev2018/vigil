// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";

const mockSearchIssues = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { searchIssues: (...args: unknown[]) => mockSearchIssues(...args) },
}));

// The modal renders status icons from the workspace status catalog, which
// would need a QueryClient and the workspace route providers. This suite is
// about the debounce lifecycle, so stub both the way the other modal/search
// suites do (see search/search-command.test.tsx).
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(undefined),
}));

import { IssuePickerModal } from "./issue-picker-modal";

function renderModal() {
  return renderWithI18n(
    <IssuePickerModal
      open
      onOpenChange={() => {}}
      title="Pick an issue"
      description="Pick an issue"
      excludeIds={[]}
      onSelect={() => {}}
    />,
  );
}

function issue(overrides: Partial<Issue>): Issue {
  return { id: "issue", identifier: "MUL-1", title: "An issue", status: "todo", ...overrides } as Issue;
}

const ORIGINAL = issue({ id: "original", identifier: "MUL-1", title: "Tap targets too small" });
const DUPLICATE = issue({
  id: "duplicate",
  identifier: "MUL-2",
  title: "Tap targets too small on iPhone",
  status: "cancelled",
  duplicate_of: { id: "original", identifier: "MUL-1", title: "Tap targets too small", status: "todo" },
});

afterEach(() => {
  cleanup();
  mockSearchIssues.mockReset();
});

describe("IssuePickerModal", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mockSearchIssues.mockResolvedValue({ issues: [] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires the debounced search after unmount is not reached — search runs while mounted", async () => {
    const { unmount } = renderModal();
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "auth" } });
    await vi.advanceTimersByTimeAsync(300);
    expect(mockSearchIssues).toHaveBeenCalledTimes(1);
    unmount();
  });

  // P3 audit finding: the debounce timer and its AbortController were only
  // ever cleared from inside search() itself (on the NEXT keystroke) — an
  // unmount mid-debounce (modal closed, or the whole tree torn down) left
  // the timer armed, firing a fetch — and a state update — against a
  // component no longer there to receive it.
  it("clears the pending debounced search on unmount instead of letting it fire later", async () => {
    const { unmount } = renderModal();
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "auth" } });
    unmount();

    await vi.advanceTimersByTimeAsync(300);
    expect(mockSearchIssues).not.toHaveBeenCalled();
  });
});

// The mark-as-duplicate picker must not offer an issue that is itself a
// duplicate; the server would reject it after the dialog has already closed.
describe("IssuePickerModal isSelectable", () => {
  it("drops unselectable issues from search results", async () => {
    mockSearchIssues.mockResolvedValue({ issues: [ORIGINAL, DUPLICATE] });
    renderWithI18n(
      <IssuePickerModal
        open
        onOpenChange={() => {}}
        title="Mark as duplicate"
        description="Pick the original"
        excludeIds={[]}
        isSelectable={(candidate) => !candidate.duplicate_of}
        onSelect={() => {}}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("Search issues..."), { target: { value: "tap" } });
    await waitFor(() => expect(mockSearchIssues).toHaveBeenCalled());
    await screen.findByText("Tap targets too small");
    expect(screen.queryByText("Tap targets too small on iPhone")).toBeNull();
  });

  it("drops unselectable issues from suggestions too", () => {
    renderWithI18n(
      <IssuePickerModal
        open
        onOpenChange={() => {}}
        title="Mark as duplicate"
        description="Pick the original"
        excludeIds={[]}
        suggestions={[{ key: "recent", heading: "Recently viewed", issues: [DUPLICATE, ORIGINAL] }]}
        isSelectable={(candidate) => !candidate.duplicate_of}
        onSelect={() => {}}
      />,
    );

    expect(screen.getByText("Recently viewed")).toBeTruthy();
    expect(screen.getByText("Tap targets too small")).toBeTruthy();
    expect(screen.queryByText("Tap targets too small on iPhone")).toBeNull();
  });
});
