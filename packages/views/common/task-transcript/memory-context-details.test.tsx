// @vitest-environment jsdom
import type { TaskMemoryContext } from "@multica/core/types/agent";
import { expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import agents from "../../locales/en/agents.json";
import { MemoryContextDetails } from "./memory-context-details";

it("shows prepared versions and keeps unavailable distinct from an empty or unrecorded context", async () => {
  const user = userEvent.setup();
  const version = { id: "11111111-1111-4111-8111-111111111111", revision: 4 };
  const context = { dispatched_at: "2026-09-05T10:00:00Z", agent_status: "loaded" as const, agent_versions: [version], project_version: version };
  const view = render(<I18nProvider locale="en" resources={{en:{agents}}}><MemoryContextDetails context={context}/></I18nProvider>);
  await user.click(screen.getByText("Memory prepared for this run"));
  expect(screen.getByText("Agent memories included: 1")).toBeTruthy();
  expect(screen.getByText(`${version.id} · r4`)).toBeTruthy();
  expect(screen.getByText("Project memory · revision 4")).toBeTruthy();
  expect(screen.getByText(/does not prove the agent received or followed/)).toBeTruthy();
  for (const [value,expected] of [
    [undefined,"Memory context was not recorded."],
    [{...context,agent_status:"unavailable" as const,agent_versions:[],project_version:null},"Agent memory could not be loaded."],
    [{...context,agent_versions:[],project_version:null},"Agent memories included: 0"],
  ] satisfies [TaskMemoryContext | undefined, string][]) {
    view.rerender(<I18nProvider locale="en" resources={{en:{agents}}}><MemoryContextDetails context={value}/></I18nProvider>);
    expect(screen.getByText(expected)).toBeTruthy();
  }
});
