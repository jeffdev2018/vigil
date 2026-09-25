"use client";

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../auth";
import { useLocaleAdapter } from "./adapter-context";
import { SUPPORTED_LOCALES, type SupportedLocale } from "./types";

// The server-stored `user.language` is the default language of a device that
// has never chosen one: signing in on a new browser or desktop install picks
// up the account's language instead of the system's. A device that already
// holds a choice keeps it — a language change made on another device must
// never reload a device in use (the Settings switcher applies the change in
// place on the device where it is made).
//
// Mounts inside CoreProvider so it has access to the auth store + locale
// adapter + i18n instance. Renders nothing.
//
// Loop safety: the first run persists the account language, so every later
// run sees a user choice and no-ops; the reload only fires on that first run
// and only when the active language differs.
export function UserLocaleSync() {
  const userLanguage = useAuthStore((s) => s.user?.language ?? null);
  const adapter = useLocaleAdapter();
  const { i18n } = useTranslation();

  useEffect(() => {
    if (!userLanguage) return;
    if (!(SUPPORTED_LOCALES as readonly string[]).includes(userLanguage)) {
      return;
    }
    if (adapter.getUserChoice()) return;
    adapter.persist(userLanguage as SupportedLocale);
    if (userLanguage === i18n.language) return;
    if (typeof window !== "undefined") window.location.reload();
  }, [userLanguage, i18n.language, adapter]);

  return null;
}
