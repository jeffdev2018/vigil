// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  PackInstall,
  PackInstallDetail,
  PackPreview,
  PackSummary,
} from "@multica/core/packs";
import { renderWithI18n } from "../../test/i18n";

// Opens a Select's popup by its trigger accessible name (scoped to `within`
// when given) and clicks the option whose accessible name matches.
async function pickOption(
  user: ReturnType<typeof userEvent.setup>,
  scope: { getByRole: typeof screen.getByRole },
  triggerName: string,
  optionName: string | RegExp,
) {
  await user.click(scope.getByRole("combobox", { name: triggerName }));
  await user.click(await screen.findByRole("option", { name: optionName }));
}

// The pack contract parsing, the per-field drift repair and the count helper
// are covered canonically in packages/core/packs/schemas.test.ts. This suite
// keeps the happy path and the wiring: the catalogue grid, the domain filter,
// detail -> preview -> install with the chosen strategy, the uninstall
// confirmation, and the upload preview.

const state = vi.hoisted(() => ({
  catalogue: null as unknown,
  cataloguePending: false,
  catalogueError: false,
  detail: null as unknown,
  installs: [] as unknown[],
  installDetail: null as unknown,
  previewData: null as unknown,
  uploadPreviewData: null as unknown,
  uploadPreviewError: null as Error | null,
  uploadInstallError: null as Error | null,
  preview: vi.fn(),
  previewUpload: vi.fn(),
  install: vi.fn(),
  installUpload: vi.fn(),
  uninstall: vi.fn(),
  exportPack: vi.fn(),
  download: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const kind = options.queryKey[2];
    if (kind === "catalogue")
      return {
        data: state.catalogue,
        isPending: state.cataloguePending,
        isError: state.catalogueError,
        refetch: vi.fn(),
      };
    if (kind === "detail")
      return { data: state.detail, isPending: false, isError: false, refetch: vi.fn() };
    if (kind === "installs")
      return { data: state.installs, isPending: false, isError: false, refetch: vi.fn() };
    if (kind === "install")
      return { data: state.installDetail, isPending: false, isError: false, refetch: vi.fn() };
    return { data: undefined, isPending: false, isError: false, refetch: vi.fn() };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/packs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/packs")>()),
  packCatalogueOptions: (wsId: string) => ({ queryKey: ["packs", wsId, "catalogue"] }),
  packDetailOptions: (wsId: string, id: string) => ({
    queryKey: ["packs", wsId, "detail", id],
  }),
  packInstallsOptions: (wsId: string) => ({ queryKey: ["packs", wsId, "installs"] }),
  packInstallOptions: (wsId: string, id: string) => ({
    queryKey: ["packs", wsId, "install", id],
  }),
  usePreviewPack: () => ({
    mutate: state.preview,
    data: state.previewData,
    isPending: false,
    reset: vi.fn(),
  }),
  usePreviewPackUpload: () => ({
    mutate: state.previewUpload,
    data: state.uploadPreviewData,
    isPending: false,
    isError: state.uploadPreviewError !== null,
    error: state.uploadPreviewError,
    reset: vi.fn(),
  }),
  useInstallPack: () => ({
    mutate: state.install,
    data: undefined,
    isPending: false,
    isSuccess: false,
    reset: vi.fn(),
  }),
  useInstallPackUpload: () => ({
    mutate: state.installUpload,
    data: undefined,
    isPending: false,
    isSuccess: false,
    isError: state.uploadInstallError !== null,
    error: state.uploadInstallError,
    reset: vi.fn(),
  }),
  useUninstallPack: () => ({ mutate: state.uninstall, isPending: false }),
  useExportPack: () => ({ mutate: state.exportPack, isPending: false }),
  useDownloadPack: () => ({ mutate: state.download, isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const member = vi.hoisted(() => ({ role: "owner" as string | null }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: member.role, userId: "user-1", member: null, isLoading: false }),
}));

vi.mock("@multica/core/api", () => ({
  api: {},
  ApiError: class ApiError extends Error {},
}));

// Shiki-backed renderer; the tab only needs it to put the markdown on screen.
vi.mock("@multica/ui/markdown", () => ({
  Markdown: ({ children }: { children: string }) => <div data-testid="pack-markdown">{children}</div>,
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { PacksTab } from "./packs-tab";

const manifest = (over: Record<string, unknown> = {}) => ({
  id: "helpdesk-it",
  version: "1.2.0",
  title: "IT helpdesk",
  summary: "Take tickets, triage them, answer them.",
  description: "## What you get",
  domain: "helpdesk",
  wave: 1,
  author: "Multica",
  license: "MIT",
  tags: [],
  works_without_agents: true,
  metric: { label: "First response time", description: "Median hours", hint: "" },
  prerequisites: [],
  changelog: [{ version: "1.2.0", note: "Added the SLA view" }],
  ...over,
});

const pack = (over: Partial<PackSummary> = {}): PackSummary =>
  ({
    manifest: manifest(),
    counts: { labels: 5, views: 3, agents: 2 },
    builtin: true,
    installed_version: null,
    install_id: null,
    upgrade_available: false,
    prerequisites: [
      { kind: "native_runtime", name: "Native runtime", optional: false, note: "", status: "met" },
      { kind: "channel", name: "Email", optional: true, note: "Or Slack", status: "unknown" },
    ],
    ...over,
  }) as PackSummary;

const salesPack = pack({
  manifest: manifest({ id: "sales", title: "Sales pipeline", domain: "sales", wave: 2 }),
  counts: { labels: 2 },
});

const preview = (over: Partial<PackPreview> = {}): PackPreview =>
  ({
    pack: pack(),
    contents: { labels: ["Bug", "Question"] },
    collisions: [{ kind: "label", name: "Bug", existing_id: "label-1" }],
    problems: [],
    strategies: ["skip", "merge", "rename"],
    strategy: "skip",
    installed: null,
    blocked: "",
    ...over,
  }) as PackPreview;

const install = (over: Partial<PackInstall> = {}): PackInstall =>
  ({
    id: "install-1",
    pack_id: "helpdesk-it",
    pack_version: "1.1.0",
    title: "IT helpdesk",
    source: "builtin",
    strategy: "skip",
    status: "installed",
    run_id: null,
    report: {},
    manifest: {},
    installed_by: "user-1",
    installed_at: "2026-09-09T09:00:00Z",
    removed_at: null,
    item_count: 12,
    metric: { label: "First response time", description: "", hint: "" },
    domain: "helpdesk",
    upgrade_to: "1.2.0",
    bundle_sha256: "abc",
    ...over,
  }) as PackInstall;

beforeEach(() => {
  vi.clearAllMocks();
  member.role = "owner";
  state.catalogue = { packs: [pack(), salesPack], domains: ["helpdesk", "sales"] };
  state.cataloguePending = false;
  state.catalogueError = false;
  state.detail = { pack: pack(), contents: { labels: ["Bug"], views: ["Open tickets"] }, source: "pack:\n" };
  state.installs = [];
  state.installDetail = null;
  state.previewData = null;
  state.uploadPreviewData = null;
  state.uploadPreviewError = null;
  state.uploadInstallError = null;
});

describe("PacksTab catalogue", () => {
  it("renders a card per pack with its counts, domain and metric", () => {
    renderWithI18n(<PacksTab />);
    const card = screen.getByRole("button", { name: /IT helpdesk/ });
    expect(within(card).getByText("5 labels · 3 views · 2 agents")).toBeTruthy();
    expect(within(card).getByText("Helpdesk")).toBeTruthy();
    expect(within(card).getByText("Works without agents")).toBeTruthy();
    expect(within(card).getByText("Metric: First response time")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Sales pipeline/ })).toBeTruthy();
  });

  it("shows the update badge when a newer catalogue version is available", () => {
    state.catalogue = {
      packs: [pack({ installed_version: "1.1.0", install_id: "i1", upgrade_available: true })],
      domains: ["helpdesk"],
    };
    renderWithI18n(<PacksTab />);
    expect(screen.getByText("Update to 1.2.0")).toBeTruthy();
  });

  it("filters the grid by domain", () => {
    renderWithI18n(<PacksTab />);
    expect(screen.getByRole("button", { name: /Sales pipeline/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Helpdesk", pressed: false }));
    expect(screen.getByRole("button", { name: /IT helpdesk/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Sales pipeline/ })).toBeNull();
  });

  it("offers a retry when the catalogue fails to load", () => {
    state.catalogueError = true;
    state.catalogue = null;
    renderWithI18n(<PacksTab />);
    expect(screen.getByRole("alert").textContent).toContain("Failed to load the catalogue");
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("tells a plain member they cannot install", () => {
    member.role = "member";
    renderWithI18n(<PacksTab />);
    expect(screen.getByText(/Only owners and admins can install/)).toBeTruthy();
  });
});

describe("PacksTab detail and install", () => {
  function openDetail() {
    renderWithI18n(<PacksTab />);
    fireEvent.click(screen.getByRole("button", { name: /IT helpdesk/ }));
    return screen.getByRole("dialog");
  }

  it("shows the description, prerequisites with their status, contents and changelog", () => {
    const dialog = openDetail();
    expect(within(dialog).getByTestId("pack-markdown").textContent).toContain("What you get");
    expect(within(dialog).getByText("Met")).toBeTruthy();
    expect(within(dialog).getByText("Up to you")).toBeTruthy();
    expect(within(dialog).getByText("Or Slack")).toBeTruthy();
    expect(within(dialog).getByText(/Open tickets/)).toBeTruthy();
    expect(within(dialog).getByText(/Added the SLA view/)).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: /Download pack.yaml/ })).toBeTruthy();
  });

  it("previews the install for the pack that was opened", () => {
    const dialog = openDetail();
    fireEvent.click(within(dialog).getByRole("button", { name: "Preview the install" }));
    expect(state.preview).toHaveBeenCalledWith({ id: "helpdesk-it" }, expect.anything());
  });

  it("installs with the strategy the user picked", async () => {
    state.previewData = preview();
    const dialog = openDetail();
    expect(within(dialog).getByText(/1 name collisions/)).toBeTruthy();
    const user = userEvent.setup();
    await pickOption(user, within(dialog), "On collision", "Merge into what exists");
    fireEvent.click(within(dialog).getByRole("button", { name: "Install" }));
    expect(state.install).toHaveBeenCalledWith(
      { id: "helpdesk-it", strategy: "merge" },
      expect.anything(),
    );
  });

  it("defaults to the strategy the server picked when the user does not choose", () => {
    state.previewData = preview({ strategy: "merge" });
    state.detail = {
      pack: pack({ installed_version: "1.1.0", install_id: "i1", upgrade_available: true }),
      contents: {},
      source: "",
    };
    const dialog = openDetail();
    // An upgrade reads as Update, not Install, and carries the server's merge.
    fireEvent.click(within(dialog).getByRole("button", { name: "Update" }));
    expect(state.install).toHaveBeenCalledWith(
      { id: "helpdesk-it", strategy: "merge" },
      expect.anything(),
    );
  });

  it("disables the install and shows why when the server blocked it", () => {
    state.previewData = preview({ blocked: "version 1.2.0 is already installed" });
    const dialog = openDetail();
    expect(within(dialog).getByText("version 1.2.0 is already installed")).toBeTruthy();
    expect(
      within(dialog).getByRole("button", { name: "Install" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("blocks the install when the pack itself is not valid", () => {
    state.previewData = preview({ problems: ["issue status `done` is missing"] });
    const dialog = openDetail();
    expect(within(dialog).getByText("issue status `done` is missing")).toBeTruthy();
    expect(
      within(dialog).getByRole("button", { name: "Install" }).hasAttribute("disabled"),
    ).toBe(true);
  });
});

describe("PacksTab installed", () => {
  it("lists an install with its version, source and item count", () => {
    state.installs = [install()];
    renderWithI18n(<PacksTab />);
    expect(screen.getByText("1.1.0")).toBeTruthy();
    expect(screen.getByText("Built-in")).toBeTruthy();
    expect(screen.getByText("12 items")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Update to 1.2.0" })).toBeTruthy();
  });

  it("expands an install into its rows grouped by kind", () => {
    state.installs = [install()];
    state.installDetail = {
      install: install(),
      items: [
        { kind: "labels", name: "Bug", id: "l1", action: "created" },
        { kind: "labels", name: "Question", id: "l2", action: "created" },
        { kind: "views", name: "Open tickets", id: "v1", action: "created" },
      ],
    } satisfies PackInstallDetail;
    renderWithI18n(<PacksTab />);
    fireEvent.click(screen.getByRole("button", { name: "IT helpdesk", expanded: false }));
    const items = screen.getByTestId("pack-install-items");
    expect(items.textContent).toContain("Bug, Question");
    expect(items.textContent).toContain("Open tickets");
  });

  it("confirms before uninstalling, saying what goes and what stays", () => {
    state.installs = [install()];
    renderWithI18n(<PacksTab />);
    fireEvent.click(screen.getByRole("button", { name: /Uninstall/ }));
    expect(screen.getByText("Remove IT helpdesk?")).toBeTruthy();
    expect(screen.getByText(/projects, goals, notes and work items the pack brought are yours/)).toBeTruthy();
    expect(state.uninstall).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Remove the pack" }));
    expect(state.uninstall).toHaveBeenCalledWith({ id: "install-1" }, expect.anything());
  });

  // useUninstallPack rewrites the installs list from the uninstall response so
  // the row is correct the moment the mutation resolves, without waiting on
  // the refetch the invalidation triggers (canonical test:
  // packages/core/packs/mutations.test.tsx). What this asserts is the other
  // half: the row's badge and its actions read that list and nothing else, so
  // a removed install can never keep a live Uninstall button — the server
  // answers 409 to the second attempt.
  it("shows Removed and drops the button once the uninstall resolves", () => {
    state.installs = [install()];
    state.uninstall.mockImplementation(
      (_v: { id: string }, opts: { onSuccess: (r: unknown) => void }) => {
        const removed = install({ status: "removed", removed_at: "2026-09-10T10:00:00Z" });
        state.installs = [removed];
        opts.onSuccess({
          install: removed,
          report: { removed: { labels: 2 }, kept: [], reasons: [] },
        });
      },
    );
    const { rerender } = renderWithI18n(<PacksTab />);
    fireEvent.click(screen.getByRole("button", { name: /Uninstall/ }));
    fireEvent.click(screen.getByRole("button", { name: "Remove the pack" }));
    expect(screen.getByTestId("pack-uninstall-report").textContent).toContain("2 labels");
    // The list the mutation rewrote reaches the row through the query.
    rerender(<PacksTab />);
    expect(screen.queryByRole("button", { name: /Uninstall/ })).toBeNull();
    expect(screen.getAllByText("Removed").length).toBeGreaterThan(0);
  });

  it("hides the destructive actions from a plain member", () => {
    member.role = "member";
    state.installs = [install()];
    renderWithI18n(<PacksTab />);
    expect(screen.queryByRole("button", { name: /Uninstall/ })).toBeNull();
  });

  it("shows the empty state when nothing is installed", () => {
    renderWithI18n(<PacksTab />);
    expect(screen.getByText("No pack installed yet.")).toBeTruthy();
  });
});

describe("PacksTab upload", () => {
  const file = () => new File(["pack:\n  id: mine\n"], "mine.pack.yaml", { type: "text/yaml" });

  it("previews the chosen file server-side instead of parsing the YAML", () => {
    renderWithI18n(<PacksTab />);
    fireEvent.change(screen.getByLabelText("Pack file"), { target: { files: [file()] } });
    expect(state.previewUpload).toHaveBeenCalledWith(
      { file: expect.any(File) },
      expect.anything(),
    );
  });

  it("refuses a file over the server's 32 MB cap before the request", () => {
    const big = new File(["x"], "big.pack.yaml", { type: "text/yaml" });
    Object.defineProperty(big, "size", { value: 33 * 1024 * 1024 });
    renderWithI18n(<PacksTab />);
    fireEvent.change(screen.getByLabelText("Pack file"), { target: { files: [big] } });
    expect(state.previewUpload).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toContain("cannot exceed 32 MB");
  });

  it("sends a 20 MB file: the /api proxy carries it since proxyClientMaxBodySize", () => {
    const big = new File(["x"], "big.pack.yaml", { type: "text/yaml" });
    Object.defineProperty(big, "size", { value: 20 * 1024 * 1024 });
    renderWithI18n(<PacksTab />);
    fireEvent.change(screen.getByLabelText("Pack file"), { target: { files: [big] } });
    expect(state.previewUpload).toHaveBeenCalled();
  });

  it("keeps a failed preview readable under the drop zone, label before raw text", () => {
    state.uploadPreviewError = new Error("API error: 500 Internal Server Error");
    renderWithI18n(<PacksTab />);
    const panel = screen.getByTestId("pack-upload");
    const alert = within(panel).getByRole("alert");
    expect(alert.textContent).toContain("Failed to preview the pack");
    expect(alert.textContent).toContain("API error: 500 Internal Server Error");
    expect(alert.textContent?.indexOf("Failed to preview the pack")).toBeLessThan(
      alert.textContent?.indexOf("API error") ?? -1,
    );
  });

  it("installs the uploaded file with the picked strategy", async () => {
    state.uploadPreviewData = preview({ collisions: [] });
    renderWithI18n(<PacksTab />);
    const panel = screen.getByTestId("pack-upload");
    fireEvent.change(screen.getByLabelText("Pack file"), { target: { files: [file()] } });
    const user = userEvent.setup();
    await pickOption(user, within(panel), "On collision", "Rename what the pack brings");
    fireEvent.click(within(panel).getByRole("button", { name: "Install" }));
    expect(state.installUpload).toHaveBeenCalledWith(
      { file: expect.any(File), strategy: "rename" },
      expect.anything(),
    );
  });
});

describe("PacksTab export", () => {
  it("keeps export disabled until the manifest fields the server requires are filled", () => {
    renderWithI18n(<PacksTab />);
    const panel = screen.getByTestId("pack-export");
    const button = screen.getByRole("button", { name: /Export the pack/ });
    expect(button.hasAttribute("disabled")).toBe(true);
    fireEvent.change(within(panel).getByLabelText("Identifier (kebab-case)"), {
      target: { value: "my-desk" },
    });
    fireEvent.change(within(panel).getByLabelText("Title"), { target: { value: "My desk" } });
    fireEvent.change(within(panel).getByLabelText("Summary"), { target: { value: "Ours." } });
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(within(panel).getByRole("checkbox", { name: "Include notes" }));
    fireEvent.click(button);
    expect(state.exportPack).toHaveBeenCalledWith(
      {
        manifest: {
          id: "my-desk",
          version: "1.0.0",
          title: "My desk",
          summary: "Ours.",
          domain: "helpdesk",
          metric: { label: "", description: "" },
        },
        include_issues: false,
        include_notes: true,
        include_skills: true,
      },
      expect.anything(),
    );
  });

  it("lets the export drop the procedures and says machine-local ones never travel", () => {
    renderWithI18n(<PacksTab />);
    const panel = screen.getByTestId("pack-export");
    expect(
      within(panel).getByText(/discovered on a connected computer are never exported/),
    ).toBeTruthy();
    fireEvent.change(within(panel).getByLabelText("Identifier (kebab-case)"), {
      target: { value: "my-desk" },
    });
    fireEvent.change(within(panel).getByLabelText("Title"), { target: { value: "My desk" } });
    fireEvent.change(within(panel).getByLabelText("Summary"), { target: { value: "Ours." } });
    fireEvent.click(within(panel).getByRole("checkbox", { name: "Include procedures (skills)" }));
    fireEvent.click(screen.getByRole("button", { name: /Export the pack/ }));
    expect(state.exportPack).toHaveBeenCalledWith(
      expect.objectContaining({ include_skills: false }),
      expect.anything(),
    );
  });
});
