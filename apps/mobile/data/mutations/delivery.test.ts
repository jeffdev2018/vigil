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

describe("delivery mutation error copy", () => {
  let deliverySaveErrorMessage: typeof import("./delivery").deliverySaveErrorMessage;
  let deliveryCorrectionErrorMessage: typeof import("./delivery").deliveryCorrectionErrorMessage;

  beforeAll(async () => {
    ({ deliverySaveErrorMessage, deliveryCorrectionErrorMessage } =
      await import("./delivery"));
  });

  it("surfaces 409 review conflicts separately from generic failures", () => {
    expect(deliverySaveErrorMessage(new FakeApiError(409))).toMatch(
      /delivery or review changed/i,
    );
    expect(deliverySaveErrorMessage(new Error("net"))).toMatch(
      /Could not confirm the save/,
    );
  });

  it("keeps correction failures honest about the saved review", () => {
    expect(deliveryCorrectionErrorMessage(new FakeApiError(409))).toMatch(
      /review is saved/i,
    );
    expect(deliveryCorrectionErrorMessage(new Error("net"))).toMatch(
      /will not create a second correction run/i,
    );
  });
});
