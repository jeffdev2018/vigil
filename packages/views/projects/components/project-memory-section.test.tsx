// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import projects from "../../locales/en/projects.json";
import common from "../../locales/en/common.json";
import { ProjectMemorySection, TeachProjectFromReviewButton } from "./project-memory-section";

const api = vi.hoisted(() => ({
  getProjectMemory: vi.fn(),
  updateProjectMemory: vi.fn(),
  getProjectMemoryHistory: vi.fn(),
  restoreProjectMemory: vi.fn(),
  getProjectMemoryUsage: vi.fn(),
}));
vi.mock("@multica/core/api", async (original) => ({ ...await original<object>(), api }));
const initial = { rules: ["Use pnpm"], revision: 3, reviewed_by: "user-1", reviewed_at: null, expires_at: null, expired: false };
const emptyUsage = {
  since: "2026-08-06T00:00:00Z",
  until: "2026-09-05T00:00:00Z",
  started_runs: 0,
  recorded_runs: 0,
  unrecorded_runs: 0,
  runs_with_project_memory: 0,
  versions: [],
};
function wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { projects, common } }}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  );
}
function mount(canEdit = true) {
  return render(<ProjectMemorySection wsId="ws-1" projectId="project-1" canEdit={canEdit} />, { wrapper });
}

describe("ProjectMemorySection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getProjectMemory.mockResolvedValue(initial);
    api.getProjectMemoryHistory.mockResolvedValue({ versions: [], next_before_revision: null });
    api.getProjectMemoryUsage.mockResolvedValue(emptyUsage);
    api.updateProjectMemory.mockResolvedValue({ ...initial, revision: 4 });
  });
  it("publishes the edited rules against the displayed revision", async () => {
    const user = userEvent.setup(); mount();
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const input = screen.getByRole("textbox", { name: "Shared rules" });
    await user.clear(input);
    await user.type(input, "Use pnpm\nConfirm the project timezone");
    await user.click(screen.getByRole("button", { name: "Publish rules" }));
    expect(api.updateProjectMemory).toHaveBeenCalledWith("project-1", ["Use pnpm", "Confirm the project timezone"], 3, null, undefined);
  });
  it("keeps a conflicting draft and allows review of the refreshed version", async () => {
    const user = userEvent.setup(); mount();
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const input = screen.getByRole("textbox", { name: "Shared rules" });
    await user.type(input, " workspaces");
    api.getProjectMemory.mockResolvedValue({ ...initial, rules: ["A newer rule"], revision: 4 });
    api.updateProjectMemory.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
    await user.click(screen.getByRole("button", { name: "Publish rules" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Someone changed this memory");
    expect(input).toHaveValue("Use pnpm workspaces");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByRole("textbox", { name: "Shared rules" })).toHaveValue("A newer rule");
  });
  it("lets members read without publishing", async () => {
    mount(false);
    expect(await screen.findByText("Use pnpm")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });
  it("reports unavailable memory without offering an empty replacement", async () => {
    api.getProjectMemory.mockRejectedValue(new Error("offline")); mount();
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be loaded");
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });
  it("previews an expired version, restores against the current revision and preserves a conflicting review", async () => {
    const old = { ...initial, rules: ["Retired rule"], revision: 1, expires_at: "2000-01-01T00:00:00Z", expired: true };
    api.getProjectMemoryHistory.mockResolvedValue({ versions: [old], next_before_revision: null });
    api.restoreProjectMemory.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
    const user = userEvent.setup(); mount();
    await user.click(await screen.findByText("Version history"));
    await user.click(await screen.findByRole("button", { name: "Restore version 1" }));
    expect(screen.getByRole("textbox", { name: "Shared rules" })).toHaveAttribute("readonly");
    expect(screen.getByText(/original expiration is preserved/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Restore this version" }));
    expect(api.restoreProjectMemory).toHaveBeenCalledWith("project-1", 1, 3);
    expect(await screen.findByRole("alert")).toHaveTextContent("Someone changed this memory");
    expect(screen.getByRole("textbox", { name: "Shared rules" })).toHaveValue("Retired rule");
  });

  it("keeps a correction promotion draft on failure and confirms sourced publish", async () => {
    const user = userEvent.setup();
    api.updateProjectMemory.mockRejectedValueOnce(new Error("temporary"));
    api.updateProjectMemory.mockResolvedValueOnce({
      ...initial,
      revision: 4,
      rules: ["Keep the empty-state link working."],
      source_review: {
        review_id: "review-1",
        issue_id: "issue-1",
        task_id: "run-1",
        feedback: "Empty-state link broke.",
        criteria: ["Keep empty state"],
        assessments: [{ passed: false, evidence: "Link 404" }],
        reviewed_by: "user-1",
        reviewed_at: "2026-09-07T00:00:00Z",
      },
    });
    render(
      <TeachProjectFromReviewButton
        wsId="ws-1"
        projectId="project-1"
        review={{ id: "review-1", feedback: "Empty-state link broke." }}
      />,
      { wrapper },
    );
    await user.click(screen.getByRole("button", { name: "Promote to project memory" }));
    const input = screen.getByRole("textbox", { name: "Shared rules" });
    expect(input).toHaveValue("Empty-state link broke.");
    await user.clear(input);
    await user.type(input, "Keep the empty-state link working.");
    await user.click(screen.getByRole("button", { name: "Publish to project" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(input).toHaveValue("Keep the empty-state link working.");
    await user.click(screen.getByRole("button", { name: "Publish to project" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Project memory published from this correction");
    expect(api.updateProjectMemory).toHaveBeenLastCalledWith(
      "project-1",
      ["Keep the empty-state link working."],
      3,
      undefined,
      "review-1",
    );
  });

  it("shows prepared project-memory coverage without inventing zeros on failure", async () => {
    api.getProjectMemoryUsage.mockResolvedValue({
      ...emptyUsage,
      started_runs: 4,
      recorded_runs: 3,
      unrecorded_runs: 1,
      runs_with_project_memory: 2,
      versions: [{
        project_id: "project-1",
        revision: 3,
        prepared_runs: 2,
        last_started_at: "2026-09-04T12:00:00Z",
      }],
    });
    mount();
    expect(await screen.findByText(/4 runs started/)).toBeInTheDocument();
    expect(screen.getByText("With project memory")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByText(/Versions included: 1/));
    expect(screen.getByText(/Revision 3 · 2 runs/)).toBeInTheDocument();
  });
});
