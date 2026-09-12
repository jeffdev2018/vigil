"use client";

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { issueKeys } from "@multica/core/issues/queries";
import { useWelcomeStore } from "@multica/core/onboarding";
import { paths, useCurrentWorkspace } from "@multica/core/paths";
import type { CreateIssueRequest, Issue } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import {
  FIRST_RUN_ISSUE_BODY,
  FIRST_RUN_ISSUE_TITLE,
  INSTALL_RUNTIME_ISSUE_BODY,
  INSTALL_RUNTIME_ISSUE_TITLE,
  pickContentLang,
} from "../onboarding/templates";

/**
 * One-shot welcome experience for the two runtime-step exits that need
 * setup work done AFTER the onboarding screen has already unmounted:
 * "skip" (no runtime connected) and "native" (OS plan, chantier 5 — the
 * browser-based runtime, whose first run replaces the install guide).
 *
 * Every other runtime-connected exit (CLI, desktop) creates Mika and opens
 * her chat directly from onboarding-flow and never writes this signal.
 */
export function WelcomeAfterOnboarding() {
  const me = useAuthStore((state) => state.user);
  const currentWorkspace = useCurrentWorkspace();
  const signal = useWelcomeStore((state) => state.signal);
  const dismissed = useWelcomeStore((state) => state.dismissed);
  const dismiss = useWelcomeStore((state) => state.dismiss);

  // The store is global while this component is workspace-scoped. Wait
  // until the matching workspace is visible before seeding guide issues.
  if (
    !me ||
    !signal ||
    dismissed ||
    !currentWorkspace ||
    currentWorkspace.id !== signal.workspaceId
  ) {
    return null;
  }

  if (signal.choice === "native") {
    return (
      <NativeFirstRunWelcome
        workspaceId={signal.workspaceId}
        agentId={signal.agentId}
        onDismiss={dismiss}
      />
    );
  }

  return (
    <SkipWelcome
      workspaceId={signal.workspaceId}
      onDismiss={dismiss}
    />
  );
}

/**
 * Module-level dedupe keeps React StrictMode double-mounts from racing
 * identical issue or comment creation requests.
 */
const pendingIssueSeed = new Map<string, Promise<Issue>>();

function seedIssueDeduped(
  cacheKey: string,
  body: CreateIssueRequest,
): Promise<Issue> {
  const existing = pendingIssueSeed.get(cacheKey);
  if (existing) return existing;

  const promise = api.createIssue(body);
  pendingIssueSeed.set(cacheKey, promise);
  promise
    .finally(() => {
      if (pendingIssueSeed.get(cacheKey) === promise) {
        pendingIssueSeed.delete(cacheKey);
      }
    })
    .catch(() => {});
  return promise;
}

interface SkipBundle {
  installIssueId: string;
}

interface SkipWelcomeProps {
  workspaceId: string;
  onDismiss: () => void;
}

/**
 * Provision one focused runtime guide before showing the completion modal.
 * Once a runtime appears, the Runtimes page offers "Start with Mika" and
 * runs the same real bootstrap used by connected onboarding.
 */
function SkipWelcome({ workspaceId, onDismiss }: SkipWelcomeProps) {
  const { t, i18n } = useT("onboarding");
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const me = useAuthStore((state) => state.user);

  const [bundle, setBundle] = useState<SkipBundle | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!me || bundle || failed) return;

    let cancelled = false;
    void (async () => {
      try {
        const lang = pickContentLang(i18n.language);
        const installRuntime = await seedIssueDeduped(
          `${workspaceId}:install-runtime`,
          {
            title: INSTALL_RUNTIME_ISSUE_TITLE[lang],
            description: INSTALL_RUNTIME_ISSUE_BODY[lang],
            status: "in_progress",
            priority: "high",
            assignee_type: "member",
            assignee_id: me.id,
          },
        );
        void queryClient.invalidateQueries({
          queryKey: issueKeys.all(workspaceId),
        });
        if (!cancelled) {
          setBundle({ installIssueId: installRuntime.id });
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [bundle, failed, i18n.language, me, queryClient, workspaceId]);

  if (!me) return null;

  // A failure used to dismiss the surface silently. Nothing recovered it: the
  // welcome signal is deliberately not persisted, onboarding is already marked
  // complete, and seedIssueDeduped's cache is one in-flight promise — so a
  // blip left the member with no guide issue, no message, and no way back.
  // Retry re-runs the effect (the `failed` guard is what gates it), and
  // dismissing is now a choice rather than the default.
  if (failed) {
    return (
      <Dialog
        open={true}
        modal={true}
        onOpenChange={(open) => {
          if (!open) onDismiss();
        }}
      >
        <DialogContent className="max-w-md sm:max-w-md">
          <DialogTitle className="text-title font-semibold">
            {t(($) => $.welcome_after_onboarding.skip.error_title)}
          </DialogTitle>
          <DialogDescription className="text-body text-muted-foreground">
            {t(($) => $.welcome_after_onboarding.skip.error_body)}
          </DialogDescription>
          <div className="mt-6 flex justify-end gap-2">
            <Button variant="ghost" onClick={onDismiss}>
              {t(($) => $.welcome_after_onboarding.skip.dismiss)}
            </Button>
            <Button onClick={() => setFailed(false)}>
              {t(($) => $.welcome_after_onboarding.skip.retry)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  if (!bundle) {
    return (
      <FullScreenLoading
        label={t(($) => $.welcome_after_onboarding.skip.loading)}
      />
    );
  }

  const handleGotIt = async () => {
    onDismiss();
    const slug = await resolveWorkspaceSlug(queryClient, workspaceId);
    navigation.push(paths.workspace(slug).issueDetail(bundle.installIssueId));
  };

  return (
    <Dialog
      open={true}
      modal={true}
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <DialogContent
        className="max-w-xl sm:max-w-xl"
        aria-describedby="welcome-after-onboarding-skip-subtitle"
      >
        <div className="flex flex-col items-center gap-4 pt-6">
          <div className="text-6xl animate-welcome-emoji-pop" aria-hidden>
            🎉
          </div>
          <DialogTitle className="text-center text-display-sm font-semibold">
            {t(($) => $.welcome_after_onboarding.skip.title)}
          </DialogTitle>
          <DialogDescription
            id="welcome-after-onboarding-skip-subtitle"
            className="text-center text-body text-muted-foreground max-w-md"
          >
            {t(($) => $.welcome_after_onboarding.skip.subtitle)}
          </DialogDescription>
        </div>

        <div className="mt-6 flex flex-col gap-2">
          <SkipPreviewCard
            cardKey="install_runtime"
            statusLabel={t(
              ($) => $.welcome_after_onboarding.skip.status_in_progress,
            )}
          />
        </div>

        <div className="mt-6 flex justify-end">
          <Button size="lg" onClick={handleGotIt}>
            {t(($) => $.welcome_after_onboarding.skip.got_it)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SkipPreviewCard({
  cardKey,
  statusLabel,
}: {
  cardKey: "install_runtime";
  statusLabel: string;
}) {
  const { t } = useT("onboarding");

  return (
    <div className="flex items-start gap-3 rounded-lg border bg-background px-3 py-2.5">
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-body font-medium leading-tight">
            {t(($) => $.welcome_after_onboarding.skip.cards[cardKey].title)}
          </p>
          <span
            className="rounded-full bg-primary/10 px-2 py-0.5 text-micro font-medium text-primary"
          >
            {statusLabel}
          </span>
        </div>
        <p className="mt-1 text-caption text-muted-foreground leading-snug">
          {t(($) => $.welcome_after_onboarding.skip.cards[cardKey].subtitle)}
        </p>
      </div>
    </div>
  );
}

interface NativeFirstRunWelcomeProps {
  workspaceId: string;
  /** Mika's agent id on the workspace's native runtime, from the bootstrap
   *  that already ran in onboarding-flow's handleRuntimeNext. */
  agentId: string;
  onDismiss: () => void;
}

/**
 * Native runtime path (OS plan, chantier 5): seed the "Your first run"
 * issue, assigned to Mika, then navigate straight there — no completion
 * modal like `SkipWelcome`'s. Mika already has a working runtime, so
 * assigning the issue starts a real run immediately; the whole point is
 * for the person to land on it and watch it happen, not read a "got it"
 * screen first.
 */
function NativeFirstRunWelcome({
  workspaceId,
  agentId,
  onDismiss,
}: NativeFirstRunWelcomeProps) {
  const { t, i18n } = useT("onboarding");
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const me = useAuthStore((state) => state.user);

  const [failed, setFailed] = useState(false);
  const [navigating, setNavigating] = useState(false);

  useEffect(() => {
    if (!me || failed || navigating) return;

    let cancelled = false;
    void (async () => {
      try {
        const lang = pickContentLang(i18n.language);
        const firstRun = await seedIssueDeduped(
          `${workspaceId}:first-run`,
          {
            title: FIRST_RUN_ISSUE_TITLE[lang],
            description: FIRST_RUN_ISSUE_BODY[lang],
            status: "todo",
            priority: "high",
            assignee_type: "agent",
            assignee_id: agentId,
          },
        );
        void queryClient.invalidateQueries({
          queryKey: issueKeys.all(workspaceId),
        });
        if (cancelled) return;
        setNavigating(true);
        const slug = await resolveWorkspaceSlug(queryClient, workspaceId);
        if (cancelled) return;
        onDismiss();
        navigation.push(paths.workspace(slug).issueDetail(firstRun.id));
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [agentId, failed, i18n.language, me, navigating, onDismiss, navigation, queryClient, workspaceId]);

  if (!me) return null;

  // Same "offer a retry, never dismiss silently" contract as SkipWelcome's
  // failure state: onboarding is already marked complete and there is no
  // other surface pointing back at this.
  if (failed) {
    return (
      <Dialog
        open={true}
        modal={true}
        onOpenChange={(open) => {
          if (!open) onDismiss();
        }}
      >
        <DialogContent className="max-w-md sm:max-w-md">
          <DialogTitle className="text-title font-semibold">
            {t(($) => $.welcome_after_onboarding.native.error_title)}
          </DialogTitle>
          <DialogDescription className="text-body text-muted-foreground">
            {t(($) => $.welcome_after_onboarding.native.error_body)}
          </DialogDescription>
          <div className="mt-6 flex justify-end gap-2">
            <Button variant="ghost" onClick={onDismiss}>
              {t(($) => $.welcome_after_onboarding.native.dismiss)}
            </Button>
            <Button onClick={() => setFailed(false)}>
              {t(($) => $.welcome_after_onboarding.native.retry)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <FullScreenLoading
      label={t(($) => $.welcome_after_onboarding.native.loading)}
    />
  );
}

function FullScreenLoading({ label }: { label: string }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="flex flex-col items-center gap-3">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <p className="text-body text-muted-foreground">{label}</p>
      </div>
    </div>
  );
}

async function resolveWorkspaceSlug(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceId: string,
): Promise<string> {
  const cached = queryClient
    .getQueriesData<{ id: string; slug: string }[] | undefined>({
      queryKey: workspaceKeys.list(),
    })
    .map(([, data]) => data)
    .find(Boolean);
  const hit = cached?.find((workspace) => workspace.id === workspaceId);
  if (hit) return hit.slug;

  const workspace = await api.getWorkspace(workspaceId);
  return workspace.slug;
}
