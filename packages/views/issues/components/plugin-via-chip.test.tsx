// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const installations = vi.hoisted(() => ({ list: [] as { id: string; name: string }[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/plugins", () => ({
  pluginInstallationsOptions: () => ({ queryKey: ["plugins", "installed"], queryFn: async () => ({ plugins: installations.list }) }),
}));

import { PluginViaChip } from "./plugin-via-chip";

function render(pluginId: string | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PluginViaChip pluginId={pluginId} />
    </QueryClientProvider>,
  );
}

describe("PluginViaChip", () => {
  it("names the plugin a comment was posted through", async () => {
    installations.list = [{ id: "inst-1", name: "Test plugin" }];
    render("inst-1");
    expect(await screen.findByText("via Test plugin")).toBeInTheDocument();
  });

  it("still says a plugin wrote it once the plugin is gone", async () => {
    installations.list = [];
    render("inst-gone");
    expect(await screen.findByText("via a plugin")).toBeInTheDocument();
  });

  it("renders nothing for an ordinary comment", () => {
    const { container } = render(null);
    expect(container.textContent).toBe("");
  });
});
