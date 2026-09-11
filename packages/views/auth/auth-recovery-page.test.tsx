import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";

const retryAuthentication = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => {
  const state = { retryAuthentication };
  const useAuthStore = Object.assign(
    (selector: (s: typeof state) => unknown) => selector(state),
    { getState: () => state },
  );
  return { useAuthStore };
});

import { AuthRecoveryPage } from "./auth-recovery-page";

// Route-level wiring (which statuses show this page) is pinned in
// apps/web/app/[workspaceSlug]/layout.test.tsx and the desktop App.
describe("AuthRecoveryPage", () => {
  beforeEach(() => {
    retryAuthentication.mockClear();
  });

  it("explains the outage and retries authentication by default", () => {
    renderWithI18n(<AuthRecoveryPage />);

    expect(screen.getByText("Reconnecting to Multica")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retryAuthentication).toHaveBeenCalledOnce();
  });

  it("uses the caller's retry and disables it while retrying", () => {
    const onRetry = vi.fn();
    renderWithI18n(<AuthRecoveryPage onRetry={onRetry} isRetrying />);

    const button = screen.getByRole("button", { name: "Trying again..." });
    expect(button).toBeDisabled();
    expect(retryAuthentication).not.toHaveBeenCalled();
  });
});
