"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FlaskConical, Play, Save, Square, ArrowUpRight } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useSkillStudioStore, saveStudioInput, useRunSkillTest, skillStudioResultOptions, useCancelSkillTest } from "@multica/core/skills/studio";
import type { Skill } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

export function SkillStudio({ skill, dirty }: { skill: Skill; dirty: boolean }) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const user = useAuthStore(s => s.user);
  const key = `${wsId}:${user?.id ?? ""}:${skill.id}`;
  const notebook = useSkillStudioStore(s => s.draft.notebooks[key]);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [inputId, setInputId] = useState("");
  const [inputName, setInputName] = useState("");
  const [input, setInput] = useState("");
  const [agentId, setAgentId] = useState("");
  const [sessionId, setSessionId] = useState<string>();
  const run = useRunSkillTest(wsId, key);
  const cancel = useCancelSkillTest();
  const selected = notebook?.runs.find(r => r.sessionId === sessionId) ?? notebook?.runs[0];
  const result = useQuery(skillStudioResultOptions(wsId, selected?.sessionId));
  const responses = result.data?.messages.filter(m => m.role === "assistant") ?? [];
  const pending = result.data?.pending;
  const ready = !dirty && !!agentId && !!input.trim() && !!skill.content.trim() && !run.isPending;
  const saveInput = () => { const id = inputId || crypto.randomUUID(); setInputId(id); saveStudioInput(key, { id, name: inputName.trim() || t($ => $.studio.untitled), text: input }); };
  const start = () => { saveInput(); run.mutate({ skillId: skill.id, expectedUpdatedAt: skill.updated_at, agentId, inputName: inputName.trim() || t($ => $.studio.untitled), input }, { onSuccess: r => setSessionId(r.sessionId) }); };
  return <section className="mx-auto max-w-[1440px] space-y-5 p-4 sm:p-6" aria-label={t($ => $.studio.title)}>
    <div className="flex items-start gap-3"><div className="rounded-lg bg-primary/10 p-3 text-primary"><FlaskConical className="size-5" /></div><div><h2 className="text-title font-semibold">{t($ => $.studio.title)}</h2><p className="mt-1 max-w-prose text-body text-muted-foreground">{t($ => $.studio.description)}</p></div></div>
    {dirty && <p role="status" className="rounded-lg bg-warning/10 p-3 text-caption text-warning">{t($ => $.studio.dirty)}</p>}
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
      <div className="space-y-4 rounded-xl border p-4">
        <label className="block space-y-1 text-caption">{t($ => $.studio.saved)}<select className="h-9 w-full rounded-md border bg-background px-2 text-body" value={inputId} onChange={e => { const item = notebook?.inputs.find(i => i.id === e.target.value); setInputId(e.target.value); setInputName(item?.name ?? ""); setInput(item?.text ?? ""); }}><option value="">{t($ => $.studio.new_input)}</option>{notebook?.inputs.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}</select></label>
        <label className="block space-y-1 text-caption">{t($ => $.studio.name)}<Input value={inputName} onChange={e => setInputName(e.target.value)} placeholder={t($ => $.studio.name_placeholder)} /></label>
        <label className="block space-y-1 text-caption">{t($ => $.studio.input)}<Textarea rows={9} value={input} onChange={e => setInput(e.target.value)} placeholder={t($ => $.studio.input_placeholder)} /></label>
        <div className="flex items-center gap-2"><Button size="sm" variant="outline" className="gap-2" disabled={!input.trim()} onClick={saveInput}><Save className="size-3.5" />{t($ => $.studio.save)}</Button><span className="text-caption text-muted-foreground">{t($ => $.studio.local)}</span></div>
        <label className="block space-y-1 text-caption">{t($ => $.studio.agent)}<select className="h-9 w-full rounded-md border bg-background px-2 text-body" value={agentId} onChange={e => setAgentId(e.target.value)}><option value="">{t($ => $.studio.choose_agent)}</option>{agents.filter(a => a.runtime_id && !a.archived_at).map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</select></label>
        <p className="text-caption text-muted-foreground">{t($ => $.studio.permissions)}</p>
        <Button className="w-full gap-2" disabled={!ready} onClick={start}><Play className="size-4" />{run.isPending ? t($ => $.studio.starting) : t($ => $.studio.run)}</Button>
        {run.error && <p role="alert" className="text-caption text-destructive">{run.error.message}</p>}
      </div>
      <div className="min-w-0 space-y-4">
        <section className="rounded-xl border p-4"><h3 className="mb-3 text-body font-semibold">{t($ => $.studio.history)}</h3>{!notebook?.runs.length ? <p className="text-caption text-muted-foreground">{t($ => $.studio.empty)}</p> : <div className="max-h-40 space-y-1 overflow-y-auto">{notebook.runs.map(r => <button key={r.sessionId} onClick={() => setSessionId(r.sessionId)} aria-pressed={selected?.sessionId === r.sessionId} className={`flex w-full flex-wrap justify-between gap-1 rounded-md px-3 py-2 text-left text-caption ${selected?.sessionId === r.sessionId ? "bg-primary/10 font-semibold text-primary" : "hover:bg-muted"}`}><span>{r.inputName} · {agents.find(a => a.id === r.agentId)?.name ?? r.agentId}</span><time>{new Date(r.startedAt).toLocaleString()}</time></button>)}</div>}</section>
        <section aria-live="polite" className="min-h-60 space-y-3 rounded-xl bg-muted/40 p-4"><div className="flex items-center justify-between"><h3 className="text-body font-semibold">{t($ => $.studio.result)}</h3>{pending?.task_id && <Button size="sm" variant="outline" className="gap-1" disabled={cancel.isPending} onClick={() => cancel.mutate(pending.task_id!, { onSuccess: () => void result.refetch() })}><Square className="size-3" />{t($ => $.studio.stop)}</Button>}</div>
          {selected && <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-caption text-muted-foreground">{t($ => $.studio.version, { date: new Date(selected.skillUpdatedAt).toLocaleString() })}</p><AppLink href={`${paths.chat()}?session=${encodeURIComponent(selected.sessionId)}`} className="inline-flex items-center gap-1 text-caption font-medium text-info underline-offset-4 hover:underline">{t($ => $.studio.conversation)}<ArrowUpRight className="size-3.5" /></AppLink></div>}
          {pending?.task_id && <p role="status" className="text-caption">{t($ => $.studio.running)} · {pending.status}</p>}
          {result.isError && <Button size="sm" variant="outline" onClick={() => void result.refetch()}>{t($ => $.studio.retry)}</Button>}
          {cancel.error && <p role="alert" className="text-caption text-destructive">{cancel.error.message}</p>}
          {!responses.length && !pending?.task_id && <p className="text-caption text-muted-foreground">{selected ? t($ => $.studio.no_result) : t($ => $.studio.result_empty)}</p>}
          {responses.map(m => <article key={m.id} className={`whitespace-pre-wrap break-words text-body ${m.failure_reason ? "text-destructive" : ""}`}>{m.content}</article>)}
        </section>
      </div>
    </div>
  </section>;
}
