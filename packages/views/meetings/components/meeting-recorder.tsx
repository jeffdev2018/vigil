"use client";

import { useEffect, useState } from "react";
import { Square } from "lucide-react";
import {
  requestStopRecording,
  useMeetingRecorderStore,
} from "@multica/core/meetings/store";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/** `mm:ss`, growing past 60 minutes rather than wrapping. */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

/**
 * Ticking elapsed time since `startedAt`. Null while nothing is recording, so
 * an idle shell runs no interval.
 */
export function useElapsed(startedAt: string | null): string | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!startedAt) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [startedAt]);
  if (!startedAt) return null;
  const started = Date.parse(startedAt);
  return formatElapsed(Number.isNaN(started) ? 0 : now - started);
}

/** Red dot that pulses only while audio is actually being captured. */
export function RecordingDot({ live = true }: { live?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "size-2 shrink-0 rounded-full bg-red-500",
        live && "animate-pulse",
      )}
    />
  );
}

/**
 * The live recorder, shown on a meeting's detail page while it records. The
 * recording itself lives in `useMeetingRecorder`, mounted once in the shell —
 * this panel only reads the store and asks it to stop.
 */
export function MeetingRecorderPanel() {
  const { t } = useT("meetings");
  const phase = useMeetingRecorderStore((s) => s.phase);
  const startedAt = useMeetingRecorderStore((s) => s.startedAt);
  const systemAudio = useMeetingRecorderStore((s) => s.systemAudio);
  const lastTranscript = useMeetingRecorderStore((s) => s.lastTranscript);
  const elapsed = useElapsed(startedAt);
  const finishing = phase === "finishing";

  return (
    <section className="flex flex-col gap-3 rounded-lg border p-3">
      <div className="flex items-center gap-2">
        <RecordingDot live={!finishing} />
        <span className="text-body font-medium">
          {finishing ? t(($) => $.recorder.stopping) : t(($) => $.recorder.title)}
        </span>
        {elapsed ? (
          <span className="font-mono text-caption tabular-nums text-muted-foreground">
            {elapsed}
          </span>
        ) : null}
        <div className="flex-1" />
        <Button
          size="sm"
          variant="outline"
          onClick={requestStopRecording}
          disabled={finishing}
        >
          {finishing ? (
            <Spinner className="size-3.5" />
          ) : (
            <Square aria-hidden="true" className="size-3.5" />
          )}
          {t(($) => $.recorder.stop)}
        </Button>
      </div>

      <div className="flex flex-col gap-1">
        <span className="text-caption font-medium text-muted-foreground">
          {t(($) => $.recorder.last_transcript)}
        </span>
        <p className="text-body">
          {lastTranscript || (
            <span className="text-faint-foreground">
              {t(($) => $.recorder.waiting)}
            </span>
          )}
        </p>
      </div>

      {!systemAudio ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.recorder.mic_only)}
        </p>
      ) : null}

      {/* Consent is a product requirement, not a hint: it stays visible for
          the whole recording rather than appearing once as a toast. */}
      <p className="text-caption text-muted-foreground">
        {t(($) => $.recorder.consent)}
      </p>
    </section>
  );
}
