"use client";

import { useQuery } from "@tanstack/react-query";
import { cycleVelocityOptions } from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { CapacityBar } from "./capacity-bar";

/**
 * Velocity (JEF-246): what each actor actually finished against what they
 * declared. The bars reuse the capacity-bar idiom with done work as the load;
 * an actor with no declared capacity shows the done number only, never an
 * empty track that would imply a limit nobody set. Work done by nobody — or
 * by a squad, which is a routing object rather than an actor — is one
 * "unassigned" line rather than a fake actor.
 */
export function VelocityPanel({ cycleId }: { cycleId: string }) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: velocity, isPending } = useQuery(cycleVelocityOptions(wsId, cycleId));

  const empty =
    !!velocity && velocity.actors.length === 0 && velocity.other_done_points === 0;

  return (
    <section className="flex flex-col gap-2" aria-label={t(($) => $.velocity.title)}>
      <h2 className="text-caption font-medium text-muted-foreground">
        {t(($) => $.velocity.title)}
      </h2>

      {isPending || !velocity ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.velocity.loading)}</p>
      ) : (
        <>
          {empty && (
            <div className="flex flex-col gap-0.5" data-testid="velocity-empty">
              <p className="text-caption text-muted-foreground">{t(($) => $.velocity.empty)}</p>
              <p className="text-caption text-faint-foreground">{t(($) => $.velocity.empty_hint)}</p>
            </div>
          )}

          {velocity.actors.map((actor) => (
            <CapacityBar
              key={`${actor.actor_type}:${actor.actor_id}`}
              label={actor.name}
              side={{ capacity: actor.capacity_points, load: actor.done_points }}
              tone={actor.actor_type === "agent" ? "accent" : "primary"}
              undeclaredLabel={t(($) => $.detail.capacity_undeclared)}
              valueLabel={(load, capacity) =>
                t(($) => $.detail.capacity_value, { load, capacity })
              }
              overLabel={(over) => t(($) => $.detail.over_capacity, { over })}
            />
          ))}

          {velocity.other_done_points > 0 && (
            <p className="text-caption text-muted-foreground" data-testid="velocity-other">
              {t(($) => $.velocity.other)}:{" "}
              <span className="font-mono tabular-nums">
                {t(($) => $.velocity.done_value, { points: velocity.other_done_points })}
              </span>
            </p>
          )}

          {velocity.history.length > 0 && (
            <div className="flex flex-col gap-1" data-testid="velocity-history">
              <h3 className="text-caption font-medium text-muted-foreground">
                {t(($) => $.velocity.history_title)}
              </h3>
              <ul className="flex flex-wrap gap-x-3 gap-y-1">
                {velocity.history.map((h) => (
                  <li key={h.cycle_id} className="text-caption">
                    <AppLink
                      href={paths.cycleDetail(h.cycle_id)}
                      className="text-muted-foreground hover:text-foreground hover:underline"
                    >
                      {h.name}
                    </AppLink>{" "}
                    <span className="font-mono tabular-nums text-muted-foreground">
                      {t(($) => $.velocity.done_value, { points: h.done_points })}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </section>
  );
}
