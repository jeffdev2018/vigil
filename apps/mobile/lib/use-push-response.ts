/**
 * Tap-to-answer for push notifications (K64 / JEF-244).
 *
 * Registers the Notifications response listener for the authenticated
 * session (mounted once from app/(app)/_layout.tsx, alongside push
 * registration). Taps route through the pure mapping in
 * lib/push-route.ts — see its header for the payload contract and the
 * safety rules (never crash, never navigate to a bogus route).
 *
 * Cold start: a tap that launches the app does not re-fire the listener
 * once JS is up, so getLastNotificationResponseAsync is checked on mount.
 * Both paths go through the same identifier dedupe, because on some SDK
 * versions the cold-start response is delivered to BOTH.
 */
import { useEffect } from "react";
import * as Notifications from "expo-notifications";
import { router } from "expo-router";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { queryClient } from "@/data/query-client";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { routeForNotification } from "@/lib/push-route";

let lastHandledIdentifier: string | null = null;

function handleResponse(response: Notifications.NotificationResponse): void {
  const { identifier } = response.notification.request;
  if (identifier === lastHandledIdentifier) return;
  lastHandledIdentifier = identifier;
  try {
    const route = routeForNotification(
      response.notification.request.content.data,
      {
        currentSlug: useWorkspaceStore.getState().currentWorkspaceSlug,
        workspaces: queryClient.getQueryData(workspaceListOptions().queryKey),
      },
    );
    if (route) router.push(route);
  } catch (e) {
    // A push tap must never take the app down.
    console.warn("[push] tap routing skipped", e instanceof Error ? e.message : e);
  }
}

export function usePushResponseRouting(): void {
  const userId = useAuthStore((s) => s.user?.id);
  useEffect(() => {
    if (!userId) return;
    const sub = Notifications.addNotificationResponseReceivedListener(handleResponse);
    void Notifications.getLastNotificationResponseAsync().then((response) => {
      if (response) handleResponse(response);
    });
    return () => sub.remove();
  }, [userId]);
}
