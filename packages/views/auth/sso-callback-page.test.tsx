// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { saveSessionResume } from "../common/session-resume";
import { SSO_DESKTOP_HANDOFF_KEY } from "./login-page";

const mockComplete = vi.hoisted(() => vi.fn());
const mockLoginWithToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({ api: { completeOIDCLogin: mockComplete } }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => ({ loginWithToken: mockLoginWithToken }) },
}));

import { SSOCallbackPage } from "./sso-callback-page";

const replace = vi.fn();
const adapter = {
  push: vi.fn(),
  replace,
  back: vi.fn(),
  pathname: "/login/sso",
  searchParams: new URLSearchParams(),
  hash: "",
  switchWorkspace: vi.fn(),
} as unknown as NavigationAdapter;

function setSearch(search: string) {
  window.history.replaceState(null, "", `/login/sso${search}`);
}

const render = (onTokenObtained?: () => void) =>
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SSOCallbackPage onTokenObtained={onTokenObtained} />
    </NavigationProvider>,
  );

beforeEach(() => {
  vi.clearAllMocks();
  mockLoginWithToken.mockResolvedValue({ id: "user-1" });
  window.sessionStorage.clear();
});

describe("SSOCallbackPage", () => {
  it("exchanges the code, logs in with the token and opens the workspace", async () => {
    setSearch("?code=abc&state=xyz");
    mockComplete.mockResolvedValue({ token: "jwt-1", user: { id: "user-1" }, workspace_slug: "acme" });
    const onTokenObtained = vi.fn();
    render(onTokenObtained);

    expect(screen.getByText("Completing sign-in...")).toBeInTheDocument();
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/acme"));
    expect(mockComplete).toHaveBeenCalledWith("abc", "xyz");
    expect(mockLoginWithToken).toHaveBeenCalledWith("jwt-1");
    expect(onTokenObtained).toHaveBeenCalled();
  });

  it("shows the server error with a way back to login", async () => {
    setSearch("?code=abc&state=xyz");
    mockComplete.mockRejectedValue(new Error("state mismatch"));
    render();

    await waitFor(() => expect(screen.getByText("SSO sign-in failed")).toBeInTheDocument());
    expect(screen.getByText("state mismatch")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to login" })).toHaveAttribute("href", "/login");
    expect(mockLoginWithToken).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("rejects a callback without code or state before calling the API", async () => {
    setSearch("?code=abc");
    render();
    await waitFor(() => expect(screen.getByText(/missing its code or state/)).toBeInTheDocument());
    expect(mockComplete).not.toHaveBeenCalled();
  });

  it("redirects to the desktop deep link with a token when the handoff flag is set, without logging the browser in", async () => {
    setSearch("?code=abc&state=xyz");
    saveSessionResume(SSO_DESKTOP_HANDOFF_KEY, true, 60_000);
    mockComplete.mockResolvedValue({ token: "jwt-desktop", user: { id: "user-1" }, workspace_slug: "acme" });

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      const onTokenObtained = vi.fn();
      render(onTokenObtained);

      await waitFor(() =>
        expect(hrefSetter).toHaveBeenCalledWith("multica://auth/callback?token=jwt-desktop"),
      );
      expect(
        await screen.findByRole("button", { name: "Open Multica Desktop" }),
      ).toBeInTheDocument();
      expect(mockLoginWithToken).not.toHaveBeenCalled();
      expect(onTokenObtained).not.toHaveBeenCalled();
      expect(replace).not.toHaveBeenCalled();
      // Consumed once: a later SSO login in this tab must not replay the handoff.
      expect(window.sessionStorage.getItem(SSO_DESKTOP_HANDOFF_KEY)).toBeNull();
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: originalLocation });
    }
  });

  it("logs in and redirects to the workspace as usual when the handoff flag is absent", async () => {
    setSearch("?code=abc&state=xyz");
    mockComplete.mockResolvedValue({ token: "jwt-1", user: { id: "user-1" }, workspace_slug: "acme" });
    const onTokenObtained = vi.fn();
    render(onTokenObtained);

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/acme"));
    expect(mockLoginWithToken).toHaveBeenCalledWith("jwt-1");
    expect(onTokenObtained).toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Open Multica Desktop" })).toBeNull();
  });
});
