"use client";

import { useState } from "react";
import { Copy, KeyRound, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useRevokePluginToken, useRotatePluginToken } from "@multica/core/plugins";
import type { PluginTokenIssue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../i18n";

/**
 * The installation token (`mpi_…`) lets the plugin's own server call back into
 * the workspace, and issuing it is the only way to get the webhook signing
 * secret (`whsec_…`). Neither is stored readable: both are shown once, right
 * after they are issued. Issuing a new pair invalidates the previous one, so
 * the button asks first.
 */
export function PluginTokenPanel({
  wsId,
  installationId,
  canManage,
}: {
  wsId: string;
  installationId: string;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const rotate = useRotatePluginToken(wsId);
  const revoke = useRevokePluginToken(wsId);
  const [confirm, setConfirm] = useState<"rotate" | "revoke" | null>(null);
  const [issued, setIssued] = useState<PluginTokenIssue | null>(null);
  const busy = rotate.isPending || revoke.isPending;

  const fail = (error: unknown) =>
    toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));

  const run = () => {
    const action = confirm;
    setConfirm(null);
    if (action === "rotate") {
      rotate.mutateAsync(installationId).then((result) => {
        if (!result.token) {
          toast.error(t(($) => $.plugins.token.issue_failed));
          return;
        }
        setIssued(result);
      }).catch(fail);
    } else if (action === "revoke") {
      revoke.mutateAsync(installationId).then(() => toast.success(t(($) => $.plugins.token.revoked))).catch(fail);
    }
  };

  const copy = async (value: string) => {
    if (await copyText(value)) toast.success(t(($) => $.plugins.token.copied));
    else toast.error(t(($) => $.plugins.token.copy_failed));
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-caption font-medium">
        <KeyRound className="size-4" />
        {t(($) => $.plugins.token.title)}
      </div>
      <p className="max-w-2xl text-caption text-muted-foreground">{t(($) => $.plugins.token.description)}</p>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={!canManage || busy} onClick={() => setConfirm("rotate")}>
          {rotate.isPending ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.plugins.token.issue)}
        </Button>
        <Button size="sm" variant="ghost" disabled={!canManage || busy} onClick={() => setConfirm("revoke")}>
          {t(($) => $.plugins.token.revoke)}
        </Button>
      </div>

      <AlertDialog open={confirm !== null} onOpenChange={(open) => { if (!open) setConfirm(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm === "revoke" ? t(($) => $.plugins.token.revoke_title) : t(($) => $.plugins.token.issue_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "revoke" ? t(($) => $.plugins.token.revoke_body) : t(($) => $.plugins.token.issue_body)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.plugins.token.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={run}>
              {confirm === "revoke" ? t(($) => $.plugins.token.revoke) : t(($) => $.plugins.token.issue)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={issued !== null} onOpenChange={(open) => { if (!open) setIssued(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.plugins.token.issued_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.plugins.token.issued_body)}</DialogDescription>
          </DialogHeader>
          {issued ? (
            <dl className="space-y-3">
              {([
                ["token", issued.token],
                ["signing_secret", issued.signing_secret],
              ] as const).map(([key, value]) => (
                <div key={key} className="space-y-1">
                  <dt className="text-caption font-medium">
                    {key === "token" ? t(($) => $.plugins.token.token_label) : t(($) => $.plugins.token.secret_label)}
                  </dt>
                  <dd className="flex items-center gap-2">
                    <code className="min-w-0 flex-1 break-all rounded-md bg-muted px-2 py-1.5 font-mono text-caption">{value}</code>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t(($) => $.plugins.token.copy)}
                      onClick={() => void copy(value)}
                    >
                      <Copy />
                    </Button>
                  </dd>
                </div>
              ))}
            </dl>
          ) : null}
          <DialogFooter>
            <Button onClick={() => setIssued(null)}>{t(($) => $.plugins.token.done)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
