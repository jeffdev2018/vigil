// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

const exposeInMainWorld = vi.hoisted(() => vi.fn());

vi.mock("electron", () => ({
  contextBridge: { exposeInMainWorld },
  ipcRenderer: {
    sendSync: vi.fn(),
    invoke: vi.fn(),
    send: vi.fn(),
    on: vi.fn(),
    removeListener: vi.fn(),
  },
}));

describe("preload bridge", () => {
  // The renderer only reaches main through the scoped APIs below. Exposing
  // @electron-toolkit's `electronAPI` would hand any script in the renderer
  // invoke/send/on for every IPC channel, including daemon token sync.
  it("exposes only the scoped APIs, never a raw ipcRenderer", async () => {
    Object.defineProperty(process, "contextIsolated", {
      value: true,
      configurable: true,
    });
    await import("./index");

    expect(exposeInMainWorld.mock.calls.map(([name]) => name).sort()).toEqual([
      "daemonAPI",
      "desktopAPI",
      "updater",
    ]);
  });
});
