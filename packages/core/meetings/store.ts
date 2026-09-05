import { create } from "zustand";

/**
 * Client state for the meeting recorder. Deliberately NOT persisted: a
 * recording lives in a MediaRecorder that dies with the page, so restoring a
 * "recording" pointer after a refresh would be a lie.
 *
 * Server data (the meeting itself, its transcript, its actions) stays in
 * TanStack Query. What lives here is only what no server round-trip can
 * answer: which meeting this tab is recording, since when, and the last
 * transcript chunk that came back — plus two nonces the UI uses to reach the
 * single mounted recorder from anywhere (see below).
 *
 * Why nonces instead of storing callbacks: the MediaRecorder and its upload
 * queue live in refs inside one mounted hook (`useMeetingRecorder`, mounted
 * once per dashboard shell by `RecordingPill`). Any surface — the meetings
 * page header, the detail page, later the desktop Zoom-detection popup — asks
 * for a start or a stop by bumping a counter; the hook watches the counter.
 * Every field is a primitive or null, so selectors return stable references.
 */
export type MeetingRecorderPhase =
  | "idle"
  | "starting"
  | "recording"
  | "finishing";

export interface MeetingRecorderOpenOptions {
  title?: string;
  appName?: string;
}

interface MeetingRecorderState {
  phase: MeetingRecorderPhase;
  /** The meeting being recorded by THIS client, once the server created it. */
  meetingId: string | null;
  /** ISO timestamp the recording started at, for the elapsed counter. */
  startedAt: string | null;
  /** Most recent transcribed chunk, shown live under the recorder. */
  lastTranscript: string;
  /** True while the provider streams words as they are spoken. */
  live: boolean;
  /** Words received so far this recording (bounded by the panel, not here). */
  liveTranscript: string;
  /** False when the user refused screen/tab audio: microphone only. */
  systemAudio: boolean;
  /**
   * True once the server answered 409 `stt_not_configured`. The list page
   * turns this into a quiet capability banner instead of an error state.
   */
  sttUnavailable: boolean;
  /** Bumped by `openMeetingRecorder`; watched by the mounted recorder hook. */
  openNonce: number;
  openOptions: MeetingRecorderOpenOptions | null;
  /** Bumped by `requestStopRecording`; watched by the same hook. */
  stopNonce: number;

  open: (options?: MeetingRecorderOpenOptions) => void;
  requestStop: () => void;
  setPhase: (phase: MeetingRecorderPhase) => void;
  started: (meetingId: string, startedAt: string, systemAudio: boolean) => void;
  setLastTranscript: (text: string) => void;
  setLive: (live: boolean) => void;
  appendLiveTranscript: (delta: string) => void;
  setSttUnavailable: (unavailable: boolean) => void;
  reset: () => void;
}

const IDLE = {
  phase: "idle" as MeetingRecorderPhase,
  meetingId: null,
  startedAt: null,
  lastTranscript: "",
  live: false,
  liveTranscript: "",
  systemAudio: true,
};

export const useMeetingRecorderStore = create<MeetingRecorderState>((set) => ({
  ...IDLE,
  sttUnavailable: false,
  openNonce: 0,
  openOptions: null,
  stopNonce: 0,

  open: (options) =>
    set((s) => ({
      openNonce: s.openNonce + 1,
      openOptions: options ?? null,
    })),
  requestStop: () => set((s) => ({ stopNonce: s.stopNonce + 1 })),
  setPhase: (phase) => set({ phase }),
  started: (meetingId, startedAt, systemAudio) =>
    set({
      phase: "recording",
      meetingId,
      startedAt,
      systemAudio,
      lastTranscript: "",
    }),
  setLastTranscript: (lastTranscript) => set({ lastTranscript }),
  setLive: (live) => set({ live }),
  appendLiveTranscript: (delta) => set((s) => ({ liveTranscript: s.liveTranscript + delta })),
  setSttUnavailable: (sttUnavailable) => set({ sttUnavailable }),
  reset: () => set({ ...IDLE }),
}));

/**
 * Ask the mounted recorder to start a meeting. The entry point the desktop
 * layer calls when it detects a conferencing app, and what the "Record a
 * meeting" header action calls. Safe to call from outside React.
 */
export function openMeetingRecorder(options?: MeetingRecorderOpenOptions): void {
  useMeetingRecorderStore.getState().open(options);
}

/** Ask the mounted recorder to stop and finish the current meeting. */
export function requestStopRecording(): void {
  useMeetingRecorderStore.getState().requestStop();
}
