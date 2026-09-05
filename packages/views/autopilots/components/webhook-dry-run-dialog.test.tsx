import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WebhookTriggerDryRunResult } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockDryRun = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/autopilots", () => ({
  useDryRunAutopilotWebhookTrigger: () => ({ mutateAsync: mockDryRun, isPending: false }),
}));

import { WebhookDryRunDialog } from "./webhook-dry-run-dialog";

const TRIGGER = {
  id: "trg-1",
  provider: "github",
  event_filters: [{ event: "pull_request", actions: ["opened"] }],
};

function verdict(overrides: Partial<WebhookTriggerDryRunResult> = {}): WebhookTriggerDryRunResult {
  return {
    would_run: true,
    reason_code: null,
    explanation: "",
    matched_filters: [],
    event: "github.pull_request.opened",
    ...overrides,
  };
}

function renderDialog(props: Partial<React.ComponentProps<typeof WebhookDryRunDialog>> = {}) {
  return renderWithI18n(
    <WebhookDryRunDialog
      open
      onOpenChange={() => {}}
      autopilotId="ap-1"
      trigger={TRIGGER}
      {...props}
    />,
  );
}

beforeEach(() => {
  mockDryRun.mockReset().mockResolvedValue(verdict());
});

describe("WebhookDryRunDialog", () => {
  it("opens on a sample payload the trigger's own filter would accept", () => {
    renderDialog();
    const box = screen.getByLabelText("Event payload (JSON)") as HTMLTextAreaElement;
    expect(JSON.parse(box.value)).toEqual({ action: "opened" });
    // The header is what GitHub event inference reads; showing it makes the
    // verdict reproducible with curl.
    expect(screen.getByText(/X-GitHub-Event: pull_request/)).toBeInTheDocument();
  });

  it("sends the edited payload and shows the would-run verdict", async () => {
    const user = userEvent.setup();
    mockDryRun.mockResolvedValue(
      verdict({ explanation: "touches auth", matched_filters: [{ event: "pull_request", actions: ["opened"] }] }),
    );
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Run dry-run" }));

    await waitFor(() => {
      expect(screen.getByText("This event would run the autopilot")).toBeInTheDocument();
    });
    expect(mockDryRun).toHaveBeenCalledWith({
      autopilotId: "ap-1",
      triggerId: "trg-1",
      payload: { action: "opened" },
      headers: { "X-GitHub-Event": "pull_request" },
    });
    expect(screen.getByText("touches auth")).toBeInTheDocument();
    expect(screen.getByText("pull_request:opened")).toBeInTheDocument();
  });

  it("localizes the blocking reason with the delivery vocabulary", async () => {
    const user = userEvent.setup();
    mockDryRun.mockResolvedValue(
      verdict({ would_run: false, reason_code: "criteria_not_matched", explanation: "staging only" }),
    );
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Run dry-run" }));

    await waitFor(() => {
      expect(screen.getByText("This event would not run the autopilot")).toBeInTheDocument();
    });
    expect(screen.getByText("Criteria not matched")).toBeInTheDocument();
    expect(screen.getByText("staging only")).toBeInTheDocument();
  });

  it("never presents an unreadable response as a routing decision", async () => {
    const user = userEvent.setup();
    mockDryRun.mockResolvedValue(verdict({ would_run: false, reason_code: "unreadable", event: "" }));
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Run dry-run" }));

    await waitFor(() => {
      expect(screen.getByText("The verdict could not be read")).toBeInTheDocument();
    });
    expect(screen.queryByText("This event would not run the autopilot")).not.toBeInTheDocument();
  });

  it("refuses to spend a classifier call on unparseable JSON", async () => {
    const user = userEvent.setup();
    renderDialog();
    const box = screen.getByLabelText("Event payload (JSON)");
    await user.clear(box);
    await user.type(box, "not json");

    expect(screen.getByRole("button", { name: "Run dry-run" })).toBeDisabled();
    expect(mockDryRun).not.toHaveBeenCalled();
  });

  it("drops a stale verdict as soon as the payload is edited", async () => {
    const user = userEvent.setup();
    renderDialog();
    await user.click(screen.getByRole("button", { name: "Run dry-run" }));
    await waitFor(() => {
      expect(screen.getByText("This event would run the autopilot")).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText("Event payload (JSON)"), " ");

    expect(screen.queryByText("This event would run the autopilot")).not.toBeInTheDocument();
  });

  it("replays a stored delivery body verbatim", () => {
    renderDialog({ initialPayload: '{\n  "number": 42\n}', initialHeaders: { "X-Event-Type": "deploy" } });
    const box = screen.getByLabelText("Event payload (JSON)") as HTMLTextAreaElement;
    expect(JSON.parse(box.value)).toEqual({ number: 42 });
    expect(screen.getByText(/X-Event-Type: deploy/)).toBeInTheDocument();
  });
});
