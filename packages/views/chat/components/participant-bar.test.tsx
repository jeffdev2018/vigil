import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatParticipantList, ChatSession } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";

// The typing expiry matrix, the solo rule and the author fallback are pinned
// in packages/core/chat/participants.test.ts. This suite covers the wiring:
// what renders, what is hidden, and what the buttons call.

const addMutate = vi.hoisted(() => vi.fn());
const removeMutate = vi.hoisted(() => vi.fn());
const roster = vi.hoisted(() => ({ current: undefined as ChatParticipantList | undefined }));
const selfId = vi.hoisted(() => ({ current: "user-1" }));
const wsHandlers = vi.hoisted(() => ({ current: new Map<string, (p: unknown) => void>() }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: selfId.current } }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (p: unknown) => void) => {
    wsHandlers.current.set(event, handler);
  },
}));

vi.mock("@tanstack/react-query", () => ({
  // Only the members list goes through useQuery here; the roster has its own
  // mocked hook below.
  useQuery: () => ({
    data: [
      { user_id: "user-2", name: "Grace", email: "grace@example.test" },
      { user_id: "user-3", name: "Alan", email: "alan@example.test" },
    ],
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/chat/participants", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/chat/participants")>();
  return {
    ...actual,
    useChatParticipants: () => ({ data: roster.current }),
    useAddChatParticipant: () => ({ mutate: addMutate }),
    useRemoveChatParticipant: () => ({ mutate: removeMutate }),
    sendChatTypingPing: vi.fn(),
  };
});

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-avatar={actorId} />,
}));

import { ParticipantBar } from "./participant-bar";

const TEST_RESOURCES = { en: { chat: enChat } };

const session: ChatSession = {
  id: "session-1",
  workspace_id: "ws-1",
  agent_id: "agent-1",
  creator_id: "user-1",
  title: "Shared chat",
  status: "active",
  has_unread: false,
  unread_count: 0,
  last_message: null,
  pinned: false,
  created_at: new Date(0).toISOString(),
  updated_at: new Date(0).toISOString(),
};

function owner(over = {}) {
  return {
    user_id: "user-1",
    name: "Ada",
    avatar_url: null,
    role: "owner" as const,
    joined_at: new Date(0).toISOString(),
    online: false,
    ...over,
  };
}

function peer(over = {}) {
  return {
    user_id: "user-2",
    name: "Grace",
    avatar_url: null,
    role: "participant" as const,
    joined_at: new Date(0).toISOString(),
    online: false,
    ...over,
  };
}

function renderBar(over: Partial<ChatSession> = {}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ParticipantBar session={{ ...session, ...over }} wsId="ws-1" />
    </I18nProvider>,
  );
}

beforeEach(() => {
  addMutate.mockReset();
  removeMutate.mockReset();
  roster.current = undefined;
  selfId.current = "user-1";
  wsHandlers.current = new Map();
});

describe("ParticipantBar", () => {
  it("renders nothing for a solo session the viewer did not create", () => {
    selfId.current = "user-9";
    roster.current = { participants: [owner()] };
    const { container } = renderBar();
    expect(container).toBeEmptyDOMElement();
  });

  it("shows no avatars for a solo session, only the creator's add control", () => {
    roster.current = { participants: [owner()] };
    renderBar();
    expect(screen.queryAllByTestId("chat-participant")).toHaveLength(0);
    expect(screen.getByLabelText(enChat.participants.add)).toBeTruthy();
  });

  it("lists every participant once the session is shared", () => {
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    expect(screen.getAllByTestId("chat-participant")).toHaveLength(2);
  });

  it("marks an online participant", () => {
    roster.current = { participants: [owner(), peer({ online: true })] };
    renderBar();
    expect(screen.getAllByLabelText(enChat.participants.online)).toHaveLength(1);
  });

  it("offers only members who are not already in the session", () => {
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    fireEvent.click(screen.getByLabelText(enChat.participants.add));
    // Grace (user-2) is already a participant; only Alan remains offerable.
    expect(screen.getByText("Alan")).toBeTruthy();
    expect(screen.queryByText("Grace")).toBeNull();
    fireEvent.click(screen.getByText("Alan"));
    expect(addMutate).toHaveBeenCalledWith("user-3");
  });

  it("lets the creator remove a participant", () => {
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    fireEvent.click(screen.getByLabelText("Remove Grace"));
    expect(removeMutate).toHaveBeenCalledWith("user-2");
  });

  it("offers a participant only the leave control, never the roster controls", () => {
    selfId.current = "user-2";
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    expect(screen.getByLabelText(enChat.participants.leave)).toBeTruthy();
    // Non-creators cannot add, and cannot remove anyone but themselves.
    expect(screen.queryByLabelText(enChat.participants.add)).toBeNull();
    expect(screen.queryByLabelText("Remove Ada")).toBeNull();
  });

  it("shows a typing line for a peer's ping and ignores its own", () => {
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    const onTyping = wsHandlers.current.get("chat:typing");
    expect(onTyping).toBeTruthy();

    act(() => onTyping?.({ session_id: "session-1", user_id: "user-1" }));
    expect(screen.queryByText(/is typing/)).toBeNull();

    act(() => onTyping?.({ session_id: "session-1", user_id: "user-2" }));
    expect(screen.getByText("Grace is typing…")).toBeTruthy();
  });

  it("ignores a typing ping addressed to another session", () => {
    roster.current = { participants: [owner(), peer()] };
    renderBar();
    act(() => wsHandlers.current.get("chat:typing")?.({ session_id: "other", user_id: "user-2" }));
    expect(screen.queryByText(/is typing/)).toBeNull();
  });
});
