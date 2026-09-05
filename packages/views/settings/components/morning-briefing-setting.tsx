"use client";

import { useState } from "react";
import { Sunrise } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const MORNING_BRIEFING_SETTING_KEY = "morning_briefing";

export interface BriefingChannel {
  type: string;
  chat_id: string;
}

export interface MorningBriefingPolicy {
  enabled: boolean;
  hour: number;
  timezone: string;
  // Multichannel digest (K64): the chats the briefing is also posted to.
  channels: BriefingChannel[];
}

export const BRIEFING_CHANNEL_TYPES = ["slack", "telegram"] as const;

export function morningBriefingPolicy(workspace: Workspace | null | undefined): MorningBriefingPolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[MORNING_BRIEFING_SETTING_KEY] ?? {}) as Record<string, unknown>;
  const hour = typeof raw.hour === "number" && raw.hour >= 0 && raw.hour <= 23 ? raw.hour : 8;
  const channels = Array.isArray(raw.channels)
    ? (raw.channels as unknown[]).flatMap((c) => {
        const r = (c ?? {}) as Record<string, unknown>;
        return typeof r.type === "string" && typeof r.chat_id === "string" ? [{ type: r.type, chat_id: r.chat_id }] : [];
      })
    : [];
  return { enabled: raw.enabled === true, hour, timezone: typeof raw.timezone === "string" && raw.timezone !== "" ? raw.timezone : "UTC", channels };
}

/**
 * Morning briefing (K30): whether the workspace gets its daily digest, at
 * which local hour, and a way to send today's now. Stored in
 * workspace.settings like the other workspace policies.
 */
export function MorningBriefingSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = morningBriefingPolicy(workspace);
  const [hour, setHour] = useState(String(policy.hour));
  const [timezone, setTimezone] = useState(policy.timezone);
  const [saving, setSaving] = useState(false);
  const [sending, setSending] = useState(false);
  const [channels, setChannels] = useState<BriefingChannel[]>(policy.channels);

  async function persist(next: MorningBriefingPolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [MORNING_BRIEFING_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.briefing_failed));
    } finally {
      setSaving(false);
    }
  }

  const commit = () => {
    const h = Math.min(23, Math.max(0, Math.floor(Number(hour) || 0)));
    setHour(String(h));
    const tz = timezone.trim() || "UTC";
    setTimezone(tz);
    if (h === policy.hour && tz === policy.timezone) return;
    void persist({ ...policy, hour: h, timezone: tz });
  };

  const commitChannels = (next: BriefingChannel[]) => {
    setChannels(next);
    const clean = next.filter((c) => c.chat_id.trim() !== "").map((c) => ({ type: c.type, chat_id: c.chat_id.trim() }));
    if (JSON.stringify(clean) === JSON.stringify(policy.channels)) return;
    void persist({ ...policy, channels: clean });
  };

  async function sendNow() {
    setSending(true);
    try {
      const b = await api.triggerMorningBriefing();
      toast.success(b.already_sent ? t(($) => $.workspace.briefing_already_sent) : t(($) => $.workspace.briefing_sent));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.briefing_failed));
    } finally {
      setSending(false);
    }
  }

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Sunrise className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.briefing_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.briefing_enabled_label)} description={t(($) => $.workspace.briefing_enabled_description)}>
          <Switch
            id="morning-briefing-enabled"
            aria-label={t(($) => $.workspace.briefing_enabled_label)}
            checked={policy.enabled}
            disabled={!canEdit || saving}
            onCheckedChange={(v) => void persist({ ...policy, enabled: v === true })}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.briefing_hour_label)} description={t(($) => $.workspace.briefing_hour_description)}>
          <div className="flex items-center gap-2">
            <Input type="number" min={0} max={23} aria-label={t(($) => $.workspace.briefing_hour_label)} className="w-20" value={hour} disabled={!canEdit || saving} onChange={(e) => setHour(e.target.value)} onBlur={commit} />
            {/* eslint-disable-next-line no-restricted-syntax -- an IANA zone name is a technical value, not copy */}
            <Input aria-label={t(($) => $.workspace.briefing_timezone_label)} placeholder="Europe/Paris" className="w-44" value={timezone} disabled={!canEdit || saving} onChange={(e) => setTimezone(e.target.value)} onBlur={commit} />
          </div>
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.briefing_channels_label)} description={t(($) => $.workspace.briefing_channels_description)}>
          <div data-testid="briefing-channels" className="flex flex-col gap-1.5">
            {channels.map((c, i) => (
              <div key={i} className="flex items-center gap-1">
                <select aria-label={t(($) => $.workspace.briefing_channel_type, { n: i + 1 })} className="rounded-md border border-input bg-transparent px-2 py-1" value={c.type} disabled={!canEdit || saving} onChange={(e) => commitChannels(channels.map((x, n) => (n === i ? { ...x, type: e.target.value } : x)))}>
                  {BRIEFING_CHANNEL_TYPES.map((ty) => <option key={ty} value={ty}>{ty}</option>)}
                </select>
                {/* eslint-disable-next-line no-restricted-syntax -- a platform chat id is a technical value, not copy */}
                <Input aria-label={t(($) => $.workspace.briefing_channel_chat, { n: i + 1 })} placeholder="C0123ABC / -1001234" className="w-44" value={c.chat_id} disabled={!canEdit || saving} onChange={(e) => setChannels(channels.map((x, n) => (n === i ? { ...x, chat_id: e.target.value } : x)))} onBlur={() => commitChannels(channels)} />
                <button type="button" aria-label={t(($) => $.workspace.briefing_channel_remove, { n: i + 1 })} className="text-muted-foreground hover:text-destructive" disabled={!canEdit || saving} onClick={() => commitChannels(channels.filter((_, n) => n !== i))}>×</button>
              </div>
            ))}
            <Button type="button" size="sm" variant="outline" className="self-start" disabled={!canEdit || saving} onClick={() => setChannels([...channels, { type: "slack", chat_id: "" }])}>
              {t(($) => $.workspace.briefing_channel_add)}
            </Button>
          </div>
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.briefing_send_label)} description={t(($) => $.workspace.briefing_send_description)}>
          <Button type="button" size="sm" variant="outline" disabled={!canEdit || sending} onClick={() => void sendNow()}>
            {t(($) => $.workspace.briefing_send_now)}
          </Button>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
