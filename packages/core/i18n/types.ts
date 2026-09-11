export type SupportedLocale = "en" | "zh-Hans" | "ko" | "ja" | "fr";

export const SUPPORTED_LOCALES: SupportedLocale[] = ["en", "zh-Hans", "ko", "ja", "fr"];
export const DEFAULT_LOCALE: SupportedLocale = "en";

// BCP-47 region tags for <html lang>, widely recognized by screen readers and
// font stacks. i18next keeps zh-Hans internally because that is the resource
// key, while the document uses zh-CN for accessibility and CJK fallback (the
// Japanese-scoped CJK font override keys on `html[lang|="ja"]`).
export const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
  fr: "fr-FR",
};

export type LocaleResources = Record<string, Record<string, unknown>>;

export interface LocaleAdapter {
  getUserChoice(): string | null;
  getSystemPreferences(): string[];
  persist(locale: SupportedLocale): void;
}
