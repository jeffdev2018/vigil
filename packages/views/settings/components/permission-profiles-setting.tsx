"use client";

import { useEffect, useState } from "react";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  parseList,
  permissionProfilesOptions,
  useUpdatePermissionProfile,
  type PermissionProfile,
  type PermissionProfilePatch,
} from "@multica/core/agents/permission-profiles";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Permission profiles (K06): the rules behind each profile an agent can
 * run under. Five builtins are seeded by the server; their rules are
 * editable, their names are not.
 */
export function PermissionProfilesSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: profiles = [] } = useQuery(permissionProfilesOptions(wsId));
  const update = useUpdatePermissionProfile(wsId);
  const save = (id: string, patch: PermissionProfilePatch) =>
    update.mutate({ id, patch }, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.profiles_failed)) });

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.profiles_section)}
        </span>
      }
    >
      <p className="mb-3 text-caption text-muted-foreground">{t(($) => $.workspace.profiles_intro)}</p>
      <div className="flex flex-col gap-3">
        {profiles.map((p) => (
          <ProfileCard key={p.id} profile={p} canEdit={canEdit && !update.isPending} onSave={(patch) => save(p.id, patch)} />
        ))}
      </div>
    </SettingsSection>
  );
}

const LIST_FIELDS = ["denied_paths", "allowed_commands", "hidden_secrets"] as const;
type ListField = (typeof LIST_FIELDS)[number];

function ProfileCard({ profile, canEdit, onSave }: { profile: PermissionProfile; canEdit: boolean; onSave: (patch: PermissionProfilePatch) => void }) {
  const { t } = useT("settings");
  const [text, setText] = useState<Record<ListField, string>>({ denied_paths: "", allowed_commands: "", hidden_secrets: "" });
  const joined = LIST_FIELDS.map((f) => profile[f].join(", "));
  useEffect(() => {
    setText({ denied_paths: joined[0] ?? "", allowed_commands: joined[1] ?? "", hidden_secrets: joined[2] ?? "" });
    // Resync only when the server value changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [joined.join("\n")]);

  const commit = (field: ListField) => {
    const list = parseList(text[field]);
    setText((prev) => ({ ...prev, [field]: list.join(", ") }));
    if (list.join("\n") !== profile[field].join("\n")) onSave({ [field]: list });
  };
  const labels: Record<ListField, { label: string; description: string }> = {
    denied_paths: { label: t(($) => $.workspace.profiles_denied_paths), description: t(($) => $.workspace.profiles_denied_paths_description) },
    allowed_commands: { label: t(($) => $.workspace.profiles_allowed_commands), description: t(($) => $.workspace.profiles_allowed_commands_description) },
    hidden_secrets: { label: t(($) => $.workspace.profiles_hidden_secrets), description: t(($) => $.workspace.profiles_hidden_secrets_description) },
  };

  return (
    <SettingsCard>
      <div data-testid="permission-profile-card" data-profile={profile.name}>
        <SettingsRow label={<span className="font-mono">{profile.name}</span>} description={profile.description}>
          <label className="flex items-center gap-2 text-caption text-muted-foreground">
            <Switch
              aria-label={`${profile.name}: ${t(($) => $.workspace.profiles_read_only)}`}
              checked={profile.read_only}
              disabled={!canEdit}
              onCheckedChange={(v) => onSave({ read_only: v })}
            />
            {t(($) => $.workspace.profiles_read_only)}
          </label>
        </SettingsRow>
        {LIST_FIELDS.map((field) => (
          <SettingsRow key={field} label={labels[field].label} description={labels[field].description}>
            <Input
              aria-label={`${profile.name}: ${labels[field].label}`}
              className="w-72 font-mono"
              value={text[field]}
              disabled={!canEdit}
              onChange={(e) => setText((prev) => ({ ...prev, [field]: e.target.value }))}
              onBlur={() => commit(field)}
            />
          </SettingsRow>
        ))}
      </div>
    </SettingsCard>
  );
}
