"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Copy, RefreshCw } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  DEFAULT_TWENTY_EVENTS,
  invalidTwentyEvents,
  normalizeTwentyEvents,
  twentyMembersOptions,
  twentyStatusOptions,
  useCheckTwenty,
  useConnectTwenty,
  useDisconnectTwenty,
  useUpdateTwentySettings,
  type TwentyConnection,
} from "@multica/core/twenty";
import { useT } from "../../i18n";

// TwentyTab is the workspace settings panel for the Twenty CRM connection
// (OS plan, chantier 2). Three states, in the order a workspace meets them:
//   1. the deployment has no MULTICA_TWENTY_SECRET_KEY — nothing to connect;
//   2. not connected — instance URL, API key, events, agent exposure;
//   3. connected — the instance, status, events, exposure, MCP endpoint,
//      inbound path, the member pairing, Check and Disconnect, with a banner
//      when Twenty refused the key. The inbound token is shown once, right
//      after connecting, because the server never returns it again.
// Reading is member-visible; everything else is owner/admin (backend-enforced;
// the UI hides the controls to match).
export function TwentyTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";

  const { data: status, isLoading, isError } = useQuery(twentyStatusOptions(wsId));
  const connected = status?.connected === true && status.connection != null;
  const { data: members = [] } = useQuery({ ...twentyMembersOptions(wsId), enabled: !!wsId && connected });

  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [eventsText, setEventsText] = useState("");
  const [expose, setExpose] = useState(true);
  const [mintedToken, setMintedToken] = useState<TwentyConnection | null>(null);
  const [draftEvents, setDraftEvents] = useState<string | null>(null);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  const connect = useConnectTwenty(wsId);
  const update = useUpdateTwentySettings(wsId);
  const check = useCheckTwenty(wsId);
  const disconnect = useDisconnectTwenty(wsId);

  const defaultEvents = status?.default_events?.length ? status.default_events : DEFAULT_TWENTY_EVENTS;
  const connection = status?.connection ?? null;

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t(($) => $.twenty.copied));
    } catch {
      toast.error(t(($) => $.twenty.copy_failed));
    }
  }

  async function handleConnect() {
    const events = normalizeTwentyEvents(eventsText || defaultEvents.join(", "));
    const bad = invalidTwentyEvents(events);
    if (bad.length > 0) {
      toast.error(t(($) => $.twenty.events_invalid, { events: bad.join(", ") }));
      return;
    }
    try {
      const conn = await connect.mutateAsync({ base_url: baseUrl.trim(), api_key: apiKey.trim(), events, expose_to_agents: expose });
      setApiKey("");
      setMintedToken(conn);
      toast.success(t(($) => $.twenty.toast_connected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.twenty.toast_connect_failed));
    }
  }

  async function handleSaveEvents() {
    if (draftEvents === null || !connection) return;
    const events = normalizeTwentyEvents(draftEvents);
    const bad = invalidTwentyEvents(events);
    if (bad.length > 0) {
      toast.error(t(($) => $.twenty.events_invalid, { events: bad.join(", ") }));
      return;
    }
    try {
      await update.mutateAsync({ events, expose_to_agents: connection.expose_to_agents === true });
      setDraftEvents(null);
      toast.success(t(($) => $.twenty.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.twenty.toast_save_failed));
    }
  }

  async function handleToggleExpose(next: boolean) {
    if (!connection) return;
    try {
      await update.mutateAsync({ events: connection.events ?? [], expose_to_agents: next });
      toast.success(t(($) => $.twenty.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.twenty.toast_save_failed));
    }
  }

  async function handleCheck() {
    try {
      const conn = await check.mutateAsync();
      if (conn.status === "connected") toast.success(t(($) => $.twenty.toast_check_ok));
      else toast.error(conn.last_error || t(($) => $.twenty.toast_check_failed));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.twenty.toast_check_failed));
    }
  }

  async function handleDisconnect() {
    try {
      // Await the server before closing: a destructive flow never removes the
      // connection from the UI on optimism.
      await disconnect.mutateAsync();
      setMintedToken(null);
      setDisconnectOpen(false);
      toast.success(t(($) => $.twenty.toast_disconnected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.twenty.toast_disconnect_failed));
    }
  }

  if (isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.twenty.loading)}</p>;
  }
  if (isError) {
    return (
      <Card>
        <CardContent className="py-6">
          <p className="text-body text-muted-foreground">{t(($) => $.twenty.load_failed)}</p>
        </CardContent>
      </Card>
    );
  }

  if (status?.available !== true && !connected) {
    return (
      <Card>
        <CardContent className="space-y-2 py-6">
          <p className="text-body font-medium">{t(($) => $.twenty.not_enabled_title)}</p>
          <p className="text-caption text-muted-foreground">{t(($) => $.twenty.not_enabled_description)}</p>
        </CardContent>
      </Card>
    );
  }

  if (!connected) {
    return (
      <div className="space-y-4" data-testid="twenty-connect">
        {canManage ? (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="twenty-url">{t(($) => $.twenty.base_url_label)}</Label>
                <Input id="twenty-url" placeholder="https://crm.example.com" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="twenty-key">{t(($) => $.twenty.api_key_label)}</Label>
                <Input id="twenty-key" type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
                <p className="text-caption text-muted-foreground">{t(($) => $.twenty.api_key_hint)}</p>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="twenty-events">{t(($) => $.twenty.events_label)}</Label>
              <Textarea id="twenty-events" rows={2} placeholder={defaultEvents.join(", ")} value={eventsText} onChange={(e) => setEventsText(e.target.value)} />
              <p className="text-caption text-muted-foreground">{t(($) => $.twenty.events_hint)}</p>
            </div>
            <div className="flex items-center gap-3">
              <Switch id="twenty-expose" checked={expose} onCheckedChange={setExpose} />
              <Label htmlFor="twenty-expose">{t(($) => $.twenty.expose_label)}</Label>
            </div>
            <Button onClick={handleConnect} disabled={!baseUrl.trim() || !apiKey.trim() || connect.isPending}>
              {connect.isPending ? t(($) => $.twenty.connecting) : t(($) => $.twenty.connect)}
            </Button>
          </div>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.twenty.admin_only)}</p>
        )}
      </div>
    );
  }

  const broken = connection?.status === "error";
  const events = connection?.events ?? [];
  const linked = members.filter((m) => m.linked === true);
  const unlinked = members.filter((m) => m.linked !== true && m.twenty_only !== true);
  const twentyOnly = members.filter((m) => m.twenty_only === true);

  return (
    <div className="space-y-6" data-testid="twenty-connected">
      {mintedToken?.inbound_token ? (
        <Card className="border-primary/40">
          <CardContent className="space-y-2 py-4">
            <p className="text-body font-medium">{t(($) => $.twenty.token_once_title)}</p>
            <p className="text-caption text-muted-foreground">
              {mintedToken.webhook_registered === true ? t(($) => $.twenty.token_once_registered) : t(($) => $.twenty.token_once_manual)}
            </p>
            <CopyField label={t(($) => $.twenty.inbound_path_label)} value={mintedToken.inbound_path} onCopy={copy} copyLabel={t(($) => $.twenty.copy)} />
            <Button variant="ghost" size="sm" onClick={() => setMintedToken(null)}>
              {t(($) => $.twenty.token_once_dismiss)}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {broken ? (
        <Card className="border-destructive/40">
          <CardContent className="flex items-start gap-3 py-4">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
            <div className="space-y-1">
              <p className="text-body font-medium">{t(($) => $.twenty.broken_title)}</p>
              <p className="text-caption text-muted-foreground">{connection?.last_error || t(($) => $.twenty.broken_description)}</p>
            </div>
          </CardContent>
        </Card>
      ) : connection?.last_error ? (
        <p className="text-caption text-muted-foreground">{connection.last_error}</p>
      ) : null}

      <dl className="grid gap-3 sm:grid-cols-2">
        <div>
          <dt className="text-caption text-muted-foreground">{t(($) => $.twenty.instance_label)}</dt>
          <dd className="text-body font-medium break-all">
            {connection?.twenty_workspace_name ? `${connection.twenty_workspace_name} · ` : ""}
            {connection?.base_url}
          </dd>
        </div>
        <div>
          <dt className="text-caption text-muted-foreground">{t(($) => $.twenty.status_label)}</dt>
          <dd className="flex items-center gap-2">
            <Badge variant={broken ? "destructive" : "secondary"}>{broken ? t(($) => $.twenty.status_error) : t(($) => $.twenty.status_connected)}</Badge>
            <Badge variant="outline">{connection?.webhook_registered === true ? t(($) => $.twenty.webhook_registered) : t(($) => $.twenty.webhook_manual)}</Badge>
          </dd>
        </div>
      </dl>

      <div className="space-y-3">
        <div className="flex items-center justify-between gap-4">
          <div>
            <Label htmlFor="twenty-expose-connected" className="text-body font-medium">{t(($) => $.twenty.expose_label)}</Label>
            <p className="text-caption text-muted-foreground">{t(($) => $.twenty.expose_hint)}</p>
          </div>
          <Switch id="twenty-expose-connected" checked={connection?.expose_to_agents === true} disabled={!canManage || update.isPending} onCheckedChange={handleToggleExpose} />
        </div>
        <CopyField label={t(($) => $.twenty.mcp_url_label)} value={connection?.mcp_url ?? ""} onCopy={copy} copyLabel={t(($) => $.twenty.copy)} />
        {connection?.inbound_path ? (
          <CopyField label={t(($) => $.twenty.inbound_path_label)} value={connection.inbound_path} onCopy={copy} copyLabel={t(($) => $.twenty.copy)} />
        ) : null}
      </div>

      <div className="space-y-2">
        <Label htmlFor="twenty-events-connected" className="text-body font-medium">{t(($) => $.twenty.events_label)}</Label>
        <p className="text-caption text-muted-foreground">{t(($) => $.twenty.events_hint)}</p>
        <Textarea id="twenty-events-connected" rows={2} disabled={!canManage} value={draftEvents ?? events.join(", ")} onChange={(e) => setDraftEvents(e.target.value)} />
        {canManage && draftEvents !== null ? (
          <div className="flex gap-2">
            <Button size="sm" onClick={handleSaveEvents} disabled={update.isPending}>
              {update.isPending ? t(($) => $.twenty.saving) : t(($) => $.twenty.save_events)}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDraftEvents(null)}>
              {t(($) => $.twenty.cancel)}
            </Button>
          </div>
        ) : null}
      </div>

      <div className="space-y-2" data-testid="twenty-members">
        <p className="text-body font-medium">{t(($) => $.twenty.members_title)}</p>
        <p className="text-caption text-muted-foreground">{t(($) => $.twenty.members_hint)}</p>
        <ul className="divide-y rounded-md border">
          {members.length === 0 ? (
            <li className="px-3 py-2 text-caption text-muted-foreground">{t(($) => $.twenty.members_empty)}</li>
          ) : null}
          {[...linked, ...unlinked, ...twentyOnly].map((m) => (
            <li key={`${m.user_id}|${m.twenty_id}|${m.email}`} className="flex items-center justify-between gap-3 px-3 py-2">
              <div className="min-w-0">
                <p className="truncate text-body">{m.name || m.twenty_name || m.email}</p>
                <p className="truncate text-caption text-muted-foreground">{m.email}</p>
              </div>
              <Badge variant={m.linked === true ? "secondary" : "outline"}>
                {m.linked === true ? t(($) => $.twenty.member_linked) : m.twenty_only === true ? t(($) => $.twenty.member_twenty_only) : t(($) => $.twenty.member_unlinked)}
              </Badge>
            </li>
          ))}
        </ul>
      </div>

      {canManage ? (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={handleCheck} disabled={check.isPending}>
            <RefreshCw className="mr-2 size-4" aria-hidden="true" />
            {check.isPending ? t(($) => $.twenty.checking) : t(($) => $.twenty.check)}
          </Button>
          <Button variant="destructive" size="sm" onClick={() => setDisconnectOpen(true)}>
            {t(($) => $.twenty.disconnect)}
          </Button>
        </div>
      ) : null}

      <AlertDialog open={disconnectOpen} onOpenChange={setDisconnectOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.twenty.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.twenty.disconnect_confirm_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.twenty.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnect.isPending}>
              {disconnect.isPending ? t(($) => $.twenty.disconnecting) : t(($) => $.twenty.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CopyField({ label, value, onCopy, copyLabel }: { label: string; value: string; onCopy: (value: string) => void; copyLabel: string }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-caption">{label}</Label>
      <div className="flex items-center gap-2">
        <Input readOnly value={value} className="min-w-0 font-mono text-caption" />
        <Button variant="outline" size="sm" className="shrink-0" onClick={() => onCopy(value)} title={copyLabel} aria-label={copyLabel}>
          <Copy className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}
