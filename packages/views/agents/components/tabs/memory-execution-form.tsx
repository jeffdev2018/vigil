"use client";

import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { memoryExecutionConfigOptions, useStartMemoryExecution } from "@multica/core/agents";
import type { AgentMemory, MemoryExecutionRequest } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../../i18n";

export function MemoryExecutionForm({ wsId, agentId, memory, disabled, onStarted }: { wsId: string; agentId: string; memory: AgentMemory; disabled: boolean; onStarted: (id: string) => void }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const config = useQuery(memoryExecutionConfigOptions(wsId, agentId, memory.id, open));
  const start = useStartMemoryExecution(wsId, agentId, memory.id);
  const [cases, setCases] = useState<MemoryExecutionRequest["cases"]>([{ id: "replay", split: "replay", prompt: "", expected: "" }, { id: "holdout", split: "holdout", prompt: "", expected: "" }]);
  // Keep the request identity across an uncertain HTTP failure: retrying must
  // never charge for another run. Editing the inputs creates a new request.
  const request = useRef<{ signature: string; id: string } | null>(null);
  const submitting = useRef(false);
  const [error, setError] = useState(false);
  if (!open) return <Button variant="outline" disabled={disabled} onClick={() => setOpen(true)}>{t(($) => $.tab_body.memory.connected.open)}</Button>;
  const busy = disabled || start.isPending || memory.revision === undefined;
  return <div className="space-y-3 rounded-lg border p-3">
    <p className="font-medium">{t(($) => $.tab_body.memory.connected.title)}</p>
    {config.isPending && <p>{t(($) => $.tab_body.memory.loading)}</p>}
    {config.isError && <div role="alert" className="space-y-2"><p>{t(($) => $.tab_body.memory.connected.unavailable)}</p><Button variant="outline" onClick={() => void config.refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button></div>}
    {config.data && !config.isError && <>
      <p className="break-words">{config.data.provider} · {config.data.model}{config.data.effort ? ` · ${config.data.effort}` : ""}</p>
      <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.connected.scope)}</p>
      {cases.map((item, index) => <fieldset key={item.id} disabled={busy} className="space-y-2 rounded border p-3">
        <legend className="px-1 font-medium">{item.split === "replay" ? t(($) => $.tab_body.memory.evaluations.replay) : t(($) => $.tab_body.memory.evaluations.holdout)}</legend>
        <label className="block space-y-1"><span>{t(($) => $.tab_body.memory.connected.check)}</span><select aria-label={t(($) => $.tab_body.memory.connected.check)} className="block w-full rounded border bg-background p-2" value={item.check ?? "exact"} onChange={(event) => {
          const check = event.target.value;
          if (check === "exact" || check === "json" || check === "javascript") setCases((values) => values.map((value, i) => i === index ? { ...value, check } : value));
        }}>{(config.data.check_modes ?? ["exact"]).map((mode) => <option key={mode} value={mode}>{mode === "json" ? t(($) => $.tab_body.memory.connected.json) : mode === "javascript" ? t(($) => $.tab_body.memory.connected.javascript) : t(($) => $.tab_body.memory.connected.exact)}</option>)}</select></label>
        {item.check === "javascript" && <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.connected.javascript_hint)}</p>}
        <label className="block space-y-1"><span>{t(($) => $.tab_body.memory.connected.prompt)}</span><Textarea aria-label={t(($) => $.tab_body.memory.connected.prompt)} value={item.prompt} maxLength={2000} onChange={(event) => setCases((values) => values.map((value, i) => i === index ? { ...value, prompt: event.target.value } : value))} /></label>
        <label className="block space-y-1"><span>{t(($) => $.tab_body.memory.connected.expected)}</span><Textarea aria-label={t(($) => $.tab_body.memory.connected.expected)} value={item.expected} maxLength={2000} onChange={(event) => setCases((values) => values.map((value, i) => i === index ? { ...value, expected: event.target.value } : value))} /></label>
      </fieldset>)}
      <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.connected.check_hint)}</p>
      {error && <p role="alert" className="text-destructive">{t(($) => $.tab_body.memory.connected.failed)}</p>}
      <Button disabled={busy || cases.some((item) => !item.prompt.trim() || !item.expected.trim()) || cases[0]?.prompt.trim() === cases[1]?.prompt.trim()} onClick={async () => {
        if (submitting.current || !config.data || memory.revision === undefined) return;
        submitting.current = true; setError(false);
        const body = { cases, expected_revision: memory.revision, config_hash: config.data.config_hash };
        const signature = JSON.stringify(body);
        if (request.current?.signature !== signature) request.current = { signature, id: crypto.randomUUID() };
        try { const result = await start.mutateAsync({ ...body, request_id: request.current.id }); onStarted(result.id); }
        catch { setError(true); }
        finally { submitting.current = false; }
      }}>{start.isPending ? t(($) => $.tab_body.memory.connected.starting) : t(($) => $.tab_body.memory.connected.start)}</Button>
    </>}
  </div>;
}
