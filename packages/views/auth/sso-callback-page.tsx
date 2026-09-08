"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

/**
 * Landing page of the OIDC redirect (K60): exchanges `code` + `state` from the
 * query string for a session, then opens the workspace the login was for.
 * Reads `window.location.search` directly so the page needs no router API.
 */
export function SSOCallbackPage({ onTokenObtained }: { onTokenObtained?: () => void }) {
  const { t } = useT("auth");
  const navigation = useNavigation();
  const [error, setError] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code") ?? "";
    const state = params.get("state") ?? "";
    if (!code || !state) {
      setError(t(($) => $.sso.callback.missing_params));
      return;
    }
    let cancelled = false;
    api
      .completeOIDCLogin(code, state)
      .then(async ({ token, workspace_slug }) => {
        await useAuthStore.getState().loginWithToken(token);
        if (cancelled) return;
        onTokenObtained?.();
        navigation.replace(workspace_slug ? `/${workspace_slug}` : "/");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error && err.message ? err.message : t(($) => $.sso.callback.failed));
      });
    return () => {
      cancelled = true;
    };
    // Runs once: the query string does not change while this page is mounted.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="flex min-h-svh items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-display-sm">
            {error ? t(($) => $.sso.callback.failed_title) : t(($) => $.sso.callback.completing)}
          </CardTitle>
          <CardDescription>
            {error || t(($) => $.sso.callback.completing_description)}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex justify-center">
          {error ? (
            <AppLink href="/login" className="text-body underline underline-offset-4">
              {t(($) => $.sso.callback.back_to_login)}
            </AppLink>
          ) : (
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
