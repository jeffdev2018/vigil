/**
 * Doctrine version diff (OS plan, chantier 22) —
 * GET /api/workspace/doctrine/versions/{id}/diff.
 *
 * The server does the diffing (an LCS line diff over a 32 000-byte cap, see
 * `diffDoctrineLines` in server/internal/handler/workspace_doctrine.go), so
 * this screen only tints what it is handed: `add` green, `del` red, `same`
 * neutral. It never re-derives the comparison, and never guesses the base —
 * with no `against` parameter the server compares a proposal with the live
 * doctrine and an activated revision with the one before it.
 *
 * Reached from the pending-proposal card on the doctrine screen and from a
 * `doctrine_review` inbox notification carrying a `version_id`.
 */
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { doctrineVersionDiffOptions } from "@/data/queries/doctrine";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useActorLookup } from "@/data/use-actor-name";
import { doctrineVersionStatusLabel } from "@/lib/doctrine-display";
import { timeAgo } from "@/lib/time-ago";

/** Line tint. `default` covers `same` and any kind a newer server adds
 *  (root CLAUDE.md: server-driven enums need a default branch). */
function lineClasses(kind: string): { row: string; text: string; sign: string } {
  switch (kind) {
    case "add":
      return { row: "bg-success/10", text: "text-foreground", sign: "+" };
    case "del":
      return { row: "bg-destructive/10", text: "text-muted-foreground", sign: "-" };
    default:
      return { row: "", text: "text-muted-foreground", sign: " " };
  }
}

export default function DoctrineVersionDiffScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { getName } = useActorLookup();
  const { data, isLoading, error, refetch } = useQuery(
    doctrineVersionDiffOptions(wsId, id ?? null),
  );

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          Could not load the changes:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const to = data?.to ?? null;
  const from = data?.from ?? null;
  const reviewer = to?.reviewed_by ? getName("member", to.reviewed_by) : null;
  const author = to?.author_id ? getName("member", to.author_id) : null;

  const meta = [
    to ? doctrineVersionStatusLabel(to.status) : null,
    to?.revision != null ? `Revision ${to.revision}` : null,
    from?.revision != null ? `compared with revision ${from.revision}` : null,
    to?.created_at ? timeAgo(to.created_at) : null,
    author,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="pb-10"
      showsVerticalScrollIndicator={false}
    >
      <View className="gap-2 px-4 py-4">
        <Text className="text-xs text-muted-foreground">{meta}</Text>
        <Text className="text-xs text-muted-foreground">
          {`+${data?.added ?? 0} / -${data?.removed ?? 0} lines`}
          {to?.restored_from_revision != null
            ? ` · restored from revision ${to.restored_from_revision}`
            : ""}
        </Text>
        {to?.note ? (
          <Text className="text-sm leading-5 text-foreground">{to.note}</Text>
        ) : null}
        {reviewer ? (
          <Text className="text-xs text-muted-foreground">
            {`Reviewed by ${reviewer}`}
            {to?.reviewed_at ? ` · ${timeAgo(to.reviewed_at)}` : ""}
            {to?.review_note ? ` · ${to.review_note}` : ""}
          </Text>
        ) : null}
      </View>

      {/* Horizontal scroll on the diff itself: a doctrine rule can be a long
          unwrapped line, and the page body must never scroll sideways. */}
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <View className="min-w-full">
          {(data?.lines ?? []).length === 0 ? (
            <Text className="px-4 py-10 text-center text-sm text-muted-foreground">
              This version is identical to the one it is compared with.
            </Text>
          ) : (
            (data?.lines ?? []).map((line, index) => {
              const style = lineClasses(line.kind);
              return (
                <View
                  // Lines have no id and repeat freely; the index is the only
                  // stable key and the list is never reordered.
                  key={`${index}-${line.kind}`}
                  className={`flex-row px-4 py-0.5 ${style.row}`}
                >
                  <Text className="w-4 font-mono text-xs leading-5 text-muted-foreground">
                    {style.sign}
                  </Text>
                  <Text className={`font-mono text-xs leading-5 ${style.text}`}>
                    {line.text || " "}
                  </Text>
                </View>
              );
            })
          )}
        </View>
      </ScrollView>
    </ScrollView>
  );
}
