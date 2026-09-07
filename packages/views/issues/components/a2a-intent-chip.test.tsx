import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { A2AIntentChip } from "./a2a-intent-chip";

// Intent recognition and recipient parsing are canonically tested in
// packages/core/issues/a2a-message.test.ts. This suite covers only what the
// component adds: the label each intent renders, the archived-recipient
// fallback, and the two ways the chip must render NOTHING.

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    // The 2222 agent resolves; the 9999 one is an agent the directory no longer
    // knows — the archived case.
    getActorName: (_type: string, id: string) => (id.startsWith("2") ? "Reviewer" : ""),
    getActorInitials: (_type: string, _id: string, name: string) => name.slice(0, 1),
    getActorAvatarUrl: () => null,
  }),
}));

// Real UUIDs: the mention markup only recognizes hex ids, exactly as the
// server writes them.
const KNOWN = "22222222-2222-2222-2222-222222222222";
const ARCHIVED = "99999999-9999-9999-9999-999999999999";
const body = (agentId: string) => `[@CodeBot](mention://agent/${agentId})\n\nplease look`;

describe("A2AIntentChip", () => {
  it("labels each intent the server can write", () => {
    for (const [intent, label] of [
      ["question", "Question for"],
      ["review", "Review requested from"],
      ["handoff", "Handoff to"],
    ] as const) {
      const { unmount } = renderWithI18n(
        <A2AIntentChip intent={intent} content={body(KNOWN)} />,
      );
      expect(screen.getByText(label)).toBeTruthy();
      expect(screen.getByText("Reviewer")).toBeTruthy();
      unmount();
    }
  });

  // The column has no CHECK, so an intent from a newer backend is a shape this
  // build WILL meet. It must fall back to an ordinary comment — the body is
  // fully rendered either way — rather than draw a chip with no label.
  it("renders nothing for an intent it cannot label", () => {
    const { container } = renderWithI18n(
      <A2AIntentChip intent="escalation" content={body(KNOWN)} />,
    );
    expect(container.textContent).toBe("");
  });

  it("renders nothing when there is no intent at all", () => {
    const { container } = renderWithI18n(<A2AIntentChip intent={null} content={body(KNOWN)} />);
    expect(container.textContent).toBe("");
  });

  // A message whose mention was edited out has no addressee left to name.
  it("renders nothing when the body carries no agent mention", () => {
    const { container } = renderWithI18n(<A2AIntentChip intent="review" content="just words" />);
    expect(container.textContent).toBe("");
  });

  // An ARCHIVED recipient drops out of the workspace directory. The chip keeps
  // the name the server wrote into the markup at send time rather than going
  // blank: who the message was for is not something archiving should erase.
  it("falls back to the name in the markup when the recipient no longer resolves", () => {
    renderWithI18n(<A2AIntentChip intent="handoff" content={body(ARCHIVED)} />);
    expect(screen.getByText("Handoff to")).toBeTruthy();
    expect(screen.getByText("CodeBot")).toBeTruthy();
  });
});
