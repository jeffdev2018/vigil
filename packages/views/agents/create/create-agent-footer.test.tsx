// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import { CreateAgentFooter } from "./create-agent-footer";

afterEach(cleanup);

function renderFooter(props: Partial<Parameters<typeof CreateAgentFooter>[0]>) {
  return render(
    <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
      <CreateAgentFooter
        canCreate={false}
        creating={false}
        squad={false}
        error={null}
        onCreate={vi.fn()}
        {...props}
      />
    </I18nProvider>,
  );
}

describe("CreateAgentFooter", () => {
  it("explains a standing block so a disabled button is not a dead end", () => {
    renderFooter({ hint: "No runtimes yet" });
    expect(screen.getByText("No runtimes yet")).toBeTruthy();
  });

  it("does not announce a hint as an alert: nothing has failed yet", () => {
    renderFooter({ hint: "No runtimes yet" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("lets a submit error win over the standing hint", () => {
    renderFooter({ error: "Name already taken", hint: "No runtimes yet" });
    expect(screen.getByRole("alert").textContent).toBe("Name already taken");
    expect(screen.queryByText("No runtimes yet")).toBeNull();
  });
});
