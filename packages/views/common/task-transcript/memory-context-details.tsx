"use client";

import type { TaskMemoryContext } from "@multica/core/types/agent";
import { useT } from "../../i18n";

export function MemoryContextDetails({ context }: { context: TaskMemoryContext | undefined }) {
  const { t } = useT("agents");
  return (
    <details className="mt-3 text-caption">
      <summary className="cursor-pointer font-medium">{t(($) => $.transcript.memory_context)}</summary>
      <div className="mt-2 max-h-64 space-y-2 overflow-y-auto break-words">
        {!context ? <p className="text-muted-foreground">{t(($) => $.transcript.memory_unrecorded)}</p> : <>
          <p className="text-muted-foreground">{t(($) => $.transcript.memory_context_hint)}</p>
          <time className="block" dateTime={context.dispatched_at}>{context.dispatched_at}</time>
          <p>{context.agent_status === "unavailable"
            ? t(($) => $.transcript.memory_load_failed)
            : t(($) => $.transcript.memory_agent_count, { count: context.agent_versions.length })}</p>
          <ul className="space-y-1">
            {context.agent_versions.map((version) => <li key={version.id} className="break-all font-mono">{t(($) => $.transcript.memory_version_reference, { id: version.id, revision: version.revision })}</li>)}
          </ul>
          <p>{context.project_version ? t(($) => $.transcript.memory_project_version, { revision: context.project_version.revision }) : t(($) => $.transcript.memory_no_project)}</p>
          {context.project_version && <p className="break-all font-mono">{context.project_version.id}</p>}
        </>}
      </div>
    </details>
  );
}
