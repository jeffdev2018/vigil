// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";

const rotate = vi.hoisted(() => vi.fn());
const revoke = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/plugins", () => ({
  useRotatePluginToken: () => ({ mutateAsync: rotate, isPending: false }),
  useRevokePluginToken: () => ({ mutateAsync: revoke, isPending: false }),
}));

import { PluginTokenPanel } from "./plugin-token-panel";

describe("PluginTokenPanel", () => {
  it("asks before issuing, then shows the token and the signing secret once", async () => {
    rotate.mockResolvedValue({ token: "mpi_abc", signing_secret: "whsec_xyz" });
    const user = userEvent.setup();
    renderWithI18n(<PluginTokenPanel wsId="ws-1" installationId="inst-1" canManage />);

    await user.click(screen.getByRole("button", { name: "Issue a new token" }));
    expect(rotate).not.toHaveBeenCalled();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Issue a new token" }));
    expect(rotate).toHaveBeenCalledWith("inst-1");
    expect(await screen.findByText("mpi_abc")).toBeInTheDocument();
    expect(screen.getByText("whsec_xyz")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByText("mpi_abc")).not.toBeInTheDocument();
  });

  it("offers nothing to someone who cannot manage plugins", () => {
    renderWithI18n(<PluginTokenPanel wsId="ws-1" installationId="inst-1" canManage={false} />);
    expect(screen.getByRole("button", { name: "Issue a new token" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Revoke the token" })).toBeDisabled();
  });
});
