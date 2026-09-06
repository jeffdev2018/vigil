"use client";

import { useQuery } from "@tanstack/react-query";
import { DatabaseZap } from "lucide-react";
import { toast } from "sonner";
import { Switch } from "@multica/ui/components/ui/switch";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  repoIndexSettingsOptions,
  repoIndexState,
  shortCommit,
  useSaveRepoIndexSettings,
  type RepoIndexRepo,
} from "@multica/core/repo-index";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Shared semantic index per repository (K47).
 *
 * One toggle per repository the workspace knows about. It is opt-in because
 * enabling it copies that repository's source into Multica's database, and on a
 * deployment with an embeddings model configured, sends it to that provider —
 * so the row states what is stored, at which commit, rather than only "on".
 */
export function RepoIndexSection({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const settingsQuery = useQuery(repoIndexSettingsOptions(wsId));
  const save = useSaveRepoIndexSettings(wsId);

  const repos = settingsQuery.data?.repos ?? [];

  const toggle = (repo: RepoIndexRepo, enabled: boolean) => {
    save.mutate(
      { repo_identifier: repo.repo_identifier, enabled },
      {
        onError: (error) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t(($) => $.repo_index.save_failed),
          ),
      },
    );
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <DatabaseZap className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.repo_index.section)}
        </span>
      }
      description={
        settingsQuery.data?.embeddings_enabled === true
          ? t(($) => $.repo_index.description_semantic)
          : t(($) => $.repo_index.description_lexical)
      }
    >
      <SettingsCard>
        {repos.length === 0 ? (
          <div className="px-4 py-8 text-center text-caption text-muted-foreground">
            {t(($) => $.repo_index.empty)}
          </div>
        ) : (
          repos.map((repo) => (
            <SettingsRow
              key={repo.repo_identifier}
              label={
                <span className="block truncate font-mono text-caption">
                  {repo.repo_identifier}
                </span>
              }
              description={<RepoIndexStatus repo={repo} />}
            >
              <Switch
                aria-label={repo.repo_identifier}
                checked={repo.enabled === true}
                disabled={!canEdit || save.isPending}
                onCheckedChange={(checked) => toggle(repo, checked === true)}
              />
            </SettingsRow>
          ))
        )}
      </SettingsCard>
    </SettingsSection>
  );
}

/**
 * The one line under each repository. "Indexing" is not a spinner: the index is
 * built by the daemon after a run finishes, so an enabled repository with no
 * chunks is genuinely waiting for the next run rather than working right now,
 * and the copy says so.
 */
function RepoIndexStatus({ repo }: { repo: RepoIndexRepo }) {
  const { t } = useT("settings");
  switch (repoIndexState(repo)) {
    case "indexed":
      return (
        <>
          {t(($) => $.repo_index.status_indexed, {
            commit: shortCommit(repo.last_indexed_commit),
            chunks: repo.chunk_count ?? 0,
            files: repo.file_count ?? 0,
          })}
        </>
      );
    case "indexing":
      return <>{t(($) => $.repo_index.status_waiting)}</>;
    default:
      return <>{t(($) => $.repo_index.status_disabled)}</>;
  }
}
