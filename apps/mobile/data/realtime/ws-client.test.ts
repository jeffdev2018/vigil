import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  readonly sent: string[] = [];

  constructor(readonly url: string) {
    MockWebSocket.instances.push(this);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  receive(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  send(frame: string) {
    this.sent.push(frame);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
}

function connectAuthenticatedClient() {
  const client = new WSClient({
    url: "wss://example.test/ws",
    token: "token",
    workspaceSlug: "workspace",
  });
  client.connect();
  const socket = MockWebSocket.instances[0];
  socket.open();
  socket.receive({ type: "auth_ack" });
  return { client, socket };
}

describe("WSClient application heartbeat", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("reconnects a stale OPEN socket through the jittered backoff path", () => {
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { client, socket } = connectAuthenticatedClient();

    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "auth", payload: { token: "token" } },
      { type: "ping" },
    ]);

    // No pong arrives even though the JS-visible readyState remains OPEN.
    vi.advanceTimersByTime(10_000);
    vi.advanceTimersByTime(1);

    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("keeps a healthy socket connected when its pong arrives", () => {
    const { client, socket } = connectAuthenticatedClient();
    socket.receive({ type: "pong" });

    vi.advanceTimersByTime(10_000);

    expect(MockWebSocket.instances).toHaveLength(1);
    client.disconnect();
  });
});

// realtime-provider.tsx foregrounding: `const resumed = ws.resume(); if
// (!resumed) ws.forceReconnect();`. Exercised here at the WSClient level
// since realtime-provider.tsx itself needs RN/AppState and is out of this
// Node-only test lane's scope (see vitest.config.ts).
describe("WSClient.resume()", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("resuming a paused socket opens exactly one fresh socket, with no redundant forceReconnect", () => {
    const { client } = connectAuthenticatedClient();
    client.pause();
    expect(MockWebSocket.instances).toHaveLength(1);

    // Mirrors realtime-provider.tsx's foreground handler exactly.
    const resumed = client.resume();
    if (!resumed) client.forceReconnect();

    expect(resumed).toBe(true);
    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("is a no-op (returns false) on an already-active socket, so the caller's forceReconnect fallback is what reconnects a zombie", () => {
    const { client } = connectAuthenticatedClient();
    // Never paused — state is already "active" (the zombie case: iOS
    // killed the socket without an AppState background event).
    const resumed = client.resume();

    expect(resumed).toBe(false);
    expect(MockWebSocket.instances).toHaveLength(1);
    client.disconnect();
  });
});
