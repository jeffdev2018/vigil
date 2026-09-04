/**
 * Mobile push (K64) and the app badge (K63).
 *
 * The device registers its Expo push token with the server, which pushes
 * the morning digest and new Decision Cards with the badge set to the
 * user's unanswered decisions. The badge is also set locally whenever the
 * decisions query refreshes, so it stays right without a push.
 *
 * Simulators and builds without an EAS project id get no token: everything
 * here degrades to a no-op instead of an error in the UI.
 */
import { Platform } from "react-native";
import Constants from "expo-constants";
import * as Device from "expo-device";
import * as Notifications from "expo-notifications";
import { api } from "@/data/api";

let registeredToken: string | null = null;

/** The EAS project id the Expo push service needs; null when the build has none. */
export function pushProjectId(config: { expoConfig?: { extra?: Record<string, unknown> } | null; easConfig?: { projectId?: string } | null }): string | null {
  const eas = (config.expoConfig?.extra as { eas?: { projectId?: unknown } } | undefined)?.eas;
  const fromExtra = typeof eas?.projectId === "string" ? eas.projectId : null;
  const fromEas = typeof config.easConfig?.projectId === "string" ? config.easConfig.projectId : null;
  return fromExtra ?? fromEas;
}

/** ios | android for the server; anything else counts as ios. */
export function pushPlatform(os: string): "ios" | "android" {
  return os === "android" ? "android" : "ios";
}

/** Registers this device's Expo token with the server. Returns the token, or null when unavailable. */
export async function registerForPush(): Promise<string | null> {
  try {
    if (!Device.isDevice) return null;
    const projectId = pushProjectId(Constants);
    if (!projectId) return null;
    const perms = await Notifications.getPermissionsAsync();
    let status = perms.status;
    if (status !== "granted") {
      status = (await Notifications.requestPermissionsAsync({ ios: { allowBadge: true, allowAlert: true, allowSound: true } })).status;
    }
    if (status !== "granted") return null;
    const { data: token } = await Notifications.getExpoPushTokenAsync({ projectId });
    await api.registerPushToken(token, pushPlatform(Platform.OS));
    registeredToken = token;
    return token;
  } catch (e) {
    console.warn("[push] registration skipped", e instanceof Error ? e.message : e);
    return null;
  }
}

/** Forgets this device's token on the server (logout). Best effort. */
export async function unregisterPush(): Promise<void> {
  const token = registeredToken;
  registeredToken = null;
  if (!token) return;
  try {
    await api.unregisterPushToken(token);
  } catch (e) {
    console.warn("[push] unregister skipped", e instanceof Error ? e.message : e);
  }
}

/** Sets the app icon badge to the number of decisions waiting for me (K63). */
export async function setDecisionsBadge(count: number): Promise<void> {
  try {
    await Notifications.setBadgeCountAsync(Math.max(0, Math.floor(count)));
  } catch {
    // No badge permission or unsupported platform: the header count remains.
  }
}
