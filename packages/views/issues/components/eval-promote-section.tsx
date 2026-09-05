"use client";

import { useState } from "react";
import { FlaskRound } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { usePromoteIssueToEvalCase } from "@multica/core/eval";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Eval Lab (K24): turn this issue into a reusable eval case.
 *
 * The server refuses (409) when the issue has no acceptance criteria or one
 * of them is not proved — that message is the useful one, so it is surfaced
 * verbatim instead of a generic failure.
 */
export function EvalPromoteSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const workspace = useCurrentWorkspace();
  const promote = usePromoteIssueToEvalCase(workspace?.id ?? "", issueId);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  const onClick = () => {
    setError("");
    promote.mutate(undefined, {
      onSuccess: () => setDone(true),
      onError: (err) =>
        setError(err instanceof Error && err.message ? err.message : t(($) => $.eval_promote.failed)),
    });
  };

  return (
    <div data-testid="eval-promote" className="flex flex-wrap items-center gap-2 text-caption">
      <FlaskRound className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
      {done ? (
        <>
          <span className="text-success">{t(($) => $.eval_promote.added)}</span>
          {workspace?.slug ? (
            <AppLink
              href={`/${workspace.slug}/settings?tab=eval-lab`}
              className="font-medium text-primary hover:underline"
            >
              {t(($) => $.eval_promote.open_eval_lab)}
            </AppLink>
          ) : null}
        </>
      ) : (
        <Button size="sm" variant="outline" disabled={promote.isPending} onClick={onClick}>
          {t(($) => $.eval_promote.promote)}
        </Button>
      )}
      {error ? (
        <span role="alert" className="text-destructive">{error}</span>
      ) : null}
    </div>
  );
}
