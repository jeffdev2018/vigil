"use client";

import { useState } from "react";
import { ArrowDown, ArrowUp, ListOrdered, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { pipelineSquadsOptions, pipelinesOptions, useDeletePipeline, useSavePipeline, type Pipeline, type PipelineStageInput } from "@multica/core/pipelines";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

const TEMPLATE_STAGES = ["triage", "plan", "implement", "test", "review"];

/**
 * Pipelines (K37): ordered stages, each routed to an agent or a squad,
 * with an optional human gate. Stages of a pipeline with open runs cannot
 * change; the empty state offers the triage → plan → implement → test →
 * review template.
 */
export function PipelinesSetting({ canManage }: { canManage: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: pipelines = [] } = useQuery(pipelinesOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(pipelineSquadsOptions(wsId));
  const save = useSavePipeline(wsId);
  const remove = useDeletePipeline(wsId);
  const [editing, setEditing] = useState<Pipeline | "new" | "template" | null>(null);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.pipelines_failed));
  const executors = [
    ...agents.map((a) => ({ key: `agent:${a.id}`, type: "agent" as const, id: a.id, label: a.name })),
    ...squads.map((s) => ({ key: `squad:${s.id}`, type: "squad" as const, id: s.id, label: t(($) => $.workspace.pipelines_squad, { name: s.name }) })),
  ];
  const label = (type: string, id: string) => executors.find((e) => e.type === type && e.id === id)?.label ?? t(($) => $.workspace.pipelines_executor_gone);
  const templateStages = (): PipelineStageInput[] => {
    const first = executors[0];
    return first ? TEMPLATE_STAGES.map((name, i) => ({ name, executor_type: first.type, executor_id: first.id, requires_human_gate: i === 2 || i === 4 })) : [];
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ListOrdered className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.pipelines_section)}
        </span>
      }
    >
      <p className="mb-3 text-caption text-muted-foreground">{t(($) => $.workspace.pipelines_intro)}</p>
      <div className="flex flex-col gap-3">
        {pipelines.map((p) =>
          editing !== null && editing !== "new" && editing !== "template" && editing.id === p.id ? (
            <PipelineEditor key={p.id} initial={{ name: p.name, stages: p.stages }} executors={executors} locked={p.open_runs > 0} pending={save.isPending} onCancel={() => setEditing(null)} onSave={(input) => save.mutate({ id: p.id, input }, { onError: fail, onSuccess: () => setEditing(null) })} />
          ) : (
            <SettingsCard key={p.id}>
              <div data-testid="pipeline-card" data-pipeline={p.name} className="flex flex-col gap-2 text-caption">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{p.name}</span>
                  {p.open_runs > 0 && <span className="text-muted-foreground">{t(($) => $.workspace.pipelines_open_runs, { count: p.open_runs })}</span>}
                  {canManage && (
                    <span className="ml-auto flex items-center gap-1">
                      <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(p)}>{t(($) => $.workspace.pipelines_edit)}</Button>
                      <Button type="button" size="icon-sm" variant="ghost" className="text-muted-foreground hover:text-destructive" aria-label={t(($) => $.workspace.pipelines_delete, { name: p.name })} disabled={p.open_runs > 0} onClick={() => remove.mutate(p.id, { onError: fail })}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </span>
                  )}
                </div>
                <ol className="flex flex-wrap items-center gap-1">
                  {p.stages.map((s, i) => (
                    <li key={s.id} className="flex items-center gap-1">
                      {i > 0 && <span className="text-muted-foreground">→</span>}
                      <span className="rounded border border-border px-1.5 py-0.5">
                        {s.requires_human_gate && <span title={t(($) => $.workspace.pipelines_gate)} className="mr-1 text-warning">⏸</span>}
                        {s.name} <span className="text-muted-foreground">· {label(s.executor_type, s.executor_id)}</span>
                      </span>
                    </li>
                  ))}
                </ol>
              </div>
            </SettingsCard>
          ),
        )}
        {(editing === "new" || editing === "template") && (
          <PipelineEditor initial={{ name: "", stages: editing === "template" ? templateStages() : [] }} executors={executors} locked={false} pending={save.isPending} onCancel={() => setEditing(null)} onSave={(input) => save.mutate({ input }, { onError: fail, onSuccess: () => setEditing(null) })} />
        )}
        {canManage && editing === null && (
          <div className="flex gap-2">
            <Button type="button" size="sm" variant="outline" onClick={() => setEditing("new")}>{t(($) => $.workspace.pipelines_new)}</Button>
            {pipelines.length === 0 && executors.length > 0 && (
              <Button type="button" size="sm" variant="outline" onClick={() => setEditing("template")}>{t(($) => $.workspace.pipelines_template)}</Button>
            )}
          </div>
        )}
      </div>
    </SettingsSection>
  );
}

function PipelineEditor({ initial, executors, locked, pending, onSave, onCancel }: {
  initial: { name: string; stages: PipelineStageInput[] };
  executors: { key: string; type: "agent" | "squad"; id: string; label: string }[];
  locked: boolean; pending: boolean;
  onSave: (input: { name: string; stages?: PipelineStageInput[] }) => void; onCancel: () => void;
}) {
  const { t } = useT("settings");
  const [name, setName] = useState(initial.name);
  const [stages, setStages] = useState<PipelineStageInput[]>(initial.stages.map((s) => ({ name: s.name, executor_type: s.executor_type, executor_id: s.executor_id, requires_human_gate: s.requires_human_gate })));
  const update = (i: number, patch: Partial<PipelineStageInput>) => setStages(stages.map((s, n) => (n === i ? { ...s, ...patch } : s)));
  const move = (i: number, delta: -1 | 1) => {
    const j = i + delta;
    if (j < 0 || j >= stages.length) return;
    const next = [...stages];
    [next[i], next[j]] = [next[j] as PipelineStageInput, next[i] as PipelineStageInput];
    setStages(next);
  };
  const valid = name.trim() !== "" && (locked || (stages.length > 0 && stages.every((s) => s.name.trim() !== "" && s.executor_id !== "")));
  return (
    <SettingsCard>
      <form data-testid="pipeline-editor" className="flex flex-col gap-2 text-caption" onSubmit={(e) => { e.preventDefault(); if (valid) onSave(locked ? { name: name.trim() } : { name: name.trim(), stages }); }}>
        <Input aria-label={t(($) => $.workspace.pipelines_name)} placeholder={t(($) => $.workspace.pipelines_name)} className="w-64" value={name} onChange={(e) => setName(e.target.value)} />
        {locked && <p className="text-muted-foreground">{t(($) => $.workspace.pipelines_locked)}</p>}
        {!locked && (
          <ol className="flex flex-col gap-1">
            {stages.map((s, i) => (
              <li key={i} className="flex flex-wrap items-center gap-2 rounded-md border border-border px-2 py-1">
                <span className="w-5 font-mono text-muted-foreground">{i + 1}.</span>
                <Input aria-label={t(($) => $.workspace.pipelines_stage_name, { n: i + 1 })} className="w-40" value={s.name} onChange={(e) => update(i, { name: e.target.value })} />
                <select aria-label={t(($) => $.workspace.pipelines_stage_executor, { n: i + 1 })} className="rounded-md border border-input bg-transparent px-2 py-1" value={`${s.executor_type}:${s.executor_id}`} onChange={(e) => { const ex = executors.find((x) => x.key === e.target.value); if (ex) update(i, { executor_type: ex.type, executor_id: ex.id }); }}>
                  <option value="">{t(($) => $.workspace.pipelines_pick_executor)}</option>
                  {executors.map((ex) => <option key={ex.key} value={ex.key}>{ex.label}</option>)}
                </select>
                <label className="flex items-center gap-1 text-muted-foreground">
                  <Checkbox aria-label={t(($) => $.workspace.pipelines_stage_gate, { n: i + 1 })} checked={s.requires_human_gate} onCheckedChange={(v) => update(i, { requires_human_gate: v === true })} />
                  {t(($) => $.workspace.pipelines_gate)}
                </label>
                <span className="ml-auto flex items-center gap-1">
                  <button type="button" aria-label={t(($) => $.workspace.pipelines_move_up, { n: i + 1 })} disabled={i === 0} className="text-muted-foreground hover:text-foreground disabled:opacity-30" onClick={() => move(i, -1)}><ArrowUp className="h-3.5 w-3.5" /></button>
                  <button type="button" aria-label={t(($) => $.workspace.pipelines_move_down, { n: i + 1 })} disabled={i === stages.length - 1} className="text-muted-foreground hover:text-foreground disabled:opacity-30" onClick={() => move(i, 1)}><ArrowDown className="h-3.5 w-3.5" /></button>
                  <button type="button" aria-label={t(($) => $.workspace.pipelines_remove_stage, { n: i + 1 })} className="text-muted-foreground hover:text-destructive" onClick={() => setStages(stages.filter((_, n) => n !== i))}>×</button>
                </span>
              </li>
            ))}
          </ol>
        )}
        <div className="flex gap-2">
          {!locked && <Button type="button" size="sm" variant="outline" onClick={() => setStages([...stages, { name: "", executor_type: "agent", executor_id: "", requires_human_gate: false }])}>{t(($) => $.workspace.pipelines_add_stage)}</Button>}
          <Button type="submit" size="sm" disabled={pending || !valid}>{t(($) => $.workspace.pipelines_save)}</Button>
          <Button type="button" size="sm" variant="ghost" onClick={onCancel}>{t(($) => $.budgets.cancel)}</Button>
        </div>
      </form>
    </SettingsCard>
  );
}
