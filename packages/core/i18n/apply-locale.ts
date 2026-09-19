import type { i18n as I18n } from "i18next";
import { HTML_LANG, type LocaleResources, type SupportedLocale } from "./types";

// Switches a live i18n instance to `locale` without reloading the page: loads
// the locale's bundles, changes language (react-i18next re-renders every
// subscriber), and keeps <html lang> in step for fonts and screen readers.
// The caller has already persisted the choice, so the next boot agrees.
export async function applyLocale(
  instance: I18n,
  locale: SupportedLocale,
  resources: LocaleResources,
): Promise<void> {
  for (const [namespace, bundle] of Object.entries(resources)) {
    instance.addResourceBundle(locale, namespace, bundle, true, true);
  }
  await instance.changeLanguage(locale);
  if (typeof document !== "undefined") {
    document.documentElement.lang = HTML_LANG[locale];
  }
}
