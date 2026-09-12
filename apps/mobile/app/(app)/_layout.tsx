import { useEffect } from "react";
import { Stack, Redirect } from "expo-router";
import { useAuthStore } from "@/data/auth-store";
import { registerForPush } from "@/lib/push";
import { usePushResponseRouting } from "@/lib/use-push-response";

/**
 * Auth-required layout. Redirects to /login when no user is loaded.
 *
 * Workspace membership is enforced one level deeper at [workspace]/_layout —
 * not here — because select-workspace.tsx itself is auth-required but
 * workspace-less.
 */
export default function AppLayout() {
  const user = useAuthStore((s) => s.user);
  // Mobile push (K64): register this device once the user is known.
  const userId = user?.id;
  useEffect(() => {
    if (userId) void registerForPush();
  }, [userId]);
  // Tap-to-answer (JEF-244): route notification taps to the decisions screen.
  usePushResponseRouting();
  if (!user) return <Redirect href="/login" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
