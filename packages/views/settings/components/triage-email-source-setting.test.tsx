// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  create: vi.fn(),
  sources: [] as { kind: string }[],
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/triage", () => ({
  triageStatsOptions: () => ({
    queryKey: ["triage-stats"],
    queryFn: async () => ({ pending: 0, shadow_pending: 0, dropped_24h: 0, oldest_pending_age_seconds: 0, sources: state.sources }),
  }),
  useCreateTriageEmailSource: () => ({ mutateAsync: state.create, isPending: false }),
}));

import { TriageEmailSourceSetting } from "./triage-email-source-setting";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TriageEmailSourceSetting wsId="ws-1" canEdit />
    </QueryClientProvider>,
  );
}

describe("TriageEmailSourceSetting", () => {
  it("shows the endpoint once, masked, after enabling intake", async () => {
    state.sources = [];
    state.create.mockResolvedValue({
      id: "src-1",
      mode: "gate",
      path: "/api/triage/inbound/email/mti_secret",
      url: "https://multica.test/api/triage/inbound/email/mti_secret",
      token: "mti_secret",
    });
    render();

    fireEvent.click(await screen.findByRole("button", { name: "Enable email intake" }));
    await waitFor(() => expect(state.create).toHaveBeenCalled());

    // The endpoint is a bearer credential: it renders masked until the user
    // asks for it, exactly like the autopilot webhook URL.
    const hidden = await screen.findByRole("button", { name: /hidden/i });
    expect(hidden.textContent).not.toContain("mti_secret");
    fireEvent.click(hidden);
    expect(await screen.findByText("https://multica.test/api/triage/inbound/email/mti_secret")).toBeTruthy();
  });

  it("offers rotation once a source exists, and falls back to the path without a public URL", async () => {
    state.sources = [{ kind: "email" }];
    state.create.mockResolvedValue({
      id: "src-1",
      mode: "gate",
      path: "/api/triage/inbound/email/mti_rotated",
      token: "mti_rotated",
    });
    render();

    const rotate = await screen.findByRole("button", { name: "Rotate token" });
    fireEvent.click(rotate);
    await waitFor(() => expect(state.create).toHaveBeenCalled());
    fireEvent.click(await screen.findByRole("button", { name: /hidden/i }));
    expect(await screen.findByText("/api/triage/inbound/email/mti_rotated")).toBeTruthy();
  });

  it("does not offer intake to a member who cannot manage the workspace", async () => {
    state.sources = [];
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <TriageEmailSourceSetting wsId="ws-1" canEdit={false} />
      </QueryClientProvider>,
    );
    const button = await screen.findByRole("button", { name: "Enable email intake" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
  });
});
