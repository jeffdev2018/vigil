import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { RuntimeCliAuthRequest } from "../types/agent";
import { runtimeKeys } from "./queries";

const POLL_INTERVAL_MS = 500;
const FALLBACK_TIMEOUT_MS = 10 * 60_000 + 40_000;
const POLL_SLACK_MS = 10_000;

/**
 * Providers whose CLI documents a sign-in command that works without a
 * terminal, mirroring the table in server/pkg/agent/cli_auth.go — the API
 * answers 422 for anything else, so offering the button would only produce an
 * error. Every other provider gets its own documentation link instead
 * (cliAuthProviderDocsHref in packages/views/runtimes).
 *
 * Deliberately small: `opencode auth login` and its peers prompt for a
 * provider to pick, so under the daemon they hang instead of signing anyone
 * in, and most of the other CLIs take an API key from the environment and
 * have no sign-in flow at all.
 */
export const CLI_AUTH_PROVIDERS = [
  "claude",
  "codex",
  "copilot",
  "cursor-agent",
] as const;

/** Whether Multica can drive this provider's sign-in from the runtime page. */
export function cliAuthSupported(provider: string): boolean {
  return (CLI_AUTH_PROVIDERS as readonly string[]).includes(provider);
}

/**
 * Whether the provider also documents a sign-OUT command. Copilot's is an
 * in-session slash command, so the button must not offer to disconnect it.
 */
export function cliAuthLogoutSupported(provider: string): boolean {
  return provider !== "copilot" && cliAuthSupported(provider);
}

export interface RuntimeCliAuthState {
  authenticated: boolean;
  checked_at?: string;
  provider?: string;
  reason?: string;
}

export function readRuntimeCliAuthState(
  metadata: Record<string, unknown> | undefined,
): RuntimeCliAuthState | null {
  const value = metadata?.cli_auth;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const raw = value as Record<string, unknown>;
  if (typeof raw.authenticated !== "boolean") return null;
  return {
    authenticated: raw.authenticated,
    checked_at: typeof raw.checked_at === "string" ? raw.checked_at : undefined,
    provider: typeof raw.provider === "string" ? raw.provider : undefined,
    reason: typeof raw.reason === "string" ? raw.reason : undefined,
  };
}

export function cliAuthRouteUnavailable(
  error: unknown,
  metadata: Record<string, unknown> | undefined,
): boolean {
  return (
    error instanceof ApiError &&
    error.status === 404 &&
    readRuntimeCliAuthState(metadata) === null
  );
}

export async function resolveRuntimeCliAuth(
  runtimeId: string,
  action: "login" | "logout",
  onProgress?: (request: RuntimeCliAuthRequest) => void,
): Promise<RuntimeCliAuthRequest> {
  const initial =
    action === "login"
      ? await api.initiateCliAuth(runtimeId)
      : await api.initiateCliLogout(runtimeId);
  let current = initial;
  onProgress?.(current);
  const parsedExpiry = Date.parse(initial.expires_at);
  const deadline = Number.isFinite(parsedExpiry)
    ? parsedExpiry + POLL_SLACK_MS
    : Date.now() + FALLBACK_TIMEOUT_MS;

  while (current.status === "pending" || current.status === "running") {
    if (Date.now() > deadline) {
      throw new Error(current.error || "CLI authentication request timed out");
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getCliAuthResult(runtimeId, initial.id);
    onProgress?.(current);
  }
  if (current.status !== "completed") {
    throw new Error(
      current.error || `CLI authentication failed (status: ${current.status})`,
    );
  }
  return current;
}

export function useRuntimeCliAuth(runtimeId: string, workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      action,
      onProgress,
    }: {
      action: "login" | "logout";
      onProgress?: (request: RuntimeCliAuthRequest) => void;
    }) => resolveRuntimeCliAuth(runtimeId, action, onProgress),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: runtimeKeys.all(workspaceId) }),
  });
}
