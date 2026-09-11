"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import type { InsightWidget } from "@multica/core/insights";
import {
  insightRunOptions,
  insightWidgetsOptions,
  useDeleteInsightWidget,
  useUpdateInsightWidget,
} from "@multica/core/insights";
import { useT } from "../../i18n";
import { AskBar } from "./ask-bar";
import { InsightChart } from "./insight-chart";

/**
 * The Insights tab: an ask bar, then the pinned cards in `position` order.
 *
 * Reordering is two buttons, not drag-and-drop. Drag needs a pointer, a
 * keyboard fallback and a live region to be accessible; two buttons are all
 * three at once, and a dashboard is reordered about once a quarter.
 */
export function InsightsTab({ wsId }: { wsId: string }) {
  const { t } = useT("usage");
  const widgetsQuery = useQuery(insightWidgetsOptions(wsId));
  const update = useUpdateInsightWidget(wsId);
  const widgets = widgetsQuery.data ?? [];

  const reportReorderFailure = (err: unknown) =>
    toast.error(
      err instanceof Error && err.message ? err.message : t(($) => $.insights.reorder_failed),
    );

  const swap = (index: number, direction: -1 | 1) => {
    const current = widgets[index];
    const neighbour = widgets[index + direction];
    if (!current || !neighbour) return;
    // Positions are swapped rather than recomputed, so a reorder is two
    // independent PATCHes and a failure on one leaves the other consistent
    // (onSettled re-reads the list either way — see useUpdateInsightWidget).
    update.mutate(
      { id: current.id, input: { position: neighbour.position, expected_revision: current.revision } },
      { onError: reportReorderFailure },
    );
    update.mutate(
      { id: neighbour.id, input: { position: current.position, expected_revision: neighbour.revision } },
      { onError: reportReorderFailure },
    );
  };

  return (
    <div className="space-y-6">
      <AskBar wsId={wsId} />

      {widgetsQuery.isLoading ? (
        <Skeleton className="h-40 w-full" />
      ) : widgets.length === 0 ? (
        <p className="py-8 text-center text-caption text-muted-foreground">
          {t(($) => $.insights.no_widgets)}
        </p>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {widgets.map((widget, index) => (
            <InsightWidgetCard
              key={widget.id}
              wsId={wsId}
              widget={widget}
              canMoveUp={index > 0}
              canMoveDown={index < widgets.length - 1}
              onMoveUp={() => swap(index, -1)}
              onMoveDown={() => swap(index, 1)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function InsightWidgetCard({
  wsId,
  widget,
  canMoveUp,
  canMoveDown,
  onMoveUp,
  onMoveDown,
}: {
  wsId: string;
  widget: InsightWidget;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onMoveUp: () => void;
  onMoveDown: () => void;
}) {
  const { t } = useT("usage");
  // /run only: refreshing a pinned card never calls the model, so what it
  // means today is what it meant when it was pinned.
  const run = useQuery(insightRunOptions(wsId, widget.query));
  const remove = useDeleteInsightWidget(wsId);

  return (
    <section data-testid="insight-widget" className="space-y-3 rounded-lg border p-4">
      <header className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="truncate text-label font-medium">{widget.name}</h3>
          {/* Requirement, not decoration: the question is the only thing that
              lets a reader judge whether the figure answers what they think. */}
          <p className="mt-0.5 line-clamp-2 text-caption text-muted-foreground">
            {widget.question || t(($) => $.insights.no_question)}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={!canMoveUp}
            aria-label={t(($) => $.insights.move_up)}
            onClick={onMoveUp}
          >
            <ArrowUp />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={!canMoveDown}
            aria-label={t(($) => $.insights.move_down)}
            onClick={onMoveDown}
          >
            <ArrowDown />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.insights.remove)}
            onClick={() =>
              remove.mutate(widget.id, {
                onError: (err) =>
                  toast.error(
                    err instanceof Error && err.message
                      ? err.message
                      : t(($) => $.insights.remove_failed),
                  ),
              })
            }
          >
            <Trash2 />
          </Button>
        </div>
      </header>

      {run.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : run.isError ? (
        <p className="py-6 text-center text-caption text-muted-foreground">
          {t(($) => $.insights.error_failed)}
        </p>
      ) : (
        <InsightChart
          query={widget.query}
          rows={run.data?.rows ?? []}
          shape={run.data?.shape}
        />
      )}
      {(run.data?.warnings ?? []).length > 0 ? (
        <ul className="space-y-0.5 text-caption text-muted-foreground">
          {(run.data?.warnings ?? []).map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}
