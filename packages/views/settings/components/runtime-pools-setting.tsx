"use client";

import { useState } from "react";
import { ArrowDown, ArrowUp, Layers, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import { moveInList, runtimePoolsOptions, useDeleteRuntimePool, useSaveRuntimePool, type RuntimePool } from "@multica/core/runtimes/pools";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Runtime pools (K28): ordered families of interchangeable runtimes an agent
 * fails over through, with an explicit degraded last resort. The order is
 * the preference; a pool still targeted by an agent cannot be deleted.
 */
export function RuntimePoolsSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: pools = [] } = useQuery(runtimePoolsOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const save = useSaveRuntimePool(wsId);
  const remove = useDeleteRuntimePool(wsId);
  const [creating, setCreating] = useState(false);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.pools_failed));

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Layers className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.pools_section)}
        </span>
      }
    >
      <p className="mb-3 text-caption text-muted-foreground">{t(($) => $.workspace.pools_intro)}</p>
      <div className="flex flex-col gap-3">
        {pools.map((p) => (
          <PoolCard
            key={p.id}
            pool={p}
            runtimes={runtimes}
            canEdit={canEdit && !save.isPending}
            onSave={(input) => save.mutate({ id: p.id, input }, { onError: fail })}
            onDelete={() => remove.mutate(p.id, { onError: fail })}
          />
        ))}
        {creating ? (
          <PoolCard
            pool={{ id: "", name: "", runtime_ids: [], degraded_runtime_id: null, agent_count: 0, created_at: "" }}
            runtimes={runtimes}
            canEdit={canEdit}
            isNew
            onSave={(input) => save.mutate({ input }, { onError: fail, onSuccess: () => setCreating(false) })}
            onDelete={() => setCreating(false)}
          />
        ) : (
          canEdit && (
            <Button type="button" size="sm" variant="outline" className="self-start" onClick={() => setCreating(true)}>
              {t(($) => $.workspace.pools_new)}
            </Button>
          )
        )}
      </div>
    </SettingsSection>
  );
}

function PoolCard({
  pool, runtimes, canEdit, isNew = false, onSave, onDelete,
}: {
  pool: RuntimePool; runtimes: AgentRuntime[]; canEdit: boolean; isNew?: boolean;
  onSave: (input: { name: string; runtime_ids: string[]; degraded_runtime_id: string | null }) => void; onDelete: () => void;
}) {
  const { t } = useT("settings");
  const [name, setName] = useState(pool.name);
  const [ids, setIds] = useState<string[]>(pool.runtime_ids);
  const [degraded, setDegraded] = useState<string | null>(pool.degraded_runtime_id);
  const byId = new Map(runtimes.map((r) => [r.id, r]));
  const label = (id: string) => {
    const r = byId.get(id);
    return r ? runtimeDisplayLabel(r) : id.slice(0, 8);
  };
  const available = runtimes.filter((r) => !ids.includes(r.id) && r.id !== degraded);
  const dirty = name !== pool.name || ids.join(",") !== pool.runtime_ids.join(",") || degraded !== pool.degraded_runtime_id;
  const commit = () => onSave({ name: name.trim(), runtime_ids: ids, degraded_runtime_id: degraded });

  return (
    <SettingsCard>
      <div data-testid="runtime-pool-card" data-pool={pool.name} className="flex flex-col gap-2 text-caption">
        <div className="flex items-center gap-2">
          <Input aria-label={t(($) => $.workspace.pools_name)} placeholder={t(($) => $.workspace.pools_name)} className="w-56" value={name} disabled={!canEdit} onChange={(e) => setName(e.target.value)} />
          {!isNew && pool.agent_count > 0 && <span className="text-muted-foreground">{t(($) => $.workspace.pools_agents, { count: pool.agent_count })}</span>}
          {canEdit && (
            <Button type="button" size="icon-sm" variant="ghost" className="ml-auto text-muted-foreground hover:text-destructive" aria-label={t(($) => $.workspace.pools_delete, { name: pool.name })} onClick={onDelete}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
        <ol className="flex flex-col gap-1">
          {ids.map((id, i) => (
            <li key={id} className="flex items-center gap-2 rounded-md border border-border px-2 py-1">
              <span className="w-5 font-mono text-muted-foreground">{i + 1}.</span>
              <span className="min-w-0 flex-1 truncate">{label(id)}</span>
              <span className={byId.get(id)?.status === "online" ? "text-success" : "text-muted-foreground"}>{byId.get(id)?.status ?? "?"}</span>
              {canEdit && (
                <>
                  <button type="button" aria-label={t(($) => $.workspace.pools_move_up, { name: label(id) })} disabled={i === 0} className="text-muted-foreground hover:text-foreground disabled:opacity-30" onClick={() => setIds(moveInList(ids, id, -1))}><ArrowUp className="h-3.5 w-3.5" /></button>
                  <button type="button" aria-label={t(($) => $.workspace.pools_move_down, { name: label(id) })} disabled={i === ids.length - 1} className="text-muted-foreground hover:text-foreground disabled:opacity-30" onClick={() => setIds(moveInList(ids, id, 1))}><ArrowDown className="h-3.5 w-3.5" /></button>
                  <button type="button" aria-label={t(($) => $.workspace.pools_remove, { name: label(id) })} className="text-muted-foreground hover:text-destructive" onClick={() => setIds(ids.filter((x) => x !== id))}>×</button>
                </>
              )}
            </li>
          ))}
        </ol>
        <div className="flex flex-wrap items-center gap-2">
          {canEdit && (
            <select aria-label={t(($) => $.workspace.pools_add)} className="rounded-md border border-input bg-transparent px-2 py-1" value="" onChange={(e) => e.target.value && setIds([...ids, e.target.value])}>
              <option value="">{t(($) => $.workspace.pools_add)}</option>
              {available.map((r) => <option key={r.id} value={r.id}>{runtimeDisplayLabel(r)}</option>)}
            </select>
          )}
          <label className="flex items-center gap-1 text-muted-foreground">
            {t(($) => $.workspace.pools_degraded)}
            <select aria-label={t(($) => $.workspace.pools_degraded_aria, { name: pool.name || name })} className="rounded-md border border-input bg-transparent px-2 py-1" value={degraded ?? ""} disabled={!canEdit} onChange={(e) => setDegraded(e.target.value || null)}>
              <option value="">{t(($) => $.workspace.pools_degraded_none)}</option>
              {runtimes.filter((r) => !ids.includes(r.id)).map((r) => <option key={r.id} value={r.id}>{runtimeDisplayLabel(r)}</option>)}
            </select>
          </label>
          {canEdit && (dirty || isNew) && (
            <Button type="button" size="sm" disabled={name.trim() === "" || (ids.length === 0 && !degraded)} onClick={commit}>
              {t(($) => $.workspace.pools_save)}
            </Button>
          )}
        </div>
        <p className="text-micro text-muted-foreground">{t(($) => $.workspace.pools_degraded_hint)}</p>
      </div>
    </SettingsCard>
  );
}
