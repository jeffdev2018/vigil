"use client";

import { useEffect, useMemo, useState } from "react";
import { Copy, ExternalLink, Loader2, LogIn, LogOut } from "lucide-react";
import type { AgentRuntime, RuntimeCliAuthRequest } from "@multica/core/types";
import {
  cliAuthLogoutSupported,
  cliAuthRouteUnavailable,
  cliAuthSupported,
  readRuntimeCliAuthState,
  useRuntimeCliAuth,
} from "@multica/core/runtimes/cli-auth";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { cliAuthProviderDocsHref } from "./runtime-docs";

function offlineReason(runtime: AgentRuntime): string | null {
  const value = runtime.metadata?.offline_reason;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const detail = (value as Record<string, unknown>).detail;
  return typeof detail === "string" && detail.trim() ? detail : null;
}

export function CliAuthSection({ runtime }: { runtime: AgentRuntime }) {
  const { t, i18n } = useT("runtimes");
  const auth = useRuntimeCliAuth(runtime.id, runtime.workspace_id);
  const durableState = readRuntimeCliAuthState(runtime.metadata);
  const [request, setRequest] = useState<RuntimeCliAuthRequest | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [routeUnavailable, setRouteUnavailable] = useState(false);
  const [now, setNow] = useState(Date.now());
  const supported = cliAuthSupported(runtime.provider);
  const authenticated =
    request?.status === "completed" && typeof request.authenticated === "boolean"
      ? request.authenticated
      : durableState?.authenticated;
  const active = request?.status === "pending" || request?.status === "running";
  // Copilot signs out from an in-session slash command, so the button stays on
  // "connect" there rather than offering a logout the server would refuse.
  const showDisconnect =
    authenticated === true && cliAuthLogoutSupported(runtime.provider);

  useEffect(() => {
    if (!open || !active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active, open]);

  const secondsLeft = useMemo(() => {
    if (!request?.expires_at) return null;
    const expires = Date.parse(request.expires_at);
    return Number.isFinite(expires)
      ? Math.max(0, Math.ceil((expires - now) / 1000))
      : null;
  }, [now, request?.expires_at]);

  if (routeUnavailable && durableState === null) return null;

  const run = (action: "login" | "logout") => {
    setRequest(null);
    setError("");
    setNow(Date.now());
    setOpen(true);
    auth.mutate(
      { action, onProgress: setRequest },
      {
        onSuccess: setRequest,
        onError: (cause) => {
          if (cliAuthRouteUnavailable(cause, runtime.metadata)) {
            setRouteUnavailable(true);
            setOpen(false);
            return;
          }
          setError(
            cause instanceof Error && cause.message
              ? cause.message
              : t(($) => $.cli_auth.failed),
          );
        },
      },
    );
  };

  const statusLabel =
    authenticated === true
      ? t(($) => $.cli_auth.authenticated)
      : authenticated === false
        ? t(($) => $.cli_auth.not_authenticated)
        : t(($) => $.cli_auth.unknown);

  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className={
          authenticated === true
            ? "text-success"
            : authenticated === false
              ? "text-warning"
              : "text-muted-foreground"
        }
      >
        {runtime.provider}: {statusLabel}
      </span>
      {supported ? (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          disabled={runtime.status !== "online" || auth.isPending}
          title={runtime.status !== "online" ? offlineReason(runtime) ?? t(($) => $.cli_auth.offline) : undefined}
          onClick={() => run(showDisconnect ? "logout" : "login")}
        >
          {showDisconnect ? <LogOut aria-hidden="true" className="h-3 w-3" /> : <LogIn aria-hidden="true" className="h-3 w-3" />}
          {showDisconnect
            ? t(($) => $.cli_auth.disconnect)
            : t(($) => $.cli_auth.connect)}
        </Button>
      ) : (
        <a
          className="text-info hover:underline"
          href={cliAuthProviderDocsHref(runtime.provider, i18n.language)}
          target="_blank"
          rel="noreferrer"
        >
          {t(($) => $.cli_auth.help)}
        </a>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-[480px]">
          <h2 className="text-title-sm font-semibold">
            {request?.action === "logout"
              ? t(($) => $.cli_auth.disconnecting_title, { provider: runtime.provider })
              : t(($) => $.cli_auth.title, { provider: runtime.provider })}
          </h2>
          <p className="text-body text-muted-foreground">
            {t(($) => $.cli_auth.machine_scope)}
          </p>

          {active && (
            <div className="flex items-center gap-2 text-body text-muted-foreground">
              <Loader2 aria-hidden="true" className="h-4 w-4 animate-spin" />
              {t(($) => $.cli_auth.waiting)}
              {secondsLeft !== null && (
                <span>· {t(($) => $.cli_auth.expires_in, { seconds: secondsLeft })}</span>
              )}
            </div>
          )}

          {request?.verification_url && (
            <div className="rounded-lg border p-3">
              <p className="text-caption text-muted-foreground">
                {t(($) => $.cli_auth.open_url)}
              </p>
              <div className="mt-1 flex items-center gap-2">
                <a
                  className="min-w-0 truncate text-body text-info hover:underline"
                  href={request.verification_url}
                  target="_blank"
                  rel="noreferrer"
                >
                  {request.verification_url}
                </a>
                <ExternalLink aria-hidden="true" className="h-4 w-4 shrink-0" />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t(($) => $.cli_auth.copy_url)}
                  onClick={() => navigator.clipboard.writeText(request.verification_url!)}
                >
                  <Copy aria-hidden="true" className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          )}

          {request?.user_code && (
            <div className="rounded-lg border p-3">
              <p className="text-caption text-muted-foreground">
                {t(($) => $.cli_auth.code)}
              </p>
              <div className="mt-1 flex items-center justify-between gap-2">
                <code className="text-title-sm tracking-widest">{request.user_code}</code>
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  onClick={() => navigator.clipboard.writeText(request.user_code!)}
                >
                  <Copy aria-hidden="true" className="h-3.5 w-3.5" />
                  {t(($) => $.cli_auth.copy_code)}
                </Button>
              </div>
            </div>
          )}

          {request?.status === "completed" && (
            <p className="text-body text-success">
              {request.authenticated
                ? t(($) => $.cli_auth.completed)
                : t(($) => $.cli_auth.disconnected)}
            </p>
          )}
          {error && <p className="text-body text-destructive">{error}</p>}
        </DialogContent>
      </Dialog>
    </span>
  );
}
