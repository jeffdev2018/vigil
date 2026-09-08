"use client";

import { useState, type FormEvent } from "react";
import { Copy, Loader2, ShieldCheck, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
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
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { copyText } from "@multica/ui/lib/clipboard";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import {
  scimTokensOptions,
  ssoConnectionOptions,
  useCreateScimToken,
  useDeleteScimToken,
  useDeleteSSOConnection,
  usePutSSOConnection,
  useSetSSOEnforced,
  type SSOConnection,
} from "@multica/core/access";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * Security (K60): SSO over OIDC and SCIM provisioning for the workspace.
 *
 * Writes are owner-only (admins read). The OIDC client secret and the SCIM
 * token are write-only: the secret is never echoed back (leaving the field
 * blank keeps the stored one) and a SCIM token is shown exactly once, at
 * creation.
 */
function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function SecurityTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage = currentMember.role === "owner";

  const ssoQuery = useQuery(ssoConnectionOptions(wsId));
  const configured = ssoQuery.data?.configured === true;
  const connection = ssoQuery.data?.connection ?? null;

  return (
    <SettingsTab
      title={t(($) => $.security.title)}
      description={t(($) => $.security.description)}
    >
      {!canManage && !currentMember.isLoading ? (
        <p className="px-0.5 text-caption text-muted-foreground">
          {t(($) => $.security.owner_only_note)}
        </p>
      ) : null}

      {ssoQuery.data && !configured ? (
        <Alert>
          <ShieldCheck />
          <AlertTitle>{t(($) => $.security.sso.not_configured_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.security.sso.not_configured_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      {ssoQuery.isLoading ? (
        <div className="flex items-center justify-center py-8 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
        </div>
      ) : configured ? (
        <SsoSection
          key={connection?.updated_at ?? "none"}
          wsId={wsId}
          connection={connection}
          canManage={canManage}
        />
      ) : null}

      <ScimSection wsId={wsId} canManage={canManage} />
    </SettingsTab>
  );
}

function splitDomains(raw: string): string[] {
  return raw
    .split(/[\n,]/)
    .map((d) => d.trim().toLowerCase())
    .filter(Boolean);
}

function SsoSection({
  wsId,
  connection,
  canManage,
}: {
  wsId: string;
  connection: SSOConnection | null;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const put = usePutSSOConnection(wsId);
  const setEnforced = useSetSSOEnforced(wsId);
  const remove = useDeleteSSOConnection(wsId);

  const [issuer, setIssuer] = useState(connection?.issuer ?? "");
  const [clientId, setClientId] = useState(connection?.client_id ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [domains, setDomains] = useState((connection?.allowed_domains ?? []).join("\n"));
  const [autoProvision, setAutoProvision] = useState(connection?.auto_provision ?? true);
  const [confirmEnforce, setConfirmEnforce] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const readOnly = !canManage;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    try {
      // The secret is write-only: an empty field means "keep the stored one".
      await put.mutateAsync({
        issuer: issuer.trim(),
        client_id: clientId.trim(),
        ...(clientSecret ? { client_secret: clientSecret } : {}),
        allowed_domains: splitDomains(domains),
        auto_provision: autoProvision,
      });
      setClientSecret("");
      toast.success(t(($) => $.security.sso.saved_toast));
    } catch (error) {
      toast.error(errorMessage(error, t(($) => $.security.sso.save_failed_toast)));
    }
  };

  const toggleEnforce = async (enforced: boolean) => {
    try {
      await setEnforced.mutateAsync(enforced);
      setConfirmEnforce(false);
      toast.success(
        enforced
          ? t(($) => $.security.sso.enforced_toast)
          : t(($) => $.security.sso.unenforced_toast),
      );
    } catch (error) {
      toast.error(errorMessage(error, t(($) => $.security.sso.save_failed_toast)));
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.security.sso.title)}
      description={t(($) => $.security.sso.description)}
    >
      <SettingsCard>
        <form onSubmit={submit} className="grid gap-4 p-4">
          <div className="grid gap-1.5">
            <Label htmlFor="sso-issuer">{t(($) => $.security.sso.field_issuer)}</Label>
            <Input
              id="sso-issuer"
              type="url"
              value={issuer}
              onChange={(e) => setIssuer(e.target.value)}
              placeholder="https://idp.example.com"
              readOnly={readOnly}
              required
            />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.security.sso.field_issuer_hint)}
            </p>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sso-client-id">{t(($) => $.security.sso.field_client_id)}</Label>
            <Input
              id="sso-client-id"
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              readOnly={readOnly}
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sso-client-secret">{t(($) => $.security.sso.field_client_secret)}</Label>
            <Input
              id="sso-client-secret"
              type="password"
              autoComplete="off"
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
              placeholder={
                connection?.has_secret === true
                  ? t(($) => $.security.sso.field_client_secret_unchanged)
                  : undefined
              }
              readOnly={readOnly}
              required={connection?.has_secret !== true}
            />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.security.sso.field_client_secret_hint)}
            </p>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sso-domains">{t(($) => $.security.sso.field_domains)}</Label>
            <Textarea
              id="sso-domains"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              // eslint-disable-next-line no-restricted-syntax -- example domain names are a technical value, not copy
              placeholder={"example.com\nexample.org"}
              rows={3}
              readOnly={readOnly}
            />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.security.sso.field_domains_hint)}
            </p>
          </div>
          <div className="flex items-center justify-between gap-4">
            <div>
              <Label htmlFor="sso-auto-provision">{t(($) => $.security.sso.field_auto_provision)}</Label>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.security.sso.field_auto_provision_hint)}
              </p>
            </div>
            <Switch
              id="sso-auto-provision"
              checked={autoProvision}
              onCheckedChange={(v) => setAutoProvision(v)}
              disabled={readOnly}
            />
          </div>
          {canManage ? (
            <div className="flex items-center justify-end gap-2">
              {connection ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="text-destructive"
                  onClick={() => setConfirmRemove(true)}
                >
                  <Trash2 className="h-4 w-4" />
                  {t(($) => $.security.sso.remove)}
                </Button>
              ) : null}
              <Button type="submit" disabled={put.isPending || !issuer.trim() || !clientId.trim()}>
                {put.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {t(($) => $.security.sso.save)}
              </Button>
            </div>
          ) : null}
        </form>

        {connection ? (
          <div className="flex items-center justify-between gap-4 p-4">
            <div>
              <Label htmlFor="sso-enforced">{t(($) => $.security.sso.enforce)}</Label>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.security.sso.enforce_hint)}
              </p>
            </div>
            <Switch
              id="sso-enforced"
              checked={connection.enforced === true}
              onCheckedChange={(v) => {
                if (v) setConfirmEnforce(true);
                else void toggleEnforce(false);
              }}
              disabled={readOnly || setEnforced.isPending}
            />
          </div>
        ) : null}
      </SettingsCard>

      <AlertDialog open={confirmEnforce} onOpenChange={(open) => !open && setConfirmEnforce(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.security.sso.enforce_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.security.sso.enforce_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.security.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void toggleEnforce(true)} disabled={setEnforced.isPending}>
              {t(($) => $.security.sso.enforce_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmRemove} onOpenChange={(open) => !open && setConfirmRemove(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.security.sso.remove_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.security.sso.remove_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.security.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                try {
                  await remove.mutateAsync();
                  setConfirmRemove(false);
                  toast.success(t(($) => $.security.sso.removed_toast));
                } catch (error) {
                  toast.error(errorMessage(error, t(($) => $.security.sso.save_failed_toast)));
                }
              }}
              disabled={remove.isPending}
            >
              {t(($) => $.security.sso.remove_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}

function ScimSection({ wsId, canManage }: { wsId: string; canManage: boolean }) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const tokensQuery = useQuery(scimTokensOptions(wsId));
  const create = useCreateScimToken(wsId);
  const revoke = useDeleteScimToken(wsId);
  const tokens = tokensQuery.data?.tokens ?? [];
  // The raw token lives only in this state, right after creation.
  const [freshToken, setFreshToken] = useState<string | null>(null);
  const [confirmCreate, setConfirmCreate] = useState(false);
  const scimUrl = `${api.getBaseUrl()}/scim/v2`;

  const copy = async (value: string) => {
    if (await copyText(value)) toast.success(t(($) => $.security.scim.copied));
    else toast.error(t(($) => $.security.scim.copy_failed));
  };

  const generate = async () => {
    try {
      const token = await create.mutateAsync();
      setFreshToken(token.token ?? null);
      setConfirmCreate(false);
      toast.success(t(($) => $.security.scim.created_toast));
    } catch (error) {
      toast.error(errorMessage(error, t(($) => $.security.scim.create_failed_toast)));
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.security.scim.title)}
      description={t(($) => $.security.scim.description)}
      action={
        canManage ? (
          <Button
            size="sm"
            onClick={() => (tokens.some((tk) => tk.active) ? setConfirmCreate(true) : void generate())}
            disabled={create.isPending}
          >
            {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t(($) => $.security.scim.generate)}
          </Button>
        ) : null
      }
    >
      <SettingsCard>
        <div className="space-y-1.5 p-4">
          <Label className="text-caption">{t(($) => $.security.scim.endpoint_label)}</Label>
          <div className="flex items-center gap-2">
            <Input readOnly value={scimUrl} className="min-w-0 font-mono text-caption" />
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={() => void copy(scimUrl)}
              title={t(($) => $.security.scim.copy)}
            >
              <Copy className="h-3 w-3" />
            </Button>
          </div>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.security.scim.endpoint_hint)}
          </p>
        </div>

        {freshToken ? (
          <div className="space-y-1.5 p-4" data-testid="scim-fresh-token">
            <Label className="text-caption">{t(($) => $.security.scim.new_token_label)}</Label>
            <div className="flex items-center gap-2">
              <Input readOnly value={freshToken} className="min-w-0 font-mono text-caption" />
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                onClick={() => void copy(freshToken)}
                title={t(($) => $.security.scim.copy)}
              >
                <Copy className="h-3 w-3" />
              </Button>
            </div>
            <p className="text-caption text-amber-600 dark:text-amber-500">
              {t(($) => $.security.scim.shown_once)}
            </p>
          </div>
        ) : null}

        {tokensQuery.isLoading ? (
          <div className="flex items-center justify-center py-8 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
          </div>
        ) : tokens.length === 0 ? (
          <p className="px-4 py-6 text-center text-caption text-muted-foreground">
            {t(($) => $.security.scim.empty)}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.security.scim.columns.token)}</TableHead>
                  <TableHead>{t(($) => $.security.scim.columns.status)}</TableHead>
                  <TableHead>{t(($) => $.security.scim.columns.created)}</TableHead>
                  <TableHead>{t(($) => $.security.scim.columns.last_used)}</TableHead>
                  {canManage ? <TableHead /> : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {tokens.map((tk) => (
                  <TableRow key={tk.id}>
                    <TableCell className="font-mono text-caption">{tk.token_hint}</TableCell>
                    <TableCell>
                      <Badge variant={tk.active ? "default" : "secondary"}>
                        {tk.active
                          ? t(($) => $.security.scim.status_active)
                          : t(($) => $.security.scim.status_revoked)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-caption text-muted-foreground">
                      {tk.created_at ? timeAgo(tk.created_at) : "—"}
                    </TableCell>
                    <TableCell className="text-caption text-muted-foreground">
                      {tk.last_used_at
                        ? timeAgo(tk.last_used_at)
                        : t(($) => $.security.scim.never_used)}
                    </TableCell>
                    {canManage ? (
                      <TableCell className="text-right">
                        {tk.active ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-destructive"
                            onClick={async () => {
                              try {
                                await revoke.mutateAsync(tk.id);
                                toast.success(t(($) => $.security.scim.revoked_toast));
                              } catch (error) {
                                toast.error(errorMessage(error, t(($) => $.security.scim.revoke_failed_toast)));
                              }
                            }}
                            disabled={revoke.isPending}
                          >
                            {t(($) => $.security.scim.revoke)}
                          </Button>
                        ) : null}
                      </TableCell>
                    ) : null}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </SettingsCard>

      <AlertDialog open={confirmCreate} onOpenChange={(open) => !open && setConfirmCreate(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.security.scim.rotate_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.security.scim.rotate_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.security.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void generate()} disabled={create.isPending}>
              {t(($) => $.security.scim.generate)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
