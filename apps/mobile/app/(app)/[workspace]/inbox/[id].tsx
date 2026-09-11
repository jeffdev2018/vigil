import { useMemo } from "react";
import { ActivityIndicator, Linking, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import {
  resolveBillingRecovery,
  type BillingRecoveryKind,
} from "@multica/core/billing/recovery";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ApprovalAskCard } from "@/components/approvals/approval-card";
import { inboxListOptions } from "@/data/queries/inbox";
import { useWorkspaceApprovals } from "@/data/queries/approvals";
import {
  appConfigOptions,
  workspaceSubscriptionSummaryOptions,
} from "@/data/queries/billing";
import { calendarEventOptions } from "@/data/queries/calendar";
import { useRespondCalendarEvent } from "@/data/mutations/calendar";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  getAutopilotQuotaBody,
  getInboxDisplayTitle,
} from "@/lib/inbox-display";
import { matchApprovalForInboxItem } from "@/lib/approvals-display";
import { formatEventTimeRange } from "@/lib/calendar-display";

// Native calendar (OS plan, chantier 19).
const CALENDAR_NOTICE_TYPES = new Set(["calendar_invitation", "calendar_reminder"]);

// Inbox item types with a decidable ask on the approvals feed (GET
// /api/approvals) — see matchApprovalForInboxItem for how each is matched.
const APPROVAL_NOTICE_TYPES = new Set([
  "decision_request",
  "decision_escalated",
  "transition_approval_requested",
  "goal_question",
]);

function BillingRecovery({
  recovery,
  billingUrl,
}: {
  recovery: BillingRecoveryKind;
  billingUrl: string | null;
}) {
  switch (recovery) {
    case "checking":
      return <ActivityIndicator />;
    case "billing_disabled":
      return (
        <Text className="text-sm leading-5 text-muted-foreground">
          Billing changes are unavailable for this workspace. Contact your
          workspace administrator for help.
        </Text>
      );
    case "contact_admin":
      return (
        <Text className="text-sm leading-5 text-muted-foreground">
          Ask a workspace owner or admin to review the billing options.
        </Text>
      );
    case "checkout":
    case "portal":
    case "billing":
    case "billing_unavailable":
      return billingUrl ? (
        <Button
          onPress={() => void Linking.openURL(billingUrl)}
          accessibilityLabel="Review billing options"
        >
          <Text>Review billing options</Text>
        </Button>
      ) : (
        <Text className="text-sm leading-5 text-muted-foreground">
          Open Multica on the web to review billing options.
        </Text>
      );
  }
}

export default function InboxNoticeDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data: items, isLoading } = useQuery(inboxListOptions(wsId));

  // Read the raw workspace-scoped cache: deduplication can replace a row,
  // but a sheet already opened for a specific notification must remain stable.
  const item = items?.find(
    (candidate) => candidate.id === id && candidate.workspace_id === wsId,
  );
  const isQuotaNotice = item?.type === "autopilot_quota_exceeded";
  const isApprovalNotice = !!item && APPROVAL_NOTICE_TYPES.has(item.type);
  const isCalendarNotice = !!item && CALENDAR_NOTICE_TYPES.has(item.type);
  const isCalendarInvitation = item?.type === "calendar_invitation";
  const eventId = isCalendarNotice ? (item?.details?.event_id ?? null) : null;
  const { data: calendarEvent } = useQuery(calendarEventOptions(wsId, eventId));
  const respondCalendarEvent = useRespondCalendarEvent();
  // Workspace-level feed: the matching ask may belong to any issue.
  const { data: approvalsFeed } = useWorkspaceApprovals(
    isApprovalNotice ? wsId : null,
  );
  const matchedApproval = useMemo(
    () =>
      item && isApprovalNotice
        ? matchApprovalForInboxItem(item, approvalsFeed?.approvals ?? [])
        : null,
    [item, isApprovalNotice, approvalsFeed],
  );
  const configQuery = useQuery({
    ...appConfigOptions(),
    enabled: isQuotaNotice,
  });
  const billingEnabled =
    configQuery.data?.feature_flags?.[
      BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG
    ] === true;
  const summaryQuery = useQuery(
    workspaceSubscriptionSummaryOptions(wsId, isQuotaNotice && billingEnabled),
  );
  const checkingConfig =
    isQuotaNotice && !configQuery.data && configQuery.isFetching;
  const recovery = resolveBillingRecovery({
    actions: summaryQuery.data?.availableActions,
    billingEnabled: checkingConfig || billingEnabled,
    loading:
      checkingConfig ||
      (billingEnabled &&
        !summaryQuery.data?.availableActions &&
        summaryQuery.isFetching),
  });
  const webUrl = process.env.EXPO_PUBLIC_WEB_URL?.replace(/\/+$/, "");
  const billingUrl =
    webUrl && wsSlug ? `${webUrl}/${wsSlug}/settings?tab=billing` : null;
  const body = item ? getAutopilotQuotaBody(item) : null;

  // One ScrollView with the header pinned as its first child: as a sibling
  // above a ScrollView the formSheet drew the body over the title (JEF-398).
  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="pb-8"
      stickyHeaderIndices={[0]}
      showsVerticalScrollIndicator={false}
    >
      <View className="flex-row items-center border-b border-border bg-background px-4 py-3">
        <Text className="flex-1 text-lg font-semibold text-foreground">
          {item ? getInboxDisplayTitle(item) : "Notification"}
        </Text>
        <IconButton
          name="close"
          variant="secondary"
          className="size-7 rounded-full"
          onPress={() => router.back()}
          accessibilityLabel="Close notification"
        />
      </View>

      {isLoading ? (
        <View className="items-center justify-center py-16">
          <ActivityIndicator />
        </View>
      ) : !item ||
        (item.type !== "autopilot_quota_exceeded" &&
          item.type !== "autopilot_paused" &&
          !isApprovalNotice &&
          !isCalendarNotice) ? (
        <View className="px-4 py-8">
          <Text className="text-sm text-muted-foreground text-center">
            This notification is no longer available.
          </Text>
        </View>
      ) : isApprovalNotice ? (
        <View className="gap-4 px-4 py-5">
          {wsId && matchedApproval ? (
            <ApprovalAskCard approval={matchedApproval} wsId={wsId} />
          ) : (
            <Text className="text-sm text-muted-foreground">
              Already decided
            </Text>
          )}
          {item.body ? (
            <Text className="text-base leading-6 text-foreground">
              {item.body}
            </Text>
          ) : null}
        </View>
      ) : isCalendarNotice ? (
        <View className="gap-4 px-4 py-5">
          <Text className="text-base leading-6 text-foreground">
            {calendarEvent ? formatEventTimeRange(calendarEvent) : item.body}
          </Text>
          {calendarEvent?.location ? (
            <Text className="text-sm text-muted-foreground">
              {calendarEvent.location}
            </Text>
          ) : null}

          {isCalendarInvitation ? (
            <View className="flex-row gap-2">
              <Button
                className="flex-1"
                onPress={() =>
                  eventId &&
                  respondCalendarEvent.mutate({ id: eventId, response: "accepted" })
                }
                disabled={!eventId || respondCalendarEvent.isPending}
              >
                <Text>Accept</Text>
              </Button>
              <Button
                className="flex-1"
                variant="outline"
                onPress={() =>
                  eventId &&
                  respondCalendarEvent.mutate({ id: eventId, response: "declined" })
                }
                disabled={!eventId || respondCalendarEvent.isPending}
              >
                <Text>Decline</Text>
              </Button>
            </View>
          ) : null}

          {eventId ? (
            <Button
              variant="outline"
              onPress={() =>
                wsSlug && router.push(`/${wsSlug}/calendar-event/${eventId}`)
              }
            >
              <Text>View event</Text>
            </Button>
          ) : null}
        </View>
      ) : (
        <View className="gap-5 px-4 py-5">
          {body ? (
            <Text className="text-base leading-6 text-foreground">
              {body}
            </Text>
          ) : null}

          {isQuotaNotice ? (
            <BillingRecovery recovery={recovery} billingUrl={billingUrl} />
          ) : null}
        </View>
      )}
    </ScrollView>
  );
}
