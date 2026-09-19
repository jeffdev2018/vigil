// @vitest-environment node
import { describe, expect, it } from "vitest";
import { chatMessageListState } from "./chat-list-state";

describe("chatMessageListState", () => {
  it("shows the transcript as soon as there is one", () => {
    expect(
      chatMessageListState({ messageCount: 3, loading: false, failed: false }),
    ).toBe("messages");
  });

  // A failed refetch must not swallow what the reader is already looking at.
  it("keeps the transcript when a refetch fails behind it", () => {
    expect(
      chatMessageListState({ messageCount: 3, loading: false, failed: true }),
    ).toBe("messages");
  });

  it("shows the spinner on a cold session, not the empty state", () => {
    expect(
      chatMessageListState({ messageCount: 0, loading: true, failed: false }),
    ).toBe("loading");
  });

  // The regression: the read defaults to [], so a refused or offline read used
  // to render the conversation-starter empty state over a session that has a
  // history.
  it("reports a failed read instead of claiming the conversation is new", () => {
    expect(
      chatMessageListState({ messageCount: 0, loading: false, failed: true }),
    ).toBe("error");
  });

  it("is the empty state only for a settled, successful, empty read", () => {
    expect(
      chatMessageListState({ messageCount: 0, loading: false, failed: false }),
    ).toBe("empty");
  });
});
