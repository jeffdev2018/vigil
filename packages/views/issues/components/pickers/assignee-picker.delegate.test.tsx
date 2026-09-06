// F01: AssigneePicker in delegate mode. Same component, one discriminator —
// so what this file pins is exactly the three things the discriminator
// changes: the emitted field names, the label sub-tree, and the absence of
// squads. Everything else (search, keyboard, ordering) is already covered by
// assignee-picker.keyboard.test.tsx and is deliberately not re-run here.
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "../../../locales/en/issues.json";
import { AssigneePicker } from "./assignee-picker";

const MEMBERS = [{ user_id: "user-1", name: "Ada Lovelace", role: "member" }];
const AGENTS = [
  { id: "agent-1", name: "CodeBot", archived_at: null, visibility: "workspace" },
];
const SQUADS = [
  { id: "squad-1", name: "Platform Squad", archived_at: null, leader_id: "agent-1" },
];

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "members") return { data: MEMBERS };
    if (queryKey[0] === "agents") return { data: AGENTS };
    if (queryKey[0] === "squads") return { data: SQUADS };
    return { data: [] };
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: () => ({ id: "user-1" }) }));
// No runtime bound anywhere: a delegate starts no run, so this must not stop
// an agent being named as one — while it still disables the assignee rows.
vi.mock("@multica/core/agents", () => ({ isAgentRuntimeBound: () => false }));
vi.mock("@multica/core/permissions", () => ({
  canAssignAgentToIssue: () => ({ allowed: true }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada Lovelace" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
  assigneeFrequencyOptions: () => ({ queryKey: ["frequency"] }),
}));
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

function renderPicker(
  kind: "assignee" | "delegate",
  onUpdate: (patch: Record<string, unknown>) => void,
) {
  return render(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <AssigneePicker
        kind={kind}
        assigneeType={null}
        assigneeId={null}
        onUpdate={onUpdate}
        open
        onOpenChange={() => {}}
      />
    </I18nProvider>,
  );
}

describe("AssigneePicker in delegate mode", () => {
  it("emits delegate_type / delegate_id, never the assignee pair", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker("delegate", onUpdate);

    await user.click(screen.getByText("Ada Lovelace"));
    expect(onUpdate).toHaveBeenCalledWith({
      delegate_type: "member",
      delegate_id: "user-1",
    });
  });

  it("clears with both halves null, so the server sees one validated pair", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderPicker("delegate", onUpdate);

    // "No delegate" is both the trigger label and the clear row; the row is
    // the one inside the popup, rendered after the trigger.
    const rows = screen.getAllByText("No delegate");
    await user.click(rows[rows.length - 1]!);
    expect(onUpdate).toHaveBeenCalledWith({
      delegate_type: null,
      delegate_id: null,
    });
  });

  it("offers no squads — the server rejects a squad delegate with a 400", () => {
    renderPicker("delegate", vi.fn());
    expect(screen.queryByText("Platform Squad")).toBeNull();
    expect(screen.queryByText("Squads")).toBeNull();
    // Control: the same fixture DOES show squads in assignee mode, so the
    // absence above is the discriminator and not a broken mock.
    screen.getByText("Members");
  });

  it("shows squads in assignee mode (control for the case above)", () => {
    renderPicker("assignee", vi.fn());
    screen.getByText("Platform Squad");
  });

  it("uses the delegate label sub-tree", () => {
    renderPicker("delegate", vi.fn());
    screen.getByPlaceholderText("Delegate to...");
    expect(screen.getAllByText("No delegate").length).toBeGreaterThan(0);
    expect(screen.queryByText("Unassigned")).toBeNull();
  });

  it("lets a runtime-less agent be a delegate but not an assignee", async () => {
    const user = userEvent.setup();
    const asDelegate = vi.fn();
    const { unmount } = renderPicker("delegate", asDelegate);
    await user.click(screen.getByText("CodeBot"));
    expect(asDelegate).toHaveBeenCalledWith({
      delegate_type: "agent",
      delegate_id: "agent-1",
    });
    unmount();

    const asAssignee = vi.fn();
    renderPicker("assignee", asAssignee);
    await user.click(screen.getByText("CodeBot"));
    expect(asAssignee).not.toHaveBeenCalled();
  });
});
