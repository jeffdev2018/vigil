import { useEffect } from "react";
import { Stack, Redirect } from "expo-router";
import { useAuthStore } from "@/data/auth-store";
import { registerForPush } from "@/lib/push";

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
  if (!user) return <Redirect href="/login" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
