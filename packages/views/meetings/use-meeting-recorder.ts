"use client";

import { useCallback, useEffect, useRef } from "react";
import { toast } from "sonner";
import { errorCode, getApi } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  useMeetingRecorderStore,
  type MeetingRecorderOpenOptions,
} from "@multica/core/meetings/store";
import {
  useAppendMeetingSegment,
  useAppendMeetingTextSegment,
  useCreateMeeting,
  useFinishMeeting,
} from "@multica/core/meetings/mutations";
import { startRealtimeTranscriber, type RealtimeTranscriber } from "./realtime-transcriber";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

/**
 * Browser-only recorder for one meeting. Mount it EXACTLY ONCE per dashboard
 * shell (`RecordingPill` does), because it owns the live MediaRecorder, the
 * media tracks and the upload queue in refs — a second instance would record
 * the same conversation twice.
 *
 * Every other surface talks to it through the core store's nonces:
 * `openMeetingRecorder()` starts a recording, `requestStopRecording()` ends
 * one. That is also the entry point the desktop layer will call when it
 * detects a conferencing app.
 *
 * Not covered by DOM tests: MediaRecorder, getUserMedia and getDisplayMedia
 * do not exist in jsdom, and faking all three tests the fakes rather than the
 * recorder. The parsing and error-code branches this hook depends on are
 * covered in `packages/core/meetings/meetings.test.ts`.
 */

/** One upload per 30s of audio: short enough to show transcript as it runs. */
const TIMESLICE_MS = 30_000;
const PREFERRED_MIME = "audio/webm;codecs=opus";

export function useMeetingRecorder() {
  const wsId = useWorkspaceId();
  const { t } = useT("meetings");
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();

  const createMeeting = useCreateMeeting(wsId);
  const appendSegment = useAppendMeetingSegment();
  const appendTextSegment = useAppendMeetingTextSegment();
  const finishMeeting = useFinishMeeting(wsId);

  const openNonce = useMeetingRecorderStore((s) => s.openNonce);
  const stopNonce = useMeetingRecorderStore((s) => s.stopNonce);

  const recorderRef = useRef<MediaRecorder | null>(null);
  const tracksRef = useRef<MediaStreamTrack[]>([]);
  const audioContextRef = useRef<AudioContext | null>(null);
  const meetingIdRef = useRef<string | null>(null);
  const queueRef = useRef<Blob[]>([]);
  const pumpingRef = useRef(false);
  const abortedRef = useRef(false);
  const stoppingRef = useRef(false);
  const seqRef = useRef(0);
  // Live transcript (provider realtime socket). While it runs, audio chunks
  // are not uploaded: the text it produced is sent instead, every 30s.
  const liveRef = useRef<RealtimeTranscriber | null>(null);
  const liveTextRef = useRef("");
  const liveFlushTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const releaseMedia = useCallback(() => {
    if (liveFlushTimerRef.current) {
      clearInterval(liveFlushTimerRef.current);
      liveFlushTimerRef.current = null;
    }
    liveRef.current = null;
    for (const track of tracksRef.current) track.stop();
    tracksRef.current = [];
    const ctx = audioContextRef.current;
    audioContextRef.current = null;
    if (ctx && ctx.state !== "closed") {
      void ctx.close().catch(() => {
        // Closing an AudioContext the browser already tore down is not a
        // failure the user can act on; the tracks above are what hold the
        // microphone indicator, and they are stopped.
      });
    }
  }, []);

  const stopRef = useRef<() => Promise<void>>(async () => {});

  /**
   * Uploads queued chunks strictly one at a time. Parallel uploads would let
   * the server interleave segments, which is exactly the ordering the
   * transcript depends on.
   */
  const pump = useCallback(async () => {
    if (pumpingRef.current) return;
    pumpingRef.current = true;
    try {
      while (queueRef.current.length > 0) {
        const meetingId = meetingIdRef.current;
        if (!meetingId || abortedRef.current) {
          queueRef.current = [];
          break;
        }
        const chunk = queueRef.current.shift();
        if (!chunk) break;
        const seq = seqRef.current;
        seqRef.current += 1;
        try {
          const res = await appendSegment.mutateAsync({ meetingId, chunk, seq });
          if (res.text) {
            useMeetingRecorderStore.getState().setLastTranscript(res.text);
          }
        } catch (err) {
          const code = errorCode(err);
          if (code === "meeting_not_recording" || code === "meeting_too_long") {
            toast.error(
              code === "meeting_too_long"
                ? t(($) => $.recorder.error_too_long)
                : t(($) => $.recorder.error_not_recording),
            );
            abortedRef.current = true;
            queueRef.current = [];
            break;
          }
          // One failed chunk is a gap in the transcript, not the end of the
          // meeting: surface it and keep recording.
          toast.error(t(($) => $.recorder.error_segment));
        }
      }
    } finally {
      pumpingRef.current = false;
      // The server closed the recording under us — wind the local one down
      // from outside the loop so `stop` never waits on this pump.
      if (abortedRef.current && meetingIdRef.current) void stopRef.current();
    }
  }, [appendSegment, t]);

  /** Sends the live text accumulated since the last flush as one segment. */
  const flushLiveText = useCallback(async () => {
    const meetingId = meetingIdRef.current;
    const text = liveTextRef.current.trim();
    if (!meetingId || !text) return;
    liveTextRef.current = "";
    const seq = seqRef.current;
    seqRef.current += 1;
    try {
      await appendTextSegment.mutateAsync({ meetingId, text, seq });
    } catch {
      // Keep the words for the next flush rather than losing them.
      liveTextRef.current = `${text} ${liveTextRef.current}`.trim();
      toast.error(t(($) => $.recorder.error_segment));
    }
  }, [appendTextSegment, t]);

  const start = useCallback(
    async (options?: MeetingRecorderOpenOptions) => {
      const store = useMeetingRecorderStore.getState();
      if (store.phase !== "idle") return;
      if (
        typeof MediaRecorder === "undefined" ||
        typeof navigator === "undefined" ||
        !navigator.mediaDevices?.getUserMedia
      ) {
        toast.error(t(($) => $.recorder.error_unsupported));
        return;
      }
      store.setPhase("starting");

      let mic: MediaStream;
      try {
        mic = await navigator.mediaDevices.getUserMedia({ audio: true });
      } catch {
        toast.error(t(($) => $.recorder.error_microphone));
        store.setPhase("idle");
        return;
      }
      tracksRef.current = [...mic.getTracks()];

      // System/tab audio is optional: a refused screen share still gives a
      // usable meeting from the microphone alone, so it must not abort.
      let systemAudio: MediaStream | null = null;
      try {
        const display = await navigator.mediaDevices.getDisplayMedia({
          audio: true,
          video: true,
        });
        // The video track only exists because Chrome refuses audio-only
        // display capture — drop it as soon as the picker closes.
        for (const track of display.getVideoTracks()) {
          track.stop();
          display.removeTrack(track);
        }
        if (display.getAudioTracks().length > 0) {
          systemAudio = display;
          tracksRef.current.push(...display.getAudioTracks());
        }
      } catch {
        systemAudio = null;
      }

      let meetingId: string;
      let startedAt: string;
      try {
        const meeting = await createMeeting.mutateAsync({
          title: options?.title,
          appName: options?.appName,
        });
        meetingId = meeting.id;
        startedAt = meeting.started_at || new Date().toISOString();
      } catch (err) {
        releaseMedia();
        if (errorCode(err) === "stt_not_configured") {
          store.setSttUnavailable(true);
          toast.error(t(($) => $.recorder.error_stt));
        } else {
          toast.error(t(($) => $.recorder.error_create));
        }
        store.setPhase("idle");
        return;
      }

      // Mic and system audio are two streams; MediaRecorder takes one, so
      // mix them through a shared destination node.
      const ctx = new AudioContext();
      audioContextRef.current = ctx;
      const destination = ctx.createMediaStreamDestination();
      ctx.createMediaStreamSource(mic).connect(destination);
      if (systemAudio) {
        ctx.createMediaStreamSource(systemAudio).connect(destination);
      }

      // Live transcript when the server offers it. Failure to connect is
      // silent: the batch path below covers the whole meeting anyway.
      if (configStore.getState().meetingRealtimeAvailable) {
        try {
          const session = await getApi().realtimeVoiceSession();
          const transcriber = await startRealtimeTranscriber({
            stream: destination.stream,
            session,
            onDelta: (delta) => {
              liveTextRef.current += delta;
              useMeetingRecorderStore.getState().appendLiveTranscript(delta);
            },
            onError: () => {
              // From here on the audio chunks are uploaded again; what the
              // socket already produced is flushed as text below.
              if (!liveRef.current) return;
              liveRef.current = null;
              useMeetingRecorderStore.getState().setLive(false);
              toast.error(t(($) => $.recorder.live_lost));
            },
          });
          liveRef.current = transcriber;
          liveTextRef.current = "";
          useMeetingRecorderStore.getState().setLive(true);
        } catch {
          liveRef.current = null;
        }
      }

      const recorder = MediaRecorder.isTypeSupported(PREFERRED_MIME)
        ? new MediaRecorder(destination.stream, { mimeType: PREFERRED_MIME })
        : new MediaRecorder(destination.stream);
      recorder.ondataavailable = (event: BlobEvent) => {
        if (abortedRef.current || event.data.size === 0) return;
        // The live socket already transcribed this stretch of audio.
        if (liveRef.current) return;
        queueRef.current.push(event.data);
        void pump();
      };
      recorder.onerror = () => {
        toast.error(t(($) => $.recorder.error_segment));
      };

      queueRef.current = [];
      seqRef.current = 0;
      abortedRef.current = false;
      meetingIdRef.current = meetingId;
      recorderRef.current = recorder;
      recorder.start(TIMESLICE_MS);
      if (liveRef.current) {
        liveFlushTimerRef.current = setInterval(() => void flushLiveText(), TIMESLICE_MS);
      }
      store.started(meetingId, startedAt, systemAudio !== null);
      push(wsPaths.meetingDetail(meetingId));
    },
    [createMeeting, flushLiveText, push, pump, releaseMedia, t, wsPaths],
  );

  const stop = useCallback(async () => {
    const meetingId = meetingIdRef.current;
    if (!meetingId || stoppingRef.current) return;
    stoppingRef.current = true;
    useMeetingRecorderStore.getState().setPhase("finishing");
    try {
      // Let the live socket deliver its last words, then send them as text.
      const live = liveRef.current;
      if (live) {
        if (liveFlushTimerRef.current) {
          clearInterval(liveFlushTimerRef.current);
          liveFlushTimerRef.current = null;
        }
        await live.stop();
        await flushLiveText();
        liveRef.current = null;
      }
      // `stop()` emits one final `dataavailable` before `stop`, which is how
      // the tail of the conversation gets uploaded at all.
      const recorder = recorderRef.current;
      if (recorder && recorder.state !== "inactive") {
        await new Promise<void>((resolve) => {
          recorder.addEventListener("stop", () => resolve(), { once: true });
          recorder.stop();
        });
      }
      recorderRef.current = null;
      releaseMedia();

      // ponytail: polled drain. The queue is at most a handful of chunks and
      // this runs once per meeting; swap for a promise chain if it grows.
      while (queueRef.current.length > 0 || pumpingRef.current) {
        await new Promise((resolve) => setTimeout(resolve, 200));
      }

      try {
        await finishMeeting.mutateAsync(meetingId);
        toast.success(t(($) => $.recorder.finished));
      } catch (err) {
        // Another client already asked for the summary — the meeting is
        // finishing correctly, so this is not a failure to report.
        if (errorCode(err) !== "meeting_summarizing") {
          toast.error(t(($) => $.recorder.error_finish));
        }
      }
    } finally {
      meetingIdRef.current = null;
      abortedRef.current = false;
      stoppingRef.current = false;
      useMeetingRecorderStore.getState().reset();
    }
  }, [finishMeeting, flushLiveText, releaseMedia, t]);

  stopRef.current = stop;

  // Nonce watchers. `start`/`stop` change identity on every render (they close
  // over mutations), so the effects key off the nonce alone and reach the
  // current implementation through a ref.
  const startRef = useRef(start);
  startRef.current = start;

  useEffect(() => {
    if (openNonce === 0) return;
    void startRef.current(
      useMeetingRecorderStore.getState().openOptions ?? undefined,
    );
  }, [openNonce]);

  useEffect(() => {
    if (stopNonce === 0) return;
    void stopRef.current();
  }, [stopNonce]);

  // Leaving the workspace shell must release the microphone even though the
  // meeting cannot be finished from an unmounting component.
  useEffect(() => releaseMedia, [releaseMedia]);
}
