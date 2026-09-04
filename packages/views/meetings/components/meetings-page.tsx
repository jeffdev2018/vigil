"use client";

import { useState } from "react";
import { AudioLines, Info, Mic, MoreHorizontal, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useConfigStore } from "@multica/core/config";
import { useWorkspacePaths } from "@multica/core/paths";
import { meetingListOptions } from "@multica/core/meetings/queries";
import { useDeleteMeeting } from "@multica/core/meetings/mutations";
import {
  openMeetingRecorder,
  useMeetingRecorderStore,
} from "@multica/core/meetings/store";
import type { Meeting } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useRowLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT, useTimeAgo } from "../../i18n";
import { DeleteMeetingDialog } from "./delete-meeting-dialog";

/**
 * Dot color per meeting status. Server-driven enum — an unknown value falls
 * through to the neutral dot rather than crashing (API compatibility rule).
 */
export function meetingStatusDotClass(status: string): string {
  switch (status) {
    case "recording":
      return "bg-red-500 animate-pulse";
    case "summarizing":
      return "bg-blue-500";
    case "done":
      return "bg-emerald-500";
    case "failed":
      return "bg-red-500";
    default:
      return "bg-muted-foreground/40";
  }
}

const KNOWN_STATUSES = ["recording", "summarizing", "done", "failed"] as const;

type KnownStatus = (typeof KNOWN_STATUSES)[number];

function knownStatus(status: string): KnownStatus | null {
  return (KNOWN_STATUSES as readonly string[]).includes(status)
    ? (status as KnownStatus)
    : null;
}

export function MeetingsPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("meetings");
  const meetingsQuery = useQuery(meetingListOptions(wsId));
  // The server declares its speech-to-text provider in /api/config; a 409
  // `stt_not_configured` on a start attempt is the same fact learned late.
  // Either way it is a capability, not an error to alarm the user with.
  const transcriptionAvailable = useConfigStore(
    (s) => s.meetingTranscriptionAvailable,
  );
  const sttRefused = useMeetingRecorderStore((s) => s.sttUnavailable);
  const sttUnavailable = sttRefused || !transcriptionAvailable;
  const recorderPhase = useMeetingRecorderStore((s) => s.phase);

  const meetings = meetingsQuery.data?.meetings ?? [];

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={AudioLines}
        title={t(($) => $.list.title)}
        count={meetings.length}
        description={t(($) => $.list.subtitle)}
        actions={
          <CollectionPageHeaderAction
            icon={Mic}
            label={t(($) => $.list.record)}
            disabled={sttUnavailable || recorderPhase !== "idle"}
            onClick={() => openMeetingRecorder()}
          />
        }
      />

      {sttUnavailable ? <CapabilityBanner /> : null}

      <MeetingList
        meetings={meetings}
        isLoading={meetingsQuery.isLoading}
        isError={meetingsQuery.isError}
      />
    </div>
  );
}

/** Quiet, non-blocking notice: the deployment has no speech-to-text provider. */
function CapabilityBanner() {
  const { t } = useT("meetings");
  return (
    <div
      role="status"
      className={cn(
        "flex shrink-0 items-start gap-2 border-b py-2 text-caption text-muted-foreground",
        PAGE_GUTTER,
      )}
    >
      <Info aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
      <p>
        <span className="font-medium text-foreground">
          {t(($) => $.capability.title)}
        </span>{" "}
        {t(($) => $.capability.description)}
      </p>
    </div>
  );
}

function MeetingList({
  meetings,
  isLoading,
  isError,
}: {
  meetings: Meeting[];
  isLoading: boolean;
  isError: boolean;
}) {
  const { t } = useT("meetings");

  if (isLoading) {
    return (
      <div className="flex flex-col gap-2 p-2" aria-busy="true">
        <span className="sr-only">{t(($) => $.list.loading)}</span>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-12 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <CollectionPageState
        icon={AudioLines}
        title={t(($) => $.list.load_error)}
        tone="destructive"
        role="alert"
      />
    );
  }

  if (meetings.length === 0) {
    return (
      <CollectionPageState
        icon={AudioLines}
        title={t(($) => $.list.empty_title)}
        description={t(($) => $.list.empty_description)}
      />
    );
  }

  return (
    <ul className="flex min-w-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
      {meetings.map((meeting) => (
        <MeetingRow key={meeting.id} meeting={meeting} />
      ))}
    </ul>
  );
}

function MeetingRow({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const timeAgo = useTimeAgo();
  const wsPaths = useWorkspacePaths();
  const rowLink = useRowLink();
  const status = knownStatus(meeting.status);

  return (
    <li>
      <div
        {...rowLink(wsPaths.meetingDetail(meeting.id), meeting.title)}
        role="button"
        tabIndex={0}
        className="flex w-full cursor-pointer items-center gap-2 rounded-lg border border-transparent px-2 py-2 text-left transition-colors hover:bg-accent/60"
      >
        <span
          title={status ? t(($) => $.status[status]) : meeting.status}
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            meetingStatusDotClass(meeting.status),
          )}
        />
        <span className="min-w-0 flex-1 truncate text-body">{meeting.title}</span>
        <span className="hidden w-28 shrink-0 truncate text-caption text-muted-foreground sm:block">
          {meeting.app_name || t(($) => $.list.no_app)}
        </span>
        <span className="hidden w-24 shrink-0 text-caption tabular-nums text-muted-foreground md:block">
          {t(($) => $.list.segments, { count: meeting.segment_count })}
        </span>
        {meeting.action_count > 0 ? (
          <span className="hidden w-28 shrink-0 text-caption tabular-nums text-muted-foreground md:block">
            {t(($) => $.list.actions, { count: meeting.action_count })}
          </span>
        ) : null}
        <span className="w-20 shrink-0 text-right text-caption tabular-nums text-muted-foreground">
          {timeAgo(meeting.started_at)}
        </span>
        {meeting.can_manage ? <MeetingRowMenu meeting={meeting} /> : null}
      </div>
    </li>
  );
}

/**
 * Row actions. Always visible rather than hover-revealed: the only action is
 * destructive and a meetings row has no other affordance competing for the
 * space, so a hidden control would just be one a touch pointer never finds.
 */
function MeetingRowMenu({ meeting }: { meeting: Meeting }) {
  const { t } = useT("meetings");
  const wsId = useWorkspaceId();
  const deleteMeeting = useDeleteMeeting(wsId);
  const [confirming, setConfirming] = useState(false);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.list.row_actions_aria, { title: meeting.title })}
              // The row owns click-to-open; the menu must not navigate.
              onPointerDown={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
              className="inline-flex size-7 shrink-0 items-center justify-center rounded text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring data-popup-open:bg-accent data-popup-open:text-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuItem
            variant="destructive"
            onClick={(e) => {
              e.stopPropagation();
              setConfirming(true);
            }}
          >
            <Trash2 className="size-4" />
            {t(($) => $.list.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DeleteMeetingDialog
        open={confirming}
        title={meeting.title}
        pending={deleteMeeting.isPending}
        onOpenChange={setConfirming}
        onConfirm={() => {
          deleteMeeting
            .mutateAsync(meeting.id)
            .then(() => setConfirming(false))
            .catch(() => toast.error(t(($) => $.delete_dialog.error)));
        }}
      />
    </>
  );
}
