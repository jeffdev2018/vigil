import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";

import {
  canCreateSubIssueFromComment,
  retryableAgentFailureComment,
} from "./comment-actions";

function entry(overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id: "c1",
    actor_type: "member",
    actor_id: "u1",
    created_at: "2026-01-01T00:00:00Z",
    comment_type: "comment",
    ...overrides,
  };
}

describe("retryableAgentFailureComment", () => {
  it("is eligible for an agent system comment naming a failed task", () => {
    expect(
      retryableAgentFailureComment(
        entry({
          actor_type: "agent",
          comment_type: "system",
          source_task_id: "t1",
        }),
      ),
    ).toBe(true);
  });

  it("rejects a member comment even if it names a task", () => {
    expect(
      retryableAgentFailureComment(
        entry({
          actor_type: "member",
          comment_type: "system",
          source_task_id: "t1",
        }),
      ),
    ).toBe(false);
  });

  it("rejects an ordinary agent comment (not a system entry)", () => {
    expect(
      retryableAgentFailureComment(
        entry({
          actor_type: "agent",
          comment_type: "comment",
          source_task_id: "t1",
        }),
      ),
    ).toBe(false);
  });

  it("rejects a system comment with no task id", () => {
    expect(
      retryableAgentFailureComment(
        entry({ actor_type: "agent", comment_type: "system" }),
      ),
    ).toBe(false);
    expect(
      retryableAgentFailureComment(
        entry({
          actor_type: "agent",
          comment_type: "system",
          source_task_id: "",
        }),
      ),
    ).toBe(false);
  });
});

describe("canCreateSubIssueFromComment", () => {
  it("allows an ordinary comment", () => {
    expect(canCreateSubIssueFromComment(entry({ comment_type: "comment" })))
      .toBe(true);
  });

  it("rejects a system comment", () => {
    expect(canCreateSubIssueFromComment(entry({ comment_type: "system" })))
      .toBe(false);
  });
});
