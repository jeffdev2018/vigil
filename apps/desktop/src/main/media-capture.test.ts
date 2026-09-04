// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

// `electron` resolves to the binary path outside a real Electron process, so
// stub the one API media-capture pulls in at module scope.
vi.mock("electron", () => ({
  desktopCapturer: { getSources: vi.fn(async () => []) },
}));

const { planDisplayMedia } = await import("./media-capture");

describe("planDisplayMedia", () => {
  it("answers with the requesting frame on Linux audio capture", () => {
    expect(planDisplayMedia("linux", true, true)).toBe("frame-loopback");
  });

  it("enumerates screens on Linux when there is no frame or no audio", () => {
    expect(planDisplayMedia("linux", true, false)).toBe("screen-loopback");
    expect(planDisplayMedia("linux", false, true)).toBe("screen-loopback");
  });

  it("enumerates screens on macOS and Windows", () => {
    expect(planDisplayMedia("darwin", true, true)).toBe("screen-loopback");
    expect(planDisplayMedia("win32", true, true)).toBe("screen-loopback");
  });
});
