"use client";

import { useEffect, useRef } from "react";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
  type ShortcutActionId,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/**
 * Run `handler` when the chord bound to `actionId` is pressed, unless the
 * keyboard belongs to something else: a text field, an open menu or dialog, an
 * IME composition, or an event another handler already claimed. A `null`
 * handler registers nothing to run, so a binding is inert while its action is
 * unavailable (busy mutation, wrong item state) instead of half-firing.
 *
 * Same shape as the inbox page's archive binding
 * (`packages/views/inbox/components/inbox-page.tsx`), kept as a hook because
 * the triage queue binds several actions across three components.
 */
export function useShortcutAction(
  actionId: ShortcutActionId,
  handler: (() => void) | null,
): void {
  // Keep the listener registered once while it always runs the latest handler.
  const handlerRef = useRef(handler);
  useEffect(() => {
    handlerRef.current = handler;
  });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (!shortcutMatchesEvent(getShortcut(actionId), event)) return;
      const run = handlerRef.current;
      if (!run) return;
      event.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [actionId]);
}
