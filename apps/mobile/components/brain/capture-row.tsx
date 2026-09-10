/**
 * One row of the capture inbox.
 *
 * Information density mirrors what the contract makes available, in the
 * order a person scans it: kind glyph, headline (the line the note will be
 * titled after — see `captureHeadline`), body, then the metadata line
 * (origin · age · status when not raw). An image capture leads with a
 * thumbnail because a photo of a whiteboard is unreadable as a filename.
 *
 * The transcription chip and the suggestion title are the two "the server is
 * still working" signals: a voice memo lands before its transcript exists,
 * and a suggestion arrives asynchronously once a model has read the capture.
 * Both must be visible in the list or the user re-taps a row that has
 * nothing new to show.
 */
import { Pressable, View } from "react-native";
import { Image } from "expo-image";
import { Ionicons } from "@expo/vector-icons";
import type { BrainCapture } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import {
  captureHeadline,
  captureKindIcon,
  captureKindLabel,
  captureOriginLabel,
  captureStatusLabel,
  captureSubline,
  captureTranscriptionChip,
} from "@/lib/brain-display";
import { timeAgo } from "@/lib/time-ago";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

export function CaptureRow({
  capture,
  onPress,
}: {
  capture: BrainCapture;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const headline = captureHeadline(capture);
  const subline = captureSubline(capture);
  const transcription = captureTranscriptionChip(capture.transcription_status);
  const thumbnail =
    capture.kind === "image"
      ? resolveAttachmentUrl(
          capture.attachment?.download_url || capture.attachment?.url || "",
        )
      : null;

  const meta = [
    captureKindLabel(capture.kind),
    captureOriginLabel(capture.origin),
    capture.created_at ? timeAgo(capture.created_at) : null,
    capture.status === "raw" ? null : captureStatusLabel(capture.status),
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${captureKindLabel(capture.kind)}: ${headline}`}
      className="flex-row gap-3 border-b border-border px-4 py-3 active:bg-secondary/50"
    >
      {thumbnail ? (
        <Image
          source={{ uri: thumbnail }}
          style={{ width: 44, height: 44, borderRadius: 6 }}
          contentFit="cover"
          accessibilityLabel={capture.attachment?.filename ?? "Photo"}
        />
      ) : (
        <View className="size-11 items-center justify-center rounded-md bg-secondary">
          <Ionicons
            name={captureKindIcon(capture.kind)}
            size={20}
            color={theme.mutedForeground}
          />
        </View>
      )}

      <View className="min-w-0 flex-1 gap-0.5">
        <Text
          className="text-sm font-medium text-foreground"
          numberOfLines={2}
        >
          {headline}
        </Text>
        {subline ? (
          <Text className="text-xs text-muted-foreground" numberOfLines={2}>
            {subline}
          </Text>
        ) : null}

        <View className="flex-row flex-wrap items-center gap-1">
          <Text className="text-xs text-muted-foreground">{meta}</Text>
          {transcription ? <Chip>{transcription}</Chip> : null}
        </View>

        {capture.suggestion?.title ? (
          <View className="flex-row items-center gap-1">
            <Ionicons
              name="sparkles-outline"
              size={12}
              color={theme.mutedForeground}
            />
            <Text
              className="min-w-0 flex-1 text-xs text-muted-foreground"
              numberOfLines={1}
            >
              {capture.suggestion.title}
            </Text>
          </View>
        ) : null}
      </View>
    </Pressable>
  );
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <View className="rounded bg-secondary px-1.5 py-0.5">
      <Text className="text-xs text-foreground">{children}</Text>
    </View>
  );
}
