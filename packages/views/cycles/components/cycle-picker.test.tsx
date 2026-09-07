// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Cycle } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { PillButton } from "../../common/pill-button";

const cycle = (over: Partial<Cycle>): Cycle => ({
  id: "c", workspace_id: "ws-1", project_id: "p1", name: "Cycle", description: "",
  start_date: "2026-03-02", end_date: "2026-03-13", rollover: true, closed_at: null,
  status: "active", late: false, load_unit: "issues", load_property_id: null,
  issue_count: 0, done_count: 0,
  capacity: {
    human: { capacity: null, load: 0 },
    agent: { capacity: null, load: 0 },
    unassigned_load: 0,
  },
  created_at: "", updated_at: "", ...over,
});

const state = vi.hoisted(() => ({ enabledFor: [] as unknown[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { enabled?: boolean }) => {
    state.enabledFor.push(o.enabled);
    return {
      data: o.enabled
        ? [
            cycle({ id: "current", name: "Sprint 13" }),
            cycle({ id: "past", name: "Sprint 12", status: "closed" }),
          ]
        : [],
    };
  },
}));
vi.mock("@multica/core/cycles", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/cycles")>()),
  cycleListOptions: (_ws: string, projectId?: string) => ({ queryKey: ["cycles", projectId ?? "all"] }),
}));

import { CyclePicker } from "./cycle-picker";

beforeEach(() => {
  state.enabledFor = [];
});

describe("CyclePicker", () => {
  it("plans the issue into a cycle of its project", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderWithI18n(
      <CyclePicker cycleId={null} projectId="p1" onUpdate={onUpdate} triggerRender={<PillButton />} />,
    );
    await user.click(screen.getByRole("button", { name: /no cycle/i }));
    await user.click(await screen.findByRole("button", { name: /Sprint 13/ }));
    expect(onUpdate).toHaveBeenCalledWith({ cycle_id: "current" });
  });

  it("offers closed cycles too, so finished work can be filed where it belonged", async () => {
    const user = userEvent.setup();
    renderWithI18n(
      <CyclePicker cycleId={null} projectId="p1" onUpdate={vi.fn()} triggerRender={<PillButton />} />,
    );
    await user.click(screen.getByRole("button", { name: /no cycle/i }));
    expect(await screen.findByRole("button", { name: /Sprint 12/ })).toBeInTheDocument();
  });

  it("takes the issue out of every cycle", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderWithI18n(
      <CyclePicker cycleId="current" projectId="p1" onUpdate={onUpdate} triggerRender={<PillButton />} />,
    );
    await user.click(screen.getByRole("button", { name: /Sprint 13/ }));
    await user.click(await screen.findByRole("button", { name: /no cycle/i }));
    expect(onUpdate).toHaveBeenCalledWith({ cycle_id: null });
  });

  it("explains that an issue needs a project before it can have a cycle", async () => {
    // A cycle only accepts its own project's issues, so with no project there
    // is nothing to offer. Saying why beats an empty list that reads as loading.
    const user = userEvent.setup();
    renderWithI18n(
      <CyclePicker cycleId={null} projectId={null} onUpdate={vi.fn()} triggerRender={<PillButton />} />,
    );
    await user.click(screen.getByRole("button", { name: /no cycle/i }));
    expect(await screen.findByText(/project first/i)).toBeInTheDocument();
    // And it does not even ask the server for a list it could not scope.
    expect(state.enabledFor.every((enabled) => enabled === false)).toBe(true);
  });
});
