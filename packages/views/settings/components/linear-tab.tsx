"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, ExternalLink } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { issueStatusListOptions } from "@multica/core/issue-statuses/queries";
import {
  DEFAULT_LINEAR_STATUS_MAP,
  LINEAR_STATE_TYPES,
  linearInstallationOptions,
  useDisconnectLinear,
  useStartLinearOAuth,
  useUpdateLinearStatusMap,
} from "@multica/core/linear";
import { useT } from "../../i18n";

// LinearTab is the workspace settings panel for the Linear Bridge (K21).
// Three states, in the order a workspace meets them:
//   1. the deployment has no Linear app configured — nothing to connect to;
//   2. connected to nothing yet — pick the agent, then approve in Linear;
//   3. connected — the org, the agent, the status map and Disconnect, with a
//      banner on top when Linear stopped accepting the token.
// Reading is member-visible; connecting, disconnecting and editing the map are
// admin-only (backend-enforced; the UI hides the controls to match).
export function LinearTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: installation, isLoading, isError } = useQuery(linearInstallationOptions(wsId));
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { data: statuses = [] } = useQuery(issueStatusListOptions(wsId));

  const [agentId, setAgentId] = useState("");
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [draftMap, setDraftMap] = useState<Record<string, string> | null>(null);

  const startOAuth = useStartLinearOAuth(wsId);
  const disconnect = useDisconnectLinear(wsId);
  const saveStatusMap = useUpdateLinearStatusMap(wsId);

  const connected = installation?.connected === true;
  const configured = installation?.configured === true;
  const broken = installation?.status === "broken";

  // The server sends its own list of Linear state types so a newer backend can
  // add one without a client release; the constant is only the fallback.
  const stateTypes = useMemo(
    () => (installation?.linear_state_types?.length ? installation.linear_state_types : LINEAR_STATE_TYPES),
    [installation?.linear_state_types],
  );
  const effectiveMap = draftMap ?? installation?.status_map ?? DEFAULT_LINEAR_STATUS_MAP;
  const statusOptions = useMemo(
    () => statuses.filter((s) => !s.archived_at).map((s) => ({ key: s.key, name: s.name })),
    [statuses],
  );

  async function handleConnect() {
    if (!agentId) return;
    try {
      const url = await startOAuth.mutateAsync({ agentId, redirect: "/settings/integrations" });
      if (!url) {
        toast.error(t(($) => $.linear.toast_connect_failed));
        return;
      }
      // A full navigation, not a popup: Linear's approval screen refuses to be
      // framed, and the callback comes back as a top-level redirect.
      window.location.href = url;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.linear.toast_connect_failed));
    }
  }

  async function handleDisconnect() {
    try {
      // Await the server before closing: a destructive flow never removes the
      // connection from the UI on optimism.
      await disconnect.mutateAsync();
      toast.success(t(($) => $.linear.toast_disconnected));
      setDisconnectOpen(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.linear.toast_disconnect_failed));
    }
  }

  async function handleSaveStatusMap() {
    if (!draftMap) return;
    try {
      await saveStatusMap.mutateAsync(draftMap);
      setDraftMap(null);
      toast.success(t(($) => $.linear.toast_status_map_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.linear.toast_status_map_failed));
    }
  }

  if (isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.linear.loading)}</p>;
  }
  if (isError) {
    return (
      <Card>
        <CardContent className="py-6">
          <p className="text-body text-muted-foreground">{t(($) => $.linear.load_failed)}</p>
        </CardContent>
      </Card>
    );
  }

  if (!configured && !connected) {
    return (
      <Card>
        <CardContent className="space-y-2 py-6">
          <p className="text-body font-medium">{t(($) => $.linear.not_enabled_title)}</p>
          <p className="text-caption text-muted-foreground">{t(($) => $.linear.not_enabled_description)}</p>
        </CardContent>
      </Card>
    );
  }

  if (!connected) {
    return (
      <div className="space-y-4" data-testid="linear-connect">
        <p className="text-caption text-muted-foreground">{t(($) => $.linear.connect_hint)}</p>
        {canManage ? (
          <div className="flex flex-wrap items-end gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="linear-agent">{t(($) => $.linear.agent_label)}</Label>
              <Select
                items={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
                value={agentId}
                onValueChange={(next) => setAgentId(next ?? "")}
              >
                <SelectTrigger id="linear-agent" className="w-64">
                  <SelectValue placeholder={t(($) => $.linear.agent_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {agents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <Button onClick={handleConnect} disabled={!agentId || startOAuth.isPending}>
              <ExternalLink className="mr-2 size-4" />
              {startOAuth.isPending ? t(($) => $.linear.connecting) : t(($) => $.linear.connect)}
            </Button>
          </div>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.linear.admin_only)}</p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6" data-testid="linear-connected">
      {broken && (
        <Card className="border-destructive/40">
          <CardContent className="flex items-start gap-3 py-4">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
            <div className="space-y-1">
              <p className="text-body font-medium">{t(($) => $.linear.broken_title)}</p>
              <p className="text-caption text-muted-foreground">
                {installation?.last_error || t(($) => $.linear.broken_description)}
              </p>
              {canManage && (
                <Button variant="outline" size="sm" onClick={handleConnect} disabled={!agentId && !installation?.agent_id}>
                  {t(($) => $.linear.reconnect)}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      <dl className="grid gap-3 sm:grid-cols-2">
        <div>
          <dt className="text-caption text-muted-foreground">{t(($) => $.linear.org_label)}</dt>
          <dd className="text-body font-medium break-words">
            {installation?.linear_org_name || installation?.linear_org_id}
          </dd>
        </div>
        <div>
          <dt className="text-caption text-muted-foreground">{t(($) => $.linear.agent_label)}</dt>
          <dd className="text-body font-medium break-words">{installation?.agent_name || installation?.agent_id}</dd>
        </div>
      </dl>

      <div className="space-y-3">
        <div>
          <p className="text-body font-medium">{t(($) => $.linear.status_map_title)}</p>
          <p className="text-caption text-muted-foreground">{t(($) => $.linear.status_map_description)}</p>
        </div>
        <div className="space-y-2">
          {stateTypes.map((stateType) => (
            <div key={stateType} className="flex flex-wrap items-center gap-3">
              <Label className="w-32 shrink-0 text-caption" htmlFor={`linear-state-${stateType}`}>
                {stateType}
              </Label>
              <Select
                items={statusOptions.map((status) => ({ value: status.key, label: status.name }))}
                value={effectiveMap[stateType] ?? ""}
                disabled={!canManage}
                onValueChange={(next) => next && setDraftMap({ ...effectiveMap, [stateType]: next })}
              >
                <SelectTrigger id={`linear-state-${stateType}`} className="w-56">
                  <SelectValue placeholder={t(($) => $.linear.status_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {statusOptions.map((status) => (
                    <SelectItem key={status.key} value={status.key}>
                      {status.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ))}
        </div>
        {canManage && draftMap && (
          <div className="flex gap-2">
            <Button size="sm" onClick={handleSaveStatusMap} disabled={saveStatusMap.isPending}>
              {saveStatusMap.isPending ? t(($) => $.linear.saving) : t(($) => $.linear.save_status_map)}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDraftMap(null)}>
              {t(($) => $.linear.cancel)}
            </Button>
          </div>
        )}
      </div>

      {canManage && (
        <div>
          <Button variant="outline" size="sm" onClick={() => setDisconnectOpen(true)}>
            {t(($) => $.linear.disconnect)}
          </Button>
        </div>
      )}

      <AlertDialog open={disconnectOpen} onOpenChange={setDisconnectOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.linear.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.linear.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.linear.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnect.isPending}>
              {disconnect.isPending ? t(($) => $.linear.disconnecting) : t(($) => $.linear.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
