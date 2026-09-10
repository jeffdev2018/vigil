"use client";

import type { TaskMemoryContext } from "@multica/core/types/agent";
import { useLocale, useT } from "../../i18n";

/**
 * What memory the server prepared for this run, as recorded on the claim.
 *
 * Collapsed by default: it answers "did the agent get its rules?" when a run
 * looks like it ignored them, which is a question asked after the fact. An
 * absent context is stated as unrecorded — runs claimed before the server
 * wrote this column have no selection to show, and that is not the same as
 * having been given nothing.
 */
export function MemoryContextDetails({
  context,
}: {
  context: TaskMemoryContext | undefined;
}) {
  const { t } = useT("agents");
  const locale = useLocale();

  return (
    <details className="mt-3 text-caption">
      <summary className="cursor-pointer font-medium">
        {t(($) => $.transcript.memory_context)}
      </summary>
      <div className="mt-2 max-h-64 space-y-2 overflow-y-auto break-words">
        {!context ? (
          <p className="text-muted-foreground">
            {t(($) => $.transcript.memory_unrecorded)}
          </p>
        ) : (
          <>
            <p className="text-muted-foreground">
              {t(($) => $.transcript.memory_context_hint)}
            </p>
            <time className="block text-muted-foreground" dateTime={context.dispatched_at}>
              {new Date(context.dispatched_at).toLocaleString(locale)}
            </time>
            <p>
              {context.agent_status === "unavailable"
                ? t(($) => $.transcript.memory_load_failed)
                : t(($) => $.transcript.memory_agent_count, {
                    count: context.agent_versions.length,
                  })}
            </p>
            {context.agent_versions.length > 0 && (
              <ul className="space-y-1">
                {context.agent_versions.map((version) => (
                  <li key={version.id} className="break-all font-mono">
                    {t(($) => $.transcript.memory_version_reference, {
                      id: version.id,
                      revision: version.revision,
                    })}
                  </li>
                ))}
              </ul>
            )}
            <p>
              {context.project_version
                ? t(($) => $.transcript.memory_project_version, {
                    revision: context.project_version.revision,
                  })
                : t(($) => $.transcript.memory_no_project)}
            </p>
            {context.project_version && (
              <p className="break-all font-mono">{context.project_version.id}</p>
            )}
          </>
        )}
      </div>
    </details>
  );
}
