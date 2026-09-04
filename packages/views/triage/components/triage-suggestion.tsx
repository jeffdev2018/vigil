"use client";

import { Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useReopenTriageItem } from "@multica/core/triage/mutations";
import type { TriageAutoSettings, TriageItem, TriageSuggestion } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Triage auto-ML (K61): what the queue learned from past accept / dismiss
 * clicks, as a suggestion with its confidence and the neighbours behind it.
 * A chip on the row, a panel in the detail; an auto-dismissed item reopens
 * in one click.
 */
export function TriageSuggestionChip({ suggestion }: { suggestion?: TriageSuggestion }) {
  const { t } = useT("triage");
  if (!suggestion?.suggested) return null;
  return (
    <Badge variant={suggestion.suggested === "dismiss" ? "secondary" : "default"} data-testid="triage-suggestion-chip" className="shrink-0 gap-1 text-micro">
      <Sparkles className="size-3" />
      {suggestion.suggested === "dismiss" ? t(($) => $.suggestion.dismiss) : t(($) => $.suggestion.accept)}
      <span className="font-mono tabular-nums">{Math.round(suggestion.confidence * 100)}%</span>
    </Badge>
  );
}

export function TriageSuggestionPanel({ item, suggestion, auto, wsId }: { item: TriageItem; suggestion?: TriageSuggestion; auto?: TriageAutoSettings; wsId: string }) {
  const { t } = useT("triage");
  const reopen = useReopenTriageItem(wsId);
  const dismissedByAuto = item.state === "dismissed";
  return (
    <section data-testid="triage-suggestion-panel" className="flex flex-col gap-1.5">
      <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.suggestion.title)}</h3>
      {!suggestion || !suggestion.suggested ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.suggestion.none)}</p>
      ) : (
        <>
          <p className="text-caption">
            {suggestion.suggested === "dismiss" ? t(($) => $.suggestion.would_dismiss) : t(($) => $.suggestion.would_accept)}
            {" · "}
            {t(($) => $.suggestion.confidence, { percent: Math.round(suggestion.confidence * 100), count: suggestion.neighbors.length })}
          </p>
          <p className="text-caption text-muted-foreground">
            {suggestion.ready
              ? auto?.enabled
                ? t(($) => $.suggestion.auto_on, { threshold: Math.round((auto?.threshold ?? 0.9) * 100) })
                : t(($) => $.suggestion.auto_off)
              : t(($) => $.suggestion.not_ready, { examples: suggestion.examples, min: suggestion.min_examples })}
          </p>
          <ul className="flex flex-col gap-0.5 text-caption text-muted-foreground">
            {suggestion.neighbors.slice(0, 5).map((n) => (
              <li key={n.id} data-testid="triage-neighbor" className="flex gap-2">
                <span className="shrink-0 font-mono">{n.state}</span>
                <span className="truncate">{n.title}</span>
              </li>
            ))}
          </ul>
        </>
      )}
      {dismissedByAuto && (
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="self-start"
          disabled={reopen.isPending}
          onClick={() => reopen.mutate(item.id, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.suggestion.reopen_failed)) })}
        >
          {t(($) => $.suggestion.reopen)}
        </Button>
      )}
    </section>
  );
}
