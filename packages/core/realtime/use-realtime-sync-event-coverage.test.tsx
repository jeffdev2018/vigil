/**
 * @vitest-environment jsdom
 *
 * Guard test (JEF-301): every server event family declared in
 * server/pkg/protocol/events.go must be consumed by useRealtimeSync — via a
 * `refreshMap` prefix handler, a dedicated `ws.on(...)` subscription, or an
 * explicit, commented entry in IGNORED_PREFIXES below. Add a new server
 * event family and forget to wire the client, and this test fails instead of
 * the cache silently going stale forever (the gap this ticket found for
 * contest / project_resource / goal / issue_view / decision).
 *
 * EXPECTED_SERVER_PREFIXES is a STATIC snapshot of the `EventXxx = "prefix:verb"`
 * constants declared in server/pkg/protocol/events.go. Keep it in sync by
 * re-running, from the repo root:
 *
 *   grep -oE '"[a-z_]+:[a-zA-Z_-]+"' server/pkg/protocol/events.go \
 *     | tr -d '"' | cut -d: -f1 | sort -u
 *
 * and pasting the result back in below whenever events.go changes.
 *
 * events.go is not the only place a server event prefix is declared — some
 * handlers define their own `EventXxx` constants in their own file
 * (epic.go, run_preview.go, pr_walkthrough.go, review_flag.go, critic.go),
 * and a few publish an inline string literal with no constant at all
 * (org.go, cross_review.go, doc_drift.go, contest.go). Those prefixes
 * ("org", "cross_review", "critic_verdict", "run_preview", "epic_artifact",
 * "review_flag", "pr_walkthrough", "doc_drift", "contest" pre-JEF-301) are
 * real and are in fact wired up in use-realtime-sync.ts today, but this
 * guard can't see them and doesn't require them — it only covers the
 * centralized events.go list.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import type { WSClient } from "../api/ws-client";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

const EXPECTED_SERVER_PREFIXES = [
  "activity",
  "agent",
  "agent_memory",
  "approval",
  "autopilot",
  "brain_capture",
  "budget",
  "calendar",
  "chat",
  "comment",
  "cycle",
  "daemon",
  "decision",
  "delivery",
  "dingtalk_installation",
  "doctrine",
  "followup",
  "github_installation",
  "goal",
  "inbox",
  "invitation",
  "issue",
  "issue_attachments",
  "issue_dependencies",
  "issue_labels",
  "issue_metadata",
  "issue_properties",
  "issue_reaction",
  "issue_recurrence",
  "issue_status",
  "issue_transition",
  "issue_type",
  "issue_view",
  "label",
  "lark_installation",
  "meeting",
  "member",
  "pack",
  "pin",
  "postmortem",
  "project",
  "project_resource",
  "property",
  "pull_request",
  "reaction",
  "run_halt",
  "server",
  "skill",
  "slack_installation",
  "squad",
  "subscriber",
  "task",
  "telegram_installation",
  "triage",
  "vcs_connection",
  "wecom_installation",
  "workspace",
  "workspace_note",
];

// Prefixes intentionally NOT wired into useRealtimeSync, with why.
const IGNORED_PREFIXES = new Set<string>([
  // server:rpc_request / server:rpc_response are the daemon<->server RPC
  // relay channel — never fanned out to browser/desktop WS clients.
  "server",
]);

function createMockWs(): WSClient {
  return {
    on: vi.fn(() => () => {}),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "u1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useRealtimeSync — server event coverage guard", () => {
  it("EXPECTED_SERVER_PREFIXES fixture has no duplicates and is sorted", () => {
    // Sanity check on the fixture itself, not the hook — a stray duplicate
    // or out-of-order paste from the grep output would silently under-test.
    const sorted = [...new Set(EXPECTED_SERVER_PREFIXES)].sort();
    expect(EXPECTED_SERVER_PREFIXES).toEqual(sorted);
  });

  it("consumes every events.go prefix via refreshMap, a dedicated handler, or an explicit ignore", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");
    const setQueryDataSpy = vi.spyOn(qc, "setQueryData");
    const ws = createMockWs();

    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });

    // Prefixes with a dedicated ws.on(exactEventType, ...) subscription —
    // covers both `specificEvents`-routed handlers (issue:updated, chat:*,
    // comment:*, ...) and events with no refreshMap entry at all (meeting,
    // calendar, doctrine, pack, delivery, property, invitation, member,
    // workspace). Introspected from the real ws.on() calls the hook made on
    // mount, not hand-copied.
    const dedicatedPrefixes = new Set(
      vi.mocked(ws.on).mock.calls.map(([type]) => String(type).split(":")[0]),
    );

    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    vi.useFakeTimers();
    try {
      const unhandled: string[] = [];
      for (const prefix of EXPECTED_SERVER_PREFIXES) {
        if (IGNORED_PREFIXES.has(prefix)) continue;
        if (dedicatedPrefixes.has(prefix)) continue;

        // No dedicated per-event handler — the only remaining path is the
        // onAny -> refreshMap[prefix] dispatch. Probe it directly: fire a
        // synthetic frame under this prefix (a made-up verb, since we only
        // care about the prefix split) and see whether anything in the
        // cache moved.
        invalidateSpy.mockClear();
        setQueryDataSpy.mockClear();
        onAny!({ type: `${prefix}:__coverage_probe__`, payload: {} } as never);
        vi.advanceTimersByTime(100);
        // Flush microtasks some handlers kick off before their first cache
        // write (e.g. onInboxInvalidate cancels in-flight queries first).
        await Promise.resolve();
        await Promise.resolve();

        if (invalidateSpy.mock.calls.length === 0 && setQueryDataSpy.mock.calls.length === 0) {
          unhandled.push(prefix);
        }
      }
      expect(unhandled).toEqual([]);
    } finally {
      vi.useRealTimers();
    }
  });
});
