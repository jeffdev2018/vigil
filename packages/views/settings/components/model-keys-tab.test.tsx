// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockCreate = vi.hoisted(() => vi.fn());
const mockRotate = vi.hoisted(() => vi.fn());
const mockRetire = vi.hoisted(() => vi.fn());

const modelKey = (over: Record<string, unknown>) => ({
  id: "key-1",
  workspace_id: "workspace-1",
  scope: "workspace",
  scope_id: null,
  provider: "anthropic",
  label: "Team key",
  key_hint: "sk-***1a2b",
  active: true,
  priority: 0,
  deactivated_reason: "",
  deactivated_at: null,
  created_by: null,
  created_at: "2026-08-14T00:00:00Z",
  updated_at: "2026-08-14T00:00:00Z",
  ...over,
});

const VENDORS = [
  { id: "anthropic", label: "Anthropic", env_var: "ANTHROPIC_API_KEY" },
  { id: "openai", label: "OpenAI", env_var: "OPENAI_API_KEY" },
];

const data = vi.hoisted(() => ({
  list: undefined as Record<string, unknown> | undefined,
  isLoading: false,
  role: "owner" as "owner" | "admin" | "member",
}));

// Two queries live on this tab: the keys and the projects (for scope names).
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) =>
    options.queryKey[0] === "model-keys"
      ? { data: data.list, isLoading: data.isLoading }
      : { data: [{ id: "proj-1", title: "Apollo" }], isLoading: false },
}));

vi.mock("@multica/core/model-keys", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/model-keys")>()),
  modelKeysOptions: (wsId: string) => ({ queryKey: ["model-keys", wsId] }),
  useCreateModelKey: () => ({ mutateAsync: mockCreate, isPending: false }),
  useRotateModelKey: () => ({ mutateAsync: mockRotate, isPending: false }),
  useRetireModelKey: () => ({ mutateAsync: mockRetire, isPending: false }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({ queryKey: ["projects", wsId] }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: data.role, isLoading: false }),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { ModelKeysTab } from "./model-keys-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("ModelKeysTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.role = "owner";
    data.isLoading = false;
    data.list = {
      configured: true,
      vendors: VENDORS,
      keys: [
        modelKey({ id: "key-1" }),
        modelKey({
          id: "key-2",
          scope: "project",
          scope_id: "proj-1",
          provider: "openai",
          label: "",
          key_hint: "sk-***9z9z",
          priority: 2,
        }),
      ],
      usage: [
        { model_key_id: "key-1", provider: "anthropic", model: "a", task_count: 3, input_tokens: 1000, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd_ticks: 12_500_000_000 },
        { model_key_id: "key-1", provider: "anthropic", model: "b", task_count: 1, input_tokens: 300, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd_ticks: 2_500_000_000 },
      ],
    };
    mockCreate.mockResolvedValue({});
    mockRotate.mockResolvedValue({});
    mockRetire.mockResolvedValue({ retired: true });
  });

  it("lists keys with their hint, scope, vendor and usage — never a raw key", () => {
    render(<ModelKeysTab />, { wrapper: Wrapper });

    // The vendor select also names the vendors, so read the table alone.
    const table = within(screen.getByRole("table"));
    expect(table.getByText("sk-***1a2b")).toBeInTheDocument();
    expect(table.getByText("sk-***9z9z")).toBeInTheDocument();
    expect(table.getByText("Anthropic")).toBeInTheDocument();
    expect(table.getByText("OpenAI")).toBeInTheDocument();
    // Project scope resolves to the project title; workspace scope is named.
    expect(table.getByText("Apollo")).toBeInTheDocument();
    expect(table.getByText("Workspace")).toBeInTheDocument();
    // Usage is summed across the key's models: 4 tasks, 1500 tokens, $1.50.
    expect(screen.getByText("4 tasks · 1.5K tokens · $1.50")).toBeInTheDocument();
    expect(screen.getByText("0 tasks · 0 tokens · $0.00")).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/sk-[a-z0-9]{8,}/i);
  });

  it("explains that keys cannot be stored and hides the form when the server has no secret key", () => {
    data.list = { ...data.list, configured: false, keys: [] };
    render(<ModelKeysTab />, { wrapper: Wrapper });

    expect(screen.getByText("Key storage is not configured")).toBeInTheDocument();
    expect(screen.queryByLabelText("API key")).toBeNull();
    expect(screen.getByText("No BYOK key")).toBeInTheDocument();
  });

  it("shows a banner listing keys the server retired after a failover", () => {
    data.list = {
      ...data.list,
      keys: [
        modelKey({ id: "key-1" }),
        modelKey({
          id: "key-3",
          label: "Old key",
          active: false,
          deactivated_reason: "agent_error.provider_quota_limit",
          deactivated_at: "2026-08-14T00:00:00Z",
        }),
        modelKey({
          id: "key-4",
          label: "",
          key_hint: "sk-***dead",
          active: false,
          deactivated_reason: "agent_error.provider_auth_or_access",
          deactivated_at: "2026-08-14T00:00:00Z",
        }),
        modelKey({ id: "key-5", active: false, deactivated_reason: "rotated" }),
      ],
    };
    render(<ModelKeysTab />, { wrapper: Wrapper });

    expect(screen.getByText("A key was retired automatically")).toBeInTheDocument();
    expect(screen.getByText(/Anthropic · Old key — retired .* after a quota failure/)).toBeInTheDocument();
    expect(screen.getByText(/Anthropic · sk-\*\*\*dead — retired .* after an authentication failure/)).toBeInTheDocument();
    // A manual rotation is not a failover.
    expect(screen.queryByText(/rotated .* after/)).toBeNull();
    expect(screen.getByText(/Retired · rotated/)).toBeInTheDocument();
  });

  it("does not show the banner when no key failed over", () => {
    render(<ModelKeysTab />, { wrapper: Wrapper });
    expect(screen.queryByText("A key was retired automatically")).toBeNull();
  });

  it("creates a workspace key with the first vendor and never echoes the value", async () => {
    const user = userEvent.setup();
    render(<ModelKeysTab />, { wrapper: Wrapper });

    await user.type(screen.getByLabelText("Label"), "Trial");
    const keyInput = screen.getByLabelText("API key") as HTMLInputElement;
    expect(keyInput.type).toBe("password");
    await user.type(keyInput, "sk-live-abcdefgh12345678");
    await user.click(screen.getByRole("button", { name: "Add key" }));

    await waitFor(() =>
      expect(mockCreate).toHaveBeenCalledWith({
        scope: "workspace",
        provider: "anthropic",
        label: "Trial",
        key: "sk-live-abcdefgh12345678",
        priority: 0,
      }),
    );
    expect(toast.success).toHaveBeenCalled();
    // The field is cleared after a save; the value never lingers in the DOM.
    await waitFor(() => expect(keyInput.value).toBe(""));
  });

  it("points at rotation when the server answers 409 for an existing active key", async () => {
    const user = userEvent.setup();
    mockCreate.mockRejectedValue(
      Object.assign(new Error("an active key already exists"), {
        status: 409,
        body: { error: "an active key already exists", code: "model_key_active_conflict" },
      }),
    );
    render(<ModelKeysTab />, { wrapper: Wrapper });

    await user.type(screen.getByLabelText("API key"), "sk-live-abcdefgh12345678");
    await user.click(screen.getByRole("button", { name: "Add key" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Rotate the existing key instead of adding another one.",
    );
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("shows a plain validation error inline", async () => {
    const user = userEvent.setup();
    mockCreate.mockRejectedValue(
      Object.assign(new Error("key looks malformed"), { status: 400 }),
    );
    render(<ModelKeysTab />, { wrapper: Wrapper });

    await user.type(screen.getByLabelText("API key"), "nope");
    await user.click(screen.getByRole("button", { name: "Add key" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("key looks malformed");
    expect(toast.error).toHaveBeenCalledWith("key looks malformed");
  });

  it("rotates a key from the dialog", async () => {
    const user = userEvent.setup();
    render(<ModelKeysTab />, { wrapper: Wrapper });

    await user.click(screen.getAllByRole("button", { name: "Rotate key" })[0]!);
    const input = screen.getByLabelText("New API key") as HTMLInputElement;
    expect(input.type).toBe("password");
    await user.type(input, "sk-live-newnewnewnew");
    await user.click(screen.getByRole("button", { name: "Rotate" }));

    await waitFor(() =>
      expect(mockRotate).toHaveBeenCalledWith({
        keyId: "key-1",
        key: "sk-live-newnewnewnew",
        label: "Team key",
      }),
    );
    expect(toast.success).toHaveBeenCalled();
  });

  it("retires a key after confirmation", async () => {
    const user = userEvent.setup();
    render(<ModelKeysTab />, { wrapper: Wrapper });

    await user.click(screen.getAllByRole("button", { name: "Retire key" })[1]!);
    await user.click(screen.getByRole("button", { name: "Retire" }));

    await waitFor(() => expect(mockRetire).toHaveBeenCalledWith("key-2"));
  });

  it("is read-only for a plain member", () => {
    data.role = "member";
    render(<ModelKeysTab />, { wrapper: Wrapper });

    // The inventory stays visible — it carries only hints.
    expect(screen.getByText("sk-***1a2b")).toBeInTheDocument();
    expect(screen.queryByLabelText("API key")).toBeNull();
    expect(screen.queryByRole("button", { name: "Rotate key" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Retire key" })).toBeNull();
    expect(screen.getByText(/Only workspace owners and admins/)).toBeInTheDocument();
  });

  it("survives an undefined payload", () => {
    data.list = undefined;
    render(<ModelKeysTab />, { wrapper: Wrapper });

    expect(screen.getByText("No BYOK key")).toBeInTheDocument();
    expect(screen.queryByLabelText("API key")).toBeNull();
  });
});
