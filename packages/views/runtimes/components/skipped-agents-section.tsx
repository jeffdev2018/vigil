import { AlertTriangle } from "lucide-react";
import { machineSkippedAgents } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * Lists the agent CLIs the daemon found on this machine but refused to
 * register, with the reason. Without it a CLI that is installed but too old
 * looks exactly like a CLI that was never installed, and the user has nothing
 * to act on.
 *
 * Renders nothing when the machine reported no skipped CLI.
 */
export function SkippedAgentsSection({
  runtimes,
}: {
  runtimes: AgentRuntime[];
}) {
  const { t } = useT("runtimes");
  const skipped = machineSkippedAgents(runtimes);
  if (skipped.length === 0) return null;

  return (
    <section className="mt-6">
      <h2 className="flex items-center gap-2 text-body font-semibold">
        <AlertTriangle
          aria-hidden="true"
          className="h-4 w-4 text-muted-foreground"
        />
        {t(($) => $.machine.skipped_agents.title)}
      </h2>
      <p className="mt-1 text-caption text-muted-foreground">
        {t(($) => $.machine.skipped_agents.hint)}
      </p>
      <ul className="mt-3 divide-y overflow-hidden rounded-lg border bg-card">
        {skipped.map(({ provider, reason }) => (
          <li
            key={provider}
            className="flex flex-col gap-0.5 px-4 py-3 sm:flex-row sm:items-baseline sm:gap-3"
          >
            <span className="font-mono text-caption font-medium">
              {provider}
            </span>
            <span className="min-w-0 text-caption text-destructive">
              {reason}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
