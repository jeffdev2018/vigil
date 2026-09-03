"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  decisionAnswerLabel,
  isDecisionPending,
  issueDecisionsOptions,
  useRespondIssueDecision,
} from "@multica/core/issues/decisions";
import type { IssueDecision } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Decision Cards (K01): the questions an agent asked on this issue. Pending
 * cards come first with one button per option (the recommended one leads)
 * and a free-text alternative; answered cards keep the recorded answer.
 * Renders nothing until a card exists.
 */
export function DecisionCardsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: decisions = [] } = useQuery(issueDecisionsOptions(wsId, issueId));
  if (decisions.length === 0) return null;
  const pending = decisions.filter(isDecisionPending).length;

  return (
    <div data-testid="decision-cards" className="text-caption">
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <span>{t(($) => $.decisions.section)}</span>
        {pending > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-warning">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warning" />
            <span className="font-mono tabular-nums">{pending}</span>
          </span>
        )}
      </div>
      <div className="flex flex-col gap-2 pl-2">
        {decisions.map((d) => (
          <DecisionCard key={d.id} decision={d} issueId={issueId} wsId={wsId} />
        ))}
      </div>
    </div>
  );
}

const URGENCY_TONE: Record<string, string> = {
  high: "text-destructive",
  normal: "text-muted-foreground",
  low: "text-faint-foreground",
};

export function DecisionCard({ decision, issueId, wsId }: { decision: IssueDecision; issueId: string; wsId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const respond = useRespondIssueDecision(wsId);
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  const pending = isDecisionPending(decision);

  const answer = (body: { option_id?: string; modified_text?: string }) =>
    respond.mutate(
      { issueId, decisionId: decision.id, answer: body },
      { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.decisions.answer_failed)) },
    );

  return (
    <div
      data-testid="decision-card"
      data-pending={pending ? "true" : "false"}
      className={cn("flex flex-col gap-1.5 rounded-md border p-2", pending ? "border-warning/50" : "border-border opacity-80")}
    >
      <div className="flex items-start gap-2">
        <span className="min-w-0 flex-1 whitespace-pre-wrap font-medium">{decision.question}</span>
        <span className={cn("shrink-0 uppercase", URGENCY_TONE[decision.urgency] ?? "text-muted-foreground")}>
          {decision.urgency}
        </span>
      </div>
      {pending ? (
        <>
          <div className="flex flex-col gap-1">
            {decision.options.map((o) => {
              const recommended = o.id === decision.recommended_option_id;
              return (
                <Button
                  key={o.id}
                  type="button"
                  size="sm"
                  variant={recommended ? "default" : "outline"}
                  disabled={respond.isPending}
                  onClick={() => answer({ option_id: o.id })}
                  className="h-auto justify-start whitespace-normal py-1 text-left"
                  title={o.impact}
                >
                  <span className="flex flex-col items-start">
                    <span>
                      {o.label}
                      {recommended && <span className="ml-1 opacity-70">· {t(($) => $.decisions.recommended)}</span>}
                    </span>
                    {o.impact && <span className="text-caption font-normal opacity-70">{o.impact}</span>}
                  </span>
                </Button>
              );
            })}
          </div>
          {editing ? (
            <div className="flex flex-col gap-1">
              <Textarea
                aria-label={t(($) => $.decisions.modify_placeholder)}
                placeholder={t(($) => $.decisions.modify_placeholder)}
                value={text}
                onChange={(e) => setText(e.target.value)}
                rows={2}
              />
              <div className="flex gap-1">
                <Button type="button" size="sm" disabled={respond.isPending || text.trim() === ""} onClick={() => answer({ modified_text: text.trim() })}>
                  {t(($) => $.decisions.send)}
                </Button>
                <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
                  {t(($) => $.decisions.cancel)}
                </Button>
              </div>
            </div>
          ) : (
            <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setEditing(true)}>
              {t(($) => $.decisions.modify)}
            </button>
          )}
        </>
      ) : (
        <div className="text-muted-foreground">
          <span className="text-foreground">{decisionAnswerLabel(decision)}</span>
          {decision.responded_at && <span> · {timeAgo(decision.responded_at)}</span>}
          {decision.resume_task_id && <span> · {t(($) => $.decisions.resumed)}</span>}
        </div>
      )}
    </div>
  );
}
