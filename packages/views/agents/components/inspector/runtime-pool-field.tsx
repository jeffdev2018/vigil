"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Layers } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimePoolsOptions, useSetAgentRuntimePool } from "@multica/core/runtimes/pools";
import type { Agent } from "@multica/core/types";
import { PickerItem, PropertyPicker } from "../../../issues/components/pickers";
import { SettingsRow } from "../../../settings/components/settings-layout";
import { useT } from "../../../i18n";

/** Runtime pool (K28): the family of runtimes this agent fails over through. */
export function RuntimePoolField({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: pools = [] } = useQuery(runtimePoolsOptions(wsId));
  const assign = useSetAgentRuntimePool(wsId, agent.id);
  const [open, setOpen] = useState(false);
  const currentId = agent.runtime_pool_id ?? null;
  const current = pools.find((p) => p.id === currentId) ?? null;
  const label = current?.name ?? t(($) => $.pickers.runtime_pool_none);
  const tooltip = t(($) => $.pickers.runtime_pool_tooltip, { value: label });
  const select = (id: string | null) => {
    setOpen(false);
    if (id === currentId) return;
    assign.mutate(id, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.pickers.runtime_pool_failed)) });
  };
  const display = (
    <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground">
      <Layers className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span className="min-w-0 truncate">{label}</span>
    </div>
  );
  return (
    <SettingsRow label={t(($) => $.inspector.prop_runtime_pool)} description={current ? t(($) => $.pickers.runtime_pool_description, { count: current.runtime_ids.length }) : undefined} size="select-wide">
      <div data-testid="runtime-pool-field" data-pool={currentId ?? ""}>
        {!canEdit || pools.length === 0 ? (
          display
        ) : (
          <PropertyPicker
            open={open}
            onOpenChange={setOpen}
            width="w-[var(--anchor-width)] min-w-[14rem] max-w-md"
            align="start"
            tooltip={tooltip}
            triggerRender={
              <button type="button" className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-body transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50" aria-label={tooltip} />
            }
            trigger={
              <>
                <Layers className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate">{label}</span>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`} aria-hidden="true" />
              </>
            }
          >
            {pools.map((p) => (
              <PickerItem key={p.id} selected={p.id === currentId} onClick={() => select(p.id)}>
                <span className="block min-w-0 flex-1 text-left">
                  <span className="truncate text-label font-medium">{p.name}</span>
                  <span className="mt-0.5 block text-micro leading-snug text-muted-foreground">{t(($) => $.pickers.runtime_pool_description, { count: p.runtime_ids.length })}</span>
                </span>
              </PickerItem>
            ))}
            {currentId ? (
              <button type="button" onClick={() => select(null)} className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-caption text-muted-foreground transition-colors hover:bg-accent/50">
                {t(($) => $.pickers.runtime_pool_clear)}
              </button>
            ) : null}
          </PropertyPicker>
        )}
      </div>
    </SettingsRow>
  );
}
