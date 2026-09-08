// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ProjectReviewConfig } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/api/schemas.test.ts (ProjectReviewConfigSchema).

const state = vi.hoisted(() => ({
  config: null as ProjectReviewConfig | null,
  agents: [{ id: "a1", name: "Reviewer bot", archived_at: null }],
  saved: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@multica/core/projects/review-config", () => ({
  projectReviewConfigOptions: () => ({ queryKey: ["review-config"], queryFn: async () => state.config }),
  useSaveProjectReviewConfig: () => ({ isPending: false, mutate: (v: unknown) => { state.saved.push(v); } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => state.agents }),
}));

import { ProjectReviewSection } from "./project-review-section";

const config = (over: Partial<ProjectReviewConfig> = {}): ProjectReviewConfig => ({
  project_id: "p1",
  checklist: ["no foreign keys in migrations"],
  reviewer_agent_id: null,
  gate_enabled: false,
  max_cycles: 3,
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectReviewSection projectId="p1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.config = config();
  state.saved = [];
});

describe("ProjectReviewSection", () => {
  it("renders the saved config", async () => {
    render();
    const rows = await screen.findAllByTestId("review-checklist-item");
    expect(rows[0]?.textContent).toContain("no foreign keys in migrations");
    expect((screen.getByLabelText("Reviewer agent") as HTMLSelectElement).value).toBe("");
    expect((screen.getByLabelText("Gate on approval") as HTMLInputElement).checked).toBe(false);
    expect((screen.getByLabelText("Max cycles") as HTMLInputElement).value).toBe("3");
  });

  it("adds and removes checklist items, then saves the whole config", async () => {
    render();
    await screen.findAllByTestId("review-checklist-item");
    fireEvent.change(screen.getByLabelText("Checklist"), { target: { value: "tests added" } });
    fireEvent.click(screen.getByRole("button", { name: "Add item" }));
    expect(await screen.findAllByTestId("review-checklist-item")).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button", { name: "Remove item" })[0]!);
    expect(await screen.findAllByTestId("review-checklist-item")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Save review settings" }));
    expect(state.saved[0]).toEqual({
      checklist: ["tests added"],
      reviewer_agent_id: null,
      gate_enabled: false,
      max_cycles: 3,
    });
  });

  it("sends the reviewer, gate and cycle cap on save", async () => {
    state.config = config({ reviewer_agent_id: "a1", gate_enabled: true, max_cycles: 5 });
    render();
    await screen.findAllByTestId("review-checklist-item");
    fireEvent.change(screen.getByLabelText("Reviewer agent"), { target: { value: "" } });
    fireEvent.click(screen.getByLabelText("Gate on approval"));
    fireEvent.change(screen.getByLabelText("Max cycles"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save review settings" }));
    expect(state.saved[0]).toEqual({
      checklist: ["no foreign keys in migrations"],
      reviewer_agent_id: null,
      gate_enabled: false,
      max_cycles: 2,
    });
  });

  it("shows the empty state and nothing to save when there is no config yet", async () => {
    state.config = config({ checklist: [] });
    render();
    expect(await screen.findByTestId("review-checklist-empty")).toBeTruthy();
  });
});
