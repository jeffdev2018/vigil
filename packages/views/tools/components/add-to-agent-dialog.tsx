"use client";

import { useMemo, useState } from "react";
import { Loader2, Search } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent, WorkspaceMcpServer } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useAttachMcpServerToAgent } from "@multica/core/workspace/mutations";
import { foldText } from "@multica/core/workspace/tool-catalog";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogClose,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";

/**
 * Gives one workspace MCP server to an agent, picked from the whole workspace.
 *
 * The dialog deliberately stays open after a success: the reason to be here is
 * "three agents need this server", and closing after the first would make the
 * other two a fresh trip through the catalogue. The attached agent is marked
 * from the refreshed library listing, not from local bookkeeping, so the state
 * shown is the state the server confirmed.
 *
 * The fine-grained decision — which of the server's tools the agent may
 * actually call — stays on the agent's own MCP tab. This is the shortcut, not
 * a second editor.
 */
export function AddToAgentDialog({
  wsId,
  server,
  onClose,
}: {
  wsId: string;
  server: WorkspaceMcpServer;
  onClose: () => void;
}) {
  const { t } = useT("tools");
  const [query, setQuery] = useState("");
  const agentsQuery = useQuery(agentListOptions(wsId));
  const attach = useAttachMcpServerToAgent(wsId);

  // The count comes from the library listing (one grouped query server-side),
  // so no per-agent request is needed to know who already has this server.
  const attachedIds = useMemo(
    () => new Set(server.agent_ids ?? []),
    [server.agent_ids],
  );

  const agents = useMemo(() => {
    const needle = foldText(query.trim());
    return (agentsQuery.data ?? [])
      .filter((agent) => !agent.archived_at)
      .filter((agent) => needle === "" || foldText(agent.name).includes(needle));
  }, [agentsQuery.data, query]);

  const add = (agent: Agent) => {
    attach.mutate(
      { agentId: agent.id, serverId: server.id },
      {
        onSuccess: () =>
          toast.success(
            t(($) => $.dialog.success_toast, {
              server: server.name,
              agent: agent.name,
            }),
          ),
        onError: () =>
          toast.error(
            t(($) => $.dialog.error_toast, {
              server: server.name,
              agent: agent.name,
            }),
          ),
      },
    );
  };

  const pendingAgentId = attach.isPending ? attach.variables?.agentId : null;

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[80vh] flex-col sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.dialog.title, { server: server.name })}
          </DialogTitle>
          <DialogDescription>{t(($) => $.dialog.description)}</DialogDescription>
        </DialogHeader>

        <div className="relative">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            className="h-8 pl-8"
            aria-label={t(($) => $.dialog.search_label)}
            placeholder={t(($) => $.dialog.search_placeholder)}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {agentsQuery.isLoading ? (
            <div className="space-y-2 py-1" role="status" aria-label={t(($) => $.dialog.loading)}>
              {[0, 1, 2].map((row) => (
                <Skeleton key={row} className="h-9 w-full" />
              ))}
            </div>
          ) : agentsQuery.isError ? (
            <div className="flex flex-col items-start gap-2 py-3">
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.dialog.load_error)}
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void agentsQuery.refetch()}
              >
                {t(($) => $.dialog.retry)}
              </Button>
            </div>
          ) : agents.length === 0 ? (
            <p className="py-6 text-center text-caption text-muted-foreground">
              {query.trim() === ""
                ? t(($) => $.dialog.empty)
                : t(($) => $.dialog.empty_results)}
            </p>
          ) : (
            <ul className="divide-y divide-surface-border">
              {agents.map((agent) => {
                const attached = attachedIds.has(agent.id);
                return (
                  <li
                    key={agent.id}
                    className="flex items-center gap-3 px-1 py-2"
                    data-attached={attached ? "true" : undefined}
                  >
                    <span
                      className={
                        attached
                          ? "min-w-0 flex-1 truncate text-body text-muted-foreground"
                          : "min-w-0 flex-1 truncate text-body"
                      }
                    >
                      {agent.name}
                    </span>
                    {attached ? (
                      <span className="shrink-0 text-caption text-muted-foreground">
                        {t(($) => $.dialog.already_added)}
                      </span>
                    ) : (
                      <Button
                        size="sm"
                        variant="outline"
                        className="shrink-0"
                        disabled={attach.isPending}
                        aria-label={t(($) => $.dialog.add_aria, {
                          server: server.name,
                          agent: agent.name,
                        })}
                        onClick={() => add(agent)}
                      >
                        {pendingAgentId === agent.id ? (
                          <Loader2 className="animate-spin" aria-hidden="true" />
                        ) : null}
                        {t(($) => $.dialog.add)}
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <DialogFooter>
          <DialogClose render={<Button variant="outline" />}>
            {t(($) => $.dialog.close)}
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
