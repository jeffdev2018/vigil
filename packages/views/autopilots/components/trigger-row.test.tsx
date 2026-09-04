import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotTrigger } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockCreateTrigger = vi.hoisted(() => vi.fn());
const mockUpdateTrigger = vi.hoisted(() => vi.fn());
const mockDeleteTrigger = vi.hoisted(() => vi.fn());
const mockRotateToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://api.example.com" },
  ApiError: class ApiError extends Error {},
}));

vi.mock("@multica/core/autopilots/queries", () => ({
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", wsId, expr, tz],
    queryFn: async () => ({ next_runs: ["2126-07-14T01:00:00Z"] }),
    retry: false,
  }),
}));

vi.mock("@multica/core/autopilots/mutations", () => ({
  useCreateAutopilotTrigger: () => ({ mutateAsync: mockCreateTrigger }),
  useUpdateAutopilotTrigger: () => ({ mutateAsync: mockUpdateTrigger, isPending: false }),
  useDeleteAutopilotTrigger: () => ({ mutateAsync: mockDeleteTrigger }),
  useRotateAutopilotTriggerWebhookToken: () => ({
    mutateAsync: mockRotateToken,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock("./pickers/timezone-picker", () => ({
  TimezonePicker: ({ value }: { value: string }) => <div data-testid="timezone-picker">{value}</div>,
}));

import { AddTriggerDialog, TriggerRow } from "./trigger-row";

const AUTOPILOT_ID = "ap-1";

function trigger(overrides: Partial<AutopilotTrigger> = {}): AutopilotTrigger {
  return {
    id: "trg-1",
    autopilot_id: AUTOPILOT_ID,
    kind: "schedule",
    enabled: true,
    cron_expression: "0 8 * * *",
    timezone: "UTC",
    next_run_at: null,
    webhook_token: null,
    label: null,
    last_fired_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  mockCreateTrigger.mockReset().mockResolvedValue({ id: "trg-new" });
  mockUpdateTrigger.mockReset().mockResolvedValue({ id: "trg-1" });
  mockDeleteTrigger.mockReset().mockResolvedValue(undefined);
  mockRotateToken.mockReset().mockResolvedValue({ id: "trg-1" });
});

describe("AddTriggerDialog", () => {
  it("sends the firing band the schedule editor was set to", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <AddTriggerDialog open onOpenChange={vi.fn()} autopilotId={AUTOPILOT_ID} />,
    );

    // "Sometime between" turns the fixed time into a band; the band is its own
    // field on the wire — it is not encoded in the cron expression.
    await user.click(screen.getByRole("checkbox", { name: /Sometime between/ }));
    await user.click(screen.getByRole("button", { name: "Add trigger" }));

    await waitFor(() => expect(mockCreateTrigger).toHaveBeenCalledTimes(1));
    expect(mockCreateTrigger.mock.calls[0]?.[0]).toMatchObject({
      autopilotId: AUTOPILOT_ID,
      kind: "schedule",
      window_minutes: 60,
    });
  });

  it("omits window_minutes when the schedule fires at an exact time", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <AddTriggerDialog open onOpenChange={vi.fn()} autopilotId={AUTOPILOT_ID} />,
    );

    await user.click(screen.getByRole("button", { name: "Add trigger" }));

    await waitFor(() => expect(mockCreateTrigger).toHaveBeenCalledTimes(1));
    expect(mockCreateTrigger.mock.calls[0]?.[0].window_minutes).toBeUndefined();
  });
});

describe("TriggerRow schedule readback", () => {
  it("describes the band, not just the band's start", () => {
    renderWithQuery(
      <TriggerRow trigger={trigger({ window_minutes: 120 })} autopilotId={AUTOPILOT_ID} canWrite />,
    );

    expect(screen.getByText(/sometime between 08:00 and 10:00/i)).toBeInTheDocument();
  });

  it("says the exact time when there is no band", () => {
    renderWithQuery(
      <TriggerRow trigger={trigger()} autopilotId={AUTOPILOT_ID} canWrite />,
    );

    expect(screen.getByText(/at 08:00/i)).toBeInTheDocument();
  });
});

describe("TriggerRow enable toggle", () => {
  it("disables an enabled trigger through the update mutation", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <TriggerRow trigger={trigger()} autopilotId={AUTOPILOT_ID} canWrite />,
    );

    await user.click(screen.getByRole("switch", { name: "Enable trigger" }));

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toMatchObject({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-1",
      enabled: false,
    });
  });

  it("is not offered to a reader", () => {
    renderWithQuery(
      <TriggerRow trigger={trigger()} autopilotId={AUTOPILOT_ID} canWrite={false} />,
    );

    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });
});

describe("TriggerRow edit dialog", () => {
  it("patches the label, cron and band of a schedule trigger", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <TriggerRow trigger={trigger()} autopilotId={AUTOPILOT_ID} canWrite />,
    );

    await user.click(screen.getByRole("button", { name: "Edit trigger" }));
    await user.type(screen.getByLabelText("Label (optional)"), "Morning sweep");
    await user.click(screen.getByRole("checkbox", { name: /Sometime between/ }));
    await user.click(screen.getByRole("button", { name: "Save trigger" }));

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    expect(mockUpdateTrigger.mock.calls[0]?.[0]).toMatchObject({
      autopilotId: AUTOPILOT_ID,
      triggerId: "trg-1",
      label: "Morning sweep",
      window_minutes: 60,
    });
    expect(mockUpdateTrigger.mock.calls[0]?.[0].cron_expression).toMatch(/(^|\s)0 8 \* \* \*$/);
  });

  it("patches criteria and filters of a webhook trigger, and no cron", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <TriggerRow
        trigger={trigger({
          kind: "webhook",
          cron_expression: null,
          timezone: null,
          webhook_token: "awt_token",
          webhook_path: "/api/webhooks/autopilots/awt_token",
          event_match_criteria: "",
        })}
        autopilotId={AUTOPILOT_ID}
        canWrite
      />,
    );

    await user.click(screen.getByRole("button", { name: "Edit trigger" }));
    await user.type(
      screen.getByLabelText("Only run when…"),
      "only production incidents",
    );
    await user.click(screen.getByRole("button", { name: "Save trigger" }));

    await waitFor(() => expect(mockUpdateTrigger).toHaveBeenCalledTimes(1));
    const patch = mockUpdateTrigger.mock.calls[0]?.[0];
    expect(patch).toMatchObject({
      triggerId: "trg-1",
      event_match_criteria: "only production incidents",
      event_filters: [],
    });
    expect(patch.cron_expression).toBeUndefined();
    expect(patch.window_minutes).toBeUndefined();
  });
});
