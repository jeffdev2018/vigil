"use client";

import { useState } from "react";
import { Swords } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { pairContestRows, useConfirmContest, type Contest, type ContestVerdict } from "@multica/core/issues/contest";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../../i18n";

const VERDICTS: ContestVerdict[] = ["upheld", "dismissed", "mixed"];

const SEVERITY_CLASS = { high: "bg-destructive/15 text-destructive", medium: "bg-warning/20 text-warning", low: "bg-muted text-muted-foreground" } as const;
const ANSWER_CLASS = { accept: "bg-success/15 text-success", refute: "bg-destructive/15 text-destructive", fix: "bg-warning/20 text-warning" } as const;
const STATUS_CLASS: Record<Contest["status"], string> = {
  running: "bg-muted text-muted-foreground animate-pulse",
  answering: "bg-muted text-muted-foreground animate-pulse",
  objections_ready: "bg-warning/20 text-warning",
  answered: "bg-warning/20 text-warning",
  confirmed: "bg-success/15 text-success",
  failed: "bg-destructive/15 text-destructive",
};

/**
 * Contest (K72): the challenger's numbered objections on the left, the
 * author's answer to each on the right, and the human verdict at the bottom.
 */
export function ContestPanel({ contest }: { contest: Contest }) {
  const { t } = useT("contests");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const confirm = useConfirmContest(wsId);
  const [note, setNote] = useState("");
  const rows = pairContestRows(contest);
  const canConfirm = contest.status === "objections_ready" || contest.status === "answered";
  const give = (verdict: ContestVerdict) =>
    confirm.mutate({ id: contest.id, verdict, note }, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.panel.confirm_failed)) });
  return (
    <div data-testid="contest-panel" data-status={contest.status} className="flex flex-col gap-2 rounded-md border border-border p-2 text-caption">
      <div className="flex flex-wrap items-center gap-2">
        <Swords className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span className="font-medium">{t(($) => $.target[contest.target_type])}</span>
        <span className="text-muted-foreground">{t(($) => $.panel.challenged_by, { provider: contest.challenger_provider || "?" })}</span>
        {contest.challenger_kind === "llm" && <span className="rounded bg-muted px-1 text-muted-foreground">{t(($) => $.dialog.service_model)}</span>}
        {contest.same_vendor && <span className="rounded bg-warning/20 px-1 text-warning">{t(($) => $.dialog.same_vendor)}</span>}
        <span className="text-muted-foreground">{t(($) => $.panel.round, { round: contest.round, max: contest.max_rounds })}</span>
        <span data-testid="contest-status" className={cn("ml-auto rounded px-1", STATUS_CLASS[contest.status])}>{t(($) => $.status[contest.status])}</span>
      </div>
      {rows.length === 0 ? (
        contest.status !== "running" && contest.status !== "failed" && <p data-testid="contest-nothing" className="text-muted-foreground">{contest.nothing_to_contest || t(($) => $.panel.nothing)}</p>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div className="flex flex-col gap-2">
            <div className="font-medium">{t(($) => $.panel.objections)}</div>
            {rows.map(({ objection: o }) => (
              <div key={o.n} data-testid="contest-objection" className="flex flex-col gap-0.5 rounded border border-border p-1.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="font-mono text-muted-foreground">#{o.n}</span>
                  <span className={cn("rounded px-1", SEVERITY_CLASS[o.severity])}>{t(($) => $.severity[o.severity])}</span>
                  <span className="text-muted-foreground">{t(($) => $.kind[o.kind])}</span>
                </div>
                <p>{o.claim}</p>
                {o.evidence && <p className="text-muted-foreground"><span className="font-medium">{t(($) => $.panel.evidence)}: </span>{o.evidence}</p>}
                {o.expected_proof && <p className="text-muted-foreground"><span className="font-medium">{t(($) => $.panel.expected_proof)}: </span>{o.expected_proof}</p>}
              </div>
            ))}
          </div>
          <div className="flex flex-col gap-2">
            <div className="font-medium">{t(($) => $.panel.answers)}</div>
            {rows.map(({ objection: o, answer: a }) => (
              <div key={o.n} data-testid="contest-answer" className="flex flex-col gap-0.5 rounded border border-border p-1.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="font-mono text-muted-foreground">#{o.n}</span>
                  {a ? (
                    <span className={cn("rounded px-1", ANSWER_CLASS[a.verdict])}>{t(($) => $.answer_verdict[a.verdict])}</span>
                  ) : (
                    <span className="text-muted-foreground">
                      {contest.author_agent_id === null ? t(($) => $.panel.no_author) : contest.status === "answering" ? t(($) => $.panel.waiting_author) : t(($) => $.panel.no_answer)}
                    </span>
                  )}
                </div>
                {a?.note && <p>{a.note}</p>}
                {a?.proof && <p className="text-muted-foreground"><span className="font-medium">{t(($) => $.panel.proof)}: </span>{a.proof}</p>}
              </div>
            ))}
          </div>
        </div>
      )}
      {canConfirm && (
        <div data-testid="contest-verdict-form" className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <span className="font-medium">{t(($) => $.panel.your_verdict)}</span>
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder={t(($) => $.panel.note_placeholder)} className="h-7 min-w-40 flex-1 text-caption" />
          {VERDICTS.map((v) => (
            <Button key={v} type="button" size="xs" variant="outline" disabled={confirm.isPending} onClick={() => give(v)}>{t(($) => $.verdict[v])}</Button>
          ))}
        </div>
      )}
      {contest.status === "confirmed" && contest.human_verdict !== null && (
        <div data-testid="contest-verdict" className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <span className="rounded bg-success/15 px-1 font-medium text-success">{t(($) => $.verdict[contest.human_verdict ?? "mixed"])}</span>
          {contest.verdict_note && <span>{contest.verdict_note}</span>}
          {contest.confirmed_at && <span className="text-muted-foreground">{t(($) => $.panel.confirmed_on, { date: new Date(contest.confirmed_at).toLocaleDateString(locale) })}</span>}
        </div>
      )}
    </div>
  );
}
