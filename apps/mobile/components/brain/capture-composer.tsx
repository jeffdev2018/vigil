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
 * Five ways in, all through modules already installed:
 *
 *   - text        → POST /api/brain/captures {content}
 *   - a lone URL  → the same endpoint with {url}, so the server files it as
 *                   kind "link" (`lonelyHttpUrl` makes the same inference
 *                   `inferBrainCaptureKind` does server-side)
 *   - to do       → the same endpoint with {kind: "todo"}
 *   - photo/file  → POST /api/brain/captures/upload, multipart
 *   - voice memo  → recorded with expo-audio, then the same upload route, so
 *                   the server files it as kind "audio" and transcribes it
 *                   (transcription_status pending → done/failed)
 *
 * Photo offers camera or library through `ActionSheetIOS` — rung 1 of the
 * iOS-native-first waterfall in apps/mobile/CLAUDE.md, no hand-rolled sheet.
 *
 * Recording follows components/voice/use-voice-conversation.ts (N20): the mic
 * permission is requested on the first tap, `setAudioModeAsync` is set before
 * `prepareToRecordAsync`, and the recorder is stopped on unmount so leaving
 * the screen never leaves the mic open. A denial is an inline message, not an
 * Alert — the user is mid-gesture and the answer is in Settings, not in a
 * modal. The one thing this composer does NOT do is voice-activity detection:
 * a memo is one deliberate take, started and stopped by hand, unlike the
 * duplex chat conversation that cuts turns at silence.
 *
 * Failure handling is the pending-message pattern from the root CLAUDE.md,
 * not optimism: a send renders immediately as a pending chip with its own
 * label, and a failure turns that chip into an inline error with a Retry.
 * Nothing is ever written into the cache before the server answers, and
 * nothing fails silently — the chip stays until it succeeds or the user
 * dismisses it.
 */
import { useCallback, useEffect, useState } from "react";
import {
  ActionSheetIOS,
  Alert,
  Platform,
  Pressable,
  View,
} from "react-native";
import { KeyboardStickyView } from "react-native-keyboard-controller";
import {
  RecordingPresets,
  requestRecordingPermissionsAsync,
  setAudioModeAsync,
  useAudioRecorder,
  useAudioRecorderState,
} from "expo-audio";
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
import { formatMediaClock, lonelyHttpUrl } from "@/lib/brain-display";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

/** Mirrors `brainCaptureMaxUpload` in server/internal/handler/brain_capture.go
 *  (50 MB), which is stricter than the 100 MB `/api/upload-file` ceiling that
 *  `MAX_FILE_SIZE` describes — so the phone refuses before the wire does. */
const MAX_CAPTURE_UPLOAD = 50 * 1024 * 1024;

/** Mirrors `brainCaptureMaxContentRunes`. */
const MAX_CONTENT_CHARS = 20000;

/** How often the elapsed clock ticks while recording. */
const RECORDER_TICK_MS = 500;

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
  /** Inline, not an Alert: the fix is in Settings and the user is mid-gesture. */
  const [micDenied, setMicDenied] = useState(false);
  const [recordError, setRecordError] = useState<string | null>(null);

  // No metering: a memo is one deliberate take, so there is no VAD to feed —
  // the state subscription exists only for the elapsed clock.
  const recorder = useAudioRecorder(RecordingPresets.HIGH_QUALITY);
  const recorderState = useAudioRecorderState(recorder, RECORDER_TICK_MS);

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
    // Server counts runes (utf8.RuneCountInString), not UTF-16 code units:
    // body.length over-counts astral characters (emoji etc, 2 units each),
    // which would refuse content the server would actually accept.
    if ([...body].length > MAX_CONTENT_CHARS) {
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

  const startRecording = useCallback(async () => {
    setRecordError(null);
    let permission;
    try {
      permission = await requestRecordingPermissionsAsync();
    } catch {
      setRecordError("This device cannot record audio.");
      return;
    }
    if (!permission.granted) {
      setMicDenied(true);
      return;
    }
    setMicDenied(false);
    try {
      // Order matters: the audio mode has to allow recording BEFORE the
      // recorder is prepared, or iOS prepares against the playback session
      // and `record()` no-ops. Same sequence as use-voice-conversation.ts.
      await setAudioModeAsync({ allowsRecording: true, playsInSilentMode: true });
      await recorder.prepareToRecordAsync();
      recorder.record();
    } catch {
      setRecordError("Could not start recording.");
    }
  }, [recorder]);

  /** `keep: false` is Cancel — stop the mic and drop the file on the floor. */
  const finishRecording = useCallback(
    async (opts: { keep: boolean }) => {
      if (!recorder.isRecording) return;
      try {
        await recorder.stop();
      } catch {
        setRecordError("Could not stop the recording.");
        return;
      }
      if (!opts.keep) return;
      const uri = recorder.uri;
      if (!uri) {
        setRecordError("The recording came back empty — nothing was captured.");
        return;
      }
      // Straight into the same upload path a picked file takes: the server
      // reads audio/* off the content type and queues the transcription.
      await upload(
        {
          uri,
          name: `voice-memo-${Date.now()}.m4a`,
          type: "audio/mp4",
        },
        "Voice memo",
      );
    },
    [recorder, upload],
  );

  // Never leave the mic open behind us: unmounting the inbox (tab switch,
  // navigating away) stops an in-flight recording and discards it.
  useEffect(
    () => () => {
      if (recorder.isRecording) void recorder.stop();
    },
    [recorder],
  );

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

  const recording = recorderState.isRecording;
  const busy =
    picking || recording || createCapture.isPending || uploadCapture.isPending;
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

        {recording ? (
          <View className="mb-2 flex-row items-center gap-2 rounded-md bg-destructive/10 px-2 py-1.5">
            <Ionicons name="mic" size={14} color={theme.destructive} />
            <Text className="flex-1 text-xs text-foreground">
              {`Recording ${formatMediaClock(recorderState.durationMillis / 1000)}`}
            </Text>
            <Pressable
              onPress={() => void finishRecording({ keep: false })}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel="Discard this recording"
            >
              <Text className="text-xs text-muted-foreground">Cancel</Text>
            </Pressable>
            <Pressable
              onPress={() => void finishRecording({ keep: true })}
              hitSlop={6}
              accessibilityRole="button"
              accessibilityLabel="Stop recording and capture it"
            >
              <Text className="text-xs font-semibold text-primary">Stop</Text>
            </Pressable>
          </View>
        ) : null}

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
            name={recording ? "stop-circle" : "mic-outline"}
            onPress={() =>
              void (recording
                ? finishRecording({ keep: true })
                : startRecording())
            }
            // Recording is the one action that stays live while `busy`.
            disabled={busy && !recording}
            accessibilityLabel={
              recording ? "Stop recording and capture it" : "Record a voice memo"
            }
            color={recording ? theme.destructive : undefined}
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

        {micDenied ? (
          <Text className="px-1 pt-1 text-xs text-destructive">
            Microphone access is off. Allow it for Multica in iOS Settings to
            record a voice memo — everything else still captures.
          </Text>
        ) : null}
        {recordError ? (
          <Text className="px-1 pt-1 text-xs text-destructive">
            {recordError}
          </Text>
        ) : null}
        {isTodo && !recording ? (
          <Text className="px-1 pt-1 text-xs text-muted-foreground">
            Filed as a to do — organize it into a note when you decide what it
            belongs to.
          </Text>
        ) : null}
        {recording ? (
          <Text className="px-1 pt-1 text-xs text-muted-foreground">
            Anything you type now rides along as the memo’s caption. The
            transcript arrives on its own once the server has read it.
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
