// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The persisted-settings round trip (parsing/defaults) is covered by
// packages/core's approval_gates.go tests on the server side and by the
// existing timeout/spend/tools fields here; this suite proves only what the
// new "who may settle a gate" select adds.

const mockUpdateWorkspace = vi.hoisted(() => vi.fn(async (id: string, data: unknown) => ({ id, ...(data as object) })));
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: mockSetQueryData, invalidateQueries: mockInvalidateQueries }),
}));
vi.mock("@multica/core/api", () => ({ api: { updateWorkspace: mockUpdateWorkspace } }));
vi.mock("@multica/core/workspace/queries", () => ({ workspaceKeys: { list: () => ["workspaces"] } }));
vi.mock("sonner", () => ({ toast: { success: mockToastSuccess, error: mockToastError } }));

import { ApprovalGatesSetting, GATE_APPROVERS_ANY_MEMBER, GATE_APPROVERS_OWNER_ADMIN } from "./approval-gates-setting";

function workspace(approvers?: string): Workspace {
  return {
    id: "ws-1",
    name: "Acme",
    slug: "acme",
    settings: approvers ? { approval_gates: { approvers } } : {},
  } as Workspace;
}

beforeEach(() => {
  mockUpdateWorkspace.mockClear();
  mockInvalidateQueries.mockClear();
  mockSetQueryData.mockClear();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
});

// Base UI Select portals its popup onto document.body.
afterEach(() => cleanup());

async function pickApprovers(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole("combobox", { name: "Who may settle a gate" }));
  await user.click(await screen.findByRole("option", { name }));
}

describe("ApprovalGatesSetting approvers", () => {
  it("defaults to any member", () => {
    renderWithI18n(<ApprovalGatesSetting workspace={workspace()} canEdit />);
    expect(screen.getByRole("combobox", { name: "Who may settle a gate" }).textContent).toContain("Any member");
  });

  it("reflects a workspace already restricted to owners and admins", () => {
    renderWithI18n(<ApprovalGatesSetting workspace={workspace(GATE_APPROVERS_OWNER_ADMIN)} canEdit />);
    expect(screen.getByRole("combobox", { name: "Who may settle a gate" }).textContent).toContain("Owners and admins only");
  });

  it("persists the choice, keeping the other fields untouched", async () => {
    const user = userEvent.setup();
    renderWithI18n(<ApprovalGatesSetting workspace={workspace()} canEdit />);

    await pickApprovers(user, "Owners and admins only");

    expect(mockUpdateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        approval_gates: {
          timeout_minutes: 30,
          spend_threshold_usd_ticks: 100_000_000_000,
          sensitive_tools: expect.any(String),
          approvers: GATE_APPROVERS_OWNER_ADMIN,
        },
      },
    });
    expect(mockToastSuccess).toHaveBeenCalled();
  });

  it("disables the select when the reader may not edit workspace settings", () => {
    renderWithI18n(<ApprovalGatesSetting workspace={workspace()} canEdit={false} />);
    expect(screen.getByRole("combobox", { name: "Who may settle a gate" })).toBeDisabled();
  });
});

// Sanity: the option constants used above match server/internal/service/approval_gates.go.
describe("gate approver constants", () => {
  it("are the two values the server accepts", () => {
    expect(GATE_APPROVERS_ANY_MEMBER).toBe("any_member");
    expect(GATE_APPROVERS_OWNER_ADMIN).toBe("owner_admin");
  });
});
