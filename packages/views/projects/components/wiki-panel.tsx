"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, BookText, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  citationUrl,
  formatCitation,
  projectCodeWikiOptions,
  projectCodeWikiPageOptions,
  repoUrlFromResource,
  useRefreshProjectCodeWiki,
  type CodeWikiCitation,
} from "@multica/core/projects";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";

/**
 * Generated code wiki (F26): the table of contents on the left, the selected
 * page rendered on the right.
 *
 * The provenance banner is not decoration. Everything here was written by a
 * model from a repository Multica does not control, so the commit it describes,
 * the date and the fact that it is generated stay on screen while the content
 * is read — and a page that cites nothing says so rather than passing for
 * sourced.
 */
export function WikiPanel({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [slug, setSlug] = useState<string | null>(null);
  const { data: wiki, isLoading, isError } = useQuery(projectCodeWikiOptions(wsId, projectId));
  const refresh = useRefreshProjectCodeWiki(wsId, projectId);

  const snapshot = wiki?.snapshot ?? null;
  const pages = wiki?.pages ?? [];
  const activeSlug = slug ?? pages[0]?.slug ?? null;
  const building = wiki?.building === true;

  const generate = () => {
    refresh.mutate(undefined, {
      onSuccess: (result) => {
        if (result?.started === true) toast.success(t(($) => $.wiki.started));
        else toast.message(result?.reason || t(($) => $.wiki.not_started));
      },
      onError: (e) =>
        toast.error(e instanceof Error && e.message ? e.message : t(($) => $.wiki.not_started)),
    });
  };

  return (
    <div data-testid="project-wiki" className="mt-6 text-caption">
      <div className="mb-2 flex items-center gap-2 px-2">
        <BookText className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.wiki.section)}</span>
      </div>
      <p className="mb-2 px-2 text-muted-foreground">{t(($) => $.wiki.description)}</p>

      {isLoading && <Skeleton className="mx-2 h-4 w-24" />}
      {isError && <p className="px-2 text-destructive">{t(($) => $.wiki.failed)}</p>}

      {!isLoading && !isError && wiki?.resource == null && (
        <p data-testid="wiki-no-repo" className="px-2 text-muted-foreground">
          {t(($) => $.wiki.no_repo)}
        </p>
      )}

      {!isLoading && !isError && wiki?.resource != null && (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-2 px-2">
            {building && (
              <span
                data-testid="wiki-building"
                className="rounded-sm bg-muted px-1.5 py-0.5 text-muted-foreground"
              >
                {t(($) => $.wiki.building)}
              </span>
            )}
            <Button size="sm" variant="outline" onClick={generate} disabled={refresh.isPending || building}>
              {snapshot ? t(($) => $.wiki.regenerate) : t(($) => $.wiki.generate)}
            </Button>
          </div>

          {snapshot == null ? (
            <p data-testid="wiki-empty" className="px-2 text-muted-foreground">
              {t(($) => $.wiki.empty)}
            </p>
          ) : (
            <>
              <p data-testid="wiki-banner" className="px-2 text-muted-foreground">
                {t(($) => $.wiki.banner, {
                  commit: (snapshot.commit_sha ?? "").slice(0, 7),
                  date: (snapshot.published_at ?? "").slice(0, 10),
                })}
              </p>
              {snapshot.stale === true && (
                <p
                  data-testid="wiki-stale"
                  className="mt-1 flex items-center gap-1 px-2 text-muted-foreground"
                >
                  <AlertTriangle className="h-3.5 w-3.5" />
                  {t(($) => $.wiki.stale)}
                </p>
              )}
              <div className="mt-2 flex gap-4 px-2">
                <nav className="w-40 shrink-0">
                  <div className="mb-1 font-medium">{t(($) => $.wiki.pages)}</div>
                  <ul className="flex max-h-80 flex-col gap-0.5 overflow-y-auto">
                    {pages.map((page) => (
                      <li key={page.id || page.slug}>
                        <button
                          type="button"
                          data-testid="wiki-toc-entry"
                          data-active={page.slug === activeSlug}
                          onClick={() => setSlug(page.slug)}
                          className="w-full truncate rounded-sm px-1 py-0.5 text-left hover:bg-muted data-[active=true]:font-medium data-[active=true]:text-foreground data-[active=true]:hover:bg-muted"
                          title={page.title}
                        >
                          {page.title || page.slug}
                        </button>
                      </li>
                    ))}
                  </ul>
                </nav>
                <div className="min-w-0 flex-1">
                  {activeSlug == null ? (
                    <p className="text-muted-foreground">{t(($) => $.wiki.select_page)}</p>
                  ) : (
                    <WikiPage
                      projectId={projectId}
                      slug={activeSlug}
                      repoUrl={repoUrlFromResource(wiki?.resource)}
                    />
                  )}
                </div>
              </div>
            </>
          )}
        </>
      )}
    </div>
  );
}

function WikiPage({
  projectId,
  slug,
  repoUrl,
}: {
  projectId: string;
  slug: string;
  repoUrl: string | null;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: page, isLoading, isError } = useQuery(projectCodeWikiPageOptions(wsId, projectId, slug));

  if (isLoading) return <Skeleton className="h-4 w-24" />;
  if (isError || !page) return <p className="text-destructive">{t(($) => $.wiki.page_failed)}</p>;

  const citations: CodeWikiCitation[] = page.citations ?? [];
  return (
    <div data-testid="wiki-page" className="min-w-0">
      <div className="max-h-96 overflow-y-auto [&_pre]:overflow-x-auto">
        <RichContent content={page.content ?? ""} />
      </div>
      <div className="mt-2 font-medium">{t(($) => $.wiki.citations)}</div>
      {citations.length === 0 ? (
        <p data-testid="wiki-no-citations" className="text-muted-foreground">
          {t(($) => $.wiki.no_citations)}
        </p>
      ) : (
        <ul className="flex flex-col gap-0.5">
          {citations.map((citation, i) => {
            const label = formatCitation(citation);
            const href = citationUrl(repoUrl, page.commit_sha ?? "", citation);
            return (
              <li key={`${label}-${i}`} data-testid="wiki-citation" className="truncate">
                {href ? (
                  <a
                    href={href}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 hover:underline"
                  >
                    <span className="truncate">{label}</span>
                    <ExternalLink className="h-3 w-3 shrink-0" />
                  </a>
                ) : (
                  <span className="text-muted-foreground">{label}</span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
