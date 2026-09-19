// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nextProvider } from "react-i18next";
import { LocaleAdapterProvider } from "./adapter-context";
import { createI18n } from "./create-i18n";
import type { LocaleAdapter } from "./types";
import { UserLocaleSync } from "./user-locale-sync";

const reload = vi.fn();
const auth = vi.hoisted(() => ({ user: null as { id: string; language: string } | null }));

vi.mock("../auth", () => {
  const state = () => ({ user: auth.user });
  return {
    useAuthStore: Object.assign((selector: (s: ReturnType<typeof state>) => unknown) => selector(state()), { getState: state }),
  };
});

function mount(choice: string | null, userLanguage: string | null) {
  const persist = vi.fn();
  const adapter: LocaleAdapter = {
    getUserChoice: () => choice,
    getSystemPreferences: () => [],
    persist,
  };
  auth.user = userLanguage ? { id: "u1", language: userLanguage } : null;
  render(
    <I18nextProvider i18n={createI18n("en", { en: {} })}>
      <LocaleAdapterProvider adapter={adapter}>
        <UserLocaleSync />
      </LocaleAdapterProvider>
    </I18nextProvider>,
  );
  return { persist };
}

beforeEach(() => {
  reload.mockReset();
  Object.defineProperty(window, "location", { configurable: true, writable: true, value: { reload } });
});

afterEach(() => {
  cleanup();
  auth.user = null;
});

describe("UserLocaleSync", () => {
  it("never reloads a device that already chose its language when the account language changes elsewhere", () => {
    const { persist } = mount("en", "fr");
    expect(persist).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();
  });

  it("gives a new device the account language as its default", () => {
    const { persist } = mount(null, "fr");
    expect(persist).toHaveBeenCalledWith("fr");
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("records the account language on a new device already showing it, without reloading", () => {
    const { persist } = mount(null, "en");
    expect(persist).toHaveBeenCalledWith("en");
    expect(reload).not.toHaveBeenCalled();
  });

  it("ignores an unsupported account language", () => {
    const { persist } = mount(null, "de");
    expect(persist).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();
  });
});
