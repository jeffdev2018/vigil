"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentVersionDiffOptions, agentVersionsOptions, diffLines, useRollbackAgentVersion } from "@multica/core/agents/versions";
import type { Agent, AgentVersion } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../../i18n";

/**
 * Agent versions (K23): every configuration the agent has run with, newest
 * first. Pick a version to diff it against the active one; roll back to
 * recreate it as a new version — the history stays linear and immutable.
 */
export function HistoryTab({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: versions = [] } = useQuery(agentVersionsOptions(wsId, agent.id));
  const [selected, setSelected] = useState<string | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const rollback = useRollbackAgentVersion(wsId, agent.id);
  const active = versions.find((v) => v.active) ?? versions[0];
  const { data: diff } = useQuery({
    ...agentVersionDiffOptions(wsId, agent.id, active?.id ?? "", selected ?? ""),
    enabled: !!active && !!selected && selected !== active.id,
  });

  if (versions.length <= 1) {
    return (
      <div data-testid="agent-history" data-empty="true" className="p-4 text-caption text-muted-foreground">
        {t(($) => $.history.single_version)}
      </div>
    );
  }

  const doRollback = (v: AgentVersion) =>
    rollback.mutate(v.id, {
      onSuccess: () => {
        setConfirming(null);
        toast.success(t(($) => $.history.rolled_back, { number: v.version_number }));
      },
      onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.history.rollback_failed)),
    });

  return (
    <div data-testid="agent-history" className="flex flex-col gap-3 p-4 text-caption">
      <ul className="flex flex-col gap-1">
        {versions.map((v) => (
          <li
            key={v.id}
            data-testid="agent-version"
            data-active={v.active ? "true" : "false"}
            className={cn("flex items-center gap-2 rounded-md border px-2 py-1.5", selected === v.id && "border-ring")}
          >
            <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setSelected(v.active ? null : v.id)}>
              <span className="font-mono">{t(($) => $.history.version_label, { number: v.version_number })}</span>
              {v.active && <span className="ml-2 text-success">{t(($) => $.history.active)}</span>}
              <span className="ml-2 text-muted-foreground">
                {timeAgo(v.created_at)} · {v.created_by_type === "system" ? t(($) => $.history.by_system) : t(($) => $.history.by_member)}
                {v.note ? ` · ${v.note}` : ""}
              </span>
            </button>
            {canEdit && !v.active && (
              confirming === v.id ? (
                <span className="flex items-center gap-1">
                  <Button type="button" size="sm" disabled={rollback.isPending} onClick={() => doRollback(v)}>
                    {t(($) => $.history.confirm_rollback, { number: v.version_number })}
                  </Button>
                  <Button type="button" size="sm" variant="ghost" onClick={() => setConfirming(null)}>
                    {t(($) => $.history.cancel)}
                  </Button>
                </span>
              ) : (
                <Button type="button" size="sm" variant="outline" onClick={() => setConfirming(v.id)}>
                  {t(($) => $.history.rollback)}
                </Button>
              )
            )}
          </li>
        ))}
      </ul>
      {selected && diff && (
        <div data-testid="agent-version-diff" className="flex flex-col gap-2 rounded-md border p-2">
          <div className="font-medium">
            {t(($) => $.history.diff_title, { from: diff.from.version_number, to: diff.to.version_number })}
          </div>
          {diff.changed_fields.length === 0 ? (
            <div className="text-muted-foreground">{t(($) => $.history.diff_empty)}</div>
          ) : (
            <>
              <div className="text-muted-foreground">{t(($) => $.history.changed_fields, { fields: diff.changed_fields.join(", ") })}</div>
              {diff.changed_fields.includes("model") && (
                <div>
                  <span className="text-muted-foreground">{t(($) => $.history.model)}: </span>
                  <span className="line-through">{diff.from.model || "—"}</span> → {diff.to.model || "—"}
                </div>
              )}
              {diff.changed_fields.includes("skills") && (
                <div className="text-muted-foreground">{t(($) => $.history.skills_count, { from: diff.from.skill_ids.length, to: diff.to.skill_ids.length })}</div>
              )}
              {diff.changed_fields.includes("instructions") && (
                <pre className="max-h-64 overflow-auto rounded bg-muted/40 p-2 font-mono text-caption">
                  {diffLines(diff.from.instructions, diff.to.instructions).map((l, i) => (
                    <div key={i} className={cn(l.kind === "added" && "bg-success/15", l.kind === "removed" && "bg-destructive/15 line-through")}>
                      {l.kind === "added" ? "+ " : l.kind === "removed" ? "- " : "  "}
                      {l.text}
                    </div>
                  ))}
                </pre>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
