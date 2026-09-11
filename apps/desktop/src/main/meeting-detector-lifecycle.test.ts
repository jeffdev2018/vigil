// @vitest-environment node
import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";

const electron = vi.hoisted(() => ({
  appHandlers: new Map<string, () => void>(),
  ipcHandlers: new Map<string, (event: unknown, value: unknown) => void>(),
}));
const children = vi.hoisted(() => [] as Array<EventEmitter & { kill: () => void }>);

vi.mock("electron", () => ({
  app: {
    on: (name: string, handler: () => void) => electron.appHandlers.set(name, handler),
  },
  ipcMain: {
    on: (name: string, handler: (event: unknown, value: unknown) => void) =>
      electron.ipcHandlers.set(name, handler),
  },
}));

vi.mock("node:fs", () => ({ existsSync: () => true }));

vi.mock("node:child_process", async () => {
  const { EventEmitter: Emitter } = await import("node:events");
  return {
    spawn: vi.fn(() => {
      const child = Object.assign(new Emitter(), {
        stdout: null,
        kill: vi.fn(),
      });
      children.push(child);
      return child;
    }),
  };
});

const originalPlatform = process.platform;

async function setup() {
  vi.useFakeTimers();
  Object.defineProperty(process, "platform", { value: "darwin" });
  const { setupMeetingDetector } = await import("./meeting-detector");
  setupMeetingDetector(() => null);
}

function setDetectionEnabled(enabled: boolean) {
  electron.ipcHandlers.get("meeting:detection-enabled")!(null, enabled);
}

afterEach(() => {
  vi.useRealTimers();
  vi.resetModules();
  Object.defineProperty(process, "platform", { value: originalPlatform });
  electron.appHandlers.clear();
  electron.ipcHandlers.clear();
  children.length = 0;
});

describe("meeting detector helper lifecycle", () => {
  it("cancels a pending helper restart when the app quits", async () => {
    await setup();
    expect(children).toHaveLength(1);

    // The helper crashes, so a restart is scheduled 5s out; then the app quits.
    children[0]!.emit("exit", 1);
    electron.appHandlers.get("will-quit")!();
    await vi.advanceTimersByTimeAsync(60_000);

    expect(children).toHaveLength(1);
  });

  it("keeps one helper when the previous one exits after a restart of detection", async () => {
    await setup();
    setDetectionEnabled(false);
    expect(children[0]!.kill).toHaveBeenCalled();
    setDetectionEnabled(true);
    expect(children).toHaveLength(2);

    // The killed helper's exit lands after the new one started.
    children[0]!.emit("exit", null);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(children).toHaveLength(2);

    electron.appHandlers.get("will-quit")!();
    expect(children[1]!.kill).toHaveBeenCalled();
  });
});
