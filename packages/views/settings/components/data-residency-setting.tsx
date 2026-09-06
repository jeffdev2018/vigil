"use client";

import { useState } from "react";
import { Globe2, X } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  dataResidencyOptions,
  isResidencyRestrictive,
  normalizeResidencyTokens,
  useSaveDataResidencyPolicy,
  type DataResidencyPolicy,
  type DataResidencySettings,
} from "@multica/core/residency";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Data residency (K46). Where this workspace's work may run. An empty policy
 * is the normal state, not a misconfiguration, so the empty view is a neutral
 * banner rather than a warning — and the one real warning here is that the
 * declarations behind the policy are operator statements nobody verifies.
 */
export function DataResidencySetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(dataResidencyOptions(wsId));
  const save = useSaveDataResidencyPolicy(wsId);
  const max = settings?.max_list_length ?? 20;

  const commit = (next: DataResidencyPolicy) =>
    save.mutate(next, {
      onError: (e: unknown) =>
        toast.error(
          e instanceof Error && e.message ? e.message : t(($) => $.workspace.data_residency_failed),
        ),
    });

  const policyOf = (s: DataResidencySettings | undefined): DataResidencyPolicy => ({
    region_allowlist: s?.region_allowlist ?? [],
    banned_providers: s?.banned_providers ?? [],
    require_on_prem: s?.require_on_prem === true,
  });

  const restrictive = isResidencyRestrictive(settings);

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Globe2 className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.data_residency_section)}
        </span>
      }
      description={t(($) => $.workspace.data_residency_description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.data_residency_regions)}
          description={t(($) => $.workspace.data_residency_regions_description)}
          align="start"
        >
          <TokenField
            label={t(($) => $.workspace.data_residency_regions)}
            placeholder={t(($) => $.workspace.data_residency_regions_placeholder)}
            tokens={settings?.region_allowlist ?? []}
            max={max}
            disabled={!canEdit || save.isPending}
            onChange={(tokens) => commit({ ...policyOf(settings), region_allowlist: tokens })}
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.data_residency_providers)}
          description={t(($) => $.workspace.data_residency_providers_description)}
          align="start"
        >
          <TokenField
            label={t(($) => $.workspace.data_residency_providers)}
            placeholder={t(($) => $.workspace.data_residency_providers_placeholder)}
            tokens={settings?.banned_providers ?? []}
            max={max}
            disabled={!canEdit || save.isPending}
            onChange={(tokens) => commit({ ...policyOf(settings), banned_providers: tokens })}
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.data_residency_on_prem)}
          description={t(($) => $.workspace.data_residency_on_prem_description)}
        >
          <Switch
            aria-label={t(($) => $.workspace.data_residency_on_prem)}
            checked={settings?.require_on_prem === true}
            disabled={!canEdit || save.isPending}
            onCheckedChange={(checked: boolean) =>
              commit({ ...policyOf(settings), require_on_prem: checked })
            }
          />
        </SettingsRow>
      </SettingsCard>

      {restrictive ? (
        <p role="note" className="px-0.5 text-caption leading-5 text-warning">
          {t(($) => $.workspace.data_residency_unverified)}
        </p>
      ) : (
        <p role="note" className="px-0.5 text-caption leading-5 text-muted-foreground">
          {t(($) => $.workspace.data_residency_empty)}
        </p>
      )}
    </SettingsSection>
  );
}

/**
 * A chips input. Entries are normalized (trimmed, lowercased, deduplicated,
 * sorted) exactly as the server does, so what the user sees after typing is
 * what the policy will compare against — and the cap is enforced here rather
 * than bounced back as a 400.
 */
function TokenField({
  label,
  placeholder,
  tokens,
  max,
  disabled,
  onChange,
}: {
  label: string;
  placeholder: string;
  tokens: string[];
  max: number;
  disabled: boolean;
  onChange: (tokens: string[]) => void;
}) {
  const { t } = useT("settings");
  const [draft, setDraft] = useState("");
  const full = tokens.length >= max;

  const add = () => {
    const next = normalizeResidencyTokens([...tokens, draft], max);
    setDraft("");
    if (next.length !== tokens.length || next.some((v, i) => v !== tokens[i])) onChange(next);
  };

  return (
    <div className="space-y-2">
      {tokens.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {tokens.map((token) => (
            <li key={token}>
              <Badge variant="secondary" className="gap-1 font-mono">
                {token}
                {!disabled && (
                  <button
                    type="button"
                    aria-label={t(($) => $.workspace.data_residency_remove, { token })}
                    onClick={() => onChange(tokens.filter((v) => v !== token))}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3 w-3" />
                  </button>
                )}
              </Badge>
            </li>
          ))}
        </ul>
      )}
      {!disabled && (
        <div className="flex gap-2">
          <Input
            aria-label={label}
            placeholder={placeholder}
            className="font-mono"
            value={draft}
            disabled={full}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== "Enter") return;
              e.preventDefault();
              add();
            }}
          />
          <Button type="button" size="sm" variant="secondary" disabled={full || !draft.trim()} onClick={add}>
            {t(($) => $.workspace.data_residency_add)}
          </Button>
        </div>
      )}
    </div>
  );
}
