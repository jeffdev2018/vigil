/**
 * Pack contents grouped by kind, one line per kind — mirrors web's
 * `KindList` in `packages/views/settings/components/packs-tab.tsx`.
 *
 * Two callers: the pack detail screen (what a pack would install) and the
 * installed row on the catalogue screen (what an install actually created).
 * Unknown kinds render under their raw server key rather than disappearing
 * (see lib/packs-display.ts).
 */
import { View } from "react-native";
import { Text } from "@/components/ui/text";
import { packKindLabel } from "@/lib/packs-display";

export function PackKindList({
  entries,
}: {
  entries: [string, string[]][];
}) {
  const rows = entries.filter(([, names]) => names.length > 0);
  if (rows.length === 0) return null;
  return (
    <View className="gap-1">
      {rows.map(([kind, names]) => (
        <Text key={kind} className="text-xs leading-5 text-muted-foreground">
          <Text className="text-xs font-medium text-foreground">
            {packKindLabel(kind)}
          </Text>
          {` · ${names.join(", ")}`}
        </Text>
      ))}
    </View>
  );
}
