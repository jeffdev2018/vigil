// @vitest-environment node
import { beforeAll, describe, expect, it, vi } from "vitest";

// The module under test imports the ApiClient (which throws at load time
// without EXPO_PUBLIC_API_URL) and the workspace store; both are mocked the
// same way data/mutations/delivery.test.ts does it, so only the pure
// failure-copy helpers are exercised.
vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: () => null,
  getCurrentSlug: () => null,
}));

class FakeApiError extends Error {
  status: number;
  body?: unknown;
  constructor(status: number, body?: unknown) {
    super("network");
    this.status = status;
    this.body = body;
  }
}

/**
 * The Brain write failures a person can actually hit, and what each one says.
 * These matter because three of them are NOT "something went wrong":
 *
 *   - organize 409 → someone (or an agent) organized the capture first;
 *     retrying fails identically, so the copy must not invite a retry.
 *   - suggest 503  → no model is configured. Nothing broke; the answer is
 *     "organize it yourself".
 *   - note 409     → the revision guard fired. The user's draft is built on
 *     a version that no longer exists and has to be re-applied.
 *
 * Canonical layer for this copy; the screens only spread the tuple into
 * `Alert.alert`.
 */
describe("Brain write failure copy", () => {
  let organizeFailure: typeof import("./brain").organizeFailure;
  let suggestFailure: typeof import("./brain").suggestFailure;
  let reopenFailure: typeof import("./brain").reopenFailure;
  let noteWriteFailure: typeof import("./brain").noteWriteFailure;

  beforeAll(async () => {
    const mod = await import("./brain");
    organizeFailure = mod.organizeFailure;
    suggestFailure = mod.suggestFailure;
    reopenFailure = mod.reopenFailure;
    noteWriteFailure = mod.noteWriteFailure;
  });

  it("reads a 409 on organize as 'someone got there first'", () => {
    const [title, body] = organizeFailure(new FakeApiError(409), "discard");
    expect(title).toBe("Already organized");
    expect(body).toMatch(/first/i);
  });

  it("reads a 404 on merge as a note that left the workspace", () => {
    expect(organizeFailure(new FakeApiError(404), "merge")[0]).toBe(
      "Note not found",
    );
  });

  it("surfaces the server's own sentence for any other organize failure", () => {
    // The Go handlers answer {"error": "..."} — the size ceiling on merge is
    // the case that matters, and its sentence tells the user what to do.
    const err = new FakeApiError(400, {
      error: "the note would exceed its size limit; create a new note instead",
    });
    const [title, body] = organizeFailure(err, "merge");
    expect(title).toBe("Could not merge the capture");
    expect(body).toContain("create a new note instead");
  });

  it("treats a 503 on suggest as a notice, not a failure", () => {
    const [title, body] = suggestFailure(new FakeApiError(503));
    expect(title).toBe("No model configured");
    expect(body).toMatch(/yourself/i);
  });

  it("treats a 502 on suggest as the model failing", () => {
    expect(suggestFailure(new FakeApiError(502))[0]).toBe(
      "The model could not answer",
    );
  });

  it("reads a 409 on reopen as already reopened", () => {
    expect(reopenFailure(new FakeApiError(409))[0]).toBe(
      "Already back in the inbox",
    );
  });

  it("reads a 409 on a note write as the revision guard", () => {
    const [title, body] = noteWriteFailure(new FakeApiError(409), "save");
    expect(title).toBe("Changed while you were editing");
    expect(body).toMatch(/re-apply/i);
  });

  it("reads a 403 on delete as the narrower delete permission", () => {
    const err = new FakeApiError(403, {
      error: "only a workspace admin or the note author can delete this note",
    });
    const [title, body] = noteWriteFailure(err, "delete");
    expect(title).toBe("Not allowed");
    // The server's own sentence wins over the generic fallback.
    expect(body).toContain("note author");
  });

  it("falls back to a generic sentence for an error with no status", () => {
    const [title, body] = noteWriteFailure(new Error("boom"), "save");
    expect(title).toBe("Could not save the note");
    expect(body).toBe("boom");
  });
});
