// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { RepoIndexRepo, RepoIndexSettings } from "@multica/core/repo-index";
import { renderWithI18n } from "../../test/i18n";

// The state matrix (disabled / waiting / indexed) and the drift-tolerant
// parsing are covered canonically in packages/core/repo-index/schemas.test.ts.
// What is pinned here is the wiring: what a toggle sends, what the row renders,
// and that a read-only member cannot flip it.

const state = vi.hoisted(() => ({
  settings: null as unknown,
  save: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.settings, isPending: state.settings === null }),
}));

vi.mock("@multica/core/repo-index", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/repo-index")>()),
  repoIndexSettingsOptions: (wsId: string) => ({ queryKey: ["repo-index-settings", wsId] }),
  useSaveRepoIndexSettings: () => ({ mutate: state.save, isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { RepoIndexSection } from "./repo-index-section";

const repo = (over: Partial<RepoIndexRepo> = {}): RepoIndexRepo => ({
  repo_identifier: "git@example.com:team/app.git",
  enabled: true,
  chunk_count: 42,
  file_count: 7,
  unusable_embedding_count: 0,
  last_indexed_commit: "abc1234def5678",
  last_indexed_at: "2026-01-02T03:04:05Z",
  ...over,
});

const settings = (over: Partial<RepoIndexSettings> = {}): RepoIndexSettings => ({
  repos: [repo()],
  embeddings_enabled: false,
  ...over,
});

beforeEach(() => {
  state.settings = settings();
  state.save.mockReset();
  toast.error.mockReset();
});

describe("RepoIndexSection", () => {
  it("shows the indexed commit and volume for an indexed repository", () => {
    renderWithI18n(<RepoIndexSection canEdit />);
    expect(screen.getByText("git@example.com:team/app.git")).toBeInTheDocument();
    // The commit is the point of the line: it tells a reader how far behind the
    // hints a run receives may be.
    expect(screen.getByText(/abc1234/)).toBeInTheDocument();
    expect(screen.getByText(/42/)).toBeInTheDocument();
  });

  it("says an enabled but empty repository is waiting for the next run", () => {
    // The index is built by the daemon AFTER a run, so this gap is normal and
    // must not read as a failure.
    state.settings = settings({ repos: [repo({ chunk_count: 0, last_indexed_commit: "" })] });
    renderWithI18n(<RepoIndexSection canEdit />);
    expect(screen.getByText(/next run/i)).toBeInTheDocument();
  });

  it("sends the repository identifier and the new value on toggle", () => {
    state.settings = settings({ repos: [repo({ enabled: false })] });
    renderWithI18n(<RepoIndexSection canEdit />);
    const toggle = screen.getByRole("switch", { name: "git@example.com:team/app.git" });
    expect(toggle).not.toBeChecked();
    fireEvent.click(toggle);
    expect(state.save).toHaveBeenCalledWith(
      { repo_identifier: "git@example.com:team/app.git", enabled: true },
      expect.anything(),
    );
  });

  it("disables the toggle for a member who cannot manage the workspace", () => {
    renderWithI18n(<RepoIndexSection canEdit={false} />);
    const toggle = screen.getByRole("switch", { name: "git@example.com:team/app.git" });
    // Base UI marks a disabled switch with aria-disabled rather than the DOM
    // attribute, so assert the state a screen reader reads AND that a click
    // reaches nothing — the second is what actually protects the setting.
    expect(toggle).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(toggle);
    expect(state.save).not.toHaveBeenCalled();
  });

  it("says whether ranking is semantic or keyword-only", () => {
    // Whether embeddings are configured is a deployment fact no client can see,
    // so the copy has to follow the server's answer rather than assume one.
    renderWithI18n(<RepoIndexSection canEdit />);
    expect(screen.getByText(/no embeddings model configured/i)).toBeInTheDocument();

    state.settings = settings({ embeddings_enabled: true });
    renderWithI18n(<RepoIndexSection canEdit />);
    expect(screen.getAllByText(/by meaning and by keyword/i).length).toBeGreaterThan(0);
  });

  it("renders an empty state when the workspace has no repository", () => {
    state.settings = settings({ repos: [] });
    renderWithI18n(<RepoIndexSection canEdit />);
    expect(screen.getByText(/No repository is attached/i)).toBeInTheDocument();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });

  it("surfaces a failed save instead of leaving the row silently unchanged", () => {
    state.save.mockImplementation((_input, opts) => opts?.onError?.(new Error("nope")));
    renderWithI18n(<RepoIndexSection canEdit />);
    fireEvent.click(screen.getByRole("switch", { name: "git@example.com:team/app.git" }));
    expect(toast.error).toHaveBeenCalledWith("nope");
  });
});
