"use client";

import { useQuery } from "@tanstack/react-query";
import { agentMemoryUsageOptions } from "@multica/core/agents";
import type { AgentMemory } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useLocale, useT } from "../../../i18n";

export function MemoryUsageSection({ wsId, agentId, memories, onHistory }: {
  wsId: string;
  agentId: string;
  memories: AgentMemory[];
  onHistory: (memoryId: string) => void;
}) {
  const { t } = useT("agents");
  const locale = useLocale();
  const query = useQuery(agentMemoryUsageOptions(wsId, agentId));
  const usage = query.data;
  const byId = new Map(memories.map((memory) => [memory.id, memory]));
  const date = (value: string) => new Date(value).toLocaleString(locale);
  return <section className="space-y-3">
    <h3 className="text-body font-medium">{t(($) => $.tab_body.memory.usage_title)}</h3>
    <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.usage_hint)}</p>
    {query.isLoading ? <p className="text-caption">{t(($) => $.tab_body.memory.loading)}</p> : query.isError || !usage ?
      <div role="alert" className="space-y-2">
        <p className="text-caption">{t(($) => $.tab_body.memory.usage_failed)}</p>
        <Button variant="outline" size="sm" onClick={() => void query.refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button>
      </div> : <>
        <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.usage_window, { since: date(usage.since), until: date(usage.until) })}</p>
        <p className="text-body">{t(($) => $.tab_body.memory.usage_coverage, { total: usage.started_runs, recorded: usage.recorded_runs })}</p>
        <dl className="grid grid-cols-2 gap-3 rounded-lg border p-3 lg:grid-cols-4">
          {[
            [t(($) => $.tab_body.memory.usage_included), usage.runs_with_agent_memory],
            [t(($) => $.tab_body.memory.usage_empty), usage.recorded_runs - usage.load_failed_runs - usage.runs_with_agent_memory],
            [t(($) => $.tab_body.memory.usage_load_failed), usage.load_failed_runs],
            [t(($) => $.tab_body.memory.usage_unknown), usage.unrecorded_runs],
          ].map(([label, count]) => <div key={label} className="min-w-0"><dt className="text-caption text-muted-foreground">{label}</dt><dd className="text-title tabular-nums">{count}</dd></div>)}
        </dl>
        {usage.versions.length > 0 && <details>
          <summary className="cursor-pointer text-body">{t(($) => $.tab_body.memory.usage_versions, { count: usage.versions.length })}</summary>
          <ul className="mt-3 max-h-96 space-y-3 overflow-y-auto rounded-lg border p-3">
            {usage.versions.map((version) => {
              const memory = byId.get(version.memory_id);
              return <li key={`${version.memory_id}:${version.revision}`} className="space-y-1">
                <p className="break-all font-mono text-caption">{version.memory_id}</p>
                <p className="text-body">{t(($) => $.tab_body.memory.usage_version_runs, { revision: version.revision, count: version.prepared_runs })}</p>
                <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.usage_last_started, { date: date(version.last_started_at) })}</p>
                {memory && <>
                  <p className="line-clamp-2 break-words text-caption text-muted-foreground">{t(($) => $.tab_body.memory.usage_current_text, { content: memory.content })}</p>
                  <Button variant="link" size="sm" onClick={() => onHistory(memory.id)}>{t(($) => $.tab_body.memory.history_action)}</Button>
                </>}
              </li>;
            })}
          </ul>
        </details>}
      </>}
  </section>;
}
