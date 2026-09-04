"use client";

import { useEffect } from "react";
import { Square } from "lucide-react";
import {
  requestStopRecording,
  useMeetingRecorderStore,
} from "@multica/core/meetings/store";
import { useMeetingPreferencesStore } from "@multica/core/meetings/preferences-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  setMeetingDetectionEnabled,
  setMeetingSelfCapture,
} from "../../platform/meeting-detection";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { useMeetingRecorder } from "../use-meeting-recorder";
import { RecordingDot, useElapsed } from "./meeting-recorder";

/**
 * Mount once per dashboard shell, through the layout's `extra` slot (the same
 * place `FloatingChat` is mounted, in both apps). Two jobs:
 *
 *  1. It hosts `useMeetingRecorder` — the single owner of the MediaRecorder,
 *     the media tracks and the upload queue. Mounting it anywhere else, or
 *     twice, would record the conversation twice.
 *  2. It shows the recording indicator on every page, so a user who navigates
 *     away mid-meeting can always see it and stop it.
 *  3. It is where the desktop shell learns the two things only the renderer
 *     knows: that our own recorder holds the microphone, and whether the user
 *     wants ambient detection running at all.
 */
export function RecordingPill() {
  useMeetingRecorder();

  const { t } = useT("meetings");
  const wsPaths = useWorkspacePaths();
  const phase = useMeetingRecorderStore((s) => s.phase);
  const meetingId = useMeetingRecorderStore((s) => s.meetingId);
  const startedAt = useMeetingRecorderStore((s) => s.startedAt);
  const elapsed = useElapsed(startedAt);

  // Ambient meeting detection (desktop) watches the same microphone this
  // recorder is about to open. Tell it the mic is ours so it never prompts
  // "Meeting detected" over our own recording. No-op on web.
  const selfCapturing = phase !== "idle";
  useEffect(() => {
    setMeetingSelfCapture(selfCapturing);
  }, [selfCapturing]);

  // Settings → Preferences. Pushed on mount too, so a shell restarted with the
  // preference off never starts watching. No-op on web.
  const detectMeetings = useMeetingPreferencesStore((s) => s.detectMeetings);
  useEffect(() => {
    setMeetingDetectionEnabled(detectMeetings);
  }, [detectMeetings]);

  if (phase === "idle") return null;

  const finishing = phase === "finishing";

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-50 flex justify-center px-4">
      <div className="pointer-events-auto flex items-center gap-2 rounded-full border bg-background px-3 py-1.5 shadow-lg">
        <RecordingDot live={phase === "recording"} />
        {meetingId ? (
          <AppLink
            href={wsPaths.meetingDetail(meetingId)}
            className="font-mono text-caption tabular-nums text-muted-foreground hover:text-foreground"
          >
            {elapsed ?? t(($) => $.recorder.starting)}
          </AppLink>
        ) : (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.recorder.starting)}
          </span>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="h-6 px-2"
          onClick={requestStopRecording}
          disabled={finishing || phase === "starting"}
        >
          {finishing ? (
            <Spinner className="size-3.5" />
          ) : (
            <Square aria-hidden="true" className="size-3.5" />
          )}
          {t(($) => $.recorder.stop)}
        </Button>
      </div>
    </div>
  );
}
