"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import type { SandboxNetworkMode, SandboxPolicy } from "@multica/core/types";
import { projectSandboxPolicyOptions, useDeleteProjectSandboxPolicy, usePutProjectSandboxPolicy } from "@multica/core/projects/sandbox-policy";
import { Button } from "@multica/ui/components/ui/button";
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
 * Sandbox policy (JEF-256): per-project restrictions on agent runs — a
 * network allowlist (or no network at all) and a best-effort block on
 * reading sensitive files. The effective line shows the workspace < project
 * merge; the daemon enforces it fail-closed.
 */
export function ProjectSandboxSection({ projectId, canEdit = true }: { projectId: string; canEdit?: boolean }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data, isError } = useQuery(projectSandboxPolicyOptions(wsId, projectId));

  return (
    <div data-testid="project-sandbox-policy" className="mt-6 text-caption">
      <div className="mb-2 flex items-center gap-2 px-2">
        <Shield className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.sandbox_policy.section)}</span>
      </div>
      <p className="mb-1 px-2 text-muted-foreground">{t(($) => $.sandbox_policy.description)}</p>
      <p className="mb-2 px-2 text-muted-foreground">{t(($) => $.sandbox_policy.capability_hint)}</p>
      {isError ? (
        <p role="alert" className="px-2 text-destructive">{t(($) => $.sandbox_policy.load_failed)}</p>
      ) : data ? (
        <ProjectSandboxForm key={projectId} wsId={wsId} projectId={projectId} policy={data.policy} effective={data.effective} canEdit={canEdit} />
      ) : null}
    </div>
  );
}

function ProjectSandboxForm({
  wsId,
  projectId,
  policy,
  effective,
  canEdit,
}: {
  wsId: string;
  projectId: string;
  policy: SandboxPolicy | null;
  effective: SandboxPolicy;
  canEdit: boolean;
}) {
  const { t } = useT("projects");
  const save = usePutProjectSandboxPolicy(wsId, projectId);
  const reset = useDeleteProjectSandboxPolicy(wsId, projectId);
  const draft = policy ?? effective;
  const [mode, setMode] = useState<SandboxNetworkMode>(draft.network_mode);
  const [hosts, setHosts] = useState(draft.allowed_hosts.join("\n"));
  const [blockSensitive, setBlockSensitive] = useState(draft.block_sensitive_files);

  const effectiveSummary =
    t(($) => $.sandbox_policy.effective, { mode: t(($) => $.sandbox_policy.mode[effective.network_mode]) }) +
    (effective.block_sensitive_files ? ` · ${t(($) => $.sandbox_policy.effective_blocked)}` : "") +
    (policy === null ? ` · ${t(($) => $.sandbox_policy.effective_inherited)}` : "");

  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.sandbox_policy.failed));

  const submit = () => {
    save.mutate(
      { network_mode: mode, allowed_hosts: mode === "allowlist" ? parseHosts(hosts) : [], block_sensitive_files: blockSensitive },
      { onSuccess: () => toast.success(t(($) => $.sandbox_policy.toast_saved)), onError: fail },
    );
  };

  return (
    <div className="flex flex-col gap-2 px-2">
      <p data-testid="sandbox-policy-effective" className="text-muted-foreground">{effectiveSummary}</p>
      {policy === null && (
        <p data-testid="sandbox-policy-empty" className="text-muted-foreground">{t(($) => $.sandbox_policy.empty)}</p>
      )}
      {canEdit && (
        <>
          <div role="radiogroup" aria-label={t(($) => $.sandbox_policy.network)} className="inline-flex w-fit items-center gap-0.5 rounded-md bg-muted p-0.5">
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
                {t(($) => $.sandbox_policy.mode[m])}
              </button>
            ))}
          </div>
          <p className="text-muted-foreground">{t(($) => $.sandbox_policy.hint[mode])}</p>
          {mode === "allowlist" && (
            <>
              <label className="block">
                <span className="text-muted-foreground">{t(($) => $.sandbox_policy.hosts_label)}</span>
                <Textarea
                  aria-label={t(($) => $.sandbox_policy.hosts_label)}
                  value={hosts}
                  onChange={(e) => setHosts(e.target.value)}
                  placeholder={t(($) => $.sandbox_policy.hosts_placeholder)}
                  className="mt-1 font-mono"
                />
              </label>
              <p className="text-muted-foreground">{t(($) => $.sandbox_policy.hosts_help)}</p>
            </>
          )}
          <label className="flex items-center gap-2">
            <Switch
              aria-label={t(($) => $.sandbox_policy.block_sensitive)}
              checked={blockSensitive}
              disabled={save.isPending}
              onCheckedChange={setBlockSensitive}
            />
            <span>{t(($) => $.sandbox_policy.block_sensitive)}</span>
          </label>
          <p className="text-muted-foreground">{t(($) => $.sandbox_policy.block_sensitive_help)}</p>
          <div className="flex items-center gap-2">
            <Button type="button" size="sm" disabled={save.isPending || reset.isPending} onClick={submit}>
              {save.isPending ? t(($) => $.sandbox_policy.saving) : t(($) => $.sandbox_policy.save)}
            </Button>
            {policy !== null && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={save.isPending || reset.isPending}
                onClick={() => reset.mutate(undefined, { onSuccess: () => toast.success(t(($) => $.sandbox_policy.toast_reset)), onError: fail })}
              >
                {t(($) => $.sandbox_policy.reset)}
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  );
}
