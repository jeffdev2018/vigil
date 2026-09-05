"use client";

import { useQuery } from "@tanstack/react-query";
import { Command as CommandPrimitive } from "cmdk";
import { Lightbulb, MessageSquare, Terminal } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { isWhyQuery, stripHeadlineMarks, whySearchOptions, type WhySearchResult } from "@multica/core/search/why";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { HighlightText } from "./highlight-text";

/**
 * Why search (K55): a question typed in the palette finds the comment, run
 * message or decision record that answers it. Shown only for multi-word or
 * "?" queries, so a single word keeps going to issues and projects. Each row
 * opens the issue the source belongs to.
 */
export function WhySearchGroup({ query, groupClassName, onNavigated }: { query: string; groupClassName: string; onNavigated: () => void }) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const active = isWhyQuery(query);
  const { data, isFetched } = useQuery(whySearchOptions(wsId, query));
  if (!active || !isFetched) return null;
  const results = data?.results ?? [];
  const sourceLabel = (r: WhySearchResult) => {
    switch (r.source_type) {
      case "comment":
        return t(($) => $.why.comment);
      case "task_message":
        return t(($) => $.why.task_message);
      case "decision_record":
        return t(($) => $.why.decision_record);
      default:
        return r.source_type;
    }
  };
  const Icon = ({ type }: { type: string }) => (type === "comment" ? <MessageSquare className="size-4" /> : type === "decision_record" ? <Lightbulb className="size-4" /> : <Terminal className="size-4" />);
  return (
    <CommandPrimitive.Group heading={t(($) => $.groups.why)} className={groupClassName}>
      {results.length === 0 ? (
        <div data-testid="why-empty" className="px-3 py-2 text-caption text-muted-foreground">{t(($) => $.why.empty)}</div>
      ) : (
        results.map((r) => (
          <CommandPrimitive.Item
            key={r.id}
            value={`why:${r.id}`}
            data-testid="why-result"
            onSelect={() => {
              if (r.issue_id) {
                push(paths.issueDetail(r.issue_id));
                onNavigated();
              }
            }}
            className="flex cursor-pointer items-start gap-2 rounded-md px-3 py-2 text-caption aria-selected:bg-accent"
          >
            <span className="mt-0.5 shrink-0 text-muted-foreground"><Icon type={r.source_type} /></span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="line-clamp-2"><HighlightText text={stripHeadlineMarks(r.snippet)} query={query} /></span>
              <span className="truncate text-muted-foreground">
                {sourceLabel(r)}
                {r.issue_identifier ? ` · ${r.issue_identifier} ${r.issue_title ?? ""}` : ""}
              </span>
            </span>
          </CommandPrimitive.Item>
        ))
      )}
    </CommandPrimitive.Group>
  );
}
