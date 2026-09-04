"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AudioLines, ChevronDown, ChevronRight, ExternalLink } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { meetingDetailOptions } from "@multica/core/meetings/queries";
import { useMeetingRecorderStore } from "@multica/core/meetings/store";
import type { Meeting, MeetingAction } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { CollectionPageState } from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { RichContent } from "../../rich-content";
import { useT, useTimeAgo } from "../../i18n";
import { MeetingRecorderPanel } from "./meeting-recorder";
import { meetingStatusDotClass } from "./meetings-page";

export function MeetingDetailPage({ meetingId }: { meetingId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("meetings");
  const wsPaths = useWorkspacePaths();
  const { data, isLoading, isError } = useQuery(
    meetingDetailOptions(wsId, meetingId),
  );

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader>
          <Skeleton className="h-4 w-40" />
        </PageHeader>
        <div className="flex-1 overflow-y-auto" aria-busy="true">
          <span className="sr-only">{t(($) => $.detail.loading)}</span>
          <div className="mx-auto max-w-4xl space-y-6 p-6">
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-24 w-full rounded-lg" />
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-16 w-full rounded-lg" />
          </div>
        </div>
      </div>
    );
  }

  if (isError || !data || !data.id) {
    return (
      <CollectionPageState
        icon={AudioLines}
        title={t(($) => $.detail.load_error)}
        tone="destructive"
        role="alert"
      />
    );
  }

  return (
    <div className="flex h-full flex-col">
      <BreadcrumbHeader
        segments={[
          { href: wsPaths.meetings(), label: t(($) => $.detail.back) },
        ]}
        leaf={
          <span className="min-w-0 truncate font-medium">{data.title}</span>
        }
        actions={<MeetingStatusChip status={data.status} />}
      />

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-4xl flex-col gap-6 p-6">
          <MeetingMeta meeting={data} />
          {data.status === "recording" ? <RecorderSlot meetingId={data.id} /> : null}
          <SummarySection meeting={data} />
          <ActionsSection meeting={data} />
          <TranscriptSection transcript={data.transcript} />
        </div>
      </div>
    </div>
  );
}

/**
 * The live recorder belongs to the client that started the recording — the
 * MediaRecorder lives in that tab. A second viewer of the same meeting sees a
 * plain "recording" note instead of controls they cannot drive.
 */
function RecorderSlot({ meetingId }: { meetingId: string }) {
  const { t } = useT("meetings");
  const activeId = useMeetingRecorderStore((s) => s.meetingId);
  if (activeId === meetingId) return <MeetingRecorderPanel />;
  return (
    <p className="rounded-lg border p-3 text-caption text-muted-foreground">
      {t(($) => $.recorder.elsewhere)}
    </p>
  );
}

function MeetingStatusChip({ status }: { status: string }) {
  const { t } = useT("meetings");
  const known =
    status === "recording" ||
    status === "summarizing" ||
    status === "done" ||
    status === "failed"
      ? status
      : null;
  return (
    <Badge variant="outline" className="gap-1.5">
      <span
        aria-hidden="true"
        className={cn("size-1.5 rounded-full", meetingStatusDotClass(status))}
      />
      {known ? t(($) => $.status[known]) : t(($) => $.status.unknown)}
    </Badge>
  );
}

function MeetingMeta({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const timeAgo = useTimeAgo();
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
      <span>{meeting.app_name || t(($) => $.list.no_app)}</span>
      <span>{t(($) => $.detail.started, { time: timeAgo(meeting.started_at) })}</span>
      {meeting.ended_at ? (
        <span>{t(($) => $.detail.ended, { time: timeAgo(meeting.ended_at) })}</span>
      ) : null}
      <span className="tabular-nums">
        {t(($) => $.list.segments, { count: meeting.segment_count })}
      </span>
    </div>
  );
}

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="text-caption font-medium text-muted-foreground">{children}</h2>
  );
}

function SummarySection({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  return (
    <section className="flex flex-col gap-2">
      <SectionHeading>{t(($) => $.detail.summary_title)}</SectionHeading>
      {meeting.status === "summarizing" ? (
        <div className="flex flex-col gap-2" aria-busy="true">
          <span className="flex items-center gap-2 text-caption text-muted-foreground">
            <Spinner className="size-3.5" />
            {t(($) => $.detail.summary_pending)}
          </span>
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : meeting.summary_markdown ? (
        <div className="rounded-lg border p-3">
          <RichContent content={meeting.summary_markdown} density="document" />
        </div>
      ) : (
        <p className="text-caption text-muted-foreground">
          {meeting.summary_unavailable
            ? t(($) => $.detail.summary_unavailable)
            : t(($) => $.detail.summary_empty)}
        </p>
      )}
    </section>
  );
}

function ActionsSection({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  if (meeting.status === "summarizing") return null;
  return (
    <section className="flex flex-col gap-2">
      <SectionHeading>{t(($) => $.detail.actions_title)}</SectionHeading>
      {meeting.actions.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.actions_empty)}
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {meeting.actions.map((action) => (
            <ActionRow key={action.triage_item_id} action={action} />
          ))}
        </ul>
      )}
    </section>
  );
}

const ACTION_STATES = [
  "pending",
  "accepted",
  "dismissed",
  "merged",
  "superseded",
  "expired",
  "dropped",
] as const;

function ActionRow({ action }: { action: MeetingAction }) {
  const { t } = useT("meetings");
  // Triage states are server-driven; an unknown one shows as-is rather than
  // crashing the row (API compatibility rule).
  const actionStateLabel = (state: string): string => {
    const known = (ACTION_STATES as readonly string[]).includes(state)
      ? (state as (typeof ACTION_STATES)[number])
      : null;
    return known ? t(($) => $.action_state[known]) : state;
  };
  const wsPaths = useWorkspacePaths();
  // An accepted action already has an issue: link there, since the triage
  // queue no longer shows it.
  const href = action.issue_id
    ? wsPaths.issueDetail(action.issue_id)
    : wsPaths.triage();
  return (
    <li className="flex items-center gap-2 rounded-lg border px-2 py-2">
      <span className="min-w-0 flex-1 truncate text-body">{action.title}</span>
      <Badge variant="secondary" className="shrink-0">
        {actionStateLabel(action.state)}
      </Badge>
      <AppLink
        href={href}
        newTabTitle={action.title}
        className="flex shrink-0 items-center gap-1 text-caption text-muted-foreground transition-colors hover:text-foreground"
      >
        <ExternalLink aria-hidden="true" className="size-3.5" />
        {action.issue_id
          ? t(($) => $.detail.actions_open_issue)
          : t(($) => $.detail.actions_open_triage)}
      </AppLink>
    </li>
  );
}

function TranscriptSection({ transcript }: { transcript: string }) {
  const { t } = useT("meetings");
  // Collapsed by default: a transcript is long, and the summary above it is
  // what a reader came for.
  const [open, setOpen] = useState(false);

  return (
    <section className="flex flex-col gap-2">
      <SectionHeading>{t(($) => $.detail.transcript_title)}</SectionHeading>
      {transcript ? (
        <>
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            className="flex w-fit items-center gap-1 text-caption text-muted-foreground transition-colors hover:text-foreground"
          >
            {open ? (
              <ChevronDown aria-hidden="true" className="size-3.5" />
            ) : (
              <ChevronRight aria-hidden="true" className="size-3.5" />
            )}
            {open
              ? t(($) => $.detail.transcript_hide)
              : t(($) => $.detail.transcript_show)}
          </button>
          {open ? (
            <pre className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-caption whitespace-pre-wrap">
              {transcript}
            </pre>
          ) : null}
        </>
      ) : (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.transcript_empty)}
        </p>
      )}
    </section>
  );
}
