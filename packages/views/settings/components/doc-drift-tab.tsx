"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, FileDiff, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
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
import { useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  docDriftProposalsOptions,
  docDriftSettingsOptions,
  driftExcerpt,
  proposalTone,
  shortDriftCommit,
  useCheckDocDrift,
  useDismissDocDriftProposal,
  useOpenDocDriftProposalPR,
  useSaveDocDriftSettings,
  type DocDriftProposal,
  type DocDriftRepo,
  type DocDriftSettingsInput,
} from "@multica/core/doc-drift";
import { useT, useTimeAgo } from "../../i18n";
import { StatusBadge, type StatusBadgeConfig } from "../../common/status-badge";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * Agent context document drift detection (K56).
 *
 * The document every agent reads before it acts is the one nobody remembers to
 * update. This screen is the review desk for that: which repositories moved,
 * what a read-only run found had stopped being true, and the draft pull request
 * a human approves. Nothing here commits to the default branch.
 *
 * Off by default, admin-only to configure.
 */


const PROPOSAL_STATUSES = ["draft", "opened_pr", "dismissed", "merged"] as const;

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function DocDriftTab() {
  const { t } = useT("settings");
  const wsId = useCurrentWorkspace()?.id ?? "";

  const settingsQuery = useQuery(docDriftSettingsOptions(wsId));
  const proposalsQuery = useQuery(docDriftProposalsOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const saveSettings = useSaveDocDriftSettings(wsId);
  const check = useCheckDocDrift(wsId);

  const settings = settingsQuery.data;
  const proposals = proposalsQuery.data ?? [];
  const agents = agentsQuery.data ?? [];
  const repos = settings?.repos ?? [];

  const [form, setForm] = useState<DocDriftSettingsInput | null>(null);
  // The form mirrors the server until the admin edits it; a refetch that lands
  // mid-edit must not overwrite what they typed, hence the null guard.
  useEffect(() => {
    if (!settings || form) return;
    setForm({
      enabled: settings.enabled,
      agent_id: settings.agent_id,
      docs: settings.docs,
      open_pr: settings.open_pr,
    });
  }, [settings, form]);

  const patch = (next: Partial<DocDriftSettingsInput>) =>
    setForm((current) => (current ? { ...current, ...next } : current));

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!form) return;
    saveSettings.mutate(form, {
      onSuccess: () => toast.success(t(($) => $.doc_drift.saved_toast)),
      onError: (error) =>
        toast.error(errorMessage(error, t(($) => $.doc_drift.save_failed))),
    });
  };

  const handleCheck = (repo: string) => {
    check.mutate(repo, {
      onSuccess: () => toast.success(t(($) => $.doc_drift.check_started_toast)),
      onError: (error) =>
        toast.error(errorMessage(error, t(($) => $.doc_drift.check_failed))),
    });
  };

  const configured = !!form?.agent_id;

  return (
    <SettingsTab
      title={t(($) => $.doc_drift.title)}
      description={t(($) => $.doc_drift.description)}
    >
      {/* Onboarding: what this does and where it stops, before any switch. */}
      {!configured ? (
        <SettingsCard>
          <div className="flex gap-3 px-4 py-4">
            <FileDiff className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="space-y-2">
              <p className="text-body font-medium">
                {t(($) => $.doc_drift.onboarding_title)}
              </p>
              <p className="text-caption leading-5 text-muted-foreground">
                {t(($) => $.doc_drift.onboarding_body)}
              </p>
            </div>
          </div>
        </SettingsCard>
      ) : null}

      <SettingsSection
        title={t(($) => $.doc_drift.config_title)}
        description={t(($) => $.doc_drift.config_description)}
      >
        <SettingsCard>
          {settingsQuery.isPending || !form ? (
            <p className="px-4 py-8 text-center text-caption text-muted-foreground">
              {t(($) => $.doc_drift.loading)}
            </p>
          ) : (
            <form className="space-y-4 px-4 py-4" onSubmit={handleSubmit}>
              <label className="flex items-center justify-between gap-4">
                <span className="min-w-0">
                  <span className="block text-body">
                    {t(($) => $.doc_drift.enabled_label)}
                  </span>
                  <span className="block text-caption text-muted-foreground">
                    {t(($) => $.doc_drift.enabled_hint)}
                  </span>
                </span>
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(checked: boolean) => patch({ enabled: checked })}
                  data-testid="doc-drift-enabled"
                />
              </label>

              <div className="space-y-1.5">
                <Label>{t(($) => $.doc_drift.agent_label)}</Label>
                <Select
                  items={[
                    { value: "", label: t(($) => $.doc_drift.pick_agent) },
                    ...agents.map((agent) => ({ value: agent.id, label: agent.name })),
                  ]}
                  value={form.agent_id}
                  onValueChange={(value) => patch({ agent_id: value ?? "" })}
                >
                  <SelectTrigger aria-label={t(($) => $.doc_drift.agent_label)} size="sm" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="">{t(($) => $.doc_drift.pick_agent)}</SelectItem>
                    {agents.map((agent) => (
                      <SelectItem key={agent.id} value={agent.id}>
                        {agent.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="doc-drift-docs">
                  {t(($) => $.doc_drift.docs_label)}
                </Label>
                <Textarea
                  id="doc-drift-docs"
                  rows={4}
                  value={form.docs.join("\n")}
                  onChange={(event) =>
                    patch({
                      docs: event.target.value
                        .split("\n")
                        .map((line) => line.trim())
                        .filter(Boolean),
                    })
                  }
                />
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.doc_drift.docs_hint)}
                </p>
              </div>

              <label className="flex items-center justify-between gap-4">
                <span className="min-w-0">
                  <span className="block text-body">
                    {t(($) => $.doc_drift.open_pr_label)}
                  </span>
                  <span className="block text-caption text-muted-foreground">
                    {t(($) => $.doc_drift.open_pr_hint)}
                  </span>
                </span>
                <Switch
                  checked={form.open_pr}
                  onCheckedChange={(checked: boolean) => patch({ open_pr: checked })}
                  data-testid="doc-drift-open-pr"
                />
              </label>

              <Button type="submit" size="sm" disabled={saveSettings.isPending}>
                {t(($) => $.doc_drift.save)}
              </Button>
            </form>
          )}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.doc_drift.repos_title)}
        description={t(($) => $.doc_drift.repos_description)}
      >
        <SettingsCard>
          {repos.length === 0 ? (
            <p
              className="px-4 py-8 text-center text-caption text-muted-foreground"
              data-testid="doc-drift-repos-empty"
            >
              {t(($) => $.doc_drift.repos_empty)}
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {repos.map((repo) => (
                <RepoRow
                  key={repo.repo_identifier}
                  repo={repo}
                  disabled={!configured || check.isPending}
                  onCheck={() => handleCheck(repo.repo_identifier)}
                />
              ))}
            </ul>
          )}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.doc_drift.proposals_title)}
        description={t(($) => $.doc_drift.proposals_description)}
      >
        <SettingsCard>
          {proposals.length === 0 ? (
            <p
              className="px-4 py-8 text-center text-caption text-muted-foreground"
              data-testid="doc-drift-proposals-empty"
            >
              {t(($) => $.doc_drift.proposals_empty)}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.doc_drift.col_document)}</TableHead>
                  <TableHead>{t(($) => $.doc_drift.col_repository)}</TableHead>
                  <TableHead>{t(($) => $.doc_drift.col_drift)}</TableHead>
                  <TableHead>{t(($) => $.doc_drift.col_status)}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {proposals.map((proposal) => (
                  <ProposalRow key={proposal.id} proposal={proposal} wsId={wsId} />
                ))}
              </TableBody>
            </Table>
          )}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

function RepoRow({
  repo,
  disabled,
  onCheck,
}: {
  repo: DocDriftRepo;
  disabled: boolean;
  onCheck: () => void;
}) {
  const { t } = useT("settings");
  const hint = repo.scanning
    ? t(($) => $.doc_drift.repo_scanning)
    : !repo.last_indexed_commit
      ? t(($) => $.doc_drift.repo_never_indexed)
      : repo.due
        ? t(($) => $.doc_drift.repo_due)
        : t(($) => $.doc_drift.repo_up_to_date);
  return (
    <li
      className="flex items-center justify-between gap-4 px-4 py-3"
      data-testid="doc-drift-repo-row"
      data-repo={repo.repo_identifier}
      data-due={repo.due}
    >
      <div className="min-w-0">
        <p className="truncate text-body">{repo.repo_identifier}</p>
        <p className="text-caption text-muted-foreground">
          {hint}
          {repo.last_indexed_commit ? ` · ${shortDriftCommit(repo.last_indexed_commit)}` : ""}
        </p>
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={onCheck}
        disabled={disabled || repo.scanning || !repo.last_indexed_commit}
        data-testid="doc-drift-check-now"
      >
        {repo.scanning ? <Loader2 className="size-3.5 animate-spin" /> : null}
        {t(($) => $.doc_drift.check_now)}
      </Button>
    </li>
  );
}

function ProposalRow({ proposal, wsId }: { proposal: DocDriftProposal; wsId: string }) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const dismiss = useDismissDocDriftProposal(wsId);
  const openPR = useOpenDocDriftProposalPR(wsId);
  const open = proposal.status === "draft" || proposal.status === "opened_pr";

  return (
    <TableRow data-testid="doc-drift-proposal-row" data-status={proposal.status}>
      <TableCell className="align-top">
        <p className="font-medium">{proposal.doc_path}</p>
        <p className="text-caption text-muted-foreground">
          {timeAgo(proposal.updated_at)}
          {proposal.detected_at_commit
            ? ` · ${shortDriftCommit(proposal.detected_at_commit)}`
            : ""}
        </p>
      </TableCell>
      <TableCell className="max-w-[16rem] truncate align-top text-caption text-muted-foreground">
        {proposal.repo_identifier}
      </TableCell>
      <TableCell className="max-w-md align-top text-caption">
        {driftExcerpt(proposal)}
      </TableCell>
      <TableCell className="align-top">
        <ProposalStatus status={proposal.status} />
      </TableCell>
      <TableCell className="align-top">
        <div className="flex flex-wrap items-center justify-end gap-2">
          {proposal.pull_request_url ? (
            <Button
              size="sm"
              variant="link"
              className="h-auto p-0"
              nativeButton={false}
              data-testid="doc-drift-pr-link"
              render={
                <a
                  href={proposal.pull_request_url}
                  target="_blank"
                  rel="noreferrer noopener"
                />
              }
            >
              <ExternalLink className="size-3.5" />
              {t(($) => $.doc_drift.view_pr)}
            </Button>
          ) : null}
          {proposal.status === "draft" ? (
            <Button
              size="sm"
              variant="outline"
              disabled={openPR.isPending}
              onClick={() =>
                openPR.mutate(proposal.id, {
                  onSuccess: () =>
                    toast.success(t(($) => $.doc_drift.open_pr_started_toast)),
                  onError: (error) =>
                    toast.error(
                      errorMessage(error, t(($) => $.doc_drift.open_pr_failed)),
                    ),
                })
              }
              data-testid="doc-drift-open-pr-action"
            >
              {t(($) => $.doc_drift.open_pr)}
            </Button>
          ) : null}
          {open ? (
            <Button
              size="sm"
              variant="ghost"
              disabled={dismiss.isPending}
              onClick={() =>
                dismiss.mutate(proposal.id, {
                  onSuccess: () =>
                    toast.success(t(($) => $.doc_drift.dismissed_toast)),
                  onError: (error) =>
                    toast.error(
                      errorMessage(error, t(($) => $.doc_drift.dismiss_failed)),
                    ),
                })
              }
              data-testid="doc-drift-dismiss"
            >
              {t(($) => $.doc_drift.dismiss)}
            </Button>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  );
}

function ProposalStatus({ status }: { status: string }) {
  const { t } = useT("settings");
  const config: StatusBadgeConfig = Object.fromEntries(
    PROPOSAL_STATUSES.map((s) => [s, { tone: proposalTone(s), label: t(($) => $.doc_drift.status[s]) }]),
  );
  return <StatusBadge status={status} config={config} data-testid="doc-drift-proposal-status" />;
}
