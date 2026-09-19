import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { RecurringBadge } from "./recurring-badge";

// The badge itself; list-row and board-card mount it when issue.recurrence_id
// is set, which the schema test in core covers (recurrence_id parses to null
// on an older backend, so nothing shows there).
describe("RecurringBadge", () => {
  it("names the series for the eye and the screen reader", () => {
    renderWithI18n(<RecurringBadge />);
    const badge = screen.getByTestId("recurring-badge");
    expect(badge.getAttribute("aria-label")).toBe("Recurring");
    expect(badge.textContent).toContain("Recurring");
  });
});
