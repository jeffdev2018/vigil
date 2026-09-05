"use client";

import { useState } from "react";
import { ArrowLeftRight } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { saveTransferBlob, transferKeys, transferRunsOptions } from "@multica/core/workspace/transfer";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { projectKeys } from "@multica/core/projects/queries";
import { goalKeys } from "@multica/core/goals";
import { autopilotKeys } from "@multica/core/autopilots/queries";
import { orgKeys } from "@multica/core/org";
import { issueKeys } from "@multica/core/issues/queries";
import type { TransferImportResult, TransferPreview, TransferSecretValues, TransferStrategy } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT, useTimeAgo } from "../../i18n";

const selectClass = "h-8 rounded-md border bg-background px-2 text-caption";

/** Workspace export / import (K76): bundle download, guided import, and the run history. */
export function ExportImportSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: runs = [] } = useQuery(transferRunsOptions(wsId));

  const [includeIssues, setIncludeIssues] = useState(false);
  const [includeNotes, setIncludeNotes] = useState(false);
  const [asTemplate, setAsTemplate] = useState(false);
  const [templateName, setTemplateName] = useState("");
  const [exporting, setExporting] = useState(false);

  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<TransferPreview | null>(null);
  const [strategy, setStrategy] = useState<TransferStrategy>("rename");
  const [secrets, setSecrets] = useState<TransferSecretValues>({});
  const [importing, setImporting] = useState(false);
  const [result, setResult] = useState<TransferImportResult | null>(null);

  async function exportBundle() {
    setExporting(true);
    try {
      const { blob, filename } = await api.exportWorkspace({
        include_issues: includeIssues,
        include_notes: includeNotes,
        template: asTemplate,
        name: asTemplate ? templateName.trim() || undefined : undefined,
      });
      saveTransferBlob(blob, filename);
      toast.success(t(($) => $.transfer.export_done));
      void qc.invalidateQueries({ queryKey: transferKeys.runs(wsId) });
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.transfer.export_failed));
    } finally {
      setExporting(false);
    }
  }

  async function pickFile(next: File | null) {
    setFile(next);
    setPreview(null);
    setResult(null);
    setSecrets({});
    if (!next) return;
    try {
      const p = await api.previewWorkspaceImport(next);
      setPreview(p);
      setStrategy(p.strategies?.includes("rename") ? "rename" : (p.strategies?.[0] ?? "rename"));
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.transfer.preview_failed));
    }
  }

  async function runImport() {
    if (!file) return;
    setImporting(true);
    try {
      const r = await api.importWorkspace(file, strategy, secrets);
      setResult(r);
      toast.success(t(($) => $.transfer.import_done));
      // The bundle may have touched any of these collections; refetch them all.
      for (const key of [
        workspaceKeys.agents(wsId),
        workspaceKeys.skills(wsId),
        projectKeys.all(wsId),
        goalKeys.all(wsId),
        autopilotKeys.all(wsId),
        orgKeys.all(wsId),
        issueKeys.all(wsId),
        transferKeys.runs(wsId),
      ]) {
        void qc.invalidateQueries({ queryKey: key });
      }
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.transfer.import_failed));
    } finally {
      setImporting(false);
    }
  }

  const disabled = !canEdit;
  const agentSecrets = preview?.secrets?.filter((s) => s.scope === "agent") ?? [];

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ArrowLeftRight className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.transfer.section)}
        </span>
      }
      description={t(($) => $.transfer.description)}
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.transfer.export_title)} description={t(($) => $.transfer.export_description)} align="start">
          <div data-testid="transfer-export" className="flex flex-col gap-2 text-caption">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={includeIssues} disabled={disabled} onChange={(e) => setIncludeIssues(e.target.checked)} />
              {t(($) => $.transfer.include_issues)}
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={includeNotes} disabled={disabled} onChange={(e) => setIncludeNotes(e.target.checked)} />
              {t(($) => $.transfer.include_notes)}
            </label>
            <label className="flex items-center gap-2">
              <Switch checked={asTemplate} disabled={disabled} onCheckedChange={(v) => setAsTemplate(v === true)} />
              {t(($) => $.transfer.as_template)}
            </label>
            {asTemplate && (
              <Input aria-label={t(($) => $.transfer.template_name)} placeholder={t(($) => $.transfer.template_name)} value={templateName} disabled={disabled} onChange={(e) => setTemplateName(e.target.value)} />
            )}
            <div>
              <Button type="button" size="sm" disabled={disabled || exporting} onClick={() => void exportBundle()}>
                {exporting ? t(($) => $.transfer.exporting) : t(($) => $.transfer.export_button)}
              </Button>
            </div>
          </div>
        </SettingsRow>

        <SettingsRow label={t(($) => $.transfer.import_title)} description={t(($) => $.transfer.import_description)} align="start">
          <div data-testid="transfer-import" className="flex flex-col gap-2 text-caption">
            <input aria-label={t(($) => $.transfer.import_file)} type="file" accept=".zip" disabled={disabled || importing} onChange={(e) => void pickFile(e.target.files?.[0] ?? null)} />
            {preview && (
              <div data-testid="transfer-preview" className="flex flex-col gap-2">
                <div className="font-medium">{t(($) => $.transfer.preview_source, { name: preview.manifest?.source?.Name ?? preview.manifest?.name ?? "" })}</div>
                <ul className="flex flex-wrap gap-x-3 gap-y-1 text-muted-foreground">
                  {Object.entries(preview.manifest?.counts ?? {}).map(([kind, n]) => (
                    <li key={kind}>{`${kind}: ${n}`}</li>
                  ))}
                </ul>
                {(preview.collisions?.length ?? 0) > 0 && (
                  <div>
                    <div className="font-medium">{t(($) => $.transfer.collisions)}</div>
                    <ul className="text-muted-foreground">
                      {preview.collisions.map((c) => (
                        <li key={`${c.kind}:${c.name}`}>{`${c.kind} · ${c.name}`}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {(preview.secrets?.length ?? 0) > 0 && (
                  <div>
                    <div className="font-medium">{t(($) => $.transfer.secrets)}</div>
                    <ul className="text-muted-foreground">
                      {preview.secrets.map((s) => (
                        <li key={`${s.scope}:${s.name}:${s.key}`}>{`${s.scope} · ${s.name} · ${s.key}`}</li>
                      ))}
                    </ul>
                  </div>
                )}
                <label className="flex items-center gap-2">
                  {t(($) => $.transfer.strategy)}
                  <select aria-label={t(($) => $.transfer.strategy)} className={selectClass} value={strategy} disabled={disabled} onChange={(e) => setStrategy(e.target.value as TransferStrategy)}>
                    {(preview.strategies ?? ["rename", "merge", "skip"]).map((s) => (
                      <option key={s} value={s}>
                        {t(($) => $.transfer[`strategy_${s}`])}
                      </option>
                    ))}
                  </select>
                </label>
                {agentSecrets.map((s) => {
                  const id = `${s.name}.${s.key}`;
                  return (
                    <label key={id} className="flex items-center gap-2">
                      <span className="min-w-0 truncate font-mono">{id}</span>
                      <Input
                        type="password"
                        aria-label={id}
                        autoComplete="off"
                        value={secrets[s.name]?.[s.key] ?? ""}
                        disabled={disabled}
                        onChange={(e) => setSecrets((prev) => ({ ...prev, [s.name]: { ...prev[s.name], [s.key]: e.target.value } }))}
                      />
                    </label>
                  );
                })}
                <div>
                  <Button type="button" size="sm" disabled={disabled || importing} onClick={() => void runImport()}>
                    {importing ? t(($) => $.transfer.importing) : t(($) => $.transfer.import_button)}
                  </Button>
                </div>
              </div>
            )}
            {result && (
              <div data-testid="transfer-report" className="flex flex-col gap-1">
                <div className="font-medium">{t(($) => $.transfer.report_title)}</div>
                <div className="text-muted-foreground">
                  {t(($) => $.transfer.report_counts, {
                    created: Object.values(result.report?.created ?? {}).reduce((a, b) => a + b, 0),
                    merged: Object.values(result.report?.merged ?? {}).reduce((a, b) => a + b, 0),
                  })}
                </div>
                {(result.report?.skipped?.length ?? 0) > 0 && (
                  <div className="text-muted-foreground">{`${t(($) => $.transfer.report_skipped)}: ${result.report.skipped.map((c) => `${c.kind} · ${c.name}`).join(", ")}`}</div>
                )}
                {(result.report?.secrets_pending?.length ?? 0) > 0 && (
                  <div className="text-muted-foreground">{`${t(($) => $.transfer.report_secrets_pending)}: ${result.report.secrets_pending.map((s) => `${s.name}.${s.key}`).join(", ")}`}</div>
                )}
                {(result.report?.warnings ?? []).map((w, i) => (
                  <div key={i} className="text-destructive">
                    {w}
                  </div>
                ))}
              </div>
            )}
          </div>
        </SettingsRow>

        <SettingsRow label={t(($) => $.transfer.history)} align="start">
          <div data-testid="transfer-history" className="flex flex-col gap-1 text-caption">
            {runs.length === 0 && <span className="text-muted-foreground">{t(($) => $.transfer.history_empty)}</span>}
            {runs.map((run) => (
              <div key={run.id} className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{t(($) => $.transfer[`direction_${run.direction}`])}</span>
                <span className="min-w-0 truncate">{run.name || run.source_name}</span>
                {run.template === true && <Badge variant="secondary">{t(($) => $.transfer.template_badge)}</Badge>}
                <span className="text-muted-foreground">{t(($) => $.transfer[`status_${run.status}`])}</span>
                {run.direction === "import" && run.strategy && <span className="text-muted-foreground">{run.strategy}</span>}
                <span className="text-muted-foreground">{timeAgo(run.created_at)}</span>
              </div>
            ))}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
