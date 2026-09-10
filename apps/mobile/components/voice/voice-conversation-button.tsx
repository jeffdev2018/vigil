/**
 * Conversation-mode control for the mobile chat composer. Same product
 * semantics as `packages/views/voice/voice-conversation-button.tsx`: tap to
 * open the mic, VAD cuts turns, each utterance is sent via `onUtterance`.
 * Errors surface with Alert (no Tooltip / toast on mobile).
 */
import { ActivityIndicator, Alert, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { Button } from "@/components/ui/button";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import {
  useVoiceConversation,
  type VoiceConversationError,
} from "./use-voice-conversation";

const IS_IOS = process.env.EXPO_OS === "ios";

function errorMessage(error: VoiceConversationError): string {
  switch (error) {
    case "unsupported":
      return "Voice conversation isn’t available on this device.";
    case "mic_denied":
      return "Microphone access is required for voice conversation. Enable it in Settings.";
    case "not_configured":
      return "Speech transcription isn’t configured on this server.";
    default:
      return "Couldn’t transcribe that turn. Try again.";
  }
}

export function VoiceConversationButton({
  onUtterance,
  disabled,
}: {
  onUtterance: (text: string) => void;
  disabled?: boolean;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { phase, start, stop } = useVoiceConversation({
    onUtterance,
    onError: (error) => {
      Alert.alert("Voice conversation", errorMessage(error));
    },
  });

  const active = phase !== "idle";
  const label =
    phase === "transcribing"
      ? "Transcribing…"
      : phase === "listening"
        ? "Stop conversation"
        : "Start conversation";

  return (
    <Button
      variant="ghost"
      size="icon"
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: !!disabled, selected: active }}
      onPress={() => {
        if (IS_IOS) {
          void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        }
        if (active) {
          stop();
          return;
        }
        void start();
      }}
      className="h-8 w-8"
    >
      {phase === "transcribing" ? (
        <ActivityIndicator size="small" color={theme.primary} />
      ) : phase === "listening" ? (
        <View className="relative h-3.5 w-3.5 items-center justify-center">
          <View
            className="absolute inset-0 rounded-full bg-primary/25"
            // Pulse is implied by listening state; keep a11y on the button.
          />
          <Ionicons name="stop" size={12} color={theme.primary} />
        </View>
      ) : (
        <Ionicons name="pulse" size={20} color={theme.mutedForeground} />
      )}
    </Button>
  );
}
