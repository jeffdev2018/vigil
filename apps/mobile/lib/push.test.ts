// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { pushPlatform, pushProjectId, registerForPush } from "./push";

vi.mock("react-native", () => ({ Platform: { OS: "ios" } }));
vi.mock("expo-constants", () => ({ default: {} }));
vi.mock("expo-device", () => ({ isDevice: false }));
vi.mock("expo-notifications", () => ({ setBadgeCountAsync: vi.fn(), getPermissionsAsync: vi.fn(), requestPermissionsAsync: vi.fn(), getExpoPushTokenAsync: vi.fn() }));
vi.mock("@/data/api", () => ({ api: { registerPushToken: vi.fn(), unregisterPushToken: vi.fn() } }));

describe("push helpers (K64/K63)", () => {
  it("reads the EAS project id from extra or easConfig", () => {
    expect(pushProjectId({ expoConfig: { extra: { eas: { projectId: "p-1" } } } })).toBe("p-1");
    expect(pushProjectId({ expoConfig: { extra: {} }, easConfig: { projectId: "p-2" } })).toBe("p-2");
    expect(pushProjectId({ expoConfig: null, easConfig: null })).toBeNull();
    expect(pushProjectId({ expoConfig: { extra: { eas: { projectId: 3 } } } })).toBeNull();
  });

  it("maps the platform and skips registration off a real device", async () => {
    expect(pushPlatform("android")).toBe("android");
    expect(pushPlatform("ios")).toBe("ios");
    expect(pushPlatform("web")).toBe("ios");
    expect(await registerForPush()).toBeNull();
  });
});
