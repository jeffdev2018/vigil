// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { applyLocale } from "./apply-locale";
import { createI18n } from "./create-i18n";

describe("applyLocale", () => {
  it("switches a live instance to a locale it did not boot with, and the document language with it", async () => {
    const instance = createI18n("en", { en: { settings: { title: "Language" } } });
    expect(instance.t("title", { ns: "settings" })).toBe("Language");

    await applyLocale(instance, "fr", { settings: { title: "Langue" } });

    expect(instance.language).toBe("fr");
    expect(instance.t("title", { ns: "settings" })).toBe("Langue");
    expect(document.documentElement.lang).toBe("fr-FR");
  });
});
