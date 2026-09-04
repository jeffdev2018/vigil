"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  permissionProfilesOptions,
  useSetAgentPermissionProfile,
} from "@multica/core/agents/permission-profiles";
import type { Agent } from "@multica/core/types";
import { PickerItem, PropertyPicker } from "../../../issues/components/pickers";
import { SettingsRow } from "../../../settings/components/settings-layout";
import { useT } from "../../../i18n";

/**
 * Permission profile (K06): which of the workspace's profiles this agent
 * runs under. None means the agent keeps its full access; the rules
 * themselves are edited in workspace settings.
 */
export function PermissionProfileField({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: profiles = [] } = useQuery(permissionProfilesOptions(wsId));
  const assign = useSetAgentPermissionProfile(wsId, agent.id);
  const [open, setOpen] = useState(false);
  const currentId = agent.permission_profile_id ?? null;
  const current = profiles.find((p) => p.id === currentId) ?? null;
  const label = current?.name ?? t(($) => $.pickers.permission_profile_none);
  const tooltip = t(($) => $.pickers.permission_profile_tooltip, { value: label });

  const select = (id: string | null) => {
    setOpen(false);
    if (id === currentId) return;
    assign.mutate(id, {
      onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.pickers.permission_profile_failed)),
    });
  };

  const display = (
    <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground">
      <ShieldCheck className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span className="min-w-0 truncate">{label}</span>
    </div>
  );

  return (
    <SettingsRow label={t(($) => $.inspector.prop_permission_profile)} description={current?.description} size="select-wide">
      <div data-testid="permission-profile-field" data-profile={currentId ?? ""}>
        {!canEdit ? (
          display
        ) : (
          <PropertyPicker
            open={open}
            onOpenChange={setOpen}
            width="w-[var(--anchor-width)] min-w-[14rem] max-w-md"
            align="start"
            tooltip={tooltip}
            triggerRender={
              <button
                type="button"
                className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-body transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                aria-label={tooltip}
              />
            }
            trigger={
              <>
                <ShieldCheck className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate">{label}</span>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`} aria-hidden="true" />
              </>
            }
          >
            {profiles.map((p) => (
              <PickerItem key={p.id} selected={p.id === currentId} onClick={() => select(p.id)}>
                <span className="block min-w-0 flex-1 text-left">
                  <span className="truncate text-label font-medium">
                    {p.name}
                    {p.read_only && <span className="ml-1 font-normal text-muted-foreground">· {t(($) => $.pickers.permission_profile_read_only)}</span>}
                  </span>
                  {p.description ? <span className="mt-0.5 block text-micro leading-snug text-muted-foreground">{p.description}</span> : null}
                </span>
              </PickerItem>
            ))}
            {currentId ? (
              <button
                type="button"
                onClick={() => select(null)}
                className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-caption text-muted-foreground transition-colors hover:bg-accent/50"
              >
                {t(($) => $.pickers.permission_profile_clear)}
              </button>
            ) : null}
          </PropertyPicker>
        )}
      </div>
    </SettingsRow>
  );
}
