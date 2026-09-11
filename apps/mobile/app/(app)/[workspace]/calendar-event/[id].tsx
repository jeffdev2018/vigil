/**
 * Calendar event detail sheet (OS plan, chantier 19). Own body header
 * (SHEET_OPTIONS sets `headerShown: false`, same as inbox/[id].tsx) —
 * title, participants + responses, Accept / Tentative / Decline for the
 * current member, Cancel for the event's creator, a link to the issue it
 * serves.
 */
import { ActivityIndicator, Alert, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { calendarEventOptions } from "@/data/queries/calendar";
import { useCancelCalendarEvent, useRespondCalendarEvent } from "@/data/mutations/calendar";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { formatEventTimeRange, responseLabel, statusLabel } from "@/lib/calendar-display";
import { cn } from "@/lib/utils";

export default function CalendarEventDetailSheet() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const myId = useAuthStore((s) => s.user?.id);
  const { data: event, isLoading } = useQuery(calendarEventOptions(wsId, id ?? null));
  const respond = useRespondCalendarEvent();
  const cancelEvent = useCancelCalendarEvent();

  const myParticipant = event?.participants.find(
    (p) => p.type === "member" && p.id === myId,
  );
  const isCreator = event?.created_by.type === "member" && event.created_by.id === myId;
  const isCancelled = event?.status === "cancelled";

  const onRespond = (response: "accepted" | "declined" | "tentative") => {
    if (!event) return;
    respond.mutate({ id: event.id, response });
  };

  const onCancel = () => {
    if (!event) return;
    Alert.alert(
      "Cancel this event?",
      "Participants keep seeing it, marked cancelled.",
      [
        { text: "Keep event", style: "cancel" },
        {
          text: "Cancel event",
          style: "destructive",
          onPress: () =>
            cancelEvent.mutate(event.id, {
              onError: (err) =>
                Alert.alert(
                  "Could not cancel the event.",
                  err instanceof Error ? err.message : "unknown error",
                ),
            }),
        },
      ],
    );
  };

  // One ScrollView with the header pinned as its first child: as a sibling
  // above a ScrollView the formSheet drew the body over the title (JEF-398).
  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="pb-8"
      stickyHeaderIndices={[0]}
      showsVerticalScrollIndicator={false}
    >
      <View className="flex-row items-center border-b border-border bg-background px-4 py-3">
        <Text className="flex-1 text-lg font-semibold text-foreground" numberOfLines={1}>
          {event?.title ?? "Event"}
        </Text>
        <IconButton
          name="close"
          variant="secondary"
          className="size-7 rounded-full"
          onPress={() => router.back()}
          accessibilityLabel="Close event"
        />
      </View>

      {isLoading ? (
        <View className="items-center justify-center py-16">
          <ActivityIndicator />
        </View>
      ) : !event ? (
        <View className="px-4 py-8">
          <Text className="text-sm text-muted-foreground text-center">
            This event is no longer available.
          </Text>
        </View>
      ) : (
        <View className="gap-5 px-4 py-5">
          <View className="gap-1">
            <Text
              className={cn(
                "text-base text-foreground",
                isCancelled && "line-through text-muted-foreground",
              )}
            >
              {formatEventTimeRange(event)}
            </Text>
            {event.location ? (
              <Text className="text-sm text-muted-foreground">{event.location}</Text>
            ) : null}
            {event.status !== "scheduled" ? (
              <Text
                className={cn(
                  "text-sm",
                  event.status === "proposed" ? "text-amber-600" : "text-muted-foreground",
                )}
              >
                {statusLabel(event.status)}
              </Text>
            ) : null}
          </View>

          {event.description ? (
            <Text className="text-sm leading-5 text-foreground">{event.description}</Text>
          ) : null}

          {event.issue_id ? (
            <Button
              variant="outline"
              onPress={() =>
                wsSlug &&
                router.push({
                  pathname: "/[workspace]/issue/[id]",
                  params: { workspace: wsSlug, id: event.issue_id! },
                })
              }
            >
              <Text>View {event.issue_identifier || "issue"}</Text>
            </Button>
          ) : null}

          <View className="gap-2">
            <Text className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Participants
            </Text>
            {event.participants.length === 0 ? (
              <Text className="text-sm text-muted-foreground">No participants.</Text>
            ) : (
              event.participants.map((p) => (
                <View
                  key={`${p.type}:${p.id}`}
                  className="flex-row items-center gap-3 py-1"
                >
                  <ActorAvatar type={p.type as "member" | "agent"} id={p.id} size={28} />
                  <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
                    {p.name || p.id}
                  </Text>
                  <Text className="text-xs text-muted-foreground">
                    {responseLabel(p.response)}
                  </Text>
                </View>
              ))
            )}
          </View>

          {myParticipant && !isCancelled ? (
            <View className="flex-row gap-2">
              <Button
                className="flex-1"
                variant={myParticipant.response === "accepted" ? "default" : "outline"}
                onPress={() => onRespond("accepted")}
                disabled={respond.isPending}
              >
                <Text>Accept</Text>
              </Button>
              <Button
                className="flex-1"
                variant={myParticipant.response === "tentative" ? "default" : "outline"}
                onPress={() => onRespond("tentative")}
                disabled={respond.isPending}
              >
                <Text>Tentative</Text>
              </Button>
              <Button
                className="flex-1"
                variant={myParticipant.response === "declined" ? "destructive" : "outline"}
                onPress={() => onRespond("declined")}
                disabled={respond.isPending}
              >
                <Text>Decline</Text>
              </Button>
            </View>
          ) : null}

          {isCreator && !isCancelled ? (
            <Button variant="destructive" onPress={onCancel} disabled={cancelEvent.isPending}>
              <Text>Cancel event</Text>
            </Button>
          ) : null}
        </View>
      )}
    </ScrollView>
  );
}
