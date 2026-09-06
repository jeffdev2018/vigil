// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MirrorLinkList } from "@multica/core/mirrors";
import { renderWithI18n } from "../../test/i18n";

// Tolerant parsing: packages/core/mirrors/schemas.test.ts.

const state = vi.hoisted(() => ({
  links: { links: [] } as MirrorLinkList,
  projects: [] as { id: string; title: string }[],
  create: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => state.projects }),
}));
vi.mock("@multica/core/mirrors", () => ({
  mirrorLinksOptions: () => ({ queryKey: ["mirror-links"], queryFn: async () => state.links }),
  useCreateMirrorLink: () => ({ mutate: state.create, isPending: false }),
  useDeleteMirrorLink: () => ({ mutate: state.remove, isPending: false }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { ProjectMirrorsSection } from "./project-mirrors-section";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectMirrorsSection projectId="p1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.links = { links: [] };
  state.projects = [
    { id: "p1", title: "This project" },
    { id: "p2", title: "Backend" },
    { id: "p3", title: "Infra" },
  ];
  state.create = vi.fn();
  state.remove = vi.fn();
});

describe("ProjectMirrorsSection", () => {
  it("says so when no link is configured", async () => {
    render();
    expect(await screen.findByText("No mirror links yet.")).toBeTruthy();
  });

  it("lists each link with its target project and trigger label", async () => {
    state.links = {
      links: [
        { id: "l1", source_project_id: "p1", target_project_id: "p2", target_project_title: "Backend", trigger_label: "mirror", created_at: "" },
        { id: "l2", source_project_id: "p1", target_project_id: "p9", target_project_title: "", trigger_label: "sync", created_at: "" },
      ],
    };
    render();
    const rows = await screen.findAllByTestId("project-mirror-link");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("Backend");
    expect(rows[0]?.textContent).toContain("mirror");
    // A link whose target project is gone still lists, named as deleted.
    expect(rows[1]?.textContent).toContain("Deleted project");
  });

  it("never offers this project as its own mirror target", async () => {
    render();
    // Wait for the project list query, not just for the empty <select> shell.
    await screen.findByRole("option", { name: "Backend" });
    const select = screen.getByLabelText("Target project");
    const values = Array.from(select.querySelectorAll("option")).map((o) => o.getAttribute("value"));
    expect(values).toEqual(["", "p2", "p3"]);
  });

  it("submits a trimmed label and only with both fields set", async () => {
    render();
    const add = await screen.findByRole("button", { name: "Add link" });
    expect((add as HTMLButtonElement).disabled).toBe(true);

    await screen.findByRole("option", { name: "Backend" });
    fireEvent.change(screen.getByLabelText("Target project"), { target: { value: "p2" } });
    fireEvent.change(screen.getByLabelText("Trigger label"), { target: { value: "  needs-mirror  " } });
    fireEvent.click(add);
    expect(state.create).toHaveBeenCalledWith(
      { targetProjectId: "p2", triggerLabel: "needs-mirror" },
      expect.anything(),
    );
  });

  it("removes a link by id", async () => {
    state.links = {
      links: [{ id: "l1", source_project_id: "p1", target_project_id: "p2", target_project_title: "Backend", trigger_label: "mirror", created_at: "" }],
    };
    render();
    fireEvent.click(await screen.findByLabelText("Remove this link"));
    expect(state.remove).toHaveBeenCalledWith("l1", expect.anything());
  });
});
