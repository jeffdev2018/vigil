/**
 * Swipe-to-review wrapper for a Decision Card (K36).
 *
 * Copied from `swipeable-inbox-row.tsx` (apps/mobile/CLAUDE.md Lesson 6 says
 * to copy that pattern rather than reinvent one) with one difference: two
 * sides instead of one.
 *
 *   - swipe RIGHT reveals the leading "Answer <label>" action — the
 *     recommended option, or the first one when the server recommended none.
 *     iOS puts the constructive action on the leading edge (Mail's
 *     Mark-as-read); the committing one on the trailing edge.
 *   - swipe LEFT reveals the trailing "Options" action, which opens the
 *     action sheet. It never answers by itself.
 *
 * **Reveal only, no auto-fire**, on BOTH sides. A decision answer is recorded
 * once and handed to a run — irreversible in the same sense Lesson 6 cares
 * about — so the drag reveals and the tap commits. That is also what removes
 * the need for an undo window: nothing is sent until the user taps.
 *
 * A medium haptic fires once per side when the drag crosses the action
 * width, wired through `useAnimatedReaction` + `runOnJS` because the shared
 * value lives on the UI thread and Haptics is JS-only.
 */
import { useRef } from "react";
import { Pressable, View } from "react-native";
import Animated, {
  type SharedValue,
  useAnimatedReaction,
  runOnJS,
} from "react-native-reanimated";
import ReanimatedSwipeable, {
  type SwipeableMethods,
} from "react-native-gesture-handler/ReanimatedSwipeable";
import { Ionicons } from "@expo/vector-icons";
import { useTheme } from "@react-navigation/native";
import * as Haptics from "expo-haptics";
import { Text } from "@/components/ui/text";
import { cn } from "@/lib/utils";

const ACTION_WIDTH = 96;

interface Props {
  /**
   * Label of the option a right swipe answers with. `null` hides the
   * leading action entirely — an option-less card can only be answered
   * with free text, through the trailing sheet.
   */
  answerLabel: string | null;
  onAnswer: () => void;
  onOptions: () => void;
  disabled?: boolean;
  children: React.ReactNode;
}

export function SwipeableDecisionCard({
  answerLabel,
  onAnswer,
  onOptions,
  disabled = false,
  children,
}: Props) {
  const ref = useRef<SwipeableMethods>(null);
  // Same source as `components/ui/icon-button.tsx`: the navigation theme's
  // foreground, so the trailing glyph flips with dark mode on its own.
  const { colors } = useTheme();

  // Close before firing so the spring doesn't fight the card leaving the
  // FlatList when the answer's invalidate lands.
  const fire = (run: () => void) => () => {
    ref.current?.close();
    run();
  };

  return (
    <ReanimatedSwipeable
      ref={ref}
      enabled={!disabled}
      friction={2}
      leftThreshold={ACTION_WIDTH}
      rightThreshold={ACTION_WIDTH}
      renderLeftActions={
        answerLabel === null
          ? undefined
          : (_progress, drag) => (
              <SwipeAction
                onPress={fire(onAnswer)}
                drag={drag}
                edge="leading"
                icon="checkmark"
                label={answerLabel}
                className="bg-success"
                textClassName="text-white"
                iconColor="white"
              />
            )
      }
      renderRightActions={(_progress, drag) => (
        <SwipeAction
          onPress={fire(onOptions)}
          drag={drag}
          edge="trailing"
          icon="ellipsis-horizontal"
          label="Options"
          className="bg-secondary"
          textClassName="text-secondary-foreground"
          iconColor={colors.text}
        />
      )}
    >
      {children}
    </ReanimatedSwipeable>
  );
}

function SwipeAction({
  onPress,
  drag,
  edge,
  icon,
  label,
  className,
  textClassName,
  iconColor,
}: {
  onPress: () => void;
  drag: SharedValue<number>;
  edge: "leading" | "trailing";
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  className: string;
  textClassName: string;
  iconColor: string;
}) {
  // drag is positive while a leading action opens and negative while a
  // trailing one does, so each edge watches its own sign.
  useAnimatedReaction(
    () =>
      edge === "leading"
        ? drag.value >= ACTION_WIDTH
        : drag.value <= -ACTION_WIDTH,
    (crossed, prev) => {
      if (crossed && !prev) {
        runOnJS(Haptics.impactAsync)(Haptics.ImpactFeedbackStyle.Medium);
      }
    },
    [edge],
  );

  return (
    <Animated.View style={{ width: ACTION_WIDTH }}>
      <Pressable
        onPress={onPress}
        accessibilityLabel={label}
        className={cn("flex-1 items-center justify-center px-2", className)}
      >
        <View className="items-center gap-0.5">
          <Ionicons name={icon} size={20} color={iconColor} />
          <Text className={cn("text-xs", textClassName)} numberOfLines={2}>
            {label}
          </Text>
        </View>
      </Pressable>
    </Animated.View>
  );
}
