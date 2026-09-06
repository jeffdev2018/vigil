import { useImperativeHandle, useRef, useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Off-peak batch lane (K45). One switch on the autopilot: may its SCHEDULED
// runs wait for the workspace's cheap hours? The window arithmetic behind
// `hasUsableBatchWindow` is covered in packages/core/batch-window/schemas.test.ts;
// this suite covers what the dialog does with the answer — offer the opt-in,
// carry it into the save, and refuse to offer a lane no window backs.

const state = vi.hoisted(() => ({
  window: { enabled: false, start_local_time: "", end_local_time: "", timezone: "UTC" },
}));
const mockUpdateAutopilot = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ name: "Acme" }) }));
vi.mock("@multica/core/batch-window", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/batch-window")>()),
  batchWindowOptions: () => ({
    queryKey: ["batch-window", JSON.stringify(state.window)],
    queryFn: async () => state.window,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: async () => [
      { id: "agent-1", name: "Scout", description: "Researches things", archived_at: null, runtime_id: "runtime-1" },
    ],
  }),
  squadListOptions: (wsId: string) => ({ queryKey: ["squads", wsId], queryFn: async () => [] }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({ queryKey: ["projects", wsId], queryFn: async () => [] }),
}));
vi.mock("@multica/core/autopilots/queries", () => ({
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => ({ next_runs: [] }),
    retry: false,
  }),
}));
vi.mock("@multica/core/autopilots/mutations", () => ({
  useCreateAutopilot: () => ({ mutateAsync: vi.fn() }),
  useCreateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
  useUpdateAutopilot: () => ({ mutateAsync: mockUpdateAutopilot }),
  useUpdateAutopilotTrigger: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../editor", () => ({
  TitleEditor: ({ ref, defaultValue, placeholder, onChange }: any) => {
    const [value, setValue] = useState(defaultValue ?? "");
    const inputRef = useRef<HTMLInputElement>(null);
    useImperativeHandle(ref, () => ({
      getText: () => value,
      focus: () => inputRef.current?.focus(),
      focusAtCoords: () => inputRef.current?.focus(),
    }));
    return (
      <input
        ref={inputRef}
        aria-label="title"
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          setValue(e.target.value);
          onChange?.(e.target.value);
        }}
      />
    );
  },
  ContentEditor: ({ placeholder }: any) => <textarea aria-label="runbook" placeholder={placeholder} />,
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span>{actorId}</span>,
}));
vi.mock("./subscriber-multi-select", () => ({
  SubscriberMultiSelect: () => <div data-testid="subscriber-multi-select" />,
}));
vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: ({ triggerRender }: { triggerRender: React.ReactElement }) => triggerRender,
}));
vi.mock("./pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { AutopilotDialog } from "./autopilot-dialog";

const AUTOPILOT_ID = "ap-1";
const TOGGLE = "Can wait for off-peak hours";

function render(batchEligible: boolean) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AutopilotDialog
        mode="edit"
        open
        onOpenChange={vi.fn()}
        autopilotId={AUTOPILOT_ID}
        initial={{
          title: "Nightly sweep",
          description: "",
          project_id: null,
          assignee_type: "agent",
          assignee_id: "agent-1",
          execution_mode: "run_only",
          batch_eligible: batchEligible,
          subscriber_user_ids: [],
        }}
        triggers={[]}
        collaborators={[]}
        canManageAccess={false}
      />
    </QueryClientProvider>,
  );
}

describe("AutopilotDialog off-peak opt-in", () => {
  beforeEach(() => {
    mockUpdateAutopilot.mockReset().mockResolvedValue({ id: AUTOPILOT_ID });
    state.window = { enabled: false, start_local_time: "", end_local_time: "", timezone: "UTC" };
  });

  it("offers the opt-in with the trade spelled out once a window exists", async () => {
    state.window = { enabled: true, start_local_time: "22:00", end_local_time: "06:00", timezone: "UTC" };
    render(false);
    const toggle = await screen.findByLabelText(TOGGLE);
    await waitFor(() => expect(toggle.getAttribute("aria-disabled")).not.toBe("true"));
    // Both halves of the trade must be on screen: cheaper, and a start time
    // that moves — an opt-in that only advertises the saving is a trap.
    const hint = screen.getByText(/queue behind everything urgent/);
    expect(hint.textContent).toContain("the start time varies");
    // And the one thing it never does, since "it can wait" reads as "it is now
    // slow" otherwise.
    expect(hint.textContent).toContain("Running it now is never delayed.");
  });

  it("carries the opt-in into the save", async () => {
    state.window = { enabled: true, start_local_time: "22:00", end_local_time: "06:00", timezone: "UTC" };
    render(false);
    const toggle = await screen.findByLabelText(TOGGLE);
    await waitFor(() => expect(toggle.getAttribute("aria-disabled")).not.toBe("true"));
    await userEvent.click(toggle);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockUpdateAutopilot).toHaveBeenCalled());
    expect(mockUpdateAutopilot.mock.calls[0]?.[0]).toMatchObject({ batch_eligible: true });
  });

  it("reflects an autopilot that already opted in", async () => {
    state.window = { enabled: true, start_local_time: "22:00", end_local_time: "06:00", timezone: "UTC" };
    render(true);
    const toggle = await screen.findByLabelText(TOGGLE);
    await waitFor(() => expect(toggle.getAttribute("aria-checked")).toBe("true"));
  });

  it("does not offer a lane no window backs, and says where to create one", async () => {
    render(false);
    const toggle = await screen.findByLabelText(TOGGLE);
    expect(toggle.getAttribute("aria-disabled")).toBe("true");
    expect(
      screen.getByText(
        "This workspace has no off-peak window yet. An admin sets one in Settings → Workspace.",
      ),
    ).toBeInTheDocument();
  });

  it("saves nothing new when the window is unusable, whatever the stored flag says", async () => {
    // A drifted window (enabled but with times the scheduler cannot apply)
    // must read exactly like no window: the switch shows off and stays off.
    state.window = { enabled: true, start_local_time: "03:00", end_local_time: "03:00", timezone: "UTC" };
    render(true);
    const toggle = await screen.findByLabelText(TOGGLE);
    await waitFor(() => expect(toggle.getAttribute("aria-disabled")).toBe("true"));
    expect(toggle.getAttribute("aria-checked")).not.toBe("true");
  });
});
