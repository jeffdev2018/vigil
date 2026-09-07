// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// The gate derivation, the rail-state mapping and the ticket parsing matrices
// are canonically in packages/core/projects/epic.test.ts. This suite keeps the
// rendering, the wiring and the named regressions.

type Artifact = {
  id: string;
  kind: string;
  version: number;
  content: string;
  payload: unknown;
  state: string;
};

const artifact = (over: Partial<Artifact> = {}): Artifact => ({
  id: "a1",
  kind: "prd",
  version: 1,
  content: "The problem statement.",
  payload: {},
  state: "draft",
  ...over,
});

const state = vi.hoisted(() => ({
  epic: { steps: {}, epic_issue_id: "", next_kind: "prd" } as Record<string, unknown>,
  resources: [] as { resource_type: string }[],
  generate: vi.fn(),
  save: vi.fn(),
  approve: vi.fn(),
  apply: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({ queryKey: ["resources"], queryFn: async () => state.resources }),
}));
vi.mock("@multica/core/projects/epic", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/projects/epic")>(
    "@multica/core/projects/epic",
  );
  return {
    ...actual,
    projectEpicOptions: () => ({ queryKey: ["epic"], queryFn: async () => state.epic }),
    useGenerateEpicStep: () => ({ mutate: state.generate, isPending: false, variables: undefined }),
    useSaveEpicStep: () => ({ mutate: state.save, isPending: false, variables: undefined }),
    useApproveEpicStep: () => ({ mutate: state.approve, isPending: false, variables: undefined }),
    useApplyEpicTickets: () => ({ mutate: state.apply, isPending: false }),
  };
});

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { EpicPanel } from "./epic-panel";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <EpicPanel projectId="p1" />
    </QueryClientProvider>,
  );
}

const step = (over: Record<string, unknown> = {}) => ({
  kind: "prd",
  latest: null,
  approved: null,
  generating: false,
  ...over,
});

beforeEach(() => {
  state.epic = { steps: {}, epic_issue_id: "", next_kind: "prd" };
  state.resources = [{ resource_type: "github_repo" }];
  state.generate = vi.fn();
  state.save = vi.fn();
  state.approve = vi.fn();
  state.apply = vi.fn();
  toast.success.mockClear();
  toast.error.mockClear();
});

describe("EpicPanel rail", () => {
  it("renders the four steps of the pipeline", async () => {
    render();
    await screen.findByTestId("epic-step-prd");
    for (const kind of ["prd", "tech_plan", "wireframe", "tickets"]) {
      expect(screen.getByTestId(`epic-step-${kind}`)).toBeTruthy();
    }
  });

  it("shows each step's state on the rail", async () => {
    state.epic = {
      steps: {
        prd: step({ latest: artifact({ state: "approved" }), approved: artifact({ state: "approved" }) }),
        tech_plan: step({ kind: "tech_plan", generating: true }),
      },
      epic_issue_id: "i1",
      next_kind: "tech_plan",
    };
    render();
    await screen.findByTestId("epic-content-prd");
    expect(screen.getByTestId("epic-step-prd").getAttribute("data-state")).toBe("approved");
    expect(screen.getByTestId("epic-step-tech_plan").getAttribute("data-state")).toBe("generating");
    // A step nobody has touched yet reads as empty, not as broken.
    expect(screen.getByTestId("epic-step-wireframe").getAttribute("data-state")).toBe("empty");
    expect(screen.getByTestId("epic-content-prd").textContent).toContain("The problem statement.");
  });

  it("says which approval a locked step is waiting on, not just that it is disabled", async () => {
    render();
    const locked = await screen.findByTestId("epic-locked-tech_plan");
    expect(locked.textContent).toContain("PRD");
    // A locked step offers no action at all — the explanation is the whole row.
    expect(screen.queryByTestId("epic-content-tech_plan")).toBeNull();
  });

  it("unlocks a step once its predecessor is approved", async () => {
    state.epic = {
      steps: { prd: step({ latest: artifact({ state: "approved" }), approved: artifact({ state: "approved" }) }) },
      epic_issue_id: "i1",
      next_kind: "tech_plan",
    };
    render();
    await screen.findByTestId("epic-content-prd");
    expect(screen.queryByTestId("epic-locked-tech_plan")).toBeNull();
    // The step after the unlocked one is still waiting on it.
    expect(screen.getByTestId("epic-locked-wireframe").textContent).toContain("Technical plan");
  });

  it("renders a state the server introduced without breaking the rail", async () => {
    // Named regression for acceptance 7: an unknown state must leave the panel
    // usable, not blank and not crashed.
    state.epic = {
      steps: { prd: step({ latest: artifact({ state: "archived" }) }) },
      epic_issue_id: "i1",
      next_kind: "prd",
    };
    render();
    await screen.findByTestId("epic-content-prd");
    expect(screen.getByTestId("epic-step-prd").getAttribute("data-state")).toBe("unknown");
    expect(screen.getByTestId("epic-content-prd").textContent).toContain("The problem statement.");
    // Approve is offered only for a draft, so an unrecognised state hides it.
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });
});

describe("EpicPanel actions", () => {
  it("generates a step", async () => {
    render();
    await screen.findByTestId("epic-step-prd");
    fireEvent.click(screen.getAllByRole("button", { name: /Generate/ })[0]!);
    expect(state.generate).toHaveBeenCalledWith({ kind: "prd" }, expect.anything());
  });

  it("edits a step into a new draft", async () => {
    state.epic = { steps: { prd: step({ latest: artifact() }) }, epic_issue_id: "i1", next_kind: "prd" };
    render();
    await screen.findByTestId("epic-content-prd");
    fireEvent.click(screen.getAllByRole("button", { name: "Edit" })[0]!);
    const box = screen.getByLabelText("Step content") as HTMLTextAreaElement;
    expect(box.value).toBe("The problem statement.");
    fireEvent.change(box, { target: { value: "# Rewritten" } });
    fireEvent.click(screen.getByRole("button", { name: "Save as a new draft" }));
    expect(state.save).toHaveBeenCalledWith(
      { kind: "prd", content: "# Rewritten", payload: {} },
      expect.anything(),
    );
  });

  it("approves the draft of a step", async () => {
    state.epic = { steps: { prd: step({ latest: artifact() }) }, epic_issue_id: "i1", next_kind: "prd" };
    render();
    await screen.findByTestId("epic-content-prd");
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(state.approve).toHaveBeenCalledWith("prd", expect.anything());
  });

  it("warns when the project has no repository, without blocking anything", async () => {
    state.resources = [];
    render();
    expect(await screen.findByTestId("epic-no-repo-warning")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: /Generate/ })[0]!.hasAttribute("disabled")).toBe(false);
  });
});

describe("EpicPanel ticket preview", () => {
  const approvedPipeline = (payload: unknown) => ({
    steps: {
      prd: step({ latest: artifact({ state: "approved" }), approved: artifact({ state: "approved" }) }),
      tech_plan: step({ kind: "tech_plan", latest: artifact({ kind: "tech_plan", state: "approved" }), approved: artifact({ kind: "tech_plan", state: "approved" }) }),
      wireframe: step({ kind: "wireframe", latest: artifact({ kind: "wireframe", state: "approved" }), approved: artifact({ kind: "wireframe", state: "approved" }) }),
      tickets: step({
        kind: "tickets",
        latest: artifact({ kind: "tickets", state: "approved", payload }),
        approved: artifact({ kind: "tickets", state: "approved", payload }),
      }),
    },
    epic_issue_id: "i1",
    next_kind: "",
  });

  it("previews what apply would create before applying it", async () => {
    state.epic = approvedPipeline({
      tickets: [
        { external_key: "T1", title: "Add the endpoint", description: "Wire the route.", depends_on: [] },
        { external_key: "T2", title: "Test it", description: "Cover the gate.", depends_on: ["T1"] },
      ],
    });
    render();
    await screen.findByTestId("epic-ticket-preview");
    const rows = screen.getAllByTestId("epic-ticket-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]!.textContent).toContain("Add the endpoint");
    expect(rows[1]!.textContent).toContain("T1");

    fireEvent.click(screen.getByRole("button", { name: "Apply the tickets" }));
    expect(state.apply).toHaveBeenCalled();
  });

  it("previews only the tickets a replay would still create", async () => {
    state.epic = approvedPipeline({
      tickets: [
        { external_key: "T1", title: "Add the endpoint", description: "", depends_on: [] },
        { external_key: "T2", title: "Test it", description: "", depends_on: ["T1"] },
      ],
      applied: { T1: "issue-1" },
    });
    render();
    await screen.findByTestId("epic-ticket-preview");
    const rows = screen.getAllByTestId("epic-ticket-row");
    expect(rows).toHaveLength(1);
    expect(rows[0]!.textContent).toContain("Test it");
  });

  it("says so when a replay would create nothing", async () => {
    state.epic = approvedPipeline({
      tickets: [{ external_key: "T1", title: "Add the endpoint", description: "", depends_on: [] }],
      applied: { T1: "issue-1" },
    });
    render();
    expect(await screen.findByTestId("epic-tickets-all-applied")).toBeTruthy();
    expect(screen.queryByTestId("epic-ticket-preview")).toBeNull();
  });
});
