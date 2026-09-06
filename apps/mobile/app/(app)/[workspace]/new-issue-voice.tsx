/**
 * Voice-dictated issue draft (K36) — two steps in one modal.
 *
 * Step 1 "dictate": an auto-focused multiline TextInput. Dictation is the
 * iOS keyboard's own mic key, not a library — mobile CLAUDE.md's waterfall
 * puts a native platform feature first, and no speech-recognition module is
 * installed (adding one needs a native dev-client rebuild). That is also why
 * the copy says "Dictate or type": the same field serves both.
 *
 * Step 2 "draft": the server's structuring, fully editable. Title,
 * description and label chips are ordinary inputs — the model's answer is a
 * starting point, never a commitment. Only Create writes anything.
 *
 * Parity: the create goes through the same `useCreateIssue` /
 * POST /api/issues as `new-issue.tsx`, so status, permissions and duplicate
 * guards behave identically; the one difference is `origin_type:
 * "voice_mobile"` (migration 738), which records how the words were
 * captured. Labels are the workspace's own — the server drops anything the
 * model invented, and the chips only offer what `labelListOptions` returns.
 */
import { useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { AttributeChip } from "@/components/issue/attribute-chip";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { useCreateIssue, useIssueDraftFromVoice } from "@/data/mutations/issues";
import { labelListOptions } from "@/data/queries/labels";
import { useWorkspaceStore } from "@/data/workspace-store";

/**
 * Non-space characters below which Continue stays disabled. Mirrors
 * `voiceTranscriptMinChars` in server/internal/handler/issue_from_voice.go,
 * so a transcript the server would refuse never leaves the phone.
 */
const MIN_TRANSCRIPT_CHARS = 8;

const countNonSpace = (s: string) => s.replace(/\s/g, "").length;

export default function NewIssueVoice() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data: labels = [] } = useQuery(labelListOptions(wsId));

  const [transcript, setTranscript] = useState("");
  const [draft, setDraft] = useState<{ title: string; description: string } | null>(null);
  const [labelIds, setLabelIds] = useState<string[]>([]);

  const structure = useIssueDraftFromVoice();
  const createIssue = useCreateIssue();

  const canContinue =
    !structure.isPending && countNonSpace(transcript) >= MIN_TRANSCRIPT_CHARS;

  const onContinue = async () => {
    if (!canContinue) return;
    try {
      const result = await structure.mutateAsync(transcript.trim());
      setDraft({ title: result.title, description: result.description });
      // Suggested labels arrive as names; only the ones this workspace
      // actually has become selected chips.
      const byName = new Map(labels.map((l) => [l.name.toLowerCase(), l.id]));
      setLabelIds(
        result.suggested_labels
          .map((name) => byName.get(name.toLowerCase()))
          .filter((id): id is string => !!id),
      );
    } catch (err) {
      // The transcript stays on screen so the user can edit and retry
      // instead of dictating it again.
      Alert.alert(
        "Could not draft the issue",
        err instanceof Error && err.message ? err.message : "Unknown error",
      );
    }
  };

  const onCreate = async () => {
    if (!draft) return;
    const title = draft.title.trim();
    if (!title) return;
    try {
      const issue = await createIssue.mutateAsync({
        title,
        description: draft.description.trim() || undefined,
        origin_type: "voice_mobile",
        ...(labelIds.length > 0 ? { label_ids: labelIds } : {}),
      });
      // Await-then-navigate (root CLAUDE.md): a create never navigates on
      // an optimistic guess.
      router.replace({
        pathname: "/[workspace]/issue/[id]",
        params: { workspace: wsSlug ?? "", id: issue.id },
      });
    } catch (err) {
      // The draft stays editable — the user has already spoken it once.
      Alert.alert(
        "Failed to create issue",
        err instanceof Error && err.message ? err.message : "Unknown error",
      );
    }
  };

  const toggleLabel = (id: string) =>
    setLabelIds((prev) =>
      prev.includes(id) ? prev.filter((l) => l !== id) : [...prev, id],
    );

  return (
    <>
      <Stack.Screen options={{ title: draft ? "Review draft" : "Dictate an issue" }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6 gap-4"
          keyboardShouldPersistTaps="handled"
        >
          {draft === null ? (
            <DictateStep
              transcript={transcript}
              onChange={setTranscript}
              canContinue={canContinue}
              pending={structure.isPending}
              onContinue={onContinue}
            />
          ) : (
            <DraftStep
              draft={draft}
              onChange={setDraft}
              labels={labels}
              labelIds={labelIds}
              onToggleLabel={toggleLabel}
              pending={createIssue.isPending}
              onCreate={onCreate}
            />
          )}
        </ScrollView>
      </KeyboardAvoidingView>
    </>
  );
}

function DictateStep({
  transcript,
  onChange,
  canContinue,
  pending,
  onContinue,
}: {
  transcript: string;
  onChange: (v: string) => void;
  canContinue: boolean;
  pending: boolean;
  onContinue: () => void;
}) {
  return (
    <>
      <TextInput
        // autoFocus raises the keyboard on mount, one tap from the iOS mic
        // key. There is no in-app record button: the OS owns dictation.
        autoFocus
        multiline
        value={transcript}
        onChangeText={onChange}
        placeholder="Dictate or type"
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        accessibilityLabel="Dictate or type the issue"
        className="min-h-40 text-base text-foreground"
        textAlignVertical="top"
      />
      <Text className="text-xs text-muted-foreground">
        Say what needs doing. Nothing is created until you review the draft.
      </Text>
      <Button disabled={!canContinue} onPress={onContinue}>
        <Text>{pending ? "Drafting…" : "Continue"}</Text>
      </Button>
    </>
  );
}

function DraftStep({
  draft,
  onChange,
  labels,
  labelIds,
  onToggleLabel,
  pending,
  onCreate,
}: {
  draft: { title: string; description: string };
  onChange: (d: { title: string; description: string }) => void;
  labels: { id: string; name: string; color: string }[];
  labelIds: string[];
  onToggleLabel: (id: string) => void;
  pending: boolean;
  onCreate: () => void;
}) {
  const selected = useMemo(() => new Set(labelIds), [labelIds]);
  return (
    <>
      <TextInput
        autoFocus
        value={draft.title}
        onChangeText={(title) => onChange({ ...draft, title })}
        placeholder="Title"
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        accessibilityLabel="Issue title"
        className="text-xl font-semibold text-foreground"
      />
      <TextInput
        multiline
        value={draft.description}
        onChangeText={(description) => onChange({ ...draft, description })}
        placeholder="Description"
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        accessibilityLabel="Issue description"
        className="min-h-32 text-base text-foreground"
        textAlignVertical="top"
      />
      {labels.length > 0 ? (
        <View className="flex-row flex-wrap gap-2">
          {labels.map((label) => (
            <AttributeChip
              key={label.id}
              label={label.name}
              variant={selected.has(label.id) ? "filled" : "dimmed"}
              onPress={() => onToggleLabel(label.id)}
              icon={
                <View
                  className="size-2 rounded-full"
                  style={{ backgroundColor: label.color }}
                />
              }
            />
          ))}
        </View>
      ) : null}
      <Button disabled={pending || draft.title.trim().length === 0} onPress={onCreate}>
        <Text>{pending ? "Creating…" : "Create issue"}</Text>
      </Button>
    </>
  );
}
