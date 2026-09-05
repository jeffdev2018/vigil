"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Inbox, Check, X, Loader2, ExternalLink } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { TriageItem, TriageItemState, TriageSource, TriageSourceMode, TriageSuggestion, TriageAutoSettings } from "@multica/core/types";
import { triageStatsOptions, triageItemsInfiniteOptions, triageSuggestionsOptions } from "@multica/core/triage/queries";
import { TriageSuggestionChip, TriageSuggestionPanel } from "./triage-suggestion";
import {
  useBatchAcceptTriageItems,
  useBatchDismissTriageItems,
  useUpdateTriageSourceMode,
} from "@multica/core/triage/mutations";
import { TriageItemActions, TriageVerdictBadge, TriageVerdictNote } from "./triage-item-actions";
import { isSnoozed } from "./snooze-presets";
import { useShortcutAction } from "./use-shortcut-action";
import { isEditableShortcutTarget, isPortalLayerShortcutTarget } from "@multica/core/shortcuts";
import { useT, useTimeAgo } from "../../i18n";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { RichContent } from "../../rich-content";

const SOURCE_MODES: TriageSourceMode[] = ["gate", "direct", "blocked"];

/**
 * Tabs above the queue. `snoozed` is not a server state: a snoozed item is
 * still `pending`, just parked, so that tab lists pending with
 * `include_snoozed` and keeps only the ones still in the future. The three
 * history tabs are the only place a dismissed item can be reopened from.
 */
const TRIAGE_TABS = ["pending", "snoozed", "accepted", "dismissed", "merged"] as const;
type TriageTab = (typeof TRIAGE_TABS)[number];

/** Oldest pending age in seconds → an ISO timestamp `timeAgo` can render. */
function ageSecondsToIso(seconds: number): string {
  return new Date(Date.now() - seconds * 1000).toISOString();
}

/**
 * Keyboard cursor for the queue: the focused row IS the cursor, so J/K and the
 * arrow keys only move DOM focus and the row's own Enter/Space handler opens
 * the detail. Nothing new to keep in state, and Tab lands on the same rows.
 */
function moveRowFocus(list: HTMLElement | null, delta: number): void {
  if (!list) return;
  const rows = [...list.querySelectorAll<HTMLElement>("[data-triage-row]")];
  if (rows.length === 0) return;
  const current = rows.findIndex((row) => row === document.activeElement);
  const next = current < 0 ? 0 : Math.min(rows.length - 1, Math.max(0, current + delta));
  rows[next]?.focus();
}

function formatPayload(payload: TriageItem["payload"]): string | null {
  const body = payload.body;
  if (body && Object.keys(body).length > 0) {
    try {
      return JSON.stringify(body, null, 2);
    } catch {
      return null;
    }
  }
  return null;
}

export function TriagePage() {
  const wsId = useWorkspaceId();
  const { t } = useT("triage");
  const timeAgo = useTimeAgo();

  const statsQuery = useQuery(triageStatsOptions(wsId));
  const [filterTab, setFilterTab] = useState<TriageTab>("pending");
  const listState: TriageItemState = filterTab === "snoozed" ? "pending" : filterTab;
  const itemsQuery = useInfiniteQuery(
    triageItemsInfiniteOptions(wsId, listState, filterTab === "snoozed"),
  );

  // `?item=` deep link: a meeting action (paths.triage(itemId)) opens the queue
  // on the entry it produced. Read once — after that the selection is the
  // user's, not the URL's.
  const { searchParams } = useNavigation();
  const [selectedId, setSelectedId] = useState<string | null>(
    () => searchParams.get("item"),
  );
  const [checkedIds, setCheckedIds] = useState<Set<string>>(() => new Set());

  const items = useMemo(() => {
    const all = itemsQuery.data?.pages.flatMap((page) => page.items) ?? [];
    // `include_snoozed` widens the pending listing rather than replacing it,
    // so the Snoozed tab keeps only what is actually parked.
    return filterTab === "snoozed" ? all.filter((item) => isSnoozed(item)) : all;
  }, [itemsQuery.data, filterTab]);

  // Triage auto-ML (K61): one request for the visible page.

  const suggestionsQuery = useQuery(triageSuggestionsOptions(wsId, items.map((i) => i.id)));

  const suggestions = suggestionsQuery.data?.suggestions ?? {};

  const autoSettings = suggestionsQuery.data?.auto;
  const stats = statsQuery.data;
  const selected = useMemo(
    () => items.find((item) => item.id === selectedId) ?? null,
    [items, selectedId],
  );

  const toggleChecked = useCallback((id: string) => {
    setCheckedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => setCheckedIds(new Set()), []);

  // Escape clears the open detail. It cannot be a rebindable action (Escape is
  // reserved for dismissing popups), so it is bound here directly and yields to
  // any open menu or dialog through the same guards as the other bindings.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (event.defaultPrevented || event.repeat) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      setSelectedId(null);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const handleFilter = useCallback((tab: TriageTab) => {
    setFilterTab(tab);
    setSelectedId(null);
    setCheckedIds(new Set());
  }, []);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Inbox}
        title={t(($) => $.title)}
        count={stats?.pending}
        description={t(($) => $.subtitle)}
      />

      <div className="flex shrink-0 items-center gap-1 border-b px-4 py-2">
        {TRIAGE_TABS.map((tab) => {
          const isActive = filterTab === tab;
          const count =
            tab === "pending" ? stats?.pending ?? 0 : tab === "snoozed" ? stats?.snoozed ?? 0 : 0;
          return (
            <button
              key={tab}
              type="button"
              onClick={() => handleFilter(tab)}
              className={cn(
                "rounded-md px-2.5 py-1 text-caption transition-colors",
                isActive
                  ? "bg-accent font-medium text-foreground"
                  : "text-muted-foreground hover:bg-accent/60",
              )}
            >
              {t(($) => $.filter[tab])}
              {count > 0 ? (
                <span className="ml-1 font-mono text-micro tabular-nums">{count}</span>
              ) : null}
            </button>
          );
        })}
      </div>

      <TriageStatsBar
        pending={stats?.pending ?? 0}
        oldestAgeSeconds={stats?.oldest_pending_age_seconds ?? 0}
        dropped24h={stats?.dropped_24h ?? 0}
        timeAgo={timeAgo}
      />

      <TriageSourcesStrip sources={stats?.sources ?? []} wsId={wsId} />

      <div className="flex min-h-0 flex-1">
        <TriageList
          items={items}
          state={listState}
          isLoading={itemsQuery.isLoading}
          isError={itemsQuery.isError}
          selectedId={selectedId}
          checkedIds={checkedIds}
          onSelect={setSelectedId}
          onToggleChecked={toggleChecked}
          timeAgo={timeAgo}
          suggestions={suggestions}
          hasNextPage={itemsQuery.hasNextPage}
          isFetchingNextPage={itemsQuery.isFetchingNextPage}
          onLoadMore={() => void itemsQuery.fetchNextPage()}
        />
        <TriageDetail item={selected} wsId={wsId} onResolved={() => setSelectedId(null)} suggestion={selected ? suggestions[selected.id] : undefined} auto={autoSettings} />
      </div>

      {checkedIds.size > 0 ? (
        <TriageBatchBar wsId={wsId} ids={[...checkedIds]} onDone={clearSelection} />
      ) : null}
    </div>
  );
}

function TriageStatsBar({
  pending,
  oldestAgeSeconds,
  dropped24h,
  timeAgo,
}: {
  pending: number;
  oldestAgeSeconds: number;
  dropped24h: number;
  timeAgo: (iso: string) => string;
}) {
  const { t } = useT("triage");
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-1 border-b px-4 py-2 text-caption text-muted-foreground">
      <span>
        {pending > 0
          ? t(($) => $.stats.pending, { count: pending })
          : t(($) => $.stats.none_pending)}
      </span>
      {pending > 0 && oldestAgeSeconds > 0 ? (
        <span>{t(($) => $.stats.oldest, { age: timeAgo(ageSecondsToIso(oldestAgeSeconds)) })}</span>
      ) : null}
      {dropped24h > 0 ? (
        <span>{t(($) => $.stats.dropped_24h, { count: dropped24h })}</span>
      ) : null}
    </div>
  );
}

// Local guard so a future server-side mode that is not in SOURCE_MODES still
// renders a valid label key instead of an out-of-bounds lookup.
function knownMode(mode: string): TriageSourceMode {
  return (SOURCE_MODES as string[]).includes(mode)
    ? (mode as TriageSourceMode)
    : "direct";
}

function TriageSourcesStrip({ sources, wsId }: { sources: TriageSource[]; wsId: string }) {
  const { t } = useT("triage");
  const updateMode = useUpdateTriageSourceMode(wsId);

  if (sources.length === 0) {
    return (
      <div className="shrink-0 border-b px-4 py-2 text-caption text-muted-foreground">
        {t(($) => $.sources.empty)}
      </div>
    );
  }

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
      {sources.map((source) => {
        const mode = knownMode(source.mode);
        return (
          <div
            key={source.id}
            className="flex items-center gap-1.5 rounded-lg border px-2 py-1"
          >
            <span className="max-w-40 truncate text-caption" title={source.name}>
              {source.name || source.kind}
            </span>
            {source.items_24h > 0 ? (
              <span className="font-mono text-micro tabular-nums text-muted-foreground">
                {t(($) => $.sources.items_24h, { count: source.items_24h })}
              </span>
            ) : null}
            <Select
              items={SOURCE_MODES.map((m) => ({
                value: m,
                label: t(($) => $.sources.mode[m]),
              }))}
              value={mode}
              disabled={updateMode.isPending}
              onValueChange={(next) => {
                if (next && (SOURCE_MODES as string[]).includes(next)) {
                  updateMode.mutate({ sourceId: source.id, mode: next as TriageSourceMode });
                }
              }}
            >
              <SelectTrigger size="sm" className="h-6 w-auto gap-1 px-1.5 text-micro">
                <SelectValue>{t(($) => $.sources.mode[mode])}</SelectValue>
              </SelectTrigger>
              <SelectContent align="start">
                {SOURCE_MODES.map((m) => (
                  <SelectItem key={m} value={m} className="text-caption">
                    {t(($) => $.sources.mode[m])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        );
      })}
    </div>
  );
}

function TriageList({
  items,
  state,
  isLoading,
  isError,
  selectedId,
  checkedIds,
  onSelect,
  onToggleChecked,
  timeAgo,
  suggestions = {},
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
}: {
  items: TriageItem[];
  state: TriageItemState;
  suggestions?: Record<string, TriageSuggestion>;
  isLoading: boolean;
  isError: boolean;
  selectedId: string | null;
  checkedIds: Set<string>;
  onSelect: (id: string) => void;
  onToggleChecked: (id: string) => void;
  timeAgo: (iso: string) => string;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  const { t } = useT("triage");
  const listRef = useRef<HTMLUListElement>(null);

  // J/K walk the queue from anywhere on the page; Up/Down do the same while a
  // row already has focus (see the list's own onKeyDown).
  useShortcutAction("triageNextItem", () => moveRowFocus(listRef.current, 1));
  useShortcutAction("triagePrevItem", () => moveRowFocus(listRef.current, -1));

  if (isLoading) {
    return (
      <div className="flex w-full flex-col gap-2 p-4" aria-busy="true">
        <span className="sr-only">{t(($) => $.list.loading)}</span>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-14 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={Inbox}
          title={t(($) => $.list.load_error)}
          tone="destructive"
          role="alert"
        />
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={Inbox}
          title={
            state === "pending"
              ? t(($) => $.list.empty_title)
              : t(($) => $.list.empty_history_title)
          }
          description={
            state === "pending" ? t(($) => $.list.empty_description) : undefined
          }
        />
      </div>
    );
  }

  return (
    <ul
      ref={listRef}
      onKeyDown={(e) => {
        if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
        e.preventDefault();
        moveRowFocus(listRef.current, e.key === "ArrowDown" ? 1 : -1);
      }}
      className="flex w-full min-w-0 flex-1 flex-col gap-1 overflow-y-auto p-2"
    >
      {items.map((item) => {
        const isActive = item.id === selectedId;
        const isChecked = checkedIds.has(item.id);
        return (
          <li key={item.id}>
            <div
              role="button"
              tabIndex={0}
              data-triage-row=""
              onClick={() => onSelect(item.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelect(item.id);
                }
              }}
              className={cn(
                "group flex w-full cursor-pointer items-center gap-2 rounded-lg border px-2 py-2 text-left transition-colors",
                "outline-none focus-visible:ring-2 focus-visible:ring-ring",
                isActive
                  ? "border-primary/40 bg-accent"
                  : "border-transparent hover:bg-accent/60",
              )}
            >
              {state === "pending" ? (
                <span
                  onClick={(e) => e.stopPropagation()}
                  className={cn(
                    "-m-1 flex shrink-0 items-center p-1",
                    isChecked ? "" : "opacity-0 transition-opacity group-hover:opacity-100",
                  )}
                >
                  <Checkbox
                    checked={isChecked}
                    onCheckedChange={() => onToggleChecked(item.id)}
                    aria-label={item.title}
                  />
                </span>
              ) : null}
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="truncate text-body">{item.title}</span>
                <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
                  {item.source_name ? <span className="truncate">{item.source_name}</span> : null}
                  <span className="shrink-0">{timeAgo(item.first_seen_at)}</span>
                </span>
              </div>
              <TriageVerdictBadge item={item} />
              <TriageSuggestionChip suggestion={suggestions[item.id]} />
              {item.collapse_count > 1 ? (
                <Badge variant="secondary" className="shrink-0 font-mono text-micro tabular-nums">
                  ×{item.collapse_count}
                </Badge>
              ) : null}
            </div>
          </li>
        );
      })}
      {hasNextPage ? (
        <li>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="w-full"
            disabled={isFetchingNextPage}
            onClick={onLoadMore}
          >
            {t(($) => $.list.load_more)}
          </Button>
        </li>
      ) : null}
    </ul>
  );
}

function TriageDetail({
  item,
  wsId,
  onResolved, suggestion, auto }: {
  item: TriageItem | null;
  wsId: string;
  onResolved: () => void; suggestion?: TriageSuggestion; auto?: TriageAutoSettings }) {
  const { t } = useT("triage");

  if (!item) {
    return (
      <aside className="hidden min-w-0 flex-1 border-l lg:flex lg:items-center lg:justify-center">
        <p className="text-caption text-muted-foreground">{t(($) => $.detail.select_prompt)}</p>
      </aside>
    );
  }

  return <TriageDetailBody key={item.id} item={item} wsId={wsId} onResolved={onResolved} suggestion={suggestion} auto={auto} />;
}

function TriageDetailBody({
  item,
  wsId,
  onResolved,
  suggestion,
  auto,
}: {
  item: TriageItem;
  wsId: string;
  onResolved: () => void;
  suggestion?: TriageSuggestion;
  auto?: TriageAutoSettings;
}) {
  const { t } = useT("triage");
  const timeAgo = useTimeAgo();
  const wsPaths = useWorkspacePaths();

  const payloadJson = useMemo(() => formatPayload(item.payload), [item.payload]);

  return (
    <aside className="flex min-w-0 flex-1 flex-col border-l">
      <div className="flex shrink-0 flex-col gap-3 border-b px-4 py-3">
        <div className="min-w-0">
          <h2 className="truncate text-body font-medium">{item.title}</h2>
          <p className="truncate text-caption text-muted-foreground">
            {item.source_name ? t(($) => $.detail.from_source, { name: item.source_name }) : null}
            {" · "}
            {timeAgo(item.first_seen_at)}
            {item.collapse_count > 1
              ? ` · ${t(($) => $.detail.collapse_count, { count: item.collapse_count })}`
              : ""}
          </p>
          {item.origin_type === "meeting" && item.origin_id ? (
            <AppLink
              href={wsPaths.meetingDetail(item.origin_id)}
              className="inline-flex items-center gap-1 text-caption text-muted-foreground transition-colors hover:text-foreground"
            >
              <ExternalLink aria-hidden="true" className="size-3.5" />
              {t(($) => $.detail.from_meeting)}
            </AppLink>
          ) : null}
        </div>
        {item.state === "pending" ? (
          <TriageItemActions item={item} wsId={wsId} onResolved={onResolved} />
        ) : null}
      </div>

      <TriageVerdictNote item={item} />

      {item.resolution_reason ? (
        <p
          data-testid="triage-resolution-reason"
          className="shrink-0 border-b px-4 py-2 text-caption text-muted-foreground"
        >
          {t(($) => $.detail.resolution_reason, { reason: item.resolution_reason })}
        </p>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-4">
        <section className="flex flex-col gap-1.5">
          <h3 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.body_label)}
          </h3>
          {item.body_markdown ? (
            <div className="rounded-lg border p-3">
              <RichContent content={item.body_markdown} density="document" />
            </div>
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.detail.no_body)}</p>
          )}
        </section>

        <TriageSuggestionPanel item={item} suggestion={suggestion} auto={auto} wsId={wsId} />

        <section className="flex flex-col gap-1.5">
          <h3 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.payload_label)}
          </h3>
          {payloadJson ? (
            <pre className="overflow-x-auto rounded-lg border bg-muted/40 p-3 font-mono text-caption">
              {payloadJson}
            </pre>
          ) : item.payload.truncated ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.detail.payload_truncated)}
            </p>
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.detail.no_payload)}</p>
          )}
        </section>
      </div>
    </aside>
  );
}

function TriageBatchBar({
  wsId,
  ids,
  onDone,
}: {
  wsId: string;
  ids: string[];
  onDone: () => void;
}) {
  const { t } = useT("triage");
  const batchAccept = useBatchAcceptTriageItems(wsId);
  const batchDismiss = useBatchDismissTriageItems(wsId);
  const [reason, setReason] = useState("");
  const [dismissOpen, setDismissOpen] = useState(false);
  const busy = batchAccept.isPending || batchDismiss.isPending;

  // The server answers 200 with a per-item outcome even when some items were
  // duplicates or failed; before this the whole array was dropped and the user
  // was told nothing at all.
  const handleBatch = useCallback(async () => {
    try {
      const res = await batchAccept.mutateAsync(ids);
      const results = res.items ?? [];
      const accepted = results.filter((r) => r.outcome === "accepted").length;
      const duplicates = results.filter((r) => r.outcome === "duplicate").length;
      const failed = results.length - accepted - duplicates;
      toast.success(
        t(($) => $.batch.summary_toast, { accepted, duplicates, failed }),
      );
      if (res.stopped) {
        toast.error(t(($) => $.batch.stopped_toast));
      }
      onDone();
    } catch {
      toast.error(t(($) => $.detail.error_toast));
    }
  }, [batchAccept, ids, onDone, t]);

  // A batch dismiss answers 200 with per-item outcomes too, so the summary
  // has to name what did NOT get dismissed rather than claim a clean sweep.
  const handleBatchDismiss = useCallback(async () => {
    try {
      const res = await batchDismiss.mutateAsync({
        itemIds: ids,
        reason: reason.trim() || undefined,
      });
      const results = res.items ?? [];
      const dismissed = results.filter((r) => r.outcome === "dismissed").length;
      toast.success(
        t(($) => $.batch.dismiss_summary_toast, {
          dismissed,
          failed: results.length - dismissed,
        }),
      );
      setDismissOpen(false);
      onDone();
    } catch {
      toast.error(t(($) => $.detail.error_toast));
    }
  }, [batchDismiss, ids, onDone, reason, t]);

  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-4 z-10 flex justify-center px-4">
      <div className="pointer-events-auto flex items-center gap-2 rounded-full border bg-background px-3 py-2 shadow-lg">
        <span className="text-caption text-muted-foreground">{ids.length}</span>
        <Popover open={dismissOpen} onOpenChange={setDismissOpen}>
          <PopoverTrigger
            render={
              <Button variant="outline" size="sm" disabled={busy}>
                <X aria-hidden="true" className="size-3.5" />
                {t(($) => $.batch.dismiss, { count: ids.length })}
              </Button>
            }
          />
          <PopoverContent align="center" className="w-72">
            <div className="flex flex-col gap-2">
              <p className="text-caption text-muted-foreground">
                {t(($) => $.batch.dismiss_summary, { count: ids.length })}
              </p>
              <Input
                aria-label={t(($) => $.dismiss_reason.label)}
                value={reason}
                placeholder={t(($) => $.dismiss_reason.placeholder)}
                onChange={(e) => setReason(e.target.value)}
              />
              <Button size="sm" onClick={handleBatchDismiss} disabled={batchDismiss.isPending}>
                {batchDismiss.isPending ? (
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                ) : null}
                {t(($) => $.dismiss_reason.confirm)}
              </Button>
            </div>
          </PopoverContent>
        </Popover>
        <Button size="sm" onClick={handleBatch} disabled={busy}>
          {batchAccept.isPending ? (
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
          ) : (
            <Check aria-hidden="true" className="size-3.5" />
          )}
          {t(($) => $.batch.accept, { count: ids.length })}
        </Button>
        <Button variant="ghost" size="sm" onClick={onDone} disabled={busy}>
          {t(($) => $.batch.clear)}
        </Button>
      </div>
    </div>
  );
}
