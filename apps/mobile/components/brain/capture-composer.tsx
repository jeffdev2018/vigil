/**
 * The Brain capture composer — the "capture first, organize later" gesture
 * on a phone.
 *
 * Shape: a bottom bar that sticks to the keyboard, same container choice as
 * the shared message composer (`components/composer/message-composer.tsx`),
 * because this is a chat-style "type and send" surface, not a form. It is
 * NOT the shared composer itself: that one owns mentions, reply targets and
 * a Stop button, none of which a capture has, and it submits comment/chat
 * bodies rather than the two different endpoints a capture uses.
 *
 * Four ways in, all through modules already installed:
 *
 *   - text        → POST /api/brain/captures {content}
 *   - a lone URL  → the same endpoint with {url}, so the server files it as
 *                   kind "link" (`lonelyHttpUrl` makes the same inference
 *                   `inferBrainCaptureKind` does server-side)
 *   - to do       → the same endpoint with {kind: "todo"}
 *   - photo/file  → POST /api/brain/captures/upload, multipart
 *
 * Photo offers camera or library through `ActionSheetIOS` — rung 1 of the
 * iOS-native-first waterfall in apps/mobile/CLAUDE.md, no hand-rolled sheet.
 *
 * NOT here, and why: in-app voice-memo RECORDING. No audio module is
 * installed (`expo-av` / `expo-audio` are both absent from
 * apps/mobile/package.json) and adding one is a new native dependency plus a
 * dev-client rebuild, which apps/mobile/CLAUDE.md Lesson 1 does not let a
 * feature PR do on its own. This is the same call the voice-dictated issue
 * draft (K36, app/(app)/[workspace]/new-issue-voice.tsx) already made: the
 * iOS keyboard's own mic key dictates into the text field. An audio file
 * picked through "File" still becomes an audio capture — the server derives
 * the kind from the content type and queues transcription — so the audio
 * path exists, only the in-app recorder does not.
 *
 * Failure handling is the pending-message pattern from the root CLAUDE.md,
 * not optimism: a send renders immediately as a pending chip with its own
 * label, and a failure turns that chip into an inline error with a Retry.
 * Nothing is ever written into the cache before the server answers, and
 * nothing fails silently — the chip stays until it succeeds or the user
 * dismisses it.
 */
import { useCallback, useState } from "react";
import {
  ActionSheetIOS,
  Alert,
  Platform,
  Pressable,
  View,
} from "react-native";
import { KeyboardStickyView } from "react-native-keyboard-controller";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Ionicons } from "@expo/vector-icons";
import * as DocumentPicker from "expo-document-picker";
import * as Haptics from "expo-haptics";
import * as ImagePicker from "expo-image-picker";
import { Text } from "@/components/ui/text";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { IconButton } from "@/components/ui/icon-button";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { type FileAsset } from "@/data/api";
import {
  useCreateBrainCapture,
  useUploadBrainCapture,
} from "@/data/mutations/brain";
import { apiErrorMessage } from "@/lib/issue-goal-display";
import { lonelyHttpUrl } from "@/lib/brain-display";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

/** Mirrors `brainCaptureMaxUpload` in server/internal/handler/brain_capture.go
 *  (50 MB), which is stricter than the 100 MB `/api/upload-file` ceiling that
 *  `MAX_FILE_SIZE` describes — so the phone refuses before the wire does. */
const MAX_CAPTURE_UPLOAD = 50 * 1024 * 1024;

/** Mirrors `brainCaptureMaxContentRunes`. */
const MAX_CONTENT_CHARS = 20000;

/** One in-flight (or failed) capture. `send` is kept so Retry re-runs the
 *  exact same request instead of asking the user to redo the gesture. */
interface PendingCapture {
  key: string;
  label: string;
  send: () => Promise<unknown>;
  error: string | null;
}

let pendingSeq = 0;

export function CaptureComposer() {
  const insets = useSafeAreaInsets();
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const [text, setText] = useState("");
  const [isTodo, setIsTodo] = useState(false);
  const [pending, setPending] = useState<PendingCapture[]>([]);
  const [picking, setPicking] = useState(false);

  const createCapture = useCreateBrainCapture();
  const uploadCapture = useUploadBrainCapture();

  /**
   * Run a capture request under a pending chip: visible while it flies, an
   * inline error with a Retry if it fails, gone once the server has it.
   */
  const run = useCallback(
    async (label: string, send: () => Promise<unknown>): Promise<void> => {
      const key = `pending-${++pendingSeq}`;
      setPending((list) => [...list, { key, label, send, error: null }]);
      try {
        await send();
        setPending((list) => list.filter((item) => item.key !== key));
        void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      } catch (err) {
        const message = apiErrorMessage(err, "Could not capture that.");
        setPending((list) =>
          list.map((item) =>
            item.key === key ? { ...item, error: message } : item,
          ),
        );
      }
    },
    [],
  );

  const retry = useCallback(
    async (key: string) => {
      const item = pending.find((candidate) => candidate.key === key);
      if (!item) return;
      setPending((list) =>
        list.map((candidate) =>
          candidate.key === key ? { ...candidate, error: null } : candidate,
        ),
      );
      try {
        await item.send();
        setPending((list) => list.filter((candidate) => candidate.key !== key));
      } catch (err) {
        const message = apiErrorMessage(err, "Could not capture that.");
        setPending((list) =>
          list.map((candidate) =>
            candidate.key === key ? { ...candidate, error: message } : candidate,
          ),
        );
      }
    },
    [pending],
  );

  const dismiss = useCallback((key: string) => {
    setPending((list) => list.filter((item) => item.key !== key));
  }, []);

  const onSend = useCallback(async () => {
    const body = text.trim();
    if (body === "") return;
    if (body.length > MAX_CONTENT_CHARS) {
      Alert.alert(
        "Too long to capture",
        `A capture holds at most ${MAX_CONTENT_CHARS} characters. Shorten it or save it as a note.`,
      );
      return;
    }
    // A pasted link on its own is a link capture, not a text one — the note
    // it becomes then leads with the URL. A link with words around it stays
    // text, because the words are the point.
    const url = isTodo ? null : lonelyHttpUrl(body);
    const input = url
      ? { url }
      : { content: body, ...(isTodo ? { kind: "todo" as const } : {}) };

    // Clear the field up front so the next capture can be typed while this
    // one flies. Nothing is restored into it on failure — the chip owns both
    // the text and the retry; restoring as well would leave the user two
    // copies of the same capture to send.
    setText("");
    setIsTodo(false);
    await run(body, () => createCapture.mutateAsync(input));
  }, [createCapture, isTodo, run, text]);

  const upload = useCallback(
    async (asset: FileAsset & { size?: number }, label: string) => {
      if (asset.size != null && asset.size > MAX_CAPTURE_UPLOAD) {
        Alert.alert(
          "File too large",
          "A capture holds files up to 50 MB. Attach it to an issue instead.",
        );
        return;
      }
      // The typed text rides along as the file's caption. Read it into the
      // closure and clear the field before the request, so the chip's retry
      // replays the same caption even if the user keeps typing.
      const caption = text.trim() || undefined;
      setText("");
      await run(label, () =>
        uploadCapture.mutateAsync({
          asset: { uri: asset.uri, name: asset.name, type: asset.type },
          content: caption,
        }),
      );
    },
    [run, text, uploadCapture],
  );

  const pickFromLibrary = useCallback(async () => {
    const result = await ImagePicker.launchImageLibraryAsync({
      // SDK 55: `MediaTypeOptions.Images` is still supported (the
      // deprecation lands in SDK 56+). Same call as components/editor/
      // use-file-attach.ts — keep the two in step.
      mediaTypes: ImagePicker.MediaTypeOptions.Images,
      quality: 1,
    });
    if (result.canceled) return;
    const picked = result.assets[0];
    if (!picked) return;
    await upload(
      {
        uri: picked.uri,
        name: picked.fileName ?? `photo-${Date.now()}.jpg`,
        type: picked.mimeType ?? "image/jpeg",
        size: picked.fileSize,
      },
      picked.fileName ?? "Photo",
    );
  }, [upload]);

  const takePhoto = useCallback(async () => {
    const permission = await ImagePicker.requestCameraPermissionsAsync();
    if (!permission.granted) {
      Alert.alert(
        "Camera access needed",
        "Allow camera access in iOS Settings to capture a photo straight into the Brain.",
      );
      return;
    }
    const result = await ImagePicker.launchCameraAsync({ quality: 1 });
    if (result.canceled) return;
    const picked = result.assets[0];
    if (!picked) return;
    await upload(
      {
        uri: picked.uri,
        name: picked.fileName ?? `photo-${Date.now()}.jpg`,
        type: picked.mimeType ?? "image/jpeg",
        size: picked.fileSize,
      },
      "Photo",
    );
  }, [upload]);

  const onPhoto = useCallback(() => {
    if (Platform.OS !== "ios") {
      // Only iOS ships the native action sheet; elsewhere go straight to the
      // library rather than hand-rolling a chooser.
      void pickFromLibrary();
      return;
    }
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: ["Take photo", "Choose from library", "Cancel"],
        cancelButtonIndex: 2,
      },
      (index) => {
        if (index === 0) void takePhoto();
        if (index === 1) void pickFromLibrary();
      },
    );
  }, [pickFromLibrary, takePhoto]);

  const onFile = useCallback(async () => {
    setPicking(true);
    try {
      const result = await DocumentPicker.getDocumentAsync({
        type: "*/*",
        copyToCacheDirectory: true,
      });
      if (result.canceled) return;
      const picked = result.assets[0];
      if (!picked) return;
      await upload(
        {
          uri: picked.uri,
          name: picked.name,
          type: picked.mimeType ?? "application/octet-stream",
          size: picked.size,
        },
        picked.name,
      );
    } finally {
      setPicking(false);
    }
  }, [upload]);

  const busy = picking || createCapture.isPending || uploadCapture.isPending;
  const canSend = text.trim() !== "";

  return (
    <KeyboardStickyView offset={{ closed: 0, opened: insets.bottom }}>
      <View
        className="border-t border-border bg-background px-3 pt-2"
        style={{ paddingBottom: insets.bottom > 0 ? insets.bottom : 8 }}
      >
        {pending.map((item) => (
          <PendingChip
            key={item.key}
            item={item}
            onRetry={() => void retry(item.key)}
            onDismiss={() => dismiss(item.key)}
          />
        ))}

        <View className="flex-row items-end gap-1">
          <AutosizeTextArea
            value={text}
            onChangeText={setText}
            placeholder={
              isTodo ? "Something to do…" : "Capture a thought, a link, a task…"
            }
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            minHeight={40}
            maxHeight={140}
            className="flex-1"
            accessibilityLabel="Capture"
          />
          <IconButton
            name={isTodo ? "checkbox" : "checkbox-outline"}
            onPress={() => setIsTodo((value) => !value)}
            disabled={busy}
            accessibilityLabel={isTodo ? "Not a to do" : "Capture as a to do"}
            accessibilityState={{ selected: isTodo }}
            color={isTodo ? theme.primary : undefined}
          />
          <IconButton
            name="camera-outline"
            onPress={onPhoto}
            disabled={busy}
            accessibilityLabel="Capture a photo"
          />
          <IconButton
            name="attach-outline"
            onPress={() => void onFile()}
            disabled={busy}
            accessibilityLabel="Capture a file"
          />
          <IconButton
            name="arrow-up-circle"
            iconSize={28}
            onPress={() => void onSend()}
            disabled={!canSend || busy}
            accessibilityLabel="Capture"
            color={canSend ? theme.primary : theme.mutedForeground}
          />
        </View>

        {isTodo ? (
          <Text className="px-1 pt-1 text-xs text-muted-foreground">
            Filed as a to do — organize it into a note when you decide what it
            belongs to.
          </Text>
        ) : null}
      </View>
    </KeyboardStickyView>
  );
}

/**
 * A capture in flight, or one that failed. The failed state keeps the text
 * and the retry together so the gesture never has to be redone; dismissing
 * is an explicit act, so nothing disappears without the user saying so.
 */
function PendingChip({
  item,
  onRetry,
  onDismiss,
}: {
  item: PendingCapture;
  onRetry: () => void;
  onDismiss: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const failed = item.error !== null;

  return (
    <View
      className={`mb-2 gap-1 rounded-md px-2 py-1.5 ${
        failed ? "bg-destructive/10" : "bg-secondary"
      }`}
    >
      <View className="flex-row items-center gap-2">
        <Ionicons
          name={failed ? "alert-circle-outline" : "time-outline"}
          size={14}
          color={failed ? theme.destructive : theme.mutedForeground}
        />
        <Text
          className="min-w-0 flex-1 text-xs text-foreground"
          numberOfLines={2}
        >
          {item.label}
        </Text>
        {failed ? (
          <>
            <Pressable
              onPress={onRetry}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel="Retry this capture"
            >
              <Text className="text-xs font-semibold text-primary">Retry</Text>
            </Pressable>
            <Pressable
              onPress={onDismiss}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel="Discard this capture"
            >
              <Ionicons name="close" size={14} color={theme.mutedForeground} />
            </Pressable>
          </>
        ) : (
          <Text className="text-xs text-muted-foreground">Capturing…</Text>
        )}
      </View>
      {failed ? (
        <Text className="text-xs text-destructive">{item.error}</Text>
      ) : null}
    </View>
  );
}
