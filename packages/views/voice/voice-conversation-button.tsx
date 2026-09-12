"use client";

import { AudioLines, Square } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useVoiceConversation, type VoiceConversationError } from "./use-voice-conversation";
import { useT } from "../i18n";

/**
 * Conversation mode for the chat composer: the microphone stays open, each
 * detected turn is transcribed and sent through `onUtterance`, replies are
 * read back (the send path arms that), and talking over a reply stops it.
 * Sits beside the single-memo mic; hidden by the same transcription gate.
 */
export function VoiceConversationButton({
  onUtterance,
  disabled,
}: {
  onUtterance: (text: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("chat");
  const { phase, start, stop } = useVoiceConversation({
    onUtterance,
    onError: (error: VoiceConversationError) => {
      toast.error(
        error === "unsupported"
          ? t(($) => $.voice.error_unsupported)
          : error === "mic_denied"
            ? t(($) => $.voice.error_mic_denied)
            : error === "not_configured"
              ? t(($) => $.voice.error_not_configured)
              : t(($) => $.voice.error_failed),
      );
    },
  });

  const active = phase !== "idle";
  const label = active
    ? phase === "transcribing"
      ? t(($) => $.voice.conversation_transcribing)
      : t(($) => $.voice.conversation_stop)
    : t(($) => $.voice.conversation_start);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={label}
            aria-pressed={active}
            disabled={disabled}
            onClick={() => {
              if (active) {
                stop();
                return;
              }
              void start();
            }}
            className={cn(
              "text-muted-foreground hover:text-foreground",
              phase === "listening" && "text-primary hover:text-primary",
            )}
          />
        }
      >
        {phase === "transcribing" ? (
          <Spinner className="size-3.5" />
        ) : phase === "listening" ? (
          <span className="relative flex size-3.5 items-center justify-center">
            <span
              aria-hidden="true"
              className="absolute inset-0 animate-pulse rounded-full bg-primary/25"
            />
            <Square className="size-2.5 fill-current" />
          </span>
        ) : (
          <AudioLines className="size-3.5" />
        )}
      </TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}
