// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent } from "@testing-library/react";
import type { Agent, ChatSession } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The floating window's session history is the second archive entry point. It
// used to advance the selection with its own copy of the sequence and no
// rollback, so a refused archive left the window reading another conversation
// while the archived one was back in the list. It now runs the shared flow
// (use-archive-session-flow.test.tsx owns the move/rollback matrix); this
// suite pins the wiring: the window's own select, its own clear, and its own
// mutation.

const h = vi.hoisted(() => ({
  archiveFails: false,
  archiveMutate: vi.fn(
    (
      _vars: { sessionId: string; archived: boolean },
      options?: { onError?: () => void },
    ) => {
      if (h.archiveFails) options?.onError?.();
    },
  ),
  setActiveSession: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: h.toastError, info: vi.fn() },
}));

// The popover renders its content inline so the history rows are mounted
// without driving the real Base UI open/close dance.
vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ children }: { children: React.ReactNode }) => (
    <button type="button">{children}</button>
  ),
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: undefined, isLoading: false }),
  useInfiniteQuery: () => ({ data: undefined, isLoading: false }),
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    setQueryData: vi.fn(),
    getQueryData: vi.fn(),
  }),
  queryOptions: (options: unknown) => options,
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (s: unknown) => unknown) =>
      selector({ setActiveSession: h.setActiveSession }),
    { getState: () => ({ setActiveSession: h.setActiveSession }) },
  ),
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useSetChatSessionArchived: () => ({ mutate: h.archiveMutate }),
  useUpdateChatSession: () => ({ mutate: vi.fn() }),
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
  useRegenerateChatQuickActions: () => ({ mutate: vi.fn() }),
  useSetChatSessionProject: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

import { SessionDropdown } from "./chat-window";

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

const AGENT = {
  id: "agent-a",
  name: "Alpha",
  runtime_id: "runtime-a",
} as unknown as Agent;

const first = makeSession("s1");
const second = makeSession("s2");

function renderDropdown(
  sessions: ChatSession[],
  activeSessionId: string | null,
) {
  const onSelectSession = vi.fn();
  renderWithI18n(
    <SessionDropdown
      sessions={sessions}
      agents={[AGENT]}
      activeSessionId={activeSessionId}
      onSelectSession={onSelectSession}
    />,
  );
  return onSelectSession;
}

/** The archive action of the history row whose title is `title`. The trigger
 *  above the list repeats the open conversation's title, so the lookup starts
 *  from the rows, not from the text. */
function archiveButtonFor(title: string): HTMLElement {
  const rows = Array.from(
    document.querySelectorAll<HTMLElement>("[class*='history-row']"),
  );
  const target = rows.find((row) => row.textContent?.includes(title));
  if (!target) throw new Error(`no history row for ${title}`);
  // The compact menu and the hover strip render the same action list; either
  // button runs the same onSelect.
  const button = target.querySelector<HTMLElement>(
    'button[aria-label="Archive"]',
  );
  if (!button) throw new Error(`no archive action for ${title}`);
  return button;
}

beforeEach(() => {
  vi.clearAllMocks();
  h.archiveFails = false;
});

describe("ChatWindow session history archive", () => {
  it("moves off the archived conversation and archives it", () => {
    const onSelectSession = renderDropdown([first, second], "s1");

    fireEvent.click(archiveButtonFor("Chat s1"));

    expect(onSelectSession).toHaveBeenCalledWith(second);
    expect(h.archiveMutate).toHaveBeenCalledWith(
      { sessionId: "s1", archived: true },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("restores the selection and reports it when the server refuses", () => {
    h.archiveFails = true;
    const onSelectSession = renderDropdown([first, second], "s1");

    fireEvent.click(archiveButtonFor("Chat s1"));

    expect(onSelectSession.mock.calls.map(([s]) => s.id)).toEqual(["s2", "s1"]);
    expect(h.toastError).toHaveBeenCalledWith(
      "Couldn't archive the conversation",
    );
  });

  it("clears the selection when the archived conversation was the only one", () => {
    renderDropdown([first], "s1");

    fireEvent.click(archiveButtonFor("Chat s1"));

    expect(h.setActiveSession).toHaveBeenCalledWith(null);
  });
});
