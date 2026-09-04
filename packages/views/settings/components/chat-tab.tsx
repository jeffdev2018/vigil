"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import { useChatStore } from "@multica/core/chat";
import { useVoiceStore } from "@multica/core/voice/store";
import { toast } from "sonner";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Chat settings — its own tab under "My Account". Both switches are persisted
 * client preferences, so they apply immediately without a round-trip:
 *
 *  - the floating window: when off, the FAB / overlay never mount and Chat is
 *    reachable only from its dedicated tab;
 *  - reading replies aloud after a voice memo: when off, a dictated message
 *    behaves like a typed one and the per-message Listen button is the only
 *    way to hear a reply.
 *
 * Both are on by default.
 */
export function ChatTab() {
  const { t } = useT("settings");
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const setEnabled = useChatStore((s) => s.setFloatingChatEnabled);

  return (
    <SettingsTab title={t(($) => $.page.tabs.chat)}>
      <SettingsSection title={t(($) => $.chat.floating_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.chat.floating_label)}
            description={t(($) => $.chat.floating_hint)}
          >
          <Switch
            checked={enabled}
            onCheckedChange={(checked) => {
              setEnabled(checked);
              toast.success(t(($) => $.auto_save.toast_saved), {
                id: "settings-auto-save",
              });
            }}
            aria-label={t(($) => $.chat.floating_label)}
          />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection title={t(($) => $.chat.voice_title)}>
        <SettingsCard>
          <ReadRepliesAloudRow />
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

function ReadRepliesAloudRow() {
  const { t } = useT("settings");
  const enabled = useVoiceStore((s) => s.readRepliesAloud);
  const setEnabled = useVoiceStore((s) => s.setReadRepliesAloud);

  return (
    <SettingsRow
      label={t(($) => $.chat.read_aloud_label)}
      description={t(($) => $.chat.read_aloud_hint)}
    >
      <Switch
        checked={enabled}
        onCheckedChange={(checked) => {
          setEnabled(checked);
          toast.success(t(($) => $.auto_save.toast_saved), {
            id: "settings-auto-save",
          });
        }}
        aria-label={t(($) => $.chat.read_aloud_label)}
      />
    </SettingsRow>
  );
}
