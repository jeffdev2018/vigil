export { useImmersiveMode } from "./use-immersive-mode";
export { useDesktopUnreadBadge } from "./use-desktop-unread-badge";
export { DragStrip } from "./drag-strip";
export { openExternal, MACOS_MICROPHONE_SETTINGS_URL } from "./open-external";
export {
  isDesktopShell,
  pickDirectory,
  validateLocalDirectory,
  type PickDirectoryResult,
  type ValidateLocalDirectoryResult,
} from "./local-directory";
export {
  useLocalDaemonStatus,
  type LocalDaemonStatus,
} from "./use-local-daemon-status";
export {
  ScrollRestorationProvider,
  useScrollRestorationAdapter,
  useRestoredScrollEntry,
  useRestoredScrollOffset,
  useRestoredScrollRef,
  useRestoredViewState,
  useViewStateWriter,
  type ExternalScrollSource,
  type ScrollRestorationAdapter,
  type ScrollRestorationEntry,
} from "./scroll-restoration";
export {
  isMeetingDetectionSupported,
  subscribeMeetingDetected,
  setMeetingDetectionEnabled,
  setMeetingSelfCapture,
  type DetectedMeeting,
  type DetectedMeetingKind,
} from "./meeting-detection";
