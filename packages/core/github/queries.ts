import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const githubKeys = {
  all: (wsId: string) => ["github", wsId] as const,
  installations: (wsId: string) => [...githubKeys.all(wsId), "installations"] as const,
  repositories: (wsId: string, installationId: string) =>
    [...githubKeys.all(wsId), "installations", installationId, "repositories"] as const,
  pullRequests: (issueId: string) => ["github", "pull-requests", issueId] as const,
  // Merge readiness (F10) is keyed like pullRequests: by issue, not workspace,
  // and the realtime `pull_request` / comment / issue paths invalidate the
  // prefixes below.
  mergeReadinessAll: () => ["github", "merge-readiness"] as const,
  mergeReadiness: (issueId: string) => [...githubKeys.mergeReadinessAll(), issueId] as const,
  prStackAll: () => ["github", "pr-stack"] as const,
  prStack: (issueId: string) => [...githubKeys.prStackAll(), issueId] as const,
};

export const githubInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: githubKeys.installations(wsId),
    queryFn: () => api.listGitHubInstallations(wsId),
    enabled: !!wsId,
  });

export const githubInstallationRepositoriesOptions = (
  wsId: string,
  installationId: string,
) =>
  infiniteQueryOptions({
    queryKey: githubKeys.repositories(wsId, installationId),
    queryFn: ({ pageParam }) =>
      api.listGitHubInstallationRepositories(wsId, installationId, {
        page: pageParam,
        per_page: 100,
      }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) => lastPage.next_page ?? undefined,
    enabled: !!wsId && !!installationId,
  });

export const issuePullRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequests(issueId),
    queryFn: () => api.listIssuePullRequests(issueId),
    enabled: !!issueId,
  });

export const issueMergeReadinessOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.mergeReadiness(issueId),
    queryFn: () => api.getIssueMergeReadiness(issueId),
    enabled: !!issueId,
  });

export const issuePRStackOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.prStack(issueId),
    queryFn: () => api.getIssuePRStack(issueId),
    enabled: !!issueId,
  });
