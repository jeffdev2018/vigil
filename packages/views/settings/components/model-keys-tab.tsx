"use client";

import { useMemo, useState, type FormEvent } from "react";
import { KeyRound, Loader2, RotateCw, Trash2, TriangleAlert } from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  modelKeysOptions,
  usageForKey,
  useCreateModelKey,
  useRetireModelKey,
  useRotateModelKey,
  type ModelKey,
  type ModelKeyScope,
  type ModelKeyVendor,
} from "@multica/core/model-keys";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * BYOK model keys (K48).
 *
 * A key declared here is the vendor API key the runs of this workspace (or of
 * one project) spend, instead of the CLI's own credentials, and every run's
 * cost is attributed to it.
 *
 * The stored value is WRITE-ONLY: the API only ever returns a hint such as
 * `sk-***1a2b`, so there is nothing to prefill and changing a key means
 * supplying a new value ("rotate"). The old row is kept as retired so its
 * cost history survives. The server also retires a key on its own when a run
 * fails on the vendor's authentication or quota — those rows are surfaced in
 * a banner so a manager notices the failover.
 */
const TICKS_PER_USD = 10_000_000_000;
const dollars = (ticks: number) =>
  new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 2,
  }).format(ticks / TICKS_PER_USD);

const AUTO_RETIRE_PREFIX = "agent_error.";

/** The server answers 409 with this code when an active key already exists for the vendor and scope. */
function isActiveConflict(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const { status, body } = error as { status?: unknown; body?: unknown };
  const code =
    typeof body === "object" && body !== null ? (body as { code?: unknown }).code : undefined;
  return code === "model_key_active_conflict" || status === 409;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function ModelKeysTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage =
    currentMember.role === "owner" || currentMember.role === "admin";

  const keysQuery = useQuery(modelKeysOptions(wsId));
  const projectsQuery = useQuery(projectListOptions(wsId));
  const rotateKey = useRotateModelKey(wsId);
  const retireKey = useRetireModelKey(wsId);

  const keys = keysQuery.data?.keys ?? [];
  const usage = keysQuery.data?.usage ?? [];
  const vendors = keysQuery.data?.vendors ?? [];
  const configured = keysQuery.data?.configured === true;
  const projects = projectsQuery.data ?? [];

  const vendorLabels = useMemo(
    () => new Map((keysQuery.data?.vendors ?? []).map((vendor) => [vendor.id, vendor.label || vendor.id])),
    [keysQuery.data?.vendors],
  );
  const projectNames = useMemo(
    () => new Map((projectsQuery.data ?? []).map((project) => [project.id, project.title])),
    [projectsQuery.data],
  );
  const vendorLabel = (id: string) => vendorLabels.get(id) ?? id;
  const failedOver = keys.filter((key) =>
    key.deactivated_reason.startsWith(AUTO_RETIRE_PREFIX),
  );

  const [rotating, setRotating] = useState<ModelKey | null>(null);
  const [retiring, setRetiring] = useState<ModelKey | null>(null);

  const handleRetire = async () => {
    if (!retiring) return;
    try {
      await retireKey.mutateAsync(retiring.id);
      toast.success(t(($) => $.model_keys.retired_toast));
      setRetiring(null);
    } catch (error) {
      toast.error(errorMessage(error, t(($) => $.model_keys.retire_failed_toast)));
    }
  };

  return (
    <SettingsTab
      title={t(($) => $.model_keys.title)}
      description={t(($) => $.model_keys.description)}
    >
      {failedOver.length > 0 ? (
        <FailoverBanner keys={failedOver} vendorLabel={vendorLabel} />
      ) : null}

      {keysQuery.data && !configured ? (
        <Alert>
          <KeyRound />
          <AlertTitle>{t(($) => $.model_keys.not_configured_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.model_keys.not_configured_description)}
          </AlertDescription>
        </Alert>
      ) : null}

      <SettingsSection
        title={t(($) => $.model_keys.keys_title)}
        description={t(($) => $.model_keys.write_only_note)}
      >
        <SettingsCard>
          {keysQuery.isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
            </div>
          ) : keys.length === 0 ? (
            <div className="px-4 py-8 text-center">
              <KeyRound className="mx-auto h-5 w-5 text-muted-foreground" />
              <p className="mt-3 text-body font-medium">
                {t(($) => $.model_keys.empty_title)}
              </p>
              <p className="mx-auto mt-1 max-w-md text-caption leading-5 text-muted-foreground">
                {t(($) => $.model_keys.empty_description)}
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t(($) => $.model_keys.columns.scope)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.vendor)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.label)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.key)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.status)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.priority)}</TableHead>
                    <TableHead>{t(($) => $.model_keys.columns.usage)}</TableHead>
                    {canManage ? <TableHead /> : null}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {keys.map((key) => (
                    <ModelKeyRow
                      key={key.id}
                      modelKey={key}
                      vendorLabel={vendorLabel(key.provider)}
                      scopeLabel={
                        key.scope === "project"
                          ? projectNames.get(key.scope_id ?? "") ??
                            `${t(($) => $.model_keys.scope_project)} ${(key.scope_id ?? "").slice(0, 8)}`
                          : t(($) => $.model_keys.scope_workspace)
                      }
                      usage={usageForKey(usage, key.id)}
                      canManage={canManage}
                      onRotate={() => setRotating(key)}
                      onRetire={() => setRetiring(key)}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </SettingsCard>
        {!canManage && !currentMember.isLoading ? (
          <p className="px-0.5 text-caption text-muted-foreground">
            {t(($) => $.model_keys.admin_only_note)}
          </p>
        ) : null}
      </SettingsSection>

      {canManage && configured ? (
        <AddKeyForm wsId={wsId} vendors={vendors} projects={projects} />
      ) : null}

      <RotateDialog
        key={rotating?.id ?? "closed"}
        modelKey={rotating}
        vendorLabel={rotating ? vendorLabel(rotating.provider) : ""}
        pending={rotateKey.isPending}
        onClose={() => setRotating(null)}
        onSubmit={async (key, label) => {
          if (!rotating) return;
          try {
            await rotateKey.mutateAsync({ keyId: rotating.id, key, label });
            toast.success(t(($) => $.model_keys.rotated_toast));
            setRotating(null);
          } catch (error) {
            toast.error(errorMessage(error, t(($) => $.model_keys.rotate_failed_toast)));
          }
        }}
      />

      <AlertDialog
        open={retiring !== null}
        onOpenChange={(open) => {
          if (!open) setRetiring(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.model_keys.retire_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.model_keys.retire_description, {
                vendor: retiring ? vendorLabel(retiring.provider) : "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={retireKey.isPending}>
              {t(($) => $.model_keys.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void handleRetire();
              }}
              disabled={retireKey.isPending}
            >
              {retireKey.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t(($) => $.model_keys.retire_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

function FailoverBanner({
  keys,
  vendorLabel,
}: {
  keys: ModelKey[];
  vendorLabel: (id: string) => string;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  return (
    <Alert variant="destructive">
      <TriangleAlert />
      <AlertTitle>{t(($) => $.model_keys.failover_title)}</AlertTitle>
      <AlertDescription>
        <p>{t(($) => $.model_keys.failover_description)}</p>
        <ul className="list-disc pl-4">
          {keys.map((key) => {
            const params = {
              vendor: vendorLabel(key.provider),
              key: key.label || key.key_hint,
              when: key.deactivated_at ? timeAgo(key.deactivated_at) : "",
            };
            return (
              <li key={key.id}>
                {key.deactivated_reason === "agent_error.provider_quota_limit"
                  ? t(($) => $.model_keys.failover_quota, params)
                  : t(($) => $.model_keys.failover_auth, params)}
              </li>
            );
          })}
        </ul>
      </AlertDescription>
    </Alert>
  );
}

function ModelKeyRow({
  modelKey,
  vendorLabel,
  scopeLabel,
  usage,
  canManage,
  onRotate,
  onRetire,
}: {
  modelKey: ModelKey;
  vendorLabel: string;
  scopeLabel: string;
  usage: { tasks: number; tokens: number; costUsdTicks: number };
  canManage: boolean;
  onRotate: () => void;
  onRetire: () => void;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const compact = new Intl.NumberFormat(undefined, { notation: "compact" });
  return (
    <TableRow>
      <TableCell className="max-w-40 truncate" title={scopeLabel}>{scopeLabel}</TableCell>
      <TableCell>{vendorLabel}</TableCell>
      <TableCell className="max-w-40 truncate" title={modelKey.label}>
        {modelKey.label || "—"}
      </TableCell>
      <TableCell className="font-mono text-caption">{modelKey.key_hint}</TableCell>
      <TableCell>
        {modelKey.active === true ? (
          <Badge>{t(($) => $.model_keys.status_active)}</Badge>
        ) : (
          <Badge
            variant="secondary"
            title={modelKey.deactivated_at ? timeAgo(modelKey.deactivated_at) : undefined}
          >
            {t(($) => $.model_keys.status_retired)}
            {modelKey.deactivated_reason
              ? ` · ${reasonLabel(modelKey.deactivated_reason, t)}`
              : null}
          </Badge>
        )}
      </TableCell>
      <TableCell>{modelKey.priority}</TableCell>
      <TableCell className="whitespace-nowrap text-caption text-muted-foreground">
        {t(($) => $.model_keys.usage, {
          tasks: usage.tasks,
          tokens: compact.format(usage.tokens),
          cost: dollars(usage.costUsdTicks),
        })}
      </TableCell>
      {canManage ? (
        <TableCell>
          {modelKey.active === true ? (
            <div className="flex justify-end gap-1">
              <Button
                variant="ghost"
                size="icon"
                onClick={onRotate}
                aria-label={t(($) => $.model_keys.rotate)}
              >
                <RotateCw className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={onRetire}
                aria-label={t(($) => $.model_keys.retire)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ) : null}
        </TableCell>
      ) : null}
    </TableRow>
  );
}

/**
 * `deactivated_reason` is a server-driven string; a value from a newer
 * backend renders as itself instead of disappearing.
 */
function reasonLabel(
  reason: string,
  t: ReturnType<typeof useT<"settings">>["t"],
): string {
  switch (reason) {
    case "rotated":
      return t(($) => $.model_keys.reason_rotated);
    case "agent_error.provider_auth_or_access":
      return t(($) => $.model_keys.reason_auth);
    case "agent_error.provider_quota_limit":
      return t(($) => $.model_keys.reason_quota);
    default:
      return reason.startsWith("retired by ")
        ? t(($) => $.model_keys.reason_retired)
        : reason;
  }
}

function AddKeyForm({
  wsId,
  vendors,
  projects,
}: {
  wsId: string;
  vendors: ModelKeyVendor[];
  projects: Array<{ id: string; title: string }>;
}) {
  const { t } = useT("settings");
  const createKey = useCreateModelKey(wsId);
  const [scope, setScope] = useState<ModelKeyScope>("workspace");
  const [projectId, setProjectId] = useState("");
  const [provider, setProvider] = useState(vendors[0]?.id ?? "");
  const [label, setLabel] = useState("");
  const [key, setKey] = useState("");
  const [priority, setPriority] = useState("0");
  const [error, setError] = useState<"conflict" | string | null>(null);

  const scopeItems = (["workspace", "project"] as const).map((value) => ({
    value,
    label:
      value === "workspace"
        ? t(($) => $.model_keys.scope_workspace)
        : t(($) => $.model_keys.scope_project),
  }));
  const vendorItems = vendors.map((vendor) => ({ value: vendor.id, label: vendor.label || vendor.id }));
  const projectItems = projects.map((project) => ({ value: project.id, label: project.title }));

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const trimmedKey = key.trim();
    if (!trimmedKey || !provider || (scope === "project" && !projectId)) return;
    const parsedPriority = Number(priority);
    setError(null);
    try {
      await createKey.mutateAsync({
        scope,
        ...(scope === "project" ? { scope_id: projectId } : {}),
        provider,
        label: label.trim() || undefined,
        key: trimmedKey,
        priority: Number.isFinite(parsedPriority) ? Math.trunc(parsedPriority) : undefined,
      });
      toast.success(t(($) => $.model_keys.added_toast));
      setKey("");
      setLabel("");
    } catch (caught) {
      if (isActiveConflict(caught)) {
        setError("conflict");
      } else {
        const message = errorMessage(caught, t(($) => $.model_keys.add_failed_toast));
        setError(message);
        toast.error(message);
      }
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.model_keys.add_title)}
      description={t(($) => $.model_keys.add_description)}
    >
      <SettingsCard>
        <form onSubmit={submit} className="grid gap-4 p-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-scope">{t(($) => $.model_keys.field_scope)}</Label>
            <Select
              items={scopeItems}
              value={scope}
              onValueChange={(value) => {
                if (value) setScope(value);
                setProjectId("");
              }}
            >
              <SelectTrigger id="model-key-scope" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {scopeItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {scope === "project" ? (
            <div className="grid gap-1.5">
              <Label htmlFor="model-key-project">{t(($) => $.model_keys.field_project)}</Label>
              <Select
                items={projectItems}
                value={projectId}
                onValueChange={(value) => setProjectId(value ?? "")}
              >
                <SelectTrigger id="model-key-project" className="w-full">
                  <SelectValue placeholder={t(($) => $.model_keys.field_project_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {projectItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-vendor">{t(($) => $.model_keys.field_vendor)}</Label>
            <Select
              items={vendorItems}
              value={provider}
              onValueChange={(value) => setProvider(value ?? "")}
            >
              <SelectTrigger id="model-key-vendor" className="w-full">
                <SelectValue placeholder={t(($) => $.model_keys.field_vendor_placeholder)} />
              </SelectTrigger>
              <SelectContent>
                {vendorItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-label">{t(($) => $.model_keys.field_label)}</Label>
            <Input
              id="model-key-label"
              value={label}
              onChange={(event) => setLabel(event.target.value)}
              placeholder={t(($) => $.model_keys.field_label_placeholder)}
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-value">{t(($) => $.model_keys.field_key)}</Label>
            <Input
              id="model-key-value"
              type="password"
              autoComplete="off"
              value={key}
              onChange={(event) => setKey(event.target.value)}
              placeholder={t(($) => $.model_keys.field_key_placeholder)}
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-priority">{t(($) => $.model_keys.field_priority)}</Label>
            <Input
              id="model-key-priority"
              type="number"
              step="1"
              value={priority}
              onChange={(event) => setPriority(event.target.value)}
            />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.model_keys.field_priority_hint)}
            </p>
          </div>
          <div className="flex items-end justify-between gap-4 sm:col-span-2">
            {error === "conflict" ? (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.model_keys.conflict)}{" "}
                {t(($) => $.model_keys.conflict_hint)}
              </p>
            ) : error ? (
              <p role="alert" className="text-caption text-destructive">{error}</p>
            ) : (
              <span />
            )}
            <Button type="submit" disabled={createKey.isPending || !key.trim() || !provider}>
              {createKey.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t(($) => $.model_keys.submit)}
            </Button>
          </div>
        </form>
      </SettingsCard>
    </SettingsSection>
  );
}

function RotateDialog({
  modelKey,
  vendorLabel,
  pending,
  onClose,
  onSubmit,
}: {
  modelKey: ModelKey | null;
  vendorLabel: string;
  pending: boolean;
  onClose: () => void;
  onSubmit: (key: string, label?: string) => Promise<void>;
}) {
  const { t } = useT("settings");
  const [key, setKey] = useState("");
  const [label, setLabel] = useState(modelKey?.label ?? "");
  return (
    <Dialog open={modelKey !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.model_keys.rotate_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.model_keys.rotate_description, {
              vendor: vendorLabel,
              hint: modelKey?.key_hint ?? "",
            })}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (!key.trim()) return;
            void onSubmit(key.trim(), label.trim() || undefined);
          }}
          className="grid gap-4"
        >
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-rotate-value">{t(($) => $.model_keys.field_new_key)}</Label>
            <Input
              id="model-key-rotate-value"
              type="password"
              autoComplete="off"
              value={key}
              onChange={(event) => setKey(event.target.value)}
              required
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="model-key-rotate-label">{t(($) => $.model_keys.field_label)}</Label>
            <Input
              id="model-key-rotate-label"
              value={label}
              onChange={(event) => setLabel(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
              {t(($) => $.model_keys.cancel)}
            </Button>
            <Button type="submit" disabled={pending || !key.trim()}>
              {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t(($) => $.model_keys.rotate_confirm)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
