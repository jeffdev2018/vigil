// @vitest-environment jsdom
/**
 * Regression: a squad member row's workspace role (owner/admin/member) came
 * straight from `wsMembers.find(...).role` — the raw server enum — with no
 * t() lookup, unlike member-detail-page.tsx/member-profile-card.tsx's
 * RoleBadge for the exact same role enum. squad-profile-card.tsx now reuses
 * member-profile-card.tsx's useWorkspaceRoleLabel().
 */
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/acme/squads/${id}`,
    memberDetail: (id: string) => `/acme/members/${id}`,
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

const squad: Squad = {
  id: "sq-1",
  workspace_id: "ws-1",
  name: "Platform",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "agent-1",
  creator_id: "user-1",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  archived_at: null,
  archived_by: null,
  member_count: 1,
  member_preview: [{ member_type: "member", member_id: "user-1", role: "owner" }],
};

vi.mock("@multica/core/workspace/queries", () => ({
  squadListOptions: () => ({ queryKey: ["squads"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey: readonly unknown[] }) => {
    if (o.queryKey[0] === "squads") return { data: [squad], isLoading: false };
    if (o.queryKey[0] === "members") {
      return { data: [{ user_id: "user-1", name: "Ada Lovelace", role: "owner" }] };
    }
    return { data: [] };
  },
}));

import { SquadProfileCard } from "./squad-profile-card";

describe("SquadProfileCard member role", () => {
  it("translates the member's workspace role instead of rendering the raw enum", () => {
    renderWithI18n(<SquadProfileCard squadId="sq-1" />, { locale: "fr" });
    expect(screen.getByText("Propriétaire")).toBeInTheDocument();
    expect(screen.queryByText("owner")).not.toBeInTheDocument();
  });
});
