"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { ownershipSuggestionOptions } from "@multica/core/issues/ownership";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Module ownership (K33): the owner and referent agent the matching rule
 * suggests for this issue. Nothing is applied until a click; the section
 * hides when no rule matches or the issue is already the owner's.
 */
export function OwnershipSuggestionSection({ issue }: { issue: Pick<Issue, "id" | "assignee_type" | "assignee_id"> }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: suggestion } = useQuery(ownershipSuggestionOptions(wsId, issue.id));
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!suggestion });
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!suggestion?.referent_agent_id });
  const update = useUpdateIssue();
  if (!suggestion) return null;
  const ownerIsAssigned = issue.assignee_type === "member" && issue.assignee_id === suggestion.owner_user_id;
  const agentIsAssigned = issue.assignee_type === "agent" && issue.assignee_id === suggestion.referent_agent_id;
  if (ownerIsAssigned || agentIsAssigned) return null;

  const owner = members.find((m) => m.user_id === suggestion.owner_user_id);
  const agent = agents.find((a) => a.id === suggestion.referent_agent_id);
  const assign = (assignee_type: "member" | "agent", assignee_id: string) =>
    update.mutate(
      { id: issue.id, assignee_type, assignee_id },
      { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.ownership.assign_failed)) },
    );
  const matched = suggestion.matched.startsWith("path:") ? suggestion.matched.slice(5) : t(($) => $.ownership.matched_label);

  return (
    <div data-testid="ownership-suggestion" className="flex flex-col gap-1.5 rounded-md border p-2 text-caption">
      <div className="font-medium">{t(($) => $.ownership.section)}</div>
      <div className="text-muted-foreground" title={suggestion.pattern}>
        {t(($) => $.ownership.matched, { what: matched })}
      </div>
      <div className="flex flex-wrap gap-1">
        <Button type="button" size="sm" disabled={update.isPending} onClick={() => assign("member", suggestion.owner_user_id)}>
          {t(($) => $.ownership.assign_owner, { name: owner?.name || owner?.email || t(($) => $.ownership.member) })}
        </Button>
        {suggestion.referent_agent_id && (
          <Button type="button" size="sm" variant="outline" disabled={update.isPending} onClick={() => assign("agent", suggestion.referent_agent_id!)}>
            {t(($) => $.ownership.assign_agent, { name: agent?.name ?? t(($) => $.ownership.agent) })}
          </Button>
        )}
      </div>
    </div>
  );
}
