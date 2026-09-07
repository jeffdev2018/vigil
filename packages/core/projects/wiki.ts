import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "../api";

// F26 (JEF-22): the generated code wiki for a project's repository.
//
// Everything here describes content a language model wrote from a repository we
// do not control. The UI's job is to keep that visible: `generated`, the commit
// and the staleness flag travel with the payload rather than being inferred, and
// a page that arrives without citations still renders — marked unsourced — so a
// backend that stopped sending them cannot blank the panel.

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; the UI switch carries a default branch.
export const CodeWikiCitationSchema = z
  .object({
    path: z.string().catch(""),
    start_line: z.number().nullable().catch(null).default(null),
    end_line: z.number().nullable().catch(null).default(null),
    commit_sha: z.string().catch("").default(""),
  })
  .loose();

export const CodeWikiSnapshotSchema = z
  .object({
    id: z.string().catch(""),
    project_resource_id: z.string().catch("").default(""),
    commit_sha: z.string().catch("").default(""),
    state: z.string().catch("published"),
    page_count: z.number().catch(0).default(0),
    created_at: z.string().catch("").default(""),
    published_at: z.string().catch("").default(""),
    generated: z.boolean().catch(true).default(true),
    stale: z.boolean().catch(false).default(false),
  })
  .loose();

export const CodeWikiPageSummarySchema = z
  .object({
    id: z.string().catch(""),
    slug: z.string().catch(""),
    title: z.string().catch(""),
    citation_count: z.number().catch(0).default(0),
  })
  .loose();

export const CodeWikiPageSchema = z
  .object({
    id: z.string().catch(""),
    slug: z.string().catch(""),
    title: z.string().catch(""),
    content: z.string().catch("").default(""),
    citations: z.array(CodeWikiCitationSchema).catch([]).default([]),
    commit_sha: z.string().catch("").default(""),
    generated: z.boolean().catch(true).default(true),
    stale: z.boolean().catch(false).default(false),
  })
  .loose();

export const CodeWikiSchema = z
  .object({
    resource: z.unknown().nullable().catch(null).default(null),
    snapshot: CodeWikiSnapshotSchema.nullable().catch(null).default(null),
    pages: z.array(CodeWikiPageSummarySchema).catch([]).default([]),
    building: z.boolean().catch(false).default(false),
    generated: z.boolean().catch(true).default(true),
  })
  .loose();

export type CodeWikiCitation = z.infer<typeof CodeWikiCitationSchema>;
export type CodeWikiSnapshot = z.infer<typeof CodeWikiSnapshotSchema>;
export type CodeWikiPageSummary = z.infer<typeof CodeWikiPageSummarySchema>;
export type CodeWikiPage = z.infer<typeof CodeWikiPageSchema>;
export type CodeWiki = z.infer<typeof CodeWikiSchema>;

export const EMPTY_CODE_WIKI: CodeWiki = {
  resource: null,
  snapshot: null,
  pages: [],
  building: false,
  generated: true,
};

// A generation takes minutes; poll only while one is running.
const WIKI_POLL_MS = 15_000;

export const codeWikiKeys = {
  all: (wsId: string) => ["project-wiki", wsId] as const,
  detail: (wsId: string, projectId: string) =>
    ["project-wiki", wsId, projectId] as const,
  page: (wsId: string, projectId: string, slug: string) =>
    ["project-wiki", wsId, projectId, "page", slug] as const,
};

export function projectCodeWikiOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: codeWikiKeys.detail(wsId, projectId),
    queryFn: () => api.getProjectCodeWiki(projectId),
    enabled: !!wsId && !!projectId,
    refetchInterval: (query) =>
      (query.state.data as CodeWiki | undefined)?.building === true
        ? WIKI_POLL_MS
        : false,
  });
}

export function projectCodeWikiPageOptions(
  wsId: string,
  projectId: string,
  slug: string,
) {
  return queryOptions({
    queryKey: codeWikiKeys.page(wsId, projectId, slug),
    queryFn: () => api.getProjectCodeWikiPage(projectId, slug),
    enabled: !!wsId && !!projectId && !!slug,
  });
}

// Refreshing navigates nowhere and its outcome is a server decision (a run may
// already be in flight), so it is awaited and then invalidated rather than
// predicted optimistically.
export function useRefreshProjectCodeWiki(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.refreshProjectCodeWiki(projectId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: codeWikiKeys.detail(wsId, projectId) });
    },
  });
}

// A citation with no line numbers points at the whole file.
export function formatCitation(citation: CodeWikiCitation): string {
  const path = citation?.path ?? "";
  const start = citation?.start_line ?? null;
  const end = citation?.end_line ?? null;
  if (!path) return "";
  if (start === null) return path;
  if (end === null || end === start) return `${path}:${start}`;
  return `${path}:${start}-${end}`;
}

// Build a link into the forge for a citation. Returns null when the repository
// URL is not one we know how to deep-link into, so the caller renders plain
// text rather than a broken link.
export function citationUrl(
  repoUrl: string | null | undefined,
  commitSha: string,
  citation: CodeWikiCitation,
): string | null {
  const path = citation?.path ?? "";
  const url = (repoUrl ?? "").trim();
  const sha = (commitSha ?? "").trim();
  if (!path || !url || !sha) return null;
  if (!/^https?:\/\//i.test(url)) return null;
  const base = url.replace(/\.git$/i, "").replace(/\/+$/, "");
  const start = citation?.start_line ?? null;
  const end = citation?.end_line ?? null;
  let fragment = "";
  if (start !== null) {
    fragment = end !== null && end !== start ? `#L${start}-L${end}` : `#L${start}`;
  }
  return `${base}/blob/${encodeURIComponent(sha)}/${path}${fragment}`;
}

// The repository URL lives in the resource ref, whose shape is server-owned.
export function repoUrlFromResource(resource: unknown): string | null {
  const ref = (resource as { resource_ref?: unknown } | null)?.resource_ref;
  const url = (ref as { url?: unknown } | null)?.url;
  return typeof url === "string" && url.trim() !== "" ? url.trim() : null;
}
