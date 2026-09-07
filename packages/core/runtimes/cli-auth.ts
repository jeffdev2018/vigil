import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { RuntimeCliAuthRequest } from "../types/agent";
import { runtimeKeys } from "./queries";

const POLL_INTERVAL_MS = 500;
const FALLBACK_TIMEOUT_MS = 10 * 60_000 + 40_000;
const POLL_SLACK_MS = 10_000;

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
