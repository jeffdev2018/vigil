"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Swords } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { duelCostUsd, duelDuration, issueDuelOptions, useConfirmDuel, useStartDuel, type AgentDuel, type AgentDuelSide, type DuelWinner } from "@multica/core/issues/duel";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Agent duel (K39): the two candidate runs side by side (status, cost,
 * duration, tool calls, arbiter score and summary), the arbiter's pick
 * with its reasoning, and the buttons for the human's final verdict. Also
 * the form to launch one (two different agents).
 */
export function DuelSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: duel } = useQuery(issueDuelOptions(wsId, issueId));
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: canManage });
  const start = useStartDuel(wsId, issueId);
  const confirm = useConfirmDuel(wsId, issueId);
  const [open, setOpen] = useState(false);
  const [agentA, setAgentA] = useState("");
  const [agentB, setAgentB] = useState("");
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.duel.failed));
  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);
  const sideLabel = (w: DuelWinner) => (w === "tie" ? "" : agentName(duel?.[w].agent_id ?? ""));
  const running = duel?.status === "running";
  const valid = agentA !== "" && agentB !== "" && agentA !== agentB;
  if (!duel && (!canManage || agents.length < 2)) return null;

  return (
    <div data-testid="duel-section" className="flex flex-col gap-2 text-caption">
      {duel && (
        <div data-testid="duel" data-status={duel.status} className={cn("flex flex-col gap-1.5 rounded-md border p-2", duel.status === "verdict_ready" ? "border-warning/60" : "border-border")}>
          <div className="flex items-center gap-2 font-medium">
            <Swords className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
            <span>{t(($) => $.duel.section)}</span>
            <span className={cn("ml-auto rounded px-1", duel.status === "confirmed" ? "bg-success/15 text-success" : duel.status === "verdict_ready" ? "bg-warning/20 text-warning" : "bg-muted text-muted-foreground")}>{t(($) => $.duel.status[duel.status])}</span>
          </div>
          <div className="grid grid-cols-2 gap-2">
            {(["a", "b"] as const).map((side) => (
              <DuelCandidate key={side} side={side} run={duel[side]} duel={duel} name={agentName(duel[side].agent_id)} />
            ))}
          </div>
          {duel.status === "inconclusive" && <p data-testid="duel-inconclusive" className="text-destructive">{t(($) => $.duel.inconclusive)}</p>}
          {duel.status !== "running" && duel.status !== "inconclusive" && (
            <p data-testid="duel-arbiter" className="text-muted-foreground">
              {duel.arbiter_winner ? (duel.arbiter_winner === "tie" ? t(($) => $.duel.arbiter_tie) : t(($) => $.duel.arbiter_pick, { side: sideLabel(duel.arbiter_winner) })) : t(($) => $.duel.arbiter_unavailable, { error: duel.arbiter_error ?? "" })}
              {duel.reasoning && <span> — {duel.reasoning}</span>}
            </p>
          )}
          {duel.status === "confirmed" && duel.winner && (
            <p data-testid="duel-winner" className="font-medium text-success">{duel.winner === "tie" ? t(($) => $.duel.winner_tie) : t(($) => $.duel.winner, { side: sideLabel(duel.winner) })}</p>
          )}
          {duel.status === "verdict_ready" && canManage && (
            <div className="flex gap-1">
              {(["a", "b", "tie"] as const).map((w) => (
                <Button key={w} size="sm" variant={w === "tie" ? "ghost" : duel.arbiter_winner === w ? "default" : "outline"} disabled={confirm.isPending} onClick={() => confirm.mutate({ duelId: duel.id, winner: w }, { onError: fail })}>
                  {w === "tie" ? t(($) => $.duel.tie) : t(($) => $.duel.choose, { side: agentName(duel[w].agent_id) })}
                </Button>
              ))}
            </div>
          )}
        </div>
      )}
      {canManage && !running && agents.length >= 2 && (
        open ? (
          <form data-testid="duel-form" className="flex flex-wrap items-center gap-1.5 rounded-md border border-border p-2" onSubmit={(e) => { e.preventDefault(); if (valid) start.mutate({ agent_a_id: agentA, agent_b_id: agentB }, { onError: fail, onSuccess: () => { setOpen(false); setAgentA(""); setAgentB(""); } }); }}>
            {([["a", agentA, setAgentA], ["b", agentB, setAgentB]] as const).map(([side, value, set]) => (
              <select key={side} aria-label={side === "a" ? t(($) => $.duel.agent_a) : t(($) => $.duel.agent_b)} className="rounded-md border border-input bg-transparent px-2 py-1" value={value} onChange={(e) => set(e.target.value)}>
                <option value="">{t(($) => $.duel.pick_agent)}</option>
                {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            ))}
            {agentA !== "" && agentA === agentB && <span className="text-destructive">{t(($) => $.duel.identical)}</span>}
            <Button type="submit" size="sm" disabled={!valid || start.isPending}>{t(($) => $.duel.launch)}</Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>{t(($) => $.duel.cancel)}</Button>
          </form>
        ) : (
          <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setOpen(true)}>{t(($) => $.duel.open)}</button>
        )
      )}
    </div>
  );
}

function DuelCandidate({ side, run, duel, name }: { side: "a" | "b"; run: AgentDuelSide; duel: AgentDuel; name: string }) {
  const { t } = useT("issues");
  const picked = duel.winner === side || (duel.winner === null && duel.arbiter_winner === side);
  return (
    <div data-testid="duel-candidate" data-side={side} data-outcome={run.outcome ?? "pending"} className={cn("flex flex-col gap-0.5 rounded-md border p-2", picked ? "border-success/60" : "border-border")}>
      <div className="flex items-center gap-2 font-medium">
        <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", run.outcome === "completed" ? "bg-success" : run.outcome === "failed" ? "bg-destructive" : "bg-info animate-pulse")} />
        <span className="truncate">{name}</span>
        <span className="ml-auto font-mono text-muted-foreground">{run.outcome ?? run.task_status}</span>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-2 text-muted-foreground">
        <dt>{t(($) => $.duel.cost)}</dt><dd className="font-mono">{duelCostUsd(run.cost_usd_ticks)}</dd>
        <dt>{t(($) => $.duel.duration)}</dt><dd className="font-mono">{duelDuration(run.duration_seconds)}</dd>
        <dt>{t(($) => $.duel.tool_calls)}</dt><dd className="font-mono">{run.tool_calls}</dd>
        {run.quality_score !== null && <><dt>{t(($) => $.duel.quality)}</dt><dd className="font-mono">{run.quality_score}/100</dd></>}
      </dl>
      {run.summary && <p className="text-muted-foreground">{run.summary}</p>}
    </div>
  );
}
