"use client";

import { useState } from "react";
import { Pin, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { ApiError } from "@multica/core/api";
import type { InsightAskResponse } from "@multica/core/insights";
import { useAskInsight, usePinInsight } from "@multica/core/insights";
import { useT } from "../../i18n";
import { InsightChart } from "./insight-chart";

/**
 * Ask a question, see the answer, pin it.
 *
 * Three rules this component exists to hold:
 *
 *  1. A failed translation never clears the box. The question is the only
 *     thing the user wrote; losing it to an error message is the fastest way
 *     to make someone stop asking.
 *  2. A timeout reads differently from a failure, because the user can act on
 *     it (narrow the range) and the retry is worth offering.
 *  3. The answer always shows the question it answers. A figure whose question
 *     is invisible cannot be checked.
 */
export function AskBar({ wsId }: { wsId: string }) {
  const { t } = useT("usage");
  const [question, setQuestion] = useState("");
  // The question the visible answer belongs to, frozen at Ask time so editing
  // the box does not silently relabel a figure that is already on screen.
  const [answeredQuestion, setAnsweredQuestion] = useState("");
  const [answer, setAnswer] = useState<InsightAskResponse | null>(null);
  const [error, setError] = useState<{ kind: "timeout" | "failed"; message: string } | null>(null);

  const ask = useAskInsight();
  const pin = usePinInsight(wsId);

  const submit = () => {
    const trimmed = question.trim();
    if (trimmed === "" || ask.isPending) return;
    setError(null);
    ask.mutate(trimmed, {
      onSuccess: (result) => {
        setAnswer(result);
        setAnsweredQuestion(trimmed);
      },
      onError: (err) => {
        // The box keeps its text either way — see rule 1 above.
        setAnswer(null);
        const status = err instanceof ApiError ? err.status : 0;
        setError(
          status === 503
            ? { kind: "timeout", message: t(($) => $.insights.error_timeout) }
            : { kind: "failed", message: t(($) => $.insights.error_failed) },
        );
      },
    });
  };

  const canPin = answer?.query != null && !pin.isPending;

  return (
    <div className="space-y-4">
      <form
        className="flex items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <Input
          value={question}
          onChange={(event) => setQuestion(event.target.value)}
          placeholder={t(($) => $.insights.ask_placeholder)}
          aria-label={t(($) => $.insights.ask_label)}
          className="flex-1"
        />
        <Button type="submit" disabled={question.trim() === "" || ask.isPending}>
          <Search aria-hidden="true" />
          {ask.isPending ? t(($) => $.insights.asking) : t(($) => $.insights.ask)}
        </Button>
      </form>

      {error ? (
        <div
          role="status"
          data-testid="insight-error"
          data-kind={error.kind}
          className="flex items-center justify-between gap-2 rounded-lg border px-4 py-3 text-caption"
        >
          <span>{error.message}</span>
          {error.kind === "timeout" ? (
            <Button variant="ghost" size="sm" onClick={submit}>
              {t(($) => $.insights.retry)}
            </Button>
          ) : null}
        </div>
      ) : null}

      {answer ? (
        <div className="space-y-3 rounded-lg border p-4">
          <div className="flex items-start justify-between gap-2">
            {/* Rule 3: the question is part of the answer, not a caption. */}
            <p className="min-w-0 flex-1 text-caption text-muted-foreground">{answeredQuestion}</p>
            <Button
              variant="outline"
              size="sm"
              disabled={!canPin}
              onClick={() => {
                if (answer.query == null) return;
                pin.mutate({
                  name: answeredQuestion.slice(0, 80),
                  question: answeredQuestion,
                  query: answer.query,
                });
              }}
            >
              <Pin aria-hidden="true" />
              {t(($) => $.insights.pin)}
            </Button>
          </div>
          <InsightChart query={answer.query} rows={answer.rows} shape={answer.shape} />
          {(answer.warnings ?? []).length > 0 ? (
            <ul className="space-y-0.5 text-caption text-muted-foreground">
              {(answer.warnings ?? []).map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
