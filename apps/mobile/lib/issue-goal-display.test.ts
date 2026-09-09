// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  apiErrorMessage,
  issueGoalBlockerLabel,
  issueGoalEvidencePreview,
  issueGoalFormError,
  issueGoalOutcomeLabel,
  issueGoalProgressLabel,
  issueGoalStatusLabel,
} from "./issue-goal-display";

describe("issueGoalStatusLabel", () => {
  it("labels every known status", () => {
    expect(
      ["active", "paused", "waiting_user", "satisfied", "stopped"].map(
        issueGoalStatusLabel,
      ),
    ).toEqual(["Active", "Paused", "Waiting on you", "Done", "Stopped"]);
  });

  it("falls back to the raw value for a status added server-side", () => {
    expect(issueGoalStatusLabel("archived")).toBe("archived");
  });
});

describe("issueGoalOutcomeLabel", () => {
  it("returns null when nothing has settled yet", () => {
    expect(issueGoalOutcomeLabel("")).toBeNull();
  });

  it("labels satisfied and continued", () => {
    expect(issueGoalOutcomeLabel("satisfied")).toBe("Goal satisfied");
    expect(issueGoalOutcomeLabel("continued")).toBe("Continued");
  });

  it("splits stopped:<why> into a human reason", () => {
    expect(issueGoalOutcomeLabel("stopped:stagnation")).toBe(
      "Stopped — made no further progress",
    );
  });

  it("falls back to the raw reason for a stopped:<why> not in the map", () => {
    expect(issueGoalOutcomeLabel("stopped:some_new_reason")).toBe(
      "Stopped — some_new_reason",
    );
  });

  it("falls back to the raw value for an unrecognised outcome", () => {
    expect(issueGoalOutcomeLabel("weird")).toBe("weird");
  });
});

describe("issueGoalProgressLabel", () => {
  it("formats continuation over max", () => {
    expect(
      issueGoalProgressLabel({ continuation: 2, max_continuations: 5 }),
    ).toBe("Continuation 2 / 5");
  });
});

describe("issueGoalEvidencePreview", () => {
  it("shows up to the limit and counts the rest", () => {
    const evidence = ["a", "b", "c", "d", "e"];
    expect(issueGoalEvidencePreview({ evidence })).toEqual({
      shown: ["a", "b", "c"],
      hiddenCount: 2,
    });
  });

  it("hiddenCount is 0 when everything fits", () => {
    expect(issueGoalEvidencePreview({ evidence: ["a"] })).toEqual({
      shown: ["a"],
      hiddenCount: 0,
    });
  });
});

describe("issueGoalFormError", () => {
  it("requires non-empty goal text", () => {
    expect(issueGoalFormError("", 8)).toBe("Goal is required.");
    expect(issueGoalFormError("   ", 8)).toBe("Goal is required.");
  });

  it("rejects goal text over 6000 characters", () => {
    expect(issueGoalFormError("a".repeat(6001), 8)).toBe(
      "Goal is longer than 6000 characters.",
    );
    expect(issueGoalFormError("a".repeat(6000), 8)).toBeNull();
  });

  it("rejects max_continuations outside [1, 20]", () => {
    expect(issueGoalFormError("ship it", 0)).toBe(
      "Max continuations must be between 1 and 20.",
    );
    expect(issueGoalFormError("ship it", 21)).toBe(
      "Max continuations must be between 1 and 20.",
    );
    expect(issueGoalFormError("ship it", 1.5)).toBe(
      "Max continuations must be between 1 and 20.",
    );
  });

  it("is null for a valid form", () => {
    expect(issueGoalFormError("ship it", 8)).toBeNull();
  });
});

describe("apiErrorMessage", () => {
  it("reads the server's {error} field off ApiError.body", () => {
    const err = { body: { error: "max_continuations must be between 1 and 20" } };
    expect(apiErrorMessage(err, "fallback")).toBe(
      "max_continuations must be between 1 and 20",
    );
  });

  it("falls back to Error.message when there is no body.error", () => {
    expect(apiErrorMessage(new Error("boom"), "fallback")).toBe("boom");
  });

  it("falls back to the given fallback for anything else", () => {
    expect(apiErrorMessage("not an error", "fallback")).toBe("fallback");
    expect(apiErrorMessage({ body: { error: "" } }, "fallback")).toBe("fallback");
  });
});

describe("issueGoalBlockerLabel", () => {
  it("names the known blockers and passes unknown tokens through", () => {
    expect(issueGoalBlockerLabel("missing_evidence")).toBe("Missing evidence");
    expect(issueGoalBlockerLabel("brand_new")).toBe("brand_new");
  });
});
