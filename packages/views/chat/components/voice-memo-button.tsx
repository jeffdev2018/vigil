"use client";

import { Mic, Square } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useVoiceMemo, type VoiceMemoError } from "../../voice";
import { useT } from "../../i18n";

/**
 * Microphone toggle for the chat composer: one press records, the next one
 * stops and transcribes. Sits beside the attachment menu, same ghost icon
 * treatment. Hidden entirely by the caller when the server has no
 * transcription provider.
 */
export function VoiceMemoButton({
  onText,
  disabled,
}: {
  onText: (text: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("chat");
  const { phase, start, stop } = useVoiceMemo({
    onText,
    onError: (error: VoiceMemoError) => {
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

  const label =
    phase === "recording"
      ? t(($) => $.voice.stop)
      : phase === "transcribing"
        ? t(($) => $.voice.transcribing)
        : t(($) => $.voice.start);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={label}
            aria-pressed={phase === "recording"}
            disabled={disabled || phase === "transcribing"}
            onClick={() => (phase === "recording" ? stop() : void start())}
            className={cn(
              "text-muted-foreground hover:text-foreground",
              phase === "recording" && "text-destructive hover:text-destructive",
            )}
          />
        }
      >
        {phase === "transcribing" ? (
          <Spinner className="size-3.5" />
        ) : phase === "recording" ? (
          <span className="relative flex size-3.5 items-center justify-center">
            <span
              aria-hidden="true"
              className="absolute inset-0 animate-pulse rounded-full bg-destructive/25"
            />
            <Square className="size-2.5 fill-current" />
          </span>
        ) : (
          <Mic className="size-3.5" />
        )}
      </TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}
