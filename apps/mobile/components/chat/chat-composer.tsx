/**
 * Chat composer — thin wrapper around the shared `<MessageComposer>` with
 * chat-specific wiring:
 *
 *   - **Controlled text**: parent (chat.tsx) owns the draft via
 *     `useChatDraftsStore` so switching sessions rehydrates the right
 *     draft. Pass `value` + `onChangeText` through.
 *   - **Stop button**: while an agent task is running for the active
 *     session, `sending` flips true and we replace the Send button slot
 *     with a Stop affordance (filled foreground bg + stop glyph). Tap →
 *     `onStop()` cancels the in-flight task.
 *   - **Mention picker mode=chat**: chat is user ↔ single agent so
 *     @member / @agent / @squad / @all are noise + would notify the
 *     wrong people. Picker route honors `?mode=chat` and surfaces only
 *     Issues (useful for "reference this ticket for context").
 *   - **No reply target**: chat is a flat conversation; passes no
 *     reply chip.
 *   - **No upload context**: chat attachments are session-scoped; the
 *     server back-fills `chat_message_id` on each row when the message
 *     persists (server-side). `MessageComposer` calls `api.uploadFile`
 *     without `{ issueId, commentId }`.
 *   - **Parent owns keyboard**: chat.tsx wraps in KeyboardAvoidingView +
 *     SafeAreaView, so `manageKeyboard={false}` prevents the composer
 *     from double-stacking its own keyboard handling.
 *
 * Previously a hand-written 400-LOC twin of inline-comment-composer.tsx;
 * now ~50 LOC plus the StopButton subcomponent.
 */
import { useCallback } from "react";
import { Pressable, View } from "react-native";
import Animated, { FadeIn, FadeOut } from "react-native-reanimated";
import * as Haptics from "expo-haptics";
import { useQuery } from "@tanstack/react-query";
import { MessageComposer } from "@/components/composer/message-composer";
import { AgentRunNoticeLine, RunNoticeBox } from "@/components/issue/comment-run-notice";
import { VoiceConversationButton } from "@/components/voice/voice-conversation-button";
import { appConfigOptions } from "@/data/queries/billing";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  /** Current draft text (controlled). Empty string = no draft. */
  value: string;
  /** Fired on every keystroke. The caller writes to the drafts store. */
  onChangeText: (next: string) => void;
  /** Send the serialised markdown content + the completed attachments'
   *  server ids. Caller resets the input by setting `value=""` after a
   *  successful send. */
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  /** Cancel the in-flight agent task. Only callable while `sending===true`. */
  onStop: () => void;
  /** True while an agent task is running for the active session. The
   *  composer swaps Send for Stop. */
  sending: boolean;
  /** Queued tasks remain busy, but do not expose Stop without draft restore. */
  allowStop?: boolean;
  /** Hard-disable typing + send. Used when there's no usable agent in the
   *  workspace or the session is archived (legacy). */
  disabled?: boolean;
  /** When `disabled`, replaces the pill label with the reason. */
  disabledReason?: string;
  /** Agent a send will run, when the conversation has not started yet: the
   *  composer says so (runtime, cost) before the first send. */
  runNoticeAgent?: { id: string; name: string } | null;
}

const IS_IOS = process.env.EXPO_OS === "ios";

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  sending,
  allowStop = true,
  disabled = false,
  disabledReason,
  runNoticeAgent,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  // Same gate as web chat-input: hide conversation when STT is not configured.
  // Mirrors `useConfigStore.meetingTranscriptionAvailable`.
  const { data: config } = useQuery(appConfigOptions());
  const voiceEnabled = config?.meeting_transcription_available === true;

  const onSubmit = useCallback(
    async ({
      content,
      attachmentIds,
    }: {
      content: string;
      attachmentIds: string[];
    }) => {
      // `onSend` may be sync or async; await is safe in both cases. If it
      // throws, MessageComposer's catch restores text + chips.
      await onSend(content, attachmentIds);
    },
    [onSend],
  );

  const handleStop = useCallback(() => {
    if (IS_IOS) {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    }
    onStop();
  }, [onStop]);

  const handleUtterance = useCallback(
    (text: string) => {
      // Mirror web VoiceConversationButton: join any typed draft, then send
      // through the normal chat path.
      const joined = value.trim() ? `${value.replace(/\s+$/, "")} ${text}` : text;
      onChangeText(joined);
      // Await + catch instead of a bare `void onSend(...)`: onSend
      // (chat.tsx's handleSend) already Alerts the user and rolls back
      // its own optimistic cache on failure, but a fire-and-forget call
      // here left that rejection unhandled. The catch is a no-op beyond
      // that — the dictated draft is never cleared on failure (only a
      // successful send clears it), so it stays visible for retry, the
      // same outcome MessageComposer's typed-send path restores to.
      void (async () => {
        try {
          await onSend(joined, []);
        } catch {
          // Handled by onSend itself; swallow here so nothing rethrows.
        }
      })();
    },
    [value, onChangeText, onSend],
  );

  return (
    <MessageComposer
      value={value}
      onChangeText={onChangeText}
      onSubmit={onSubmit}
      mentionPickerPath={{
        pathname: "/[workspace]/mention-picker",
        params: { workspace: wsSlug ?? "", mode: "chat" },
      }}
      placeholder={sending ? "Agent is working…" : "Message…"}
      pillLabel={
        sending
          ? "Agent is working…"
          : disabled
            ? (disabledReason ?? "Chat unavailable")
            : "Message…"
      }
      pillIcon="chatbubble-ellipses-outline"
      disabled={disabled}
      disabledReason={disabledReason}
      isSending={sending}
      renderStop={allowStop ? () => <StopButton onPress={handleStop} /> : undefined}
      toolbarExtras={
        voiceEnabled ? (
          <VoiceConversationButton
            disabled={!!disabled}
            onUtterance={handleUtterance}
          />
        ) : null
      }
      manageKeyboard={false}
      renderNotice={
        runNoticeAgent && !disabled
          ? () => (
              <RunNoticeBox>
                <AgentRunNoticeLine agentId={runNoticeAgent.id} name={runNoticeAgent.name} />
              </RunNoticeBox>
            )
          : undefined
      }
    />
  );
}

function StopButton({ onPress }: { onPress: () => void }) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  return (
    <Animated.View
      key="stop"
      entering={FadeIn.duration(120)}
      exiting={FadeOut.duration(120)}
    >
      <Pressable
        onPress={onPress}
        className="h-8 w-8 items-center justify-center rounded-full bg-foreground active:opacity-80"
        hitSlop={12}
        accessibilityRole="button"
        accessibilityLabel="Stop agent"
      >
        <View
          style={{
            width: 10,
            height: 10,
            backgroundColor: theme.background,
            borderRadius: 1.5,
          }}
        />
      </Pressable>
    </Animated.View>
  );
}
