"use client";

import { useMemo, useState } from "react";
import { Lock, UserMinus } from "lucide-react";
import type { Agent, IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions, assigneeFrequencyOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../../common/actor-avatar";
import { DeferredPopup } from "../../../common/deferred-popup";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "./property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";

/**
 * Legacy boolean shape kept around for callers (e.g. `use-issue-actions.ts`)
 * that haven't migrated to the new `canAssignAgentToIssue` Decision API yet.
 * Internally redirects to the canonical rule so behaviour stays in sync.
 */
export function canAssignAgent(
  agent: Agent,
  userId: string | undefined,
  memberRole: string | undefined,
): boolean {
  return canAssignAgentToIssue(agent, {
    userId: userId ?? null,
    role: memberRole === "owner" || memberRole === "admin" || memberRole === "member"
      ? memberRole
      : null,
  }).allowed;
}

/**
 * Which of an issue's two actor pairs this picker edits (F01). The rows, the
 * search, the avatars and the frequency ordering are identical; only the
 * emitted field names, the label sub-tree, and whether squads are offered
 * differ — so this is one component with a discriminator rather than a second
 * 300-line copy that drifts.
 */
export type ActorPickerKind = "assignee" | "delegate";

interface AssigneePickerProps {
  /** Defaults to "assignee" so every existing call site is unchanged. */
  kind?: ActorPickerKind;
  /**
   * The issue's current actor for `kind` — assignee_type/assignee_id, or
   * delegate_type/delegate_id in delegate mode. The prop names kept the
   * assignee wording rather than churn 14 call sites for a rename.
   */
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  /**
   * `true` when a batch selection spans different assignees ("mixed"): no row
   * is checked, including the unassigned row. Distinct from `assigneeType` /
   * `assigneeId` both being `null`, which means every selected issue is
   * genuinely unassigned and the unassigned row should be checked.
   */
  mixed?: boolean;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}

/**
 * Mounting the real picker subscribes to members/agents/squads/frequency
 * queries — multiplied per board card / list row that cost froze tab
 * switches. Uncontrolled callers that bring their own trigger content get a
 * deferred lookalike trigger instead; the picker mounts on first interaction.
 * The default trigger needs `getActorName` (a members/agents subscription
 * itself), so trigger-less callers stay eager.
 */
export function AssigneePicker(props: AssigneePickerProps) {
  const hasDeferredTriggerContent =
    props.trigger !== undefined || props.triggerRender?.props.children != null;
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    hasDeferredTriggerContent;
  if (!canDefer) {
    return <AssigneePickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger}
      triggerRender={props.triggerRender}
      triggerClassName={PICKER_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <AssigneePickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function AssigneePickerImpl({
  kind = "assignee",
  assigneeType,
  assigneeId,
  mixed = false,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: AssigneePickerProps) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  // A squad is a routing object whose work runs through its leader, not a
  // person to partner with — the server rejects delegate_type='squad' with a
  // 400, so the rows are not offered here either.
  const showSquads = kind === "assignee";
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: frequency = [] } = useQuery(assigneeFrequencyOptions(wsId));
  const { getActorName } = useActorName();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  // Build a lookup map from frequency data for sorting.
  const freqMap = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of frequency) {
      map.set(`${entry.assignee_type}:${entry.assignee_id}`, entry.frequency);
    }
    return map;
  }, [frequency]);

  const getFreq = (type: string, id: string) => freqMap.get(`${type}:${id}`) ?? 0;

  const query = filter.trim().toLowerCase();
  const filteredMembers = members
    .filter((m) => m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query))
    .sort((a, b) => getFreq("member", b.user_id) - getFreq("member", a.user_id));
  const filteredAgents = agents
    .filter((a) => !a.archived_at && (a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)))
    .sort((a, b) => getFreq("agent", b.id) - getFreq("agent", a.id));
  const filteredSquads = squads
    .filter((s) => !s.archived_at && (s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)))
    .sort((a, b) => getFreq("squad", b.id) - getFreq("squad", a.id));
  const runnableAgentIds = new Set(
    agents
      .filter((agent) => !agent.archived_at && isAgentRuntimeBound(agent))
      .map((agent) => agent.id),
  );

  const isSelected = (type: string, id: string) =>
    assigneeType === type && assigneeId === id;

  // The two sub-trees have the same shape; resolving them once keeps the JSX
  // below free of a ternary per label.
  const labels =
    kind === "delegate"
      ? {
          none: t(($) => $.pickers.delegate.trigger_none),
          search: t(($) => $.pickers.delegate.search_placeholder),
          members: t(($) => $.pickers.delegate.members_group),
          agents: t(($) => $.pickers.delegate.agents_group),
        }
      : {
          none: t(($) => $.pickers.assignee.trigger_unassigned),
          search: t(($) => $.pickers.assignee.search_placeholder),
          members: t(($) => $.pickers.assignee.members_group),
          agents: t(($) => $.pickers.assignee.agents_group),
        };

  /** The update payload for whichever pair this picker owns. */
  const patch = (
    type: IssueAssigneeType | null,
    id: string | null,
  ): Partial<UpdateIssueRequest> =>
    kind === "delegate"
      ? { delegate_type: type as "member" | "agent" | null, delegate_id: id }
      : { assignee_type: type, assignee_id: id };

  const triggerLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : labels.none;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-64"
      align={align}
      searchable
      searchPlaceholder={labels.search}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ? customTrigger : assigneeType && assigneeId ? (
          <>
            <ActorAvatar actorType={assigneeType} actorId={assigneeId} size="sm" enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{labels.none}</span>
        )
      }
    >
      {/* Unassigned — always the first row, search active or not. Every
          picker in the app puts the empty value there, so "clear this field"
          never moves. */}
      <PickerItem
        emptyValue
        selected={!mixed && !assigneeType && !assigneeId}
        onClick={() => {
          onUpdate(patch(null, null));
          setOpen(false);
        }}
      >
        <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">{labels.none}</span>
      </PickerItem>

      {/* Members */}
      {filteredMembers.length > 0 && (
        <PickerSection label={labels.members}>
          {filteredMembers.map((m) => (
            <PickerItem
              key={m.user_id}
              selected={isSelected("member", m.user_id)}
              onClick={() => {
                onUpdate(patch("member", m.user_id));
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Agents */}
      {filteredAgents.length > 0 && (
        <PickerSection label={labels.agents}>
          {filteredAgents.map((a) => {
            const decision = canAssignAgentToIssue(a, {
              userId: user?.id ?? null,
              role:
                memberRole === "owner" ||
                memberRole === "admin" ||
                memberRole === "member"
                  ? memberRole
                  : null,
            });
            // A delegate starts no run, so an agent with no runtime bound is
            // a perfectly valid partner and the server accepts it. Only the
            // permission decision gates delegate mode; requiring a runtime
            // would refuse a write the API allows.
            const runtimeBound = isAgentRuntimeBound(a);
            const allowed = decision.allowed && (kind === "delegate" || runtimeBound);
            return (
              <PickerItem
                key={a.id}
                selected={isSelected("agent", a.id)}
                disabled={!allowed}
                tooltip={
                  !decision.allowed
                    ? decision.message
                    : !allowed
                      ? t(($) => $.pickers.assignee.agent_runtime_required)
                      : undefined
                }
                onClick={() => {
                  if (!allowed) return;
                  onUpdate(patch("agent", a.id));
                  setOpen(false);
                }}
              >
                <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
                <span className={`truncate ${allowed ? "" : "text-muted-foreground"}`}>{a.name}</span>
                {a.visibility === "private" && (
                  <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
                )}
              </PickerItem>
            );
          })}
        </PickerSection>
      )}

      {/* Squads — group ownership; assigning to a squad routes the issue to
          its leader agent on the backend. */}
      {showSquads && filteredSquads.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.squads_group)}>
          {filteredSquads.map((s) => {
            const runtimeBound = runnableAgentIds.has(s.leader_id);
            return (
              <PickerItem
                key={s.id}
                selected={isSelected("squad", s.id)}
                disabled={!runtimeBound}
                tooltip={
                  runtimeBound
                    ? undefined
                    : t(($) => $.pickers.assignee.squad_runtime_required)
                }
                onClick={() => {
                  if (!runtimeBound) return;
                  onUpdate(patch("squad", s.id));
                  setOpen(false);
                }}
              >
                <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                <span className="truncate">{s.name}</span>
              </PickerItem>
            );
          })}
        </PickerSection>
      )}

      {filteredMembers.length === 0 &&
        filteredAgents.length === 0 &&
        (!showSquads || filteredSquads.length === 0) &&
        filter && <PickerEmpty />}
    </PropertyPicker>
  );
}
