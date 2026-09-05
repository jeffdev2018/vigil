"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AudioLines,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  Pencil,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  isMeetingSummaryStalled,
  meetingDetailOptions,
} from "@multica/core/meetings/queries";
import { useMeetingRecorderStore } from "@multica/core/meetings/store";
import {
  useDeleteMeeting,
  useEditMeetingSegment,
  useFinishMeeting,
  useRenameMeeting,
  useResummarizeMeeting,
} from "@multica/core/meetings/mutations";
import { useAuthStore } from "@multica/core/auth";
import { toast } from "sonner";
import type { Meeting, MeetingAction } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { CollectionPageState } from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { RichContent } from "../../rich-content";
import { useT, useTimeAgo } from "../../i18n";
import { TitleEditor } from "../../editor";
import { useNavigation } from "../../navigation";
import { DeleteMeetingDialog } from "./delete-meeting-dialog";
import {
  formatTranscriptLine,
  parseTranscriptBlocks,
  parseTranscriptLines,
  type TranscriptLine,
} from "../transcript-speakers";
import { MeetingRecorderPanel } from "./meeting-recorder";
import { meetingStatusDotClass } from "./meetings-page";

export function MeetingDetailPage({ meetingId }: { meetingId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("meetings");
  const wsPaths = useWorkspacePaths();
  const { data, isLoading, isError } = useQuery(
    meetingDetailOptions(wsId, meetingId),
  );
  // Editing the transcript deliberately does not re-run the summary: the model
  // call is slow and costs money, and only the reader knows whether the
  // correction changed anything the summary said. So we say so, and point at
  // the button that already exists. Session-scoped on purpose — the reminder
  // is about what THIS reader just changed.
  const [transcriptEdited, setTranscriptEdited] = useState(false);

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
        actions={
          <div className="flex items-center gap-2">
            <MeetingStatusChip status={data.status} />
            {data.can_manage ? <DeleteMeetingAction meeting={data} /> : null}
          </div>
        }
      />

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-4xl flex-col gap-6 p-6">
          <MeetingTitle meeting={data} />
          <MeetingMeta meeting={data} />
          {data.status === "recording" ? (
            <RecorderSlot meeting={data} />
          ) : null}
          <SummarySection meeting={data} transcriptEdited={transcriptEdited} />
          <ActionsSection meeting={data} />
          <TranscriptSection
            meeting={data}
            onEdited={() => setTranscriptEdited(true)}
          />
        </div>
      </div>
    </div>
  );
}

/**
 * The live recorder belongs to the client that started the recording — the
 * MediaRecorder lives in that tab. Anyone else sees a "recording elsewhere"
 * note. The recorder themself also lands here after a refresh, when the
 * MediaRecorder is gone for good: they get a way to close the meeting, since
 * nothing else ever will (the server only accepts finish from the creator).
 */
function RecorderSlot({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const activeId = useMeetingRecorderStore((s) => s.meetingId);
  const userId = useAuthStore((s) => s.user?.id);
  const finishMeeting = useFinishMeeting(wsId);
  if (activeId === meeting.id) return <MeetingRecorderPanel />;
  const isRecorder = !!userId && userId === meeting.created_by;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border p-3">
      <p className="min-w-0 flex-1 text-caption text-muted-foreground">
        {isRecorder ? t(($) => $.recorder.orphaned) : t(($) => $.recorder.elsewhere)}
      </p>
      {meeting.can_manage ? (
        <Button
          size="sm"
          variant="outline"
          disabled={finishMeeting.isPending}
          onClick={() => {
            finishMeeting.mutateAsync(meeting.id).catch(() => {
              toast.error(t(($) => $.recorder.error_finish));
            });
          }}
        >
          {t(($) => $.recorder.finish_now)}
        </Button>
      ) : null}
    </div>
  );
}

/**
 * Delete from the detail page. The list is awaited-then-navigated (never
 * optimistic): the user is standing on the page that is about to stop
 * existing, so the server has to have agreed before we leave it.
 */
function DeleteMeetingAction({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const deleteMeeting = useDeleteMeeting(wsId);
  const [confirming, setConfirming] = useState(false);

  return (
    <>
      <Button
        size="sm"
        variant="ghost"
        className="text-muted-foreground hover:text-destructive"
        onClick={() => setConfirming(true)}
      >
        <Trash2 aria-hidden="true" className="size-3.5" />
        {t(($) => $.detail.delete)}
      </Button>
      <DeleteMeetingDialog
        open={confirming}
        title={meeting.title}
        pending={deleteMeeting.isPending}
        onOpenChange={setConfirming}
        onConfirm={() => {
          deleteMeeting
            .mutateAsync(meeting.id)
            .then(() => {
              setConfirming(false);
              push(wsPaths.meetings());
            })
            .catch(() => toast.error(t(($) => $.delete_dialog.error)));
        }}
      />
    </>
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

/**
 * The title, edited in place by whoever may manage the meeting — same
 * blur-to-save shape as the issue title. `key` is the server's value, so a
 * rename (or another client's) re-seeds the editor instead of leaving a stale
 * document behind; the editor is already blurred by then.
 */
function MeetingTitle({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const renameMeeting = useRenameMeeting(wsId);

  if (!meeting.can_manage) {
    return <h1 className="text-title font-semibold">{meeting.title}</h1>;
  }
  return (
    <TitleEditor
      key={meeting.title}
      defaultValue={meeting.title}
      placeholder={t(($) => $.detail.title_placeholder)}
      className="w-full text-title font-semibold"
      onBlur={(value) => {
        const trimmed = value.trim();
        // An empty title is refused by the server; treat it as "no change"
        // rather than bouncing a 400 back at someone who just selected all.
        if (!trimmed || trimmed === meeting.title) return;
        renameMeeting
          .mutateAsync({ meetingId: meeting.id, title: trimmed })
          .catch(() => toast.error(t(($) => $.detail.rename_error)));
      }}
    />
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

function SummarySection({
  meeting,
  transcriptEdited,
}: {
  meeting: Meeting;
  transcriptEdited: boolean;
}) {
  const { t } = useT("meetings");
  // A summary that is still running owns the section; one that stalled is the
  // main reason this button exists, so it appears there too.
  const stalled = isMeetingSummaryStalled(meeting);
  const canRegenerate =
    meeting.can_manage && (meeting.status !== "recording") &&
    (meeting.status !== "summarizing" || stalled);
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <SectionHeading>{t(($) => $.detail.summary_title)}</SectionHeading>
        {canRegenerate ? <ResummarizeButton meeting={meeting} /> : null}
      </div>
      {transcriptEdited && canRegenerate ? (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.detail.transcript_edited_hint)}
        </p>
      ) : null}
      {stalled ? (
        // The finish request that owned this summary is gone (a closed tab, a
        // restarted server): nothing will move the row on its own, so stop
        // pretending it is still working and offer the way out.
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.detail.summary_stalled)}
        </p>
      ) : meeting.status === "summarizing" ? (
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

/**
 * Re-runs the summary + action-item extraction. Offered on any meeting that
 * stopped recording, not only an empty one: the reasons a summary is missing or
 * poor (no model configured at the time, a provider blip, a finish that died
 * with the tab) are all fixed by asking again.
 */
function ResummarizeButton({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const resummarize = useResummarizeMeeting(wsId);
  return (
    <Button
      size="sm"
      variant="outline"
      disabled={resummarize.isPending}
      onClick={() => {
        resummarize.mutateAsync(meeting.id).catch(() => {
          toast.error(t(($) => $.detail.resummarize_error));
        });
      }}
    >
      {resummarize.isPending ? (
        <Spinner className="size-3.5" />
      ) : (
        <RefreshCw aria-hidden="true" className="size-3.5" />
      )}
      {t(($) => $.detail.resummarize)}
    </Button>
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
    : wsPaths.triage(action.triage_item_id);
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

/**
 * The transcript, one paragraph per speaker turn. A diarized batch transcript
 * arrives as "Speaker 1: …" lines; a live one has no speakers and renders as
 * plain paragraphs (see transcript-speakers.ts for the parsing rules).
 */
function TranscriptBody({ transcript }: { transcript: string }) {
  const blocks = useMemo(() => parseTranscriptBlocks(transcript), [transcript]);
  return (
    <div className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3">
      <div className="flex flex-col gap-3">
        {blocks.map((block, index) => (
          <p key={index} className="text-caption leading-relaxed">
            {block.speaker ? (
              <span className="mr-2 font-medium text-muted-foreground">
                {block.speaker}
              </span>
            ) : null}
            <span className="whitespace-pre-wrap">{block.text}</span>
          </p>
        ))}
      </div>
    </div>
  );
}

/**
 * The editable transcript: one paragraph per STORED line, not per speaker run.
 * A saved edit rewrites exactly one line (PATCH .../segments/{seq}), so
 * merging consecutive lines the way the read view does would make a paragraph
 * unaddressable. Clicking one opens a textarea; Enter or blur saves it,
 * Escape drops it.
 */
function TranscriptEditor({
  meeting,
  onEdited,
}: {
  meeting: Meeting;
  onEdited: () => void;
}) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const editSegment = useEditMeetingSegment(wsId);
  const lines = useMemo(
    () => parseTranscriptLines(meeting.transcript),
    [meeting.transcript],
  );
  const [editing, setEditing] = useState<number | null>(null);
  // Escape and Enter both close the textarea, and closing it fires blur in
  // some browsers. Without this the save would run twice.
  const handled = useRef(false);

  const save = (line: TranscriptLine, value: string) => {
    handled.current = true;
    setEditing(null);
    const next = value.trim();
    if (!next || next === line.text) return;
    editSegment
      .mutateAsync({
        meetingId: meeting.id,
        seq: line.index,
        text: formatTranscriptLine(line.speaker, next),
      })
      .then(() => onEdited())
      .catch(() => toast.error(t(($) => $.detail.transcript_edit_error)));
  };

  return (
    <div className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3">
      <div className="flex flex-col gap-3">
        {lines.map((line) => (
          <div key={line.index} className="flex flex-col gap-1">
            {line.speaker ? (
              <span className="text-caption font-medium text-muted-foreground">
                {line.speaker}
              </span>
            ) : null}
            {editing === line.index ? (
              <Textarea
                autoFocus
                defaultValue={line.text}
                aria-label={t(($) => $.detail.transcript_edit_label)}
                className="min-h-16 text-caption leading-relaxed"
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    handled.current = true;
                    setEditing(null);
                    return;
                  }
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    save(line, event.currentTarget.value);
                  }
                }}
                onBlur={(event) => {
                  if (handled.current) {
                    handled.current = false;
                    return;
                  }
                  save(line, event.target.value);
                }}
              />
            ) : (
              <button
                type="button"
                onClick={() => {
                  handled.current = false;
                  setEditing(line.index);
                }}
                className="rounded-sm px-1 py-0.5 text-left text-caption leading-relaxed whitespace-pre-wrap transition-colors hover:bg-background"
              >
                {line.text}
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function TranscriptSection({
  meeting,
  onEdited,
}: {
  meeting: Meeting;
  onEdited: () => void;
}) {
  const { t } = useT("meetings");
  // Collapsed by default: a transcript is long, and the summary above it is
  // what a reader came for.
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  // A recording is still being appended to and a summarizing one is being
  // read by the model, so only a finished transcript is editable — the same
  // rule the server enforces (`meeting_not_done`).
  const canEdit = meeting.can_manage === true && meeting.status === "done";

  return (
    <section className="flex flex-col gap-2">
      <SectionHeading>{t(($) => $.detail.transcript_title)}</SectionHeading>
      {meeting.transcript ? (
        <>
          <div className="flex items-center justify-between gap-2">
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
            {open && canEdit ? (
              <Button
                size="sm"
                variant={editing ? "secondary" : "ghost"}
                onClick={() => setEditing((v) => !v)}
              >
                <Pencil aria-hidden="true" className="size-3.5" />
                {editing
                  ? t(($) => $.detail.transcript_edit_done)
                  : t(($) => $.detail.transcript_edit)}
              </Button>
            ) : null}
          </div>
          {open ? (
            editing && canEdit ? (
              <TranscriptEditor meeting={meeting} onEdited={onEdited} />
            ) : (
              <TranscriptBody transcript={meeting.transcript} />
            )
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
