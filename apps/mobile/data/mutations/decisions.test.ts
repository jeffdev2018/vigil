// @vitest-environment node
import { beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: () => null,
  getCurrentSlug: () => null,
}));

class FakeApiError extends Error {
  status: number;
  constructor(status: number) {
    super("x");
    this.status = status;
  }
}

describe("decision mutation error copy", () => {
  let decisionAnswerErrorMessage: typeof import("./decisions").decisionAnswerErrorMessage;
  let decisionResumeErrorMessage: typeof import("./decisions").decisionResumeErrorMessage;

  beforeAll(async () => {
    ({ decisionAnswerErrorMessage, decisionResumeErrorMessage } = await import(
      "./decisions"
    ));
  });

  it("surfaces 409 answer conflicts separately from generic failures", () => {
    expect(decisionAnswerErrorMessage(new FakeApiError(409))).toMatch(
      /already has a final response/,
    );
    expect(decisionAnswerErrorMessage(new Error("net"))).toMatch(
      /could not be confirmed/,
    );
  });

  it("keeps resume 409/403 distinct from a generic retry", () => {
    expect(decisionResumeErrorMessage(new FakeApiError(409))).toMatch(
      /Finish the source run/,
    );
    expect(decisionResumeErrorMessage(new FakeApiError(403))).toMatch(
      /do not have permission/,
    );
    expect(decisionResumeErrorMessage(new Error("net"))).toMatch(
      /could not be confirmed/,
    );
  });
});
