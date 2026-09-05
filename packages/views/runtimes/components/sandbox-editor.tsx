"use client";

import { useState } from "react";
import { AlertTriangle, Container, Monitor, Shield } from "lucide-react";
import { toast } from "sonner";
import type { AgentRuntime, SandboxMode } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

const MODES: SandboxMode[] = ["none", "sandbox", "container"];
const MODE_ICON = { none: Monitor, sandbox: Shield, container: Container };

/** Older backends omit the field; missing means the run stays on the host. */
export function requestedSandboxMode(runtime: AgentRuntime): SandboxMode {
  return runtime.sandbox_mode ?? "none";
}

// The daemon reports `modes` on its heartbeat. Until it has, we cannot know
// what the machine can run, so every choice stays enabled and a hint says so.
function canRun(runtime: AgentRuntime, mode: SandboxMode): boolean {
  const modes = runtime.sandbox_capabilities?.modes;
  if (mode === "none" || !Array.isArray(modes)) return true;
  return modes.includes(mode);
}

function parseHosts(text: string): string[] {
  return text
    .split("\n")
    .map((h) => h.trim())
    .filter(Boolean);
}

// SandboxEditor lets the runtime owner pick the confinement of its runs
// (K10). `none` and `sandbox` save on click, like VisibilityEditor;
// `container` reveals the image / allowed-hosts form and saves on submit.
// The inputs are uncontrolled and read through FormData, so the only local
// state is the not-yet-saved mode choice and the last server error.
export function SandboxEditor({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const updateRuntime = useUpdateRuntime(wsId);
  const requested = requestedSandboxMode(runtime);
  const [draftMode, setDraftMode] = useState<SandboxMode | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const selected = draftMode ?? requested;
  const capabilitiesKnown = Array.isArray(runtime.sandbox_capabilities?.modes);

  const save = (patch: Parameters<typeof updateRuntime.mutate>[0]["patch"]) => {
    setServerError(null);
    updateRuntime.mutate(
      { runtimeId: runtime.id, patch },
      {
        onSuccess: () => {
          setDraftMode(null);
          toast.success(
            t(($) => $.detail.sandbox.toast_updated, {
              mode: t(($) => $.detail.sandbox.mode[patch.sandbox_mode ?? "none"]),
            }),
          );
        },
        onError: (err) =>
          setServerError(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.detail.sandbox.toast_failed),
          ),
      },
    );
  };

  const choose = (mode: SandboxMode) => {
    if (mode === "container") {
      setDraftMode(mode);
      return;
    }
    setDraftMode(null);
    if (mode !== requested) save({ sandbox_mode: mode });
  };

  const submitContainer = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    save({
      sandbox_mode: "container",
      sandbox_image: String(form.get("sandbox_image") ?? "").trim(),
      sandbox_allowed_hosts: parseHosts(String(form.get("sandbox_allowed_hosts") ?? "")),
    });
  };

  return (
    <div className="space-y-2">
      {canEdit ? (
        <div
          role="radiogroup"
          className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5"
        >
          {MODES.map((mode) => {
            const runnable = canRun(runtime, mode);
            const Icon = MODE_ICON[mode];
            return (
              <Tooltip key={mode}>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      role="radio"
                      aria-checked={selected === mode}
                      onClick={() => choose(mode)}
                      disabled={!runnable || updateRuntime.isPending}
                      className={`inline-flex items-center gap-1.5 rounded px-2 py-1 text-caption font-medium transition-colors ${
                        selected === mode
                          ? "bg-background text-foreground shadow-sm"
                          : "text-muted-foreground hover:text-foreground"
                      } ${!runnable || updateRuntime.isPending ? "cursor-not-allowed opacity-60" : ""}`}
                    >
                      <Icon className="h-3 w-3 shrink-0" />
                      <span>{t(($) => $.detail.sandbox.mode[mode])}</span>
                    </button>
                  }
                />
                <TooltipContent>
                  {runnable || mode === "none"
                    ? t(($) => $.detail.sandbox.hint[mode])
                    : t(($) => $.detail.sandbox.unavailable[mode])}
                </TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      ) : (
        <SandboxReadout runtime={runtime} />
      )}

      {canEdit && !capabilitiesKnown && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.sandbox.capabilities_unknown)}
        </p>
      )}

      {canEdit && selected === "container" && (
        <form onSubmit={submitContainer} className="space-y-2">
          <label className="block text-caption">
            <span className="text-muted-foreground">
              {t(($) => $.detail.sandbox.image_label)}
            </span>
            <Input
              name="sandbox_image"
              defaultValue={runtime.sandbox_image ?? ""}
              placeholder={t(($) => $.detail.sandbox.image_placeholder)}
              className="mt-1"
            />
          </label>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.detail.sandbox.image_help)}
          </p>
          <label className="block text-caption">
            <span className="text-muted-foreground">
              {t(($) => $.detail.sandbox.hosts_label)}
            </span>
            <Textarea
              name="sandbox_allowed_hosts"
              defaultValue={(runtime.sandbox_allowed_hosts ?? []).join("\n")}
              placeholder={t(($) => $.detail.sandbox.hosts_placeholder)}
              className="mt-1 font-mono"
            />
          </label>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.detail.sandbox.hosts_help)}
          </p>
          <Button type="submit" size="sm" disabled={updateRuntime.isPending}>
            {updateRuntime.isPending
              ? t(($) => $.detail.sandbox.saving)
              : t(($) => $.detail.sandbox.save)}
          </Button>
        </form>
      )}

      {serverError && (
        <p role="alert" className="text-caption text-destructive">
          {serverError}
        </p>
      )}

      <SandboxStatus runtime={runtime} />
    </div>
  );
}

function SandboxReadout({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const mode = requestedSandboxMode(runtime);
  const Icon = MODE_ICON[mode];
  const hosts = runtime.sandbox_allowed_hosts ?? [];
  return (
    <div className="space-y-1">
      <Tooltip>
        <TooltipTrigger
          render={
            <span className="inline-flex items-center gap-1.5 rounded-md border bg-muted/30 px-2 py-1.5 text-caption">
              <Icon className="h-3 w-3 text-muted-foreground" />
              <span className="font-medium">
                {t(($) => $.detail.sandbox.mode[mode])}
              </span>
            </span>
          }
        />
        <TooltipContent>{t(($) => $.detail.sandbox.hint[mode])}</TooltipContent>
      </Tooltip>
      {mode === "container" && (
        <dl className="text-caption text-muted-foreground">
          <div className="flex gap-1.5">
            <dt>{t(($) => $.detail.sandbox.image_label)}:</dt>
            <dd className="min-w-0 truncate font-mono">
              {runtime.sandbox_image || t(($) => $.detail.sandbox.image_placeholder)}
            </dd>
          </div>
          {hosts.length > 0 && (
            <div className="flex gap-1.5">
              <dt>{t(($) => $.detail.sandbox.hosts_label)}:</dt>
              <dd className="min-w-0 break-all font-mono">{hosts.join(", ")}</dd>
            </div>
          )}
        </dl>
      )}
    </div>
  );
}

// What the last run actually got. Never silent: a requested mode the
// machine could not run is degraded (container → sandbox → none) and the
// mismatch is called out here, mirroring the `run.sandbox_degraded` audit
// entry in the run replay.
function SandboxStatus({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const requested = requestedSandboxMode(runtime);
  const effective = runtime.sandbox_effective;
  const degraded = effective !== undefined && effective !== requested;
  return (
    <div className="space-y-1.5">
      <p className="text-caption text-muted-foreground">
        {effective
          ? t(($) => $.detail.sandbox.last_run, {
              mode: t(($) => $.detail.sandbox.mode[effective]),
            })
          : t(($) => $.detail.sandbox.last_run_none)}
      </p>
      {degraded && (
        <div
          role="status"
          className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-caption text-warning"
        >
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span>
            {t(($) => $.detail.sandbox.degraded, {
              requested: t(($) => $.detail.sandbox.mode[requested]),
              effective: t(($) => $.detail.sandbox.mode[effective]),
            })}
          </span>
        </div>
      )}
    </div>
  );
}
