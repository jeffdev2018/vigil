"use client";

import { useCallback, useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { FileText, Check, X, Loader2, Sparkles, Wand2, Bot, ExternalLink } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { ApiError } from "@multica/core/api";
import type { Postmortem, PostmortemState } from "@multica/core/types";
import {
  postmortemStatsOptions,
  postmortemItemsOptions,
} from "@multica/core/postmortem/queries";
import {
  useApprovePostmortem,
  useDiscardPostmortem,
} from "@multica/core/postmortem/mutations";
import { useT, useTimeAgo } from "../../i18n";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";

const STATES: PostmortemState[] = ["draft", "approved", "discarded"];

/** Provider-reported cost in 1e-10 USD ticks → a short USD string. */
function formatCost(ticks: number): string {
  const usd = ticks / 1e10;
  return `$${usd.toFixed(usd < 0.01 ? 4 : 2)}`;
}

export function PostmortemPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("postmortem");

  const statsQuery = useQuery(postmortemStatsOptions(wsId));
  const [filterState, setFilterState] = useState<PostmortemState>("draft");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const itemsQuery = useInfiniteQuery(postmortemItemsOptions(wsId, filterState));
  const items = useMemo(
    () => itemsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [itemsQuery.data],
  );
  const stats = statsQuery.data;
  const selected = useMemo(
    () => items.find((item) => item.id === selectedId) ?? null,
    [items, selectedId],
  );

  const handleFilter = useCallback((state: PostmortemState) => {
    setFilterState(state);
    setSelectedId(null);
  }, []);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={FileText}
        title={t(($) => $.title)}
        count={stats?.draft}
        description={t(($) => $.subtitle)}
      />

      <div className="flex shrink-0 items-center gap-1 border-b px-4 py-2">
        {STATES.map((state) => {
          const count =
            state === "draft"
              ? stats?.draft
              : state === "approved"
                ? stats?.approved
                : stats?.discarded;
          const isActive = filterState === state;
          return (
            <button
              key={state}
              type="button"
              onClick={() => handleFilter(state)}
              className={cn(
                "rounded-md px-2.5 py-1 text-caption transition-colors",
                isActive
                  ? "bg-accent font-medium text-foreground"
                  : "text-muted-foreground hover:bg-accent/60",
              )}
            >
              {t(($) => $.filter[state])}
              {typeof count === "number" && count > 0 ? (
                <span className="ml-1 font-mono text-micro tabular-nums">{count}</span>
              ) : null}
            </button>
          );
        })}
      </div>

      <div className="flex min-h-0 flex-1">
        <PostmortemList
          items={items}
          state={filterState}
          isLoading={itemsQuery.isLoading}
          isError={itemsQuery.isError}
          selectedId={selectedId}
          onSelect={setSelectedId}
          hasMore={itemsQuery.hasNextPage}
          isLoadingMore={itemsQuery.isFetchingNextPage}
          onLoadMore={() => void itemsQuery.fetchNextPage()}
        />
        <PostmortemDetail
          item={selected}
          wsId={wsId}
          onResolved={() => setSelectedId(null)}
        />
      </div>
    </div>
  );
}

function PostmortemList({
  items,
  state,
  isLoading,
  isError,
  selectedId,
  onSelect,
  hasMore,
  isLoadingMore,
  onLoadMore,
}: {
  items: Postmortem[];
  state: PostmortemState;
  isLoading: boolean;
  isError: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
  hasMore: boolean;
  isLoadingMore: boolean;
  onLoadMore: () => void;
}) {
  const { t } = useT("postmortem");
  const timeAgo = useTimeAgo();

  if (isLoading) {
    return (
      <div className="flex w-full flex-col gap-2 p-4" aria-busy="true">
        <span className="sr-only">{t(($) => $.list.loading)}</span>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-14 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={FileText}
          title={t(($) => $.list.load_error)}
          tone="destructive"
          role="alert"
        />
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={FileText}
          title={t(($) =>
            state === "draft"
              ? $.list.empty_title_draft
              : state === "approved"
                ? $.list.empty_title_approved
                : $.list.empty_title_discarded,
          )}
          description={t(($) =>
            state === "draft"
              ? $.list.empty_description_draft
              : state === "approved"
                ? $.list.empty_description_approved
                : $.list.empty_description_discarded,
          )}
        />
      </div>
    );
  }

  return (
    <ul className="flex w-full min-w-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
      {items.map((item) => {
        const isActive = item.id === selectedId;
        return (
          <li key={item.id}>
            <div
              role="button"
              tabIndex={0}
              onClick={() => onSelect(item.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelect(item.id);
                }
              }}
              className={cn(
                "flex w-full cursor-pointer flex-col gap-0.5 rounded-lg border px-2 py-2 text-left transition-colors",
                isActive
                  ? "border-primary/40 bg-accent"
                  : "border-transparent hover:bg-accent/60",
              )}
            >
              <span className="line-clamp-2 text-body">{item.summary}</span>
              <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
                {item.failure_reason ? (
                  <Badge variant="secondary" className="font-mono text-micro">
                    {item.failure_reason}
                  </Badge>
                ) : null}
                <span className="shrink-0">{timeAgo(item.created_at)}</span>
              </span>
            </div>
          </li>
        );
      })}
      {hasMore ? (
        <li className="flex justify-center pt-1">
          <Button variant="ghost" size="sm" onClick={onLoadMore} disabled={isLoadingMore}>
            {isLoadingMore ? t(($) => $.list.loading) : t(($) => $.list.load_more)}
          </Button>
        </li>
      ) : null}
    </ul>
  );
}

function PostmortemDetail({
  item,
  wsId,
  onResolved,
}: {
  item: Postmortem | null;
  wsId: string;
  onResolved: () => void;
}) {
  const { t } = useT("postmortem");

  if (!item) {
    return (
      <aside className="hidden min-w-0 flex-1 border-l lg:flex lg:items-center lg:justify-center">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.select_prompt)}
        </p>
      </aside>
    );
  }

  return <PostmortemDetailBody key={item.id} item={item} wsId={wsId} onResolved={onResolved} />;
}

function PostmortemDetailBody({
  item,
  wsId,
  onResolved,
}: {
  item: Postmortem;
  wsId: string;
  onResolved: () => void;
}) {
  const { t } = useT("postmortem");
  const timeAgo = useTimeAgo();
  // Null-safe: the page can render in tests without a workspace route.
  const slug = useWorkspaceSlug();
  const approve = useApprovePostmortem(wsId);
  const discard = useDiscardPostmortem(wsId);

  const isDraft = item.state === "draft";
  const busy = approve.isPending || discard.isPending;

  const handleApprove = useCallback(async () => {
    try {
      const result = await approve.mutateAsync(item.id);
      const applied = result?.applied_rules ?? 0;
      toast.success(
        applied > 0
          ? t(($) => $.detail.rules_toast, { count: applied })
          : t(($) => $.detail.approved_toast),
      );
      onResolved();
    } catch (err) {
      handleResolveError(err, t);
    }
  }, [approve, item.id, onResolved, t]);

  const handleDiscard = useCallback(async () => {
    try {
      await discard.mutateAsync(item.id);
      toast.success(t(($) => $.detail.discarded_toast));
      onResolved();
    } catch (err) {
      handleResolveError(err, t);
    }
  }, [discard, item.id, onResolved, t]);

  return (
    <aside className="flex min-w-0 flex-1 flex-col border-l">
      <div className="flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  className="inline-flex shrink-0"
                  aria-label={
                    item.llm_generated
                      ? t(($) => $.detail.llm_generated)
                      : t(($) => $.detail.scaffold_generated)
                  }
                />
              }
            >
              {item.llm_generated ? (
                <Sparkles aria-hidden="true" className="size-4 text-muted-foreground" />
              ) : (
                <Wand2 aria-hidden="true" className="size-4 text-muted-foreground" />
              )}
            </TooltipTrigger>
            <TooltipContent>
              {item.llm_generated
                ? t(($) => $.detail.llm_generated)
                : t(($) => $.detail.scaffold_generated)}
            </TooltipContent>
          </Tooltip>
          <p className="truncate text-caption text-muted-foreground">
            {item.failure_reason || t(($) => $.detail.failure_reason_label)}
            {" · "}
            {timeAgo(item.created_at)}
            {typeof item.cost_usd_ticks === "number" && item.cost_usd_ticks > 0
              ? ` · ${formatCost(item.cost_usd_ticks)}`
              : ""}
          </p>
        </div>
        {isDraft ? (
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="outline" size="sm" onClick={handleDiscard} disabled={busy}>
              {discard.isPending ? (
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              ) : (
                <X aria-hidden="true" className="size-3.5" />
              )}
              {discard.isPending ? t(($) => $.detail.discarding) : t(($) => $.detail.discard)}
            </Button>
            <Button size="sm" onClick={handleApprove} disabled={busy}>
              {approve.isPending ? (
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              ) : (
                <Check aria-hidden="true" className="size-3.5" />
              )}
              {approve.isPending ? t(($) => $.detail.approving) : t(($) => $.detail.approve)}
            </Button>
          </div>
        ) : (
          <Badge variant="secondary" className="shrink-0">
            {item.state === "approved"
              ? t(($) => $.filter.approved)
              : t(($) => $.filter.discarded)}
          </Badge>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-4">
        {slug && (item.issue_id || item.agent_id) ? (
          <div className="flex flex-wrap items-center gap-2">
            {item.issue_id ? (
              <AppLink
                href={paths.workspace(slug).issueDetail(item.issue_id)}
                className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
              >
                <ExternalLink aria-hidden="true" className="size-3.5" />
                {t(($) => $.detail.open_issue)}
              </AppLink>
            ) : null}
            {item.agent_id ? (
              <AppLink
                href={paths.workspace(slug).agentDetail(item.agent_id)}
                className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
              >
                <Bot aria-hidden="true" className="size-3.5" />
                {t(($) => $.detail.open_agent)}
              </AppLink>
            ) : null}
          </div>
        ) : null}
        <Section label={t(($) => $.detail.summary_label)} body={item.summary} />
        <Section label={t(($) => $.detail.root_cause_label)} body={item.root_cause} />
        <Section label={t(($) => $.detail.impact_label)} body={item.impact} />

        <section className="flex flex-col gap-1.5">
          <h3 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.rules_label)}
          </h3>
          {item.preventive_rules.length > 0 ? (
            <>
              <ul className="flex list-disc flex-col gap-1 pl-5">
                {item.preventive_rules.map((rule, i) => (
                  <li key={i} className="text-body">
                    {rule}
                  </li>
                ))}
              </ul>
              {isDraft && item.agent_id ? (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.detail.rules_hint)}
                </p>
              ) : null}
            </>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.detail.no_rules)}
            </p>
          )}
        </section>
      </div>
    </aside>
  );
}

function Section({ label, body }: { label: string; body: string }) {
  if (!body) return null;
  return (
    <section className="flex flex-col gap-1.5">
      <h3 className="text-caption font-medium text-muted-foreground">{label}</h3>
      <p className="whitespace-pre-wrap text-body">{body}</p>
    </section>
  );
}

function handleResolveError(
  err: unknown,
  t: ReturnType<typeof useT<"postmortem">>["t"],
) {
  if (err instanceof ApiError && err.status === 409) {
    toast.info(t(($) => $.detail.conflict_toast));
    return;
  }
  toast.error(t(($) => $.detail.error_toast));
}
