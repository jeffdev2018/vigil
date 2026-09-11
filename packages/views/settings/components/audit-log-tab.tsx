"use client";

import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { auditLogInfiniteOptions } from "@multica/core/workspace/audit";
import type { AuditChainStatus, AuditLogFilter } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsTab } from "./settings-layout";

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
  const [chain, setChain] = useState<AuditChainStatus | null>(null);
  const [verifying, setVerifying] = useState(false);
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

  async function verify() {
    setVerifying(true);
    try {
      setChain(await api.verifyAuditLog());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.audit.verify_failed));
    } finally {
      setVerifying(false);
    }
  }

  return (
    <SettingsTab title={t(($) => $.audit.title)} description={t(($) => $.audit.description)}>
    <div data-testid="audit-log" className="flex flex-col gap-3 text-caption">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" disabled={verifying} onClick={() => void verify()}>
          {t(($) => $.audit.verify)}
        </Button>
        {chain && (
          <span data-testid="audit-chain" data-ok={chain.ok ? "true" : "false"} className={chain.ok ? "text-muted-foreground" : "text-destructive"}>
            {chain.ok
              ? t(($) => $.audit.chain_ok, { count: chain.total, hash: chain.head_hash.slice(0, 12) })
              : t(($) => $.audit.chain_broken, { seq: chain.broken_seq ?? 0 })}
          </span>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Select
          items={[
            { value: "", label: t(($) => $.audit.any_actor) },
            { value: "member", label: t(($) => $.audit.actor_member) },
            { value: "agent", label: t(($) => $.audit.actor_agent) },
            { value: "system", label: t(($) => $.audit.actor_system) },
          ]}
          value={actorType}
          onValueChange={(value) => setActorType(value ?? "")}
        >
          <SelectTrigger aria-label={t(($) => $.audit.actor_type)} size="sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="">{t(($) => $.audit.any_actor)}</SelectItem>
            <SelectItem value="member">{t(($) => $.audit.actor_member)}</SelectItem>
            <SelectItem value="agent">{t(($) => $.audit.actor_agent)}</SelectItem>
            <SelectItem value="system">{t(($) => $.audit.actor_system)}</SelectItem>
          </SelectContent>
        </Select>
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
                  <td className="max-w-md px-3 py-1.5 text-muted-foreground">
                    <AuditDetails details={e.details} />
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
    </SettingsTab>
  );
}

/** One audit value on a single line: scalars verbatim, nested data as JSON. */
function detailValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

/**
 * Details as `key: value` pairs. Collapsed, the row shows the pairs on one
 * truncated line; open, each pair gets its own wrapping line so nothing is
 * cut off and nothing widens the table.
 */
function AuditDetails({ details }: { details: Record<string, unknown> }) {
  const pairs = Object.entries(details ?? {});
  if (pairs.length === 0) return <span>—</span>;
  const inline = pairs.map(([k, v]) => `${k}: ${detailValue(v)}`).join(" · ");
  return (
    <details className="min-w-0">
      <summary className="cursor-pointer list-none truncate" title={inline}>
        {inline}
      </summary>
      <dl className="mt-1 flex flex-col gap-0.5">
        {pairs.map(([k, v]) => (
          <div key={k} className="flex gap-1.5 break-all">
            <dt className="shrink-0 font-mono">{k}</dt>
            <dd className="min-w-0">{detailValue(v)}</dd>
          </div>
        ))}
      </dl>
    </details>
  );
}
