// @vitest-environment node
import { describe, expect, it } from "vitest";
import { a2aRecipientFromContent, knownA2AIntent } from "./a2a-message";

describe("knownA2AIntent", () => {
  it("accepts the three intents the server writes", () => {
    expect(knownA2AIntent("question")).toBe("question");
    expect(knownA2AIntent("review")).toBe("review");
    expect(knownA2AIntent("handoff")).toBe("handoff");
  });

  // The column has no CHECK, so an unknown value is a shape this client WILL
  // meet against a newer backend. It must degrade to an ordinary comment, never
  // to a chip with an empty label.
  it("returns null for anything it cannot label", () => {
    for (const value of ["", "gossip", "Review", "a2a_question", null, undefined]) {
      expect(knownA2AIntent(value)).toBeNull();
    }
  });
});

describe("a2aRecipientFromContent", () => {
  it("reads the recipient out of the server-composed markup", () => {
    const id = "22222222-2222-2222-2222-222222222222";
    expect(a2aRecipientFromContent(`[@CodeBot](mention://agent/${id})\n\nplease review`)).toEqual({
      agentId: id,
      label: "CodeBot",
    });
  });

  it("takes the first agent mention when the body carries several", () => {
    const first = "11111111-1111-1111-1111-111111111111";
    const second = "22222222-2222-2222-2222-222222222222";
    expect(
      a2aRecipientFromContent(
        `[@A](mention://agent/${first}) and [@B](mention://agent/${second})`,
      )?.agentId,
    ).toBe(first);
  });

  it("ignores member, squad and issue mentions", () => {
    const content =
      "[@Alice](mention://member/11111111-1111-1111-1111-111111111111) " +
      "[MUL-9](mention://issue/33333333-3333-3333-3333-333333333333)";
    expect(a2aRecipientFromContent(content)).toBeNull();
  });

  it("returns null for a body with no mention at all, and for empty input", () => {
    expect(a2aRecipientFromContent("just words")).toBeNull();
    expect(a2aRecipientFromContent("")).toBeNull();
  });
});
