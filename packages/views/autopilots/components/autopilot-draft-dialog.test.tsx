// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AutopilotDraft } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import en from "../../locales/en/autopilots.json";
import { renderWithI18n } from "../../test/i18n";

// Boundary parsing (draft/propose, 503, malformed bodies) lives in
// packages/core/autopilots/draft.test.ts. This suite keeps the flow: sentence
// → preview → create, what propose is sent, and that a workspace without a
// model is offered the manual form instead of a dead button.

const state = vi.hoisted(() => ({
  draft: vi.fn(),
  propose: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [{ id: "a-1", name: "Ada", archived_at: null }],
  }),
}));
vi.mock("@multica/core/autopilots/mutations", () => ({
  useDraftAutopilot: () => ({ mutate: state.draft, isPending: false }),
  useProposeAutopilot: () => ({ mutateAsync: state.propose, isPending: false }),
}));

import { AutopilotDraftDialog } from "./autopilot-draft-dialog";

const draft: AutopilotDraft = {
  title: "Monday open tickets",
  cron_expression: "0 9 * * 1",
  timezone: "Europe/Paris",
  description: "List the open tickets and post them.",
  execution_mode: "create_issue",
  issue_title_template: "Open tickets {{date}}",
  reason: "Weekly on Monday at 09:00.",
  next_runs: ["2026-09-14T07:00:00Z", "2026-09-21T07:00:00Z", "2026-09-28T07:00:00Z"],
};

function render(onWriteYourself = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <AutopilotDraftDialog open onOpenChange={vi.fn()} onWriteYourself={onWriteYourself} />
    </QueryClientProvider>,
  );
}

/** Walk the sentence step and land on the preview. */
async function toPreview() {
  state.draft.mockImplementation(
    (_input: unknown, opts: { onSuccess?: (d: AutopilotDraft) => void }) => opts.onSuccess?.(draft),
  );
  render();
  fireEvent.change(screen.getByLabelText(en.from_sentence.sentence_label), {
    target: { value: "Every Monday at 9, list the open tickets." },
  });
  fireEvent.click(screen.getByRole("button", { name: en.from_sentence.draft }));
  return screen.findByTestId("autopilot-draft-preview");
}

describe("AutopilotDraftDialog", () => {
  beforeEach(() => {
    cleanup();
    state.draft.mockReset();
    state.propose.mockReset().mockResolvedValue({
      autopilot: { id: "ap-1", status: "paused" },
      decision_id: "d-1",
      next_runs: draft.next_runs,
    });
  });

  it("sends the sentence with the browser's timezone", async () => {
    await toPreview();
    const [input] = state.draft.mock.calls[0] as [{ text: string; timezone?: string }];
    expect(input.text).toBe("Every Monday at 9, list the open tickets.");
    expect(input.timezone).toBeTruthy();
  });

  it("previews the schedule the model proposed, editable, with its next runs", async () => {
    const preview = await toPreview();
    expect(preview.textContent).toContain(draft.reason);
    expect(
      (screen.getByLabelText(en.from_sentence.field_cron) as HTMLInputElement).value,
    ).toBe("0 9 * * 1");
    expect(
      (screen.getByLabelText(en.from_sentence.field_timezone) as HTMLInputElement).value,
    ).toBe("Europe/Paris");
    expect(
      (screen.getByLabelText(en.from_sentence.field_prompt) as HTMLTextAreaElement).value,
    ).toContain("List the open tickets");
    expect(
      (screen.getByLabelText(en.from_sentence.field_issue_title) as HTMLInputElement).value,
    ).toBe("Open tickets {{date}}");
    expect(preview.querySelectorAll("li")).toHaveLength(3);
  });

  it("creates paused with the edited fields and the chosen agent", async () => {
    await toPreview();
    fireEvent.change(screen.getByLabelText(en.from_sentence.field_title), {
      target: { value: "Monday triage" },
    });
    fireEvent.click(screen.getByLabelText(en.from_sentence.field_assignee));
    fireEvent.click(await screen.findByRole("option", { name: "Ada" }));

    fireEvent.click(screen.getByRole("button", { name: en.from_sentence.create_paused }));
    await waitFor(() => expect(state.propose).toHaveBeenCalled());
    expect(state.propose.mock.calls[0]?.[0]).toMatchObject({
      title: "Monday triage",
      cron_expression: "0 9 * * 1",
      timezone: "Europe/Paris",
      execution_mode: "create_issue",
      assignee_id: "a-1",
      activate: false,
    });
  });

  it("activates in the one propose request, not by patching afterwards", async () => {
    await toPreview();
    fireEvent.click(screen.getByLabelText(en.from_sentence.field_assignee));
    fireEvent.click(await screen.findByRole("option", { name: "Ada" }));

    fireEvent.click(screen.getByRole("button", { name: en.from_sentence.create_active }));
    await waitFor(() => expect(state.propose).toHaveBeenCalled());
    expect(state.propose).toHaveBeenCalledTimes(1);
    expect(state.propose.mock.calls[0]?.[0]).toMatchObject({ assignee_id: "a-1", activate: true });
  });

  it("offers the manual form when no model is configured (503)", async () => {
    const write = vi.fn();
    state.draft.mockImplementation((_input: unknown, opts: { onError?: (e: unknown) => void }) =>
      opts.onError?.(new ApiError("no model is configured to draft", 503, "Service Unavailable")),
    );
    render(write);
    fireEvent.change(screen.getByLabelText(en.from_sentence.sentence_label), {
      target: { value: "every monday" },
    });
    fireEvent.click(screen.getByRole("button", { name: en.from_sentence.draft }));

    const notice = await screen.findByRole("alert");
    expect(notice.textContent).toContain(en.from_sentence.no_model);
    fireEvent.click(screen.getByRole("button", { name: en.from_sentence.write_yourself }));
    expect(write).toHaveBeenCalled();
  });
});
