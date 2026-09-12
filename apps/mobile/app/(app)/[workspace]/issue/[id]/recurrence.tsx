/**
 * "Make it recurring" (OS plan, table stakes) — pick how often the issue is
 * filed again.
 *
 * A formSheet route: a form with a keyboard (the custom-cron box), per the
 * container table in apps/mobile/CLAUDE.md Lesson 5. Self-contained per
 * Lesson 5 rule 3 — it reads the rule out of the TanStack Query cache, calls
 * its own mutation and `router.back()`s rather than handing a callback up to
 * the issue screen.
 *
 * Six choices cover what the contract lists as the suggested presets
 * (daily / weekdays / weekly + weekday / monthly + day / custom cron / when
 * closed). The time is a native UIDatePicker in `time` mode — the iOS-native
 * rung of the component waterfall — and the timezone defaults to the device's
 * own but stays editable, because "every Monday at 9" can legitimately mean
 * 9 o'clock somewhere the person filing it does not live.
 *
 * Awaits the server before dismissing: the 400 (bad cron / unknown zone) and
 * the 403 (only a member sets a recurrence) have to be readable in place.
 */
import { useMemo, useState } from "react";
import { ActionSheetIOS, Pressable, ScrollView, View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { issueRecurrenceOptions } from "@/data/queries/recurrence";
import { useSetIssueRecurrence } from "@/data/mutations/recurrence";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  DEFAULT_RECURRENCE_DRAFT,
  RECURRENCE_PRESETS,
  WEEKDAY_NAMES,
  cronError,
  cronToDraft,
  cronSentence,
  deviceTimezone,
  ordinal,
  presetToCron,
  presetToMode,
  recurrenceSetError,
  type RecurrenceDraft,
} from "@/lib/recurrence-display";
import { cn } from "@/lib/utils";

export default function IssueRecurrenceSheet() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data } = useQuery(issueRecurrenceOptions(wsId, id));
  const save = useSetIssueRecurrence(id);

  const existing = data?.recurrence ?? null;
  // Seeded once: re-seeding on every cache change would yank the form out
  // from under the user when a realtime event lands mid-edit.
  const [draft, setDraft] = useState<RecurrenceDraft>(() =>
    existing
      ? cronToDraft(existing.cron_expression, existing.mode)
      : DEFAULT_RECURRENCE_DRAFT,
  );
  const [timezone, setTimezone] = useState(
    () => existing?.timezone || deviceTimezone(),
  );
  const [enabled, setEnabled] = useState(() => existing?.enabled ?? true);
  const [serverError, setServerError] = useState<string | null>(null);

  const patch = (over: Partial<RecurrenceDraft>) =>
    setDraft((current) => ({ ...current, ...over }));

  const cron = presetToCron(draft);
  const mode = presetToMode(draft.preset);
  const isScheduled = mode === "schedule";

  // The picker only carries an hour and a minute; the date half is a throwaway
  // anchor so `DateTimePicker` has a Date to render.
  const timeValue = useMemo(() => {
    const d = new Date();
    d.setHours(draft.hour, draft.minute, 0, 0);
    return d;
  }, [draft.hour, draft.minute]);

  const cronBlocker = isScheduled ? cronError(cron) : null;
  const tzBlocker =
    timezone.trim() === "" ? "Enter a timezone, or use the device's." : null;
  const blocker = cronBlocker ?? tzBlocker;

  const submit = () => {
    if (blocker || save.isPending) return;
    setServerError(null);
    save.mutate(
      {
        // "" for on_close, which is exactly what the server accepts for it.
        cron_expression: cron,
        timezone: timezone.trim(),
        mode,
        enabled,
      },
      {
        onSuccess: () => router.back(),
        onError: (e) => setServerError(recurrenceSetError(e)),
      },
    );
  };

  const pickWeekday = () => {
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title: "Which day?",
        options: [...WEEKDAY_NAMES, "Cancel"],
        cancelButtonIndex: WEEKDAY_NAMES.length,
      },
      (index) => {
        if (index < WEEKDAY_NAMES.length) patch({ weekday: index });
      },
    );
  };

  const pickMonthDay = () => {
    const days = Array.from({ length: 28 }, (_, i) => ordinal(i + 1));
    ActionSheetIOS.showActionSheetWithOptions(
      {
        // 29–31 are deliberately absent: a cron on the 31st silently skips
        // every short month, which reads as a broken rule rather than a rule
        // the user chose. Custom cron is still there for anyone who means it.
        title: "Which day of the month?",
        options: [...days, "Cancel"],
        cancelButtonIndex: days.length,
      },
      (index) => {
        if (index < days.length) patch({ monthDay: index + 1 });
      },
    );
  };

  return (
    <ScrollView
      className="flex-1"
      contentContainerClassName="pb-8"
      stickyHeaderIndices={[0]}
      keyboardShouldPersistTaps="handled"
    >
      <View className="flex-row items-center justify-between bg-background px-4 pb-2 pt-4">
        <Text className="text-base font-semibold text-foreground">
          {existing ? "Edit recurrence" : "Make it recurring"}
        </Text>
        <Pressable
          onPress={submit}
          disabled={!!blocker || save.isPending}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel="Save this recurrence"
          className={cn(
            "rounded-md px-3 py-1.5",
            blocker || save.isPending ? "opacity-50" : "active:bg-secondary",
          )}
        >
          <Text className="text-sm font-semibold text-primary">
            {save.isPending ? "Saving…" : "Save"}
          </Text>
        </Pressable>
      </View>

      <View className="gap-4 px-4 pt-2">
        <View className="flex-row flex-wrap gap-2">
          {RECURRENCE_PRESETS.map((preset) => (
            <Chip
              key={preset.id}
              label={preset.label}
              selected={draft.preset === preset.id}
              onPress={() => patch({ preset: preset.id })}
            />
          ))}
        </View>

        {draft.preset === "weekly" ? (
          <Row
            label="Day"
            value={WEEKDAY_NAMES[draft.weekday]}
            onPress={pickWeekday}
            accessibilityLabel="Pick the day of the week"
          />
        ) : null}

        {draft.preset === "monthly" ? (
          <Row
            label="Day of the month"
            value={ordinal(draft.monthDay)}
            onPress={pickMonthDay}
            accessibilityLabel="Pick the day of the month"
          />
        ) : null}

        {draft.preset === "custom" ? (
          <View className="gap-1">
            <Text className="text-xs text-muted-foreground">
              Cron expression
            </Text>
            <Input
              value={draft.customCron}
              onChangeText={(next) => patch({ customCron: next })}
              placeholder="0 9 * * 1"
              placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
              autoCapitalize="none"
              autoCorrect={false}
              accessibilityLabel="Cron expression"
            />
            <Text className="text-xs text-muted-foreground">
              minute hour day month weekday
            </Text>
          </View>
        ) : null}

        {isScheduled && draft.preset !== "custom" ? (
          <View className="flex-row items-center justify-between">
            <Text className="text-sm text-muted-foreground">At</Text>
            <DateTimePicker
              value={timeValue}
              mode="time"
              display="compact"
              onChange={(_event, selected) => {
                if (selected) {
                  patch({
                    hour: selected.getHours(),
                    minute: selected.getMinutes(),
                  });
                }
              }}
            />
          </View>
        ) : null}

        {isScheduled ? (
          <View className="gap-1">
            <Text className="text-xs text-muted-foreground">Timezone</Text>
            <Input
              value={timezone}
              onChangeText={setTimezone}
              placeholder="Europe/Paris"
              placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
              autoCapitalize="none"
              autoCorrect={false}
              accessibilityLabel="IANA timezone"
            />
            {/* The device's own zone is the default; naming it makes the
                field editable without making it a chore. */}
            {timezone.trim() !== deviceTimezone() ? (
              <Pressable
                onPress={() => setTimezone(deviceTimezone())}
                accessibilityLabel="Use this device's timezone"
                className="self-start pt-1"
              >
                <Text className="text-xs text-primary">
                  {`Use ${deviceTimezone()}`}
                </Text>
              </Pressable>
            ) : null}
          </View>
        ) : null}

        <View className="flex-row items-center justify-between">
          <View className="flex-1 min-w-0 pr-3">
            <Text className="text-sm text-foreground">Active</Text>
            <Text className="text-xs text-muted-foreground">
              Off keeps the rule but files nothing.
            </Text>
          </View>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </View>

        <View className="gap-1 rounded-md border border-border px-3 py-2.5">
          <Text className="text-xs uppercase tracking-wider text-muted-foreground">
            What this means
          </Text>
          <Text className="text-sm text-foreground">
            {draft.preset === "on_close"
              ? "The next one is filed when the current issue reaches a done or cancelled status."
              : cronBlocker
                ? "—"
                : `${cronSentence(cron)} · ${timezone.trim() || "UTC"}`}
          </Text>
          {isScheduled && !cronBlocker ? (
            <Text className="text-xs text-muted-foreground">{cron}</Text>
          ) : null}
        </View>

        {/* A recurrence copies the issue as it stands at each run: title,
            description with its checkboxes reset, priority, assignee,
            project, labels, properties, acceptance criteria, and the due
            date shifted to keep the same lead. Worth saying once — it is
            what makes the rule useful and what surprises people. */}
        <Text className="text-xs text-muted-foreground">
          Each new issue copies this one — title, description, priority,
          assignee, project and labels — with its checkboxes reset and its due
          date shifted by the same lead.
        </Text>

        {cronBlocker ? (
          <Text className="text-xs text-destructive">{cronBlocker}</Text>
        ) : null}
        {tzBlocker ? (
          <Text className="text-xs text-destructive">{tzBlocker}</Text>
        ) : null}
        {/* The server's 400 names the field it refused and the 403 names the
            rule — both shown verbatim, in place, because retrying the same
            request is not the answer. */}
        {serverError ? (
          <Text className="text-xs text-destructive">{serverError}</Text>
        ) : null}
      </View>
    </ScrollView>
  );
}

function Row({
  label,
  value,
  onPress,
  accessibilityLabel,
}: {
  label: string;
  value: string;
  onPress: () => void;
  accessibilityLabel: string;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      className="flex-row items-center justify-between rounded-md border border-border px-3 py-2.5 active:bg-secondary"
    >
      <Text className="text-sm text-muted-foreground">{label}</Text>
      <Text className="text-sm text-foreground">{value}</Text>
    </Pressable>
  );
}

/** Same chip as the follow-up sheet's: selected reads on border + weight,
 *  which press does not touch (UI rule "active state stays identifiable"). */
function Chip({
  label,
  selected,
  onPress,
}: {
  label: string;
  selected: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected }}
      className={cn(
        "rounded-full border px-3 py-1.5",
        selected
          ? "border-primary bg-primary/10"
          : "border-border active:bg-secondary",
      )}
    >
      <Text
        className={cn(
          "text-sm",
          selected ? "font-semibold text-primary" : "text-foreground",
        )}
      >
        {label}
      </Text>
    </Pressable>
  );
}
