// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CriticPolicy } from "@multica/core/critic";
import { renderWithI18n } from "../../test/i18n";

// The validation matrix (critic_required, critic_is_author, budget parsing) is
// canonical in packages/core/critic/schemas.test.ts. This covers the wiring:
// what the form sends, and that it refuses to send an invalid policy at all.

const state = vi.hoisted(() => ({
  policy: null as CriticPolicy | null,
  saved: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [
      { id: "a1", name: "Author", archived_at: null },
      { id: "a2", name: "Critic", archived_at: null },
      { id: "a3", name: "Retired", archived_at: "2026-01-01T00:00:00Z" },
    ],
  }),
}));
vi.mock("@multica/core/critic", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/critic")>()),
  criticPolicyOptions: () => ({ queryKey: ["critic-policy"], queryFn: async () => state.policy }),
  useSaveCriticPolicy: () => ({
    mutate: (data: unknown) => state.saved.push(data),
    isPending: false,
    isError: false,
  }),
}));

import { CriticPolicySection } from "./critic-policy-section";

const policy = (over: Partial<CriticPolicy> = {}): CriticPolicy => ({
  subject_type: "agent",
  subject_id: "a1",
  enabled: false,
  critic_agent_id: null,
  require_distinct_provider: true,
  blocking: false,
  max_rounds: 1,
  max_cost_usd_ticks: null,
  phases: ["change"],
  ...over,
});

function render(subjectType: "agent" | "squad" = "agent", subjectId = "a1") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CriticPolicySection subjectType={subjectType} subjectId={subjectId} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.policy = null;
  state.saved = [];
});

// Base UI Select portals its popup onto document.body.
afterEach(() => cleanup());

describe("CriticPolicySection", () => {
  it("hides the settings while the policy is off", async () => {
    state.policy = policy();
    render();
    await screen.findByTestId("critic-policy");
    expect(screen.queryByTestId("critic-agent")).toBeNull();
    expect(screen.queryByTestId("critic-error")).toBeNull();
  });

  it("refuses to save an enabled policy with no critic", async () => {
    state.policy = policy();
    render();
    fireEvent.click(await screen.findByTestId("critic-enabled"));
    expect(screen.getByTestId("critic-error").textContent).toContain("Choose a critic");
    expect((screen.getByTestId("critic-save") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("critic-save"));
    expect(state.saved).toHaveLength(0);
  });

  it("warns and refuses to save a stored policy naming the agent as its own critic", async () => {
    // The picker cannot produce this — it never lists the subject. It reaches
    // the form from a policy stored before that rule, and the warning has to
    // say which of the two problems it is rather than "choose a critic" when
    // one is already chosen.
    state.policy = policy({ enabled: true, critic_agent_id: "a1" });
    render();
    await screen.findByTestId("critic-policy");
    expect(screen.getByTestId("critic-error").textContent).toContain("cannot be its own critic");
    expect((screen.getByTestId("critic-save") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("critic-save"));
    expect(state.saved).toHaveLength(0);
  });

  it("never offers the subject agent, nor an archived one, as its critic", async () => {
    state.policy = policy({ enabled: true, critic_agent_id: "a2" });
    render();
    const user = userEvent.setup();
    await user.click(await screen.findByTestId("critic-agent"));
    const labels = (await screen.findAllByRole("option")).map((o) => o.textContent);
    expect(labels).toEqual(["Choose an agent", "Critic"]);
  });

  it("sends the whole policy, with the budget converted to ticks", async () => {
    state.policy = policy({ enabled: true, critic_agent_id: "a2", max_cost_usd_ticks: 15_000_000_000 });
    render();
    await screen.findByTestId("critic-policy");
    // The stored budget round-trips into the field rather than showing ticks.
    expect((screen.getByTestId("critic-budget") as HTMLInputElement).value).toBe("1.50");
    fireEvent.click(screen.getByTestId("critic-blocking"));
    fireEvent.change(screen.getByTestId("critic-rounds"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("critic-budget"), { target: { value: "2" } });
    fireEvent.click(screen.getByTestId("critic-save"));
    expect(state.saved).toEqual([
      {
        enabled: true,
        critic_agent_id: "a2",
        require_distinct_provider: true,
        blocking: true,
        max_rounds: 3,
        max_cost_usd_ticks: 20_000_000_000,
      },
    ]);
  });

  it("lets a squad name any agent, itself included in the list", async () => {
    // A squad is not an agent; only the server knows its roster, so the form
    // must not invent a rule about which agent may critique it.
    state.policy = policy({ subject_type: "squad", subject_id: "s1", enabled: true, critic_agent_id: "a1" });
    render("squad", "s1");
    await screen.findByTestId("critic-policy");
    expect(screen.queryByTestId("critic-error")).toBeNull();
    const user = userEvent.setup();
    await user.click(screen.getByTestId("critic-agent"));
    const labels = (await screen.findAllByRole("option")).map((o) => o.textContent);
    expect(labels).toEqual(["Choose an agent", "Author", "Critic"]);
  });
});
