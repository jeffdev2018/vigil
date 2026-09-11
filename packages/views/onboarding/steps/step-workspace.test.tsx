import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import enWorkspace from "../../locales/en/workspace.json";
// The pack picker's rows read the kind and domain glossary from the settings
// namespace, which owns the Packs tab — one glossary for the product.
import enSettings from "../../locales/en/settings.json";
import type { Workspace } from "@multica/core/types";

const TEST_RESOURCES = {
  en: {
    common: enCommon,
    onboarding: enOnboarding,
    workspace: enWorkspace,
    settings: enSettings,
  },
};

type MockConfigState = {
  workspaceCreationDisabled: boolean;
  daemonAppUrl: string;
};

const mockLogout = vi.hoisted(() => vi.fn());
const mockUseConfigStore = vi.hoisted(() =>
  vi.fn((selector: (state: MockConfigState) => unknown) =>
    selector({ workspaceCreationDisabled: false, daemonAppUrl: "" }),
  ),
);

vi.mock("../../auth", () => ({
  useLogout: () => mockLogout,
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: MockConfigState) => unknown) =>
    mockUseConfigStore(selector),
}));

const mockCreateMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutate: mockCreateMutate, isPending: false }),
}));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "http://127.0.0.1:8080" },
}));

const mockTemplates = vi.hoisted(() => ({ list: [] as { id: string; name: string; workspace_name: string }[] }));
vi.mock("@multica/core/workspace/transfer", () => ({
  workspaceTemplatesOptions: () => ({ queryKey: ["workspace-templates"], queryFn: async () => mockTemplates.list }),
}));

const mockPacks = vi.hoisted(() => ({
  catalogue: { packs: [] as unknown[], domains: [] as string[] },
}));
vi.mock("@multica/core/packs", () => ({
  packSeedCatalogueOptions: () => ({
    queryKey: ["pack-catalogue"],
    queryFn: async () => mockPacks.catalogue,
  }),
}));

import { StepWorkspace } from "./step-workspace";

function I18nWrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function renderStep({
  existing,
  disabled,
  daemonAppUrl = "",
}: {
  existing: Workspace | null;
  disabled: boolean;
  daemonAppUrl?: string;
}) {
  mockUseConfigStore.mockImplementation(
    (selector: (state: MockConfigState) => unknown) =>
      selector({ workspaceCreationDisabled: disabled, daemonAppUrl }),
  );
  return render(
    <StepWorkspace existing={existing} onCreated={vi.fn()} />,
    { wrapper: I18nWrapper },
  );
}

const EXISTING_WORKSPACE: Workspace = {
  id: "00000000-0000-0000-0000-000000000001",
  name: "Acme",
  slug: "acme",
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "ACM",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
} as unknown as Workspace;

// Regression for #3433 (PR feedback): when DISABLE_WORKSPACE_CREATION is on,
// every onboarding entry point must steer the user toward an existing
// workspace or a logout escape — never toward the create form, even
// indirectly (stale CTA copy, "or start another" prose, etc.).
describe("StepWorkspace — DISABLE_WORKSPACE_CREATION gate", () => {
  it("renders the create form when the flag is off and the user has no workspace", () => {
    renderStep({ existing: null, disabled: false });

    expect(
      screen.getByText("Name your workspace.", { exact: false }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Workspace name")).toBeInTheDocument();
    expect(screen.getByLabelText("URL")).toBeInTheDocument();
  });

  it("hides the create form and shows the disabled notice when the flag is on and there is no workspace", () => {
    renderStep({ existing: null, disabled: true });

    expect(
      screen.getByText("Ask your administrator for an invitation.", {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Workspace name")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("URL")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /log out/i })).toBeInTheDocument();
  });

  it("forces the existing-workspace-only state when the flag is on and the user already has a workspace", () => {
    renderStep({ existing: EXISTING_WORKSPACE, disabled: true });

    // Disabled-specific copy is used in place of the "or start another" prose.
    expect(
      screen.getByText("Continue with Acme.", { exact: false }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/start another/i),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/create a new one alongside it/i),
    ).not.toBeInTheDocument();

    // Resume picker still shows the existing workspace card (its name
    // appears in both the avatar and the card label — at least one is
    // enough to know the card is rendered), but the "Create a new
    // workspace" radio card is gone entirely.
    expect(screen.getAllByText("Acme").length).toBeGreaterThan(0);
    expect(
      screen.queryByText("Create a new workspace", { exact: false }),
    ).not.toBeInTheDocument();

    // CTA is pre-selected to the existing-only action and immediately
    // enabled, so the user can press it without further interaction.
    const cta = screen.getByRole("button", { name: "Open Acme" });
    expect(cta).toBeEnabled();
  });
});

// #4263: the workspace URL prefix must reflect the deployment's own host on
// self-hosted instances instead of the hardcoded `multica.ai`.
describe("StepWorkspace — workspace URL prefix", () => {
  it("shows the brand host when no app URL is configured", () => {
    renderStep({ existing: null, disabled: false });
    expect(screen.getByText("multica.ai/")).toBeInTheDocument();
  });

  it("shows the deployment host for self-hosted instances", () => {
    renderStep({
      existing: null,
      disabled: false,
      daemonAppUrl: "https://multica.example.com",
    });
    expect(screen.getByText("multica.example.com/")).toBeInTheDocument();
    expect(screen.queryByText("multica.ai/")).not.toBeInTheDocument();
  });
});

describe("StepWorkspace — random workspace identity", () => {
  it("fills the name and a suffixed URL from the celestial list", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.click(screen.getByRole("button", { name: "Random" }));

    const name = screen.getByLabelText("Workspace name") as HTMLInputElement;
    const slug = screen.getByLabelText("URL") as HTMLInputElement;
    const expectedSlugPrefix = name.value
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "");

    expect(name.value).not.toBe("");
    expect(slug.value).toMatch(
      new RegExp(`^${expectedSlugPrefix}-[a-z0-9]{4}$`),
    );
  });
});

// MUL-6050: the issue prefix used to be a read-only preview derived
// server-side from the workspace NAME, so every workspace named in Chinese
// (or Japanese, Korean, emoji…) was created as "WS" with no way to change it
// in the create flow. It now derives from the slug — which the same form
// already forces the user to pick in ASCII — and is editable here.
describe("StepWorkspace — issue prefix", () => {
  const prefixInput = () =>
    screen.getByLabelText("Issue prefix") as HTMLInputElement;

  it("derives the prefix from the slug, not the name", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });

    // Slug auto-filled to "acme-inc" → first 4 alphanumerics, uppercased.
    expect(prefixInput().value).toBe("ACME");
    expect(screen.getByText("ACME-123")).toBeInTheDocument();
  });

  // A Chinese name fills the whole form on its own: the URL romanizes from
  // the name, and the prefix follows the URL like any other name would.
  it("fills the URL and prefix from a Chinese name", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "蜘蛛侠" },
    });

    expect(screen.getByLabelText("URL")).toHaveValue("zhizhuxia");
    expect(prefixInput()).toHaveValue("ZHIZ");
    expect(screen.getByText("ZHIZ-123")).toBeInTheDocument();
    expect(screen.queryByText(/WS/)).not.toBeInTheDocument();
  });

  // Romanization only covers Han, so kana / Hangul / emoji names still reach
  // the empty state. Nothing may advertise a prefix there — "WS" in
  // particular is the string this issue exists to remove.
  it("shows no prefix at all when the name romanizes to nothing", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "スパイダーマン" },
    });

    expect(screen.getByLabelText("URL")).toHaveValue("");
    expect(prefixInput()).toHaveValue("");
    expect(prefixInput().placeholder).toBe("");
    expect(screen.queryByText(/WS/)).not.toBeInTheDocument();
    expect(screen.queryByText(/-123/)).not.toBeInTheDocument();
    // The hint takes the example line's place so the field isn't a bare box.
    expect(
      screen.getByText("Set the URL above and issue numbers will follow it", {
        exact: false,
      }),
    ).toBeInTheDocument();

    // …and the moment a URL exists, the prefix follows it.
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "spider" },
    });
    expect(prefixInput()).toHaveValue("SPID");
    expect(screen.getByText("SPID-123")).toBeInTheDocument();
  });

  it("follows a hand-typed URL over the romanized one", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "前端团队" },
    });
    expect(prefixInput().value).toBe("QIAN");

    // Overriding the URL re-derives the prefix from what the user chose.
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "frontend" },
    });

    expect(prefixInput().value).toBe("FRON");
    expect(prefixInput().value).not.toBe("WS");
  });

  it("stops following the slug once the user edits it, and normalizes input", () => {
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });
    fireEvent.change(prefixInput(), { target: { value: "fe-team!" } });

    // Uppercased, non-alphanumerics dropped — matching the server's
    // `^[A-Z0-9]{1,10}$` rule and the settings tab's guardrail.
    expect(prefixInput().value).toBe("FETEAM");

    // A later slug edit must not clobber the user's choice.
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "acme-corp" },
    });
    expect(prefixInput().value).toBe("FETEAM");
    expect(screen.getByText("FETEAM-123")).toBeInTheDocument();
  });

  it("submits the prefix the user was shown", () => {
    mockCreateMutate.mockClear();
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "前端团队" },
    });
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "frontend" },
    });
    fireEvent.change(prefixInput(), { target: { value: "fe" } });
    fireEvent.click(screen.getByRole("button", { name: /^Create 前端团队$/ }));

    expect(mockCreateMutate).toHaveBeenCalledTimes(1);
    expect(mockCreateMutate.mock.calls[0]![0]).toEqual({
      name: "前端团队",
      slug: "frontend",
      issue_prefix: "FE",
    });
  });

  it("falls back to the slug-derived default when the field is cleared", () => {
    mockCreateMutate.mockClear();
    renderStep({ existing: null, disabled: false });

    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });
    fireEvent.change(prefixInput(), { target: { value: "" } });

    // Empty input doesn't block the CTA: the placeholder already shows the
    // default that will be used, so submitting an empty field can't surprise.
    expect(prefixInput().placeholder).toBe("ACME");
    fireEvent.click(screen.getByRole("button", { name: /^Create Acme Inc$/ }));

    expect(mockCreateMutate.mock.calls[0]![0]).toMatchObject({
      issue_prefix: "ACME",
    });
  });

  it("lists saved templates and sends the picked one as template_run_id", async () => {
    mockCreateMutate.mockClear();
    mockTemplates.list = [{ id: "run-1", name: "Agency starter", workspace_name: "Agency" }];
    renderStep({ existing: null, disabled: false });

    const picker = await screen.findByRole("combobox", { name: "Start from" });
    expect(picker.textContent).toContain("Start from scratch");
    const user = userEvent.setup();
    await user.click(picker);
    await user.click(await screen.findByRole("option", { name: "Agency starter" }));
    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Create Acme Inc$/ }));

    expect(mockCreateMutate.mock.calls[0]![0]).toMatchObject({
      slug: "acme-inc",
      template_run_id: "run-1",
    });
    mockTemplates.list = [];
  });
});

// Packs (OS plan, vague B): the new workspace can start as a ready-to-use
// setup for one function. The catalogue read here is the pre-workspace one
// (/api/pack-catalogue) — no workspace header, no install state — because
// the workspace being seeded does not exist yet.
describe("StepWorkspace — pack picker", () => {
  const HELPDESK = {
    manifest: {
      id: "helpdesk-it",
      title: "IT helpdesk",
      summary: "Take tickets, triage them, answer them.",
      domain: "helpdesk",
      works_without_agents: true,
    },
    counts: { labels: 5, views: 3, agents: 2 },
    contents: { labels: ["Bug"] },
  };
  const SALES = {
    manifest: {
      id: "sales",
      title: "Sales pipeline",
      summary: "Deals, stages, follow-ups.",
      domain: "sales",
      works_without_agents: false,
    },
    counts: { labels: 2 },
    contents: {},
  };

  it("stays hidden when the catalogue is empty", () => {
    mockPacks.catalogue = { packs: [], domains: [] };
    renderStep({ existing: null, disabled: false });
    expect(screen.queryByText("No pack")).not.toBeInTheDocument();
  });

  it("renders a row per catalogue pack, defaulting to no pack", async () => {
    mockPacks.catalogue = { packs: [HELPDESK, SALES], domains: ["helpdesk", "sales"] };
    renderStep({ existing: null, disabled: false });

    const none = await screen.findByRole("radio", { name: /No pack/ });
    expect(none).toBeChecked();
    const helpdesk = screen.getByRole("radio", { name: /IT helpdesk/ });
    expect(helpdesk).not.toBeChecked();
    // Domain badge, the counts summary and the zero-agent promise all read
    // off the catalogue entry, not off a hardcoded list.
    expect(helpdesk.textContent).toContain("Helpdesk");
    expect(helpdesk.textContent).toContain("5 labels · 3 views · 2 agents");
    expect(helpdesk.textContent).toContain("No agent needed");
    expect(
      screen.getByRole("radio", { name: /Sales pipeline/ }).textContent,
    ).not.toContain("No agent needed");
  });

  it("sends the picked pack as pack_id", async () => {
    mockCreateMutate.mockClear();
    mockPacks.catalogue = { packs: [HELPDESK, SALES], domains: ["helpdesk", "sales"] };
    renderStep({ existing: null, disabled: false });

    fireEvent.click(await screen.findByRole("radio", { name: /IT helpdesk/ }));
    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Create Acme Inc$/ }));

    expect(mockCreateMutate.mock.calls[0]![0]).toMatchObject({
      slug: "acme-inc",
      pack_id: "helpdesk-it",
    });
  });

  it("sends no pack_id when the user deselects back to no pack", async () => {
    mockCreateMutate.mockClear();
    mockPacks.catalogue = { packs: [HELPDESK], domains: ["helpdesk"] };
    renderStep({ existing: null, disabled: false });

    const helpdesk = await screen.findByRole("radio", { name: /IT helpdesk/ });
    fireEvent.click(helpdesk);
    fireEvent.click(helpdesk);
    fireEvent.change(screen.getByLabelText("Workspace name"), {
      target: { value: "Acme Inc" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Create Acme Inc$/ }));

    expect(mockCreateMutate.mock.calls[0]![0]).not.toHaveProperty("pack_id");
    mockPacks.catalogue = { packs: [], domains: [] };
  });
});
