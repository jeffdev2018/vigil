"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { HeartPulse, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import {
  codeHealthScansOptions,
  codeHealthSettingsOptions,
  openedFindings,
  scanTone,
  useSaveCodeHealthSettings,
  useTriggerCodeHealthScan,
  type CodeHealthScan,
  type CodeHealthSettingsInput,
} from "@multica/core/code-health";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * Code health autopilot (K22).
 *
 * Maintenance work is the work nobody schedules. On a cron, the maintenance
 * agent reads the repository read-only and reports what it found; the server
 * opens one issue per finding it trusts, assigned to that same agent, under
 * whatever budget policy covers it. Off by default, admin-only.
 *
 * The history is the honest part of this screen: every reported finding is
 * listed with what happened to it, including the ones that were dropped for
 * being unsure, already open, or over the per-scan cap.
 */

const TONE_CLASS = {
  success: "text-success",
  warning: "text-warning",
  destructive: "text-destructive",
  muted: "text-muted-foreground",
} as const;

const SCAN_STATUSES = ["running", "completed", "failed", "empty"] as const;
const SKIP_REASONS = ["low_confidence", "duplicate", "cap", "budget", "error"] as const;

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function CodeHealthTab() {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const wsId = useCurrentWorkspace()?.id ?? "";

  const settingsQuery = useQuery(codeHealthSettingsOptions(wsId));
  const scansQuery = useQuery(codeHealthScansOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const projectsQuery = useQuery(projectListOptions(wsId));
  const saveSettings = useSaveCodeHealthSettings(wsId);
  const triggerScan = useTriggerCodeHealthScan(wsId);

  const settings = settingsQuery.data;
  const scans = scansQuery.data ?? [];
  const agents = agentsQuery.data ?? [];
  const projects = projectsQuery.data ?? [];

  const [form, setForm] = useState<CodeHealthSettingsInput | null>(null);
  // The form mirrors the server until the admin edits it; a refetch that lands
  // mid-edit must not overwrite what they typed, hence the null guard.
  useEffect(() => {
    if (!settings || form) return;
    setForm({
      enabled: settings.enabled,
      cron: settings.cron,
      timezone: settings.timezone,
      agent_id: settings.agent_id,
      project_id: settings.project_id,
      max_issues_per_scan: settings.max_issues_per_scan,
      min_confidence: settings.min_confidence,
    });
  }, [settings, form]);

  const patch = (next: Partial<CodeHealthSettingsInput>) =>
    setForm((current) => (current ? { ...current, ...next } : current));

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!form) return;
    saveSettings.mutate(form, {
      onSuccess: () => toast.success(t(($) => $.code_health.saved_toast)),
      onError: (error) =>
        toast.error(errorMessage(error, t(($) => $.code_health.save_failed))),
    });
  };

  const handleScanNow = () => {
    triggerScan.mutate(undefined, {
      onSuccess: () => toast.success(t(($) => $.code_health.scan_started_toast)),
      onError: (error) =>
        toast.error(errorMessage(error, t(($) => $.code_health.scan_failed))),
    });
  };

  const configured = !!form?.agent_id;
  const scanning = scans.some((scan) => scan.status === "running");

  return (
    <SettingsTab
      title={t(($) => $.code_health.title)}
      description={t(($) => $.code_health.description)}
    >
      {/* Onboarding: what this does and what it costs, before any switch. */}
      {!configured ? (
        <SettingsCard>
          <div className="flex gap-3 px-4 py-4">
            <HeartPulse className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="space-y-2">
              <p className="text-body font-medium">
                {t(($) => $.code_health.onboarding_title)}
              </p>
              <p className="text-caption leading-5 text-muted-foreground">
                {t(($) => $.code_health.onboarding_body)}
              </p>
            </div>
          </div>
        </SettingsCard>
      ) : null}

      <SettingsSection
        title={t(($) => $.code_health.config_title)}
        description={t(($) => $.code_health.config_description)}
        action={
          <Button
            size="sm"
            variant="outline"
            onClick={handleScanNow}
            disabled={!configured || scanning || triggerScan.isPending}
            data-testid="code-health-scan-now"
          >
            {triggerScan.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : null}
            {t(($) => $.code_health.scan_now)}
          </Button>
        }
      >
        <SettingsCard>
          {settingsQuery.isPending || !form ? (
            <p className="px-4 py-8 text-center text-caption text-muted-foreground">
              {t(($) => $.code_health.loading)}
            </p>
          ) : (
            <form className="space-y-4 px-4 py-4" onSubmit={handleSubmit}>
              <label className="flex items-center justify-between gap-4">
                <span className="min-w-0">
                  <span className="block text-body">
                    {t(($) => $.code_health.enabled_label)}
                  </span>
                  <span className="block text-caption text-muted-foreground">
                    {t(($) => $.code_health.enabled_hint)}
                  </span>
                </span>
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(checked: boolean) => patch({ enabled: checked })}
                  data-testid="code-health-enabled"
                />
              </label>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label>{t(($) => $.code_health.agent_label)}</Label>
                  <Select
                    items={[
                      { value: "", label: t(($) => $.code_health.pick_agent) },
                      ...agents.map((agent) => ({ value: agent.id, label: agent.name })),
                    ]}
                    value={form.agent_id}
                    onValueChange={(value) => patch({ agent_id: value ?? "" })}
                  >
                    <SelectTrigger aria-label={t(($) => $.code_health.agent_label)} size="sm" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">{t(($) => $.code_health.pick_agent)}</SelectItem>
                      {agents.map((agent) => (
                        <SelectItem key={agent.id} value={agent.id}>
                          {agent.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-1.5">
                  <Label>{t(($) => $.code_health.project_label)}</Label>
                  <Select
                    items={[
                      { value: "", label: t(($) => $.code_health.whole_workspace) },
                      ...projects.map((project) => ({ value: project.id, label: project.title })),
                    ]}
                    value={form.project_id}
                    onValueChange={(value) => patch({ project_id: value ?? "" })}
                  >
                    <SelectTrigger aria-label={t(($) => $.code_health.project_label)} size="sm" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">
                        {t(($) => $.code_health.whole_workspace)}
                      </SelectItem>
                      {projects.map((project) => (
                        <SelectItem key={project.id} value={project.id}>
                          {project.title}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="code-health-cron">
                    {t(($) => $.code_health.cron_label)}
                  </Label>
                  <Input
                    id="code-health-cron"
                    value={form.cron}
                    onChange={(event) => patch({ cron: event.target.value })}
                  />
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.code_health.cron_hint)}
                  </p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="code-health-timezone">
                    {t(($) => $.code_health.timezone_label)}
                  </Label>
                  <Input
                    id="code-health-timezone"
                    value={form.timezone}
                    onChange={(event) => patch({ timezone: event.target.value })}
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="code-health-max-issues">
                    {t(($) => $.code_health.max_issues_label)}
                  </Label>
                  <Input
                    id="code-health-max-issues"
                    type="number"
                    min={settings?.min_issues_allowed ?? 1}
                    max={settings?.max_issues_allowed ?? 20}
                    value={form.max_issues_per_scan}
                    onChange={(event) =>
                      patch({ max_issues_per_scan: Number(event.target.value) })
                    }
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="code-health-min-confidence">
                    {t(($) => $.code_health.min_confidence_label)}
                  </Label>
                  <Input
                    id="code-health-min-confidence"
                    type="number"
                    min={0}
                    max={100}
                    value={form.min_confidence}
                    onChange={(event) =>
                      patch({ min_confidence: Number(event.target.value) })
                    }
                  />
                </div>
              </div>

              <Button type="submit" size="sm" disabled={saveSettings.isPending}>
                {t(($) => $.code_health.save)}
              </Button>
            </form>
          )}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.code_health.history_title)}
        description={t(($) => $.code_health.history_description)}
      >
        <SettingsCard>
          {scans.length === 0 ? (
            <p
              className="px-4 py-8 text-center text-caption text-muted-foreground"
              data-testid="code-health-scans-empty"
            >
              {t(($) => $.code_health.history_empty)}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.code_health.col_date)}</TableHead>
                  <TableHead>{t(($) => $.code_health.col_status)}</TableHead>
                  <TableHead>{t(($) => $.code_health.col_findings)}</TableHead>
                  <TableHead>{t(($) => $.code_health.col_issues)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scans.map((scan) => (
                  <ScanRow
                    key={scan.id}
                    scan={scan}
                    timeAgo={timeAgo}
                    issueHref={(id: string) => paths.issueDetail(id)}
                  />
                ))}
              </TableBody>
            </Table>
          )}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

function ScanRow({
  scan,
  timeAgo,
  issueHref,
}: {
  scan: CodeHealthScan;
  timeAgo: (value: string) => string;
  issueHref: (issueId: string) => string;
}) {
  const { t } = useT("settings");
  const opened = openedFindings(scan);
  return (
    <TableRow data-testid="code-health-scan-row" data-status={scan.status}>
      <TableCell className="whitespace-nowrap">{timeAgo(scan.created_at)}</TableCell>
      <TableCell>
        <ScanStatus status={scan.status} />
        {scan.error ? (
          <p className="mt-1 max-w-md text-caption text-destructive">{scan.error}</p>
        ) : null}
      </TableCell>
      <TableCell>
        {scan.findings.length === 0 ? (
          <span className="text-muted-foreground">
            {t(($) => $.code_health.no_findings)}
          </span>
        ) : (
          <ul className="space-y-1">
            {scan.findings.map((finding, index) => (
              <li key={`${scan.id}-${index}`} className="text-caption">
                <span className="font-medium">{finding.title}</span>{" "}
                <span className="text-muted-foreground">
                  {t(($) => $.code_health.finding_meta, {
                    kind: finding.kind,
                    confidence: finding.confidence,
                  })}
                </span>
                {finding.skipped ? (
                  <span className="ml-1 text-muted-foreground">
                    · <SkipReason reason={finding.skipped} />
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </TableCell>
      <TableCell>
        {opened.length === 0 ? (
          <span className="text-muted-foreground">{scan.issues_created}</span>
        ) : (
          <ul className="space-y-1">
            {opened.map((finding) => (
              <li key={finding.issue_id} className="text-caption">
                <Button
                  variant="link"
                  size="sm"
                  className="h-auto p-0"
                  render={<AppLink href={issueHref(finding.issue_id)} />}
                >
                  {finding.title}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </TableCell>
    </TableRow>
  );
}

function ScanStatus({ status }: { status: string }) {
  const { t } = useT("settings");
  const known = (SCAN_STATUSES as readonly string[]).includes(status);
  const tone = scanTone(status);
  return (
    <Badge
      variant={tone === "destructive" ? "destructive" : "outline"}
      className={TONE_CLASS[tone]}
      data-testid="code-health-scan-status"
      data-status={status}
    >
      {known
        ? t(($) => $.code_health.status[status as (typeof SCAN_STATUSES)[number]])
        : t(($) => $.code_health.status_unknown)}
    </Badge>
  );
}

function SkipReason({ reason }: { reason: string }) {
  const { t } = useT("settings");
  const known = (SKIP_REASONS as readonly string[]).includes(reason);
  return (
    <>
      {known
        ? t(($) => $.code_health.skipped[reason as (typeof SKIP_REASONS)[number]])
        : t(($) => $.code_health.skipped_unknown)}
    </>
  );
}
