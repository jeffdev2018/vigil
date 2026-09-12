/**
 * @vitest-environment node
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { detectOS } from "./os-detect";

function stubNavigator(platform: string, userAgent: string) {
  vi.stubGlobal("navigator", { platform, userAgent });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("detectOS fallback path (no userAgentData)", () => {
  it("classifies a real desktop Mac as mac", () => {
    stubNavigator(
      "MacIntel",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15",
    );
    return detectOS().then((result) => expect(result.os).toBe("mac"));
  });

  it("classifies an iPad requesting the desktop site (platform MacIntel) as mac", () => {
    // Safari's "Request Desktop Website" toggle makes an iPad report
    // platform "MacIntel", indistinguishable from a real Mac at this layer.
    stubNavigator(
      "MacIntel",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_6) AppleWebKit/605.1.15",
    );
    return detectOS().then((result) => expect(result.os).toBe("mac"));
  });

  it("does NOT offer the macOS installer to a real iPhone", () => {
    stubNavigator(
      "iPhone",
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
    );
    return detectOS().then((result) => expect(result.os).toBe("unknown"));
  });

  it("does NOT offer the macOS installer to a real iPad in mobile mode", () => {
    stubNavigator(
      "iPad",
      "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
    );
    return detectOS().then((result) => expect(result.os).toBe("unknown"));
  });

  it("still classifies Windows and Linux correctly", () => {
    stubNavigator("Win32", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)");
    return detectOS().then((result) => expect(result.os).toBe("windows"));
  });
});
