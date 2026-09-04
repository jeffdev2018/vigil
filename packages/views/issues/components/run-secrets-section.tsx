"use client";

import { useQuery } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { groupRunSecrets, issueRunSecretsOptions, type RunSecretStatus } from "@multica/core/issues/run-secrets";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

const STATUS_TONE: Record<RunSecretStatus, string> = {
  active: "text-success",
  revoked: "text-muted-foreground",
  expired: "text-warning",
};

/**
 * Run-scoped secrets (K09): which secret keys each run of this issue
 * received as a revocable token, and whether the token still lives. Never
 * a value. Renders nothing until a run used one.
 */
export function RunSecretsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: secrets = [] } = useQuery(issueRunSecretsOptions(wsId, issueId));
  if (secrets.length === 0) return null;
  const groups = groupRunSecrets(secrets);

  return (
    <div data-testid="run-secrets" className="text-caption">
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <KeyRound className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.run_secrets.section)}</span>
      </div>
      <div className="flex flex-col gap-2 pl-2">
        {groups.map((g) => (
          <div key={g.taskId} data-testid="run-secrets-run" className="rounded-md border border-border p-2">
            <div className="mb-1 font-mono text-micro text-muted-foreground">{t(($) => $.run_secrets.run, { id: g.taskId.slice(0, 8) })}</div>
            <ul className="flex flex-col gap-0.5">
              {g.secrets.map((s) => (
                <li key={s.id} className="flex items-center gap-2">
                  <span className="font-mono">{s.key}</span>
                  <span className={cn("ml-auto", STATUS_TONE[s.status])}>{t(($) => $.run_secrets.status[s.status])}</span>
                  <span className="text-muted-foreground">
                    {s.status === "revoked" && s.revoked_at ? timeAgo(s.revoked_at) : s.expires_at ? timeAgo(s.expires_at) : ""}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  );
}
