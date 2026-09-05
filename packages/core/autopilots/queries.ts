import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const autopilotKeys = {
  all: (wsId: string) => ["autopilots", wsId] as const,
  usage: (wsId: string) => [...autopilotKeys.all(wsId), "usage"] as const,
  list: (wsId: string) => [...autopilotKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "runs", id] as const,
  run: (wsId: string, autopilotId: string, runId: string) =>
    [...autopilotKeys.all(wsId), "runs", autopilotId, runId] as const,
  deliveries: (wsId: string, id: string) =>
    [...autopilotKeys.all(wsId), "deliveries", id] as const,
  delivery: (wsId: string, autopilotId: string, deliveryId: string) =>
    [...autopilotKeys.all(wsId), "deliveries", autopilotId, deliveryId] as const,
  cronPreview: (wsId: string, expr: string, tz: string, windowMinutes: number) =>
    [...autopilotKeys.all(wsId), "cron-preview", expr, tz, windowMinutes] as const,
  scheduleDryRun: (wsId: string, autopilotId: string, triggerId: string) =>
    [...autopilotKeys.all(wsId), "dry-run", autopilotId, triggerId] as const,
};

export function autopilotQuotaUsageOptions(wsId: string) {
  return queryOptions({
    queryKey: autopilotKeys.usage(wsId),
    queryFn: () => api.getAutopilotQuotaUsage(),
    enabled: wsId.length > 0,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });
}

export function autopilotListOptions(wsId: string) {
  return queryOptions({
    queryKey: autopilotKeys.list(wsId),
    queryFn: () => api.listAutopilots(),
    select: (data) => data.autopilots,
  });
}

export function autopilotDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: autopilotKeys.detail(wsId, id),
    queryFn: () => api.getAutopilot(id),
  });
}

// Runs and deliveries are both unbounded histories, and both used to load one
// server-default page (20) with no way to reach the 21st row. Paged by
// offset — the server orders by created_at DESC with no cursor — and the
// authoritative `total` comes from a COUNT, so "N of total" is true and
// `getNextPageParam` stops on the real end rather than on a short page.
export const AUTOPILOT_PAGE_SIZE = 20;

function offsetPager<T>(pages: readonly T[], count: (page: T) => number, total: number) {
  const loaded = pages.reduce((n, page) => n + count(page), 0);
  // A page shorter than requested still means "no more" even if a concurrent
  // delete made `total` stale, so guard on both.
  return loaded >= total || count(pages[pages.length - 1] as T) === 0 ? undefined : loaded;
}

export function autopilotRunsOptions(wsId: string, id: string) {
  return infiniteQueryOptions({
    queryKey: autopilotKeys.runs(wsId, id),
    queryFn: ({ pageParam }) =>
      api.listAutopilotRuns(id, { limit: AUTOPILOT_PAGE_SIZE, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, allPages) =>
      offsetPager(allPages, (p) => p.runs.length, lastPage.total),
    select: (data) => ({
      items: data.pages.flatMap((page) => page.runs),
      total: data.pages[data.pages.length - 1]?.total ?? 0,
    }),
  });
}

// autopilotRunOptions fetches a single run with its full trigger_payload.
// The list endpoint (autopilotRunsOptions) omits trigger_payload to keep
// list responses small; callers (e.g. the run-detail dialog) use this
// query on demand when the user opens a run.
export function autopilotRunOptions(
  wsId: string,
  autopilotId: string,
  runId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.run(wsId, autopilotId, runId),
    queryFn: () => api.getAutopilotRun(autopilotId, runId),
    enabled: options?.enabled ?? true,
  });
}

// autopilotDeliveriesOptions powers the Deliveries section in the autopilot
// detail page. The list is slim — raw_body / selected_headers / response_body
// are omitted server-side. Detail rows are fetched on-demand when the user
// expands a row (see autopilotDeliveryOptions).
export function autopilotDeliveriesOptions(
  wsId: string,
  autopilotId: string,
  options?: { enabled?: boolean },
) {
  return infiniteQueryOptions({
    queryKey: autopilotKeys.deliveries(wsId, autopilotId),
    queryFn: ({ pageParam }) =>
      api.listAutopilotDeliveries(autopilotId, {
        limit: AUTOPILOT_PAGE_SIZE,
        offset: pageParam,
      }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, allPages) =>
      offsetPager(allPages, (p) => p.deliveries.length, lastPage.total),
    select: (data) => ({
      items: data.pages.flatMap((page) => page.deliveries),
      total: data.pages[data.pages.length - 1]?.total ?? 0,
    }),
    enabled: options?.enabled ?? true,
  });
}

// autopilotDeliveryOptions fetches the full delivery row including raw_body
// and headers subset. Used by the detail dialog opened from a list row.
export function autopilotDeliveryOptions(
  wsId: string,
  autopilotId: string,
  deliveryId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.delivery(wsId, autopilotId, deliveryId),
    queryFn: () => api.getAutopilotDelivery(autopilotId, deliveryId),
    enabled: options?.enabled ?? true,
  });
}

// cronPreviewOptions backs the schedule editor's next-run preview. The server
// owns cron/timezone evaluation, so the editor never approximates it locally.
export function cronPreviewOptions(
  wsId: string,
  expr: string,
  tz: string,
  // The firing band is not in the expression, so it has to be part of the key
  // as well as the request — the same cron with and without a band previews to
  // different instants.
  windowMinutes: number,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.cronPreview(wsId, expr, tz, windowMinutes),
    queryFn: () => api.cronPreview({ expr, tz, windowMinutes }),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
    // A 400 (invalid expression/timezone) is a stable answer for this input,
    // not a transient failure — retrying would only delay the inline error.
    retry: false,
  });
}

// scheduleTriggerDryRunOptions previews a saved schedule trigger: the next
// firing instants the scheduler itself would pick, plus whatever would
// suppress the dispatch. Server-computed — the band offset is derived from the
// trigger id and the client cannot reproduce it.
export function scheduleTriggerDryRunOptions(
  wsId: string,
  autopilotId: string,
  triggerId: string,
  options?: { enabled?: boolean },
) {
  return queryOptions({
    queryKey: autopilotKeys.scheduleDryRun(wsId, autopilotId, triggerId),
    queryFn: () => api.dryRunAutopilotScheduleTrigger(autopilotId, triggerId),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
    // A 400 (the stored cron no longer parses) is a stable answer for this
    // trigger, not a transient failure — retrying only delays the message.
    retry: false,
  });
}
