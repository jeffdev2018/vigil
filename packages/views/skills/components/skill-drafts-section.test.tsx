// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SkillDraft } from "@multica/core/skills/drafts";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and origin reading: packages/core/skills/drafts.test.ts.

const state = vi.hoisted(() => ({ drafts: [] as SkillDraft[], review: vi.fn(), push: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/ws/issues/${id}`, skillDetail: (id: string) => `/ws/skills/${id}` }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => <a href={href} {...rest}>{children}</a>,
  useNavigation: () => ({ push: state.push }),
}));
vi.mock("@multica/core/skills/drafts", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/skills/drafts")>()),
  skillDraftListOptions: () => ({ queryKey: ["drafts"], queryFn: async () => state.drafts }),
  useReviewSkillDraft: () => ({ mutate: state.review, isPending: false }),
}));

import { SkillDraftsSection } from "./skill-drafts-section";

const draft = (over: Partial<SkillDraft> = {}): SkillDraft => ({
  id: "s1", workspace_id: "ws-1", name: "mined-unit-tests", description: "Add tests before marking done.", config: { origin: { type: "skill_miner", agent_name: "Builder", signals: 3, status_regressed: 1 } },
  created_by: null, created_at: "", updated_at: "", status: "draft",
  sources: [{ issue_id: "i1", issue_number: 12, issue_title: "First", comment_id: "c1", status_regressed: true }, { issue_id: "i2", issue_number: 15, issue_title: "Second", comment_id: "c2", status_regressed: false }], ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <SkillDraftsSection />
    </QueryClientProvider>,
  );
}

describe("SkillDraftsSection", () => {
  beforeEach(() => {
    state.drafts = [draft()];
    state.review.mockReset();
    state.push.mockReset();
  });

  it("lists a mined draft with its origin and sources, publishes and dismisses", async () => {
    render();
    const card = await screen.findByTestId("skill-draft");
    expect(card.getAttribute("data-origin")).toBe("skill_miner");
    expect(screen.getByText("from 3 corrections of Builder (1 with a status moved back)")).toBeTruthy();
    expect(screen.getByText("#12 ↩").getAttribute("href")).toBe("/ws/issues/i1");
    fireEvent.click(screen.getByText("Publish and edit"));
    expect(state.review).toHaveBeenCalledWith({ id: "s1", action: "publish" }, expect.anything());
    fireEvent.click(screen.getByText("Dismiss"));
    expect(state.review).toHaveBeenCalledWith({ id: "s1", action: "dismiss" }, expect.anything());
  });

  it("renders nothing when there is nothing to review", async () => {
    state.drafts = [];
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.querySelector('[data-testid="skill-drafts"]')).toBeNull();
  });
});
