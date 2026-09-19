// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const updateWorkspace = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/api", () => ({ api: { updateWorkspace } }));

import { StandupSetting } from "./standup-setting";

const at = (blocked_hours: number) =>
  ({ id: "ws-1", settings: { standup: { enabled: true, blocked_hours, weekly_retro: false } } }) as unknown as Workspace;

describe("StandupSetting", () => {
  // Regression: a workspace:updated patch changes the prop without a remount.
  // The buffered hours kept 24 and a blur wrote it over the other admin's 48.
  it("follows a threshold changed elsewhere and does not write it back on blur", () => {
    const qc = new QueryClient();
    const ui = (ws: Workspace) => (
      <QueryClientProvider client={qc}>
        <StandupSetting workspace={ws} canEdit />
      </QueryClientProvider>
    );
    const { rerender } = renderWithI18n(ui(at(24)));
    rerender(ui(at(48)));
    const input = screen.getByRole("spinbutton");
    expect(input).toHaveValue(48);
    fireEvent.blur(input);
    expect(updateWorkspace).not.toHaveBeenCalled();
  });
});
