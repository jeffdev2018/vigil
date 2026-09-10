// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { TaskMemoryContext } from "@multica/core/types/agent";
import { expect, it } from "vitest";
import agents from "../../locales/en/agents.json";
import { MemoryContextDetails } from "./memory-context-details";

const version = { id: "11111111-1111-4111-8111-111111111111", revision: 4 };
const loaded: TaskMemoryContext = {
  dispatched_at: "2026-09-05T10:00:00Z",
  agent_status: "loaded",
  agent_versions: [version],
  project_version: version,
};

function mount(context: TaskMemoryContext | undefined) {
  return (
    <I18nProvider locale="en" resources={{ en: { agents } }}>
      <MemoryContextDetails context={context} />
    </I18nProvider>
  );
}

it("lists the prepared versions and never claims the agent followed them", async () => {
  const user = userEvent.setup();
  render(mount(loaded));
  await user.click(screen.getByText("Memory prepared for this run"));

  expect(screen.getByText("Agent memories included: 1")).toBeTruthy();
  expect(screen.getByText(`${version.id} · r4`)).toBeTruthy();
  expect(screen.getByText("Project memory · revision 4")).toBeTruthy();
  expect(
    screen.getByText(/does not prove the agent received or followed/),
  ).toBeTruthy();
});

// The three "nothing to show" shapes mean different things and must not
// collapse into one message: a run the server never recorded, a read that
// failed, and a successful read that found no memory.
it.each([
  [undefined, "Memory context was not recorded."],
  [
    { ...loaded, agent_status: "unavailable", agent_versions: [], project_version: null },
    "Agent memory could not be loaded.",
  ],
  [
    { ...loaded, agent_versions: [], project_version: null },
    "Agent memories included: 0",
  ],
] satisfies [TaskMemoryContext | undefined, string][])(
  "distinguishes an empty selection from a failed read and an unrecorded run: %#",
  (context, expected) => {
    render(mount(context));
    expect(screen.getByText(expected)).toBeTruthy();
  },
);

it("states that no project rules were included rather than omitting the line", () => {
  render(mount({ ...loaded, project_version: null }));
  expect(screen.getByText("No project memory rules included.")).toBeTruthy();
});
