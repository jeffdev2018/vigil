// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SkillSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import type { SkillRow } from "./skills-page";
import type { SkillActionsContext } from "./skill-list-actions";

const TEST_RESOURCES = { en: { common: enCommon, skills: enSkills } };

vi.mock("@multica/core/api", () => ({
  api: {
    refreshSkill: vi.fn(),
    deleteSkill: vi.fn(),
    addAgentSkills: vi.fn(),
    getBaseUrl: () => "https://api.example.com",
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { AddToAgentDialog, DeleteSkillsDialog, UpdateSkillsDialog } from "./skill-list-actions";

const refreshSkill = vi.mocked(api.refreshSkill);
const deleteSkill = vi.mocked(api.deleteSkill);
const addAgentSkills = vi.mocked(api.addAgentSkills);

function makeRow(id: string): SkillRow {
  const skill: SkillSummary = {
    id,
    workspace_id: "ws-1",
    name: `skill-${id}`,
    description: "",
    config: {
      origin: { type: "github", source_url: `https://github.com/acme/${id}` },
    },
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
  return {
    skill,
    agents: [],
    creator: null,
    runtime: null,
    originType: "github",
    canEdit: true,
  };
}

const ctx: SkillActionsContext = {
  wsId: "ws-1",
  agents: [],
  currentUserId: "user-1",
  isAdmin: true,
};

function renderDialog(
  rows: SkillRow[],
  skippedCount = 0,
  onUpdated?: () => void,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <UpdateSkillsDialog
          rows={rows}
          skippedCount={skippedCount}
          ctx={ctx}
          open
          onOpenChange={() => {}}
          onUpdated={onUpdated}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("UpdateSkillsDialog", () => {
  it("summarizes the updatable selection and the skipped entries", async () => {
    renderDialog([makeRow("a"), makeRow("b")], 1);

    expect(
      await screen.findByText("2 skills will be updated from their sources"),
    ).toBeTruthy();
    // The never-updatable selection entries passed via skippedCount.
    expect(
      screen.getByText("1 can't be updated from a source — skipped"),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /Update 2/ })).toBeTruthy();
  });

  it("omits the skipped line when the whole selection is updatable", async () => {
    renderDialog([makeRow("a")]);

    expect(
      await screen.findByText("1 skill will be updated from its source"),
    ).toBeTruthy();
    expect(screen.queryByText(/skipped/)).toBeNull();
  });

  it("refreshes every updatable skill and clears the selection on success", async () => {
    refreshSkill.mockResolvedValue({} as never);
    const onUpdated = vi.fn();
    renderDialog([makeRow("a"), makeRow("b")], 0, onUpdated);

    await userEvent.click(
      await screen.findByRole("button", { name: /Update 2/ }),
    );

    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
    expect(refreshSkill.mock.calls.map(([id]) => id)).toEqual(["a", "b"]);
    expect(toast.success).toHaveBeenCalled();
  });

  it("continues past a failing item and reports a partial toast", async () => {
    refreshSkill.mockImplementation((id: string) =>
      id === "a"
        ? Promise.reject(new Error("name conflict"))
        : Promise.resolve({} as never),
    );
    const onUpdated = vi.fn();
    renderDialog([makeRow("a"), makeRow("b")], 0, onUpdated);

    await userEvent.click(
      await screen.findByRole("button", { name: /Update 2/ }),
    );

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    // The failure on "a" must not strand "b".
    expect(refreshSkill.mock.calls.map(([id]) => id)).toEqual(["a", "b"]);
    // Partial success keeps the selection: onUpdated only fires on a clean run.
    expect(onUpdated).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// DeleteSkillsDialog — batch delete used to be a `for...of` loop that threw
// out of the whole handler on the first rejection: the dialog stayed open,
// no cache invalidation ran, and there was no indication which row failed.
// ---------------------------------------------------------------------------

function renderDeleteDialog(
  rows: SkillRow[],
  onOpenChange: (open: boolean) => void,
  onDeleted?: () => void,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <DeleteSkillsDialog
          rows={rows}
          ctx={ctx}
          open
          onOpenChange={onOpenChange}
          onDeleted={onDeleted}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("DeleteSkillsDialog", () => {
  it("deletes every row and closes on a clean run", async () => {
    deleteSkill.mockResolvedValue({} as never);
    const onOpenChange = vi.fn();
    const onDeleted = vi.fn();
    renderDeleteDialog([makeRow("a"), makeRow("b")], onOpenChange, onDeleted);

    await userEvent.click(screen.getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(onDeleted).toHaveBeenCalled());
    expect(deleteSkill.mock.calls.map(([id]) => id).sort()).toEqual(["a", "b"]);
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(toast.success).toHaveBeenCalled();
  });

  it("keeps the dialog open and reports a partial toast when one row fails", async () => {
    deleteSkill.mockImplementation((id: string) =>
      id === "a"
        ? Promise.reject(new Error("in use"))
        : Promise.resolve({} as never),
    );
    const onOpenChange = vi.fn();
    const onDeleted = vi.fn();
    renderDeleteDialog([makeRow("a"), makeRow("b")], onOpenChange, onDeleted);

    await userEvent.click(screen.getByRole("button", { name: /Delete/ }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Deleted 1, 1 failed"));
    // "b" still deleted even though "a" failed.
    expect(deleteSkill.mock.calls.map(([id]) => id).sort()).toEqual(["a", "b"]);
    expect(onDeleted).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });
});

// ---------------------------------------------------------------------------
// AddToAgentDialog — same defect: a rejection on agent #2 exited the handler
// before the cache invalidation for agent #1's already-persisted mutation.
// ---------------------------------------------------------------------------

function makeAgent(id: string, overrides: Partial<Agent> = {}): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: `Agent ${id}`,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    max_concurrent_tasks: 1,
    owner_id: "user-1",
    archived_at: null,
    custom_args: [],
    visibility: "private",
    permission_mode: "private",
    invocation_targets: [],
    model: "claude",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    status: "idle",
    skills: [],
    archived_by: null,
    ...overrides,
  } as Agent;
}

const skill: SkillSummary = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "Skill One",
  description: "",
  config: { origin: { type: "manual" } },
  created_by: "user-1",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function renderAddDialog(agents: Agent[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <AddToAgentDialog
          skills={[skill]}
          ctx={{ ...ctx, agents }}
          open
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AddToAgentDialog", () => {
  it("adds the skill to every selected agent and invalidates the cache when one fails", async () => {
    addAgentSkills.mockImplementation((id: string) =>
      id === "b"
        ? Promise.reject(new Error("permission denied"))
        : Promise.resolve({} as never),
    );
    renderAddDialog([makeAgent("a"), makeAgent("b")]);

    await userEvent.click(await screen.findByRole("button", { name: /Agent a/ }));
    await userEvent.click(screen.getByRole("button", { name: /Agent b/ }));
    await userEvent.click(screen.getByRole("button", { name: /Add \(2\)/ }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Added to 1, 1 failed"));
    // Agent "a" was attempted even though "b" failed — no early exit.
    expect(addAgentSkills.mock.calls.map(([id]) => id).sort()).toEqual(["a", "b"]);
  });
});
