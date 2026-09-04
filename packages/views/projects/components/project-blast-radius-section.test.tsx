// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { BlastRadiusRule } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/projects/blast-radius.test.ts.

const state = vi.hoisted(() => ({ rules: [] as BlastRadiusRule[], created: [] as unknown[], removed: [] as string[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@multica/core/projects/blast-radius", () => ({
  blastRadiusRulesOptions: () => ({ queryKey: ["rules"], queryFn: async () => ({ rules: state.rules, levels: ["autonomous", "read_only", "dual_approval"] }) }),
  blastRadiusPreviewOptions: (_wsId: string, _projectId: string, path: string) => ({
    queryKey: ["preview", path],
    queryFn: async () => (path.startsWith("server/migrations") ? { path, level: "read_only", path_pattern: "server/migrations/**" } : { path, level: "inherit" }),
    enabled: path.trim().length > 0,
  }),
  useCreateBlastRadiusRule: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.created.push(v); o.onSuccess(); } }),
  useDeleteBlastRadiusRule: () => ({ mutate: (id: string) => state.removed.push(id) }),
}));

import { ProjectBlastRadiusSection } from "./project-blast-radius-section";

const rule = (over: Partial<BlastRadiusRule> = {}): BlastRadiusRule => ({ id: "r1", project_id: "p1", path_pattern: "server/migrations/**", autonomy_level: "read_only", specificity: 18, created_by: "u", created_at: "", ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectBlastRadiusSection projectId="p1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.rules = [];
  state.created = [];
  state.removed = [];
});

describe("ProjectBlastRadiusSection", () => {
  it("says no rule means inherited permissions and adds a rule", async () => {
    render();
    expect(await screen.findByTestId("blast-radius-empty")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Path pattern"), { target: { value: "apps/mobile/**" } });
    fireEvent.change(screen.getByLabelText("Autonomy"), { target: { value: "autonomous" } });
    fireEvent.click(screen.getByRole("button", { name: "Add rule" }));
    expect(state.created[0]).toEqual({ path_pattern: "apps/mobile/**", autonomy_level: "autonomous" });
  });

  it("lists rules in resolution order, previews a path and removes a rule", async () => {
    state.rules = [rule(), rule({ id: "r2", path_pattern: "**", autonomy_level: "dual_approval", specificity: 0 })];
    render();
    const rows = await screen.findAllByTestId("blast-radius-rule");
    expect(rows[0]?.textContent).toContain("server/migrations/**");
    expect(rows[0]?.getAttribute("data-level")).toBe("read_only");
    fireEvent.change(screen.getByLabelText("Try a path"), { target: { value: "server/migrations/500.sql" } });
    expect((await screen.findByTestId("blast-radius-preview")).textContent).toContain("Read only");
    fireEvent.click(screen.getAllByRole("button", { name: "Remove rule" })[1]!);
    expect(state.removed).toEqual(["r2"]);
  });
});
