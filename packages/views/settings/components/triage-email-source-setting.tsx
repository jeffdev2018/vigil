"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Mail } from "lucide-react";
import { toast } from "sonner";
import { triageStatsOptions, useCreateTriageEmailSource } from "@multica/core/triage";
import type { TriageEmailSource } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { WebhookUrlField } from "../../autopilots/components/webhook-url-field";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Email intake: one endpoint per workspace whose bearer token in the URL path
 * is the whole credential. Forwarded messages land in the triage queue and
 * wait for a human — email is the least authenticated material in the product,
 * so the source is created gated and there is no "make it direct" affordance
 * here on purpose.
 *
 * The token is in the create/rotate response and nowhere else (the server
 * keeps only its digest), so it is held in local state for exactly as long as
 * this screen is mounted and never refetched.
 */
export function TriageEmailSourceSetting({ wsId, canEdit }: { wsId: string; canEdit: boolean }) {
  const { t } = useT("settings");
  const [minted, setMinted] = useState<TriageEmailSource | null>(null);
  const create = useCreateTriageEmailSource(wsId);

  // Whether intake already exists is a property of the source list, which the
  // queue header already loads. `?? false` keeps the button labelled "enable"
  // while the list is loading — the safer wording of the two.
  const stats = useQuery({ ...triageStatsOptions(wsId), enabled: !!wsId });
  const alreadyEnabled = stats.data?.sources?.some((s) => s.kind === "email") === true;

  const run = async () => {
    if (create.isPending) return;
    try {
      const source = await create.mutateAsync();
      setMinted(source);
      toast.success(t(($) => $.workspace.triage_email_enabled_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.triage_email_failed));
    }
  };

  // Prefer the absolute URL; a deployment without a configured public URL
  // still gets a usable path behind whatever host serves it.
  const endpoint = minted ? (minted.url || minted.path) : "";

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Mail className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.triage_email_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.triage_email_enable)}
          description={t(($) => $.workspace.triage_email_description)}
        >
          <Button variant="outline" disabled={!canEdit || create.isPending} onClick={() => void run()}>
            {alreadyEnabled || minted
              ? t(($) => $.workspace.triage_email_rotate)
              : t(($) => $.workspace.triage_email_enable)}
          </Button>
        </SettingsRow>
        {minted && (
          <SettingsRow
            label={t(($) => $.workspace.triage_email_endpoint_label)}
            description={t(($) => $.workspace.triage_email_token_once)}
          >
            <div className="flex w-full min-w-0 flex-col gap-2">
              <WebhookUrlField url={endpoint} size="md" />
            </div>
          </SettingsRow>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}
