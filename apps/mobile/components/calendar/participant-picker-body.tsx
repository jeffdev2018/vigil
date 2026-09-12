/**
 * Multi-select participant picker for a calendar event draft — members and
 * agents (no squads: `CalendarParticipant.type` is `member | agent` only,
 * server/internal/handler/calendar_events.go `calendarParticipantInput`).
 *
 * Structurally mirrors `AssigneePickerBody` (same member/agent lists,
 * `ActorAvatar`, alphabetical sort, search-filter query prop) per
 * apps/mobile/CLAUDE.md UI Principle 1 ("existing pattern first") — but
 * that picker is single-select with an "Unassigned" row, which doesn't fit
 * a participant list (there is no "no participants" affordance and an
 * event has none-to-many attendees), so it's a small new component rather
 * than an extension of AssigneePickerBody's props.
 */
import { useMemo } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { useColorScheme } from "nativewind";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useScrollToTopOnChange } from "@/lib/use-scroll-to-top-on-change";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";
import type { CalendarParticipantValue } from "@/data/stores/new-event-draft-store";

const AVATAR_SIZE = 36;

type Row =
  | { kind: "member"; member: MemberWithUser }
  | { kind: "agent"; agent: Agent };

function rowValue(row: Row): CalendarParticipantValue {
  return row.kind === "member"
    ? { type: "member", id: row.member.user_id }
    : { type: "agent", id: row.agent.id };
}

function isSelected(value: CalendarParticipantValue[], row: Row): boolean {
  const v = rowValue(row);
  return value.some((p) => p.type === v.type && p.id === v.id);
}

interface Props {
  value: CalendarParticipantValue[];
  query: string;
  onChange: (next: CalendarParticipantValue[]) => void;
}

export function ParticipantPickerBody({ value, query, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);
  const { colorScheme } = useColorScheme();
  const checkColor =
    colorScheme === "dark" ? THEME.dark.primary : THEME.light.primary;

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (name: string) => !q || name.toLowerCase().includes(q);
    const memberRows: Row[] = [...members]
      .filter((m) => matchName(m.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((m) => ({ kind: "member" as const, member: m }));
    const agentRows: Row[] = [...agents]
      .filter((a) => matchName(a.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((a) => ({ kind: "agent" as const, agent: a }));
    return [...memberRows, ...agentRows];
  }, [members, agents, query]);

  const toggle = (row: Row) => {
    const v = rowValue(row);
    const already = isSelected(value, row);
    onChange(
      already
        ? value.filter((p) => !(p.type === v.type && p.id === v.id))
        : [...value, v],
    );
  };

  return (
    <FlatList
      ref={listRef}
      data={rows}
      className="flex-1"
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
      keyExtractor={(row) =>
        row.kind === "member" ? `m:${row.member.user_id}` : `a:${row.agent.id}`
      }
      renderItem={({ item }) => {
        const selected = isSelected(value, item);
        return (
          <Pressable
            onPress={() => toggle(item)}
            className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
          >
            {item.kind === "member" ? (
              <ActorAvatar type="member" id={item.member.user_id} size={AVATAR_SIZE} />
            ) : (
              <ActorAvatar type="agent" id={item.agent.id} size={AVATAR_SIZE} />
            )}
            <Text className="flex-1 text-base text-foreground">
              {item.kind === "member" ? item.member.name : item.agent.name}
            </Text>
            {item.kind === "agent" ? (
              <Text className="text-sm text-muted-foreground">Agent</Text>
            ) : null}
            {selected ? (
              <Ionicons name="checkmark" size={20} color={checkColor} />
            ) : null}
          </Pressable>
        );
      }}
      ListEmptyComponent={
        <View className={cn("px-3 py-8 items-center")}>
          <Text className="text-sm text-muted-foreground">No matches.</Text>
        </View>
      }
    />
  );
}
