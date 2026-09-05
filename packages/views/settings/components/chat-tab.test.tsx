// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useVoiceStore } from "@multica/core/voice";
import { renderWithI18n } from "../../test/i18n";

// The store's own matrix (auto vs explicit, locale resolution) is canonical in
// packages/core/voice/voice.test.ts; this covers the wiring only.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// The floating-window switch needs the app's registered chat store; this tab
// test is about the voice row, so stand in for it.
const chatState = vi.hoisted(() => ({
  floatingChatEnabled: true,
  setFloatingChatEnabled: vi.fn(),
}));
vi.mock("@multica/core/chat", () => {
  const useChatStore = Object.assign(
    (selector: (s: typeof chatState) => unknown) => selector(chatState),
    { getState: () => chatState },
  );
  return { useChatStore };
});

import { ChatTab } from "./chat-tab";

describe("ChatTab — voice language", () => {
  beforeEach(() => {
    useVoiceStore.getState().setVoiceLanguage("auto");
  });

  it("shows the stored preference", () => {
    useVoiceStore.getState().setVoiceLanguage("fr");

    renderWithI18n(<ChatTab />);

    expect(screen.getByLabelText("Voice language")).toHaveTextContent("Français");
  });

  it("persists a new choice to the voice store", async () => {
    const user = userEvent.setup();
    renderWithI18n(<ChatTab />);

    await user.click(screen.getByLabelText("Voice language"));
    await user.click(await screen.findByRole("option", { name: "日本語" }));

    await waitFor(() =>
      expect(useVoiceStore.getState().voiceLanguage).toBe("ja"),
    );
  });
});
