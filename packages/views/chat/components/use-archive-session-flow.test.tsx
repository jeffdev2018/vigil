// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ChatSession } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../test/i18n";
import {
  useArchiveSessionFlow,
  type ArchiveSessionFlowDeps,
} from "./use-archive-session-flow";

// Canonical layer for the archive sequence both chat surfaces run: the
// selection move, the mutation call, and the rollback when the server refuses.
// ChatPage's own suite keeps the wiring and the compact deviation.

const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: toastError },
}));

function makeSession(id: string): ChatSession {
  return {
    id,
    workspace_id: "ws-1",
    agent_id: "agent-a",
    creator_id: "user-1",
    title: `Chat ${id}`,
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
  };
}

const first = makeSession("s1");
const second = makeSession("s2");
const third = makeSession("s3");

interface Harness {
  selectSession: ReturnType<typeof vi.fn>;
  clearSelection: ReturnType<typeof vi.fn>;
  archive: ReturnType<typeof vi.fn>;
  flow: ReturnType<typeof useArchiveSessionFlow>;
}

function setup(
  activeSessionId: string | null,
  history: ChatSession[],
  opts: { fails?: boolean } = {},
): Harness {
  const selectSession = vi.fn();
  const clearSelection = vi.fn();
  const archive = vi.fn(
    (_sessionId: string, options: { onError: () => void }) => {
      if (opts.fails) options.onError();
    },
  );
  const deps: ArchiveSessionFlowDeps = {
    activeSessionId,
    history,
    selectSession,
    clearSelection,
    archive,
  };
  const { result } = renderHook(() => useArchiveSessionFlow(deps), {
    wrapper: ({ children }) => (
      <I18nProvider locale="en" resources={RESOURCES}>
        {children}
      </I18nProvider>
    ),
  });
  return { selectSession, clearSelection, archive, flow: result.current };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("useArchiveSessionFlow", () => {
  it("advances to the next chat in the displayed history", () => {
    const h = setup("s1", [first, second, third]);

    act(() => h.flow(first));

    expect(h.selectSession).toHaveBeenCalledWith(second);
    expect(h.clearSelection).not.toHaveBeenCalled();
    expect(h.archive).toHaveBeenCalledWith(
      "s1",
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("falls back to the previous chat when archiving the last one", () => {
    const h = setup("s3", [first, second, third]);

    act(() => h.flow(third));

    expect(h.selectSession).toHaveBeenCalledWith(second);
  });

  it("clears the selection when archiving the only chat", () => {
    const h = setup("s1", [first]);

    act(() => h.flow(first));

    expect(h.clearSelection).toHaveBeenCalledTimes(1);
    expect(h.selectSession).not.toHaveBeenCalled();
  });

  it("leaves the selection put when the archived chat is not the open one", () => {
    const h = setup("s2", [first, second, third]);

    act(() => h.flow(first));

    expect(h.selectSession).not.toHaveBeenCalled();
    expect(h.clearSelection).not.toHaveBeenCalled();
    expect(h.archive).toHaveBeenCalledTimes(1);
  });

  // The move is optimistic, like the list patch in useSetChatSessionArchived:
  // a refused archive must put the user back on the conversation that is still
  // in the list, not leave them reading another one.
  it("restores the selection and reports the failure when the server refuses", () => {
    const h = setup("s1", [first, second], { fails: true });

    act(() => h.flow(first));

    expect(h.selectSession.mock.calls.map(([s]) => s.id)).toEqual(["s2", "s1"]);
    expect(toastError).toHaveBeenCalledWith(
      "Couldn't archive the conversation",
    );
  });

  it("reports the failure without touching the selection it never moved", () => {
    const h = setup("s2", [first, second], { fails: true });

    act(() => h.flow(first));

    expect(h.selectSession).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledWith(
      "Couldn't archive the conversation",
    );
  });

  it("uses the caller's own selection move, and rolls it back too", () => {
    const h = setup("s1", [first, second], { fails: true });
    const moveSelection = vi.fn();
    const rollback = vi.fn();

    act(() => h.flow(first, { moveSelection, rollback }));

    expect(moveSelection).toHaveBeenCalledTimes(1);
    // The default advance must not run beside the caller's move.
    expect(h.selectSession).toHaveBeenCalledTimes(1);
    expect(h.selectSession).toHaveBeenCalledWith(first);
    expect(rollback).toHaveBeenCalledTimes(1);
  });

  it("does not run the caller's rollback when the selection never moved", () => {
    const h = setup("s2", [first, second], { fails: true });
    const rollback = vi.fn();

    act(() => h.flow(first, { rollback }));

    expect(rollback).not.toHaveBeenCalled();
  });
});
