"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { calendarUpcomingOptions } from "@multica/core/calendar/queries";
import {
  openMeetingRecorder,
  useMeetingRecorderStore,
} from "@multica/core/meetings/store";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  subscribeMeetingDetected,
  type DetectedMeeting,
} from "../../platform/meeting-detection";
import { useT } from "../../i18n";

/**
 * Desktop-only prompt: the main process noticed a conferencing app take the
 * microphone and asks whether to take notes. Mount it next to `RecordingPill`
 * in the desktop shell — on web `subscribeMeetingDetected` never fires, so
 * mounting it there would only add a dead subscription.
 *
 * Never auto-records. The user clicks, and the click goes through the same
 * `openMeetingRecorder` entry point as the "Record a meeting" header action.
 */
export function MeetingDetectedPrompt() {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const phase = useMeetingRecorderStore((s) => s.phase);
  const [detected, setDetected] = useState<DetectedMeeting | null>(null);
  // The calendar, when the user subscribed one: the app that took the
  // microphone says "Zoom", the calendar says what the meeting is called.
  // Only fetched while a prompt is up, so a workspace nobody records in never
  // asks. A failure is silent — this is a nicety, not a requirement.
  const { data: upcoming } = useQuery({
    ...calendarUpcomingOptions(wsId),
    enabled: wsId.length > 0 && detected !== null && phase === "idle",
  });
  const current = upcoming?.events.find(
    (event) => event.in_progress === true && event.summary.trim() !== "",
  );

  useEffect(() => subscribeMeetingDetected(setDetected), []);

  // A recording already running (or starting) answers the question: the user
  // is taking notes, so drop the pending prompt instead of stacking a dialog
  // over the recorder.
  useEffect(() => {
    if (phase !== "idle") setDetected(null);
  }, [phase]);

  if (!detected || phase !== "idle") return null;

  const appName = detected.appName;
  const title =
    detected.kind === "huddle"
      ? t(($) => $.detected.title_huddle)
      : detected.kind === "call"
        ? t(($) => $.detected.title_call)
        : t(($) => $.detected.title_meeting);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) setDetected(null);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {t(($) => $.detected.description, { appName })}
          </DialogDescription>
        </DialogHeader>
        {current ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.detected.looks_like, { summary: current.summary })}
          </p>
        ) : null}
        <DialogFooter>
          <Button variant="ghost" onClick={() => setDetected(null)}>
            {t(($) => $.detected.dismiss)}
          </Button>
          <Button
            onClick={() => {
              setDetected(null);
              openMeetingRecorder({
                // The calendar's name for it beats "Meeting on Zoom".
                title:
                  current?.summary ??
                  t(($) => $.detected.meeting_title, { appName }),
                appName,
              });
            }}
          >
            {t(($) => $.detected.accept)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
