// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { OrgSelect } from "./org-select";

it("searches a long list by label and assigns its identity from the keyboard", async () => {
  const change = vi.fn();
  const user = userEvent.setup();
  const items = Array.from({ length: 12 }, (_, i) => ({ value: `agent-${i}`, label: `Agent ${i}` }));
  renderWithI18n(<OrgSelect aria-label="Person" value="agent-0" items={items} onValueChange={change} />);
  await user.click(screen.getByRole("combobox", { name: "Person" }));
  await user.type(screen.getByRole("combobox", { name: "Find a person or agent" }), "Agent 11");
  expect(screen.getByRole("option", { name: "Agent 11" })).toBeVisible();
  expect(screen.queryByRole("option", { name: "Agent 3" })).not.toBeInTheDocument();
  await user.keyboard("{Enter}");
  expect(change).toHaveBeenCalledWith("agent-11");
  expect(screen.queryByRole("option")).not.toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Person" })).toHaveFocus();
});
