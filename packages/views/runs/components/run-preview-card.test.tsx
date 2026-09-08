// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunPreview, TaskShareLink } from "@multica/core/runs";
import { renderWithI18n } from "../../test/i18n";

// Parsing and the openable/local matrix: packages/core/runs/schemas.test.ts.
// This file keeps the states a reader can actually see and the one rule the
// card exists to enforce — a link is rendered only when it can be opened.

const state = vi.hoisted(() => ({
  preview: undefined as RunPreview | undefined,
  links: [] as TaskShareLink[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/runs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runs")>()),
  useRunPreview: () => ({ data: state.preview }),
  useTaskShareLinks: () => ({ data: { links: state.links } }),
  useCreateTaskShareLink: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useRevokeTaskShareLink: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

import { RunPreviewCard } from "./run-preview-card";

const preview = (over: Partial<RunPreview>): RunPreview => ({
  status: "ready",
  scheme: "relay",
  url: "https://api.example/preview/abcdefgh/",
  port: 21000,
  expires_at: null,
  error: "",
  relay_available: true,
  ...over,
});

function render(machineName?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunPreviewCard taskId="t1" machineName={machineName} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.preview = undefined;
  state.links = [];
});

describe("RunPreviewCard", () => {
  it("renders nothing when the run has no preview", () => {
    const { container } = render();
    expect(container.innerHTML).toBe("");
  });

  it("offers the URL and an open link when the preview is relayed and ready", () => {
    state.preview = preview({});
    render();
    const open = screen.getByTestId("run-preview-open") as HTMLAnchorElement;
    expect(open.getAttribute("href")).toBe("https://api.example/preview/abcdefgh/");
    // Truncated for the layout, complete in the title: a reviewer pastes the
    // whole thing into a phone.
    const shown = screen.getByTitle("https://api.example/preview/abcdefgh/");
    expect(shown.textContent).not.toBe("");
  });

  it("names the machine and offers no link for a local preview", () => {
    state.preview = preview({ scheme: "loopback", url: "http://127.0.0.1:21000" });
    render("jeff-mbp");
    expect(screen.getByText(/jeff-mbp/)).toBeTruthy();
    // Acceptance 7: the web UI says the preview is local instead of handing
    // out a dead link.
    expect(screen.queryByTestId("run-preview-open")).toBeNull();
  });

  it("falls back to generic copy when the machine has no name", () => {
    state.preview = preview({ scheme: "loopback", url: "http://127.0.0.1:21000" });
    render();
    expect(screen.getByText(/local to the machine that ran it/)).toBeTruthy();
  });

  it("shows the port it is waiting on while starting, with no link", () => {
    state.preview = preview({ status: "starting", url: "" });
    render();
    expect(screen.getByText(/21000/)).toBeTruthy();
    expect(screen.queryByTestId("run-preview-open")).toBeNull();
  });

  it("shows the run script's own log tail when the preview failed", () => {
    state.preview = preview({ status: "error", url: "", error: "ERR_MODULE_NOT_FOUND: vite" });
    render();
    expect(screen.getByText(/ERR_MODULE_NOT_FOUND/)).toBeTruthy();
    expect(screen.queryByTestId("run-preview-open")).toBeNull();
  });

  it("says the preview ended and removes the link once the run stopped", () => {
    state.preview = preview({ status: "stopped", url: "" });
    render();
    expect(screen.getByText(/The run ended/)).toBeTruthy();
    expect(screen.queryByTestId("run-preview-open")).toBeNull();
  });

  it("warns when the machine hosting the preview went offline", () => {
    state.preview = preview({ status: "stale", url: "" });
    render();
    expect(screen.getByText(/went offline/)).toBeTruthy();
  });

  it("renders an unknown status without offering a link", () => {
    // An installed client against a newer server: show the state, never turn
    // it into something to click.
    state.preview = preview({ status: "hibernating", url: "https://api.example/preview/x/" });
    render();
    expect(screen.getByText("hibernating")).toBeTruthy();
    expect(screen.queryByTestId("run-preview-open")).toBeNull();
  });

  it("offers to create a link when a relayed preview has none yet", () => {
    state.preview = preview({ url: "" });
    render();
    expect(screen.getByText("Create a link")).toBeTruthy();
  });

  it("lists existing links with their use count and a revoke control", () => {
    state.preview = preview({});
    state.links = [
      { id: "l1", code: "abcdefgh", url: "https://api.example/preview/abcdefgh/", capabilities: ["preview"], expires_at: "2026-09-08T10:00:00Z", created_at: "2026-09-06T10:00:00Z", use_count: 3 },
    ];
    render();
    expect(screen.getByTestId("run-preview-link")).toBeTruthy();
    expect(screen.getByText("abcdefgh")).toBeTruthy();
    expect(screen.getByText("3 uses")).toBeTruthy();
    expect(screen.getAllByLabelText("Revoke").length).toBe(1);
  });
});
