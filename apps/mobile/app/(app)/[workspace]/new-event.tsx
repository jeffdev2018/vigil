/**
 * New Event modal (OS plan, chantier 19) — a member schedules a calendar
 * event directly (POST /api/calendar/events; a member always lands
 * "scheduled", see calendar_events.go CreateCalendarEvent).
 *
 * Layout follows new-issue.tsx: one vertical form, title at top, a modal
 * presentation (not a formSheet) because it's a top-level multi-screen flow
 * whose participants picker pushes a sibling formSheet on top — same
 * "multi-screen flow" bucket as new-issue in the container-selection table
 * (apps/mobile/CLAUDE.md Lesson 5).
 *
 * Start/end use the native `@react-native-community/datetimepicker` inline
 * in the form (`display="compact"`) rather than a routed picker sheet like
 * due-date-picker-body.tsx: a compact native control already opens its own
 * system popover on tap, so a dedicated formSheet route would just be a
 * second sheet wrapping the same native control for no benefit.
 */
import { useCallback, useEffect, useMemo } from "react";
import { Alert, KeyboardAvoidingView, Platform, Pressable, ScrollView, View } from "react-native";
import { Stack, router } from "expo-router";
import DateTimePicker from "@react-native-community/datetimepicker";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { Input } from "@/components/ui/input";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { useCreateCalendarEvent } from "@/data/mutations/calendar";
import { useNewEventDraftStore } from "@/data/stores/new-event-draft-store";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <View className="flex-row items-center justify-between py-2.5 border-b border-border">
      <Text className="text-sm text-foreground">{label}</Text>
      {children}
    </View>
  );
}

export default function NewEventModal() {
  const title = useNewEventDraftStore((s) => s.title);
  const setTitle = useNewEventDraftStore((s) => s.setTitle);
  const location = useNewEventDraftStore((s) => s.location);
  const setLocation = useNewEventDraftStore((s) => s.setLocation);
  const allDay = useNewEventDraftStore((s) => s.allDay);
  const setAllDay = useNewEventDraftStore((s) => s.setAllDay);
  const startsAt = useNewEventDraftStore((s) => s.startsAt);
  const setStartsAt = useNewEventDraftStore((s) => s.setStartsAt);
  const endsAt = useNewEventDraftStore((s) => s.endsAt);
  const setEndsAt = useNewEventDraftStore((s) => s.setEndsAt);
  const participants = useNewEventDraftStore((s) => s.participants);
  const resetDraft = useNewEventDraftStore((s) => s.reset);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];

  // Same "reset on mount + unmount" safety net as new-issue.tsx, on top of
  // the workspace-change reset wired in _layout.tsx.
  useEffect(() => {
    resetDraft();
    return () => {
      resetDraft();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const createEvent = useCreateCalendarEvent();
  const isSubmitting = createEvent.isPending;
  const canSubmit = !isSubmitting && title.trim().length > 0 && endsAt > startsAt;

  const participantsLabel = useMemo(() => {
    if (participants.length === 0) return "None";
    if (participants.length === 1) {
      return getName(participants[0]!.type, participants[0]!.id);
    }
    return `${participants.length} people`;
  }, [participants, getName]);

  const onSubmit = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (!trimmedTitle || !(endsAt > startsAt)) return;
    try {
      await createEvent.mutateAsync({
        title: trimmedTitle,
        starts_at: startsAt.toISOString(),
        ends_at: endsAt.toISOString(),
        all_day: allDay,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
        location: location.trim(),
        participants: participants.map((p) => ({ type: p.type, id: p.id })),
      });
      router.back();
    } catch (err) {
      Alert.alert(
        "Failed to create event",
        err instanceof Error ? err.message : "Unknown error",
      );
    }
  }, [title, startsAt, endsAt, allDay, location, participants, createEvent]);

  return (
    <>
      <Stack.Screen
        options={{
          headerRight: () => (
            <Pressable onPress={onSubmit} disabled={!canSubmit} hitSlop={8}>
              <Text
                className={canSubmit ? "text-base font-medium text-primary" : "text-base font-medium text-muted-foreground/50"}
              >
                Create
              </Text>
            </Pressable>
          ),
        }}
      />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6"
          keyboardShouldPersistTaps="handled"
        >
          <Input
            value={title}
            onChangeText={setTitle}
            placeholder="Event title"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            className="text-2xl font-semibold border-0 px-0 h-auto py-2"
            style={{ fontSize: 22 }}
            autoFocus
            editable={!isSubmitting}
          />

          <View className="mt-2">
            <Row label="All day">
              <Switch checked={allDay} onCheckedChange={setAllDay} disabled={isSubmitting} />
            </Row>
            <Row label="Starts">
              <DateTimePicker
                value={startsAt}
                mode={allDay ? "date" : "datetime"}
                display="compact"
                onChange={(_e, selected) => {
                  if (!selected) return;
                  setStartsAt(selected);
                  if (endsAt <= selected) {
                    const bumped = new Date(selected);
                    bumped.setMinutes(bumped.getMinutes() + 30);
                    setEndsAt(bumped);
                  }
                }}
              />
            </Row>
            <Row label="Ends">
              <DateTimePicker
                value={endsAt}
                mode={allDay ? "date" : "datetime"}
                display="compact"
                onChange={(_e, selected) => {
                  if (selected) setEndsAt(selected);
                }}
              />
            </Row>
            <View className="py-2.5 border-b border-border">
              <Text className="text-sm text-foreground mb-1.5">Location</Text>
              <Input
                value={location}
                onChangeText={setLocation}
                placeholder="Add a place or link"
                placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                editable={!isSubmitting}
              />
            </View>
            <Pressable
              onPress={() =>
                wsSlug && router.push(`/${wsSlug}/new-event-picker/participants`)
              }
              className="flex-row items-center justify-between py-3 border-b border-border active:opacity-70"
            >
              <Text className="text-sm text-foreground">Participants</Text>
              <View className="flex-row items-center gap-2">
                {participants.slice(0, 3).map((p) => (
                  <ActorAvatar key={`${p.type}:${p.id}`} type={p.type} id={p.id} size={22} />
                ))}
                <Text className="text-sm text-muted-foreground" numberOfLines={1}>
                  {participantsLabel}
                </Text>
                <Ionicons name="chevron-forward" size={16} color={t.mutedForeground} />
              </View>
            </Pressable>
          </View>

          {endsAt <= startsAt ? (
            <Text className="text-xs text-destructive mt-3">
              End time must be after the start time.
            </Text>
          ) : null}
        </ScrollView>
      </KeyboardAvoidingView>
    </>
  );
}
