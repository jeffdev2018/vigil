"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ClipboardList } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueHandoffPacketsOptions, issueRunsOptions, splitLines, useCreateHandoffPacket, type HandoffPacket } from "@multica/core/issues/handoff";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Handoff packet (K17): what the last hand left on this issue — objective,
 * decisions, evidence, failed attempts, next action — with the earlier
 * packets behind it and a form for a member to leave a new one against a
 * run. Shows nothing but the form trigger until a packet exists.
 */
export function HandoffPacketCard({ issueId, canWrite = true }: { issueId: string; canWrite?: boolean }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: packets = [] } = useQuery(issueHandoffPacketsOptions(wsId, issueId));
  const { data: runs = [] } = useQuery(issueRunsOptions(issueId));
  const latestRunId = runs[0]?.id ?? null;
  const create = useCreateHandoffPacket(wsId, issueId);
  const [writing, setWriting] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const latest = packets[packets.length - 1];
  const history = packets.slice(0, -1).reverse();
  if (!latest && !(canWrite && latestRunId)) return null;

  return (
    <div data-testid="handoff-packet" className="text-caption">
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <ClipboardList className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.handoff.section)}</span>
        {history.length > 0 && (
          <button type="button" className="ml-auto text-muted-foreground hover:text-foreground" onClick={() => setShowHistory((v) => !v)}>
            {showHistory ? t(($) => $.handoff.hide_history) : t(($) => $.handoff.show_history, { count: history.length })}
          </button>
        )}
      </div>
      <div className="flex flex-col gap-2 pl-2">
        {latest ? <PacketView packet={latest} latest timeAgo={timeAgo} /> : <p className="text-muted-foreground">{t(($) => $.handoff.none)}</p>}
        {showHistory && history.map((p) => <PacketView key={p.id} packet={p} timeAgo={timeAgo} />)}
        {canWrite && latestRunId && (
          writing ? (
            <PacketForm
              runId={latestRunId}
              pending={create.isPending}
              onCancel={() => setWriting(false)}
              onSubmit={(input) =>
                create.mutate(input, {
                  onSuccess: () => setWriting(false),
                  onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.handoff.create_failed)),
                })
              }
            />
          ) : (
            <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setWriting(true)}>
              {t(($) => $.handoff.write)}
            </button>
          )
        )}
      </div>
    </div>
  );
}

function PacketView({ packet, latest = false, timeAgo }: { packet: HandoffPacket; latest?: boolean; timeAgo: (d: string) => string }) {
  const { t } = useT("issues");
  const list = (label: string, items: string[]) =>
    items.length > 0 ? (
      <div>
        <div className="text-muted-foreground">{label}</div>
        <ul className="list-disc pl-4">{items.map((it, i) => <li key={i} className="whitespace-pre-wrap">{it}</li>)}</ul>
      </div>
    ) : null;
  return (
    <div data-testid="handoff-packet-item" data-latest={latest ? "true" : "false"} className={cn("flex flex-col gap-1.5 rounded-md border p-2", latest ? "border-border" : "border-border/60 opacity-80")}>
      <div className="flex items-center gap-2 text-micro text-muted-foreground">
        <span className="rounded border border-border px-1">{t(($) => $.handoff.by[packet.created_by_type])}</span>
        <span className="font-mono">{t(($) => $.handoff.run, { id: packet.run_id.slice(0, 8) })}</span>
        {packet.created_at && <span className="ml-auto">{timeAgo(packet.created_at)}</span>}
      </div>
      <div className="font-medium">{packet.objective}</div>
      {list(t(($) => $.handoff.decisions), packet.decisions)}
      {list(t(($) => $.handoff.evidence), packet.evidence)}
      {list(t(($) => $.handoff.failed_attempts), packet.failed_attempts)}
      {packet.next_action && (
        <div className="rounded bg-muted px-2 py-1">
          <span className="text-muted-foreground">{t(($) => $.handoff.next_action)} </span>
          <span className="font-medium">{packet.next_action}</span>
        </div>
      )}
    </div>
  );
}

function PacketForm({ runId, pending, onSubmit, onCancel }: { runId: string; pending: boolean; onSubmit: (input: { run_id: string; objective: string; decisions: string[]; evidence: string[]; failed_attempts: string[]; next_action: string }) => void; onCancel: () => void }) {
  const { t } = useT("issues");
  const [objective, setObjective] = useState("");
  const [decisions, setDecisions] = useState("");
  const [evidence, setEvidence] = useState("");
  const [failed, setFailed] = useState("");
  const [next, setNext] = useState("");
  return (
    <form
      data-testid="handoff-packet-form"
      className="flex flex-col gap-1.5 rounded-md border border-border p-2"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit({ run_id: runId, objective: objective.trim(), decisions: splitLines(decisions), evidence: splitLines(evidence), failed_attempts: splitLines(failed), next_action: next.trim() });
      }}
    >
      <Input aria-label={t(($) => $.handoff.objective)} placeholder={t(($) => $.handoff.objective)} value={objective} onChange={(e) => setObjective(e.target.value)} />
      <Textarea aria-label={t(($) => $.handoff.decisions)} placeholder={t(($) => $.handoff.one_per_line, { field: t(($) => $.handoff.decisions) })} rows={2} value={decisions} onChange={(e) => setDecisions(e.target.value)} />
      <Textarea aria-label={t(($) => $.handoff.evidence)} placeholder={t(($) => $.handoff.one_per_line, { field: t(($) => $.handoff.evidence) })} rows={2} value={evidence} onChange={(e) => setEvidence(e.target.value)} />
      <Textarea aria-label={t(($) => $.handoff.failed_attempts)} placeholder={t(($) => $.handoff.one_per_line, { field: t(($) => $.handoff.failed_attempts) })} rows={2} value={failed} onChange={(e) => setFailed(e.target.value)} />
      <Input aria-label={t(($) => $.handoff.next_action)} placeholder={t(($) => $.handoff.next_action)} value={next} onChange={(e) => setNext(e.target.value)} />
      <div className="flex gap-1">
        <Button type="submit" size="sm" disabled={pending || objective.trim() === ""}>{t(($) => $.handoff.save)}</Button>
        <Button type="button" size="sm" variant="ghost" onClick={onCancel}>{t(($) => $.handoff.cancel)}</Button>
      </div>
    </form>
  );
}
