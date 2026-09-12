"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import type { SandboxNetworkMode, SandboxPolicy } from "@multica/core/types";
import { issueSandboxOverrideOptions, useDeleteIssueSandboxOverride, usePutIssueSandboxOverride } from "@multica/core/issues/sandbox-override";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

const NETWORK_MODES: SandboxNetworkMode[] = ["unrestricted", "allowlist", "none"];

function parseHosts(text: string): string[] {
  return text
    .split("\n")
    .map((h) => h.trim())
    .filter(Boolean);
}

/**
 * Sandbox override (JEF-256): an issue-scoped layer on top of the
 * workspace < project chain. No override means the issue inherits; the
 * most restrictive of the three wins and the daemon enforces fail-closed.
 */
export function SandboxOverrideSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data, isError } = useQuery(issueSandboxOverrideOptions(wsId, issueId));
  const [open, setOpen] = useState(false);

  return (
    <div data-testid="sandbox-override" className="text-caption">
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <Shield className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.sandbox_override.section)}</span>
      </div>
      <div className="flex flex-col gap-1.5 pl-2">
        {isError ? (
          <p role="alert" className="text-destructive">{t(($) => $.sandbox_override.load_failed)}</p>
        ) : data ? (
          <>
            {data.override === null ? (
              <p data-testid="sandbox-override-none" className="text-muted-foreground">{t(($) => $.sandbox_override.none)}</p>
            ) : (
              <p data-testid="sandbox-override-effective" className="text-muted-foreground">
                {t(($) => $.sandbox_override.effective, { mode: t(($) => $.sandbox_override.mode[data.effective.network_mode]) })}
                {data.effective.block_sensitive_files ? ` · ${t(($) => $.sandbox_override.effective_blocked)}` : ""}
              </p>
            )}
            <div>
              <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(true)}>
                {t(($) => $.sandbox_override.edit)}
              </Button>
            </div>
          </>
        ) : null}
      </div>
      {data && (
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>{t(($) => $.sandbox_override.dialog_title)}</DialogTitle>
            </DialogHeader>
            <SandboxOverrideForm
              key={`${issueId}:${open}`}
              wsId={wsId}
              issueId={issueId}
              override={data.override}
              onDone={() => setOpen(false)}
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function SandboxOverrideForm({
  wsId,
  issueId,
  override,
  onDone,
}: {
  wsId: string;
  issueId: string;
  override: SandboxPolicy | null;
  onDone: () => void;
}) {
  const { t } = useT("issues");
  const save = usePutIssueSandboxOverride(wsId, issueId);
  const remove = useDeleteIssueSandboxOverride(wsId, issueId);
  const [mode, setMode] = useState<SandboxNetworkMode>(override?.network_mode ?? "unrestricted");
  const [hosts, setHosts] = useState((override?.allowed_hosts ?? []).join("\n"));
  const [blockSensitive, setBlockSensitive] = useState(override?.block_sensitive_files ?? false);

  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.sandbox_override.failed));

  const submit = () => {
    save.mutate(
      { network_mode: mode, allowed_hosts: mode === "allowlist" ? parseHosts(hosts) : [], block_sensitive_files: blockSensitive },
      { onSuccess: () => { toast.success(t(($) => $.sandbox_override.toast_saved)); onDone(); }, onError: fail },
    );
  };

  return (
    <div className="flex flex-col gap-2 text-caption">
      <p className="text-muted-foreground">{t(($) => $.sandbox_override.description)}</p>
      <p className="text-muted-foreground">{t(($) => $.sandbox_override.capability_hint)}</p>
      <div role="radiogroup" aria-label={t(($) => $.sandbox_override.network)} className="inline-flex w-fit items-center gap-0.5 rounded-md bg-muted p-0.5">
        {NETWORK_MODES.map((m) => (
          <button
            key={m}
            type="button"
            role="radio"
            aria-checked={mode === m}
            disabled={save.isPending}
            onClick={() => setMode(m)}
            className={`rounded px-2 py-1 font-medium transition-colors ${
              mode === m ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            } ${save.isPending ? "cursor-not-allowed" : ""}`}
          >
            {t(($) => $.sandbox_override.mode[m])}
          </button>
        ))}
      </div>
      <p className="text-muted-foreground">{t(($) => $.sandbox_override.hint[mode])}</p>
      {mode === "allowlist" && (
        <>
          <label className="block">
            <span className="text-muted-foreground">{t(($) => $.sandbox_override.hosts_label)}</span>
            <Textarea
              aria-label={t(($) => $.sandbox_override.hosts_label)}
              value={hosts}
              onChange={(e) => setHosts(e.target.value)}
              placeholder={t(($) => $.sandbox_override.hosts_placeholder)}
              className="mt-1 font-mono"
            />
          </label>
          <p className="text-muted-foreground">{t(($) => $.sandbox_override.hosts_help)}</p>
        </>
      )}
      <label className="flex items-center gap-2">
        <Switch
          aria-label={t(($) => $.sandbox_override.block_sensitive)}
          checked={blockSensitive}
          disabled={save.isPending}
          onCheckedChange={setBlockSensitive}
        />
        <span>{t(($) => $.sandbox_override.block_sensitive)}</span>
      </label>
      <p className="text-muted-foreground">{t(($) => $.sandbox_override.block_sensitive_help)}</p>
      <div className="flex items-center gap-2">
        <Button type="button" size="sm" disabled={save.isPending || remove.isPending} onClick={submit}>
          {save.isPending ? t(($) => $.sandbox_override.saving) : t(($) => $.sandbox_override.save)}
        </Button>
        {override !== null && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={save.isPending || remove.isPending}
            onClick={() => remove.mutate(undefined, { onSuccess: () => { toast.success(t(($) => $.sandbox_override.toast_removed)); onDone(); }, onError: fail })}
          >
            {t(($) => $.sandbox_override.remove)}
          </Button>
        )}
      </div>
    </div>
  );
}
