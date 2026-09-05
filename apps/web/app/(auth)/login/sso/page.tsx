"use client";

import { SSOCallbackPage } from "@multica/views/auth";
import { setLoggedInCookie } from "@/features/auth/auth-cookie";

// OIDC redirect target (K60). The shared page reads `code` and `state` from
// the location itself, so no search-params plumbing is needed here.
export default function Page() {
  return <SSOCallbackPage onTokenObtained={setLoggedInCookie} />;
}
