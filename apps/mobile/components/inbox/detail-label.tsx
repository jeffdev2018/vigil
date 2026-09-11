/**
 * Mobile InboxDetailLabel — type-aware second-line for inbox rows.
 *
 * Mirrors packages/views/inbox/components/inbox-detail-label.tsx exactly:
 * for each InboxItemType the user sees the same label they would see on
 * web/desktop. This is a Behavioral parity concern — if web shows "Set
 * status to ✓ Done", mobile must show "Set status to ✓ Done" (rendered
 * with mobile primitives, not the literal HTML).
 *
 * Web is i18n-driven (useT). Mobile v1 is English-only; when mobile ships
 * i18n, mirror the namespace structure.
 */
import { View } from "react-native";
import type {
  InboxItem,
  InboxItemType,
  IssuePriority,
} from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { cn } from "@/lib/utils";

// Mirrors PRIORITY_CONFIG.label in packages/core/issues/config/priority.ts
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

// Mirrors useTypeLabels in packages/views/inbox/components/inbox-detail-label.tsx
const TYPE_LABEL: Record<InboxItemType, string> = {
  // Workspace doctrine (OS plan, chantier 22).
  doctrine_review: "Doctrine review",
  doctrine_report: "Doctrine report",
  // Native calendar (OS plan, chantier 19).
  calendar_invitation: "Invitation",
  calendar_reminder: "Reminder",
  issue_assigned: "Assigned",
  issue_subscribed: "Subscribed",
  unassigned: "Unassigned",
  assignee_changed: "Reassigned",
  delegate_assigned: "Delegated",
  status_changed: "Status changed",
  priority_changed: "Priority changed",
  start_date_changed: "Start date changed",
  due_date_changed: "Due date changed",
  new_comment: "New comment",
  mentioned: "Mentioned",
  review_requested: "Review requested",
  task_completed: "Task completed",
  task_failed: "Task failed",
  agent_blocked: "Agent blocked",
  agent_completed: "Agent completed",
  reaction_added: "Reaction added",
  quick_create_done: "Quick-create done",
  quick_create_failed: "Quick-create failed",
  quick_create_unconfirmed: "Quick-create needs a check",
  autopilot_paused: "Autopilot paused",
  autopilot_quota_exceeded: "Autopilot run limit reached",
  decision_request: "Decision requested",
  decision_escalated: "Decision escalated",
  ownership_suggested: "Owner suggested",
  morning_briefing: "Morning briefing",
  standup_question: "Standup question",
  weekly_retro: "Weekly retro",
  trust_promotion_suggested: "Trust promotion suggested",
  budget_warning: "Budget warning",
  budget_exceeded: "Budget exceeded",
  postmortem_ready: "Postmortem ready",
  triage_stale: "Triage is stalling",
  transition_approval_requested: "Approval needed",
  goal_question: "Question from the agent",
  critic_degraded: "Adversarial review skipped",
  critic_budget: "Adversarial review stopped",
  cycle_rollover_orphaned: "Work left a cycle with nowhere to go",
  watchdog_escalation: "Watchdog escalation",
  code_health_report: "Code health report",
  doc_drift_report: "Agent context drift",
  contest_ready: "Contest ready for your verdict",
  org_alert: "Organisation alert",
  mcp_alert: "MCP gateway alert",
  model_key_alert: "Model key retired",
  routing_alert: "Agent cannot be routed",
  residency_policy_blocked: "Blocked by data residency",
  linear_alert: "Linear is disconnected",
  decision_auto_decided: "Decided for you",
  confidence_review: "Delivery flagged for review",
  run_limit_warn: "Run nearing its limit",
  run_limit_exceeded: "Run over its limit",
  run_limit_stopped: "Run stopped: limit reached",
};

// due_date is a calendar day — format timezone-safely (no offset day shift).
function shortDate(dateStr: string): string {
  return formatDateOnly(dateStr, { month: "short", day: "numeric" }, "en-US");
}

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

export function InboxDetailLabel({
  item,
  className,
}: {
  item: InboxItem;
  className?: string;
}) {
  const { getName } = useActorLookup();
  // `details.to` is a status KEY and may be a custom one, so its name, colour
  // and glyph all resolve through the workspace catalog. (MUL-6243)
  const { categoryOf, colorOf, labelOf } = useIssueStatuses();
  const details = item.details ?? {};
  const type = item.type;

  // Cases with inline icons → Row layout.
  if (type === "status_changed" && details.to) {
    const status = details.to;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">Set status to</Text>
        <StatusIcon
          status={status}
          category={categoryOf(status)}
          color={colorOf(status)}
          size={12}
        />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {labelOf(status)}
        </Text>
      </View>
    );
  }

  if (type === "priority_changed" && details.to) {
    const priority = details.to as IssuePriority;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">Set priority to</Text>
        <PriorityIcon priority={priority} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {PRIORITY_LABEL[priority] ?? priority}
        </Text>
      </View>
    );
  }

  // Single-string cases.
  const text = (() => {
    switch (type) {
      case "issue_assigned":
      case "assignee_changed":
        if (details.new_assignee_id) {
          const name = getName(
            (details.new_assignee_type ?? "member") as "member" | "agent",
            details.new_assignee_id,
          );
          return `Assigned to ${name}`;
        }
        return TYPE_LABEL[type];
      case "delegate_assigned":
        if (details.new_delegate_id) {
          const name = getName(
            (details.new_delegate_type ?? "member") as "member" | "agent",
            details.new_delegate_id,
          );
          return `Delegated to ${name}`;
        }
        return TYPE_LABEL[type];
      case "unassigned":
        return "Removed assignee";
      case "due_date_changed":
        return details.to
          ? `Set due date to ${shortDate(details.to)}`
          : "Removed due date";
      case "new_comment":
        return singleLine(item.body) || TYPE_LABEL[type];
      case "reaction_added":
        return details.emoji
          ? `Reacted with ${details.emoji}`
          : TYPE_LABEL[type];
      case "quick_create_done":
        return details.identifier
          ? `Created with agent: ${details.identifier}`
          : TYPE_LABEL[type];
      case "quick_create_failed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail ? `Failed: ${detail}` : TYPE_LABEL[type];
      }
      // Mirrors packages/views/inbox/components/inbox-detail-label.tsx: the
      // unconfirmed outcome deliberately drops the "Failed:" prefix, because
      // the issue may actually have been created.
      case "quick_create_unconfirmed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail || TYPE_LABEL[type];
      }
      case "autopilot_quota_exceeded":
        return "Run blocked because the limit was reached";
      // Native calendar (OS plan, chantier 19): the server already renders
      // a ready-to-read body ("Mon 2 Jan 2006 15:04 – 15:04 (tz) · location"
      // for an invitation, "Starts in N min · location" for a reminder —
      // see notifyCalendarInvitations / RemindCalendarEvents in
      // server/internal/handler/calendar_events.go), so this mirrors the
      // new_comment case: show the body, fall back to the type label.
      case "calendar_invitation":
      case "calendar_reminder":
        return singleLine(item.body) || TYPE_LABEL[type];
      // Workspace doctrine (OS plan, chantier 22): same reasoning — the
      // server writes a ready-to-read body ("A new doctrine revision is
      // waiting for your review.", "…now live as revision N.", or the
      // report summary — notifyDoctrineReview / notifyDoctrineReport in
      // server/internal/handler/workspace_doctrine.go).
      case "doctrine_review":
      case "doctrine_report":
        return singleLine(item.body) || TYPE_LABEL[type];
      default:
        return TYPE_LABEL[type] ?? type;
    }
  })();

  return (
    <Text
      className={cn("text-xs text-muted-foreground", className)}
      numberOfLines={1}
    >
      {text}
    </Text>
  );
}
