import { shell, type BrowserWindow } from "electron";

// True when the URL parses and uses http/https — the only schemes we let
// reach `shell.openExternal`. Scheme comparison is safe because the WHATWG
// URL parser lowercases the protocol field.
export function isSafeExternalHttpUrl(url: string): boolean {
  return getHttpProtocol(url) !== null;
}

// The ONLY non-http(s) URLs the renderer may hand to the OS shell: exact
// strings, not a scheme allowlist, so `x-apple.systempreferences:` stays
// closed to everything except the two panes we deliberately deep-link to.
// A recorder whose microphone was denied cannot re-prompt — macOS only asks
// once — so the toast has to send the user to the pane that can undo it.
//
// Mirrored in packages/views/platform/open-external.ts, which is what the
// renderer actually calls with them.
const ALLOWED_SYSTEM_SETTINGS_URLS = new Set([
  "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
  "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
]);

// Canonical wrapper around shell.openExternal. All renderer-controlled URLs
// that eventually reach the OS shell MUST flow through here; direct calls
// to `shell.openExternal` elsewhere in the main process are banned by the
// no-restricted-syntax rule in apps/desktop/eslint.config.mjs.
export function openExternalSafely(url: string): Promise<void> | void {
  if (getHttpProtocol(url) === null && !ALLOWED_SYSTEM_SETTINGS_URLS.has(url)) {
    console.warn(`[security] blocked openExternal: ${describeScheme(url)}`);
    return;
  }
  return shell.openExternal(url);
}

// Canonical wrapper around webContents.downloadURL. All renderer-controlled
// URLs that trigger a native download MUST flow through here; direct calls
// to `webContents.downloadURL` elsewhere in the main process are banned by
// the no-restricted-syntax rule in apps/desktop/eslint.config.mjs.
// Reuses the same http/https allowlist as openExternalSafely.
export function downloadURLSafely(win: BrowserWindow, url: string): void {
  if (getHttpProtocol(url) === null) {
    console.warn(`[security] blocked downloadURL: ${describeScheme(url)}`);
    return;
  }
  win.webContents.downloadURL(url);
}

function getHttpProtocol(url: string): "http:" | "https:" | null {
  try {
    const { protocol } = new URL(url);
    if (protocol === "http:" || protocol === "https:") return protocol;
    return null;
  } catch {
    return null;
  }
}

function describeScheme(url: string): string {
  try {
    return `scheme=${new URL(url).protocol}`;
  } catch {
    return "invalid URL";
  }
}
