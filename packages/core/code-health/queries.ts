import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { CodeHealthScan } from "./schemas";

// Code health autopilot (K22). Settings are static; the scan history polls
// while a scan is in flight (same 10s cadence as the K24 eval run) so the
// findings land without a websocket subscription.

const SCAN_POLL_MS = 10_000;

export const codeHealthKeys = {
  settings: (wsId: string) => ["code-health-settings", wsId] as const,
  scans: (wsId: string) => ["code-health-scans", wsId] as const,
};

export function codeHealthSettingsOptions(wsId: string) {
  return queryOptions({
    queryKey: codeHealthKeys.settings(wsId),
    queryFn: () => api.getCodeHealthSettings(),
    enabled: !!wsId,
  });
}

export function codeHealthScansOptions(wsId: string) {
  return queryOptions({
    queryKey: codeHealthKeys.scans(wsId),
    queryFn: () => api.listCodeHealthScans(),
    enabled: !!wsId,
    refetchInterval: (query) =>
      (query.state.data as CodeHealthScan[] | undefined)?.some((scan) => scan.status === "running")
        ? SCAN_POLL_MS
        : false,
  });
}
