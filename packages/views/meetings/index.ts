export {
  MeetingsPage,
  MeetingDetailPage,
  MeetingRecorderPanel,
  RecordingPill,
  formatElapsed,
  meetingStatusDotClass,
} from "./components";
export { useMeetingRecorder } from "./use-meeting-recorder";
// Entry point for surfaces outside this package — notably the desktop layer,
// which will call it from its conferencing-app detection popup with
// `{ title, appName }`. It is a store action, so it works outside React too.
export {
  openMeetingRecorder,
  requestStopRecording,
} from "@multica/core/meetings/store";
export type { MeetingRecorderOpenOptions } from "@multica/core/meetings/store";
