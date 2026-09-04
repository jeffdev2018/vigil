"use client";

import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { auditLogInfiniteOptions } from "@multica/core/workspace/audit";
import type { AuditLogFilter } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Audit log (K08): the workspace's actions, newest first, filterable by
 * actor type and action, exportable as CSV or JSON with the same filter.
 * The export streams from the server and is saved by the browser.
 */
export function AuditLogTab() {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const [actorType, setActorType] = useState("");
  const [action, setAction] = useState("");
  const [exporting, setExporting] = useState<"csv" | "json" | null>(null);
  const filter: AuditLogFilter = { actor_type: actorType || undefined, action: action.trim() || undefined };
  const { data, isLoading, isError, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery(auditLogInfiniteOptions(wsId, filter));
  const entries = data?.pages.flatMap((p) => p.entries) ?? [];

  async function exportAs(format: "csv" | "json") {
    setExporting(format);
    try {
      const text = await api.exportAuditLog(format, filter);
      const blob = new Blob([text], { type: format === "csv" ? "text/csv;charset=utf-8" : "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `audit-log.${format}`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.audit.export_failed));
    } finally {
      setExporting(null);
    }
  }

  return (
    <div data-testid="audit-log" className="flex flex-col gap-3 text-caption">
      <p className="text-muted-foreground">{t(($) => $.audit.description)}</p>
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label={t(($) => $.audit.actor_type)} className="h-8 rounded-md border bg-background px-2" value={actorType} onChange={(e) => setActorType(e.target.value)}>
          <option value="">{t(($) => $.audit.any_actor)}</option>
          <option value="member">{t(($) => $.audit.actor_member)}</option>
          <option value="agent">{t(($) => $.audit.actor_agent)}</option>
          <option value="system">{t(($) => $.audit.actor_system)}</option>
        </select>
        <Input aria-label={t(($) => $.audit.action)} placeholder={t(($) => $.audit.action_placeholder)} className="h-8 w-56 font-mono" value={action} onChange={(e) => setAction(e.target.value)} />
        <span className="flex-1" />
        <Button type="button" size="sm" variant="outline" disabled={exporting !== null} onClick={() => void exportAs("csv")}>
          {t(($) => $.audit.export_csv)}
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={exporting !== null} onClick={() => void exportAs("json")}>
          {t(($) => $.audit.export_json)}
        </Button>
      </div>
      {isLoading ? (
        <p className="text-muted-foreground">{t(($) => $.audit.loading)}</p>
      ) : isError ? (
        <p className="text-destructive">{t(($) => $.audit.load_failed)}</p>
      ) : entries.length === 0 ? (
        <p data-testid="audit-empty" className="text-muted-foreground">{t(($) => $.audit.empty)}</p>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-normal">{t(($) => $.audit.col_when)}</th>
                <th className="px-3 py-2 font-normal">{t(($) => $.audit.col_actor)}</th>
                <th className="px-3 py-2 font-normal">{t(($) => $.audit.col_action)}</th>
                <th className="px-3 py-2 font-normal">{t(($) => $.audit.col_entity)}</th>
                <th className="px-3 py-2 font-normal">{t(($) => $.audit.col_details)}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id} data-testid="audit-row" className="border-t align-top">
                  <td className="whitespace-nowrap px-3 py-1.5 text-muted-foreground" title={e.occurred_at}>{timeAgo(e.occurred_at)}</td>
                  <td className="whitespace-nowrap px-3 py-1.5">
                    {e.actor_type}
                    {e.approver_id && <span className="text-muted-foreground"> · {t(($) => $.audit.approved)}</span>}
                  </td>
                  <td className="whitespace-nowrap px-3 py-1.5 font-mono">{e.action}</td>
                  <td className="whitespace-nowrap px-3 py-1.5 text-muted-foreground">
                    {e.entity_type}
                    {e.entity_id ? ` ${e.entity_id.slice(0, 8)}` : ""}
                  </td>
                  <td className="max-w-md truncate px-3 py-1.5 font-mono text-muted-foreground" title={JSON.stringify(e.details)}>
                    {JSON.stringify(e.details)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {hasNextPage && (
        <Button type="button" size="sm" variant="ghost" className="self-start" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
          {t(($) => $.audit.load_more)}
        </Button>
      )}
    </div>
  );
}
