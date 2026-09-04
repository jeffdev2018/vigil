// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { MeetingOriginLink } from "./meeting-origin-link";

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    meetingDetail: (id: string) => `/acme/meetings/${id}`,
  }),
}));

function issue(overrides: Partial<Issue>): Issue {
  return { id: "issue-1", title: "Ship it", ...overrides } as Issue;
}

function render(value: Issue) {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issue/issue-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    openInNewTab: vi.fn(),
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <MeetingOriginLink issue={value} />
    </NavigationProvider>,
  );
}

describe("MeetingOriginLink", () => {
  it("links back to the meeting the issue came out of", () => {
    render(issue({ origin_type: "meeting", origin_id: "meet-1" }));
    const link = screen.getByRole("link", { name: /from meeting/i });
    expect(link.getAttribute("href")).toBe("/acme/meetings/meet-1");
  });

  it("renders nothing for another origin, or when the response did not resolve one", () => {
    for (const value of [
      issue({ origin_type: "slack", origin_id: "x" }),
      // Absent means "this response did not load it", not "no origin".
      issue({}),
      issue({ origin_type: "meeting", origin_id: null }),
    ]) {
      render(value);
      expect(screen.queryByRole("link", { name: /from meeting/i })).toBeNull();
    }
  });
});
