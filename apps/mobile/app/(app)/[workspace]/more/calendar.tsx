/**
 * Agenda screen (OS plan, chantier 19) — mobile parity target is the docs
 * contract in apps/docs/content/docs/calendar.mdx (no packages/views
 * implementation exists yet to mirror; it's being built in a separate
 * worktree in parallel — see lib/calendar-display.ts's file header).
 *
 * Renders GET /api/calendar/agenda for a 14-day window, grouped by local
 * calendar day (`buildAgendaDays`), with Previous/Next fortnight paging.
 * Row order per day: cycle band(s), events (status tone; a proposed event
 * reads "Awaits a decision"), issues due (tap → issue), meetings.
 */
import { useMemo, useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type { CalendarEventEntry, AgendaIssue, AgendaMeeting } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { StatusIcon } from "@/components/ui/status-icon";
import { calendarAgendaOptions } from "@/data/queries/calendar";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import {
  buildAgendaDays,
  enumerateLocalDays,
  formatEventTimeRange,
  localDateKey,
  statusLabel,
  type AgendaDay,
} from "@/lib/calendar-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

const WINDOW_DAYS = 14;
const DEVICE_TZ = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

function startOfLocalDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

export default function CalendarAgendaScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [anchor, setAnchor] = useState(() => startOfLocalDay(new Date()));
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];

  const from = anchor;
  const to = useMemo(() => {
    const d = new Date(anchor);
    d.setDate(d.getDate() + WINDOW_DAYS);
    return d;
  }, [anchor]);
  const windowDates = useMemo(
    () => enumerateLocalDays(anchor, WINDOW_DAYS),
    [anchor],
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    calendarAgendaOptions(wsId, from.toISOString(), to.toISOString()),
  );

  const days = useMemo(
    () =>
      buildAgendaDays(
        data ?? { events: [], issues_due: [], cycles: [], meetings: [] },
        windowDates,
        DEVICE_TZ,
      ),
    [data, windowDates],
  );

  const todayKey = localDateKey(new Date());
  const rangeLabel = `${from.toLocaleDateString(undefined, { month: "short", day: "numeric" })} – ${new Date(to.getTime() - 1).toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;

  return (
    <View className="flex-1 bg-background">
      <View className="flex-row items-center justify-between px-4 py-2 border-b border-border">
        <IconButton
          name="chevron-back"
          accessibilityLabel="Previous period"
          onPress={() =>
            setAnchor((a) => {
              const d = new Date(a);
              d.setDate(d.getDate() - WINDOW_DAYS);
              return d;
            })
          }
        />
        <Pressable onPress={() => setAnchor(startOfLocalDay(new Date()))}>
          <Text className="text-sm font-medium text-foreground">{rangeLabel}</Text>
        </Pressable>
        <IconButton
          name="chevron-forward"
          accessibilityLabel="Next period"
          onPress={() =>
            setAnchor((a) => {
              const d = new Date(a);
              d.setDate(d.getDate() + WINDOW_DAYS);
              return d;
            })
          }
        />
      </View>

      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <Text className="text-sm text-muted-foreground">Loading…</Text>
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load the agenda:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <FlatList
          data={days}
          keyExtractor={(day) => day.date}
          contentContainerClassName="pb-24"
          refreshing={isRefetching}
          onRefresh={refetch}
          renderItem={({ item }) => (
            <DayGroup day={item} isToday={item.date === todayKey} wsSlug={wsSlug} />
          )}
          ListEmptyComponent={
            <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
              Nothing on the calendar for this period.
            </Text>
          }
        />
      )}

      <Pressable
        onPress={() => wsSlug && router.push(`/${wsSlug}/new-event`)}
        accessibilityLabel="New event"
        className="absolute bottom-6 right-6 size-14 rounded-full bg-primary items-center justify-center shadow-lg"
      >
        <Ionicons name="add" size={28} color={t.primaryForeground} />
      </Pressable>
    </View>
  );
}

function DayGroup({
  day,
  isToday,
  wsSlug,
}: {
  day: AgendaDay;
  isToday: boolean;
  wsSlug: string | null;
}) {
  const hasContent =
    day.cycles.length + day.events.length + day.issuesDue.length + day.meetings.length > 0;
  if (!hasContent) return null;

  const dayLabel = new Date(`${day.date}T00:00:00`).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
  });

  return (
    <View className="px-4 pt-4">
      <Text
        className={cn(
          "text-xs font-semibold uppercase tracking-wide mb-2",
          isToday ? "text-primary" : "text-muted-foreground",
        )}
      >
        {isToday ? `Today · ${dayLabel}` : dayLabel}
      </Text>

      {day.cycles.map((cycle) => (
        <View
          key={cycle.id}
          className="mb-2 rounded-md bg-secondary/60 px-3 py-1.5"
        >
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            Cycle · {cycle.name}
          </Text>
        </View>
      ))}

      {day.events.map((event) => (
        <EventRow key={event.id} event={event} wsSlug={wsSlug} />
      ))}

      {day.issuesDue.map((issue) => (
        <IssueDueRow key={issue.id} issue={issue} wsSlug={wsSlug} />
      ))}

      {day.meetings.map((meeting) => (
        <MeetingRow key={meeting.id} meeting={meeting} />
      ))}
    </View>
  );
}

function EventRow({ event, wsSlug }: { event: CalendarEventEntry; wsSlug: string | null }) {
  const isProposed = event.status === "proposed";
  const isCancelled = event.status === "cancelled";
  return (
    <Pressable
      onPress={() => wsSlug && router.push(`/${wsSlug}/calendar-event/${event.id}`)}
      className="flex-row items-center gap-3 rounded-md px-3 py-2.5 mb-1.5 bg-secondary/40 active:bg-secondary/70"
    >
      <View
        className={cn(
          "size-2 rounded-full",
          isCancelled
            ? "bg-muted-foreground/40"
            : isProposed
              ? "bg-amber-500"
              : "bg-brand",
        )}
      />
      <View className="flex-1 min-w-0">
        <Text
          className={cn(
            "text-sm text-foreground",
            isCancelled && "line-through text-muted-foreground",
          )}
          numberOfLines={1}
        >
          {event.title}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {formatEventTimeRange(event)}
          {isProposed ? ` · ${statusLabel(event.status)}` : ""}
        </Text>
      </View>
    </Pressable>
  );
}

function IssueDueRow({ issue, wsSlug }: { issue: AgendaIssue; wsSlug: string | null }) {
  const { categoryOf, colorOf } = useIssueStatuses();
  return (
    <Pressable
      onPress={() =>
        wsSlug &&
        router.push({
          pathname: "/[workspace]/issue/[id]",
          params: { workspace: wsSlug, id: issue.id },
        })
      }
      className="flex-row items-center gap-2 px-3 py-2 active:opacity-70"
    >
      <StatusIcon
        status={issue.status}
        category={categoryOf(issue.status)}
        color={colorOf(issue.status)}
        size={14}
      />
      <Text className="flex-1 text-sm text-muted-foreground" numberOfLines={1}>
        {issue.identifier} · {issue.title}
      </Text>
      <Text className="text-xs text-muted-foreground">Due</Text>
    </Pressable>
  );
}

function MeetingRow({ meeting }: { meeting: AgendaMeeting }) {
  return (
    <View className="flex-row items-center gap-2 px-3 py-2">
      <Ionicons name="mic-outline" size={14} color="#71717a" />
      <Text className="flex-1 text-sm text-muted-foreground" numberOfLines={1}>
        {meeting.title || "Meeting"}
      </Text>
    </View>
  );
}
