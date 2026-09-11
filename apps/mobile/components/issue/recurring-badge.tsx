import { View } from "react-native";
import Ionicons from "@expo/vector-icons/Ionicons";
import { Text } from "@/components/ui/text";

/**
 * Marks an issue that belongs to a recurrence series (source or occurrence)
 * on list rows. The detail's Recurrence section carries the rule itself.
 */
export function RecurringBadge() {
  return (
    <View
      accessibilityLabel="Recurring"
      className="flex-row items-center gap-1 shrink-0 rounded-full bg-secondary/60 px-1.5 py-0.5"
    >
      <Ionicons name="repeat" size={10} color="#71717a" />
      <Text className="text-[10px] text-muted-foreground">Recurring</Text>
    </View>
  );
}
