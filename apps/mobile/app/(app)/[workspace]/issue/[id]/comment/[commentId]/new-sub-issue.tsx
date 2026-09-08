/**
 * Sub-issue creation anchored on a comment — web parity: `onCreateSubIssue`
 * in packages/views/issues/components/comment-card.tsx, which opens the
 * `quick-create-issue` modal in "source context" mode
 * (packages/views/modals/create-issue-dialog.tsx
 * SourceContextCreateIssueDialog). Reached from the comment long-press
 * menu's "Create Sub-issue" item — see comment-context-menu.tsx.
 *
 * Server contract: useCreateCommentSubIssue captures the anchor comment's
 * thread as source context (`GET .../sub-issue-preview` → `capture_token`)
 * and creates the sub-issue in the same call
 * (`POST /api/comments/:id/sub-issues`, manual mode). The parent issue is
 * derived server-side from the anchor comment — this screen never sends
 * `parent_issue_id` itself.
 *
 * Minimal form: title + description only, same fields new-issue.tsx
 * requires. Web's source-context dialog also offers status / priority /
 * assignee / due-date / project chips and an agent-quick-create mode.
 * Mobile skips both here:
 *   - Attribute chips reuse `useNewIssueDraftStore` +
 *     `new-issue-picker/<field>` routes, which are scoped to the top-level
 *     new-issue flow (see apps/mobile/CLAUDE.md Lesson 5 — that store
 *     exists BECAUSE those routes can't share state with the screen that
 *     opened them). Forking a parallel draft store + picker route tree for
 *     this one entry point isn't justified by a single caller; the created
 *     issue can be edited immediately after through the existing
 *     issue/[id]/picker/* sheets. The server defaults status="todo" and
 *     priority="none" when omitted, matching new-issue.tsx's own baseline.
 *   - Agent-quick-create mode requires an agent-runtime capability check
 *     mobile's create-issue flow doesn't have anywhere else (see
 *     server/internal/handler/source_context.go
 *     prepareAgentCommentSubIssue) — same manual-only scope new-issue.tsx
 *     already has.
 */
import { useCallback, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  TextInput,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { SubmitIssueButton } from "@/components/issue/submit-issue-button";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { DescriptionField } from "@/components/issue/description-field";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { useCreateCommentSubIssue } from "@/data/mutations/issues";
import { useMentionInput } from "@/lib/use-mention-input";

export default function NewSubIssueFromCommentModal() {
  const { commentId } = useLocalSearchParams<{
    id: string;
    commentId: string;
  }>();
  const [title, setTitle] = useState("");
  const description = useMentionInput();

  const createSubIssue = useCreateCommentSubIssue(commentId);
  const isSubmitting = createSubIssue.isPending;
  const canSubmit = !isSubmitting && title.trim().length > 0;

  const onSubmit = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (trimmedTitle.length === 0) return;
    const finalDescription = description.serialize().trim();
    try {
      await createSubIssue.mutateAsync({
        title: trimmedTitle,
        description: finalDescription || undefined,
      });
      router.back();
    } catch (err) {
      Alert.alert(
        "Failed to create sub-issue",
        err instanceof Error ? err.message : "Unknown error",
      );
    }
  }, [title, description, createSubIssue]);

  const headerRight = useCallback(
    () => (
      <SubmitIssueButton
        disabled={!canSubmit}
        loading={isSubmitting}
        onPress={onSubmit}
      />
    ),
    [canSubmit, isSubmitting, onSubmit],
  );

  return (
    <>
      <Stack.Screen options={{ headerRight }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6 gap-4"
          keyboardShouldPersistTaps="handled"
        >
          <TextInput
            value={title}
            onChangeText={setTitle}
            placeholder="Sub-issue title"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            className="text-2xl font-semibold text-foreground py-2"
            autoFocus
            returnKeyType="next"
            editable={!isSubmitting}
          />
          <DescriptionField
            description={description}
            disabled={isSubmitting}
          />
        </ScrollView>

        {/* Mention suggestions float above the keyboard only when the user
            types `@`. Self-hides via `if (!visible) return null` so it
            doesn't take space at rest. */}
        <MentionSuggestionBar {...description.suggestionBar} />
      </KeyboardAvoidingView>
    </>
  );
}
