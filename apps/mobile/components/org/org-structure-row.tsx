/**
 * Org structure row (K75). Read-only. Mirrors `GoalRow`'s layout shape
 * (flex title + right column + secondary line) and folds the unit list
 * under a `Collapsible` like `chat-timeline.tsx` does for process steps.
 *
 * Layout:
 *   Structure name                                   r3
 *   [Owner network] [Active] [Workspace default]     ▸ 4 units
 *     ▸ Unit name · auto · 3 members · paused · owner
 */
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { OrgStructure, OrgUnit } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { agentListOptions } from "@/data/queries/agents";
import { memberListOptions } from "@/data/queries/members";
import { useWorkspaceStore } from "@/data/workspace-store";
import { orgIsLive, orgModelLabel, orgStatusLabel } from "@/lib/org-display";

interface Props {
  structure: OrgStructure;
  /** Resolved project name; null means "Workspace default" (project_id null). */
  projectName: string | null;
}

export function OrgStructureRow({ structure, projectName }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  // Unit owner_id is untyped on the server (member user id or agent id):
  // look it up in both workspace lists, raw id as the last resort.
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const ownerName = (id: string) =>
    members.find((m) => m.user_id === id)?.name ??
    agents.find((a) => a.id === id)?.name ??
    id;

  const units = structure.definition?.units ?? [];
  const paused = new Set(structure.paused_units ?? []);
  const scope = projectName ?? "Workspace default";

  return (
    <View className="px-4 py-3 gap-1.5">
      <View className="flex-row items-start gap-3">
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={2}
          >
            {structure.name || "Untitled structure"}
          </Text>
          <View className="flex-row flex-wrap items-center gap-x-3 gap-y-1">
            <Text className="text-xs text-muted-foreground">
              {orgModelLabel(structure.model)}
            </Text>
            <Text
              className={
                orgIsLive(structure)
                  ? "text-xs text-brand font-medium"
                  : "text-xs text-muted-foreground"
              }
            >
              {orgStatusLabel(structure.status)}
            </Text>
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {scope}
            </Text>
          </View>
        </View>
        <Text className="text-xs text-muted-foreground tabular-nums">
          r{structure.revision}
        </Text>
      </View>
      {units.length === 0 ? (
        <Text className="text-xs text-muted-foreground/60">No units</Text>
      ) : (
        <Collapsible>
          <CollapsibleTrigger asChild>
            <View
              accessibilityRole="button"
              accessibilityLabel={`${units.length} unit${units.length === 1 ? "" : "s"}`}
              className="active:opacity-70"
            >
              <Text className="text-xs text-muted-foreground">
                {units.length} unit{units.length === 1 ? "" : "s"} ▸
              </Text>
            </View>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <View className="mt-1 rounded-lg border border-border bg-muted/20 px-3 py-2 gap-2">
              {units.map((u) => (
                <UnitLine
                  key={u.id}
                  unit={u}
                  paused={paused.has(u.id)}
                  owner={u.owner_id ? ownerName(u.owner_id) : null}
                />
              ))}
            </View>
          </CollapsibleContent>
        </Collapsible>
      )}
    </View>
  );
}

function UnitLine({
  unit,
  paused,
  owner,
}: {
  unit: OrgUnit;
  paused: boolean;
  owner: string | null;
}) {
  const members = unit.members?.length ?? 0;
  return (
    <View className="gap-0.5">
      <View className="flex-row items-center gap-2">
        <Text
          className="text-sm text-foreground font-medium flex-1"
          numberOfLines={1}
        >
          {unit.name || unit.id}
        </Text>
        {paused ? (
          <Text className="text-[11px] text-destructive font-medium">
            paused
          </Text>
        ) : null}
      </View>
      <Text className="text-xs text-muted-foreground" numberOfLines={1}>
        {unit.autonomy} · {members} member{members === 1 ? "" : "s"}
        {owner ? ` · ${owner}` : ""}
      </Text>
    </View>
  );
}
