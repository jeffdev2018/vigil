// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enIssues from "../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, modals: enModals, issues: enIssues } };

const mockSearchIssues = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { searchIssues: (...args: unknown[]) => mockSearchIssues(...args) },
}));

// The modal renders status icons from the workspace status catalog, which
// would need a QueryClient and the workspace route providers. This suite is
// about the debounce lifecycle, so stub both the way the other modal/search
// suites do (see search/search-command.test.tsx).
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([]) };
});

import { IssuePickerModal } from "./issue-picker-modal";

function renderModal() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <IssuePickerModal
        open
        onOpenChange={() => {}}
        title="Pick an issue"
        description="Pick an issue"
        excludeIds={[]}
        onSelect={() => {}}
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  mockSearchIssues.mockResolvedValue({ issues: [] });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  mockSearchIssues.mockReset();
});

describe("IssuePickerModal", () => {
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
