/**
 * What the chat transcript should show, from the state of its read.
 *
 * The messages query defaults to `[]`, so "the read failed" and "this
 * conversation has no message yet" arrive at the component as the same empty
 * array — and the conversation-starter empty state then claims a session with
 * a history is brand new. Deciding it here keeps the three cases in one
 * testable place (the mobile vitest lane covers `lib/` and `data/`).
 *
 * Precedence:
 *  - anything already downloaded wins: a failed REFETCH must not replace a
 *    transcript the reader is looking at, which is how web treats a failed
 *    background refetch too;
 *  - then loading, so the first paint of a cold session is a spinner and not
 *    a flash of "start the conversation";
 *  - then the failure, with a retry;
 *  - only a settled, successful, empty read is the empty state.
 */
export type ChatMessageListState = "messages" | "loading" | "error" | "empty";

export function chatMessageListState(input: {
  messageCount: number;
  loading: boolean;
  failed: boolean;
}): ChatMessageListState {
  if (input.messageCount > 0) return "messages";
  if (input.loading) return "loading";
  if (input.failed) return "error";
  return "empty";
}
