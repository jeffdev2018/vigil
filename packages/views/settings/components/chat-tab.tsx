"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useChatStore } from "@multica/core/chat";
import { useVoiceStore } from "@multica/core/voice/store";
import { VOICE_LANGUAGES, type VoiceLanguage } from "@multica/core/voice";
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
          <VoiceLanguageRow />
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

/**
 * The language dictation is transcribed in and replies are spoken in.
 * "Auto" follows the app locale for speech and leaves transcription to the
 * server default — which is the right answer for someone whose app language
 * and speaking language agree, and the wrong one for everybody else.
 */
function VoiceLanguageRow() {
  const { t } = useT("settings");
  const language = useVoiceStore((s) => s.voiceLanguage);
  const setLanguage = useVoiceStore((s) => s.setVoiceLanguage);
  const label = (value: VoiceLanguage) => t(($) => $.chat.voice_languages[value]);

  return (
    <SettingsRow
      label={t(($) => $.chat.voice_language_label)}
      description={t(($) => $.chat.voice_language_hint)}
    >
      <Select
        items={VOICE_LANGUAGES.map((value) => ({ value, label: label(value) }))}
        value={language}
        onValueChange={(value) => {
          if (!value) return;
          setLanguage(value as VoiceLanguage);
          toast.success(t(($) => $.auto_save.toast_saved), {
            id: "settings-auto-save",
          });
        }}
      >
        <SelectTrigger
          className="w-40"
          aria-label={t(($) => $.chat.voice_language_label)}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {VOICE_LANGUAGES.map((value) => (
            <SelectItem key={value} value={value}>
              {label(value)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
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
