import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  installUpdate: vi.fn(),
  openExternal: vi.fn(),
}));

const translations = {
  desktop: {
    updates: {
      notification_ready_title: "Update ready",
      notification_ready_body: "v{{version}} will be applied on next launch.",
      see_changelog: "See changelog",
      restart_now: "Restart now",
      dismiss: "Dismiss",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (
      selector: (resources: typeof translations) => string,
      values?: Record<string, string>,
    ) => {
      const template = selector(translations);
      return Object.entries(values ?? {}).reduce(
        (result, [key, value]) => result.replace(`{{${key}}}`, value),
        template,
      );
    },
  }),
}));

import { UpdateNotification } from "./update-notification";

type UpdateDownloadedListener = (info: {
  version: string;
  releaseNotes?: string;
}) => void;

describe("UpdateNotification", () => {
  let updateDownloaded: UpdateDownloadedListener;

  beforeEach(() => {
    mocks.installUpdate.mockReset().mockResolvedValue(undefined);
    mocks.openExternal.mockReset().mockResolvedValue(undefined);

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: { openExternal: mocks.openExternal },
    });
    Object.defineProperty(window, "updater", {
      configurable: true,
      value: {
        onUpdateDownloaded: (listener: UpdateDownloadedListener) => {
          updateDownloaded = listener;
          return vi.fn();
        },
        installUpdate: mocks.installUpdate,
      },
    });
  });

  it("opens the downloaded version's changelog from the update prompt", () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    expect(screen.queryByRole("button", { name: "Later" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "See changelog" }));

    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://multica.ai/changelog#release-0-4-27",
    );
  });

  it("still installs the update immediately from the primary action", () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    fireEvent.click(screen.getByRole("button", { name: "Restart now" }));

    expect(mocks.installUpdate).toHaveBeenCalledOnce();
  });

  // Every string in this notification used to be hardcoded English with no
  // useT — the ONE component every desktop user sees at every update,
  // regardless of their app locale.
  it("renders the ready title and body through i18n, with the version interpolated", () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    expect(screen.getByText("Update ready")).toBeInTheDocument();
    expect(
      screen.getByText("v0.4.27 will be applied on next launch."),
    ).toBeInTheDocument();
  });

  it("gives the icon-only close button an accessible name", () => {
    render(<UpdateNotification />);
    act(() => updateDownloaded({ version: "0.4.27" }));

    const dismiss = screen.getByRole("button", { name: "Dismiss" });
    fireEvent.click(dismiss);

    expect(screen.queryByText("Update ready")).not.toBeInTheDocument();
  });
});
