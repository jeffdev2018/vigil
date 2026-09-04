// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Multichannel digest (K64): the channels editor reads and writes
// morning_briefing.channels in the workspace settings.

const state = vi.hoisted(() => ({ update: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", () => ({ api: { updateWorkspace: state.update, triggerMorningBriefing: vi.fn() } }));

import { MorningBriefingSetting, morningBriefingPolicy } from "./morning-briefing-setting";

const workspace = { id: "ws-1", settings: { morning_briefing: { enabled: true, hour: 8, timezone: "UTC", channels: [{ type: "slack", chat_id: "C0123" }] } } } as unknown as Workspace;

describe("MorningBriefingSetting channels", () => {
  it("parses channels defensively", () => {
    expect(morningBriefingPolicy(workspace).channels).toEqual([{ type: "slack", chat_id: "C0123" }]);
    expect(morningBriefingPolicy({ id: "w", settings: { morning_briefing: { channels: "nope" } } } as unknown as Workspace).channels).toEqual([]);
  });

  it("lists the configured channels and persists an added one", async () => {
    state.update.mockResolvedValue(workspace);
    const qc = new QueryClient();
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <MorningBriefingSetting workspace={workspace} canEdit />
      </QueryClientProvider>,
    );
    expect((screen.getByLabelText("Chat id 1") as HTMLInputElement).value).toBe("C0123");
    fireEvent.click(screen.getByRole("button", { name: "Add a channel" }));
    fireEvent.change(screen.getByLabelText("Channel type 2"), { target: { value: "telegram" } });
    fireEvent.change(screen.getByLabelText("Chat id 2"), { target: { value: "-1001" } });
    fireEvent.blur(screen.getByLabelText("Chat id 2"));
    await waitFor(() => expect(state.update).toHaveBeenCalled());
    const settings = state.update.mock.calls.at(-1)![1].settings as { morning_briefing: { channels: unknown } };
    expect(settings.morning_briefing.channels).toEqual([{ type: "slack", chat_id: "C0123" }, { type: "telegram", chat_id: "-1001" }]);
  });
});
