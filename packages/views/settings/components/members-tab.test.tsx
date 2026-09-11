// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { MemberWithUser } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  members: [] as MemberWithUser[],
  membersError: false,
  refetchMembers: vi.fn(),
  invitations: [] as unknown[],
  invitationsError: false,
  refetchInvitations: vi.fn(),
  shareLinks: [] as unknown[],
  shareLinksError: false,
  refetchShareLinks: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/billing", () => ({
  usePreviewWorkspaceSeatPurchase: () => ({ mutateAsync: vi.fn(), isPending: false }),
  usePurchaseWorkspaceSeats: () => ({ mutateAsync: vi.fn(), isPending: false }),
  workspaceSubscriptionSummaryOptions: () => ({ queryKey: ["billing-summary"] }),
}));
vi.mock("@multica/core/api", () => ({
  api: { createMember: vi.fn() },
  errorCode: () => undefined,
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId] }),
  invitationListOptions: (wsId: string) => ({ queryKey: ["invitations", wsId] }),
  shareLinkListOptions: (wsId: string) => ({ queryKey: ["share-links", wsId] }),
  workspaceKeys: {
    members: (wsId: string) => ["members", wsId],
    invitations: (wsId: string) => ["invitations", wsId],
    shareLinks: (wsId: string) => ["share-links", wsId],
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const key = options.queryKey[0];
    if (key === "members") {
      return { data: state.members, isError: state.membersError, refetch: state.refetchMembers };
    }
    if (key === "invitations") {
      return {
        data: state.invitations,
        isError: state.invitationsError,
        refetch: state.refetchInvitations,
      };
    }
    if (key === "share-links") {
      return {
        data: state.shareLinks,
        isError: state.shareLinksError,
        refetch: state.refetchShareLinks,
      };
    }
    return { data: undefined, isError: false, refetch: vi.fn() };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

import { MembersTab } from "./members-tab";

function owner(): MemberWithUser {
  return {
    id: "m1",
    workspace_id: "ws-1",
    user_id: "user-1",
    role: "owner",
    created_at: "2026-01-01T00:00:00Z",
    name: "Jeff",
    email: "jeff@example.com",
    avatar_url: null,
  };
}

beforeEach(() => {
  state.members = [owner()];
  state.membersError = false;
  state.refetchMembers.mockClear();
  state.invitations = [];
  state.invitationsError = false;
  state.refetchInvitations.mockClear();
  state.shareLinks = [];
  state.shareLinksError = false;
  state.refetchShareLinks.mockClear();
});

// Regression: members/invitations/shareLinks all defaulted to `?? []` with
// no isError read anywhere — a failed fetch rendered "No members found." on
// the most sensitive admin tab, and the pending-invitations section simply
// vanished instead of showing an outage.
describe("MembersTab — load failures", () => {
  it("shows an error state with retry instead of 'No members found' when the members fetch fails", () => {
    state.membersError = true;
    renderWithI18n(<MembersTab />);

    expect(screen.queryByText("No members found.")).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load members.");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchMembers).toHaveBeenCalled();
  });

  it("surfaces the pending-invitations section on failure instead of silently disappearing", () => {
    state.invitationsError = true;
    renderWithI18n(<MembersTab />);

    expect(screen.getByRole("alert")).toHaveTextContent("Could not load pending invitations.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchInvitations).toHaveBeenCalled();
  });

  it("shows an error state for share links instead of a silently empty list", () => {
    state.shareLinksError = true;
    renderWithI18n(<MembersTab />);

    expect(screen.getByRole("alert")).toHaveTextContent("Could not load share links.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchShareLinks).toHaveBeenCalled();
  });

  it("renders the real member list when nothing failed", () => {
    renderWithI18n(<MembersTab />);
    expect(screen.getByText("Jeff")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
