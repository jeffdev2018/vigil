/**
 * A small value that survives a reload of this tab for a bounded time, so a
 * flow interrupted by a refresh resumes where it was instead of restarting:
 * the login page waiting for its emailed code, the onboarding flow after its
 * workspace was created. sessionStorage keeps it to this tab and drops it when
 * the tab closes; the expiry bounds it to how long resuming makes sense.
 *
 * Read it after mount, never during render: the server has no sessionStorage.
 */
interface Envelope {
  value: unknown;
  expiresAt: number;
}

export function saveSessionResume(key: string, value: unknown, ttlMs: number, now = Date.now()): void {
  try {
    window.sessionStorage.setItem(key, JSON.stringify({ value, expiresAt: now + ttlMs } satisfies Envelope));
  } catch {
    // Storage full or blocked: the flow still works, it just won't resume.
  }
}

/** The stored value when present, unexpired and accepted by `valid`; null otherwise. */
export function readSessionResume<T>(key: string, valid: (value: unknown) => value is T, now = Date.now()): T | null {
  let raw: string | null;
  try {
    raw = window.sessionStorage.getItem(key);
  } catch {
    return null;
  }
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<Envelope>;
    if (typeof parsed.expiresAt === "number" && parsed.expiresAt > now && valid(parsed.value)) {
      return parsed.value;
    }
  } catch {
    // Corrupt entry: fall through and drop it.
  }
  clearSessionResume(key);
  return null;
}

export function clearSessionResume(key: string): void {
  try {
    window.sessionStorage.removeItem(key);
  } catch {
    // Nothing to clear when storage is blocked.
  }
}
