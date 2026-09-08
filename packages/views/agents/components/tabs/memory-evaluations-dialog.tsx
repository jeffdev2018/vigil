"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { AgentMemory } from "@multica/core/types";
import { agentMemoryEvaluationsOptions, agentMemoryEvaluationOptions, useImportAgentMemoryEvaluation, useDeleteAgentMemoryEvaluation, useUpdateAgentMemory, useCancelMemoryExecution } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { MemoryExecutionForm } from "./memory-execution-form";
import { useT } from "../../../i18n";

export function MemoryEvaluationsDialog({ wsId, agentId, memory, onClose }: { wsId: string; agentId: string; memory: AgentMemory; onClose: () => void }) {
  const { t } = useT("agents");
  const list = useQuery(agentMemoryEvaluationsOptions(wsId, agentId, memory.id));
  const [selected, setSelected] = useState("");
  const detail = useQuery(agentMemoryEvaluationOptions(wsId, agentId, memory.id, selected));
  const upload = useImportAgentMemoryEvaluation(wsId, agentId, memory.id);
  const remove = useDeleteAgentMemoryEvaluation(wsId, agentId, memory.id);
  const adopt = useUpdateAgentMemory(wsId, agentId);
  const cancel = useCancelMemoryExecution(wsId, agentId, memory.id);
  const [reading, setReading] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const busy = reading || upload.isPending || remove.isPending || adopt.isPending || cancel.isPending;
  const result = detail.data;
  const active = result?.execution_status === "queued" || result?.execution_status === "running";
  const eligible = (result?.execution_status === undefined || result.execution_status === "imported" || result.execution_status === "completed") && !active && !detail.isError && result?.eligible === true && result.report !== undefined && result.revision === memory.revision && memory.status === "pending" && memory.expired !== true && result.adopted_revision === null;
  const failure = (err: unknown) => setError(err instanceof ApiError && err.status === 409 ? t(($) => $.tab_body.memory.evaluations.conflict) : t(($) => $.tab_body.memory.evaluations.operation_failed));
  const executionStatus = (status?: string) => {
    switch (status) {
      case "queued": return t(($) => $.tab_body.memory.connected.queued);
      case "running": return t(($) => $.tab_body.memory.connected.running);
      case "completed": return t(($) => $.tab_body.memory.connected.completed);
      case "failed": return t(($) => $.tab_body.memory.connected.execution_failed);
      case "cancelled": return t(($) => $.tab_body.memory.connected.cancelled);
      case undefined:
      case "imported": return t(($) => $.tab_body.memory.evaluations.imported);
      default: return t(($) => $.tab_body.memory.status_unknown);
    }
  };
  const reportStatus = (status: string) => {
    switch (status) {
      case "passed": return t(($) => $.tab_body.memory.evaluations.passed);
      case "failed": return t(($) => $.tab_body.memory.evaluations.failed);
      case "error": return t(($) => $.tab_body.memory.evaluations.execution_error);
      default: return t(($) => $.tab_body.memory.evaluations.not_run);
    }
  };
  const splitLabel = (split: string) => {
    switch (split) {
      case "holdout": return t(($) => $.tab_body.memory.evaluations.holdout);
      case "replay": return t(($) => $.tab_body.memory.evaluations.replay);
      default: return t(($) => $.tab_body.memory.status_unknown);
    }
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
    <DialogContent className="sm:max-w-2xl">
      <DialogHeader><DialogTitle>{t(($) => $.tab_body.memory.evaluations.title)}</DialogTitle><DialogDescription>{t(($) => $.tab_body.memory.evaluations.trust_hint)}</DialogDescription></DialogHeader>
      {error && <p role="alert" className="text-body text-destructive">{error}</p>}
      <div className="max-h-[60vh] space-y-4 overflow-y-auto text-body">
        {!selected && <>
          {memory.status === "pending" && memory.expired !== true && <MemoryExecutionForm wsId={wsId} agentId={agentId} memory={memory} disabled={busy || (list.data?.length ?? 0) >= 10 || list.data?.some((item) => item.execution_status === "queued" || item.execution_status === "running") === true} onStarted={(id) => { setSelected(id); setReviewed(false); setError(null); }} />}
          <label className="block space-y-2">
            <span className="font-medium">{t(($) => $.tab_body.memory.evaluations.import_action)}</span>
            <input type="file" accept=".json,application/json" disabled={busy || (list.data?.length ?? 0) >= 10} className="block w-full min-w-0 rounded-md border p-2 text-caption" onChange={async (event) => {
              const file = event.target.files?.[0]; event.target.value = "";
              if (!file || busy) return;
              setError(null);
              if (file.size > 2 * 1024 * 1024) { setError(t(($) => $.tab_body.memory.evaluations.invalid_file)); return; }
              setReading(true);
              try { const report: unknown = JSON.parse(await file.text()); await upload.mutateAsync(report); }
              catch (err) { if (err instanceof SyntaxError) setError(t(($) => $.tab_body.memory.evaluations.invalid_file)); else failure(err); }
              finally { setReading(false); }
            }} />
            <span className="block text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.import_hint)}</span>
          </label>
          {list.isPending && <p>{t(($) => $.tab_body.memory.loading)}</p>}
          {list.isError && <div role="alert"><p>{t(($) => $.tab_body.memory.load_failed)}</p><Button variant="outline" onClick={() => void list.refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button></div>}
          {!list.isPending && !list.isError && list.data?.length === 0 && <p className="rounded-lg border border-dashed p-6 text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.empty)}</p>}
          {list.data?.map((item) => <div key={item.id} className="space-y-2 rounded-lg border p-3">
            <div className="flex flex-wrap items-center gap-2"><Badge variant="outline">{executionStatus(item.execution_status)}</Badge><span>{t(($) => $.tab_body.memory.version_label, { revision: item.revision })}</span><span className="text-caption text-muted-foreground">{new Date(item.created_at).toLocaleString()}</span></div>
            <p>{t(($) => $.tab_body.memory.evaluations.score, { baseline: item.baseline_passed, candidate: item.candidate_passed, total: item.total })}</p>
            <p className="text-caption text-muted-foreground">{item.adopted_revision ? t(($) => $.tab_body.memory.evaluations.adopted, { revision: item.adopted_revision }) : item.revision !== memory.revision ? t(($) => $.tab_body.memory.evaluations.outdated) : item.eligible ? t(($) => $.tab_body.memory.evaluations.eligible) : t(($) => $.tab_body.memory.evaluations.not_eligible)}</p>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => { setSelected(item.id); setReviewed(false); setConfirmDelete(false); setError(null); }}>{t(($) => $.tab_body.memory.evaluations.inspect)}</Button>
          </div>)}
        </>}
        {selected && <>
          {detail.isPending && <p>{t(($) => $.tab_body.memory.loading)}</p>}
          {detail.isError && <div role="alert"><p>{t(($) => $.tab_body.memory.load_failed)}</p><Button variant="outline" onClick={() => void detail.refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button></div>}
          {result?.report && <>
            <Badge variant="outline">{executionStatus(result.execution_status)}</Badge>
            <p className="whitespace-pre-wrap break-words">{result.report.candidate.content}</p>
            <p>{t(($) => $.tab_body.memory.evaluations.score, { baseline: result.baseline_passed, candidate: result.candidate_passed, total: result.total })}</p>
            <p>{t(($) => $.tab_body.memory.evaluations.regressions, { count: result.regressions, errors: result.errors })}</p>
            {result.cost_status === "estimated" && result.estimated_cost_usd != null && (
              <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.estimated_cost, { amount: result.estimated_cost_usd.toFixed(4) })}</p>
            )}
            {result.cost_status === "partial" && (
              <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.partial_cost, { amount: result.estimated_cost_usd != null ? result.estimated_cost_usd.toFixed(4) : "—" })}</p>
            )}
            {(result.cost_status === undefined || result.cost_status === "unavailable") && (
              <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.unknown_cost)}</p>
            )}
            {result.report_hash && <p className="break-all text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.report_hash_receipt)}: {result.report_hash}</p>}
            <details className="rounded-lg border p-3"><summary className="cursor-pointer font-medium">{t(($) => $.tab_body.memory.evaluations.execution)}</summary><pre className="mt-2 whitespace-pre-wrap break-all text-caption">{JSON.stringify(result.report.suite, null, 2)}</pre></details>
            {result.report.cases.map((item, index) => <details key={`${item.id}-${index}`} className="rounded-lg border p-3">
              <summary className="cursor-pointer break-words font-medium">{item.id} · {splitLabel(item.split)}</summary>
              {(["baseline", "candidate"] as const).map((variant) => <div key={variant} className="mt-3 space-y-2">
                <p className="font-medium">{variant === "baseline" ? t(($) => $.tab_body.memory.evaluations.baseline) : t(($) => $.tab_body.memory.evaluations.candidate)} · {reportStatus(item[variant].status)} · {t(($) => $.tab_body.memory.evaluations.duration, { duration: item[variant].duration_ms })}</p>
                <pre className="max-h-64 overflow-y-auto whitespace-pre-wrap break-all rounded bg-muted p-2 text-caption">{item[variant].artifact}</pre>
                <pre className="max-h-64 overflow-y-auto whitespace-pre-wrap break-all text-caption text-muted-foreground">{item[variant].diagnostic}</pre>
                {item[variant].runtime && <details><summary className="cursor-pointer text-caption">{t(($) => $.tab_body.memory.evaluations.runtime_observations)}</summary><pre className="mt-2 max-h-64 overflow-y-auto whitespace-pre-wrap break-all text-caption">{JSON.stringify(item[variant].runtime, null, 2)}</pre></details>}
              </div>)}
              <p className="mt-3 break-all text-caption text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.fingerprints)}: {item.input_hash} / {item.checks_hash}</p>
            </details>)}
            {active && <p>{t(($) => $.tab_body.memory.connected.cancel_hint)}</p>}
            {eligible && <label className="flex items-start gap-2 rounded-lg border p-3"><input type="checkbox" checked={reviewed} disabled={busy} onChange={(event) => setReviewed(event.target.checked)} className="mt-1" /><span>{t(($) => $.tab_body.memory.evaluations.review_confirmation)}</span></label>}
            {!eligible && <p className="text-muted-foreground">{t(($) => $.tab_body.memory.evaluations.unavailable_adoption)}</p>}
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" disabled={busy} onClick={() => {
                const blob = new Blob([JSON.stringify(result.report, null, 2)], { type: "application/json" });
                const url = URL.createObjectURL(blob); const link = document.createElement("a"); link.href = url; link.download = `memory-evaluation-${result.id}.json`; link.click(); URL.revokeObjectURL(url);
              }}>{t(($) => $.tab_body.memory.evaluations.export_action)}</Button>
              <Button variant="outline" disabled={busy || active} onClick={() => setConfirmDelete(true)}>{t(($) => $.tab_body.memory.evaluations.delete_action)}</Button>
            </div>
            {confirmDelete && <div className="space-y-2 rounded-lg border p-3"><p>{t(($) => $.tab_body.memory.evaluations.delete_hint)}</p><div className="flex flex-wrap gap-2"><Button variant="destructive" disabled={busy} onClick={() => { setError(null); remove.mutate(selected, { onSuccess: () => { setSelected(""); setConfirmDelete(false); }, onError: failure }); }}>{t(($) => $.tab_body.memory.evaluations.delete_confirm)}</Button><Button variant="ghost" disabled={busy} onClick={() => setConfirmDelete(false)}>{t(($) => $.tab_body.memory.dialog_cancel)}</Button></div></div>}
          </>}
        </>}
      </div>
      <DialogFooter>
        {active && <Button variant="outline" disabled={busy} onClick={() => cancel.mutate(selected, { onError: failure })}>{t(($) => $.tab_body.memory.connected.cancel)}</Button>}
        <Button variant="ghost" disabled={busy} onClick={() => { if (selected) { setSelected(""); setError(null); } else onClose(); }}>{selected ? t(($) => $.tab_body.memory.evaluations.back) : t(($) => $.tab_body.memory.dialog_cancel)}</Button>
        {selected && eligible && <Button disabled={busy || !reviewed} onClick={() => {
          if (!result || busy || !reviewed) return;
          setError(null);
          adopt.mutate({ memoryId: memory.id, status: "active", expected_revision: result.revision, evaluation_id: result.id }, { onSuccess: onClose, onError: failure });
        }}>{t(($) => $.tab_body.memory.evaluations.adopt_action)}</Button>}
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
